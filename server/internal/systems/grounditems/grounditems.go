// Package grounditems is the ground item system (docs/specs/gameplay.md,
// NPCs and Death in the PvP zone): other systems spawn stacks of items on the
// ground with an owner. Only the owner can pick a stack up for
// Rules.DropReservation, then anyone within Rules.PickupRangeTiles can, and
// the stack disappears at Rules.DropDespawn. A pick-up moves the stack into
// the player's inventory and records one event in their economy outbox.
//
// It does no I/O and reads time only from the injected world.Clock.
package grounditems

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// ReasonPickup is the outbox reason of a pick-up.
const ReasonPickup = "pickup"

// ID identifies a ground item on the wire (protocol GroundItem.id). 0 is
// never used. An id is not handed out again while its item exists.
type ID uint16

// Errors returned by Spawn and PickUp. Each leaves everything unchanged.
var (
	ErrNoFreeID     = errors.New("no free ground item id")
	ErrUnknownItem  = errors.New("unknown item")
	ErrBadQuantity  = errors.New("quantity must be positive")
	ErrNoSuchItem   = errors.New("no such ground item")
	ErrTooFar       = errors.New("ground item out of pick-up range")
	ErrReserved     = errors.New("ground item reserved for its owner")
	ErrNoInvSpace   = errors.New("inventory cannot take the stack")
	ErrUnknownOwner = errors.New("owner must be a player")
)

// Item is a stack lying on the ground.
type Item struct {
	ID    ID
	Item  gamedata.ItemID
	Qty   int
	Pos   world.Vec2
	Owner world.PlayerID
	// Spawned is when the stack appeared. The reservation and despawn
	// times count from it.
	Spawned time.Time
}

// PickUpCommand asks for Player to pick up the ground item ID.
type PickUpCommand struct {
	Player world.PlayerID
	ID     ID
}

func (c PickUpCommand) Target() world.PlayerID { return c.Player }

// System holds the ground items. Only the tick goroutine may call its methods.
type System struct {
	data     *gamedata.Data
	clock    world.Clock
	tileSize float64

	items map[ID]*Item
	order []*Item // spawn order, for deterministic iteration
	next  ID      // next id Spawn tries
}

// New creates the ground item system. The reservation, despawn and pick-up
// range rules come from d; tileSize is the length of a tile in metres (the
// collision grid's cell size).
func New(d *gamedata.Data, clock world.Clock, tileSize float64) *System {
	return &System{
		data:     d,
		clock:    clock,
		tileSize: tileSize,
		items:    make(map[ID]*Item),
		next:     1,
	}
}

func (s *System) Name() string { return "grounditems" }

// Spawn puts qty of item on the ground at pos, reserved for owner, as of at.
// It returns the new item's id, or ErrNoFreeID when every id is in use.
func (s *System) Spawn(item gamedata.ItemID, qty int, pos world.Vec2, owner world.PlayerID, at time.Time) (ID, error) {
	if _, ok := s.data.Item(item); !ok {
		return 0, fmt.Errorf("spawn item %d: %w", item, ErrUnknownItem)
	}
	if qty <= 0 {
		return 0, fmt.Errorf("spawn %d × item %d: %w", qty, item, ErrBadQuantity)
	}
	if owner == 0 {
		return 0, fmt.Errorf("spawn item %d: %w", item, ErrUnknownOwner)
	}
	id, ok := s.allocID()
	if !ok {
		return 0, fmt.Errorf("spawn item %d: %w", item, ErrNoFreeID)
	}
	it := &Item{ID: id, Item: item, Qty: qty, Pos: pos, Owner: owner, Spawned: at}
	s.items[id] = it
	s.order = append(s.order, it)
	return id, nil
}

// allocID returns the first free id at or after s.next, wrapping from 65535
// to 1 and skipping ids still in use.
func (s *System) allocID() (ID, bool) {
	if len(s.items) >= math.MaxUint16 {
		return 0, false
	}
	for {
		id := s.next
		s.next++
		if s.next == 0 {
			s.next = 1
		}
		if _, used := s.items[id]; !used {
			return id, true
		}
	}
}

// Get returns a copy of the ground item with id.
func (s *System) Get(id ID) (Item, bool) {
	it, ok := s.items[id]
	if !ok {
		return Item{}, false
	}
	return *it, true
}

// Len returns how many ground items exist.
func (s *System) Len() int { return len(s.items) }

// Near appends to dst the ground items within radius metres of pos (the
// boundary counts as inside), in spawn order, and returns it.
func (s *System) Near(dst []Item, pos world.Vec2, radius float64) []Item {
	r2 := radius * radius
	for _, it := range s.order {
		dx := float64(it.Pos.X) - float64(pos.X)
		dy := float64(it.Pos.Y) - float64(pos.Y)
		if dx*dx+dy*dy <= r2 {
			dst = append(dst, *it)
		}
	}
	return dst
}

// Handle applies a pick-up command for p. A failed pick-up changes nothing.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	if c, ok := cmd.(PickUpCommand); ok {
		_ = s.PickUp(p, c.ID)
	}
}

// PickUp moves ground item id into p's inventory, removes it from the ground
// and records one ReasonPickup event in p's outbox. It fails, changing
// nothing, if the item doesn't exist (or has expired), is out of range, is
// still reserved for another player, or doesn't fit in p's inventory.
func (s *System) PickUp(p *world.Player, id ID) error {
	it, ok := s.items[id]
	now := s.clock.Now()
	if !ok || !now.Before(it.Spawned.Add(s.data.Rules.DropDespawn())) {
		return fmt.Errorf("pick up %d: %w", id, ErrNoSuchItem)
	}
	if !s.inRange(p.Pos, it.Pos) {
		return fmt.Errorf("pick up %d: %w", id, ErrTooFar)
	}
	if p.ID != it.Owner && now.Before(it.Spawned.Add(s.data.Rules.DropReservation())) {
		return fmt.Errorf("pick up %d: %w", id, ErrReserved)
	}
	inv := p.Inv.Clone()
	added, err := inv.Add(s.data, it.Item, it.Qty)
	if err != nil {
		return fmt.Errorf("pick up %d: %w: %w", id, ErrNoInvSpace, err)
	}
	p.Inv = inv
	s.remove(id)
	p.RecordEconomy(ReasonPickup, added)
	return nil
}

// Tick removes every ground item that has reached Rules.DropDespawn.
func (s *System) Tick(*world.World, time.Duration) {
	now := s.clock.Now()
	ttl := s.data.Rules.DropDespawn()
	kept := s.order[:0]
	for _, it := range s.order {
		if now.Before(it.Spawned.Add(ttl)) {
			kept = append(kept, it)
		} else {
			delete(s.items, it.ID)
		}
	}
	clear(s.order[len(kept):])
	s.order = kept
}

func (s *System) remove(id ID) {
	it, ok := s.items[id]
	if !ok {
		return
	}
	delete(s.items, id)
	for i, o := range s.order {
		if o == it {
			s.order = slices.Delete(s.order, i, i+1)
			break
		}
	}
}

// inRange reports whether b's tile is within Rules.PickupRangeTiles of a's,
// counting diagonal steps as one when Rules.PickupRangeDiagonal is set.
func (s *System) inRange(a, b world.Vec2) bool {
	dx := abs(s.tile(a.X) - s.tile(b.X))
	dy := abs(s.tile(a.Y) - s.tile(b.Y))
	reach := s.data.Rules.PickupRangeTiles
	if s.data.Rules.PickupRangeDiagonal {
		return max(dx, dy) <= reach
	}
	return dx+dy <= reach
}

func (s *System) tile(v float32) int {
	return int(math.Floor(float64(v) / s.tileSize))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
