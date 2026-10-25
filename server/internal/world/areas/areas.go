// Package areas says which gameplay area a world position is in: the safe
// circle around spawn, the two PvE areas, the PvP zone, or none.
//
// It parses an embedded copy of shared/map/areas.json (see
// shared/map/README.md and docs/specs/map.md, Gameplay areas). Positions are
// world metres, X east and Y south (the map's z). Lookups are pure, allocate
// nothing and are safe for concurrent use.
package areas

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"

	"github.com/cleiderg/lush/server/internal/world"
)

// The embedded copy must match shared/map/areas.json; refresh it from the repo
// root with: cp shared/map/areas.json server/internal/world/areas/areas.json
//
//go:embed areas.json
var embedded []byte

// Area identifies a gameplay area.
type Area uint8

const (
	None        Area = iota // outside every area
	SafeCircle              // within safeRadius of spawn; no combat
	AldrichPark             // Aldrich Park PvE, outside the safe circle
	SouthFields             // South fields PvE
	PvPZone                 // the only place players can attack each other
)

var areaNames = [...]string{"none", "safe_circle", "aldrich_park", "south_fields", "pvp_zone"}

func (a Area) String() string {
	if int(a) < len(areaNames) {
		return areaNames[a]
	}
	return fmt.Sprintf("Area(%d)", uint8(a))
}

// PvP reports whether players can attack each other in a.
func (a Area) PvP() bool { return a == PvPZone }

// PvE reports whether a is one of the PvE areas.
func (a Area) PvE() bool { return a == AldrichPark || a == SouthFields }

// polygon ids in areas.json and their kinds.
var fileAreas = map[string]struct {
	area Area
	kind string
}{
	"aldrich_park": {AldrichPark, "pve"},
	"south_fields": {SouthFields, "pve"},
	"pvp_zone":     {PvPZone, "pvp"},
}

type polygon struct {
	area                   Area
	minX, minY, maxX, maxY float64
	xs, ys                 []float64 // metres
}

// Lookup holds the parsed areas. It is immutable after Parse.
type Lookup struct {
	spawn      world.Vec2
	shop       world.Vec2
	safeRadius float64 // metres
	polys      []polygon
}

type fileVec struct {
	X *int `json:"x"`
	Z *int `json:"z"`
}

type fileFormat struct {
	Version    int      `json:"version"`
	Units      string   `json:"units"`
	Spawn      *fileVec `json:"spawn"`
	SafeRadius int      `json:"safeRadius"`
	Shop       *fileVec `json:"shop"`
	Areas      []struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		P    []int  `json:"p"`
	} `json:"areas"`
}

func (v *fileVec) vec(name string) (world.Vec2, error) {
	if v == nil || v.X == nil || v.Z == nil {
		return world.Vec2{}, fmt.Errorf("areas: missing %s", name)
	}
	return world.Vec2{X: float32(*v.X) / 10, Y: float32(*v.Z) / 10}, nil
}

// Load parses the embedded copy of shared/map/areas.json.
func Load() (*Lookup, error) { return Parse(embedded) }

// Parse decodes the areas.json format documented in shared/map/README.md.
func Parse(data []byte) (*Lookup, error) {
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("areas: %w", err)
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("areas: unsupported version %d", f.Version)
	}
	if f.Units != "decimetres" {
		return nil, fmt.Errorf("areas: unsupported units %q", f.Units)
	}
	if f.SafeRadius <= 0 {
		return nil, fmt.Errorf("areas: safeRadius %d must be positive", f.SafeRadius)
	}
	l := &Lookup{safeRadius: float64(f.SafeRadius) / 10}
	var err error
	if l.spawn, err = f.Spawn.vec("spawn"); err != nil {
		return nil, err
	}
	if l.shop, err = f.Shop.vec("shop"); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, a := range f.Areas {
		def, ok := fileAreas[a.ID]
		if !ok {
			return nil, fmt.Errorf("areas: unknown area id %q", a.ID)
		}
		if seen[a.ID] {
			return nil, fmt.Errorf("areas: duplicate area id %q", a.ID)
		}
		seen[a.ID] = true
		if a.Kind != def.kind {
			return nil, fmt.Errorf("areas: %s has kind %q, want %q", a.ID, a.Kind, def.kind)
		}
		if len(a.P) < 6 || len(a.P)%2 != 0 {
			return nil, fmt.Errorf("areas: %s polygon has %d coordinates, need an even number >= 6", a.ID, len(a.P))
		}
		p := polygon{area: def.area, minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
		for i := 0; i < len(a.P); i += 2 {
			x, y := float64(a.P[i])/10, float64(a.P[i+1])/10
			p.xs = append(p.xs, x)
			p.ys = append(p.ys, y)
			p.minX, p.maxX = math.Min(p.minX, x), math.Max(p.maxX, x)
			p.minY, p.maxY = math.Min(p.minY, y), math.Max(p.maxY, y)
		}
		l.polys = append(l.polys, p)
	}
	for id := range fileAreas {
		if !seen[id] {
			return nil, fmt.Errorf("areas: missing area %q", id)
		}
	}
	return l, nil
}

// Spawn returns the safe spawn point in metres.
func (l *Lookup) Spawn() world.Vec2 { return l.spawn }

// Shop returns the NPC shop position in metres.
func (l *Lookup) Shop() world.Vec2 { return l.shop }

// SafeRadius returns the safe circle's radius in metres.
func (l *Lookup) SafeRadius() float64 { return l.safeRadius }

// InSafeCircle reports whether p is within the safe radius of spawn
// (the boundary counts as inside).
func (l *Lookup) InSafeCircle(p world.Vec2) bool {
	dx := float64(p.X) - float64(l.spawn.X)
	dy := float64(p.Y) - float64(l.spawn.Y)
	return dx*dx+dy*dy <= l.safeRadius*l.safeRadius
}

// InPvP reports whether p is in the PvP zone.
func (l *Lookup) InPvP(p world.Vec2) bool { return l.AreaAt(p) == PvPZone }

// AreaAt returns the area containing p. The safe circle takes precedence over
// the Aldrich Park polygon around it. Non-finite positions are in None.
func (l *Lookup) AreaAt(p world.Vec2) Area {
	if l.InSafeCircle(p) {
		return SafeCircle
	}
	x, y := float64(p.X), float64(p.Y)
	for i := range l.polys {
		if l.polys[i].contains(x, y) {
			return l.polys[i].area
		}
	}
	return None
}

// contains is an even-odd ray-crossing test. NaN coordinates fail the
// bounding-box check and are outside.
func (p *polygon) contains(x, y float64) bool {
	if !(x >= p.minX && x <= p.maxX && y >= p.minY && y <= p.maxY) {
		return false
	}
	in := false
	n := len(p.xs)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi, xj, yj := p.xs[i], p.ys[i], p.xs[j], p.ys[j]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}
