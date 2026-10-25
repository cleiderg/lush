package gamedata_test

import (
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

func load(t *testing.T) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	return d
}

const none = -1 // "—" in the spec: the shop doesn't sell it

// TestItemsMatchSpec checks every row of the Items table in docs/specs/gameplay.md.
func TestItemsMatchSpec(t *testing.T) {
	d := load(t)
	tests := []struct {
		name      string
		kind      gamedata.ItemKind
		slot      gamedata.Slot
		acc, str  int
		def       int
		speed     int
		level     int
		buys      int
		sells     int
		heals     int
		stackable bool
	}{
		{"Wooden stick", gamedata.KindWeapon, gamedata.SlotWeapon, 3, 2, 0, 4, 1, 2, 5, 0, false},
		{"Bronze dagger", gamedata.KindWeapon, gamedata.SlotWeapon, 8, 5, 0, 3, 1, 15, 40, 0, false},
		{"Bike lock", gamedata.KindWeapon, gamedata.SlotWeapon, 14, 16, 0, 5, 10, 60, none, 0, false},
		{"Hoodie", gamedata.KindArmor, gamedata.SlotBody, 0, 0, 4, 0, 1, 5, 15, 0, false},
		{"Cardboard shield", gamedata.KindArmor, gamedata.SlotShield, 0, 0, 3, 0, 1, 3, 10, 0, false},
		{"Leather cap", gamedata.KindArmor, gamedata.SlotHead, 0, 0, 3, 0, 5, 10, none, 0, false},
		{"Bike helmet", gamedata.KindArmor, gamedata.SlotHead, 0, 0, 6, 0, 10, 30, 80, 0, false},
		{"Robot plating", gamedata.KindArmor, gamedata.SlotBody, -2, 0, 14, 0, 12, 150, none, 0, false},
		{"Boba", gamedata.KindFood, gamedata.SlotNone, 0, 0, 0, 0, 1, 1, 5, 4, false},
		{"Burrito", gamedata.KindFood, gamedata.SlotNone, 0, 0, 0, 0, 1, 3, 15, 10, false},
		{"Acorn", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 1, none, 0, true},
		{"Shiny trinket", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 25, none, 0, true},
		{"Coyote pelt", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 12, none, 0, true},
		{"Scrap metal", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 8, none, 0, true},
	}
	if len(d.Items) != len(tests) {
		t.Fatalf("got %d items, spec has %d", len(d.Items), len(tests))
	}
	byName := map[string]gamedata.Item{}
	for _, it := range d.Items {
		byName[it.Name] = it
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it, ok := byName[tt.name]
			if !ok {
				t.Fatalf("missing item %q", tt.name)
			}
			if it.Kind != tt.kind || it.Slot != tt.slot {
				t.Errorf("kind/slot = %s/%q, want %s/%q", it.Kind, it.Slot, tt.kind, tt.slot)
			}
			if want := (gamedata.Stats{Accuracy: tt.acc, Strength: tt.str, Defence: tt.def}); it.Stats != want {
				t.Errorf("stats = %+v, want %+v", it.Stats, want)
			}
			if it.AttackSpeed != tt.speed || it.LevelToEquip != tt.level || it.Heals != tt.heals {
				t.Errorf("speed/level/heals = %d/%d/%d, want %d/%d/%d",
					it.AttackSpeed, it.LevelToEquip, it.Heals, tt.speed, tt.level, tt.heals)
			}
			if it.ShopBuysFor != tt.buys {
				t.Errorf("shop buys for %d, want %d", it.ShopBuysFor, tt.buys)
			}
			price, sold := d.ShopSellPrice(it.ID)
			if tt.sells == none && sold {
				t.Errorf("shop sells for %d, want not sold", price)
			}
			if tt.sells != none && (!sold || price != tt.sells) {
				t.Errorf("shop sells for %d (sold %v), want %d", price, sold, tt.sells)
			}
			if it.Stackable() != tt.stackable {
				t.Errorf("stackable = %v, want %v", it.Stackable(), tt.stackable)
			}
		})
	}
}

type drop struct {
	item     string
	chance   int // 1 in chance
	quantity int
}

// TestNPCsMatchSpec checks every row of the NPCs table in docs/specs/gameplay.md.
func TestNPCsMatchSpec(t *testing.T) {
	d := load(t)
	tests := []struct {
		name             string
		area             gamedata.Area
		level, hp        int
		acc, str, def    int
		speed            int
		behaviour        gamedata.Behaviour
		respawn          time.Duration
		goldMin, goldMax int
		drops            []drop
		maxAlive         int
	}{
		{"Rabbit", gamedata.AreaAldrichPark, 1, 3, 0, 0, 0, 4, gamedata.Passive, 15 * time.Second, 1, 3, nil, 0},
		{"Squirrel", gamedata.AreaAldrichPark, 2, 5, 2, 0, 2, 4, gamedata.Passive, 20 * time.Second, 2, 6,
			[]drop{{"Acorn", 2, 1}}, 0},
		{"Crow", gamedata.AreaAldrichPark, 4, 8, 5, 3, 3, 4, gamedata.Passive, 30 * time.Second, 3, 10,
			[]drop{{"Shiny trinket", 8, 1}}, 0},
		{"Coyote", gamedata.AreaSouthFields, 9, 20, 12, 10, 8, 4, gamedata.Aggressive, 45 * time.Second, 8, 25,
			[]drop{{"Coyote pelt", 2, 1}, {"Leather cap", 20, 1}}, 0},
		{"Rogue delivery robot", gamedata.AreaSouthFields, 15, 35, 20, 18, 25, 5, gamedata.Aggressive, 60 * time.Second, 20, 50,
			[]drop{{"Scrap metal", 2, 1}, {"Bike lock", 32, 1}, {"Robot plating", 64, 1}}, 0},
		{"Coyote alpha", gamedata.AreaSouthFields, 20, 50, 28, 26, 22, 4, gamedata.Aggressive, 3 * time.Minute, 40, 100,
			[]drop{{"Coyote pelt", 1, 3}, {"Bike helmet", 8, 1}}, 1},
	}
	if len(d.NPCs) != len(tests) {
		t.Fatalf("got %d NPC types, spec has %d", len(d.NPCs), len(tests))
	}
	byName := map[string]gamedata.NPCType{}
	for _, n := range d.NPCs {
		byName[n.Name] = n
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := byName[tt.name]
			if !ok {
				t.Fatalf("missing NPC %q", tt.name)
			}
			if n.Area != tt.area || n.Level != tt.level || n.HP != tt.hp {
				t.Errorf("area/level/hp = %s/%d/%d, want %s/%d/%d", n.Area, n.Level, n.HP, tt.area, tt.level, tt.hp)
			}
			if want := (gamedata.Stats{Accuracy: tt.acc, Strength: tt.str, Defence: tt.def}); n.Stats != want {
				t.Errorf("stats = %+v, want %+v", n.Stats, want)
			}
			if n.AttackSpeed != tt.speed || n.Behaviour != tt.behaviour || n.Respawn() != tt.respawn {
				t.Errorf("speed/behaviour/respawn = %d/%s/%v, want %d/%s/%v",
					n.AttackSpeed, n.Behaviour, n.Respawn(), tt.speed, tt.behaviour, tt.respawn)
			}
			if n.GoldMin != tt.goldMin || n.GoldMax != tt.goldMax {
				t.Errorf("gold = %d to %d, want %d to %d", n.GoldMin, n.GoldMax, tt.goldMin, tt.goldMax)
			}
			if n.MaxAlive != tt.maxAlive {
				t.Errorf("max alive = %d, want %d", n.MaxAlive, tt.maxAlive)
			}
			if len(n.Drops) != len(tt.drops) {
				t.Fatalf("got %d drops, want %d", len(n.Drops), len(tt.drops))
			}
			for i, want := range tt.drops {
				got := n.Drops[i]
				it, ok := d.Item(got.Item)
				if !ok {
					t.Fatalf("drop %d: item id %d not found", i, got.Item)
				}
				if it.Name != want.item || got.Chance != want.chance || got.Quantity != want.quantity {
					t.Errorf("drop %d = %s 1 in %d ×%d, want %s 1 in %d ×%d",
						i, it.Name, got.Chance, got.Quantity, want.item, want.chance, want.quantity)
				}
			}
		})
	}
}

// TestIDsAndKeysUnique checks every item and NPC type has a distinct id and key.
func TestIDsAndKeysUnique(t *testing.T) {
	d := load(t)
	itemIDs, itemKeys := map[gamedata.ItemID]bool{}, map[string]bool{}
	for _, it := range d.Items {
		if it.ID == 0 || itemIDs[it.ID] || it.Key == "" || itemKeys[it.Key] {
			t.Errorf("item %q: id %d or key %q is zero, empty or repeated", it.Name, it.ID, it.Key)
		}
		itemIDs[it.ID], itemKeys[it.Key] = true, true
		if got, ok := d.Item(it.ID); !ok || got.Key != it.Key {
			t.Errorf("Item(%d) = %q, %v", it.ID, got.Key, ok)
		}
		if got, ok := d.ItemByKey(it.Key); !ok || got.ID != it.ID {
			t.Errorf("ItemByKey(%q) = %d, %v", it.Key, got.ID, ok)
		}
	}
	npcIDs, npcKeys := map[gamedata.NPCTypeID]bool{}, map[string]bool{}
	for _, n := range d.NPCs {
		if n.ID == 0 || npcIDs[n.ID] || n.Key == "" || npcKeys[n.Key] {
			t.Errorf("NPC %q: id %d or key %q is zero, empty or repeated", n.Name, n.ID, n.Key)
		}
		npcIDs[n.ID], npcKeys[n.Key] = true, true
		if got, ok := d.NPC(n.ID); !ok || got.Key != n.Key {
			t.Errorf("NPC(%d) = %q, %v", n.ID, got.Key, ok)
		}
		if got, ok := d.NPCByKey(n.Key); !ok || got.ID != n.ID {
			t.Errorf("NPCByKey(%q) = %d, %v", n.Key, got.ID, ok)
		}
	}
}

// TestRulesMatchSpec checks the rule constants in docs/specs/gameplay.md.
func TestRulesMatchSpec(t *testing.T) {
	r := load(t).Rules
	tests := []struct {
		name      string
		got, want any
	}{
		{"inventory slots", r.InventorySlots, 28},
		{"starting gold", r.StartingGold, 100},
		{"attack range tiles", r.AttackRangeTiles, 1},
		{"attack range includes diagonals", r.AttackRangeDiagonal, true},
		{"unarmed attack speed", r.UnarmedAttackSpeed, 4},
		{"min combat level", r.MinCombatLevel, 1},
		{"max combat level", r.MaxCombatLevel, 30},
		{"base HP", r.BaseHP, 9},
		{"XP per damage", r.XPPerDamage, 4},
		{"regen HP", r.RegenHP, 1},
		{"regen interval", r.RegenInterval(), 6 * time.Second},
		{"regen delay", r.RegenDelay(), 10 * time.Second},
		{"aggression range tiles", r.AggressionRangeTiles, 4},
		{"leash tiles", r.LeashTiles, 12},
		{"PvP level gap", r.PvPLevelGap, 5},
		{"shop distance tiles", r.ShopDistanceTiles, 2},
		{"trade distance tiles", r.TradeDistanceTiles, 3},
		{"PvP items kept", r.PvPItemsKept, 3},
		{"drop reservation", r.DropReservation(), 60 * time.Second},
		{"drop despawn", r.DropDespawn(), 3 * time.Minute},
		{"logout combat lock", r.LogoutCombatLock(), 10 * time.Second},
		{"eat attack delay combat ticks", r.EatAttackDelayTicks, 1},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestStartingKit(t *testing.T) {
	d := load(t)
	kit := d.Rules.StartingKit
	var equipped []string
	for _, id := range kit.Equipped {
		it, ok := d.Item(id)
		if !ok {
			t.Fatalf("equipped id %d not found", id)
		}
		equipped = append(equipped, it.Name)
	}
	if len(equipped) != 2 || equipped[0] != "Wooden stick" || equipped[1] != "Hoodie" {
		t.Errorf("equipped = %v, want [Wooden stick Hoodie]", equipped)
	}
	if len(kit.Inventory) != 1 {
		t.Fatalf("inventory = %+v, want 3 Boba", kit.Inventory)
	}
	if it, _ := d.Item(kit.Inventory[0].Item); it.Name != "Boba" || kit.Inventory[0].Quantity != 3 {
		t.Errorf("inventory = %d × %q, want 3 × Boba", kit.Inventory[0].Quantity, it.Name)
	}
}
