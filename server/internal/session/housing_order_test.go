package session

import (
	"context"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

// houseStore is a cardStore that also logs every committed plot claim.
type houseStore struct{ *cardStore }

func (h houseStore) ClaimPlot(ctx context.Context, c store.ClaimPlot) (store.Plot, error) {
	p, err := h.Memory.ClaimPlot(ctx, c)
	if err == nil {
		h.note("claim")
	}
	return p, err
}

// AC1 (#587): a claim is saved after every earlier economy write of the
// claimer, including one that failed and waits for a retry and one still in
// the outbox, so the gold those writes add pays for the claim in the store.
func TestHouseClaimAfterEarlierWrites(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	hs := houseStore{newCardStore()}
	logs := &logBuf{}
	w := world.New(world.DefaultConfig())
	clock := world.NewFakeClock(time.Unix(0, 0))
	s := New(Config{Data: d}, w, clock, hs, slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(s.Close)
	r := &cardRig{econRig: &econRig{clock: clock, t: t, d: d, w: w, s: s, st: hs.econStore, logs: logs, hashes: map[*guest]string{}}, cs: hs.cardStore}
	p, g := r.addPlayer("a")

	areas := []store.Area{{ID: 1, Name: "Row", X: p.Pos.X, Z: p.Pos.Y}}
	if err := hs.UpsertPlots(context.Background(), areas, []store.Plot{{ID: 1, AreaID: 1, X: p.Pos.X, Z: p.Pos.Y}}); err != nil {
		t.Fatal(err)
	}
	stored, err := hs.LoadPlots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	homes, err := housing.NewSystem(d, housing.DefaultConfig(), []housing.Plot{{ID: 1, Door: p.Pos}}, clock, s, s)
	if err != nil {
		t.Fatal(err)
	}
	s.SetHousing(homes, hs, stored)
	for _, sys := range []world.System{homes, s} {
		if err := w.Register(sys); err != nil {
			t.Fatal(err)
		}
	}

	cost := d.Rules.HouseClaimMonths * d.Rules.HouseRentPerMonth
	start := p.Inv.Gold
	if start >= cost {
		t.Fatalf("starting gold %d already pays the claim (%d)", start, cost)
	}
	r.st.setFailFor(g.storeID)
	gain(t, p, cost/2)
	r.w.Tick()
	r.waitSaverFailed("the pick-up to fail", g.storeID)

	gain(t, p, cost-cost/2) // in the outbox when the claim is committed
	r.run(housing.ClaimCommand{Player: p.ID, House: 1})
	if !p.Inv.Locked() {
		t.Fatal("claim not committing")
	}
	for i := 0; i < 5; i++ {
		r.w.Tick()
	}
	if got := r.cs.writes(); len(got) != 0 || !p.Inv.Locked() {
		t.Fatalf("writes %v while the earlier write waits for a retry", got)
	}

	r.st.setFailFor(0)
	r.clock.Advance(DefaultSaveInterval) // the periodic save retries failures
	r.tickUntil("the claim", func() bool { return !p.Inv.Locked() })
	if got, want := r.cs.writes(), []string{"apply", "apply", "claim"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("writes %v, want both pick-ups before the claim", got)
	}
	if p.Inv.Gold != start || r.stored(g).Gold != int64(start) {
		t.Fatalf("gold: world %d, stored %d, want %d", p.Inv.Gold, r.stored(g).Gold, start)
	}
	if l := homes.List(); l[0].Owner != p.ID {
		t.Fatalf("house owner %d, want %d", l[0].Owner, p.ID)
	}
}
