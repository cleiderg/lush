package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
)

// AuctionStore is a store with housing and auctions.
type AuctionStore interface {
	store.Players
	store.Housing
	store.Auctions
}

// RunAuctions runs the Auctions contract against fresh stores from factory,
// which also returns a function listing the store's audit entries in append
// order (Kind and Actor only).
func RunAuctions(t *testing.T, factory func(t *testing.T) (AuctionStore, func() []store.Entry)) {
	ctx := context.Background()
	areas := []store.Area{{ID: 1, Name: "Mesa Court", X: 10, Z: 20}}
	plots := []store.Plot{{ID: 1, AreaID: 1, X: 11, Z: 21}, {ID: 2, AreaID: 1, X: 12, Z: 22}}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ends := now.Add(5 * 24 * time.Hour)
	month := 30 * 24 * time.Hour

	newRig := func(t *testing.T, gold ...int64) (AuctionStore, func() []store.Entry, []store.Player) {
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
	gold := func(t *testing.T, s AuctionStore, p store.Player) int64 {
		t.Helper()
		l, err := s.LoadByTokenHash(ctx, p.TokenHash)
		if err != nil {
			t.Fatal(err)
		}
		return l.Gold
	}
	open := func(t *testing.T, s AuctionStore, plot int32) {
		t.Helper()
		if err := s.OpenAuction(ctx, plot, 0, ends); err != nil {
			t.Fatal(err)
		}
	}
	bid := func(s AuctionStore, p store.Player, plot int32, amount int64) (store.Auction, error) {
		return s.PlaceAuctionBid(ctx, store.PlaceAuctionBid{PlotID: plot, Player: p.ID, Amount: amount})
	}
	finish := func(s AuctionStore, plot int32, rentCost int64) (store.AuctionResult, error) {
		return s.FinishAuction(ctx, store.FinishAuction{PlotID: plot, RentCost: rentCost, Rent: 2 * month, Now: ends})
	}
	kinds := func(es []store.Entry) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.Kind)
		}
		return out
	}

	t.Run("OpenAuction on empty plots only", func(t *testing.T) {
		s, _, ps := newRig(t, 500, 500)
		if err := s.OpenAuction(ctx, 1, ps[0].ID, ends); err != nil {
			t.Fatal(err)
		}
		if err := s.OpenAuction(ctx, 1, 0, ends); !errors.Is(err, store.ErrAlreadyExists) {
			t.Errorf("second auction: err %v, want ErrAlreadyExists", err)
		}
		if err := s.OpenAuction(ctx, 9, 0, ends); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("unknown plot: err %v, want ErrNotFound", err)
		}
		if err := s.OpenAuction(ctx, 2, 12345, ends); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("unknown previous owner: err %v, want ErrNotFound", err)
		}
		if _, err := s.ClaimPlot(ctx, store.ClaimPlot{PlayerID: ps[1].ID, PlotID: 2, Cost: 100, Rent: 2 * month, Now: now}); err != nil {
			t.Fatal(err)
		}
		if err := s.OpenAuction(ctx, 2, 0, ends); !errors.Is(err, store.ErrAlreadyExists) {
			t.Errorf("owned plot: err %v, want ErrAlreadyExists", err)
		}
		as, err := s.LoadAuctions(ctx)
		if err != nil || len(as) != 1 || as[0].PlotID != 1 || as[0].PreviousOwner != ps[0].ID || !as[0].EndsAt.Equal(ends) {
			t.Errorf("auctions = %+v, err %v", as, err)
		}
	})

	t.Run("ClaimPlot fails on a plot with an open auction", func(t *testing.T) {
		s, audit, ps := newRig(t, 500)
		open(t, s, 1)
		_, err := s.ClaimPlot(ctx, store.ClaimPlot{PlayerID: ps[0].ID, PlotID: 1, Cost: 100, Rent: 2 * month, Now: now})
		if !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("err %v, want ErrAlreadyExists", err)
		}
		if g := gold(t, s, ps[0]); g != 500 || len(audit()) != 0 {
			t.Errorf("gold %d, audit %d entries; want nothing changed", g, len(audit()))
		}
		all, _ := s.LoadPlots(ctx)
		if all[0].OwnerID != 0 {
			t.Errorf("plot owner = %d", all[0].OwnerID)
		}
	})

	t.Run("PlaceAuctionBid holds and refunds", func(t *testing.T) {
		s, audit, ps := newRig(t, 500, 500)
		open(t, s, 1)
		a, err := bid(s, ps[0], 1, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Bids) != 1 || a.Bids[0] != (store.AuctionBid{Player: ps[0].ID, Amount: 100, Held: 100}) {
			t.Errorf("bids = %+v", a.Bids)
		}
		if g := gold(t, s, ps[0]); g != 400 {
			t.Errorf("bidder gold = %d, want 400", g)
		}
		a, err = bid(s, ps[1], 1, 150)
		if err != nil {
			t.Fatal(err)
		}
		want := []store.AuctionBid{{Player: ps[1].ID, Amount: 150, Held: 150}, {Player: ps[0].ID, Amount: 100, Held: 0}}
		if len(a.Bids) != 2 || a.Bids[0] != want[0] || a.Bids[1] != want[1] {
			t.Errorf("bids = %+v, want %+v", a.Bids, want)
		}
		if g0, g1 := gold(t, s, ps[0]), gold(t, s, ps[1]); g0 != 500 || g1 != 350 {
			t.Errorf("gold = %d, %d; want 500, 350", g0, g1)
		}
		// The outbid player raises: only a full new hold is taken.
		if _, err := bid(s, ps[0], 1, 200); err != nil {
			t.Fatal(err)
		}
		if g0, g1 := gold(t, s, ps[0]), gold(t, s, ps[1]); g0 != 300 || g1 != 500 {
			t.Errorf("gold = %d, %d; want 300, 500", g0, g1)
		}
		// The top bidder raises: only the difference is taken.
		if _, err := bid(s, ps[0], 1, 250); err != nil {
			t.Fatal(err)
		}
		if g := gold(t, s, ps[0]); g != 250 {
			t.Errorf("gold after raise = %d, want 250", g)
		}
		es := audit()
		wantKinds := []string{store.AuctionHoldKind, store.AuctionHoldKind, store.AuctionRefundKind, store.AuctionHoldKind, store.AuctionRefundKind, store.AuctionHoldKind}
		got := kinds(es)
		if len(got) != len(wantKinds) {
			t.Fatalf("audit kinds = %v, want %v", got, wantKinds)
		}
		for i := range got {
			if got[i] != wantKinds[i] {
				t.Fatalf("audit kinds = %v, want %v", got, wantKinds)
			}
		}
		if es[2].Actor != ps[0].ID || es[1].Actor != ps[1].ID {
			t.Errorf("audit actors = %+v", es)
		}
	})

	t.Run("PlaceAuctionBid rejects without changing anything", func(t *testing.T) {
		s, audit, ps := newRig(t, 500, 50)
		open(t, s, 1)
		if _, err := bid(s, ps[0], 1, 100); err != nil {
			t.Fatal(err)
		}
		before := len(audit())
		for name, tc := range map[string]struct {
			p      store.Player
			plot   int32
			amount int64
			want   error
		}{
			"too little gold": {ps[1], 1, 120, store.ErrInvalid},
			"not above top":   {ps[1], 1, 100, store.ErrInvalid},
			"zero":            {ps[1], 1, 0, store.ErrInvalid},
			"no auction":      {ps[1], 2, 10, store.ErrNotFound},
			"unknown bidder":  {store.Player{ID: 9999}, 1, 200, store.ErrNotFound},
		} {
			if _, err := bid(s, tc.p, tc.plot, tc.amount); !errors.Is(err, tc.want) {
				t.Errorf("%s: err %v, want %v", name, err, tc.want)
			}
		}
		if g0, g1 := gold(t, s, ps[0]), gold(t, s, ps[1]); g0 != 400 || g1 != 50 {
			t.Errorf("gold = %d, %d; want 400, 50", g0, g1)
		}
		if n := len(audit()); n != before {
			t.Errorf("audit grew from %d to %d", before, n)
		}
		as, _ := s.LoadAuctions(ctx)
		if len(as) != 1 || len(as[0].Bids) != 1 || as[0].Bids[0].Amount != 100 {
			t.Errorf("auctions = %+v", as)
		}
	})

	t.Run("concurrent bids keep gold totals", func(t *testing.T) {
		s, _, ps := newRig(t, 1000, 1000, 1000)
		open(t, s, 1)
		var wg sync.WaitGroup
		for i := 0; i < 30; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Failures (not above the top) are expected.
				_, _ = bid(s, ps[i%3], 1, int64(100+i*7))
			}()
		}
		wg.Wait()
		as, err := s.LoadAuctions(ctx)
		if err != nil || len(as) != 1 {
			t.Fatalf("auctions = %+v, err %v", as, err)
		}
		var held, holding int64
		for _, b := range as[0].Bids {
			held += b.Held
			if b.Held > 0 {
				holding++
			}
		}
		if holding != 1 || len(as[0].Bids) == 0 || as[0].Bids[0].Held != as[0].Bids[0].Amount {
			t.Errorf("held bids = %+v, want only the top held", as[0].Bids)
		}
		total := held
		for _, p := range ps {
			total += gold(t, s, p)
		}
		if total != 3000 {
			t.Errorf("spendable + held = %d, want 3000", total)
		}
	})

	t.Run("FinishAuction gives the plot to the top bidder", func(t *testing.T) {
		s, audit, ps := newRig(t, 1000, 1000, 1000)
		open(t, s, 1)
		for i, amount := range []int64{100, 200, 300} {
			if _, err := bid(s, ps[i], 1, amount); err != nil {
				t.Fatal(err)
			}
		}
		res, err := finish(s, 1, 80)
		if err != nil {
			t.Fatal(err)
		}
		if res.Winner != ps[2].ID || res.Bid != 300 || res.Rent != 80 || res.Plot.OwnerID != ps[2].ID ||
			!res.Plot.ClaimedAt.Equal(ends) || !res.Plot.RentPaidUntil.Equal(ends.Add(2*month)) || len(res.Refunds) != 0 {
			t.Errorf("result = %+v", res)
		}
		if g := gold(t, s, ps[2]); g != 620 {
			t.Errorf("winner gold = %d, want 620", g)
		}
		if g0, g1 := gold(t, s, ps[0]), gold(t, s, ps[1]); g0 != 1000 || g1 != 1000 {
			t.Errorf("gold = %d, %d; want 1000, 1000", g0, g1)
		}
		h, err := s.LoadHome(ctx, ps[2].ID)
		if err != nil || h.PlotID != 1 {
			t.Errorf("home = %+v, err %v", h, err)
		}
		stored, _ := s.LoadPlots(ctx)
		if stored[0].OwnerID != ps[2].ID || !stored[0].RentPaidUntil.Equal(ends.Add(2*month)) {
			t.Errorf("stored plot = %+v", stored[0])
		}
		if as, _ := s.LoadAuctions(ctx); len(as) != 0 {
			t.Errorf("auctions left: %+v", as)
		}
		got := kinds(audit())
		if n := len(got); n != 6 || got[5] != store.AuctionWinKind {
			t.Errorf("audit kinds = %v", got)
		}
		if _, err := finish(s, 1, 80); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("finishing twice: err %v, want ErrNotFound", err)
		}
	})

	t.Run("FinishAuction skips bidders who can't pay or own a plot", func(t *testing.T) {
		s, _, ps := newRig(t, 1000, 1000, 1000, 2000)
		if _, err := s.ClaimPlot(ctx, store.ClaimPlot{PlayerID: ps[3].ID, PlotID: 2, Cost: 100, Rent: 2 * month, Now: now}); err != nil {
			t.Fatal(err)
		}
		open(t, s, 1)
		for i, amount := range []int64{100, 950, 960, 970} {
			if _, err := bid(s, ps[i], 1, amount); err != nil {
				t.Fatal(err)
			}
		}
		// ps[3] bids highest but owns a plot. ps[2] and ps[1] are outbid, so
		// have only 1000 spendable: 960 + 80 and 950 + 80 are too much.
		// ps[0] pays 100 + 80.
		res, err := finish(s, 1, 80)
		if err != nil {
			t.Fatal(err)
		}
		if res.Winner != ps[0].ID || res.Bid != 100 {
			t.Fatalf("result = %+v, want winner ps[0] at 100", res)
		}
		if len(res.Refunds) != 1 || res.Refunds[0] != (store.AuctionRefund{Player: ps[3].ID, Gold: 970}) {
			t.Errorf("refunds = %+v", res.Refunds)
		}
		want := []int64{820, 1000, 1000, 1900}
		for i, w := range want {
			if g := gold(t, s, ps[i]); g != w {
				t.Errorf("player %d gold = %d, want %d", i, g, w)
			}
		}
	})

	t.Run("FinishAuction with no taker leaves the plot empty", func(t *testing.T) {
		s, audit, ps := newRig(t, 1000, 100)
		open(t, s, 1)
		if _, err := bid(s, ps[0], 1, 1000); err != nil {
			t.Fatal(err)
		}
		if _, err := bid(s, ps[1], 1, 1100); err == nil {
			t.Fatal("bid beyond gold accepted")
		}
		res, err := finish(s, 1, 80)
		if err != nil {
			t.Fatal(err)
		}
		if res.Winner != 0 || res.Plot.OwnerID != 0 || len(res.Refunds) != 1 || res.Refunds[0].Gold != 1000 {
			t.Errorf("result = %+v", res)
		}
		if g := gold(t, s, ps[0]); g != 1000 {
			t.Errorf("gold = %d, want 1000", g)
		}
		stored, _ := s.LoadPlots(ctx)
		if stored[0].OwnerID != 0 || !stored[0].ClaimedAt.IsZero() {
			t.Errorf("stored plot = %+v", stored[0])
		}
		if _, err := s.LoadHome(ctx, ps[0].ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("home: err %v, want ErrNotFound", err)
		}
		if as, _ := s.LoadAuctions(ctx); len(as) != 0 {
			t.Errorf("auctions left: %+v", as)
		}
		// The plot can be claimed again.
		if _, err := s.ClaimPlot(ctx, store.ClaimPlot{PlayerID: ps[0].ID, PlotID: 1, Cost: 100, Rent: 2 * month, Now: now}); err != nil {
			t.Errorf("claim after empty auction: %v", err)
		}
		got := kinds(audit())
		if len(got) != 3 || got[1] != store.AuctionRefundKind {
			t.Errorf("audit kinds = %v", got)
		}
	})

	t.Run("FinishAuction with no bids", func(t *testing.T) {
		s, _, _ := newRig(t)
		open(t, s, 1)
		res, err := finish(s, 1, 80)
		if err != nil || res.Winner != 0 {
			t.Errorf("result %+v, err %v", res, err)
		}
		if as, _ := s.LoadAuctions(ctx); len(as) != 0 {
			t.Errorf("auctions left: %+v", as)
		}
	})

	t.Run("LoadAuctions returns every open auction with its bids", func(t *testing.T) {
		s, _, ps := newRig(t, 500, 500)
		if as, err := s.LoadAuctions(ctx); err != nil || len(as) != 0 {
			t.Fatalf("empty: %+v, err %v", as, err)
		}
		open(t, s, 1)
		open(t, s, 2)
		for _, b := range []struct {
			p    store.Player
			plot int32
			amt  int64
		}{{ps[0], 1, 100}, {ps[1], 1, 120}, {ps[1], 2, 90}} {
			if _, err := bid(s, b.p, b.plot, b.amt); err != nil {
				t.Fatal(err)
			}
		}
		as, err := s.LoadAuctions(ctx)
		if err != nil || len(as) != 2 || as[0].PlotID != 1 || as[1].PlotID != 2 {
			t.Fatalf("auctions = %+v, err %v", as, err)
		}
		if len(as[0].Bids) != 2 || as[0].Bids[0].Player != ps[1].ID || as[0].Bids[0].Held != 120 || as[0].Bids[1].Held != 0 {
			t.Errorf("plot 1 bids = %+v", as[0].Bids)
		}
		if len(as[1].Bids) != 1 || as[1].Bids[0] != (store.AuctionBid{Player: ps[1].ID, Amount: 90, Held: 90}) {
			t.Errorf("plot 2 bids = %+v", as[1].Bids)
		}
	})
}
