package session

import (
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// tradeRig is an econRig with the trade system registered before the
// session, and new players spawning at the campus spawn (outside the PvP
// zone, next to each other).
type tradeRig struct {
	*econRig
	trades *trade.System
}

func newTradeRig(t *testing.T) *tradeRig {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	l, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	st := &econStore{Memory: store.NewMemory()}
	logs := &logBuf{}
	w := world.New(world.DefaultConfig())
	clock := world.NewFakeClock(time.Unix(0, 0))
	s := New(Config{Data: d, Spawn: l.Spawn()}, w, clock, st, slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(s.Close)
	tr := trade.New(d, l, 1, s, s)
	s.SetTrades(tr)
	for _, sys := range []world.System{tr, s} {
		if err := w.Register(sys); err != nil {
			t.Fatal(err)
		}
	}
	return &tradeRig{econRig: &econRig{clock: clock, t: t, d: d, w: w, s: s, st: st, logs: logs, hashes: map[*guest]string{}}, trades: tr}
}

func (r *tradeRig) run(cmds ...world.Command) {
	for _, c := range cmds {
		r.w.Enqueue(c)
	}
	r.w.Tick()
}

// agree has a give b gold and accepts both screens on both sides; the next
// Tick commits the trade.
func (r *tradeRig) agree(a, b world.PlayerID, gold int) {
	r.run(trade.RequestCommand{Player: a, Partner: b})
	r.run(trade.RequestCommand{Player: b, Partner: a})
	if !r.trades.Trading(a) {
		r.t.Fatal("trade did not open")
	}
	r.run(trade.SetGoldCommand{Player: a, Gold: gold})
	r.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer}, trade.AcceptCommand{Player: b, Screen: trade.ScreenOffer})
	r.w.Enqueue(trade.AcceptCommand{Player: a, Screen: trade.ScreenConfirm})
	r.w.Enqueue(trade.AcceptCommand{Player: b, Screen: trade.ScreenConfirm})
}

// goldWrites lists, for store id sid, the gold of every committed holdings
// write naming it, in commit order.
func (r *tradeRig) goldWrites(sid int64) []int64 {
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	var out []int64
	for _, c := range r.st.applied {
		if c.PlayerID == sid {
			out = append(out, c.Holdings.Gold)
		}
	}
	return out
}

// AC3: a trade is written after every earlier economy write of both players,
// including one that failed and waits for a retry and one still in the
// outbox when the trade commits, so the stored holdings end post-trade.
func TestTradeWriteAfterEarlierEconomyWrites(t *testing.T) {
	r := newTradeRig(t)
	pa, ga := r.addPlayer("a")
	pb, gb := r.addPlayer("b")
	startA, startB := pa.Inv.Gold, pb.Inv.Gold

	r.st.setFailFor(ga.storeID)
	gain(t, pa, 5)
	r.w.Tick()
	waitUntil(t, "a's write to fail", func() bool { _, f := r.st.counts(); return f == 1 })

	r.agree(pa.ID, pb.ID, 3)
	gain(t, pb, 1) // in b's outbox when the trade commits
	r.w.Tick()     // commits; the submit retries a's write, which fails again
	if !r.trades.Committing(pa.ID) || !r.trades.Committing(pb.ID) {
		t.Fatal("trade not committing")
	}
	waitUntil(t, "the retry to fail", func() bool { _, f := r.st.counts(); return f == 2 })
	for i := 0; i < 5; i++ {
		r.w.Tick()
	}
	if n, f := r.st.counts(); n != 1 || f != 2 {
		// Only b's pick-up may commit: the trade waits behind a's write.
		t.Fatalf("%d writes committed and %d failed, want 1 and 2", n, f)
	}
	if !r.trades.Committing(pa.ID) {
		t.Fatal("trade resolved while a's earlier write waits for a retry")
	}

	r.st.setFailFor(0)
	r.clock.Advance(DefaultSaveInterval) // the periodic save retries failures
	r.w.Tick()
	r.w.Tick()
	waitUntil(t, "the trade to commit", func() bool { n, _ := r.st.counts(); return n == 4 })
	for i := 0; i < 100 && r.trades.Committing(pa.ID); i++ {
		time.Sleep(time.Millisecond)
		r.w.Tick()
	}
	if r.trades.Committing(pa.ID) {
		t.Fatal("trade result never applied")
	}

	if got, want := r.goldWrites(ga.storeID), []int64{int64(startA + 5), int64(startA + 2)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a's writes gold %v, want %v", got, want)
	}
	if got, want := r.goldWrites(gb.storeID), []int64{int64(startB + 1), int64(startB + 4)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("b's writes gold %v, want %v", got, want)
	}
	if err := r.saveAll(); err != nil {
		t.Fatal(err)
	}
	if got := r.stored(ga).Gold; got != int64(startA+2) || pa.Inv.Gold != startA+2 {
		t.Fatalf("a: stored gold %d, world gold %d, want %d", got, pa.Inv.Gold, startA+2)
	}
	if got := r.stored(gb).Gold; got != int64(startB+4) || pb.Inv.Gold != startB+4 {
		t.Fatalf("b: stored gold %d, world gold %d, want %d", got, pb.Inv.Gold, startB+4)
	}
	if n, _ := r.st.counts(); n != 4 {
		t.Fatalf("%d holdings writes, want 4 (the completed trade is not saved twice)", n)
	}
}

// AC5: a player whose connection closes while their trade commit is in
// flight stays in the world until the result is applied, and SaveAll waits
// for the in-flight trade write.
func TestTradeCommitOutlivesDisconnect(t *testing.T) {
	r := newTradeRig(t)
	r.st.gate = make(chan struct{})
	pa, ga := r.addPlayer("a")
	pb, gb := r.addPlayer("b")
	startA, startB := pa.Inv.Gold, pb.Inv.Gold

	r.agree(pa.ID, pb.ID, 10)
	r.w.Tick() // commits; the write blocks on the gate
	if !r.trades.Committing(pa.ID) {
		t.Fatal("trade not committing")
	}
	r.s.mu.Lock()
	r.s.ops = append(r.s.ops, op{g: ga}) // a's connection closed
	r.s.mu.Unlock()
	r.w.Tick()
	r.clock.Advance(2 * r.d.Rules.LogoutCombatLock())
	for i := 0; i < 5; i++ {
		r.w.Tick()
	}
	if r.w.Player(pa.ID) != pa {
		t.Fatal("player removed while their trade commit was in flight")
	}

	saved := make(chan error, 1)
	go func() { saved <- r.saveAll() }()
	select {
	case err := <-saved:
		t.Fatalf("SaveAll returned (%v) before the trade write committed", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(r.st.gate)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if a, b := r.stored(ga).Gold, r.stored(gb).Gold; a != int64(startA-10) || b != int64(startB+10) {
		t.Fatalf("stored gold a %d b %d, want %d and %d", a, b, startA-10, startB+10)
	}

	r.w.Tick() // applies the result, then the session removes a
	if r.w.Player(pa.ID) != nil {
		t.Fatal("player still in the world after the trade result was applied")
	}
	r.s.mu.Lock()
	gold := ga.inv.Gold
	r.s.mu.Unlock()
	if gold != startA-10 {
		t.Fatalf("a left with gold %d, want the post-trade %d", gold, startA-10)
	}
	if pb.Inv.Gold != startB+10 || pb.Inv.Locked() {
		t.Fatalf("b: gold %d locked %v, want %d unlocked", pb.Inv.Gold, pb.Inv.Locked(), startB+10)
	}
}
