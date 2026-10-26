package world

import (
	"errors"
	"fmt"
	"math"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

// Inventory errors. Every operation that returns one leaves the inventory
// unchanged.
var (
	ErrUnknownItem      = errors.New("unknown item")
	ErrBadQuantity      = errors.New("quantity must be positive")
	ErrInventoryFull    = errors.New("inventory full")
	ErrNotEnoughItems   = errors.New("not enough items")
	ErrNotEnoughGold    = errors.New("not enough gold")
	ErrGoldOverflow     = errors.New("gold balance overflow")
	ErrStackOverflow    = errors.New("stack overflow")
	ErrBadSlot          = errors.New("no such inventory slot")
	ErrEmptySlot        = errors.New("inventory slot is empty")
	ErrNotEquipment     = errors.New("item is not equipment")
	ErrLevelTooLow      = errors.New("combat level too low to equip")
	ErrNothingEquipped  = errors.New("nothing equipped in slot")
	ErrBadEquipmentSlot = errors.New("not an equipment slot")
	ErrInventoryLocked  = errors.New("inventory locked")
)

// Stack is the content of one inventory slot. Item 0 means the slot is empty.
type Stack struct {
	Item gamedata.ItemID
	Qty  int
}

// Empty reports whether the slot holds nothing.
func (s Stack) Empty() bool { return s.Item == 0 }

// Equipment holds the worn items, one per slot. 0 means nothing worn.
type Equipment struct {
	Weapon, Head, Body, Shield gamedata.ItemID
}

// Get returns the item worn in slot, or 0.
func (e *Equipment) Get(slot gamedata.Slot) gamedata.ItemID {
	if p := e.ref(slot); p != nil {
		return *p
	}
	return 0
}

func (e *Equipment) ref(slot gamedata.Slot) *gamedata.ItemID {
	switch slot {
	case gamedata.SlotWeapon:
		return &e.Weapon
	case gamedata.SlotHead:
		return &e.Head
	case gamedata.SlotBody:
		return &e.Body
	case gamedata.SlotShield:
		return &e.Shield
	}
	return nil
}

func (e *Equipment) all() [4]gamedata.ItemID {
	return [4]gamedata.ItemID{e.Weapon, e.Head, e.Body, e.Shield}
}

// ChangeKind names what a Change did, for the economy audit log.
type ChangeKind uint8

const (
	// ChangeCreate: Qty of Item came into existence in the player's
	// possession (the starting kit), worn or in the inventory.
	ChangeCreate ChangeKind = iota + 1
	// ChangeAdd: Qty of Item entered the inventory.
	ChangeAdd
	// ChangeRemove: Qty of Item left the inventory.
	ChangeRemove
	// ChangeEquip: one Item moved from the inventory into its equipment slot.
	ChangeEquip
	// ChangeUnequip: one Item moved from its equipment slot into the inventory.
	ChangeUnequip
	// ChangeGold: the gold balance changed by Gold.
	ChangeGold
)

var changeKindNames = [...]string{
	ChangeCreate:  "item_create",
	ChangeAdd:     "item_add",
	ChangeRemove:  "item_remove",
	ChangeEquip:   "item_equip",
	ChangeUnequip: "item_unequip",
	ChangeGold:    "gold",
}

func (k ChangeKind) String() string {
	if int(k) < len(changeKindNames) && changeKindNames[k] != "" {
		return changeKindNames[k]
	}
	return fmt.Sprintf("ChangeKind(%d)", uint8(k))
}

// Change records one creation or movement of items or gold. Callers write
// changes to the economy audit log; producing them does no I/O.
type Change struct {
	Kind ChangeKind
	Item gamedata.ItemID // 0 for ChangeGold
	Qty  int             // 0 for ChangeGold
	Gold int             // gold delta; 0 unless ChangeGold
}

// Inventory is a player's items and gold: a fixed number of slots
// (Rules.InventorySlots), the equipment worn (which takes no slot) and a gold
// balance that is never negative. Only the tick may mutate it.
//
// A locked inventory (see SetLocked) refuses every change: each method that would
// change items, equipment or gold returns ErrInventoryLocked instead. Clone
// keeps the lock, so a system working on a copy is refused too.
type Inventory struct {
	Slots     []Stack
	Equipment Equipment
	Gold      int

	locked bool
}

// SetLocked locks or unlocks the inventory. While locked, every change fails
// with ErrInventoryLocked. The trade system locks both players' inventories
// while their trade is being saved.
func (inv *Inventory) SetLocked(locked bool) { inv.locked = locked }

// Locked reports whether the inventory is locked.
func (inv *Inventory) Locked() bool { return inv.locked }

// NewInventory returns an empty inventory with d.Rules.InventorySlots slots.
func NewInventory(d *gamedata.Data) Inventory {
	return Inventory{Slots: make([]Stack, d.Rules.InventorySlots)}
}

// NewStartingInventory returns a new player's inventory: the starting kit's
// items equipped, its inventory items added and the starting gold, all from
// d.Rules. The changes record everything created.
func NewStartingInventory(d *gamedata.Data) (Inventory, []Change, error) {
	inv := NewInventory(d)
	var changes []Change
	kit := d.Rules.StartingKit
	for _, id := range kit.Equipped {
		it, ok := d.Item(id)
		if !ok {
			return Inventory{}, nil, fmt.Errorf("starting kit: item %d: %w", id, ErrUnknownItem)
		}
		ref := inv.Equipment.ref(it.Slot)
		if ref == nil {
			return Inventory{}, nil, fmt.Errorf("starting kit: %s: %w", it.Key, ErrNotEquipment)
		}
		*ref = id
		changes = append(changes, Change{Kind: ChangeCreate, Item: id, Qty: 1})
	}
	for _, s := range kit.Inventory {
		if _, err := inv.Add(d, s.Item, s.Quantity); err != nil {
			return Inventory{}, nil, fmt.Errorf("starting kit: %s: %w", s.ItemKey, err)
		}
		changes = append(changes, Change{Kind: ChangeCreate, Item: s.Item, Qty: s.Quantity})
	}
	ch, err := inv.AddGold(d.Rules.StartingGold)
	if err != nil {
		return Inventory{}, nil, fmt.Errorf("starting gold: %w", err)
	}
	return inv, append(changes, ch...), nil
}

// Count returns how many of item the inventory slots hold (equipment excluded).
func (inv *Inventory) Count(item gamedata.ItemID) int {
	n := 0
	for _, s := range inv.Slots {
		if s.Item == item {
			n += s.Qty
		}
	}
	return n
}

// FreeSlots returns the number of empty slots.
func (inv *Inventory) FreeSlots() int {
	n := 0
	for _, s := range inv.Slots {
		if s.Empty() {
			n++
		}
	}
	return n
}

func (inv *Inventory) firstFree() int {
	for i, s := range inv.Slots {
		if s.Empty() {
			return i
		}
	}
	return -1
}

// Add puts qty of item into the inventory. Stackable items (loot) go onto the
// existing stack or one free slot; other items take one free slot each. If
// they don't all fit, nothing changes.
func (inv *Inventory) Add(d *gamedata.Data, item gamedata.ItemID, qty int) ([]Change, error) {
	if inv.locked {
		return nil, fmt.Errorf("add item %d: %w", item, ErrInventoryLocked)
	}
	it, ok := d.Item(item)
	if !ok {
		return nil, fmt.Errorf("add item %d: %w", item, ErrUnknownItem)
	}
	if qty <= 0 {
		return nil, fmt.Errorf("add %d × %s: %w", qty, it.Key, ErrBadQuantity)
	}
	if it.Stackable() {
		for i := range inv.Slots {
			if s := &inv.Slots[i]; s.Item == item {
				if s.Qty > math.MaxInt-qty {
					return nil, fmt.Errorf("add %d × %s: %w", qty, it.Key, ErrStackOverflow)
				}
				s.Qty += qty
				return []Change{{Kind: ChangeAdd, Item: item, Qty: qty}}, nil
			}
		}
		i := inv.firstFree()
		if i < 0 {
			return nil, fmt.Errorf("add %d × %s: %w", qty, it.Key, ErrInventoryFull)
		}
		inv.Slots[i] = Stack{Item: item, Qty: qty}
		return []Change{{Kind: ChangeAdd, Item: item, Qty: qty}}, nil
	}
	if inv.FreeSlots() < qty {
		return nil, fmt.Errorf("add %d × %s: %w", qty, it.Key, ErrInventoryFull)
	}
	for n := 0; n < qty; n++ {
		inv.Slots[inv.firstFree()] = Stack{Item: item, Qty: 1}
	}
	return []Change{{Kind: ChangeAdd, Item: item, Qty: qty}}, nil
}

// Remove takes qty of item out of the inventory slots, from the last slot
// holding it backwards. If fewer than qty are held, nothing changes.
func (inv *Inventory) Remove(item gamedata.ItemID, qty int) ([]Change, error) {
	if inv.locked {
		return nil, fmt.Errorf("remove item %d: %w", item, ErrInventoryLocked)
	}
	if qty <= 0 {
		return nil, fmt.Errorf("remove %d × item %d: %w", qty, item, ErrBadQuantity)
	}
	if item == 0 {
		return nil, fmt.Errorf("remove item 0: %w", ErrUnknownItem)
	}
	if have := inv.Count(item); have < qty {
		return nil, fmt.Errorf("remove %d × item %d (have %d): %w", qty, item, have, ErrNotEnoughItems)
	}
	left := qty
	for i := len(inv.Slots) - 1; i >= 0 && left > 0; i-- {
		s := &inv.Slots[i]
		if s.Item != item {
			continue
		}
		take := min(s.Qty, left)
		s.Qty -= take
		left -= take
		if s.Qty == 0 {
			*s = Stack{}
		}
	}
	return []Change{{Kind: ChangeRemove, Item: item, Qty: qty}}, nil
}

// RemoveAt takes one item out of inventory slot index. It fails, changing
// nothing, if there is no such slot or it is empty.
func (inv *Inventory) RemoveAt(index int) ([]Change, error) {
	if inv.locked {
		return nil, fmt.Errorf("remove from slot %d: %w", index, ErrInventoryLocked)
	}
	if index < 0 || index >= len(inv.Slots) {
		return nil, fmt.Errorf("remove from slot %d: %w", index, ErrBadSlot)
	}
	s := &inv.Slots[index]
	if s.Empty() {
		return nil, fmt.Errorf("remove from slot %d: %w", index, ErrEmptySlot)
	}
	item := s.Item
	s.Qty--
	if s.Qty <= 0 {
		*s = Stack{}
	}
	return []Change{{Kind: ChangeRemove, Item: item, Qty: 1}}, nil
}

// RemoveFromSlot takes qty items out of inventory slot index. It fails,
// changing nothing, if there is no such slot, it is empty, qty is not positive
// or the slot holds fewer than qty.
func (inv *Inventory) RemoveFromSlot(index, qty int) ([]Change, error) {
	if inv.locked {
		return nil, fmt.Errorf("remove from slot %d: %w", index, ErrInventoryLocked)
	}
	if index < 0 || index >= len(inv.Slots) {
		return nil, fmt.Errorf("remove from slot %d: %w", index, ErrBadSlot)
	}
	s := &inv.Slots[index]
	if s.Empty() {
		return nil, fmt.Errorf("remove from slot %d: %w", index, ErrEmptySlot)
	}
	if qty <= 0 {
		return nil, fmt.Errorf("remove %d from slot %d: %w", qty, index, ErrBadQuantity)
	}
	if qty > s.Qty {
		return nil, fmt.Errorf("remove %d from slot %d (has %d): %w", qty, index, s.Qty, ErrNotEnoughItems)
	}
	item := s.Item
	s.Qty -= qty
	if s.Qty == 0 {
		*s = Stack{}
	}
	return []Change{{Kind: ChangeRemove, Item: item, Qty: qty}}, nil
}

// AddGold increases the gold balance by amount.
func (inv *Inventory) AddGold(amount int) ([]Change, error) {
	if inv.locked {
		return nil, fmt.Errorf("add %d gold: %w", amount, ErrInventoryLocked)
	}
	if amount <= 0 {
		return nil, fmt.Errorf("add %d gold: %w", amount, ErrBadQuantity)
	}
	if inv.Gold > math.MaxInt-amount {
		return nil, fmt.Errorf("add %d gold: %w", amount, ErrGoldOverflow)
	}
	inv.Gold += amount
	return []Change{{Kind: ChangeGold, Gold: amount}}, nil
}

// SpendGold decreases the gold balance by amount. It fails, changing nothing,
// if the balance would go below 0.
func (inv *Inventory) SpendGold(amount int) ([]Change, error) {
	if inv.locked {
		return nil, fmt.Errorf("spend %d gold: %w", amount, ErrInventoryLocked)
	}
	if amount <= 0 {
		return nil, fmt.Errorf("spend %d gold: %w", amount, ErrBadQuantity)
	}
	if amount > inv.Gold {
		return nil, fmt.Errorf("spend %d gold (have %d): %w", amount, inv.Gold, ErrNotEnoughGold)
	}
	inv.Gold -= amount
	return []Change{{Kind: ChangeGold, Gold: -amount}}, nil
}

// Equip wears the item in inventory slot index, for a player of combat level
// level. Any item already worn in that equipment slot goes into the freed
// inventory slot. It fails, changing nothing, if the slot is empty, the item
// isn't equipment or level is below the item's level to equip.
func (inv *Inventory) Equip(d *gamedata.Data, index, level int) ([]Change, error) {
	if inv.locked {
		return nil, fmt.Errorf("equip slot %d: %w", index, ErrInventoryLocked)
	}
	if index < 0 || index >= len(inv.Slots) {
		return nil, fmt.Errorf("equip slot %d: %w", index, ErrBadSlot)
	}
	s := inv.Slots[index]
	if s.Empty() {
		return nil, fmt.Errorf("equip slot %d: %w", index, ErrEmptySlot)
	}
	it, ok := d.Item(s.Item)
	if !ok {
		return nil, fmt.Errorf("equip item %d: %w", s.Item, ErrUnknownItem)
	}
	ref := inv.Equipment.ref(it.Slot)
	if !it.Kind.IsEquipment() || ref == nil {
		return nil, fmt.Errorf("equip %s: %w", it.Key, ErrNotEquipment)
	}
	if level < it.LevelToEquip {
		return nil, fmt.Errorf("equip %s at level %d (needs %d): %w", it.Key, level, it.LevelToEquip, ErrLevelTooLow)
	}
	prev := *ref
	*ref = s.Item
	inv.Slots[index] = Stack{}
	changes := []Change{{Kind: ChangeEquip, Item: s.Item, Qty: 1}}
	if prev != 0 {
		inv.Slots[index] = Stack{Item: prev, Qty: 1}
		changes = append(changes, Change{Kind: ChangeUnequip, Item: prev, Qty: 1})
	}
	return changes, nil
}

// Unequip moves the item worn in slot into the first free inventory slot. It
// fails, changing nothing, if nothing is worn there or the inventory is full.
func (inv *Inventory) Unequip(slot gamedata.Slot) ([]Change, error) {
	if inv.locked {
		return nil, fmt.Errorf("unequip %q: %w", slot, ErrInventoryLocked)
	}
	ref := inv.Equipment.ref(slot)
	if ref == nil {
		return nil, fmt.Errorf("unequip %q: %w", slot, ErrBadEquipmentSlot)
	}
	if *ref == 0 {
		return nil, fmt.Errorf("unequip %s: %w", slot, ErrNothingEquipped)
	}
	i := inv.firstFree()
	if i < 0 {
		return nil, fmt.Errorf("unequip %s: %w", slot, ErrInventoryFull)
	}
	item := *ref
	inv.Slots[i] = Stack{Item: item, Qty: 1}
	*ref = 0
	return []Change{{Kind: ChangeUnequip, Item: item, Qty: 1}}, nil
}

// Bonuses totals accuracy, strength and defence over the worn items and
// returns the attack speed in combat ticks: the weapon's, or
// d.Rules.UnarmedAttackSpeed with no weapon.
func (e *Equipment) Bonuses(d *gamedata.Data) (gamedata.Stats, int) {
	var total gamedata.Stats
	speed := d.Rules.UnarmedAttackSpeed
	for _, id := range e.all() {
		if id == 0 {
			continue
		}
		it, ok := d.Item(id)
		if !ok {
			continue
		}
		total.Accuracy += it.Accuracy
		total.Strength += it.Strength
		total.Defence += it.Defence
		if it.Slot == gamedata.SlotWeapon {
			speed = it.AttackSpeed
		}
	}
	return total, speed
}
