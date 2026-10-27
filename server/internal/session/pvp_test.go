package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/grounditems"
	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/transport"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// pvpEverywhere is a map that is all PvP zone, respawning at spawn.
type pvpEverywhere struct{ spawn world.Vec2 }

func (pvpEverywhere) AreaAt(world.Vec2) areas.Area { return areas.PvPZone }
func (z pvpEverywhere) Spawn() world.Vec2          { return z.spawn }

// pvpRand rolls the same values every time: Float64 returns f and IntN the
// max hit.
type pvpRand struct{ f float64 }

func (r pvpRand) Float64() float64 { return r.f }
func (pvpRand) IntN(n int) int     { return n - 1 }

// econSpy records each committed ApplyEconomy call's audit entries.
type econSpy struct {
	*store.Memory
	mu    sync.Mutex
	calls [][]store.Entry
}

func (e *econSpy) ApplyEconomy(ctx context.Context, changes []store.EconomyChange, entries []store.Entry) error {
	if err := e.Memory.ApplyEconomy(ctx, changes, entries); err != nil {
		return err
	}
	e.mu.Lock()
	e.calls = append(e.calls, entries)
	e.mu.Unlock()
	return nil
}

// callsWithReason returns the committed calls with an entry whose payload
// reason is reason.
func (e *econSpy) callsWithReason(t *testing.T, reason string) [][]store.Entry {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	var out [][]store.Entry
	for _, c := range e.calls {
		for _, en := range c {
			if entryReason(t, en) == reason {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func entryReason(t *testing.T, e store.Entry) string {
	t.Helper()
	b, err := json.Marshal(e.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var p struct{ Reason string }
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p.Reason
}

type pvpRig struct {
	*rig
	data   *gamedata.Data
	combat *pvp.Combat
	ground *grounditems.System
	store  *econSpy
}

// newPvPRig is a rig with ground items and PvP combat wired as in main.go:
// combat on every 12th tick, hits and drops going to the session. New guests
// spawn at the origin, and everywhere is the PvP zone.
func newPvPRig(t *testing.T, r pvpRand) *pvpRig {
	t.Helper()
	d := testData(t)
	clock := world.NewFakeClock(time.Unix(0, 0))
	w := world.New(world.DefaultConfig())
	spy := &econSpy{Memory: store.NewMemory()}
	sess := session.New(session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d}, w, clock, spy, nil)
	t.Cleanup(sess.Close)
	mv, err := movement.New(movement.Config{MaxSpeed: 5})
	if err != nil {
		t.Fatal(err)
	}
	ground := grounditems.New(d, clock, tileSize)
	sess.SetGroundItems(ground)
	combat, err := pvp.NewCombat(d, pvpEverywhere{}, tileSize, clock, r, sess, sess)
	if err != nil {
		t.Fatal(err)
	}
	sess.SetPvP(combat)
	for _, s := range []struct {
		sys   world.System
		every int
	}{{mv, 1}, {ground, 1}, {combat, 12}, {sess, 1}} {
		if err := w.RegisterEvery(s.sys, s.every); err != nil {
			t.Fatal(err)
		}
	}
	st := &stats{}
	srv := httptest.NewServer(transport.NewWebSocket(st, transport.WithSendQueue(1024)).HTTPHandler(sess.ServeConn))
	t.Cleanup(srv.Close)
	return &pvpRig{
		rig:  &rig{t: t, w: w, clock: clock, sess: sess, url: "ws" + strings.TrimPrefix(srv.URL, "http"), stats: st},
		data: d, combat: combat, ground: ground, store: spy,
	}
}

func attackTarget(id uint16) []byte {
	return envelope(protocol.MessageAttackTarget, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.AttackTargetStart(b)
		protocol.AttackTargetAddTargetId(b, id)
		return protocol.AttackTargetEnd(b)
	})
}

// settle makes sure every message c sent before now has been applied: the
// Pong comes after the earlier messages were queued, and one more tick
// applies them.
func (r *pvpRig) settle(c *client) {
	r.t.Helper()
	c.send(ping(7))
	r.waitFor(c, protocol.MessagePong)
	r.w.Tick()
}

// AC1: AttackTarget naming a player reaches the PvP combat system; one naming
// nobody, an NPC id, or oneself is ignored.
func TestAttackTargetPlayerReachesPvPCombat(t *testing.T) {
	r := newPvPRig(t, pvpRand{f: 0.999999})
	ca := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	a, _, _, _, _ := joinFully(t, r.rig, ca)
	cb := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	b, _, _, _, _ := joinFully(t, r.rig, cb)

	for _, bad := range []uint16{0, 999, uint16(world.DefaultNPCIDMin), uint16(a)} {
		ca.send(attackTarget(bad))
	}
	r.settle(ca)
	if v, ok := r.combat.TargetOf(a); ok {
		t.Fatalf("after bad AttackTargets player %d targets %d, want none", a, v)
	}

	ca.send(attackTarget(uint16(b)))
	r.settle(ca)
	if v, ok := r.combat.TargetOf(a); !ok || v != b {
		t.Fatalf("TargetOf(%d) = %d, %v; want %d, true", a, v, ok, b)
	}
}

// combatHit decodes a CombatHit.
func combatHit(tbl flatbuffers.Table) (uint32, []pvp.Hit) {
	var ch protocol.CombatHit
	ch.Init(tbl.Bytes, tbl.Pos)
	out := make([]pvp.Hit, ch.HitsLength())
	var h protocol.Hit
	for i := range out {
		ch.Hits(&h, i)
		out[i] = pvp.Hit{Attacker: world.PlayerID(h.Attacker()), Target: world.PlayerID(h.Target()),
			Damage: int(h.Damage()), Died: h.Died()}
	}
	return ch.Tick(), out
}

// AC2: each combat tick's hits arrive as one CombatHit carrying that tick's
// world tick number, sent to the fighters and to a player who sees them, but
// not to a player out of view.
func TestCombatHitSentToPlayersInView(t *testing.T) {
	r := newPvPRig(t, pvpRand{f: 0.999999}) // every swing misses: nobody dies
	v := uint16(protocol.ProtocolVersionCurrent)
	ca, cb, cn, cf := r.dial(hello(v, "")), r.dial(hello(v, "")), r.dial(hello(v, "")), r.dial(hello(v, ""))
	a, _, _, _, _ := joinFully(t, r.rig, ca)
	b, _, _, _, _ := joinFully(t, r.rig, cb)
	joinFully(t, r.rig, cn)
	far, _, _, _, _ := joinFully(t, r.rig, cf)
	r.w.Player(far).Pos = world.Vec2{X: 500, Y: 500} // out of everyone's view
	r.w.Player(far).HasDesired = false

	ca.send(attackTarget(uint16(b)))
	want := pvp.Hit{Attacker: a, Target: b}
	for _, c := range []*client{ca, cb, cn} {
		tick, hits := combatHit(r.waitFor(c, protocol.MessageCombatHit))
		if tick%12 != 0 {
			t.Errorf("CombatHit tick %d is not a combat tick", tick)
		}
		if len(hits) != 1 || hits[0] != want {
			t.Fatalf("hits = %+v, want [%+v]", hits, want)
		}
		// The Snapshot of the same world tick follows.
		typ, tbl := r.next(c)
		if typ != protocol.MessageSnapshot {
			t.Fatalf("after CombatHit got %v, want Snapshot", typ)
		}
		var s protocol.Snapshot
		s.Init(tbl.Bytes, tbl.Pos)
		if s.Tick() != tick {
			t.Errorf("CombatHit tick %d, Snapshot tick %d", tick, s.Tick())
		}
	}
	// Read the far player's messages up to a Snapshot three combat ticks
	// later; every combat tick in between had a swing.
	end := uint32(r.w.TickCount()) + 36
	for {
		typ, tbl := r.next(cf)
		if typ == protocol.MessageCombatHit {
			t.Fatal("player out of view got a CombatHit")
		}
		if typ != protocol.MessageSnapshot {
			continue
		}
		var s protocol.Snapshot
		if s.Init(tbl.Bytes, tbl.Pos); s.Tick() >= end {
			break
		}
	}
}

// holdingsCount counts every item a player holds, worn or carried, and gold
// (under item 0).
func holdingsCount(h store.Holdings) map[int32]int64 {
	out := map[int32]int64{}
	for _, s := range h.Inventory {
		if s.Item != 0 {
			out[s.Item] += s.Qty
		}
	}
	for _, e := range h.Equipment {
		if e != 0 {
			out[e]++
		}
	}
	out[0] = h.Gold
	return out
}

func hashTok(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// AC3: a PvP death's outbox changes are written in one transaction with
// pvp_death audit entries, and the dropped stacks spawn as ground items owned
// by the killer, so the store and the ground together hold exactly the
// victim's holdings from before the death.
func TestPvPDeathSavesAndDropsExactHoldings(t *testing.T) {
	r := newPvPRig(t, pvpRand{f: 0}) // every swing hits for the max
	v := uint16(protocol.ProtocolVersionCurrent)
	ca, cb := r.dial(hello(v, "")), r.dial(hello(v, ""))
	a, _, _, _, _ := joinFully(t, r.rig, ca)
	b, tokB, _, _, _ := joinFully(t, r.rig, cb)
	loadB := func() store.Holdings {
		sp, err := r.store.LoadByTokenHash(context.Background(), hashTok(tokB))
		if err != nil {
			t.Fatal(err)
		}
		return sp.Holdings
	}

	// Give the victim more than they keep, through the outbox so the store
	// has it too.
	pb := r.w.Player(b)
	trinket, ok := r.data.ItemByKey("shiny_trinket")
	if !ok {
		t.Fatal("no shiny_trinket")
	}
	ch, err := pb.Inv.Add(r.data, trinket.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	pb.RecordEconomy("pickup", ch)
	r.w.Tick()
	waitFor(t, "pick-up saved", func() bool { return holdingsCount(loadB())[int32(trinket.ID)] == 5 })
	before := holdingsCount(loadB())

	pb.HP = 1
	deathPos := pb.Pos
	ca.send(attackTarget(uint16(b)))
	_, hits := combatHit(r.waitFor(cb, protocol.MessageCombatHit))
	if len(hits) != 1 || hits[0].Attacker != a || hits[0].Target != b || !hits[0].Died {
		t.Fatalf("hits = %+v, want one killing hit from %d on %d", hits, a, b)
	}
	if r.ground.Len() == 0 {
		t.Fatal("no ground items after the death")
	}
	waitFor(t, "pvp_death saved", func() bool { return len(r.store.callsWithReason(t, pvp.ReasonPvPDeath)) > 0 })

	after := holdingsCount(loadB())
	var dropped int
	for _, it := range r.ground.Near(nil, deathPos, 1) {
		if it.Owner != a {
			t.Errorf("ground item %+v owned by %d, want the killer %d", it, it.Owner, a)
		}
		after[int32(it.Item)] += int64(it.Qty)
		dropped++
	}
	if dropped != r.ground.Len() {
		t.Errorf("%d ground items at the death spot, %d in all", dropped, r.ground.Len())
	}
	maps.DeleteFunc(after, func(k int32, v int64) bool { return k != 0 && v == 0 })
	if !maps.Equal(after, before) {
		t.Fatalf("store + ground = %v, want the pre-death holdings %v", after, before)
	}

	calls := r.store.callsWithReason(t, pvp.ReasonPvPDeath)
	if len(calls) != 1 {
		t.Fatalf("%d transactions with pvp_death entries, want 1", len(calls))
	}
	if len(calls[0]) != dropped {
		t.Errorf("pvp_death transaction has %d audit entries, want one per dropped stack (%d)", len(calls[0]), dropped)
	}
	for _, e := range calls[0] {
		if got := entryReason(t, e); got != pvp.ReasonPvPDeath {
			t.Errorf("entry %+v has reason %q, want %q", e, got, pvp.ReasonPvPDeath)
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}
