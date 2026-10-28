package pvp_test

import (
	"errors"
	"maps"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/world"
)

func TestReasonPvPDeath(t *testing.T) {
	if pvp.ReasonPvPDeath != "pvp_death" {
		t.Errorf("ReasonPvPDeath = %q, want %q", pvp.ReasonPvPDeath, "pvp_death")
	}
}

// TestApplyDeathFullInventory uses a full 28-slot inventory with all four
// equipment slots worn: dropped worn items leave their slots directly, a kept
// worn item stays equipped, and nothing needs a free slot.
func TestApplyDeathFullInventory(t *testing.T) {
	d := loadData(t)
	var (
		stick, helmet = id(t, d, "wooden_stick"), id(t, d, "bike_helmet")
		plating       = id(t, d, "robot_plating")
		shield        = id(t, d, "cardboard_shield")
		trinket, boba = id(t, d, "shiny_trinket"), id(t, d, "boba")
	)
	if d.Rules.InventorySlots != 28 || d.Rules.PvPItemsKept != 3 {
		t.Fatalf("test assumes 28 slots and 3 kept, have %d and %d", d.Rules.InventorySlots, d.Rules.PvPItemsKept)
	}
	inv := world.NewInventory(d)
	inv.Gold = 1234
	inv.Equipment = world.Equipment{Weapon: stick, Head: helmet, Body: plating, Shield: shield}
	inv.Slots[0] = world.Stack{Item: trinket, Qty: 5}
	for i := 1; i < len(inv.Slots); i++ {
		inv.Slots[i] = world.Stack{Item: boba, Qty: 1}
	}

	dropped, changes, err := pvp.ApplyDeath(d, &inv)
	if err != nil {
		t.Fatal(err)
	}

	// Kept: robot plating (150), bike helmet (30), one trinket (25).
	want := world.NewInventory(d)
	want.Gold = 1234
	want.Equipment = world.Equipment{Head: helmet, Body: plating}
	want.Slots[0] = world.Stack{Item: trinket, Qty: 1}
	if !reflect.DeepEqual(inv, want) {
		t.Errorf("after death:\n  %+v\nwant\n  %+v", inv, want)
	}

	wantDropped := []world.Stack{{Item: trinket, Qty: 4}, {Item: shield, Qty: 1}, {Item: stick, Qty: 1}}
	wantChanges := []world.Change{
		{Kind: world.ChangeRemove, Item: trinket, Qty: 4},
		{Kind: world.ChangeRemoveWorn, Item: shield, Qty: 1},
		{Kind: world.ChangeRemoveWorn, Item: stick, Qty: 1},
	}
	for range 27 {
		wantDropped = append(wantDropped, world.Stack{Item: boba, Qty: 1})
		wantChanges = append(wantChanges, world.Change{Kind: world.ChangeRemove, Item: boba, Qty: 1})
	}
	if !reflect.DeepEqual(dropped, wantDropped) {
		t.Errorf("dropped = %+v, want %+v", dropped, wantDropped)
	}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Errorf("changes = %+v, want %+v", changes, wantChanges)
	}
	for _, c := range changes {
		if c.Kind == world.ChangeUnequip || c.Kind == world.ChangeAdd {
			t.Errorf("worn item moved through the inventory: %+v", c)
		}
	}
}

func TestApplyDeath(t *testing.T) {
	d := loadData(t)
	var (
		lock, dagger = id(t, d, "bike_lock"), id(t, d, "bronze_dagger")
		hoodie, pelt = id(t, d, "hoodie"), id(t, d, "coyote_pelt")
		trinket      = id(t, d, "shiny_trinket")
	)
	inv := func(eq world.Equipment, gold int, stacks ...world.Stack) world.Inventory {
		s := make([]world.Stack, d.Rules.InventorySlots)
		copy(s, stacks)
		return world.Inventory{Slots: s, Equipment: eq, Gold: gold}
	}
	tests := []struct {
		name        string
		kept        int // Rules.PvPItemsKept; -1 means the gamedata value
		inv         world.Inventory
		want        world.Inventory
		wantDropped []world.Stack
		wantChanges []world.Change
	}{
		{
			name: "nothing held",
			kept: -1,
			inv:  inv(world.Equipment{}, 50),
			want: inv(world.Equipment{}, 50),
		},
		{
			name: "fewer items than kept count",
			kept: -1,
			inv:  inv(world.Equipment{Body: hoodie}, 9, world.Stack{Item: pelt, Qty: 1}),
			want: inv(world.Equipment{Body: hoodie}, 9, world.Stack{Item: pelt, Qty: 1}),
		},
		{
			name:        "keep none: everything but gold dropped",
			kept:        0,
			inv:         inv(world.Equipment{Weapon: lock}, 3, world.Stack{Item: trinket, Qty: 2}),
			want:        inv(world.Equipment{}, 3),
			wantDropped: []world.Stack{{Item: lock, Qty: 1}, {Item: trinket, Qty: 2}},
			wantChanges: []world.Change{
				{Kind: world.ChangeRemoveWorn, Item: lock, Qty: 1},
				{Kind: world.ChangeRemove, Item: trinket, Qty: 2},
			},
		},
		{
			name: "carried item kept, worn item dropped",
			kept: 1,
			inv: inv(world.Equipment{Weapon: dagger}, 0,
				world.Stack{}, world.Stack{Item: lock, Qty: 1}, world.Stack{Item: pelt, Qty: 3}),
			want:        inv(world.Equipment{}, 0, world.Stack{}, world.Stack{Item: lock, Qty: 1}),
			wantDropped: []world.Stack{{Item: dagger, Qty: 1}, {Item: pelt, Qty: 3}},
			wantChanges: []world.Change{
				{Kind: world.ChangeRemoveWorn, Item: dagger, Qty: 1},
				{Kind: world.ChangeRemove, Item: pelt, Qty: 3},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dd := d
			if tt.kept >= 0 {
				dd = withRules(d, func(r *gamedata.Rules) { r.PvPItemsKept = tt.kept })
			}
			got := clone(tt.inv)
			dropped, changes, err := pvp.ApplyDeath(dd, &got)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("after death:\n  %+v\nwant\n  %+v", got, tt.want)
			}
			if len(dropped) != 0 || len(tt.wantDropped) != 0 {
				if !reflect.DeepEqual(dropped, tt.wantDropped) {
					t.Errorf("dropped = %+v, want %+v", dropped, tt.wantDropped)
				}
			}
			if len(changes) != 0 || len(tt.wantChanges) != 0 {
				if !reflect.DeepEqual(changes, tt.wantChanges) {
					t.Errorf("changes = %+v, want %+v", changes, tt.wantChanges)
				}
			}
		})
	}
}

// holdings counts every unit of every item held, worn or carried.
func holdings(inv world.Inventory) map[gamedata.ItemID]int {
	m := map[gamedata.ItemID]int{}
	for _, id := range []gamedata.ItemID{inv.Equipment.Weapon, inv.Equipment.Head, inv.Equipment.Body, inv.Equipment.Shield} {
		if id != 0 {
			m[id]++
		}
	}
	for _, s := range inv.Slots {
		if !s.Empty() {
			m[s.Item] += s.Qty
		}
	}
	return m
}

// TestApplyDeathConserves checks, over many random holdings, that afterwards
// the inventory holds exactly the split's kept pieces where they were, the
// gold balance is unchanged, the kept holdings plus the dropped stacks equal
// the holdings before death item by item, and there is one removal change
// per dropped piece with matching item and quantity.
func TestApplyDeathConserves(t *testing.T) {
	d := loadData(t)
	r := rand.New(rand.NewPCG(2, 201))
	slots := []gamedata.Slot{gamedata.SlotWeapon, gamedata.SlotHead, gamedata.SlotBody, gamedata.SlotShield}
	for n := range 2000 {
		inv := world.Inventory{Slots: make([]world.Stack, d.Rules.InventorySlots), Gold: r.IntN(10_000)}
		for _, s := range slots {
			if r.IntN(4) == 0 {
				continue
			}
			for {
				it := d.Items[r.IntN(len(d.Items))]
				if it.Slot == s {
					*equip(&inv.Equipment, s) = it.ID
					break
				}
			}
		}
		for i := range inv.Slots {
			if r.IntN(4) != 0 {
				it := d.Items[r.IntN(len(d.Items))]
				qty := 1
				if it.Stackable() {
					qty = 1 + r.IntN(20)
				}
				inv.Slots[i] = world.Stack{Item: it.ID, Qty: qty}
			}
		}
		before := clone(inv)
		split := pvp.SplitOnDeath(d, before)

		dropped, changes, err := pvp.ApplyDeath(d, &inv)
		if err != nil {
			t.Fatalf("case %d: %v", n, err)
		}

		// Exactly the kept pieces remain, in place.
		want := world.Inventory{Slots: make([]world.Stack, len(before.Slots)), Gold: before.Gold}
		for _, p := range split.Kept {
			if p.Worn != gamedata.SlotNone {
				*equip(&want.Equipment, p.Worn) = p.Item
			} else {
				want.Slots[p.Slot] = world.Stack{Item: p.Item, Qty: p.Qty}
			}
		}
		if !reflect.DeepEqual(inv, want) {
			t.Fatalf("case %d: after death\n  %+v\nwant kept pieces only\n  %+v", n, inv, want)
		}
		if inv.Gold != before.Gold {
			t.Fatalf("case %d: gold %d, want %d", n, inv.Gold, before.Gold)
		}

		// Kept plus dropped equals before, item by item.
		total := holdings(inv)
		for _, s := range dropped {
			total[s.Item] += s.Qty
		}
		if b := holdings(before); !maps.Equal(total, b) {
			t.Fatalf("case %d: kept+dropped %v, before %v", n, total, b)
		}

		// One removal per dropped piece, matching it.
		if len(changes) != len(split.Dropped) || len(dropped) != len(split.Dropped) {
			t.Fatalf("case %d: %d changes, %d dropped stacks for %d dropped pieces",
				n, len(changes), len(dropped), len(split.Dropped))
		}
		for i, p := range split.Dropped {
			kind := world.ChangeRemove
			if p.Worn != gamedata.SlotNone {
				kind = world.ChangeRemoveWorn
			}
			if wc := (world.Change{Kind: kind, Item: p.Item, Qty: p.Qty}); changes[i] != wc {
				t.Fatalf("case %d: change %d = %+v, want %+v", n, i, changes[i], wc)
			}
			if ws := (world.Stack{Item: p.Item, Qty: p.Qty}); dropped[i] != ws {
				t.Fatalf("case %d: dropped %d = %+v, want %+v", n, i, dropped[i], ws)
			}
		}
	}
}

// TestApplyDeathLocked checks that a locked inventory is refused with
// world.ErrInventoryLocked and left exactly as it was.
func TestApplyDeathLocked(t *testing.T) {
	d := loadData(t)
	inv := world.NewInventory(d)
	inv.Gold = 77
	inv.Equipment = world.Equipment{Weapon: id(t, d, "bike_lock"), Shield: id(t, d, "cardboard_shield")}
	for i := range inv.Slots {
		inv.Slots[i] = world.Stack{Item: id(t, d, "burrito"), Qty: 1}
	}
	inv.SetLocked(true)
	before := clone(inv)

	dropped, changes, err := pvp.ApplyDeath(d, &inv)
	if !errors.Is(err, world.ErrInventoryLocked) {
		t.Fatalf("err = %v, want ErrInventoryLocked", err)
	}
	if dropped != nil || changes != nil {
		t.Errorf("dropped %+v, changes %+v; want none", dropped, changes)
	}
	if !reflect.DeepEqual(inv, before) {
		t.Errorf("locked inventory changed:\n  %+v\nwant\n  %+v", inv, before)
	}
}

func BenchmarkApplyDeath(b *testing.B) {
	d := loadData(b)
	base := world.NewInventory(d)
	base.Equipment = world.Equipment{Weapon: id(b, d, "bike_lock"), Head: id(b, d, "bike_helmet")}
	for i := range base.Slots {
		base.Slots[i] = world.Stack{Item: d.Items[i%len(d.Items)].ID, Qty: 1}
	}
	b.ReportAllocs()
	for b.Loop() {
		inv := clone(base)
		_, _, _ = pvp.ApplyDeath(d, &inv)
	}
}
