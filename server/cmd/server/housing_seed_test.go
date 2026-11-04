package main

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/furniture"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// AC1 (#621): the embedded copy matches shared/map/housing.json.
func TestEmbeddedHousingMatchShared(t *testing.T) {
	shared, err := os.ReadFile("../../../shared/map/housing.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(housingData, shared) {
		t.Fatal("server/cmd/server/housing.json differs from shared/map/housing.json; " +
			"refresh it from the repo root with: cp shared/map/housing.json server/cmd/server/housing.json")
	}
}

// AC2 (#621): ids follow file order, positions are metres.
func TestHousingSeedMapping(t *testing.T) {
	seed, err := loadHousingSeed()
	if err != nil {
		t.Fatal(err)
	}
	if len(seed.Areas) != 2 || len(seed.Plots) != 40 {
		t.Fatalf("%d areas, %d plots, want 2 and 40", len(seed.Areas), len(seed.Plots))
	}
	if a := seed.Areas[0]; a.ID != 1 || a.Name != "Mesa Court" || a.X != -166.5 || a.Z != -765.5 {
		t.Fatalf("area 1 = %+v", a)
	}
	if seed.Areas[1].ID != 2 {
		t.Fatalf("area 2 = %+v", seed.Areas[1])
	}
	if p := seed.Plots[0]; p.ID != 1 || p.AreaID != 1 || p.X != -298.5 || p.Z != -696.5 {
		t.Fatalf("plot 1 = %+v", p)
	}
	for j, p := range seed.Plots {
		if p.ID != int32(j+1) || (p.AreaID != 1 && p.AreaID != 2) {
			t.Fatalf("plot %d = %+v", j, p)
		}
	}
	mem := store.NewMemory()
	if err := seedHousing(seed, mem); err != nil {
		t.Fatal(err)
	}
	got, err := mem.LoadPlots(context.Background())
	if err != nil || len(got) != 40 {
		t.Fatalf("stored %d plots, err %v", len(got), err)
	}
}

// AC3 (#621): reseeding keeps owners and rent dates, and the systems are built
// with every door.
func TestHousingReseedKeepsOwnersAndBuildsSystems(t *testing.T) {
	seed, err := loadHousingSeed()
	if err != nil {
		t.Fatal(err)
	}
	mem := store.NewMemory()
	if err := seedHousing(seed, mem); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	acc, err := mem.CreateGuest(ctx, "tokenhash", store.Holdings{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	rent := 30 * 24 * time.Hour
	paid := now.Add(rent)
	if _, err := mem.ClaimPlot(ctx, store.ClaimPlot{PlayerID: acc.ID, PlotID: 7, Rent: rent, Now: now}); err != nil {
		t.Fatal(err)
	}
	if err := seedHousing(seed, mem); err != nil {
		t.Fatal(err)
	}
	plots, _ := mem.LoadPlots(ctx)
	if len(plots) != 40 || plots[6].OwnerID != acc.ID || !plots[6].RentPaidUntil.Equal(paid) {
		t.Fatalf("plot 7 = %+v", plots[6])
	}

	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(world.DefaultConfig())
	clock := world.NewFakeClock(time.Unix(0, 0))
	sess := session.New(session.Config{Data: data}, w, clock, mem, nil)
	t.Cleanup(sess.Close)
	homes, err := housingSystem(data, 5, clock, sess, mem)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(homes.List()); got != 40 {
		t.Fatalf("housing system has %d plots, want 40", got)
	}
	rooms, err := homeRoomSystem(data, 5, clock, sess, mem)
	if err != nil {
		t.Fatal(err)
	}
	_ = rooms
}

type furnSink struct{ got []furniture.Result }

func (s *furnSink) FurnitureResult(_ world.PlayerID, r furniture.Result) { s.got = append(s.got, r) }

// AC4 (#621): stations come from every area, with reach shop_distance_tiles ×
// tile size; a thrift buy works at a stall and is TooFar elsewhere.
func TestFurnitureStations(t *testing.T) {
	seed, err := loadHousingSeed()
	if err != nil {
		t.Fatal(err)
	}
	if len(seed.Stalls) != 2 || len(seed.Workbenches) != 2 {
		t.Fatalf("%d stalls, %d workbenches", len(seed.Stalls), len(seed.Workbenches))
	}
	if seed.Stalls[0] != (world.Vec2{X: -166.5, Y: -765.5}) || seed.Workbenches[0] != (world.Vec2{X: -167.5, Y: -767.5}) {
		t.Fatalf("area 1 stations %v %v", seed.Stalls[0], seed.Workbenches[0])
	}
	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	const tile = 5.0
	reach := float64(data.Rules.ShopDistanceTiles) * tile
	var item gamedata.ItemID
	for _, it := range data.Items {
		if _, ok := data.ThriftSellPrice(it.ID); ok {
			item = it.ID
			break
		}
	}
	buy := func(sys *furniture.System, sink *furnSink, pos world.Vec2) furniture.Result {
		p := &world.Player{ID: 1, Pos: pos, Inv: world.NewInventory(data)}
		p.Inv.Gold = 100000
		sys.Handle(furniture.BuyCommand{Player: 1, Item: item, Quantity: 1}, p)
		return sink.got[len(sink.got)-1]
	}
	sink := &furnSink{}
	sys := newFurniture(data, tile, seed, sink)
	for i, st := range seed.Stalls {
		if got := buy(sys, sink, st); got != furniture.Ok {
			t.Fatalf("stall %d: %v", i, got)
		}
		edge := world.Vec2{X: st.X + float32(reach), Y: st.Y}
		if got := buy(sys, sink, edge); got != furniture.Ok {
			t.Fatalf("stall %d at the boundary: %v", i, got)
		}
		out := world.Vec2{X: st.X + float32(reach) + 0.5, Y: st.Y}
		if got := buy(sys, sink, out); got != furniture.TooFar {
			t.Fatalf("stall %d beyond reach: %v", i, got)
		}
	}
	if got := buy(sys, sink, world.Vec2{X: 0, Y: 0}); got != furniture.TooFar {
		t.Fatalf("far from every stall: %v", got)
	}
}
