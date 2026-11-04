package main

import (
	"context"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

// AC4 (#601): housingSystem loads the store's open auctions, with their held
// bids, into the housing system.
func TestHousingSystemLoadsAuctions(t *testing.T) {
	ctx := context.Background()
	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1_700_000_000, 0)
	ends := start.Add(5 * 24 * time.Hour)
	players := store.NewMemory()
	if err := players.UpsertPlots(ctx, []store.Area{{ID: 1, Name: "a"}},
		[]store.Plot{{ID: 1, AreaID: 1, X: 10, Z: 10}, {ID: 2, AreaID: 1, X: 20, Z: 20}}); err != nil {
		t.Fatal(err)
	}
	bidder, err := players.CreateGuest(ctx, "bidder", store.Holdings{Gold: 1000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := players.OpenAuction(ctx, 1, 0, ends); err != nil {
		t.Fatal(err)
	}
	if _, err := players.PlaceAuctionBid(ctx, store.PlaceAuctionBid{PlotID: 1, Player: bidder.ID, Amount: 300}); err != nil {
		t.Fatal(err)
	}

	clock := world.NewFakeClock(start)
	sess := session.New(session.Config{Data: data}, world.New(world.DefaultConfig()), clock, players, nil)
	t.Cleanup(sess.Close)
	h, err := housingSystem(data, 5, clock, sess, players)
	if err != nil {
		t.Fatal(err)
	}
	got := map[housing.HouseID]housing.Info{}
	for _, i := range h.List() {
		got[i.ID] = i
	}
	if a := got[1]; a.State != housing.InAuction || a.TopBid != 300 || !a.AuctionEnd.Equal(ends) {
		t.Fatalf("house 1 %+v, want in auction with top bid 300 ending %v", a, ends)
	}
	if e := got[2]; e.State != housing.Empty {
		t.Fatalf("house 2 %+v, want empty", e)
	}
}
