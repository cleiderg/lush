// Package homeroom hosts the 8×8 room of each home on the world tick
// (docs/specs/housing.md, Social and Server): players walk in and out of a
// home (open or locked, at most MaxVisitors inside, owner included), and the
// owner edits the room from the inventory. Placing an item moves it out of the
// owner's inventory into the room, removing moves it back, and setting a
// wallpaper or floor swaps it with the one in place. Each of those is one
// event in the owner's economy outbox (constitution rule 3), so no edit
// creates or destroys an item. Every accepted edit sends the new room state to
// everyone inside. Grid rules live in world/housing; persistence and session
// wiring belong to the caller.
package homeroom

import (
	"errors"
	"fmt"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/housing"
)

// MaxVisitors is how many players fit in a home at once, owner included
// (docs/specs/housing.md, Social).
const MaxVisitors = 10

// Economy audit log reasons, one per kind of accepted edit.
const (
	ReasonPlace     = "home_place"
	ReasonRemove    = "home_remove"
	ReasonWallpaper = "home_wallpaper"
	ReasonFloor     = "home_floor"
)

// HomeID identifies a home.
type HomeID uint32

// Result is the outcome of a request.
type Result uint8

// Results. Ok is the only success. The room-grid codes mirror housing.Result,
// each with its own value.
const (
	Ok Result = iota
	NoHome
	Locked
	Full
	TooFar
	NotOwner
	NotInside
	AlreadyInside
	BadItem
	InventoryFull
	Busy // the inventory is locked (for example during a trade)

	OutOfBounds
	Overlap
	BadRotation
	BadLayer
	NotSurfaceItem
	NoSurface
	SurfaceFull
	HasItems
	NotFound
	Exists
)

var resultNames = [...]string{"ok", "no_home", "locked", "full", "too_far", "not_owner", "not_inside",
	"already_inside", "bad_item", "inventory_full", "busy", "out_of_bounds", "overlap", "bad_rotation",
	"bad_layer", "not_surface_item", "no_surface", "surface_full", "has_items", "not_found", "exists"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return fmt.Sprintf("Result(%d)", uint8(r))
}

// FromGrid maps a room-grid result to this package's result.
func FromGrid(r housing.Result) Result {
	switch r {
	case housing.OK:
		return Ok
	case housing.OutOfBounds:
		return OutOfBounds
	case housing.Overlap:
		return Overlap
	case housing.BadRotation:
		return BadRotation
	case housing.BadLayer:
		return BadLayer
	case housing.NotSurfaceItem:
		return NotSurfaceItem
	case housing.NoSurface:
		return NoSurface
	case housing.SurfaceFull:
		return SurfaceFull
	case housing.HasItems:
		return HasItems
	case housing.NotFound:
		return NotFound
	case housing.Exists:
		return Exists
	}
	return BadItem
}

// Commands. Each acts for Player; edits apply to the home they are inside.
type (
	// EnterCommand asks to walk into Home through its door.
	EnterCommand struct {
		Player world.PlayerID
		Home   HomeID
	}
	// LeaveCommand asks to walk out of the home the player is in.
	LeaveCommand struct{ Player world.PlayerID }
	// SetLockedCommand opens or locks Home (owner only).
	SetLockedCommand struct {
		Player world.PlayerID
		Home   HomeID
		Locked bool
	}
	// PlaceCommand places the furniture in inventory slot Slot.
	PlaceCommand struct {
		Player world.PlayerID
		Slot   int
		At     housing.Placement
	}
	// MoveCommand moves a placed item.
	MoveCommand struct {
		Player world.PlayerID
		Placed housing.ID
		To     housing.Placement
	}
	// RotateCommand turns a placed item.
	RotateCommand struct {
		Player world.PlayerID
		Placed housing.ID
		Rot    housing.Rotation
	}
	// RemoveCommand returns a placed item to the inventory.
	RemoveCommand struct {
		Player world.PlayerID
		Placed housing.ID
	}
	// SetWallpaperCommand applies the wallpaper in inventory slot Slot.
	SetWallpaperCommand struct {
		Player world.PlayerID
		Slot   int
	}
	// SetFloorCommand applies the floor covering in inventory slot Slot.
	SetFloorCommand struct {
		Player world.PlayerID
		Slot   int
	}
)

func (c EnterCommand) Target() world.PlayerID        { return c.Player }
func (c LeaveCommand) Target() world.PlayerID        { return c.Player }
func (c SetLockedCommand) Target() world.PlayerID    { return c.Player }
func (c PlaceCommand) Target() world.PlayerID        { return c.Player }
func (c MoveCommand) Target() world.PlayerID         { return c.Player }
func (c RotateCommand) Target() world.PlayerID       { return c.Player }
func (c RemoveCommand) Target() world.PlayerID       { return c.Player }
func (c SetWallpaperCommand) Target() world.PlayerID { return c.Player }
func (c SetFloorCommand) Target() world.PlayerID     { return c.Player }

// PlacedItem is an item in a room.
type PlacedItem struct {
	ID   housing.ID
	Item gamedata.ItemID
	housing.Placement
	Layer housing.Layer
}

// State is a home room as everyone inside sees it.
type State struct {
	Home      HomeID
	Owner     world.PlayerID
	Locked    bool
	Wallpaper gamedata.ItemID // 0 when none
	Floor     gamedata.ItemID // 0 when none
	Items     []PlacedItem
}

// Sink receives results and room updates; the session implements it. It is
// called on the tick goroutine and must not block.
type Sink interface {
	// HomeResult answers a request.
	HomeResult(to world.PlayerID, r Result)
	// HomeState sends the room's current state to a player inside it.
	HomeState(to world.PlayerID, s State)
	// HomeLeft tells a player they are no longer inside the home.
	HomeLeft(to world.PlayerID, home HomeID)
}

// Spec describes a home when it is added: its owner, whether it is locked and
// what is already in the room.
type Spec struct {
	ID        HomeID
	Owner     world.PlayerID
	Locked    bool
	Wallpaper gamedata.ItemID
	Floor     gamedata.ItemID
	Items     []PlacedItem
}

// Config holds the system's settings.
type Config struct {
	// Doors maps each home to its door position.
	Doors map[HomeID]world.Vec2
	// Reach is how close to the door a player must be to enter, in metres.
	Reach float64
	// RoomSize is the room edge in tiles; 0 means housing.DefaultSize.
	RoomSize int
}

type home struct {
	id        HomeID
	owner     world.PlayerID
	locked    bool
	wallpaper gamedata.ItemID
	floor     gamedata.ItemID
	room      *housing.Room
	inside    []world.PlayerID // in the order they entered
}

// System runs the home rooms.
type System struct {
	data   *gamedata.Data
	cfg    Config
	sink   Sink
	homes  map[HomeID]*home
	where  map[world.PlayerID]*home
	nextID housing.ID
}

// New creates the system with no homes; add them with AddHome.
func New(d *gamedata.Data, cfg Config, sink Sink) *System {
	if cfg.RoomSize <= 0 {
		cfg.RoomSize = housing.DefaultSize
	}
	return &System{data: d, cfg: cfg, sink: sink, homes: map[HomeID]*home{}, where: map[world.PlayerID]*home{}, nextID: 1}
}

func (s *System) Name() string { return "homeroom" }

// furn adapts a furniture item to housing.ItemDef.
type furn struct {
	item gamedata.ItemID
	f    gamedata.Furniture
}

func (d furn) Footprint() (w, depth int) { return d.f.Width, d.f.Depth }
func (d furn) SurfaceSlots() int         { return d.f.SurfaceSlots }
func (d furn) Layers() housing.Layer {
	var l housing.Layer
	switch d.f.Layer {
	case gamedata.LayerFloor:
		l = housing.LayerFloor
	case gamedata.LayerRug:
		l = housing.LayerRug
	case gamedata.LayerWall:
		l = housing.LayerWall
	case gamedata.LayerSurface:
		l = housing.LayerSurface
	}
	if d.f.OnSurface {
		l |= housing.LayerSurface
	}
	return l
}

func (s *System) def(id gamedata.ItemID) (furn, bool) {
	it, ok := s.data.Item(id)
	if !ok || it.Kind != gamedata.KindFurniture || it.Furniture == nil {
		return furn{}, false
	}
	return furn{id, *it.Furniture}, true
}

// AddHome registers a home, or re-registers an emptied one (see Release) for
// a new owner. It fails if the home has no door, is already owned, or an item
// doesn't fit the room.
func (s *System) AddHome(sp Spec) error {
	if sp.Owner == 0 {
		return errors.New("homeroom: home needs an owner")
	}
	if _, ok := s.cfg.Doors[sp.ID]; !ok {
		return fmt.Errorf("homeroom: home %d has no door", sp.ID)
	}
	if h, ok := s.homes[sp.ID]; ok && h.owner != 0 {
		return fmt.Errorf("homeroom: home %d already exists", sp.ID)
	}
	h := &home{id: sp.ID, owner: sp.Owner, locked: sp.Locked, wallpaper: sp.Wallpaper, floor: sp.Floor,
		room: housing.New(s.cfg.RoomSize)}
	// Place floor items before the surface items that sit on them.
	for pass := 0; pass < 2; pass++ {
		for _, it := range sp.Items {
			if (it.Parent != 0) != (pass == 1) {
				continue
			}
			def, ok := s.def(it.Item)
			if !ok {
				return fmt.Errorf("homeroom: home %d: item %d is not furniture", sp.ID, it.Item)
			}
			if r := h.room.Place(it.ID, def, it.Placement); r != housing.OK {
				return fmt.Errorf("homeroom: home %d: place item %d: %s", sp.ID, it.ID, r)
			}
			if it.ID >= s.nextID {
				s.nextID = it.ID + 1
			}
		}
	}
	s.homes[sp.ID] = h
	return nil
}

// Inside returns the players in a home, in the order they entered.
func (s *System) Inside(id HomeID) []world.PlayerID {
	if h := s.homes[id]; h != nil {
		return append([]world.PlayerID(nil), h.inside...)
	}
	return nil
}

// InHome returns the home a player is inside.
func (s *System) InHome(p world.PlayerID) (HomeID, bool) {
	if h := s.where[p]; h != nil {
		return h.id, true
	}
	return 0, false
}

// State returns the current state of a home.
func (s *System) State(id HomeID) (State, bool) {
	h := s.homes[id]
	if h == nil {
		return State{}, false
	}
	return h.state(), true
}

func (h *home) state() State {
	st := State{Home: h.id, Owner: h.owner, Locked: h.locked, Wallpaper: h.wallpaper, Floor: h.floor}
	for _, it := range h.room.Items() {
		st.Items = append(st.Items, PlacedItem{ID: it.ID, Item: it.Def.(furn).item, Placement: it.Placement, Layer: it.Layer()})
	}
	return st
}

// Handle applies a home command for p and reports its result.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	var r Result
	switch c := cmd.(type) {
	case EnterCommand:
		r = s.enter(p, c.Home)
	case LeaveCommand:
		r = s.leave(p)
	case SetLockedCommand:
		r = s.setLocked(p, c.Home, c.Locked)
	case PlaceCommand:
		r = s.edit(p, func(h *home) Result { return s.place(h, p, c.Slot, c.At) })
	case MoveCommand:
		r = s.edit(p, func(h *home) Result { return FromGrid(h.room.Move(c.Placed, c.To)) })
	case RotateCommand:
		r = s.edit(p, func(h *home) Result { return FromGrid(h.room.Rotate(c.Placed, c.Rot)) })
	case RemoveCommand:
		r = s.edit(p, func(h *home) Result { return s.remove(h, p, c.Placed) })
	case SetWallpaperCommand:
		r = s.edit(p, func(h *home) Result {
			return s.cover(h, p, c.Slot, gamedata.KindWallpaper, &h.wallpaper, ReasonWallpaper)
		})
	case SetFloorCommand:
		r = s.edit(p, func(h *home) Result { return s.cover(h, p, c.Slot, gamedata.KindFloorCovering, &h.floor, ReasonFloor) })
	default:
		return
	}
	s.sink.HomeResult(p.ID, r)
}

// Tick drops players who have left the world from the homes they were in.
func (s *System) Tick(w *world.World, _ time.Duration) {
	for _, h := range s.homes {
		for i := 0; i < len(h.inside); {
			if w.Player(h.inside[i]) == nil {
				delete(s.where, h.inside[i])
				h.inside = append(h.inside[:i], h.inside[i+1:]...)
				continue
			}
			i++
		}
	}
}

func (s *System) enter(p *world.Player, id HomeID) Result {
	h := s.homes[id]
	switch {
	case h == nil || h.owner == 0:
		return NoHome
	case s.where[p.ID] != nil:
		return AlreadyInside
	case !s.nearDoor(p.Pos, id):
		return TooFar
	case h.locked && h.owner != p.ID:
		return Locked
	case len(h.inside) >= MaxVisitors:
		return Full
	}
	h.inside = append(h.inside, p.ID)
	s.where[p.ID] = h
	s.sink.HomeState(p.ID, h.state())
	return Ok
}

func (s *System) nearDoor(pos world.Vec2, id HomeID) bool {
	door := s.cfg.Doors[id]
	dx := float64(pos.X) - float64(door.X)
	dy := float64(pos.Y) - float64(door.Y)
	return dx*dx+dy*dy <= s.cfg.Reach*s.cfg.Reach
}

func (s *System) leave(p *world.Player) Result {
	h := s.where[p.ID]
	if h == nil {
		return NotInside
	}
	s.exit(h, p.ID)
	p.Pos = s.cfg.Doors[h.id]
	s.sink.HomeLeft(p.ID, h.id)
	return Ok
}

func (s *System) exit(h *home, id world.PlayerID) {
	delete(s.where, id)
	for i, q := range h.inside {
		if q == id {
			h.inside = append(h.inside[:i], h.inside[i+1:]...)
			return
		}
	}
}

func (s *System) setLocked(p *world.Player, id HomeID, locked bool) Result {
	h := s.homes[id]
	switch {
	case h == nil || h.owner == 0:
		return NoHome
	case h.owner != p.ID:
		return NotOwner
	}
	if h.locked != locked {
		h.locked = locked
		s.broadcast(h)
	}
	return Ok
}

// edit checks that p is inside a home they own, runs fn, and on success
// sends the new room state to everyone inside.
func (s *System) edit(p *world.Player, fn func(*home) Result) Result {
	h := s.where[p.ID]
	switch {
	case h == nil:
		return NotInside
	case h.owner != p.ID:
		return NotOwner
	}
	r := fn(h)
	if r == Ok {
		s.broadcast(h)
	}
	return r
}

func (s *System) broadcast(h *home) {
	st := h.state()
	for _, id := range h.inside {
		s.sink.HomeState(id, st)
	}
}

// invResult maps an inventory error to a result.
func invResult(err error) Result {
	switch {
	case errors.Is(err, world.ErrInventoryFull):
		return InventoryFull
	case errors.Is(err, world.ErrInventoryLocked):
		return Busy
	}
	return BadItem
}

func (s *System) place(h *home, p *world.Player, slot int, at housing.Placement) Result {
	if slot < 0 || slot >= len(p.Inv.Slots) || p.Inv.Slots[slot].Empty() {
		return BadItem
	}
	def, ok := s.def(p.Inv.Slots[slot].Item)
	if !ok {
		return BadItem
	}
	// Place in the room first; it leaves the room unchanged on failure.
	id := s.nextID
	if r := h.room.Place(id, def, at); r != housing.OK {
		return FromGrid(r)
	}
	inv := p.Inv.Clone()
	changes, err := inv.RemoveFromSlot(slot, 1)
	if err != nil {
		h.room.Remove(id)
		return invResult(err)
	}
	s.nextID++
	p.Inv = inv
	p.RecordEconomy(ReasonPlace, changes)
	return Ok
}

func (s *System) remove(h *home, p *world.Player, id housing.ID) Result {
	it, ok := h.room.Get(id)
	if !ok {
		return NotFound
	}
	item := it.Def.(furn).item
	inv := p.Inv.Clone()
	changes, err := inv.Add(s.data, item, 1)
	if err != nil {
		return invResult(err)
	}
	if r := h.room.Remove(id); r != housing.OK {
		return FromGrid(r)
	}
	p.Inv = inv
	p.RecordEconomy(ReasonRemove, changes)
	return Ok
}

// cover swaps the wallpaper or floor covering in *cur with the one in
// inventory slot slot.
func (s *System) cover(h *home, p *world.Player, slot int, kind gamedata.ItemKind, cur *gamedata.ItemID, reason string) Result {
	if slot < 0 || slot >= len(p.Inv.Slots) || p.Inv.Slots[slot].Empty() {
		return BadItem
	}
	item := p.Inv.Slots[slot].Item
	if it, ok := s.data.Item(item); !ok || it.Kind != kind {
		return BadItem
	}
	inv := p.Inv.Clone()
	var changes []world.Change
	var err error
	if *cur != 0 {
		// The old covering takes the new one's slot, so it always fits.
		changes, err = inv.ReplaceAt(slot, *cur)
	} else {
		changes, err = inv.RemoveFromSlot(slot, 1)
	}
	if err != nil {
		return invResult(err)
	}
	*cur = item
	p.Inv = inv
	p.RecordEconomy(reason, changes)
	return Ok
}

// Returned is an item handed back to a previous owner when a house is lost.
type Returned struct {
	Item gamedata.ItemID
	Qty  int
}

// Release empties a lost house: everyone inside is moved out to the door, the
// room is cleared, and every placed item, wallpaper and floor is returned as a
// list for the caller to deliver to the previous owner's inventory (as one
// audited transaction). The home stays registered, ownerless, until AddHome
// gives it to a new owner. Call it on the tick.
func (s *System) Release(w *world.World, id HomeID) ([]Returned, error) {
	h := s.homes[id]
	if h == nil || h.owner == 0 {
		return nil, fmt.Errorf("homeroom: release home %d: %w", id, errNoHome)
	}
	door := s.cfg.Doors[id]
	for _, pid := range h.inside {
		delete(s.where, pid)
		if p := w.Player(pid); p != nil {
			p.Pos = door
		}
		s.sink.HomeLeft(pid, id)
	}
	var out []Returned
	for _, it := range h.room.Items() {
		out = append(out, Returned{Item: it.Def.(furn).item, Qty: 1})
	}
	for _, c := range []gamedata.ItemID{h.wallpaper, h.floor} {
		if c != 0 {
			out = append(out, Returned{Item: c, Qty: 1})
		}
	}
	*h = home{id: id, room: housing.New(s.cfg.RoomSize)}
	return out, nil
}

var errNoHome = errors.New("no such owned home")
