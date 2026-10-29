package cardroom

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

const sharedDir = "../../../../shared/map/"

// TestEmbeddedPositionsMatchShared fails when the embedded copy drifts from
// the shared map's card table file.
func TestEmbeddedPositionsMatchShared(t *testing.T) {
	shared, err := os.ReadFile(sharedDir + "cardtables.json")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := os.ReadFile("cardtables.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embedded, shared) {
		t.Fatal("server/internal/systems/cardroom/cardtables.json differs from shared/map/cardtables.json; " +
			"refresh it from the repo root with: cp shared/map/cardtables.json server/internal/systems/cardroom/cardtables.json")
	}
}

// TestTileSizeMatchesCollisionGrid checks TileSize against the shared
// collision grid's cell size.
func TestTileSizeMatchesCollisionGrid(t *testing.T) {
	g, err := world.LoadGrid(sharedDir + "collision.bin")
	if err != nil {
		t.Fatal(err)
	}
	if g.CellSize() != TileSize {
		t.Fatalf("collision cell size %v m, TileSize %v m", g.CellSize(), TileSize)
	}
}

// TestEveryTableHasOnePosition checks every table id in the card table data
// file has exactly one position in the embedded file, and the reverse.
func TestEveryTableHasOnePosition(t *testing.T) {
	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	positions, err := Positions()
	if err != nil {
		t.Fatal(err)
	}
	// ParsePositions rejects duplicate ids, so each id has at most one.
	tables := map[TableID]bool{}
	for _, c := range data.CardTables {
		tables[TableID(c.ID)] = true
		if _, ok := positions[TableID(c.ID)]; !ok {
			t.Errorf("card table %d has no position", c.ID)
		}
	}
	for id := range positions {
		if !tables[id] {
			t.Errorf("position for table %d, which is not in %s", id, gamedata.CardTablesFile)
		}
	}
}

func TestParsePositions(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    map[TableID]world.Vec2
		wantErr string
	}{
		{"ok", `{"version":1,"units":"decimetres","tables":[{"id":1,"x":-245,"z":-585},{"id":2,"x":10,"z":0}]}`,
			map[TableID]world.Vec2{1: {X: -24.5, Y: -58.5}, 2: {X: 1}}, ""},
		{"bad json", `{`, nil, "table positions"},
		{"version", `{"version":2,"units":"decimetres","tables":[]}`, nil, "version 2"},
		{"units", `{"version":1,"units":"metres","tables":[]}`, nil, "units"},
		{"zero id", `{"version":1,"units":"decimetres","tables":[{"id":0,"x":1,"z":1}]}`, nil, "non-zero id"},
		{"missing z", `{"version":1,"units":"decimetres","tables":[{"id":1,"x":1}]}`, nil, "non-zero id, x and z"},
		{"duplicate", `{"version":1,"units":"decimetres","tables":[{"id":1,"x":1,"z":1},{"id":1,"x":2,"z":2}]}`,
			nil, "duplicate table id 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePositions([]byte(tt.data))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for id, w := range tt.want {
				if got[id] != w {
					t.Errorf("table %d at %v, want %v", id, got[id], w)
				}
			}
		})
	}
}

// TestSitNeedsReach checks a sit within Reach of the table's centre succeeds
// and one outside it is rejected with TooFar, leaving the table unchanged.
func TestSitNeedsReach(t *testing.T) {
	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	const id = TableID(1)
	tests := []struct {
		name   string
		offset world.Vec2 // from the table's centre
		want   Result
	}{
		{"at centre", world.Vec2{}, OK},
		{"just inside east", world.Vec2{X: ReachTiles*TileSize - 0.05}, OK},
		{"exactly at reach south", world.Vec2{Y: ReachTiles * TileSize}, OK},
		{"inside diagonal", world.Vec2{X: 1.4, Y: -1.4}, OK},
		{"just outside west", world.Vec2{X: -(ReachTiles*TileSize + 0.05)}, TooFar},
		{"outside diagonal", world.Vec2{X: 1.5, Y: 1.5}, TooFar},
		{"next table", world.Vec2{X: 6}, TooFar},
		{"far away", world.Vec2{X: 500, Y: 500}, TooFar},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &fakeSink{}
			sys, err := FromData(data, world.New(world.DefaultConfig()).TickInterval(), sink, &fakeCommitter{})
			if err != nil {
				t.Fatal(err)
			}
			centre, ok := sys.Position(id)
			if !ok {
				t.Fatalf("no position for table %d", id)
			}
			tb, _ := sys.Table(id)
			pos := world.Vec2{X: centre.X + tt.offset.X, Y: centre.Y + tt.offset.Y}
			sys.Handle(SitCommand{Player: 7, TableID: id}, &world.Player{ID: 7, Pos: pos, Inv: world.Inventory{Gold: roomGold}})
			if len(sink.sits) != 1 || sink.sits[0].r != tt.want {
				t.Fatalf("sit results %+v, want one %v", sink.sits, tt.want)
			}
			_, seated := tb.SeatOf(7)
			if tt.want == OK {
				if !seated {
					t.Fatal("player not seated after OK")
				}
				return
			}
			if seated {
				t.Fatal("player seated after TooFar")
			}
			for i := 0; i < tb.Seats(); i++ {
				if _, occ := tb.Occupant(cardtable.Seat(i)); occ {
					t.Errorf("seat %d occupied after TooFar", i)
				}
			}
			if len(sink.changes) != 0 {
				t.Errorf("%d table changes published after TooFar, want 0", len(sink.changes))
			}
		})
	}
}
