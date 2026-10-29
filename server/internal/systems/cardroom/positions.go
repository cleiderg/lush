package cardroom

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/cleiderg/lush/server/internal/world"
)

// The embedded copy must match shared/map/cardtables.json; refresh it from the
// repo root with:
// cp shared/map/cardtables.json server/internal/systems/cardroom/cardtables.json
//
//go:embed cardtables.json
var embeddedPositions []byte

// TileSize is the length of a tile in metres: the collision cell size
// (shared/map/config.mjs CELL_SIZE). A test checks it against the shared
// collision grid.
const TileSize = 1.0

// ReachTiles is how close, in tiles, a player must be to a table's centre to
// sit at it (docs/specs/gameplay.md, Interact button).
const ReachTiles = 2

// Reach is ReachTiles in metres.
const Reach = ReachTiles * TileSize

type positionsFile struct {
	Version int    `json:"version"`
	Units   string `json:"units"`
	Tables  []struct {
		ID *uint32 `json:"id"`
		X  *int    `json:"x"`
		Z  *int    `json:"z"`
	} `json:"tables"`
}

// Positions parses the embedded copy of shared/map/cardtables.json: each
// table's centre in world metres, X east and Y south (the map's z).
func Positions() (map[TableID]world.Vec2, error) { return ParsePositions(embeddedPositions) }

// ParsePositions decodes the cardtables.json format documented in
// shared/map/README.md.
func ParsePositions(data []byte) (map[TableID]world.Vec2, error) {
	var f positionsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("cardroom: table positions: %w", err)
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("cardroom: table positions: unsupported version %d", f.Version)
	}
	if f.Units != "decimetres" {
		return nil, fmt.Errorf("cardroom: table positions: unsupported units %q", f.Units)
	}
	out := make(map[TableID]world.Vec2, len(f.Tables))
	for i, t := range f.Tables {
		if t.ID == nil || *t.ID == 0 || t.X == nil || t.Z == nil {
			return nil, fmt.Errorf("cardroom: table positions: entry %d needs a non-zero id, x and z", i)
		}
		id := TableID(*t.ID)
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("cardroom: table positions: duplicate table id %d", id)
		}
		out[id] = world.Vec2{X: float32(*t.X) / 10, Y: float32(*t.Z) / 10}
	}
	return out, nil
}

// inReach reports whether pos is within Reach of the table centre at.
func inReach(pos, at world.Vec2) bool {
	dx, dy := float64(pos.X-at.X), float64(pos.Y-at.Y)
	return dx*dx+dy*dy <= Reach*Reach
}
