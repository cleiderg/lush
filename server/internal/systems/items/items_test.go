package items_test

import (
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/world"
)

func gameData(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
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

// xpForLevel returns the least XP that gives combat level level.
func xpForLevel(t testing.TB, r gamedata.Rules, level int) int {
	t.Helper()
	for xp := 0; xp < 1_000_000; xp++ {
		if world.CombatLevelForXP(r, xp) >= level {
			return xp
		}
	}
	t.Fatalf("no XP reaches level %d", level)
	return 0
}

// newPlayer returns a level-1 player at full HP with the starting kit
// (stick and hoodie worn, Boba in slots 0-2).
func newPlayer(t testing.TB, d *gamedata.Data) *world.Player {
	t.Helper()
	inv, _, err := world.NewStartingInventory(d)
	if err != nil {
		t.Fatal(err)
	}
	p := &world.Player{ID: 1, Inv: inv}
	p.HP = p.MaxHP(d.Rules)
	return p
}

// handle runs cmd through the system on a one-player world tick.
func handle(t testing.TB, sys *items.System, p *world.Player, cmd world.Command) {
	t.Helper()
	w := world.New(world.DefaultConfig())
	if err := w.Register(sys); err != nil {
		t.Fatal(err)
	}
	if err := w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	w.Enqueue(cmd)
	w.Tick()
}

func TestActions(t *testing.T) {
	d := gameData(t)
	stick, dagger, hoodie := itemID(t, d, "wooden_stick"), itemID(t, d, "bronze_dagger"), itemID(t, d, "hoodie")
	shield, boba := itemID(t, d, "cardboard_shield"), itemID(t, d, "boba")

	tests := []struct {
		name        string
		setup       func(p *world.Player)
		cmd         world.Command
		reason      string
		wantSlots   map[int]world.Stack // slots expected to differ from the starting kit
		wantEquip   world.Equipment
		wantChanges []world.Change
	}{
		{
			name:        "equip into empty slot",
			setup:       func(p *world.Player) { p.Inv.Slots[5] = world.Stack{Item: shield, Qty: 1} },
			cmd:         items.EquipCommand{Player: 1, Slot: 5},
			reason:      items.ReasonEquip,
			wantSlots:   map[int]world.Stack{5: {}},
			wantEquip:   world.Equipment{Weapon: stick, Body: hoodie, Shield: shield},
			wantChanges: []world.Change{{Kind: world.ChangeEquip, Item: shield, Qty: 1}},
		},
		{
			name:      "equip swaps the worn item into the freed slot",
			setup:     func(p *world.Player) { p.Inv.Slots[5] = world.Stack{Item: dagger, Qty: 1} },
			cmd:       items.EquipCommand{Player: 1, Slot: 5},
			reason:    items.ReasonEquip,
			wantSlots: map[int]world.Stack{5: {Item: stick, Qty: 1}},
			wantEquip: world.Equipment{Weapon: dagger, Body: hoodie},
			wantChanges: []world.Change{
				{Kind: world.ChangeEquip, Item: dagger, Qty: 1},
				{Kind: world.ChangeUnequip, Item: stick, Qty: 1},
			},
		},
		{
			name:        "unequip into the first free slot",
			cmd:         items.UnequipCommand{Player: 1, Slot: gamedata.SlotWeapon},
			reason:      items.ReasonUnequip,
			wantSlots:   map[int]world.Stack{3: {Item: stick, Qty: 1}},
			wantEquip:   world.Equipment{Body: hoodie},
			wantChanges: []world.Change{{Kind: world.ChangeUnequip, Item: stick, Qty: 1}},
		},
		{
			name:        "eat removes the food from that slot",
			setup:       func(p *world.Player) { p.HP = 1 },
			cmd:         items.EatCommand{Player: 1, Slot: 1},
			reason:      items.ReasonEat,
			wantSlots:   map[int]world.Stack{1: {}},
			wantEquip:   world.Equipment{Weapon: stick, Body: hoodie},
			wantChanges: []world.Change{{Kind: world.ChangeRemove, Item: boba, Qty: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayer(t, d)
			if tt.setup != nil {
				tt.setup(p)
			}
			wantSlots := append([]world.Stack(nil), p.Inv.Slots...)
			for i, s := range tt.wantSlots {
				wantSlots[i] = s
			}
			handle(t, items.New(d), p, tt.cmd)

			if !reflect.DeepEqual(p.Inv.Slots, wantSlots) {
				t.Errorf("slots = %+v, want %+v", p.Inv.Slots, wantSlots)
			}
			if p.Inv.Equipment != tt.wantEquip {
				t.Errorf("equipment = %+v, want %+v", p.Inv.Equipment, tt.wantEquip)
			}
			events := p.DrainEconomy()
			if len(events) != 1 {
				t.Fatalf("outbox has %d events, want 1: %+v", len(events), events)
			}
			ev := events[0]
			if ev.Reason != tt.reason {
				t.Errorf("reason = %q, want %q", ev.Reason, tt.reason)
			}
			if !reflect.DeepEqual(ev.Changes, tt.wantChanges) {
				t.Errorf("changes = %+v, want %+v", ev.Changes, tt.wantChanges)
			}
			if !reflect.DeepEqual(ev.Inv, p.Inv) {
				t.Errorf("event inventory = %+v, want the player's %+v", ev.Inv, p.Inv)
			}
		})
	}
}

func TestEquipAtLevel(t *testing.T) {
	d := gameData(t)
	lock := itemID(t, d, "bike_lock") // level to equip 10
	p := newPlayer(t, d)
	p.XP = xpForLevel(t, d.Rules, 10)
	p.Inv.Slots[5] = world.Stack{Item: lock, Qty: 1}
	handle(t, items.New(d), p, items.EquipCommand{Player: 1, Slot: 5})
	if p.Inv.Equipment.Weapon != lock {
		t.Errorf("weapon = %d, want bike lock %d at level 10", p.Inv.Equipment.Weapon, lock)
	}
	if n := len(p.DrainEconomy()); n != 1 {
		t.Errorf("outbox events = %d, want 1", n)
	}
}

func TestInvalidRequestsChangeNothing(t *testing.T) {
	d := gameData(t)
	lock, acorn, dagger := itemID(t, d, "bike_lock"), itemID(t, d, "acorn"), itemID(t, d, "bronze_dagger")
	n := d.Rules.InventorySlots

	tests := []struct {
		name  string
		setup func(p *world.Player)
		cmd   world.Command
	}{
		{"equip slot below range", nil, items.EquipCommand{Player: 1, Slot: -1}},
		{"equip slot above range", nil, items.EquipCommand{Player: 1, Slot: n}},
		{"equip empty slot", nil, items.EquipCommand{Player: 1, Slot: 10}},
		{"equip food", nil, items.EquipCommand{Player: 1, Slot: 0}},
		{"equip loot", func(p *world.Player) { p.Inv.Slots[5] = world.Stack{Item: acorn, Qty: 4} }, items.EquipCommand{Player: 1, Slot: 5}},
		{"equip above combat level", func(p *world.Player) { p.Inv.Slots[5] = world.Stack{Item: lock, Qty: 1} }, items.EquipCommand{Player: 1, Slot: 5}},
		{"eat slot below range", nil, items.EatCommand{Player: 1, Slot: -1}},
		{"eat slot above range", nil, items.EatCommand{Player: 1, Slot: n}},
		{"eat empty slot", nil, items.EatCommand{Player: 1, Slot: 10}},
		{"eat equipment", func(p *world.Player) { p.Inv.Slots[5] = world.Stack{Item: dagger, Qty: 1} }, items.EatCommand{Player: 1, Slot: 5}},
		{"eat loot", func(p *world.Player) { p.Inv.Slots[5] = world.Stack{Item: acorn, Qty: 4} }, items.EatCommand{Player: 1, Slot: 5}},
		{"unequip empty slot", nil, items.UnequipCommand{Player: 1, Slot: gamedata.SlotHead}},
		{"unequip unknown slot", nil, items.UnequipCommand{Player: 1, Slot: "feet"}},
		{"unequip into full inventory", func(p *world.Player) {
			for i := range p.Inv.Slots {
				p.Inv.Slots[i] = world.Stack{Item: dagger, Qty: 1}
			}
		}, items.UnequipCommand{Player: 1, Slot: gamedata.SlotWeapon}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayer(t, d)
			p.HP = 1
			if tt.setup != nil {
				tt.setup(p)
			}
			before := *p
			before.Inv = p.Inv.Clone()
			handle(t, items.New(d), p, tt.cmd)

			if !reflect.DeepEqual(p.Inv, before.Inv) {
				t.Errorf("inventory changed: %+v, was %+v", p.Inv, before.Inv)
			}
			if p.HP != before.HP || p.AttackDelay != before.AttackDelay {
				t.Errorf("HP %d, attack delay %d; was %d, %d", p.HP, p.AttackDelay, before.HP, before.AttackDelay)
			}
			if ev := p.DrainEconomy(); len(ev) != 0 {
				t.Errorf("outbox = %+v, want empty", ev)
			}
		})
	}
}

func TestEat(t *testing.T) {
	d := gameData(t)
	boba, burrito := itemID(t, d, "boba"), itemID(t, d, "burrito")
	maxHP := world.MaxHPForLevel(d.Rules, d.Rules.MinCombatLevel)
	heals := map[gamedata.ItemID]int{}
	for _, id := range []gamedata.ItemID{boba, burrito} {
		it, _ := d.Item(id)
		heals[id] = it.Heals
	}

	tests := []struct {
		name   string
		food   gamedata.ItemID
		hp     int
		wantHP int
	}{
		{"boba heals its full value", boba, 1, 1 + heals[boba]},
		{"burrito heals its full value", burrito, 0, heals[burrito]},
		{"heal stops at max HP", burrito, maxHP - 2, maxHP},
		{"eating at max HP heals nothing", boba, maxHP, maxHP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantHP > maxHP {
				t.Fatalf("test case wants %d HP above max %d", tt.wantHP, maxHP)
			}
			p := newPlayer(t, d)
			p.HP = tt.hp
			p.Inv.Slots[7] = world.Stack{Item: tt.food, Qty: 1}
			count := p.Inv.Count(tt.food)
			handle(t, items.New(d), p, items.EatCommand{Player: 1, Slot: 7})

			if p.HP != tt.wantHP {
				t.Errorf("HP = %d, want %d", p.HP, tt.wantHP)
			}
			if !p.Inv.Slots[7].Empty() {
				t.Errorf("slot 7 = %+v, want empty", p.Inv.Slots[7])
			}
			if got := p.Inv.Count(tt.food); got != count-1 {
				t.Errorf("food count = %d, want %d", got, count-1)
			}
			if p.AttackDelay != d.Rules.EatAttackDelayTicks {
				t.Errorf("attack delay = %d, want %d", p.AttackDelay, d.Rules.EatAttackDelayTicks)
			}
		})
	}
}

// TestEatDelayFromRules shows the attack delay is read from the rules data.
func TestEatDelayFromRules(t *testing.T) {
	d := gameData(t)
	custom := *d
	custom.Rules.EatAttackDelayTicks = 7
	p := newPlayer(t, &custom)
	handle(t, items.New(&custom), p, items.EatCommand{Player: 1, Slot: 0})
	if p.AttackDelay != 7 {
		t.Errorf("attack delay = %d, want 7 from the rules", p.AttackDelay)
	}
}
