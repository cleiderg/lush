package housing_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

// aucFixture has house 1 in auction (previous owner 9) and online players
// 1, 2 and 3 standing at its door.
type aucFixture struct {
	*sysFixture
	players map[world.PlayerID]*world.Player
	dur     time.Duration
}

func newAucFixture(t *testing.T, plots []housing.Plot, gold int) *aucFixture {
	d, _ := gamedata.Default()
	dur := housing.New(d.Rules).AuctionDuration()
	if plots == nil {
		plots = []housing.Plot{{ID: 1, Door: door, Auction: &housing.Auction{PreviousOwner: 9, EndsAt: sysStart.Add(dur)}}}
	}
	f := &aucFixture{sysFixture: newSysFixture(t, plots, gold), players: map[world.PlayerID]*world.Player{}, dur: dur}
	f.players[1] = f.p
	for _, id := range []world.PlayerID{2, 3, 9} {
		p := &world.Player{ID: id, Pos: door, Inv: world.NewInventory(f.d)}
		p.Inv.Gold = gold
		if err := f.w.AddPlayer(p); err != nil {
			t.Fatal(err)
		}
		f.players[id] = p
	}
	return f
}

func (f *aucFixture) bid(player world.PlayerID, amount int) []housing.AuctionOutcome {
	f.w.Enqueue(housing.BidCommand{Player: player, House: 1, Amount: amount})
	f.w.Tick()
	got := f.sink.auction
	f.sink.auction = nil
	return got
}

func (f *aucFixture) confirmLast(ok bool) {
	f.sys.Resolve(f.com.reqs[len(f.com.reqs)-1].ID, ok)
	f.w.Tick()
}

func TestLapseOpensAuctionAndBlocksClaim(t *testing.T) {
	plots := []housing.Plot{{ID: 1, Door: door, Owner: 7, PaidUntil: sysStart}}
	f := newSysFixture(t, plots, 1000)
	f.run()
	f.sys.Resolve(f.com.reqs[0].ID, true)
	f.run()
	d, _ := gamedata.Default()
	i := f.info(1)
	if i.State != housing.InAuction || !i.AuctionEnd.Equal(sysStart.Add(housing.New(d.Rules).AuctionDuration())) {
		t.Fatalf("info %+v", i)
	}
	if got := f.run(housing.ClaimCommand{Player: 1, House: 1}); !reflect.DeepEqual(got, []housing.Outcome{housing.AlreadyOwned}) {
		t.Fatalf("claim in auction: %v", got)
	}
}

func TestBidRefusals(t *testing.T) {
	far := world.Vec2{X: 500, Y: 500}
	plots := []housing.Plot{
		{ID: 1, Door: door, Auction: &housing.Auction{PreviousOwner: 9, EndsAt: sysStart.Add(time.Hour),
			Bids: []housing.Bid{{Player: 3, Amount: 300, Held: 300}}}},
		{ID: 2, Door: door, Owner: 8, PaidUntil: sysStart.Add(900 * time.Hour)},
		{ID: 3, Door: far, Auction: &housing.Auction{PreviousOwner: 9, EndsAt: sysStart.Add(time.Hour)}},
		{ID: 4, Door: door, Owner: 2, PaidUntil: sysStart.Add(900 * time.Hour)},
	}
	tests := []struct {
		name   string
		player world.PlayerID
		house  housing.HouseID
		amount int
		want   housing.AuctionOutcome
	}{
		{"not auction", 1, 2, 500, housing.AuctionNotAuction},
		{"no such house", 1, 77, 500, housing.AuctionNotAuction},
		{"too far", 1, 3, 500, housing.AuctionTooFar},
		{"previous owner", 9, 1, 500, housing.AuctionPreviousOwner},
		{"too low", 1, 1, 300, housing.AuctionBidTooLow},
		{"not enough gold", 1, 1, 5000, housing.AuctionNotEnoughGold},
		{"owns another", 2, 1, 500, housing.AuctionOwnsAnother},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newAucFixture(t, plots, 1000)
			f.w.Enqueue(housing.BidCommand{Player: tc.player, House: tc.house, Amount: tc.amount})
			f.w.Tick()
			if !reflect.DeepEqual(f.sink.auction, []housing.AuctionOutcome{tc.want}) {
				t.Fatalf("got %v, want %v", f.sink.auction, tc.want)
			}
			if len(f.com.reqs) != 0 || len(f.sink.lists) != 0 || f.players[tc.player].Inv.Gold != 1000 || f.players[tc.player].Inv.Locked() {
				t.Fatal("refusal changed something")
			}
		})
	}
}

func TestBidCommitAndRefund(t *testing.T) {
	f := newAucFixture(t, nil, 1000)
	if got := f.bid(2, 250); len(got) != 0 {
		t.Fatalf("result before commit: %v", got)
	}
	r := f.com.reqs[0]
	if r.Kind != housing.CommitBid || r.Player != 2 || r.Gold != 250 || r.Amount != 250 || r.Reason != housing.ReasonAuctionHold || len(r.Refunds) != 0 {
		t.Fatalf("request %+v", r)
	}
	if !f.players[2].Inv.Locked() || f.players[2].Inv.Gold != 1000 || f.info(1).TopBid != 0 {
		t.Fatal("state before confirmation")
	}
	f.confirmLast(true)
	if !reflect.DeepEqual(f.sink.auction, []housing.AuctionOutcome{housing.AuctionOk}) || f.players[2].Inv.Gold != 750 || f.players[2].Inv.Locked() || f.info(1).TopBid != 250 {
		t.Fatalf("after confirm: gold %d info %+v", f.players[2].Inv.Gold, f.info(1))
	}
	f.sink.auction = nil

	// Player 1 outbids player 2: both locked, refund in the request.
	f.bid(1, 300)
	r = f.com.reqs[1]
	if r.Gold != 300 || len(r.Refunds) != 1 || r.Refunds[0].Player != 2 || r.Refunds[0].Gold != 250 {
		t.Fatalf("request %+v", r)
	}
	if !f.players[1].Inv.Locked() || !f.players[2].Inv.Locked() {
		t.Fatal("inventories not locked")
	}
	// Failure leaves everything unchanged.
	f.confirmLast(false)
	if !reflect.DeepEqual(f.sink.auction, []housing.AuctionOutcome{housing.AuctionSaveFailed}) ||
		f.players[1].Inv.Gold != 1000 || f.players[2].Inv.Gold != 750 || f.players[1].Inv.Locked() || f.players[2].Inv.Locked() || f.info(1).TopBid != 250 {
		t.Fatal("failed commit changed something")
	}
	f.sink.auction = nil
	f.bid(1, 300)
	f.confirmLast(true)
	if f.players[1].Inv.Gold != 700 || f.players[2].Inv.Gold != 1000 || f.info(1).TopBid != 300 {
		t.Fatalf("gold %d %d", f.players[1].Inv.Gold, f.players[2].Inv.Gold)
	}
	if ev := f.players[2].PendingEconomy(); len(ev) != 2 || ev[1].Reason != housing.ReasonAuctionRefund {
		t.Fatalf("economy %+v", ev)
	}
}

func TestAuctionResolution(t *testing.T) {
	bids := []housing.Bid{{Player: 1, Amount: 300}, {Player: 2, Amount: 500, Held: 500}, {Player: 3, Amount: 400}}
	// 1 and 3 were outbid earlier (their gold already returned: Held 0).
	plots := []housing.Plot{{ID: 1, Door: door, Auction: &housing.Auction{PreviousOwner: 9, EndsAt: sysStart.Add(time.Hour), Bids: bids}}}
	f := newAucFixture(t, plots, 1000)
	f.players[2].Inv.Gold = 600 // 500 is held elsewhere
	f.run()
	if len(f.com.reqs) != 0 {
		t.Fatal("resolved early")
	}
	f.clock.Advance(time.Hour)
	f.run()
	f.run() // only once
	if len(f.com.reqs) != 1 {
		t.Fatalf("%d commits", len(f.com.reqs))
	}
	r := f.com.reqs[0]
	if r.Kind != housing.CommitResolve || r.Rent != 400 ||
		!reflect.DeepEqual(r.Bids, []housing.AuctionBid{{2, 500, 500}, {3, 400, 0}, {1, 300, 0}}) {
		t.Fatalf("request %+v", r)
	}
	for _, id := range []world.PlayerID{1, 2, 3} {
		if !f.players[id].Inv.Locked() {
			t.Fatalf("player %d not locked", id)
		}
	}
	if f.players[9].Inv.Locked() {
		t.Fatal("non-bidder locked")
	}
	// A failed commit leaves the auction open and it is tried again.
	f.confirmLast(false)
	if f.info(1).State != housing.InAuction || !f.players[2].Inv.Locked() || len(f.com.reqs) != 2 {
		t.Fatalf("after failure: %d commits", len(f.com.reqs))
	}
	// The store picks player 3 (2 couldn't pay); 2's held bid is refunded.
	f.sys.ResolveAuction(f.com.reqs[1].ID, 3)
	f.run()
	i := f.info(1)
	if i.Owner != 3 || i.State != housing.Owned || !i.PaidUntil.Equal(sysStart.Add(time.Hour+60*24*time.Hour)) {
		t.Fatalf("info %+v", i)
	}
	if f.players[3].Inv.Gold != 1000-400-400+0 || f.players[2].Inv.Gold != 1100 || f.players[1].Inv.Gold != 1000 {
		t.Fatalf("gold %d %d %d", f.players[1].Inv.Gold, f.players[2].Inv.Gold, f.players[3].Inv.Gold)
	}
	for _, id := range []world.PlayerID{1, 2, 3} {
		if f.players[id].Inv.Locked() {
			t.Fatalf("player %d still locked", id)
		}
	}
	if ev := f.players[3].PendingEconomy(); len(ev) != 1 || ev[0].Reason != housing.ReasonAuctionWin {
		t.Fatalf("economy %+v", ev)
	}
}

func TestAuctionNoTakerAndNoBids(t *testing.T) {
	for _, name := range []string{"no bids", "no taker"} {
		t.Run(name, func(t *testing.T) {
			a := &housing.Auction{PreviousOwner: 9, EndsAt: sysStart}
			if name == "no taker" {
				a.Bids = []housing.Bid{{Player: 2, Amount: 500, Held: 500}}
			}
			f := newAucFixture(t, []housing.Plot{{ID: 1, Door: door, Auction: a}}, 1000)
			f.players[2].Inv.Gold = 500
			f.run()
			f.sys.ResolveAuction(f.com.reqs[0].ID, 0)
			f.run()
			if i := f.info(1); i.Owner != 0 || i.State != housing.Empty {
				t.Fatalf("info %+v", i)
			}
			want := 500
			if name == "no taker" {
				want = 1000
			}
			if f.players[2].Inv.Gold != want {
				t.Fatalf("gold %d", f.players[2].Inv.Gold)
			}
			if got := f.run(housing.ClaimCommand{Player: 1, House: 1}); len(got) != 0 || len(f.com.reqs) != 2 {
				t.Fatalf("claim: %v", got)
			}
		})
	}
}

func TestAuctionHouseList(t *testing.T) {
	f := newAucFixture(t, nil, 1000)
	f.bid(2, 250)
	f.confirmLast(true)
	byPlayer := map[world.PlayerID]housing.Info{}
	n := len(f.sink.lists)
	for i, to := range f.sink.listTo[n-4:] {
		byPlayer[to] = f.sink.lists[n-4+i][0]
	}
	for id, i := range byPlayer {
		if i.State != housing.InAuction || i.TopBid != 250 || !i.AuctionEnd.Equal(sysStart.Add(f.dur)) {
			t.Fatalf("player %d: %+v", id, i)
		}
		want := 0
		if id == 2 {
			want = 250
		}
		if i.MyBid != want {
			t.Fatalf("player %d MyBid %d, want %d", id, i.MyBid, want)
		}
	}
	if len(byPlayer) != 4 {
		t.Fatalf("lists to %d players", len(byPlayer))
	}
}
