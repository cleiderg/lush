package gamedata_test

import (
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

type furnitureRow struct {
	name        string
	w, d        int
	layer       gamedata.FurnitureLayer
	slots       int
	floor, surf bool
	thrift      int // 0: not sold
}

// furnitureTable is the "Starter furniture" table in docs/specs/housing.md.
var furnitureTable = []furnitureRow{
	{"Wooden chair", 1, 1, gamedata.LayerFloor, 0, true, false, 40},
	{"Stool", 1, 1, gamedata.LayerFloor, 0, true, false, 25},
	{"Wooden table", 2, 1, gamedata.LayerFloor, 2, true, false, 80},
	{"Oak dining table", 2, 2, gamedata.LayerFloor, 4, true, false, 300},
	{"Desk", 2, 1, gamedata.LayerFloor, 2, true, false, 120},
	{"Bookshelf", 2, 1, gamedata.LayerFloor, 2, true, false, 150},
	{"Single bed", 1, 2, gamedata.LayerFloor, 0, true, false, 200},
	{"Double bed", 2, 2, gamedata.LayerFloor, 0, true, false, 450},
	{"Sofa", 2, 1, gamedata.LayerFloor, 0, true, false, 260},
	{"Beanbag", 1, 1, gamedata.LayerFloor, 0, true, false, 90},
	{"Rug (small)", 2, 2, gamedata.LayerRug, 0, true, false, 60},
	{"Rug (large)", 3, 3, gamedata.LayerRug, 0, true, false, 140},
	{"Floor lamp", 1, 1, gamedata.LayerFloor, 0, true, false, 70},
	{"Eucalyptus wardrobe", 2, 1, gamedata.LayerFloor, 0, true, false, 600},
	{"Eucalyptus cabinet", 1, 1, gamedata.LayerFloor, 1, true, false, 400},
	{"Potted plant", 1, 1, gamedata.LayerFloor, 0, true, true, 35},
	{"Cactus", 1, 1, gamedata.LayerSurface, 0, false, true, 20},
	{"Desk lamp", 1, 1, gamedata.LayerSurface, 0, false, true, 30},
	{"Laptop", 1, 1, gamedata.LayerSurface, 0, false, true, 250},
	{"Stack of books", 1, 1, gamedata.LayerSurface, 0, false, true, 15},
	{"Coffee mug", 1, 1, gamedata.LayerSurface, 0, false, true, 10},
	{"Fish bowl", 1, 1, gamedata.LayerSurface, 0, false, true, 120},
	{"Mini fridge", 1, 1, gamedata.LayerFloor, 1, true, false, 180},
	{"TV stand", 2, 1, gamedata.LayerFloor, 2, true, false, 160},
	{"Small TV", 1, 1, gamedata.LayerSurface, 0, false, true, 300},
	{"Wall clock", 1, 1, gamedata.LayerWall, 0, false, false, 50},
	{"Poster", 1, 1, gamedata.LayerWall, 0, false, false, 25},
	{"Window", 2, 1, gamedata.LayerWall, 0, false, false, 100},
	{"Trophy shelf", 2, 1, gamedata.LayerWall, 0, false, false, 140},
	{"Mounted coyote pelt", 2, 1, gamedata.LayerWall, 0, false, false, 0},
}

var wallpaperNames = []string{"White wallpaper", "Cream wallpaper", "Sky blue wallpaper", "Sage green wallpaper", "Brick stripes wallpaper", "Blue check wallpaper"}
var floorNames = []string{"Pale oak boards floor", "Dark boards floor", "Grey tile floor", "Checker tile floor", "Green carpet floor", "Sand carpet floor"}

func itemByName(t *testing.T, d *gamedata.Data, name string) gamedata.Item {
	t.Helper()
	for _, it := range d.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no item named %q", name)
	return gamedata.Item{}
}

// TestFurnitureMatchesSpec checks AC1: the 30 furniture rows, 6 wallpapers
// and 6 floors, none stackable.
func TestFurnitureMatchesSpec(t *testing.T) {
	d := load(t)
	if len(furnitureTable) != 30 {
		t.Fatalf("table has %d rows, want 30", len(furnitureTable))
	}
	for _, r := range furnitureTable {
		t.Run(r.name, func(t *testing.T) {
			it := itemByName(t, d, r.name)
			if it.Kind != gamedata.KindFurniture || it.Furniture == nil {
				t.Fatalf("kind = %q, furniture = %v", it.Kind, it.Furniture)
			}
			want := gamedata.Furniture{Width: r.w, Depth: r.d, Layer: r.layer, SurfaceSlots: r.slots, OnFloor: r.floor, OnSurface: r.surf}
			if *it.Furniture != want {
				t.Errorf("furniture = %+v, want %+v", *it.Furniture, want)
			}
			if it.Stackable() {
				t.Error("furniture must not stack")
			}
		})
	}
	for kind, names := range map[gamedata.ItemKind][]string{gamedata.KindWallpaper: wallpaperNames, gamedata.KindFloorCovering: floorNames} {
		count := 0
		for _, it := range d.Items {
			if it.Kind == kind {
				count++
				if it.Stackable() {
					t.Errorf("%s must not stack", it.Name)
				}
			}
		}
		if count != len(names) {
			t.Errorf("%s items = %d, want %d", kind, count, len(names))
		}
		for _, n := range names {
			if it := itemByName(t, d, n); it.Kind != kind {
				t.Errorf("%s kind = %q, want %q", n, it.Kind, kind)
			}
		}
	}
}

// TestThriftStock checks AC2.
func TestThriftStock(t *testing.T) {
	d := load(t)
	sold := 0
	check := func(name string, price int) {
		t.Helper()
		it := itemByName(t, d, name)
		got, ok := d.ThriftSellPrice(it.ID)
		if price == 0 {
			if ok {
				t.Errorf("%s is sold for %d, want not sold", name, got)
			}
			if _, ok := d.ThriftBuyPrice(it.ID); ok {
				t.Errorf("%s is bought back, want not", name)
			}
			return
		}
		sold++
		if !ok || got != price {
			t.Errorf("%s sells for %d, %v; want %d", name, got, ok, price)
		}
		if buy, ok := d.ThriftBuyPrice(it.ID); !ok || buy != price/3 {
			t.Errorf("%s buys back for %d, %v; want %d", name, buy, ok, price/3)
		}
	}
	for _, r := range furnitureTable {
		check(r.name, r.thrift)
	}
	if sold != 29 {
		t.Errorf("%d furniture items sold, want 29", sold)
	}
	for _, n := range append(append([]string{}, wallpaperNames...), floorNames...) {
		check(n, 50)
	}
	if len(d.Thrift.Sells) != 41 {
		t.Errorf("thrift stock has %d entries, want 41", len(d.Thrift.Sells))
	}
}

// TestWorkbenchRecipes checks AC3.
func TestWorkbenchRecipes(t *testing.T) {
	d := load(t)
	want := []struct {
		out, in   string
		qty, gold int
	}{
		{"Wooden chair", "Logs", 4, 10},
		{"Stool", "Logs", 2, 5},
		{"Wooden table", "Logs", 8, 20},
		{"Oak dining table", "Oak logs", 6, 60},
		{"Desk", "Oak logs", 4, 30},
		{"Bookshelf", "Oak logs", 5, 30},
		{"Single bed", "Logs", 10, 50},
		{"Double bed", "Oak logs", 8, 100},
		{"Eucalyptus wardrobe", "Eucalyptus logs", 6, 150},
		{"Eucalyptus cabinet", "Eucalyptus logs", 4, 100},
		{"TV stand", "Logs", 6, 40},
		{"Trophy shelf", "Oak logs", 3, 30},
		{"Mounted coyote pelt", "Coyote pelt", 3, 50},
	}
	if len(d.Workbench) != len(want) {
		t.Fatalf("%d recipes, want %d", len(d.Workbench), len(want))
	}
	for i, w := range want {
		r := d.Workbench[i]
		out, in := itemByName(t, d, w.out), itemByName(t, d, w.in)
		if r.Output != out.ID || r.Gold != w.gold || len(r.Inputs) != 1 || r.Inputs[0].Item != in.ID || r.Inputs[0].Quantity != w.qty {
			t.Errorf("recipe %d = %+v, want %d %s + %d gold for %s", i, r, w.qty, w.in, w.gold, w.out)
		}
		if _, ok := d.Item(r.Output); !ok {
			t.Errorf("recipe %d output %d missing", i, r.Output)
		}
	}
}

// TestLoadRejectsBadHousingData checks AC4.
func TestLoadRejectsBadHousingData(t *testing.T) {
	tests := []struct {
		name, file, old, new, wantErr string
	}{
		{"unknown layer", gamedata.ItemsFile, `"layer": "wall"`, `"layer": "ceiling"`, `unknown furniture layer "ceiling"`},
		{"zero footprint", gamedata.ItemsFile, `"width": 1, "depth": 1`, `"width": 0, "depth": 1`, `footprint 0x1 must be at least 1x1`},
		{"recipe with unknown input", gamedata.WorkbenchFile, `"item": "logs"`, `"item": "planks"`, `unknown item "planks"`},
		{"recipe with unknown output", gamedata.WorkbenchFile, `"output": "stool"`, `"output": "ottoman"`, `unknown item "ottoman"`},
		{"thrift entry for unknown item", gamedata.ThriftFile, `"item": "stool"`, `"item": "ottoman"`, `unknown item "ottoman"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gamedata.Load(fixture(t, tt.file, tt.old, tt.new))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
