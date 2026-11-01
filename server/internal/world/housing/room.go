// Package housing holds the rules for one home room's grid: where floor items,
// rugs, surface items and wall items may go. It is pure game logic. It checks
// geometry only; ownership, inventory and storage are handled elsewhere.
//
// Rules follow docs/specs/housing.md (D-052). Rotations are 90° steps, and
// 90° or 270° swaps an item's width and depth.
package housing

import "sort"

// DefaultSize is the room edge length in tiles for the first release.
const DefaultSize = 8

// Layer is a bit set of the places an item may go.
type Layer uint8

const (
	// LayerFloor items occupy floor tiles and may not overlap each other.
	LayerFloor Layer = 1 << iota
	// LayerRug items lie under floor items and may not overlap other rugs.
	LayerRug
	// LayerSurface items sit in a slot of a floor item that has surface slots.
	LayerSurface
	// LayerWall items take slots along one wall.
	LayerWall
)

// ItemDef is what the room needs to know about an item. Callers adapt their
// own item definitions to it; this package knows nothing about gamedata.
type ItemDef interface {
	// Footprint is the width and depth in tiles at rotation 0. For wall items
	// only the width is used.
	Footprint() (w, d int)
	// Layers is the set of layers the item can be placed on.
	Layers() Layer
	// SurfaceSlots is how many small items can sit on top of it (0 for none).
	SurfaceSlots() int
}

// Rotation is an item's turn in degrees: 0, 90, 180 or 270.
type Rotation int

// Valid reports whether r is a multiple of 90 in [0, 360).
func (r Rotation) Valid() bool { return r >= 0 && r < 360 && r%90 == 0 }

// Side is a room edge for wall items.
type Side uint8

// Walls of the room. Pos along a wall runs west to east on North and South,
// and north to south on West and East.
const (
	North Side = iota
	East
	South
	West
)

// ID identifies a placed item. IDs must be non-zero.
type ID uint64

// Placement says where an item goes. Which fields matter depends on the item:
// with Parent set it goes on that item's surface (Slot, or -1 for the first
// free slot); otherwise floor items and rugs use X, Y and Rot, and wall items
// use Side and Pos.
type Placement struct {
	X, Y   int
	Rot    Rotation
	Parent ID
	Slot   int
	Side   Side
	Pos    int
}

// Result is the outcome of a room operation.
type Result uint8

// Results. OK is the only success.
const (
	OK Result = iota
	OutOfBounds
	Overlap
	BadRotation
	BadLayer       // the item can't go where the placement asks
	NotSurfaceItem // item can't sit on a surface
	NoSurface      // parent is missing or has no surface slots
	SurfaceFull    // no free slot (or the requested slot is taken)
	HasItems       // surface item still holds items; remove them first
	NotFound
	Exists
)

var resultNames = [...]string{"ok", "out_of_bounds", "overlap", "bad_rotation", "bad_layer",
	"not_surface_item", "no_surface", "surface_full", "has_items", "not_found", "exists"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return "unknown"
}

// Item is an item placed in the room.
type Item struct {
	ID  ID
	Def ItemDef
	Placement
	kind Layer // layer it is placed on: floor, rug, surface or wall
}

// Layer is the layer the item is placed on.
func (it Item) Layer() Layer { return it.kind }

// Room is one home room's grid. It is not safe for concurrent use.
type Room struct {
	size  int
	items map[ID]*Item
}

// New returns an empty size×size room.
func New(size int) *Room {
	return &Room{size: size, items: make(map[ID]*Item)}
}

// Size is the room edge length in tiles.
func (r *Room) Size() int { return r.size }

// Get returns the placed item with the given id.
func (r *Room) Get(id ID) (Item, bool) {
	it, ok := r.items[id]
	if !ok {
		return Item{}, false
	}
	return *it, true
}

// Items returns all placed items ordered by ID.
func (r *Room) Items() []Item {
	out := make([]Item, 0, len(r.items))
	for _, it := range r.items {
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Size returns the width and depth of def at rotation rot.
func Size(def ItemDef, rot Rotation) (w, d int) {
	w, d = def.Footprint()
	if rot == 90 || rot == 270 {
		w, d = d, w
	}
	return w, d
}

// Place adds def to the room as id.
func (r *Room) Place(id ID, def ItemDef, p Placement) Result {
	if id == 0 || def == nil {
		return NotFound
	}
	if _, ok := r.items[id]; ok {
		return Exists
	}
	it := &Item{ID: id, Def: def}
	if res := r.resolve(it, p); res != OK {
		return res
	}
	r.items[id] = it
	return OK
}

// Move relocates an item (and may rotate it). It fails with HasItems if the
// item still holds items on its surface. On any failure the room is unchanged.
func (r *Room) Move(id ID, p Placement) Result {
	old, ok := r.items[id]
	if !ok {
		return NotFound
	}
	if r.holds(id) {
		return HasItems
	}
	cand := &Item{ID: id, Def: old.Def}
	if res := r.resolve(cand, p); res != OK {
		return res
	}
	*old = *cand
	return OK
}

// Rotate turns an item in place to rot. Surface items keep their slot, wall
// items can't rotate, and a surface item that holds items stays attached.
func (r *Room) Rotate(id ID, rot Rotation) Result {
	old, ok := r.items[id]
	if !ok {
		return NotFound
	}
	if !rot.Valid() {
		return BadRotation
	}
	if old.kind == LayerWall {
		return BadLayer
	}
	if old.kind == LayerSurface {
		old.Rot = rot
		return OK
	}
	p := old.Placement
	p.Rot = rot
	cand := &Item{ID: id, Def: old.Def}
	if res := r.resolve(cand, p); res != OK {
		return res
	}
	*old = *cand
	return OK
}

// Remove takes an item out of the room. It fails with HasItems if the item
// still holds items on its surface, so nothing is lost.
func (r *Room) Remove(id ID) Result {
	if _, ok := r.items[id]; !ok {
		return NotFound
	}
	if r.holds(id) {
		return HasItems
	}
	delete(r.items, id)
	return OK
}

// holds reports whether any item sits on id's surface.
func (r *Room) holds(id ID) bool {
	for _, o := range r.items {
		if o.kind == LayerSurface && o.Parent == id {
			return true
		}
	}
	return false
}

// resolve validates p for it against every other item (ignoring it.ID itself)
// and, on success, fills in it's placement and layer. It never touches the
// room, so a failure leaves the room as it was.
func (r *Room) resolve(it *Item, p Placement) Result {
	layers := it.Def.Layers()
	switch {
	case p.Parent != 0:
		if layers&LayerSurface == 0 {
			return NotSurfaceItem
		}
		it.kind = LayerSurface
		return r.resolveSurface(it, p)
	case layers&LayerFloor != 0:
		it.kind = LayerFloor
	case layers&LayerRug != 0:
		it.kind = LayerRug
	case layers&LayerWall != 0:
		it.kind = LayerWall
		return r.resolveWall(it, p)
	default:
		return BadLayer
	}
	if !p.Rot.Valid() {
		return BadRotation
	}
	w, d := Size(it.Def, p.Rot)
	if p.X < 0 || p.Y < 0 || p.X+w > r.size || p.Y+d > r.size {
		return OutOfBounds
	}
	for _, o := range r.items {
		if o.ID == it.ID || o.kind != it.kind {
			continue
		}
		ow, od := Size(o.Def, o.Rot)
		if p.X < o.X+ow && o.X < p.X+w && p.Y < o.Y+od && o.Y < p.Y+d {
			return Overlap
		}
	}
	it.Placement = p
	return OK
}

func (r *Room) resolveSurface(it *Item, p Placement) Result {
	if !p.Rot.Valid() {
		return BadRotation
	}
	parent, ok := r.items[p.Parent]
	if !ok || parent.ID == it.ID || parent.kind != LayerFloor || parent.Def.SurfaceSlots() <= 0 {
		return NoSurface
	}
	n := parent.Def.SurfaceSlots()
	used := make([]bool, n)
	for _, o := range r.items {
		if o.ID != it.ID && o.kind == LayerSurface && o.Parent == parent.ID && o.Slot >= 0 && o.Slot < n {
			used[o.Slot] = true
		}
	}
	switch {
	case p.Slot < 0:
		p.Slot = -1
		for i, u := range used {
			if !u {
				p.Slot = i
				break
			}
		}
		if p.Slot < 0 {
			return SurfaceFull
		}
	case p.Slot >= n:
		return OutOfBounds
	case used[p.Slot]:
		return SurfaceFull
	}
	p.X, p.Y = parent.X, parent.Y
	it.Placement = p
	return OK
}

func (r *Room) resolveWall(it *Item, p Placement) Result {
	if p.Side > West {
		return OutOfBounds
	}
	w, _ := it.Def.Footprint()
	if w < 1 || p.Pos < 0 || p.Pos+w > r.size {
		return OutOfBounds
	}
	for _, o := range r.items {
		if o.ID == it.ID || o.kind != LayerWall || o.Side != p.Side {
			continue
		}
		ow, _ := o.Def.Footprint()
		if p.Pos < o.Pos+ow && o.Pos < p.Pos+w {
			return Overlap
		}
	}
	p.Rot = 0
	it.Placement = p
	return OK
}
