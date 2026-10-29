package session

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/cardroom"
	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/systems/cardtable/shed"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

const (
	cardTable = cardroom.TableID(3)
	cardStake = 50
)

// cardStore is an econStore whose card pot writes can fail, and which logs
// every committed economy and card pot write in commit order.
type cardStore struct {
	*econStore

	cmu      sync.Mutex
	failOpen bool
	failPay  int // the next failPay payouts fail
	log      []string
}

func (c *cardStore) note(s string) {
	c.cmu.Lock()
	c.log = append(c.log, s)
	c.cmu.Unlock()
}

func (c *cardStore) ApplyEconomy(ctx context.Context, changes []store.EconomyChange, entries []store.Entry) error {
	if err := c.econStore.ApplyEconomy(ctx, changes, entries); err != nil {
		return err
	}
	c.note("apply")
	return nil
}

func (c *cardStore) OpenCardPot(ctx context.Context, table int32, stakes []store.CardStake, changes []store.EconomyChange, entries []store.Entry) (int64, error) {
	c.cmu.Lock()
	fail := c.failOpen
	c.cmu.Unlock()
	if fail {
		return 0, errors.New("db down")
	}
	id, err := c.Memory.OpenCardPot(ctx, table, stakes, changes, entries)
	if err == nil {
		c.note("open")
	}
	return id, err
}

func (c *cardStore) PayCardPot(ctx context.Context, pot int64, changes []store.EconomyChange, entries []store.Entry) error {
	c.cmu.Lock()
	fail := c.failPay > 0
	if fail {
		c.failPay--
	}
	c.cmu.Unlock()
	if fail {
		return errors.New("db down")
	}
	err := c.Memory.PayCardPot(ctx, pot, changes, entries)
	if err == nil {
		c.note("pay")
	}
	return err
}

func (c *cardStore) writes() []string {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return append([]string(nil), c.log...)
}

// nopCardSink ignores every card room result; the card room's messages are
// not wired to clients yet.
type nopCardSink struct{}

func (nopCardSink) SitResult(world.PlayerID, cardroom.TableID, cardtable.Seat, cardroom.Result) {}
func (nopCardSink) LeaveResult(world.PlayerID, cardroom.TableID, cardroom.Result)               {}
func (nopCardSink) TableChanged(cardroom.TableID, cardroom.Snapshot, []cardroom.PlayerView)     {}
func (nopCardSink) ReadyResult(world.PlayerID, cardroom.TableID, cardroom.Result)               {}
func (nopCardSink) MoveResult(world.PlayerID, cardroom.TableID, cardroom.Result)                {}
func (nopCardSink) StakeResult(world.PlayerID, cardroom.TableID, cardroom.Result)               {}

// cardRig is an econRig over a cardStore with a card room, registered before
// the session, hosting one 2-seat Shed table at the spawn with stake
// cardStake.
type cardRig struct {
	*econRig
	cs    *cardStore
	cards *cardroom.System
}

func newCardRig(t *testing.T, cs *cardStore) *cardRig {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuf{}
	w := world.New(world.DefaultConfig())
	clock := world.NewFakeClock(time.Unix(0, 0))
	s := New(Config{Data: d}, w, clock, cs, slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(s.Close)
	tb, err := cardtable.New(2, 1000, shed.New())
	if err != nil {
		t.Fatal(err)
	}
	cards, err := cardroom.New([]cardroom.Entry{{ID: cardTable, Table: tb, Stake: cardStake}}, nopCardSink{}, s)
	if err != nil {
		t.Fatal(err)
	}
	s.SetCardRoom(cards)
	for _, sys := range []world.System{cards, s} {
		if err := w.Register(sys); err != nil {
			t.Fatal(err)
		}
	}
	return &cardRig{econRig: &econRig{clock: clock, t: t, d: d, w: w, s: s, st: cs.econStore, logs: logs, hashes: map[*guest]string{}},
		cs: cs, cards: cards}
}

func newCardStore() *cardStore { return &cardStore{econStore: &econStore{Memory: store.NewMemory()}} }

func (r *cardRig) run(cmds ...world.Command) {
	for _, c := range cmds {
		r.w.Enqueue(c)
	}
	r.w.Tick()
}

// tickUntil ticks until cond holds.
func (r *cardRig) tickUntil(what string, cond func() bool) {
	r.t.Helper()
	for i := 0; i < 1000 && !cond(); i++ {
		time.Sleep(time.Millisecond)
		r.w.Tick()
	}
	if !cond() {
		r.t.Fatalf("timed out waiting for %s", what)
	}
}

// sitReady seats a and b at the table and readies them; the Tick after the
// Ready commands commits the stakes.
func (r *cardRig) sitReady(a, b world.PlayerID) {
	r.run(cardroom.SitCommand{Player: a, TableID: cardTable}, cardroom.SitCommand{Player: b, TableID: cardTable})
	r.w.Enqueue(cardroom.ReadyCommand{Player: a, Ready: true})
	r.w.Enqueue(cardroom.ReadyCommand{Player: b, Ready: true})
}

// cardAudit returns the audit entries with a card room reason, as
// "reason actor gold table pot".
func cardAudit(m *store.Memory) []auditPayload {
	var out []auditPayload
	for _, e := range m.AuditEntries() {
		if p, ok := e.Payload.(auditPayload); ok && isCardRoom(p.Reason) {
			out = append(out, p)
		}
	}
	return out
}

func openPots(t *testing.T, m *store.Memory) []store.CardPot {
	t.Helper()
	pots, err := m.OpenCardPots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return pots
}

// AC3: the session saves a game's stakes after every earlier economy write
// of each player, including one that failed and waits for a retry and one
// still in the outbox, as one pot transaction; the game starts only once it
// committed; the payout to the winner closes the pot; and neither is written
// a second time from the outbox.
func TestCardRoomStakeAndPayoutAfterEarlierWrites(t *testing.T) {
	r := newCardRig(t, newCardStore())
	pa, ga := r.addPlayer("a")
	pb, gb := r.addPlayer("b")
	gain(t, pa, 200)
	gain(t, pb, 200)
	r.w.Tick()
	waitUntil(t, "the pick-ups to commit", func() bool { n, _ := r.st.counts(); return n == 2 })
	startA, startB := pa.Inv.Gold, pb.Inv.Gold

	r.st.setFailFor(ga.storeID)
	gain(t, pa, 5)
	r.w.Tick()
	r.waitSaverFailed("a's write to fail", ga.storeID)

	r.sitReady(pa.ID, pb.ID)
	gain(t, pb, 1) // in b's outbox when the stakes are committed
	r.w.Tick()     // Ready, then the stake request
	if !r.cards.Staking(cardTable) {
		t.Fatal("stakes not committing")
	}
	r.waitSaverFailed("the retry to fail", ga.storeID)
	for i := 0; i < 5; i++ {
		r.w.Tick()
	}
	if !r.cards.Staking(cardTable) || len(openPots(t, r.cs.Memory)) != 0 {
		t.Fatal("stakes saved while a's earlier write waits for a retry")
	}

	r.st.setFailFor(0)
	r.clock.Advance(DefaultSaveInterval) // the periodic save retries failures
	r.tickUntil("the game to start", func() bool { tb, _ := r.cards.Table(cardTable); return tb.InProgress() })
	if got, want := r.cs.writes(), []string{"apply", "apply", "apply", "apply", "open"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("writes %v, want both pick-ups, a's retried write and b's pick-up before the stakes", got)
	}
	wantA, wantB := startA+5-cardStake, startB+1-cardStake
	if got := r.stored(ga).Gold; got != int64(wantA) || pa.Inv.Gold != wantA {
		t.Fatalf("a: stored gold %d, world gold %d, want %d", got, pa.Inv.Gold, wantA)
	}
	if got := r.stored(gb).Gold; got != int64(wantB) || pb.Inv.Gold != wantB {
		t.Fatalf("b: stored gold %d, world gold %d, want %d", got, pb.Inv.Gold, wantB)
	}
	pots := openPots(t, r.cs.Memory)
	if len(pots) != 1 || pots[0].Table != int32(cardTable) ||
		!reflect.DeepEqual(pots[0].Stakes, []store.CardStake{{PlayerID: ga.storeID, Gold: cardStake}, {PlayerID: gb.storeID, Gold: cardStake}}) {
		t.Fatalf("open pots %+v, want one at table %d with both stakes", pots, cardTable)
	}
	pot := pots[0].ID
	if got, want := cardAudit(r.cs.Memory), []auditPayload{
		{Reason: cardroom.ReasonStake, Gold: -cardStake, Table: int64(cardTable)},
		{Reason: cardroom.ReasonStake, Gold: -cardStake, Table: int64(cardTable)},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("card audit %+v, want %+v", got, want)
	}

	// b leaves mid-game and forfeits, so a wins the pot. The first payout
	// save fails and is retried.
	r.cs.cmu.Lock()
	r.cs.failPay = 1
	r.cs.cmu.Unlock()
	r.run(cardroom.LeaveCommand{Player: pb.ID})
	r.tickUntil("the payout", func() bool { return !r.cards.Committing(pa.ID) && pa.Inv.Gold == wantA+2*cardStake })
	if got := r.stored(ga).Gold; got != int64(wantA+2*cardStake) {
		t.Fatalf("a: stored gold %d, want %d", got, wantA+2*cardStake)
	}
	if got := r.stored(gb).Gold; got != int64(wantB) {
		t.Fatalf("b: stored gold %d, want %d", got, wantB)
	}
	if pots := openPots(t, r.cs.Memory); len(pots) != 0 {
		t.Fatalf("open pots %+v after the payout", pots)
	}
	audit := cardAudit(r.cs.Memory)
	if want := (auditPayload{Reason: cardroom.ReasonPot, Gold: 2 * cardStake, Table: int64(cardTable), Pot: pot}); len(audit) != 3 || audit[2] != want {
		t.Fatalf("card audit %+v, want the payout %+v last", audit, want)
	}
	if err := r.saveAll(); err != nil {
		t.Fatal(err)
	}
	if got, want := r.cs.writes()[4:], []string{"open", "pay"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("writes after the pick-ups %v, want %v (the stakes and payout are not saved again)", got, want)
	}
}

// AC3: a failed stake save is reported through Resolve: the game doesn't
// start, no gold moves in the world or the store, and no pot is opened.
func TestCardRoomStakeFailureResolved(t *testing.T) {
	cs := newCardStore()
	cs.failOpen = true
	r := newCardRig(t, cs)
	pa, ga := r.addPlayer("a")
	pb, gb := r.addPlayer("b")
	gain(t, pa, 100)
	gain(t, pb, 100)
	r.sitReady(pa.ID, pb.ID)
	r.w.Tick()
	if !r.cards.Staking(cardTable) {
		t.Fatal("stakes not committing")
	}
	r.tickUntil("the failure", func() bool { return !r.cards.Staking(cardTable) })
	if tb, _ := r.cards.Table(cardTable); tb.InProgress() {
		t.Fatal("game started after a failed stake save")
	}
	if err := r.saveAll(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		p *world.Player
		g *guest
	}{{pa, ga}, {pb, gb}} {
		if got := r.stored(c.g).Gold; got != 100+int64(r.d.Rules.StartingGold) || c.p.Inv.Gold != int(got) || c.p.Inv.Locked() {
			t.Errorf("stored gold %d, world gold %d, locked %v; want %d unlocked", got, c.p.Inv.Gold, c.p.Inv.Locked(), 100+r.d.Rules.StartingGold)
		}
	}
	if pots := openPots(t, cs.Memory); len(pots) != 0 {
		t.Fatalf("open pots %+v", pots)
	}
	if a := cardAudit(cs.Memory); len(a) != 0 {
		t.Fatalf("card audit %+v, want none", a)
	}
	if !strings.Contains(r.logs.String(), "db down") {
		t.Error("failure not logged")
	}
}

// AC4: after the server stops with a staked game in progress, the next
// startup returns every stake in the open pot to the players' stored gold,
// with a card_refund audit entry each, and closes the pot, before any player
// is loaded.
func TestCardRoomRefundOpenPotsAtStartup(t *testing.T) {
	cs := newCardStore()
	r := newCardRig(t, cs)
	pa, ga := r.addPlayer("a")
	pb, gb := r.addPlayer("b")
	gain(t, pa, 100)
	gain(t, pb, 100)
	r.sitReady(pa.ID, pb.ID)
	r.tickUntil("the game to start", func() bool { tb, _ := r.cards.Table(cardTable); return tb.InProgress() })
	before := 100 + r.d.Rules.StartingGold
	if pa.Inv.Gold != before-cardStake {
		t.Fatalf("a gold %d, want %d", pa.Inv.Gold, before-cardStake)
	}

	// The server stops mid-game: everything is saved and the game is lost.
	if err := r.saveAll(); err != nil {
		t.Fatal(err)
	}
	r.s.Close()
	pots := openPots(t, cs.Memory)
	if len(pots) != 1 {
		t.Fatalf("open pots %+v after stopping, want the game's pot", pots)
	}
	if got := r.stored(ga).Gold; got != int64(before-cardStake) {
		t.Fatalf("a: stored gold %d before restart, want %d", got, before-cardStake)
	}

	// The next startup.
	r2 := newCardRig(t, cs)
	if err := r2.s.RefundCardPots(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, g := range []*guest{ga, gb} {
		sp, err := cs.LoadByTokenHash(context.Background(), r.hashes[g])
		if err != nil {
			t.Fatal(err)
		}
		if sp.Gold != int64(before) {
			t.Errorf("player %d: stored gold %d after restart, want %d", sp.ID, sp.Gold, before)
		}
		// Joining loads the refunded gold.
		r2.s.mu.Lock()
		g2, err := r2.s.guestLocked(sp, false)
		r2.s.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if g2.inv.Gold != before {
			t.Errorf("player %d joins with %d gold, want %d", sp.ID, g2.inv.Gold, before)
		}
	}
	if pots := openPots(t, cs.Memory); len(pots) != 0 {
		t.Fatalf("open pots %+v after the startup refund", pots)
	}
	audit := cardAudit(cs.Memory)
	want := auditPayload{Reason: cardroom.ReasonRefund, Gold: cardStake, Table: int64(cardTable), Pot: pots[0].ID}
	if len(audit) != 4 || audit[2] != want || audit[3] != want {
		t.Fatalf("card audit %+v, want two stakes then a refund %+v each", audit, want)
	}
	var actors []int64
	for _, e := range cs.AuditEntries() {
		if p, ok := e.Payload.(auditPayload); ok && p.Reason == cardroom.ReasonRefund {
			actors = append(actors, e.Actor)
		}
	}
	if !reflect.DeepEqual(actors, []int64{ga.storeID, gb.storeID}) {
		t.Fatalf("refund actors %v, want %d and %d", actors, ga.storeID, gb.storeID)
	}
	if !strings.Contains(r2.logs.String(), "refunded open card pot") {
		t.Error("refund not logged")
	}

	// A second startup has nothing to refund.
	if err := r2.s.RefundCardPots(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(cardAudit(cs.Memory)); n != 4 {
		t.Fatalf("%d card audit entries after a second startup, want 4", n)
	}
}
