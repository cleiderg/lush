// Package spawner keeps each NPC type's population alive in its gameplay
// area (docs/specs/gameplay.md, NPCs; docs/specs/map.md, Gameplay areas).
//
// On its first Tick it places every type's population on random walkable
// cells inside the type's area, which never includes the safe circle. When an
// NPC it placed leaves the world (combat removes NPCs that are killed), it
// places a replacement once the type's respawn time has passed, as long as
// fewer than the type's max_alive NPCs of that type are alive.
package spawner

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// The embedded copy must match shared/map/spawnareas.json; refresh it from the
// repo root with: cp shared/map/spawnareas.json server/internal/systems/spawner/spawnareas.json
//
//go:embed spawnareas.json
var embeddedZones []byte

// zonePoly is one spawn zone: a polygon in world metres.
type zonePoly struct{ xs, ys []float64 }

func (z zonePoly) contains(p world.Vec2) bool {
	in := false
	x, y := float64(p.X), float64(p.Y)
	for i, j := 0, len(z.xs)-1; i < len(z.xs); j, i = i, i+1 {
		if (z.ys[i] > y) != (z.ys[j] > y) && x < (z.xs[j]-z.xs[i])*(y-z.ys[i])/(z.ys[j]-z.ys[i])+z.xs[i] {
			in = !in
		}
	}
	return in
}

// loadZones parses spawnareas.json (decimetres, x east, z south).
func loadZones() (map[string]zonePoly, error) {
	var f struct {
		Areas []struct {
			ID string `json:"id"`
			P  []int  `json:"p"`
		} `json:"areas"`
	}
	if err := json.Unmarshal(embeddedZones, &f); err != nil {
		return nil, fmt.Errorf("spawner: spawnareas.json: %w", err)
	}
	out := map[string]zonePoly{}
	for _, a := range f.Areas {
		if len(a.P) < 6 || len(a.P)%2 != 0 {
			return nil, fmt.Errorf("spawner: spawn area %q: bad polygon", a.ID)
		}
		var z zonePoly
		for i := 0; i < len(a.P); i += 2 {
			z.xs = append(z.xs, float64(a.P[i])/10)
			z.ys = append(z.ys, float64(a.P[i+1])/10)
		}
		out[a.ID] = z
	}
	return out, nil
}

// Rand picks spawn cells. *math/rand/v2.Rand satisfies it.
type Rand interface {
	IntN(n int) int // uniform in [0, n)
}

// Config holds the spawner's dependencies. All are required.
type Config struct {
	Data      *gamedata.Data
	Areas     *areas.Lookup
	Collision *world.Grid
	Clock     world.Clock
	Rand      Rand
}

// gameArea maps the NPC table's area names to areas.Area.
var gameArea = map[gamedata.Area]areas.Area{
	gamedata.AreaAldrichPark: areas.AldrichPark,
	gamedata.AreaSouthFields: areas.SouthFields,
}

// kind is the spawner's state for one NPC type.
type kind struct {
	typ     gamedata.NPCType
	cells   []world.Vec2  // walkable cell centres in the type's area
	limit   int           // most alive at once: population, capped by max_alive
	alive   []world.NPCID // NPCs this spawner placed that are still in the world
	pending []time.Time   // when each missing NPC may respawn, oldest first
	count   int           // NPCs of this type in the world, refreshed each Tick
}

// System is the NPC spawner. Register it every tick, after the combat
// system, so a kill starts its respawn timer on the tick it happens.
type System struct {
	cfg     Config
	kinds   []*kind
	byType  map[world.NPCTypeID]*kind
	started bool
}

// New creates the spawner and finds the walkable cells of every area an NPC
// type with a population spawns in. It fails if such an area has none.
func New(cfg Config) (*System, error) {
	if cfg.Data == nil || cfg.Areas == nil || cfg.Collision == nil || cfg.Clock == nil || cfg.Rand == nil {
		return nil, errors.New("spawner: nil dependency")
	}
	cells := map[areas.Area][]world.Vec2{}
	for _, a := range gameArea {
		cells[a] = nil
	}
	w, h := cfg.Collision.Size()
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			if cfg.Collision.CellBlocked(col, row) {
				continue
			}
			p := cfg.Collision.CellCenter(col, row)
			a := cfg.Areas.AreaAt(p) // the safe circle is never a PvE area
			if _, ok := cells[a]; ok {
				cells[a] = append(cells[a], p)
			}
		}
	}
	zones, err := loadZones()
	if err != nil {
		return nil, err
	}
	s := &System{cfg: cfg, byType: map[world.NPCTypeID]*kind{}}
	for _, t := range cfg.Data.NPCs {
		a, ok := gameArea[t.Area]
		if !ok {
			return nil, fmt.Errorf("spawner: npc %q: unknown area %q", t.Key, t.Area)
		}
		limit := t.Population
		if t.MaxAlive > 0 && limit > t.MaxAlive {
			limit = t.MaxAlive
		}
		kc := cells[a]
		if t.SpawnZone != "" {
			z, ok := zones[t.SpawnZone]
			if !ok {
				return nil, fmt.Errorf("spawner: npc %q: unknown spawn zone %q", t.Key, t.SpawnZone)
			}
			kc = nil
			for _, p := range cells[a] {
				if z.contains(p) {
					kc = append(kc, p)
				}
			}
		}
		if limit > 0 && len(kc) == 0 {
			return nil, fmt.Errorf("spawner: npc %q: no walkable cell in area %s", t.Key, a)
		}
		k := &kind{typ: t, cells: kc, limit: limit}
		s.kinds = append(s.kinds, k)
		s.byType[world.NPCTypeID(t.ID)] = k
	}
	return s, nil
}

func (s *System) Name() string { return "spawner" }

// Handle does nothing: players don't command the spawner.
func (s *System) Handle(world.Command, *world.Player) {}

// Tick places the initial population on the first call. After that it
// starts a respawn timer for every placed NPC that has left the world, and
// places a replacement for every timer that has run out, while fewer than
// the type's limit are alive. A replacement that can't be placed now (the
// type is at its limit, or the NPC id range is full) waits for a later tick.
func (s *System) Tick(w *world.World, _ time.Duration) {
	now := s.cfg.Clock.Now()
	s.countAlive(w)
	for _, k := range s.kinds {
		if !s.started {
			for range k.limit {
				k.pending = append(k.pending, now)
			}
		}
		kept := k.alive[:0]
		for _, id := range k.alive {
			if w.NPC(id) != nil {
				kept = append(kept, id)
			} else {
				k.pending = append(k.pending, now.Add(k.typ.Respawn()))
			}
		}
		k.alive = kept
		for len(k.pending) > 0 && !k.pending[0].After(now) && k.count < k.limit {
			if !s.spawn(w, k) {
				break
			}
			k.pending = k.pending[1:]
		}
	}
	s.started = true
}

// countAlive counts every NPC in the world by type, including ones the
// spawner didn't place, so max_alive holds for the whole world.
func (s *System) countAlive(w *world.World) {
	for _, k := range s.kinds {
		k.count = 0
	}
	for _, n := range w.NPCs() {
		if k, ok := s.byType[n.Type]; ok {
			k.count++
		}
	}
}

// spawn places one NPC of kind k on a random walkable cell of its area.
func (s *System) spawn(w *world.World, k *kind) bool {
	p := k.cells[s.cfg.Rand.IntN(len(k.cells))]
	n := &world.NPC{Type: world.NPCTypeID(k.typ.ID), Pos: p, Home: p, HP: k.typ.HP, Anim: world.AnimIdle}
	id, err := w.AddNPC(n)
	if err != nil {
		return false
	}
	k.alive = append(k.alive, id)
	k.count++
	return true
}
