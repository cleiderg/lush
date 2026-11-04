package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/furniture"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// housingData is a copy of shared/map/housing.json, embedded so the deployed
// binary needs no extra file. TestEmbeddedHousingMatchShared keeps it in sync;
// refresh with: cp shared/map/housing.json server/cmd/server/housing.json
//
//go:embed housing.json
var housingData []byte

type dmPoint struct {
	X float32 `json:"x"`
	Z float32 `json:"z"`
}

func (p dmPoint) metres() world.Vec2 { return world.Vec2{X: p.X / 10, Y: p.Z / 10} }

// housingSeed is housing.json as store rows plus the station positions, in
// metres. The area at index i of areas[] gets area_id i+1 and the house at
// index j of houses[] gets plot_id j+1, so ids stay stable while the file only
// appends. An area's map position is its thrift stall.
type housingSeed struct {
	Areas       []store.Area
	Plots       []store.Plot
	Stalls      []world.Vec2
	Workbenches []world.Vec2
}

// loadHousingSeed parses the embedded housing data.
func loadHousingSeed() (housingSeed, error) {
	var hm struct {
		Units string `json:"units"`
		Areas []struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Thrift    *dmPoint `json:"thrift"`
			Workbench *dmPoint `json:"workbench"`
		} `json:"areas"`
		Houses []struct {
			Area string  `json:"area"`
			X    float32 `json:"x"`
			Z    float32 `json:"z"`
		} `json:"houses"`
	}
	if err := json.Unmarshal(housingData, &hm); err != nil {
		return housingSeed{}, fmt.Errorf("embedded housing data: %w", err)
	}
	if hm.Units != "decimetres" {
		return housingSeed{}, fmt.Errorf("embedded housing data: want decimetres units, got %q", hm.Units)
	}
	var s housingSeed
	ids := map[string]int32{}
	for i, a := range hm.Areas {
		if a.Thrift == nil || a.Workbench == nil {
			return housingSeed{}, fmt.Errorf("embedded housing data: area %q lacks a thrift or workbench point", a.ID)
		}
		id := int32(i + 1)
		ids[a.ID] = id
		t := a.Thrift.metres()
		s.Areas = append(s.Areas, store.Area{ID: id, Name: a.Name, X: t.X, Z: t.Y})
		s.Stalls = append(s.Stalls, t)
		s.Workbenches = append(s.Workbenches, a.Workbench.metres())
	}
	for j, h := range hm.Houses {
		area, ok := ids[h.Area]
		if !ok {
			return housingSeed{}, fmt.Errorf("embedded housing data: house %d names unknown area %q", j, h.Area)
		}
		s.Plots = append(s.Plots, store.Plot{ID: int32(j + 1), AreaID: area, X: h.X / 10, Z: h.Z / 10})
	}
	return s, nil
}

// seedHousing upserts the areas and plots into the store, if it keeps
// housing. Existing owners and rent dates are kept.
func seedHousing(seed housingSeed, players store.Players) error {
	homes, ok := players.(store.Housing)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbStartTimeout)
	defer cancel()
	if err := homes.UpsertPlots(ctx, seed.Areas, seed.Plots); err != nil {
		return fmt.Errorf("seed housing: %w", err)
	}
	return nil
}

// newFurniture creates the furniture system with every area's thrift stall
// and workbench, reachable from the same distance as the shop and bank.
func newFurniture(data *gamedata.Data, tileSize float64, seed housingSeed, sink furniture.Sink) *furniture.System {
	reach := float64(data.Rules.ShopDistanceTiles) * tileSize
	return furniture.New(data, seed.Workbenches, seed.Stalls, reach, sink)
}

// furnitureSystem connects the furniture system to the session, which hands it
// its commands; it is not registered on the world.
func furnitureSystem(data *gamedata.Data, tileSize float64, seed housingSeed, sess *session.Server) {
	sess.SetFurniture(newFurniture(data, tileSize, seed, sess.FurnitureSink()))
}
