package session

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/world"
)

// econStore records ApplyEconomy calls that committed, and can block or fail them.
type econStore struct {
	*store.Memory
	gate chan struct{} // if set, ApplyEconomy waits for it to close

	mu       sync.Mutex
	fail     bool
	failures int
	applied  []store.EconomyChange // one per committed call, in commit order
}

func (e *econStore) ApplyEconomy(ctx context.Context, changes []store.EconomyChange, entries []store.Entry) error {
	if e.gate != nil {
		select {
		case <-e.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fail {
		e.failures++
		return errors.New("db down")
	}
	if err := e.Memory.ApplyEconomy(ctx, changes, entries); err != nil {
		return err
	}
	e.applied = append(e.applied, changes...)
	return nil
}

func (e *econStore) setFail(v bool) {
	e.mu.Lock()
	e.fail = v
	e.mu.Unlock()
}

func (e *econStore) counts() (applied, failures int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.applied), e.failures
}

// appliedGold lists the gold of every committed holdings write, in order.
func (e *econStore) appliedGold() []int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]int64, len(e.applied))
	for i, c := range e.applied {
		out[i] = c.Holdings.Gold
	}
	return out
}

type logBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type econRig struct {
	clock *world.FakeClock
	t     *testing.T
	d     *gamedata.Data
	w     *world.World
	s     *Server
	st    *econStore
	logs  *logBuf

	hashes map[*guest]string
}

func newEconRig(t *testing.T) *econRig {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	st := &econStore{Memory: store.NewMemory()}
	logs := &logBuf{}
	w := world.New(world.DefaultConfig())
	clock := world.NewFakeClock(time.Unix(0, 0))
	s := New(Config{Data: d}, w, clock, st, slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(s.Close)
	for _, sys := range []world.System{items.New(d), s} {
		if err := w.Register(sys); err != nil {
			t.Fatal(err)
		}
	}
	return &econRig{clock: clock, t: t, d: d, w: w, s: s, st: st, logs: logs, hashes: map[*guest]string{}}
}

// addPlayer creates a stored guest with the starting kit and puts it in the
// world as a live player, as a join would.
func (r *econRig) addPlayer(tokenHash string) (*world.Player, *guest) {
	r.t.Helper()
	inv, changes, err := world.NewStartingInventory(r.d)
	if err != nil {
		r.t.Fatal(err)
	}
	kit, err := toHoldings(inv)
	if err != nil {
		r.t.Fatal(err)
	}
	sp, err := r.st.CreateGuest(context.Background(), tokenHash, kit, auditEntries(changes, "starting_kit"))
	if err != nil {
		r.t.Fatal(err)
	}
	r.s.mu.Lock()
	g, err := r.s.guestLocked(sp, true)
	r.s.mu.Unlock()
	if err != nil {
		r.t.Fatal(err)
	}
	p := &world.Player{ID: g.id, Pos: g.pos, Inv: cloneInventory(g.inv)}
	p.HP = 1 // so eating heals
	if err := r.w.AddPlayer(p); err != nil {
		r.t.Fatal(err)
	}
	r.s.live[g.id] = g
	r.hashes[g] = tokenHash
	return p, g
}

func (r *econRig) stored(g *guest) store.Holdings {
	r.t.Helper()
	sp, err := r.st.LoadByTokenHash(context.Background(), r.hashes[g])
	if err != nil {
		r.t.Fatal(err)
	}
	return sp.Holdings
}

// gain records a gold pick-up of n on the tick.
func gain(t *testing.T, p *world.Player, n int) {
	t.Helper()
	ch, err := p.Inv.AddGold(n)
	if err != nil {
		t.Fatal(err)
	}
	p.RecordEconomy("pickup", ch)
}

func (r *econRig) saveAll() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.s.SaveAll(ctx)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// AC1: an equip, an eat and a gold change are drained on the tick and end up
// in the stored holdings, each event one ApplyEconomy with an audit entry per
// change tagged with its reason.
func TestEconomyEventsReachStoreWithAudit(t *testing.T) {
	r := newEconRig(t)
	p, g := r.addPlayer("a")
	kitAudit := len(r.st.AuditEntries())

	// The same actions on a copy give the expected holdings and audit trail.
	want := cloneInventory(p.Inv)
	var wantAudit []store.Entry
	var wantHoldings []store.Holdings
	step := func(reason string, ch []world.Change, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", reason, err)
		}
		for _, e := range auditEntries(ch, reason) {
			e.Actor = g.storeID
			wantAudit = append(wantAudit, e)
		}
		h, err := toHoldings(want)
		if err != nil {
			t.Fatal(err)
		}
		wantHoldings = append(wantHoldings, h)
	}
	ch, err := want.Unequip(gamedata.SlotWeapon)
	step(items.ReasonUnequip, ch, err)
	stick := -1
	for i, s := range want.Slots {
		if s.Item == p.Inv.Equipment.Weapon {
			stick = i
		}
	}
	if stick < 0 {
		t.Fatal("unequipped weapon not in inventory")
	}
	ch, err = want.Equip(r.d, stick, 1)
	step(items.ReasonEquip, ch, err)
	boba, ok := r.d.ItemByKey("boba")
	if !ok {
		t.Fatal("no boba")
	}
	food := -1
	for i, s := range want.Slots {
		if s.Item == boba.ID {
			food = i
		}
	}
	ch, err = want.RemoveAt(food)
	step(items.ReasonEat, ch, err)
	ch, err = want.AddGold(25)
	step("pickup", ch, err)

	r.w.Enqueue(items.UnequipCommand{Player: p.ID, Slot: gamedata.SlotWeapon})
	r.w.Enqueue(items.EquipCommand{Player: p.ID, Slot: stick})
	r.w.Enqueue(items.EatCommand{Player: p.ID, Slot: food})
	r.w.Tick()
	gain(t, p, 25)
	r.w.Tick()
	if n := len(p.PendingEconomy()); n != 0 {
		t.Fatalf("%d events left in the outbox after the tick", n)
	}
	waitUntil(t, "four economy writes", func() bool { n, _ := r.st.counts(); return n == 4 })

	r.st.mu.Lock()
	applied := append([]store.EconomyChange(nil), r.st.applied...)
	r.st.mu.Unlock()
	for i, c := range applied {
		if c.PlayerID != g.storeID || c.Holdings != wantHoldings[i] {
			t.Fatalf("write %d = %+v, want player %d holdings %+v", i, c, g.storeID, wantHoldings[i])
		}
	}
	if got := r.stored(g); got != wantHoldings[3] {
		t.Fatalf("stored holdings %+v, want %+v", got, wantHoldings[3])
	}
	if got := r.st.AuditEntries()[kitAudit:]; !reflect.DeepEqual(got, wantAudit) {
		t.Fatalf("audit entries\n got %+v\nwant %+v", got, wantAudit)
	}
}

// AC2: one player's writes commit in event order, and a blocked store never
// blocks the tick.
func TestEconomyWritesInOrderAndOffTick(t *testing.T) {
	r := newEconRig(t)
	r.st.gate = make(chan struct{})
	p, g := r.addPlayer("a")
	start := p.Inv.Gold

	const n = 20
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < n; i++ {
			gain(t, p, 1)
			if i%3 == 0 {
				gain(t, p, 1) // sometimes two events in one tick
			}
			r.w.Tick()
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ticks stalled while the store blocked")
	}
	if got, _ := r.st.counts(); got != 0 {
		t.Fatalf("%d writes committed through a closed gate", got)
	}
	close(r.st.gate)
	if err := r.saveAll(); err != nil {
		t.Fatal(err)
	}
	gold := r.st.appliedGold()
	if len(gold) != n+(n+2)/3 {
		t.Fatalf("%d writes, want %d", len(gold), n+(n+2)/3)
	}
	for i, v := range gold {
		if v != int64(start+i+1) {
			t.Fatalf("writes out of order: gold %v", gold)
		}
	}
	if got := r.stored(g).Gold; got != int64(p.Inv.Gold) {
		t.Fatalf("stored gold %d, want %d", got, p.Inv.Gold)
	}
}

// AC3: a failed write is logged and retried, and later events for that player
// wait behind it instead of being dropped or reordered.
func TestFailedEconomyWriteRetriedInOrder(t *testing.T) {
	r := newEconRig(t)
	r.st.setFail(true)
	p, g := r.addPlayer("a")
	q, _ := r.addPlayer("b") // another player's writes are not held back
	start, qStart := p.Inv.Gold, q.Inv.Gold

	gain(t, p, 1)
	r.w.Tick()
	waitUntil(t, "a failed write", func() bool { _, f := r.st.counts(); return f > 0 })
	waitUntil(t, "the failure logged", func() bool {
		return strings.Contains(r.logs.String(), "apply economy") && strings.Contains(r.logs.String(), "db down")
	})
	gain(t, p, 1)
	r.w.Tick() // retries the failure, which fails again
	waitUntil(t, "a second failure", func() bool { _, f := r.st.counts(); return f > 1 })
	if err := r.saveAll(); err == nil {
		t.Fatal("SaveAll reported success with failing economy writes")
	}

	r.st.setFail(false)
	gain(t, p, 1)
	gain(t, q, 5)
	r.w.Tick() // the next submit retries
	waitUntil(t, "every write committed", func() bool { n, _ := r.st.counts(); return n == 4 })
	var mine []int64
	r.st.mu.Lock()
	for _, c := range r.st.applied {
		if c.PlayerID == g.storeID {
			mine = append(mine, c.Holdings.Gold)
		} else if c.Holdings.Gold != int64(qStart+5) {
			t.Errorf("other player's write gold %d, want %d", c.Holdings.Gold, qStart+5)
		}
	}
	r.st.mu.Unlock()
	if want := []int64{int64(start + 1), int64(start + 2), int64(start + 3)}; !reflect.DeepEqual(mine, want) {
		t.Fatalf("player writes gold %v, want %v", mine, want)
	}
	if err := r.saveAll(); err != nil {
		t.Fatal(err)
	}
	if got := r.stored(g).Gold; got != int64(start+3) {
		t.Fatalf("stored gold %d, want %d", got, start+3)
	}
}

// AC4: a leaving player's pending events are drained and written, and SaveAll
// returns only once every economy write has committed.
func TestLeaveDrainsOutboxAndSaveAllWaits(t *testing.T) {
	r := newEconRig(t)
	r.st.gate = make(chan struct{})
	p, g := r.addPlayer("a")
	stay, sg := r.addPlayer("b")
	start := p.Inv.Gold

	r.s.mu.Lock()
	r.s.ops = append(r.s.ops, op{g: g}) // leave, as a closed connection queues it
	r.s.mu.Unlock()
	r.w.Tick() // disconnected; stays in the world for the logout lock
	if r.w.Player(p.ID) == nil {
		t.Fatal("player left before the logout lock ended")
	}
	r.clock.Advance(r.d.Rules.LogoutCombatLock())
	gain(t, p, 7) // pending when the player leaves the world
	r.w.Tick()
	if r.w.Player(p.ID) != nil {
		t.Fatal("player still in the world")
	}
	gain(t, stay, 3) // pending at shutdown, never ticked

	saved := make(chan error, 1)
	go func() { saved <- r.saveAll() }()
	select {
	case err := <-saved:
		t.Fatalf("SaveAll returned (%v) before the economy writes committed", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(r.st.gate)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if got := r.stored(g).Gold; got != int64(start+7) {
		t.Fatalf("left player's stored gold %d, want %d", got, start+7)
	}
	if got := r.stored(sg).Gold; got != int64(stay.Inv.Gold) {
		t.Fatalf("staying player's stored gold %d, want %d", got, stay.Inv.Gold)
	}
}
