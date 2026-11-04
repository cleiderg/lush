package gamedata_test

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
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
		// docs/specs/skills.md; nothing from the skills stacks (Q2, #441).
		{"Bronze hatchet", gamedata.KindWeapon, gamedata.SlotWeapon, 4, 4, 0, 5, 1, 10, 30, 0, false},
		{"Steel hatchet", gamedata.KindWeapon, gamedata.SlotWeapon, 8, 9, 0, 5, 5, 70, 200, 0, false},
		{"Small net", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 5, 20, 0, false},
		{"Fishing rod", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 12, 40, 0, false},
		{"Logs", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 2, none, 0, false},
		{"Oak logs", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 6, none, 0, false},
		{"Eucalyptus logs", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 15, none, 0, false},
		{"Raw shrimp", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 1, none, 0, false},
		{"Raw sardine", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 2, none, 0, false},
		{"Raw trout", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 8, none, 0, false},
		{"Shrimp", gamedata.KindFood, gamedata.SlotNone, 0, 0, 0, 0, 1, 2, 6, 3, false},
		{"Sardine", gamedata.KindFood, gamedata.SlotNone, 0, 0, 0, 0, 1, 3, none, 4, false},
		{"Trout", gamedata.KindFood, gamedata.SlotNone, 0, 0, 0, 0, 1, 10, none, 7, false},
		{"Burnt fish", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 0, none, 0, false},
		// docs/specs/quests.md: More items, and the quest reward and quest items.
		{"Steel sword", gamedata.KindWeapon, gamedata.SlotWeapon, 24, 26, 0, 4, 20, 200, none, 0, false},
		{"Lab coat", gamedata.KindArmor, gamedata.SlotBody, 0, 0, 20, 0, 22, 250, none, 0, false},
		{"Watchman's visor", gamedata.KindArmor, gamedata.SlotHead, 2, 0, 9, 0, 25, 220, none, 0, false},
		{"Steel shield", gamedata.KindArmor, gamedata.SlotShield, 0, 0, 10, 0, 15, 60, 160, 0, false},
		{"Feather", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 1, none, 0, true},
		{"Goose egg", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 30, none, 0, true},
		{"Lithium cell", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 20, none, 0, true},
		{"Library card", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 0, none, 0, true},
		{"Ranger's machete", gamedata.KindWeapon, gamedata.SlotWeapon, 20, 22, 0, 4, 18, 120, none, 0, false},
		{"Chef's hat", gamedata.KindArmor, gamedata.SlotHead, 0, 0, 2, 0, 0, 5, none, 0, false},
		{"Overdue book", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 0, none, 0, true},
		{"Alpha's fang", gamedata.KindLoot, gamedata.SlotNone, 0, 0, 0, 0, 0, 0, none, 0, true},
	}
	// Housing items are checked in housing_test.go.
	byName := map[string]gamedata.Item{}
	for _, it := range d.Items {
		switch it.Kind {
		case gamedata.KindFurniture, gamedata.KindWallpaper, gamedata.KindFloorCovering:
			continue
		}
		byName[it.Name] = it
	}
	if len(byName) != len(tests) {
		t.Fatalf("got %d items, spec has %d", len(byName), len(tests))
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
		population       int
	}{
		{"Rabbit", gamedata.AreaAldrichPark, 1, 3, 0, 0, 0, 4, gamedata.Passive, 15 * time.Second, 1, 3, nil, 0, 12},
		{"Squirrel", gamedata.AreaAldrichPark, 2, 5, 2, 0, 2, 4, gamedata.Passive, 20 * time.Second, 2, 6,
			[]drop{{"Acorn", 2, 1}}, 0, 10},
		{"Crow", gamedata.AreaAldrichPark, 4, 8, 5, 3, 3, 4, gamedata.Passive, 30 * time.Second, 3, 10,
			[]drop{{"Shiny trinket", 8, 1}, {"Coffee mug", 64, 1}}, 0, 8},
		{"Coyote", gamedata.AreaSouthFields, 9, 20, 12, 10, 8, 4, gamedata.Aggressive, 45 * time.Second, 8, 25,
			[]drop{{"Coyote pelt", 2, 1}, {"Leather cap", 20, 1}, {"Potted plant", 128, 1}}, 0, 8},
		{"Rogue delivery robot", gamedata.AreaSouthFields, 15, 35, 20, 18, 25, 5, gamedata.Aggressive, 60 * time.Second, 20, 50,
			[]drop{{"Scrap metal", 2, 1}, {"Bike lock", 32, 1}, {"Robot plating", 64, 1}, {"Laptop", 256, 1}}, 0, 5},
		{"Coyote alpha", gamedata.AreaSouthFields, 20, 50, 28, 26, 22, 4, gamedata.Aggressive, 3 * time.Minute, 40, 100,
			[]drop{{"Coyote pelt", 1, 3}, {"Bike helmet", 8, 1}, {"Trophy shelf", 128, 1}}, 1, 1},
		// docs/specs/quests.md: More monsters.
		{"Goose", gamedata.AreaAldrichPark, 6, 12, 8, 6, 4, 4, gamedata.Aggressive, 30 * time.Second, 4, 12,
			[]drop{{"Feather", 1, 1}, {"Goose egg", 10, 1}}, 0, 6},
		{"Runaway e-scooter", gamedata.AreaSouthFields, 24, 60, 32, 30, 34, 5, gamedata.Aggressive, 90 * time.Second, 50, 120,
			[]drop{{"Lithium cell", 3, 1}, {"Steel sword", 40, 1}}, 0, 4},
		{"Night watchman bot", gamedata.AreaSouthFields, 30, 90, 40, 38, 40, 4, gamedata.Aggressive, 5 * time.Minute, 100, 250,
			[]drop{{"Lithium cell", 1, 2}, {"Lab coat", 6, 1}, {"Watchman's visor", 12, 1}}, 0, 1},
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
			if n.Population != tt.population {
				t.Errorf("population = %d, want %d", n.Population, tt.population)
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
		{"bank slots", r.BankSlots, 200},
		{"bank distance tiles", r.BankDistanceTiles, 2},
		{"PvP items kept", r.PvPItemsKept, 3},
		{"drop reservation", r.DropReservation(), 60 * time.Second},
		{"drop despawn", r.DropDespawn(), 3 * time.Minute},
		{"pickup range tiles", r.PickupRangeTiles, 1},
		{"pickup range includes diagonals", r.PickupRangeDiagonal, true},
		{"logout combat lock", r.LogoutCombatLock(), 10 * time.Second},
		{"eat attack delay combat ticks", r.EatAttackDelayTicks, 1},
		{"house rent per month", r.HouseRentPerMonth, 200},
		{"house days per month", r.HouseDaysPerMonth, 30},
		{"house claim months", r.HouseClaimMonths, 2},
		{"house max prepaid months", r.HouseMaxPrepaidMonths, 6},
		{"house warning days", r.HouseWarningDays, 7},
		{"house auction days", r.HouseAuctionDays, 5},
		{"home edit per second", r.HomeEditPerSecond, 4},
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

// TestCardTablesMatchSpec checks the card room's tables: 4 tables, ids 1 to
// 4, each with 4 seats and a 20 s turn timer, staking 10, 25, 50 and 100
// gold (docs/specs/card-tables.md, PRD 13 Q2).
func TestCardTablesMatchSpec(t *testing.T) {
	d := load(t)
	want := []gamedata.CardTable{
		{ID: 1, Seats: 4, TurnTimerSeconds: 20, Stake: 10},
		{ID: 2, Seats: 4, TurnTimerSeconds: 20, Stake: 25},
		{ID: 3, Seats: 4, TurnTimerSeconds: 20, Stake: 50},
		{ID: 4, Seats: 4, TurnTimerSeconds: 20, Stake: 100},
	}
	if len(d.CardTables) != len(want) {
		t.Fatalf("got %d card tables, want %d", len(d.CardTables), len(want))
	}
	for i, w := range want {
		if got := d.CardTables[i]; got != w {
			t.Errorf("card table %d = %+v, want %+v", i, got, w)
		}
		if got := d.CardTables[i].TurnTimer(); got != 20*time.Second {
			t.Errorf("card table %d turn timer = %v, want 20s", w.ID, got)
		}
	}
}

// TestQuestItemFlag checks the flag loads, defaults to false, and is set on
// exactly the two quest items.
func TestQuestItemFlag(t *testing.T) {
	d := load(t)
	for _, it := range d.Items {
		want := it.Key == "overdue_book" || it.Key == "alphas_fang"
		if it.QuestItem != want {
			t.Errorf("%s: quest_item = %v, want %v", it.Key, it.QuestItem, want)
		}
	}
	// mk loads the shipped tables with the Feather's row changed: the
	// quest_item flag removed, set, or set together with a shop price.
	mk := func(featherRow string) fstest.MapFS {
		m := fstest.MapFS{}
		for _, f := range []string{"items.json", "npcs.json", "shop.json", "thrift.json", "workbench.json", "rules.json", "cardtables.json", "skills.json", "quests.json", "dialogue.json", "groups.json"} {
			b, err := os.ReadFile("data/" + f)
			if err != nil {
				t.Fatal(err)
			}
			m[f] = &fstest.MapFile{Data: b}
		}
		old := `"key": "feather", "name": "Feather", "kind": "loot", "shop_buys_for": 1`
		items := string(m["items.json"].Data)
		if !strings.Contains(items, old) {
			t.Fatal("feather row not found")
		}
		m["items.json"].Data = []byte(strings.Replace(items, old, featherRow, 1))
		return m
	}
	const base = `"key": "feather", "name": "Feather", "kind": "loot"`
	flag := func(row string) (bool, error) {
		d, err := gamedata.Load(mk(row))
		if err != nil {
			return false, err
		}
		it, _ := d.ItemByKey("feather")
		return it.QuestItem, nil
	}
	if got, err := flag(base + `, "shop_buys_for": 1`); err != nil || got {
		t.Errorf("item without the flag: err %v, flag %v", err, got)
	}
	if got, err := flag(base + `, "quest_item": true`); err != nil || !got {
		t.Errorf("item with the flag: err %v, flag %v", err, got)
	}
	if _, err := gamedata.Load(mk(base + `, "quest_item": true, "shop_buys_for": 5`)); err == nil {
		t.Error("quest item the shop buys: no error")
	}
}
