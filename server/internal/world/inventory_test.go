package world_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

func gameData(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatalf("gamedata: %v", err)
	}
	return d
}

func itemID(t testing.TB, d *gamedata.Data, key string) gamedata.ItemID {
	t.Helper()
	it, ok := d.ItemByKey(key)
	if !ok {
		t.Fatalf("no item %q", key)
	}
	return it.ID
}

// fill returns an inventory whose every slot holds one item.
func fill(t testing.TB, d *gamedata.Data, key string) world.Inventory {
	t.Helper()
	inv := world.NewInventory(d)
	if _, err := inv.Add(d, itemID(t, d, key), len(inv.Slots)); err != nil {
		t.Fatalf("fill: %v", err)
	}
	return inv
}

func clone(inv world.Inventory) world.Inventory {
	inv.Slots = append([]world.Stack(nil), inv.Slots...)
	return inv
}

func TestStartingInventory(t *testing.T) {
	d := gameData(t)
	stick, hoodie, boba := itemID(t, d, "wooden_stick"), itemID(t, d, "hoodie"), itemID(t, d, "boba")

	inv, changes, err := world.NewStartingInventory(d)
	if err != nil {
		t.Fatal(err)
	}
	p := &world.Player{ID: 1, Inv: inv}
	if len(p.Inv.Slots) != 28 {
		t.Errorf("slots = %d, want 28", len(p.Inv.Slots))
	}
	if want := (world.Equipment{Weapon: stick, Body: hoodie}); p.Inv.Equipment != want {
		t.Errorf("equipment = %+v, want %+v", p.Inv.Equipment, want)
	}
	if got := p.Inv.Count(boba); got != 3 {
		t.Errorf("boba = %d, want 3", got)
	}
	if got := p.Inv.FreeSlots(); got != 25 {
		t.Errorf("free slots = %d, want 25 (3 Boba take a slot each)", got)
	}
	if p.Inv.Gold != 100 {
		t.Errorf("gold = %d, want 100", p.Inv.Gold)
	}
	wantChanges := []world.Change{
		{Kind: world.ChangeCreate, Item: stick, Qty: 1},
		{Kind: world.ChangeCreate, Item: hoodie, Qty: 1},
		{Kind: world.ChangeCreate, Item: boba, Qty: 3},
		{Kind: world.ChangeGold, Gold: 100},
	}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Errorf("changes = %+v, want %+v", changes, wantChanges)
	}
}

func TestAddRemove(t *testing.T) {
	d := gameData(t)
	acorn, pelt, boba, dagger := itemID(t, d, "acorn"), itemID(t, d, "coyote_pelt"), itemID(t, d, "boba"), itemID(t, d, "bronze_dagger")

	// setup returns an inventory with 5 acorns in slot 0 and 2 boba in slots 1 and 2.
	setup := func(t *testing.T) world.Inventory {
		inv := world.NewInventory(d)
		inv.Slots[0] = world.Stack{Item: acorn, Qty: 5}
		inv.Slots[1] = world.Stack{Item: boba, Qty: 1}
		inv.Slots[2] = world.Stack{Item: boba, Qty: 1}
		return inv
	}
	full := func(t *testing.T) world.Inventory {
		inv := fill(t, d, "boba")
		inv.Slots[27] = world.Stack{Item: acorn, Qty: 5}
		return inv
	}

	tests := []struct {
		name    string
		inv     func(*testing.T) world.Inventory
		op      func(*world.Inventory) ([]world.Change, error)
		wantErr error
		check   func(*testing.T, world.Inventory)
		changes []world.Change
	}{
		{
			name: "loot stacks onto existing stack",
			inv:  setup,
			op:   func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, acorn, 1000) },
			check: func(t *testing.T, inv world.Inventory) {
				if inv.Slots[0] != (world.Stack{Item: acorn, Qty: 1005}) || inv.FreeSlots() != 25 {
					t.Errorf("slot 0 = %+v, free = %d", inv.Slots[0], inv.FreeSlots())
				}
			},
			changes: []world.Change{{Kind: world.ChangeAdd, Item: acorn, Qty: 1000}},
		},
		{
			name: "new loot takes one slot",
			inv:  setup,
			op:   func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, pelt, 3) },
			check: func(t *testing.T, inv world.Inventory) {
				if inv.Slots[3] != (world.Stack{Item: pelt, Qty: 3}) || inv.FreeSlots() != 24 {
					t.Errorf("slot 3 = %+v, free = %d", inv.Slots[3], inv.FreeSlots())
				}
			},
			changes: []world.Change{{Kind: world.ChangeAdd, Item: pelt, Qty: 3}},
		},
		{
			name: "food does not stack",
			inv:  setup,
			op:   func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, boba, 2) },
			check: func(t *testing.T, inv world.Inventory) {
				if inv.Count(boba) != 4 || inv.FreeSlots() != 23 {
					t.Errorf("boba = %d, free = %d", inv.Count(boba), inv.FreeSlots())
				}
				for _, i := range []int{3, 4} {
					if inv.Slots[i] != (world.Stack{Item: boba, Qty: 1}) {
						t.Errorf("slot %d = %+v", i, inv.Slots[i])
					}
				}
			},
			changes: []world.Change{{Kind: world.ChangeAdd, Item: boba, Qty: 2}},
		},
		{
			name: "equipment does not stack",
			inv:  setup,
			op:   func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, dagger, 2) },
			check: func(t *testing.T, inv world.Inventory) {
				if inv.Count(dagger) != 2 || inv.FreeSlots() != 23 {
					t.Errorf("dagger = %d, free = %d", inv.Count(dagger), inv.FreeSlots())
				}
			},
			changes: []world.Change{{Kind: world.ChangeAdd, Item: dagger, Qty: 2}},
		},
		{
			name:    "non-stackable items that don't all fit",
			inv:     func(t *testing.T) world.Inventory { inv := setup(t); _, _ = inv.Add(d, dagger, 24); return inv },
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, boba, 2) },
			wantErr: world.ErrInventoryFull,
		},
		{
			name:    "equipment into full inventory",
			inv:     full,
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, dagger, 1) },
			wantErr: world.ErrInventoryFull,
		},
		{
			name:    "new loot into full inventory",
			inv:     full,
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, pelt, 1) },
			wantErr: world.ErrInventoryFull,
		},
		{
			name: "loot onto existing stack in full inventory",
			inv:  full,
			op:   func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, acorn, 2) },
			check: func(t *testing.T, inv world.Inventory) {
				if inv.Slots[27] != (world.Stack{Item: acorn, Qty: 7}) {
					t.Errorf("slot 27 = %+v", inv.Slots[27])
				}
			},
			changes: []world.Change{{Kind: world.ChangeAdd, Item: acorn, Qty: 2}},
		},
		{
			name:    "unknown item",
			inv:     setup,
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, 9999, 1) },
			wantErr: world.ErrUnknownItem,
		},
		{
			name:    "add zero",
			inv:     setup,
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, acorn, 0) },
			wantErr: world.ErrBadQuantity,
		},
		{
			name: "remove part of a stack",
			inv:  setup,
			op:   func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(acorn, 2) },
			check: func(t *testing.T, inv world.Inventory) {
				if inv.Slots[0] != (world.Stack{Item: acorn, Qty: 3}) {
					t.Errorf("slot 0 = %+v", inv.Slots[0])
				}
			},
			changes: []world.Change{{Kind: world.ChangeRemove, Item: acorn, Qty: 2}},
		},
		{
			name: "remove a whole stack frees the slot",
			inv:  setup,
			op:   func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(acorn, 5) },
			check: func(t *testing.T, inv world.Inventory) {
				if !inv.Slots[0].Empty() || inv.FreeSlots() != 26 {
					t.Errorf("slot 0 = %+v, free = %d", inv.Slots[0], inv.FreeSlots())
				}
			},
			changes: []world.Change{{Kind: world.ChangeRemove, Item: acorn, Qty: 5}},
		},
		{
			name: "remove non-stacked items across slots",
			inv:  setup,
			op:   func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(boba, 2) },
			check: func(t *testing.T, inv world.Inventory) {
				if inv.Count(boba) != 0 || inv.FreeSlots() != 27 {
					t.Errorf("boba = %d, free = %d", inv.Count(boba), inv.FreeSlots())
				}
			},
			changes: []world.Change{{Kind: world.ChangeRemove, Item: boba, Qty: 2}},
		},
		{
			name:    "remove more than held from a stack",
			inv:     setup,
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(acorn, 6) },
			wantErr: world.ErrNotEnoughItems,
		},
		{
			name:    "remove more than held across slots",
			inv:     setup,
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(boba, 3) },
			wantErr: world.ErrNotEnoughItems,
		},
		{
			name:    "remove item not held",
			inv:     setup,
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(pelt, 1) },
			wantErr: world.ErrNotEnoughItems,
		},
		{
			name:    "remove negative",
			inv:     setup,
			op:      func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(acorn, -1) },
			wantErr: world.ErrBadQuantity,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := tt.inv(t)
			before := clone(inv)
			changes, err := tt.op(&inv)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if !reflect.DeepEqual(inv, before) {
					t.Errorf("failed op changed inventory:\n got %+v\nwant %+v", inv, before)
				}
				if changes != nil {
					t.Errorf("failed op returned changes %+v", changes)
				}
				return
			}
			tt.check(t, inv)
			if !reflect.DeepEqual(changes, tt.changes) {
				t.Errorf("changes = %+v, want %+v", changes, tt.changes)
			}
		})
	}
}

func TestGold(t *testing.T) {
	tests := []struct {
		name     string
		start    int
		op       func(*world.Inventory) ([]world.Change, error)
		wantErr  error
		wantGold int
	}{
		{"add", 100, func(inv *world.Inventory) ([]world.Change, error) { return inv.AddGold(25) }, nil, 125},
		{"spend some", 100, func(inv *world.Inventory) ([]world.Change, error) { return inv.SpendGold(40) }, nil, 60},
		{"spend all", 100, func(inv *world.Inventory) ([]world.Change, error) { return inv.SpendGold(100) }, nil, 0},
		{"overspend", 100, func(inv *world.Inventory) ([]world.Change, error) { return inv.SpendGold(101) }, world.ErrNotEnoughGold, 100},
		{"spend from empty", 0, func(inv *world.Inventory) ([]world.Change, error) { return inv.SpendGold(1) }, world.ErrNotEnoughGold, 0},
		{"spend negative", 100, func(inv *world.Inventory) ([]world.Change, error) { return inv.SpendGold(-5) }, world.ErrBadQuantity, 100},
		{"add negative", 100, func(inv *world.Inventory) ([]world.Change, error) { return inv.AddGold(-5) }, world.ErrBadQuantity, 100},
		{"add zero", 100, func(inv *world.Inventory) ([]world.Change, error) { return inv.AddGold(0) }, world.ErrBadQuantity, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := world.Inventory{Gold: tt.start}
			changes, err := tt.op(&inv)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if inv.Gold != tt.wantGold {
				t.Errorf("gold = %d, want %d", inv.Gold, tt.wantGold)
			}
			if err != nil {
				if changes != nil {
					t.Errorf("failed op returned changes %+v", changes)
				}
				return
			}
			want := []world.Change{{Kind: world.ChangeGold, Gold: tt.wantGold - tt.start}}
			if !reflect.DeepEqual(changes, want) {
				t.Errorf("changes = %+v, want %+v", changes, want)
			}
		})
	}
}

func TestRemoveFromSlot(t *testing.T) {
	d := gameData(t)
	acorn, boba := itemID(t, d, "acorn"), itemID(t, d, "boba")
	n := d.Rules.InventorySlots
	tests := []struct {
		name     string
		index    int
		qty      int
		wantErr  error
		wantSlot world.Stack // slot index afterwards, when index is in range
	}{
		{"part of a stack", 2, 3, nil, world.Stack{Item: acorn, Qty: 2}},
		{"whole stack", 2, 5, nil, world.Stack{}},
		{"single item", 0, 1, nil, world.Stack{}},
		{"more than held", 2, 6, world.ErrNotEnoughItems, world.Stack{Item: acorn, Qty: 5}},
		{"zero", 2, 0, world.ErrBadQuantity, world.Stack{Item: acorn, Qty: 5}},
		{"negative", 2, -1, world.ErrBadQuantity, world.Stack{Item: acorn, Qty: 5}},
		{"empty slot", 3, 1, world.ErrEmptySlot, world.Stack{}},
		{"below range", -1, 1, world.ErrBadSlot, world.Stack{}},
		{"above range", n, 1, world.ErrBadSlot, world.Stack{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := world.NewInventory(d)
			inv.Slots[0] = world.Stack{Item: boba, Qty: 1}
			inv.Slots[2] = world.Stack{Item: acorn, Qty: 5}
			before := clone(inv)
			changes, err := inv.RemoveFromSlot(tt.index, tt.qty)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if !reflect.DeepEqual(inv, before) || changes != nil {
					t.Errorf("failed remove changed the inventory or returned changes %+v", changes)
				}
				return
			}
			if inv.Slots[tt.index] != tt.wantSlot {
				t.Errorf("slot = %+v, want %+v", inv.Slots[tt.index], tt.wantSlot)
			}
			want := []world.Change{{Kind: world.ChangeRemove, Item: before.Slots[tt.index].Item, Qty: tt.qty}}
			if !reflect.DeepEqual(changes, want) {
				t.Errorf("changes = %+v, want %+v", changes, want)
			}
		})
	}
}

func TestEquip(t *testing.T) {
	d := gameData(t)
	stick, dagger, lock := itemID(t, d, "wooden_stick"), itemID(t, d, "bronze_dagger"), itemID(t, d, "bike_lock")
	leatherCap, boba, acorn := itemID(t, d, "leather_cap"), itemID(t, d, "boba"), itemID(t, d, "acorn")

	// Slots: 0 dagger, 1 bike lock (level 10), 2 leather cap (level 5), 3 boba, 4 acorn.
	// Wearing the wooden stick.
	setup := func() world.Inventory {
		inv := world.NewInventory(d)
		inv.Slots[0] = world.Stack{Item: dagger, Qty: 1}
		inv.Slots[1] = world.Stack{Item: lock, Qty: 1}
		inv.Slots[2] = world.Stack{Item: leatherCap, Qty: 1}
		inv.Slots[3] = world.Stack{Item: boba, Qty: 1}
		inv.Slots[4] = world.Stack{Item: acorn, Qty: 4}
		inv.Equipment.Weapon = stick
		return inv
	}

	tests := []struct {
		name      string
		index     int
		level     int
		wantErr   error
		wantSlot  world.Stack
		wantEquip world.Equipment
		changes   []world.Change
	}{
		{"swap weapon returns old one to freed slot", 0, 1, nil,
			world.Stack{Item: stick, Qty: 1}, world.Equipment{Weapon: dagger},
			[]world.Change{{Kind: world.ChangeEquip, Item: dagger, Qty: 1}, {Kind: world.ChangeUnequip, Item: stick, Qty: 1}}},
		{"equip into empty slot frees inventory slot", 2, 5, nil,
			world.Stack{}, world.Equipment{Weapon: stick, Head: leatherCap},
			[]world.Change{{Kind: world.ChangeEquip, Item: leatherCap, Qty: 1}}},
		{"level exactly enough", 1, 10, nil,
			world.Stack{Item: stick, Qty: 1}, world.Equipment{Weapon: lock},
			[]world.Change{{Kind: world.ChangeEquip, Item: lock, Qty: 1}, {Kind: world.ChangeUnequip, Item: stick, Qty: 1}}},
		{"level too low", 1, 9, world.ErrLevelTooLow, world.Stack{}, world.Equipment{}, nil},
		{"level too low for armor", 2, 4, world.ErrLevelTooLow, world.Stack{}, world.Equipment{}, nil},
		{"food is not equipment", 3, 30, world.ErrNotEquipment, world.Stack{}, world.Equipment{}, nil},
		{"loot is not equipment", 4, 30, world.ErrNotEquipment, world.Stack{}, world.Equipment{}, nil},
		{"empty slot", 5, 30, world.ErrEmptySlot, world.Stack{}, world.Equipment{}, nil},
		{"slot out of range", 28, 30, world.ErrBadSlot, world.Stack{}, world.Equipment{}, nil},
		{"negative slot", -1, 30, world.ErrBadSlot, world.Stack{}, world.Equipment{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := setup()
			before := clone(inv)
			changes, err := inv.Equip(d, tt.index, tt.level)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if !reflect.DeepEqual(inv, before) || changes != nil {
					t.Errorf("failed equip changed inventory or returned changes %+v", changes)
				}
				return
			}
			if inv.Slots[tt.index] != tt.wantSlot {
				t.Errorf("slot %d = %+v, want %+v", tt.index, inv.Slots[tt.index], tt.wantSlot)
			}
			if inv.Equipment != tt.wantEquip {
				t.Errorf("equipment = %+v, want %+v", inv.Equipment, tt.wantEquip)
			}
			if !reflect.DeepEqual(changes, tt.changes) {
				t.Errorf("changes = %+v, want %+v", changes, tt.changes)
			}
		})
	}
}

func TestUnequip(t *testing.T) {
	d := gameData(t)
	stick, hoodie := itemID(t, d, "wooden_stick"), itemID(t, d, "hoodie")

	tests := []struct {
		name    string
		inv     func() world.Inventory
		slot    gamedata.Slot
		wantErr error
	}{
		{"to first free slot", func() world.Inventory {
			inv := world.NewInventory(d)
			inv.Slots[0] = world.Stack{Item: hoodie, Qty: 1}
			inv.Equipment.Weapon = stick
			return inv
		}, gamedata.SlotWeapon, nil},
		{"inventory full", func() world.Inventory {
			inv := fill(t, d, "boba")
			inv.Equipment.Weapon = stick
			return inv
		}, gamedata.SlotWeapon, world.ErrInventoryFull},
		{"nothing worn", func() world.Inventory {
			inv := world.NewInventory(d)
			inv.Equipment.Weapon = stick
			return inv
		}, gamedata.SlotHead, world.ErrNothingEquipped},
		{"not a slot", func() world.Inventory { return world.NewInventory(d) }, gamedata.SlotNone, world.ErrBadEquipmentSlot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := tt.inv()
			before := clone(inv)
			changes, err := inv.Unequip(tt.slot)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if !reflect.DeepEqual(inv, before) || changes != nil {
					t.Errorf("failed unequip changed inventory or returned changes %+v", changes)
				}
				return
			}
			if inv.Equipment.Weapon != 0 || inv.Slots[1] != (world.Stack{Item: stick, Qty: 1}) {
				t.Errorf("equipment = %+v, slot 1 = %+v", inv.Equipment, inv.Slots[1])
			}
			want := []world.Change{{Kind: world.ChangeUnequip, Item: stick, Qty: 1}}
			if !reflect.DeepEqual(changes, want) {
				t.Errorf("changes = %+v, want %+v", changes, want)
			}
		})
	}
}

func TestRemoveWorn(t *testing.T) {
	d := gameData(t)
	stick, helm := itemID(t, d, "wooden_stick"), itemID(t, d, "leather_cap")

	tests := []struct {
		name    string
		inv     func() world.Inventory
		slot    gamedata.Slot
		wantErr error
	}{
		{"empty inventory", func() world.Inventory {
			inv := world.NewInventory(d)
			inv.Equipment = world.Equipment{Weapon: stick, Head: helm}
			return inv
		}, gamedata.SlotWeapon, nil},
		{"full inventory", func() world.Inventory {
			inv := fill(t, d, "boba")
			inv.Equipment = world.Equipment{Weapon: stick, Head: helm}
			return inv
		}, gamedata.SlotWeapon, nil},
		{"nothing worn", func() world.Inventory {
			inv := world.NewInventory(d)
			inv.Equipment.Weapon = stick
			return inv
		}, gamedata.SlotHead, world.ErrNothingEquipped},
		{"not a slot", func() world.Inventory { return world.NewInventory(d) }, gamedata.SlotNone, world.ErrBadEquipmentSlot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := tt.inv()
			before := clone(inv)
			changes, err := inv.RemoveWorn(tt.slot)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if !reflect.DeepEqual(inv, before) || changes != nil {
					t.Errorf("failed remove changed inventory or returned changes %+v", changes)
				}
				return
			}
			want := before
			want.Equipment.Weapon = 0
			if !reflect.DeepEqual(inv, want) {
				t.Errorf("inventory = %+v, want %+v", inv, want)
			}
			wantCh := []world.Change{{Kind: world.ChangeRemoveWorn, Item: stick, Qty: 1}}
			if !reflect.DeepEqual(changes, wantCh) {
				t.Errorf("changes = %+v, want %+v", changes, wantCh)
			}
		})
	}
}

func TestBonuses(t *testing.T) {
	d := gameData(t)
	id := func(key string) gamedata.ItemID { return itemID(t, d, key) }
	tests := []struct {
		name      string
		eq        world.Equipment
		want      gamedata.Stats
		wantSpeed int
	}{
		{"unarmed and unarmoured", world.Equipment{}, gamedata.Stats{}, 4},
		{"armour but no weapon", world.Equipment{Head: id("bike_helmet"), Body: id("hoodie")},
			gamedata.Stats{Defence: 10}, 4},
		{"starting kit", world.Equipment{Weapon: id("wooden_stick"), Body: id("hoodie")},
			gamedata.Stats{Accuracy: 3, Strength: 2, Defence: 4}, 4},
		{"fully geared", world.Equipment{Weapon: id("bike_lock"), Head: id("bike_helmet"), Body: id("robot_plating"), Shield: id("cardboard_shield")},
			gamedata.Stats{Accuracy: 14 - 2, Strength: 16, Defence: 6 + 14 + 3}, 5},
		{"fast weapon", world.Equipment{Weapon: id("bronze_dagger")}, gamedata.Stats{Accuracy: 8, Strength: 5}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, speed := tt.eq.Bonuses(d)
			if stats != tt.want || speed != tt.wantSpeed {
				t.Errorf("Bonuses = %+v, %d; want %+v, %d", stats, speed, tt.want, tt.wantSpeed)
			}
		})
	}
}

func TestChangeKindString(t *testing.T) {
	tests := []struct {
		k    world.ChangeKind
		want string
	}{
		{world.ChangeCreate, "item_create"},
		{world.ChangeAdd, "item_add"},
		{world.ChangeRemove, "item_remove"},
		{world.ChangeEquip, "item_equip"},
		{world.ChangeUnequip, "item_unequip"},
		{world.ChangeGold, "gold"},
		{world.ChangeRemoveWorn, "item_remove_worn"},
		{world.ChangeBankAdd, "bank_add"},
		{world.ChangeBankRemove, "bank_remove"},
		{0, "ChangeKind(0)"},
		{200, "ChangeKind(200)"},
	}
	for _, tt := range tests {
		if got := tt.k.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", uint8(tt.k), got, tt.want)
		}
	}
}

// BenchmarkBonuses runs per player per combat tick.
func BenchmarkBonuses(b *testing.B) {
	d := gameData(b)
	eq := world.Equipment{Weapon: itemID(b, d, "bike_lock"), Head: itemID(b, d, "bike_helmet"),
		Body: itemID(b, d, "robot_plating"), Shield: itemID(b, d, "cardboard_shield")}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = eq.Bonuses(d)
	}
}

// TestLocked checks that every method that changes items, equipment or gold
// fails with ErrInventoryLocked on a locked inventory, or a clone of one,
// and changes nothing, and that unlocking lets changes through again.
func TestLocked(t *testing.T) {
	d := gameData(t)
	acorn, dagger := itemID(t, d, "acorn"), itemID(t, d, "bronze_dagger")
	ops := []struct {
		name string
		op   func(inv *world.Inventory) ([]world.Change, error)
	}{
		{"Add", func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(d, acorn, 1) }},
		{"Remove", func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(acorn, 1) }},
		{"RemoveAt", func(inv *world.Inventory) ([]world.Change, error) { return inv.RemoveAt(0) }},
		{"RemoveFromSlot", func(inv *world.Inventory) ([]world.Change, error) { return inv.RemoveFromSlot(0, 1) }},
		{"AddGold", func(inv *world.Inventory) ([]world.Change, error) { return inv.AddGold(1) }},
		{"SpendGold", func(inv *world.Inventory) ([]world.Change, error) { return inv.SpendGold(1) }},
		{"Equip", func(inv *world.Inventory) ([]world.Change, error) { return inv.Equip(d, 1, 99) }},
		{"Unequip", func(inv *world.Inventory) ([]world.Change, error) { return inv.Unequip(gamedata.SlotWeapon) }},
		{"RemoveWorn", func(inv *world.Inventory) ([]world.Change, error) { return inv.RemoveWorn(gamedata.SlotWeapon) }},
	}
	for _, tt := range ops {
		t.Run(tt.name, func(t *testing.T) {
			inv, _, err := world.NewStartingInventory(d)
			if err != nil {
				t.Fatal(err)
			}
			inv.Slots[0] = world.Stack{Item: acorn, Qty: 5}
			inv.Slots[1] = world.Stack{Item: dagger, Qty: 1}
			inv.SetLocked(true)
			if !inv.Locked() {
				t.Fatal("not locked after SetLocked")
			}
			before := clone(inv)
			changes, err := tt.op(&inv)
			if !errors.Is(err, world.ErrInventoryLocked) || changes != nil {
				t.Fatalf("locked: changes %v, err %v; want ErrInventoryLocked", changes, err)
			}
			if !reflect.DeepEqual(inv, before) {
				t.Fatalf("locked inventory changed: %+v, want %+v", inv, before)
			}
			cp := inv.Clone()
			if _, err := tt.op(&cp); !errors.Is(err, world.ErrInventoryLocked) {
				t.Fatalf("locked clone: err %v, want ErrInventoryLocked", err)
			}
			inv.SetLocked(false)
			if _, err := tt.op(&inv); err != nil {
				t.Fatalf("unlocked: %v", err)
			}
		})
	}
}

func TestReplaceAt(t *testing.T) {
	d := gameData(t)
	raw, _ := d.ItemByKey("raw_shrimp")
	cooked, _ := d.ItemByKey("shrimp")
	inv := world.NewInventory(d)
	_, _ = inv.Add(d, cooked.ID, 1)
	_, _ = inv.Add(d, raw.ID, 1)
	if _, err := inv.RemoveAt(0); err != nil {
		t.Fatal(err)
	}
	ch, err := inv.ReplaceAt(1, cooked.ID)
	if err != nil || inv.Slots[1].Item != cooked.ID || inv.Slots[0].Item != 0 || len(ch) != 2 {
		t.Fatalf("replace: %v %v %+v", err, ch, inv.Slots[:2])
	}
	if _, err := inv.ReplaceAt(0, raw.ID); err == nil {
		t.Error("replaced an empty slot")
	}
	if _, err := inv.ReplaceAt(99, raw.ID); err == nil {
		t.Error("replaced a missing slot")
	}
	inv.SetLocked(true)
	if _, err := inv.ReplaceAt(1, raw.ID); err == nil || inv.Slots[1].Item != cooked.ID {
		t.Error("replaced in a locked inventory")
	}
}
