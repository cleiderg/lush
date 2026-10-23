// Package interest decides which entities each player receives each tick.
// It is pure game logic: a uniform spatial grid plus nearest-N selection.
package interest

import (
	"math"

	"github.com/cleiderg/lush/server/internal/world"
)

const (
	// ViewRadius is how far a player sees other entities, in world units.
	ViewRadius float32 = 20
	// ChatRadius is how far chat is heard.
	ChatRadius float32 = 25
	// MaxVisible is the cap on entities sent to one player (nearest first).
	MaxVisible = 100
	// CellSize is the grid cell edge. It is the larger of the view and chat
	// radii, so a 3x3 cell scan around a point covers both.
	CellSize = ChatRadius
)

type cell struct{ x, y int32 }

type entry struct {
	id   world.PlayerID
	pos  world.Vec2
	cell cell
	slot int // index within cells[cell]
}

// Grid is a uniform spatial hash. It is not safe for concurrent use; only the
// tick touches it.
type Grid struct {
	entries map[world.PlayerID]*entry
	cells   map[cell][]*entry
}

// NewGrid returns an empty grid.
func NewGrid() *Grid {
	return &Grid{entries: map[world.PlayerID]*entry{}, cells: map[cell][]*entry{}}
}

func cellOf(p world.Vec2) cell {
	return cell{int32(math.Floor(float64(p.X / CellSize))), int32(math.Floor(float64(p.Y / CellSize)))}
}

// Len is the number of tracked entities.
func (g *Grid) Len() int { return len(g.entries) }

// Set inserts the entity or moves it, re-bucketing only on a cell change.
func (g *Grid) Set(id world.PlayerID, pos world.Vec2) {
	c := cellOf(pos)
	e, ok := g.entries[id]
	if !ok {
		e = &entry{id: id, pos: pos}
		g.entries[id] = e
		g.attach(e, c)
		return
	}
	e.pos = pos
	if e.cell != c {
		g.detach(e)
		g.attach(e, c)
	}
}

// Remove drops the entity; unknown ids are ignored.
func (g *Grid) Remove(id world.PlayerID) {
	if e, ok := g.entries[id]; ok {
		g.detach(e)
		delete(g.entries, id)
	}
}

func (g *Grid) attach(e *entry, c cell) {
	e.cell = c
	e.slot = len(g.cells[c])
	g.cells[c] = append(g.cells[c], e)
}

func (g *Grid) detach(e *entry) {
	s := g.cells[e.cell]
	last := len(s) - 1
	s[e.slot] = s[last]
	s[e.slot].slot = e.slot
	s[last] = nil
	if last == 0 {
		delete(g.cells, e.cell)
		return
	}
	g.cells[e.cell] = s[:last]
}

// Hit is a query result.
type Hit struct {
	ID    world.PlayerID
	Dist2 float32
}

// Query appends to out the entities within radius of center (inclusive),
// excluding self, limited to the max nearest. The result is the exact nearest
// set but unordered; selection is a quickselect, not a full sort. Ties on
// distance break by lower id so results are deterministic. Pass out[:0] to
// reuse a buffer.
func (g *Grid) Query(center world.Vec2, radius float32, max int, self world.PlayerID, out []Hit) []Hit {
	start := len(out)
	r2 := radius * radius
	span := int32(math.Ceil(float64(radius / CellSize)))
	cc := cellOf(center)
	for cy := cc.y - span; cy <= cc.y+span; cy++ {
		for cx := cc.x - span; cx <= cc.x+span; cx++ {
			for _, e := range g.cells[cell{cx, cy}] {
				if e.id == self {
					continue
				}
				dx, dy := e.pos.X-center.X, e.pos.Y-center.Y
				if d2 := dx*dx + dy*dy; d2 <= r2 {
					out = append(out, Hit{e.id, d2})
				}
			}
		}
	}
	if max >= 0 && len(out)-start > max {
		selectNearest(out[start:], max)
		out = out[:start+max]
	}
	return out
}

func less(a, b Hit) bool {
	return a.Dist2 < b.Dist2 || (a.Dist2 == b.Dist2 && a.ID < b.ID)
}

// selectNearest rearranges h so its k smallest elements occupy h[:k]
// (Hoare quickselect, median-of-three pivot).
func selectNearest(h []Hit, k int) {
	lo, hi := 0, len(h)-1
	for lo < hi {
		mid := lo + (hi-lo)/2
		if less(h[mid], h[lo]) {
			h[mid], h[lo] = h[lo], h[mid]
		}
		if less(h[hi], h[lo]) {
			h[hi], h[lo] = h[lo], h[hi]
		}
		if less(h[hi], h[mid]) {
			h[hi], h[mid] = h[mid], h[hi]
		}
		pivot := h[mid]
		i, j := lo, hi
		for i <= j {
			for less(h[i], pivot) {
				i++
			}
			for less(pivot, h[j]) {
				j--
			}
			if i <= j {
				h[i], h[j] = h[j], h[i]
				i++
				j--
			}
		}
		switch {
		case k-1 <= j:
			hi = j
		case k-1 >= i:
			lo = i
		default:
			return
		}
	}
}
