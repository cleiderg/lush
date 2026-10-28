package session_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	pvecombat "github.com/cleiderg/lush/server/internal/systems/combat"
	"github.com/cleiderg/lush/server/internal/world"
)

// coyoteAlpha is the NPC type from the default NPC table with an item drop
// at chance 1 (3 coyote pelts) and 40..100 gold.
const coyoteAlpha world.NPCTypeID = 6

// killRig is a PvP rig with player a next to a one-HP coyote alpha targeted
// by a, and player b out of the coyote's reach. Every roll is the max, so
// a's first swing kills it, the gold is 100 and only chance-1 drops land.
type killRig struct {
	*pvpRig
	ca, cb  *client
	a, b    world.PlayerID
	tokA    string
	npc     *world.NPC
	killPos world.Vec2
}

func newKillRig(t *testing.T) *killRig {
	t.Helper()
	r := newPvPRig(t, pvpRand{f: 0})
	v := uint16(protocol.ProtocolVersionCurrent)
	ca, cb := r.dial(hello(v, "")), r.dial(hello(v, ""))
	a, tokA, _, _, _ := joinFully(t, r.rig, ca)
	b, _, _, _, _ := joinFully(t, r.rig, cb)
	pb := r.w.Player(b)
	pb.Pos, pb.HasDesired = world.Vec2{X: 500, Y: 500}, false
	pa := r.w.Player(a)
	pa.HasDesired = false
	n := addNPC(t, r.w, coyoteAlpha, pa.Pos)
	ca.send(attackTarget(uint16(n.ID)))
	r.settle(ca)
	if got, ok := r.pve.TargetOf(a); !ok || got != n.ID {
		t.Fatalf("PvE TargetOf(%d) = %d, %v; want %d", a, got, ok, n.ID)
	}
	return &killRig{pvpRig: r, ca: ca, cb: cb, a: a, b: b, tokA: tokA, npc: n, killPos: n.Pos}
}

// tickUntilKill ticks the world from the test goroutine until the NPC is
// gone and returns the tick of the kill.
func (r *killRig) tickUntilKill() uint64 {
	r.t.Helper()
	for i := 0; i < 48; i++ {
		r.w.Tick()
		if r.w.NPC(r.npc.ID) == nil {
			return r.w.TickCount()
		}
	}
	r.t.Fatal("the NPC was never killed")
	return 0
}

// statsUntil reads c's messages up to the Snapshot of tick and returns the
// gold of every PlayerStats sent with that tick's Snapshot.
func (r *killRig) statsUntil(c *client, tick uint64) []int64 {
	r.t.Helper()
	var golds []int64
	for {
		typ, tbl := r.next(c)
		switch typ {
		case protocol.MessagePlayerStats:
			var st protocol.PlayerStats
			st.Init(tbl.Bytes, tbl.Pos)
			golds = append(golds, int64(st.Gold()))
		case protocol.MessageSnapshot:
			var s protocol.Snapshot
			s.Init(tbl.Bytes, tbl.Pos)
			if uint64(s.Tick()) >= tick {
				if uint64(s.Tick()) != tick {
					r.t.Fatalf("Snapshot tick %d, want the kill tick %d", s.Tick(), tick)
				}
				return golds
			}
			golds = golds[:0] // an earlier tick's messages
		}
	}
}

// AC1: NPCKilled spawns each item stack of the kill as a ground item at the
// kill position, reserved for the killer, on the tick of the kill.
func TestNPCKillSpawnsReservedGroundItems(t *testing.T) {
	r := newKillRig(t)
	pelt, ok := r.data.ItemByKey("coyote_pelt")
	if !ok {
		t.Fatal("no coyote_pelt")
	}
	if n := r.ground.Len(); n != 0 {
		t.Fatalf("%d ground items before the kill", n)
	}
	r.tickUntilKill()
	// Checked before any further tick.
	items := r.ground.Near(nil, r.killPos, 0)
	if len(items) != 1 || r.ground.Len() != 1 {
		t.Fatalf("ground items at the kill spot = %+v (%d in all), want one stack", items, r.ground.Len())
	}
	it := items[0]
	if it.Item != pelt.ID || it.Qty != 3 || it.Owner != r.a || it.Pos != r.killPos || !it.Spawned.Equal(r.clock.Now()) {
		t.Fatalf("ground item = %+v, want 3 × coyote_pelt (%d) at %v owned by the killer %d, spawned now", it, pelt.ID, r.killPos, r.a)
	}
}

// AC1: without a ground item system NPCKilled logs an error and doesn't
// panic.
func TestNPCKillWithoutGroundItemsLogs(t *testing.T) {
	d := testData(t)
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	w := world.New(world.DefaultConfig())
	sess := session.New(session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d},
		w, world.NewFakeClock(time.Unix(0, 0)), store.NewMemory(), log)
	t.Cleanup(sess.Close)
	pelt, _ := d.ItemByKey("coyote_pelt")
	sess.NPCKilled(pvecombat.Kill{NPC: 300, Type: coyoteAlpha, Killer: 1, Items: []world.Stack{{Item: pelt.ID, Qty: 3}}})
	if out := buf.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "no ground item system") {
		t.Fatalf("log = %q, want an error about the missing ground item system", out)
	}
}

// AC2: the killer's npc_drop outbox event is written through the economy
// writer as one audited transaction with the reason npc_drop, and the store
// then holds the new gold balance.
func TestNPCKillGoldSavedAsAuditedTransaction(t *testing.T) {
	r := newKillRig(t)
	pa := r.w.Player(r.a)
	before := pa.Inv.Gold
	r.tickUntilKill()
	if pa.Inv.Gold != before+100 {
		t.Fatalf("gold after the kill = %d, want %d", pa.Inv.Gold, before+100)
	}
	waitFor(t, "npc_drop saved", func() bool {
		return len(r.store.callsWithReason(t, pvecombat.ReasonNPCDrop)) > 0
	})
	calls := r.store.callsWithReason(t, pvecombat.ReasonNPCDrop)
	if len(calls) != 1 {
		t.Fatalf("%d transactions with npc_drop entries, want 1", len(calls))
	}
	if len(calls[0]) == 0 {
		t.Fatal("npc_drop transaction has no audit entries")
	}
	for _, e := range calls[0] {
		if got := entryReason(t, e); got != "npc_drop" {
			t.Errorf("entry %+v has reason %q, want npc_drop", e, got)
		}
	}
	sp, err := r.store.LoadByTokenHash(context.Background(), hashTok(r.tokA))
	if err != nil {
		t.Fatal(err)
	}
	if sp.Holdings.Gold != int64(pa.Inv.Gold) {
		t.Fatalf("stored gold = %d, want %d", sp.Holdings.Gold, pa.Inv.Gold)
	}
}

// AC3: on the tick of the kill the killer gets a PlayerStats with the new
// gold balance, and the other player gets none.
func TestNPCKillSendsKillerPlayerStats(t *testing.T) {
	r := newKillRig(t)
	pa := r.w.Player(r.a)
	tick := r.tickUntilKill()
	want := int64(pa.Inv.Gold)
	if got := r.statsUntil(r.ca, tick); len(got) != 1 || got[0] != want {
		t.Fatalf("killer's PlayerStats gold on the kill tick = %v, want [%d]", got, want)
	}
	if got := r.statsUntil(r.cb, tick); len(got) != 0 {
		t.Fatalf("other player got PlayerStats %v on the kill tick, want none", got)
	}
}
