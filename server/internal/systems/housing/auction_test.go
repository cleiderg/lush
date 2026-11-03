package housing_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

const rent = 200 // rules.json house_rent_per_month; see TestReadsRulesJSON

func auctionRules() housing.Rules { return housing.New(testRules()) }

func TestPlaceBidRefused(t *testing.T) {
	r := auctionRules()
	now := time.Unix(1e6, 0)
	a := r.StartAuction(9, now)
	a, _, _, err := r.PlaceBid(a, 1, rent+50, false, 1e6, now)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		player world.PlayerID
		amount int
		owns   bool
		gold   int
		want   error
	}{
		{"below rent", 2, rent - 1, false, 1e6, housing.ErrBidTooLow},
		{"equal to top", 2, rent + 50, false, 1e6, housing.ErrNotAboveTop},
		{"previous owner", 9, rent + 100, false, 1e6, housing.ErrPreviousOwner},
		{"owns a house", 2, rent + 100, true, 1e6, housing.ErrAlreadyOwnsHouse},
		{"not enough gold", 2, rent + 100, false, rent + 99, housing.ErrNotEnoughGold},
		{"raise own, short of difference", 1, rent + 100, false, 49, housing.ErrNotEnoughGold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, _, err := r.PlaceBid(a, tt.player, tt.amount, tt.owns, tt.gold, now); err != tt.want {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
	if _, _, _, err := r.PlaceBid(r.StartAuction(9, now), 2, rent-1, false, 1e6, now); err != housing.ErrBidTooLow {
		t.Fatalf("first bid below rent: %v", err)
	}
	if _, hold, _, err := r.PlaceBid(r.StartAuction(9, now), 2, rent, false, rent, now); err != nil || hold != rent {
		t.Fatalf("first bid at rent: hold %d err %v", hold, err)
	}
	if _, _, _, err := r.PlaceBid(a, 2, rent+100, false, 1e6, a.EndsAt); err != housing.ErrAuctionOver {
		t.Fatalf("after end: %v", err)
	}
}

func TestPlaceBidRefundsAndDifference(t *testing.T) {
	r := auctionRules()
	now := time.Unix(1e6, 0)
	a := r.StartAuction(9, now)
	a, hold, refunds, _ := r.PlaceBid(a, 1, rent, false, 1e6, now)
	if hold != rent || len(refunds) != 0 {
		t.Fatalf("first: hold %d refunds %v", hold, refunds)
	}
	a, hold, refunds, err := r.PlaceBid(a, 2, rent+10, false, 1e6, now)
	if err != nil || hold != rent+10 || !reflect.DeepEqual(refunds, []housing.Refund{{1, rent}}) {
		t.Fatalf("outbid: hold %d refunds %v err %v", hold, refunds, err)
	}
	a, hold, refunds, err = r.PlaceBid(a, 2, rent+30, false, 20, now)
	if err != nil || hold != 20 || len(refunds) != 0 {
		t.Fatalf("raise own: hold %d refunds %v err %v", hold, refunds, err)
	}
	// Player 1 returns: nothing was held for them, so the whole bid is held.
	_, hold, refunds, err = r.PlaceBid(a, 1, rent+40, false, 1e6, now)
	if err != nil || hold != rent+40 || !reflect.DeepEqual(refunds, []housing.Refund{{2, rent + 30}}) {
		t.Fatalf("rebid: hold %d refunds %v err %v", hold, refunds, err)
	}
}

func TestResolve(t *testing.T) {
	r := auctionRules()
	now := time.Unix(1e6, 0)
	two := r.ClaimCost()
	a := r.StartAuction(9, now)
	for _, b := range []struct {
		p world.PlayerID
		n int
	}{{1, rent}, {2, rent + 10}, {3, rent + 20}} {
		a, _, _, _ = r.PlaceBid(a, b.p, b.n, false, 1e6, now)
	}
	end := a.EndsAt
	rich := func(world.PlayerID) housing.Wallet { return housing.Wallet{Gold: 1e6} }

	t.Run("before end changes nothing", func(t *testing.T) {
		if got := r.Resolve(a, rich, end.Add(-time.Second)); !reflect.DeepEqual(got, housing.Result{}) {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("top bidder wins", func(t *testing.T) {
		got := r.Resolve(a, rich, end)
		want := housing.House{Owner: 3, PaidUntil: end.Add(2 * 30 * day)}
		if !got.Done || got.House != want || got.Bid != rent+20 || got.Rent != two || got.Take != two {
			t.Fatalf("got %+v", got)
		}
		if len(got.Refunds) != 0 {
			t.Fatalf("refunds %v", got.Refunds)
		}
	})
	t.Run("top cannot pay rent, next takes it", func(t *testing.T) {
		w := func(p world.PlayerID) housing.Wallet {
			if p == 3 {
				return housing.Wallet{Gold: two - 1}
			}
			return housing.Wallet{Gold: 1e6}
		}
		got := r.Resolve(a, w, end)
		if got.Winner != 2 || got.Bid != rent+10 || got.Take != rent+10+two {
			t.Fatalf("got %+v", got)
		}
		if !reflect.DeepEqual(got.Refunds, []housing.Refund{{3, rent + 20}}) {
			t.Fatalf("refunds %v", got.Refunds)
		}
	})
	t.Run("top now owns a house", func(t *testing.T) {
		w := func(p world.PlayerID) housing.Wallet { return housing.Wallet{Gold: 1e6, OwnsHouse: p == 3} }
		got := r.Resolve(a, w, end)
		if got.Winner != 2 || !reflect.DeepEqual(got.Refunds, []housing.Refund{{3, rent + 20}}) {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("nobody can take it", func(t *testing.T) {
		poor := func(world.PlayerID) housing.Wallet { return housing.Wallet{} }
		got := r.Resolve(a, poor, end)
		if !got.Done || got.House != (housing.House{}) || got.Winner != 0 || got.Take != 0 {
			t.Fatalf("got %+v", got)
		}
		if !reflect.DeepEqual(got.Refunds, []housing.Refund{{3, rent + 20}}) {
			t.Fatalf("refunds %v", got.Refunds)
		}
		if r.State(got.House, end) != housing.Empty {
			t.Fatal("not claimable")
		}
	})
	t.Run("no bids", func(t *testing.T) {
		got := r.Resolve(r.StartAuction(9, now), rich, end)
		if !got.Done || got.House != (housing.House{}) || len(got.Refunds) != 0 {
			t.Fatalf("got %+v", got)
		}
	})
}
