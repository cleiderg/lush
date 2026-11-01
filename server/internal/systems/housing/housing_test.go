package housing_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

const day = 24 * time.Hour

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func testRules() gamedata.Rules {
	return gamedata.Rules{HouseRentPerMonth: 200, HouseDaysPerMonth: 30, HouseClaimMonths: 2,
		HouseMaxPrepaidMonths: 6, HouseWarningDays: 7, HouseAuctionDays: 5}
}

func TestClaim(t *testing.T) {
	r := housing.New(testRules())
	owned := housing.House{Owner: 9, PaidUntil: t0.Add(90 * day)}
	tests := []struct {
		name  string
		house housing.House
		owns  bool
		gold  int
		err   error
	}{
		{"empty, exact gold", housing.House{}, false, 400, nil},
		{"owned", owned, false, 1000, housing.ErrOwned},
		{"player has a house", housing.House{}, true, 1000, housing.ErrAlreadyOwnsHouse},
		{"too little gold", housing.House{}, false, 399, housing.ErrNotEnoughGold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, cost, err := r.Claim(tt.house, 1, tt.owns, tt.gold, t0)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err != nil {
				if h != tt.house || cost != 0 {
					t.Errorf("rejected claim changed state: %v %d", h, cost)
				}
				return
			}
			if cost != 400 || h.Owner != 1 || !h.PaidUntil.Equal(t0.Add(60*day)) {
				t.Errorf("got %+v cost %d", h, cost)
			}
		})
	}
}

func TestPayRent(t *testing.T) {
	r := housing.New(testRules())
	h := housing.House{Owner: 1, PaidUntil: t0.Add(60 * day)}
	tests := []struct {
		name    string
		player  world.PlayerID
		months  int
		gold    int
		want    int
		err     error
		newLeft time.Duration
	}{
		{"one month", 1, 1, 200, 200, nil, 90 * day},
		{"up to the maximum", 1, 4, 800, 800, nil, 180 * day},
		{"past the maximum", 1, 5, 1000, 0, housing.ErrTooManyMonths, 0},
		{"too little gold", 1, 2, 399, 0, housing.ErrNotEnoughGold, 0},
		{"zero months", 1, 0, 100, 0, housing.ErrBadMonths, 0},
		{"not the owner", 2, 1, 200, 0, housing.ErrNotOwner, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cost, err := r.PayRent(h, tt.player, tt.months, tt.gold, t0)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err != nil {
				if got != h {
					t.Error("rejected payment changed the house")
				}
				return
			}
			if cost != tt.want || !got.PaidUntil.Equal(t0.Add(tt.newLeft)) {
				t.Errorf("cost %d paidUntil +%v", cost, got.PaidUntil.Sub(t0))
			}
		})
	}
	lapsed := housing.House{Owner: 1, PaidUntil: t0.Add(30*day - time.Second)}
	if _, _, err := r.PayRent(lapsed, 1, 1, 200, t0); !errors.Is(err, housing.ErrLapsed) {
		t.Errorf("lapsed house: err = %v", err)
	}
}

func TestStateEdges(t *testing.T) {
	r := housing.New(testRules())
	clock := world.NewFakeClock(t0)
	h := housing.House{Owner: 1, PaidUntil: t0.Add(40 * day)}
	// Warning starts when 7 days remain before the 1-month mark: 37 days left.
	steps := []struct {
		advance time.Duration
		want    housing.State
	}{
		{0, housing.Owned},
		{3*day - time.Second, housing.Owned},   // 37d+1s left
		{time.Second, housing.Owned},           // exactly 37d left: 7d before the mark, not fewer
		{time.Second, housing.Warning},         // 37d-1s left
		{7*day - time.Second, housing.Warning}, // exactly 30d left: still a month prepaid
		{time.Second, housing.Lapsed},          // just under a month
	}
	for i, s := range steps {
		clock.Advance(s.advance)
		if got := r.State(h, clock.Now()); got != s.want {
			t.Errorf("step %d: state = %v, want %v", i, got, s.want)
		}
	}
	if got := r.State(housing.House{}, t0); got != housing.Empty {
		t.Errorf("empty house = %v", got)
	}
}

// TestStateEdgesExact pins the state exactly at 30 days left.
func TestStateExactMonth(t *testing.T) {
	r := housing.New(testRules())
	h := housing.House{Owner: 1, PaidUntil: t0.Add(30 * day)}
	if got := r.State(h, t0); got != housing.Warning {
		t.Errorf("exactly one month prepaid = %v, want Warning", got)
	}
}

// TestReadsRulesJSON checks each setting comes from rules.json and changing it
// changes the behaviour.
func TestReadsRulesJSON(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	rules := d.Rules
	want := gamedata.Rules{HouseRentPerMonth: 200, HouseDaysPerMonth: 30, HouseClaimMonths: 2,
		HouseMaxPrepaidMonths: 6, HouseWarningDays: 7, HouseAuctionDays: 5}
	if rules.HouseRentPerMonth != want.HouseRentPerMonth || rules.HouseDaysPerMonth != want.HouseDaysPerMonth ||
		rules.HouseClaimMonths != want.HouseClaimMonths || rules.HouseMaxPrepaidMonths != want.HouseMaxPrepaidMonths ||
		rules.HouseWarningDays != want.HouseWarningDays || rules.HouseAuctionDays != want.HouseAuctionDays {
		t.Errorf("rules.json house settings = %+v", rules)
	}

	custom := gamedata.Rules{HouseRentPerMonth: 50, HouseDaysPerMonth: 10, HouseClaimMonths: 3,
		HouseMaxPrepaidMonths: 4, HouseWarningDays: 2, HouseAuctionDays: 9}
	r := housing.New(custom)
	h, cost, err := r.Claim(housing.House{}, 1, false, 150, t0)
	if err != nil || cost != 150 || !h.PaidUntil.Equal(t0.Add(30*day)) {
		t.Errorf("claim = %+v %d %v", h, cost, err)
	}
	if _, _, err := r.PayRent(h, 1, 2, 100, t0); !errors.Is(err, housing.ErrTooManyMonths) {
		t.Errorf("pay past custom max: %v", err)
	}
	if got := r.AuctionDuration(); got != 9*day {
		t.Errorf("auction duration = %v", got)
	}
	// 10-day month, 2 warning days: 12 days left is the edge.
	h = housing.House{Owner: 1, PaidUntil: t0.Add(12*day - time.Second)}
	if got := r.State(h, t0); got != housing.Warning {
		t.Errorf("state = %v", got)
	}
	if got := r.State(housing.House{Owner: 1, PaidUntil: t0.Add(12 * day)}, t0); got != housing.Owned {
		t.Errorf("state = %v", got)
	}
}
