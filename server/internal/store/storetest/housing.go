package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
)

// HousingStore is a store with housing.
type HousingStore interface {
	store.Players
	store.Housing
}

// RunHousing runs the Housing contract against fresh stores from factory,
// which also returns a function listing the store's audit entries in append
// order (Kind, Actor and Counterparty only).
func RunHousing(t *testing.T, factory func(t *testing.T) (HousingStore, func() []store.Entry)) {
	ctx := context.Background()
	areas := []store.Area{{ID: 1, Name: "Mesa Court", X: 10, Z: 20}, {ID: 2, Name: "Middle Earth", X: -5, Z: 7}}
	plots := []store.Plot{
		{ID: 1, AreaID: 1, X: 11, Z: 21},
		{ID: 2, AreaID: 1, X: 12, Z: 22},
		{ID: 3, AreaID: 2, X: -4, Z: 8},
	}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	month := 30 * 24 * time.Hour

	newRig := func(t *testing.T, gold ...int64) (HousingStore, func() []store.Entry, []store.Player) {
		t.Helper()
		s, audit := factory(t)
		if err := s.UpsertPlots(ctx, areas, plots); err != nil {
			t.Fatal(err)
		}
		var ps []store.Player
		for i, g := range gold {
			p, err := s.CreateGuest(ctx, string(rune('a'+i)), store.Holdings{Gold: g}, nil)
			if err != nil {
				t.Fatal(err)
			}
			ps = append(ps, p)
		}
		return s, audit, ps
	}
	gold := func(t *testing.T, s HousingStore, p store.Player) int64 {
		t.Helper()
		l, err := s.LoadByTokenHash(ctx, p.TokenHash)
		if err != nil {
			t.Fatal(err)
		}
		return l.Gold
	}
	plotByID := func(t *testing.T, s HousingStore, id int32) store.Plot {
		t.Helper()
		all, err := s.LoadPlots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range all {
			if p.ID == id {
				return p
			}
		}
		t.Fatalf("plot %d not loaded", id)
		return store.Plot{}
	}
	claim := func(p store.Player, plot int32, cost int64) store.ClaimPlot {
		return store.ClaimPlot{PlayerID: p.ID, PlotID: plot, Cost: cost, Rent: 2 * month, Now: now}
	}

	t.Run("UpsertPlots seeds and is idempotent", func(t *testing.T) {
		s, _, ps := newRig(t, 500)
		got, err := s.LoadPlots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(plots) {
			t.Fatalf("loaded %d plots, want %d", len(got), len(plots))
		}
		for i, p := range got {
			if p != plots[i] {
				t.Errorf("plot %d = %+v, want %+v", i, p, plots[i])
			}
		}
		if _, err := s.ClaimPlot(ctx, claim(ps[0], 2, 400)); err != nil {
			t.Fatal(err)
		}
		// Seeding again keeps the owner and updates the position.
		moved := append([]store.Plot(nil), plots...)
		moved[1].X = 99
		for i := 0; i < 2; i++ {
			if err := s.UpsertPlots(ctx, areas, moved); err != nil {
				t.Fatal(err)
			}
		}
		got, err = s.LoadPlots(ctx)
		if err != nil || len(got) != 3 {
			t.Fatalf("after reseed: %d plots, err %v", len(got), err)
		}
		p := got[1]
		if p.OwnerID != ps[0].ID || p.X != 99 || !p.ClaimedAt.Equal(now) || !p.RentPaidUntil.Equal(now.Add(2*month)) {
			t.Errorf("reseeded claimed plot = %+v", p)
		}
		bad := []store.Plot{{ID: 9, AreaID: 77}}
		if err := s.UpsertPlots(ctx, nil, bad); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("unknown area: err %v, want ErrInvalid", err)
		}
		if got, _ := s.LoadPlots(ctx); len(got) != 3 {
			t.Errorf("failed seed left %d plots", len(got))
		}
	})

	t.Run("ClaimPlot", func(t *testing.T) {
		s, audit, ps := newRig(t, 500)
		p, err := s.ClaimPlot(ctx, claim(ps[0], 1, 400))
		if err != nil {
			t.Fatal(err)
		}
		want := now.Add(2 * month)
		if p.OwnerID != ps[0].ID || !p.ClaimedAt.Equal(now) || !p.RentPaidUntil.Equal(want) {
			t.Errorf("claimed plot = %+v", p)
		}
		if stored := plotByID(t, s, 1); stored.OwnerID != ps[0].ID || !stored.RentPaidUntil.Equal(want) {
			t.Errorf("stored plot = %+v", stored)
		}
		if g := gold(t, s, ps[0]); g != 100 {
			t.Errorf("gold = %d, want 100", g)
		}
		h, err := s.LoadHome(ctx, ps[0].ID)
		if err != nil || h.PlotID != 1 || h.OwnerID != ps[0].ID || h.GridSize != store.DefaultGridSize {
			t.Errorf("home = %+v, err %v", h, err)
		}
		es := audit()
		if len(es) != 1 || es[0].Kind != store.HousingClaimKind || es[0].Actor != ps[0].ID {
			t.Errorf("audit = %+v", es)
		}
	})

	t.Run("ClaimPlot failures change nothing", func(t *testing.T) {
		s, audit, ps := newRig(t, 500, 50)
		if _, err := s.ClaimPlot(ctx, claim(ps[0], 1, 400)); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name string
			c    store.ClaimPlot
			want error
		}{
			{"too poor", claim(ps[1], 2, 400), store.ErrInvalid},
			{"taken", claim(ps[1], 1, 10), store.ErrAlreadyExists},
			{"second plot", claim(ps[0], 2, 10), store.ErrAlreadyExists},
			{"unknown plot", claim(ps[1], 99, 10), store.ErrNotFound},
			{"unknown player", store.ClaimPlot{PlayerID: 999, PlotID: 2, Cost: 1, Rent: month, Now: now}, store.ErrNotFound},
			{"negative cost", claim(ps[1], 2, -5), store.ErrInvalid},
			{"no rent", store.ClaimPlot{PlayerID: ps[1].ID, PlotID: 2, Cost: 1, Now: now}, store.ErrInvalid},
		} {
			if _, err := s.ClaimPlot(ctx, tc.c); !errors.Is(err, tc.want) {
				t.Errorf("%s: err %v, want %v", tc.name, err, tc.want)
			}
		}
		if g := gold(t, s, ps[1]); g != 50 {
			t.Errorf("gold = %d, want 50", g)
		}
		if g := gold(t, s, ps[0]); g != 100 {
			t.Errorf("owner gold = %d, want 100", g)
		}
		if p := plotByID(t, s, 2); p.OwnerID != 0 {
			t.Errorf("plot 2 owner = %d, want none", p.OwnerID)
		}
		if _, err := s.LoadHome(ctx, ps[1].ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("home of failed claim: err %v, want ErrNotFound", err)
		}
		if n := len(audit()); n != 1 {
			t.Errorf("audit has %d entries, want 1", n)
		}
	})

	t.Run("concurrent claims", func(t *testing.T) {
		s, audit, ps := newRig(t, 500, 500, 500, 500)
		errs := make([]error, len(ps))
		var wg sync.WaitGroup
		for i := range ps {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = s.ClaimPlot(ctx, claim(ps[i], 3, 400))
			}()
		}
		wg.Wait()
		winners := 0
		for i, err := range errs {
			g := gold(t, s, ps[i])
			switch {
			case err == nil:
				winners++
				if g != 100 {
					t.Errorf("winner %d gold = %d, want 100", i, g)
				}
			case !errors.Is(err, store.ErrAlreadyExists):
				t.Errorf("loser %d: err %v, want ErrAlreadyExists", i, err)
			case g != 500:
				t.Errorf("loser %d gold = %d, want 500", i, g)
			}
		}
		if winners != 1 {
			t.Fatalf("%d winners, want 1", winners)
		}
		owners := 0
		for _, p := range ps {
			if plotByID(t, s, 3).OwnerID == p.ID {
				owners++
			}
		}
		if owners != 1 || len(audit()) != 1 {
			t.Errorf("owners %d, audit entries %d; want 1 and 1", owners, len(audit()))
		}
	})

	t.Run("PayRent", func(t *testing.T) {
		s, audit, ps := newRig(t, 500)
		if _, err := s.ClaimPlot(ctx, claim(ps[0], 1, 300)); err != nil {
			t.Fatal(err)
		}
		p, err := s.PayRent(ctx, store.PayRent{PlayerID: ps[0].ID, Cost: 100, Extend: month})
		if err != nil {
			t.Fatal(err)
		}
		want := now.Add(3 * month)
		if !p.RentPaidUntil.Equal(want) || p.OwnerID != ps[0].ID {
			t.Errorf("paid plot = %+v, want until %v", p, want)
		}
		if stored := plotByID(t, s, 1); !stored.RentPaidUntil.Equal(want) {
			t.Errorf("stored until %v, want %v", stored.RentPaidUntil, want)
		}
		if g := gold(t, s, ps[0]); g != 100 {
			t.Errorf("gold = %d, want 100", g)
		}
		es := audit()
		if len(es) != 2 || es[1].Kind != store.HousingRentKind || es[1].Actor != ps[0].ID {
			t.Errorf("audit = %+v", es)
		}
	})

	t.Run("PayRent failures change nothing", func(t *testing.T) {
		s, audit, ps := newRig(t, 500, 100)
		if _, err := s.ClaimPlot(ctx, claim(ps[0], 1, 450)); err != nil {
			t.Fatal(err)
		}
		until := now.Add(2 * month)
		for _, tc := range []struct {
			name string
			r    store.PayRent
			want error
		}{
			{"too poor", store.PayRent{PlayerID: ps[0].ID, Cost: 51, Extend: month}, store.ErrInvalid},
			{"no plot", store.PayRent{PlayerID: ps[1].ID, Cost: 1, Extend: month}, store.ErrNotFound},
			{"unknown player", store.PayRent{PlayerID: 999, Cost: 1, Extend: month}, store.ErrNotFound},
			{"negative cost", store.PayRent{PlayerID: ps[0].ID, Cost: -1, Extend: month}, store.ErrInvalid},
			{"no extension", store.PayRent{PlayerID: ps[0].ID, Cost: 1}, store.ErrInvalid},
		} {
			if _, err := s.PayRent(ctx, tc.r); !errors.Is(err, tc.want) {
				t.Errorf("%s: err %v, want %v", tc.name, err, tc.want)
			}
		}
		if g := gold(t, s, ps[0]); g != 50 {
			t.Errorf("gold = %d, want 50", g)
		}
		if p := plotByID(t, s, 1); !p.RentPaidUntil.Equal(until) {
			t.Errorf("until = %v, want %v", p.RentPaidUntil, until)
		}
		if g := gold(t, s, ps[1]); g != 100 {
			t.Errorf("other gold = %d, want 100", g)
		}
		if n := len(audit()); n != 1 {
			t.Errorf("audit has %d entries, want 1", n)
		}
	})
}
