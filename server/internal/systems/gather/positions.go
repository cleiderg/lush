package gather

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/cleiderg/lush/server/internal/world"
)

// The embedded copy must match shared/map/skills.json; refresh it from the
// repo root with:
// cp shared/map/skills.json server/internal/systems/gather/skills.json
//
//go:embed skills.json
var embeddedMap []byte

// TileSize is the length of a tile in metres: the collision cell size
// (shared/map/config.mjs CELL_SIZE). A test checks it against the shared
// collision grid.
const TileSize = 1.0

// Place is a tree or one candidate fishing spot.
type Place struct {
	ID   string
	Kind string // tree kind (tree, oak, eucalyptus) or spot kind (net, rod)
	Pos  world.Vec2
}

// Layout is the gathering part of shared/map/skills.json.
type Layout struct {
	Trees []Place
	Spots []Place // candidate positions, in file order
	// Active is how many spots of each kind are out at once.
	Active map[string]int
	// Grill is where the cooking range is.
	Grill world.Vec2
}

type placeJSON struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	X    *int   `json:"x"`
	Z    *int   `json:"z"`
}

type layoutFile struct {
	Version int `json:"version"`
	Grill   *struct {
		X *int `json:"x"`
		Z *int `json:"z"`
	} `json:"grill"`
	Units string      `json:"units"`
	Trees []placeJSON `json:"trees"`
	Pond  struct {
		Active map[string]int `json:"active"`
		Spots  []placeJSON    `json:"spots"`
	} `json:"pond"`
}

// DefaultLayout parses the embedded copy of shared/map/skills.json.
func DefaultLayout() (Layout, error) { return ParseLayout(embeddedMap) }

// ParseLayout decodes the skills.json format documented in
// shared/map/README.md. Positions are decimetres in the file and metres in
// the result, X east and Y south (the map's z).
func ParseLayout(data []byte) (Layout, error) {
	var f layoutFile
	if err := json.Unmarshal(data, &f); err != nil {
		return Layout{}, fmt.Errorf("gather: skills map: %w", err)
	}
	if f.Version != 1 {
		return Layout{}, fmt.Errorf("gather: skills map: unsupported version %d", f.Version)
	}
	if f.Units != "decimetres" {
		return Layout{}, fmt.Errorf("gather: skills map: unsupported units %q", f.Units)
	}
	seen := map[string]bool{}
	conv := func(in []placeJSON, what string) ([]Place, error) {
		out := make([]Place, 0, len(in))
		for i, p := range in {
			if p.ID == "" || p.Kind == "" || p.X == nil || p.Z == nil {
				return nil, fmt.Errorf("gather: skills map: %s %d needs id, kind, x and z", what, i)
			}
			if seen[p.ID] {
				return nil, fmt.Errorf("gather: skills map: duplicate id %q", p.ID)
			}
			seen[p.ID] = true
			out = append(out, Place{ID: p.ID, Kind: p.Kind, Pos: world.Vec2{X: float32(*p.X) / 10, Y: float32(*p.Z) / 10}})
		}
		return out, nil
	}
	var l Layout
	var err error
	if l.Trees, err = conv(f.Trees, "tree"); err != nil {
		return Layout{}, err
	}
	if l.Spots, err = conv(f.Pond.Spots, "spot"); err != nil {
		return Layout{}, err
	}
	l.Active = f.Pond.Active
	if f.Grill != nil && f.Grill.X != nil && f.Grill.Z != nil {
		l.Grill = world.Vec2{X: float32(*f.Grill.X) / 10, Y: float32(*f.Grill.Z) / 10}
	}
	return l, nil
}
