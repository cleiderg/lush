// Package gamedata loads the Phase 1 gameplay tables (items, NPC types, the
// shop list, rule constants and the card tables) from embedded JSON files and validates them.
//
// Every number comes from docs/specs/gameplay.md and is a placeholder to tune
// after playtests (D-047, PRD 13). Systems read the numbers from here instead
// of hard-coding them. The package imports no other server/internal package,
// so world and every system can use it.
package gamedata

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"time"
)

//go:embed data/*.json
var embedded embed.FS

// File names inside the data directory.
const (
	ItemsFile = "items.json"
	NPCsFile  = "npcs.json"
	ShopFile  = "shop.json"
	RulesFile = "rules.json"
	// CardTablesFile lists the card room's tables (docs/specs/card-tables.md).
	CardTablesFile = "cardtables.json"
	// SkillsFile holds the woodcutting, fishing and cooking tables (docs/specs/skills.md).
	SkillsFile = "skills.json"
)

// ItemID identifies an item type. 0 is never a valid id.
type ItemID uint16

// NPCTypeID identifies an NPC type. 0 is never a valid id.
type NPCTypeID uint16

// ItemKind is what an item is for.
type ItemKind string

const (
	KindWeapon ItemKind = "weapon"
	KindArmor  ItemKind = "armor"
	KindFood   ItemKind = "food"
	KindLoot   ItemKind = "loot"
)

// IsEquipment reports whether items of this kind are worn in a slot.
func (k ItemKind) IsEquipment() bool { return k == KindWeapon || k == KindArmor }

// Slot is an equipment slot. Items that aren't equipment have SlotNone.
type Slot string

const (
	SlotNone   Slot = ""
	SlotWeapon Slot = "weapon"
	SlotHead   Slot = "head"
	SlotBody   Slot = "body"
	SlotShield Slot = "shield"
)

func (s Slot) valid() bool {
	return s == SlotWeapon || s == SlotHead || s == SlotBody || s == SlotShield
}

// Behaviour is how an NPC picks fights.
type Behaviour string

const (
	Passive    Behaviour = "passive"    // only fights back
	Aggressive Behaviour = "aggressive" // attacks players within Rules.AggressionRangeTiles
)

// Area is the gameplay area an NPC type spawns in (docs/specs/map.md).
type Area string

const (
	AreaAldrichPark Area = "aldrich_park"
	AreaSouthFields Area = "south_fields"
)

// Stats are combat bonuses. Equipment adds them to a player; NPCs have their own.
type Stats struct {
	Accuracy int `json:"accuracy"`
	Strength int `json:"strength"`
	Defence  int `json:"defence"`
}

// Item is one row of the item table.
type Item struct {
	ID   ItemID   `json:"id"`
	Key  string   `json:"key"`
	Name string   `json:"name"`
	Kind ItemKind `json:"kind"`
	Slot Slot     `json:"slot"`
	Stats
	AttackSpeed  int `json:"attack_speed"`   // combat ticks between attacks; weapons only, else 0
	LevelToEquip int `json:"level_to_equip"` // combat level to equip or eat; 0 for loot
	ShopBuysFor  int `json:"shop_buys_for"`  // gold the shop pays for one; 0 if it won't buy
	Heals        int `json:"heals"`          // HP restored when eaten; food only, else 0
	// NoStack makes a loot item take one inventory slot each (logs, fish, tools).
	NoStack bool `json:"no_stack"`
	// ToolSkill, ToolLevel and ToolBonus make the item a skill tool: the skill
	// it works for, the skill level needed and the chance added to both ends of
	// the success range. Hatchets are weapons that are also tools.
	ToolSkill Skill   `json:"tool_skill"`
	ToolLevel int     `json:"tool_level"`
	ToolBonus float64 `json:"tool_bonus"`
}

// Stackable reports whether any number of this item fits in one inventory
// slot. Loot stacks unless NoStack is set; equipment and food don't.
func (it Item) Stackable() bool { return it.Kind == KindLoot && !it.NoStack }

// IsTool reports whether the item is a skill tool.
func (it Item) IsTool() bool { return it.ToolSkill != SkillNone }

// Drop is one item roll on an NPC kill.
type Drop struct {
	ItemKey  string `json:"item"`
	Chance   int    `json:"chance"` // drops 1 time in Chance; 1 means always
	Quantity int    `json:"quantity"`
	Item     ItemID `json:"-"` // resolved from ItemKey by Load
}

// NPCType is one row of the NPC table.
type NPCType struct {
	ID    NPCTypeID `json:"id"`
	Key   string    `json:"key"`
	Name  string    `json:"name"`
	Area  Area      `json:"area"`
	Level int       `json:"level"`
	HP    int       `json:"hp"`
	Stats
	AttackSpeed    int       `json:"attack_speed"` // combat ticks between attacks
	Behaviour      Behaviour `json:"behaviour"`
	RespawnSeconds int       `json:"respawn_s"`
	Population     int       `json:"population"` // how many the spawner keeps in the area
	MaxAlive       int       `json:"max_alive"`  // most alive at once; 0 means no limit
	GoldMin        int       `json:"gold_min"`   // every kill drops GoldMin..GoldMax gold
	GoldMax        int       `json:"gold_max"`
	Drops          []Drop    `json:"drops"` // each rolls separately
}

// Respawn is the time between death and respawn.
func (n NPCType) Respawn() time.Duration { return seconds(n.RespawnSeconds) }

// ShopEntry is one item the shop sells, with unlimited stock.
type ShopEntry struct {
	ItemKey string `json:"item"`
	Price   int    `json:"price"`
	Item    ItemID `json:"-"`
}

// Shop is the NPC shop. It buys every item with Item.ShopBuysFor > 0.
type Shop struct {
	Name  string      `json:"name"`
	Sells []ShopEntry `json:"sells"`
}

// KitStack is a quantity of one item.
type KitStack struct {
	ItemKey  string `json:"item"`
	Quantity int    `json:"quantity"`
	Item     ItemID `json:"-"`
}

// StartingKit is what a new player starts with.
type StartingKit struct {
	EquippedKeys []string   `json:"equipped"`
	Equipped     []ItemID   `json:"-"`
	Inventory    []KitStack `json:"inventory"`
}

// Rules holds the rule constants. Distances are in tiles, times in seconds
// (use the Duration methods), attack speeds in combat ticks.
type Rules struct {
	InventorySlots          int         `json:"inventory_slots"`
	StartingGold            int         `json:"starting_gold"`
	StartingKit             StartingKit `json:"starting_kit"`
	AttackRangeTiles        int         `json:"attack_range_tiles"`
	AttackRangeDiagonal     bool        `json:"attack_range_diagonal"` // diagonal neighbours count as in range
	UnarmedAttackSpeed      int         `json:"unarmed_attack_speed"`
	MinCombatLevel          int         `json:"min_combat_level"`
	MaxCombatLevel          int         `json:"max_combat_level"`
	BaseHP                  int         `json:"base_hp"` // max HP = BaseHP + combat level
	XPPerDamage             int         `json:"xp_per_damage"`
	RegenHP                 int         `json:"regen_hp"`
	RegenIntervalSeconds    int         `json:"regen_interval_s"`
	RegenDelaySeconds       int         `json:"regen_delay_s"` // after last dealing or taking damage
	AggressionRangeTiles    int         `json:"aggression_range_tiles"`
	LeashTiles              int         `json:"leash_tiles"`
	PvPLevelGap             int         `json:"pvp_level_gap"`
	ShopDistanceTiles       int         `json:"shop_distance_tiles"`
	TradeDistanceTiles      int         `json:"trade_distance_tiles"`
	BankSlots               int         `json:"bank_slots"` // distinct items the bank holds
	BankDistanceTiles       int         `json:"bank_distance_tiles"`
	PvPItemsKept            int         `json:"pvp_items_kept"`
	DropReservationSeconds  int         `json:"drop_reservation_s"`
	DropDespawnSeconds      int         `json:"drop_despawn_s"`
	PickupRangeTiles        int         `json:"pickup_range_tiles"`
	PickupRangeDiagonal     bool        `json:"pickup_range_diagonal"` // diagonal neighbours count as in range
	LogoutCombatLockSeconds int         `json:"logout_combat_lock_s"`
	EatAttackDelayTicks     int         `json:"eat_attack_delay_combat_ticks"`

	// Housing (docs/specs/housing.md, D-052). First-release numbers are placeholders.
	HouseRentPerMonth     int `json:"house_rent_per_month"`
	HouseDaysPerMonth     int `json:"house_days_per_month"`
	HouseClaimMonths      int `json:"house_claim_months"`
	HouseMaxPrepaidMonths int `json:"house_max_prepaid_months"`
	HouseWarningDays      int `json:"house_warning_days"`
	HouseAuctionDays      int `json:"house_auction_days"`
}

func (r Rules) RegenInterval() time.Duration    { return seconds(r.RegenIntervalSeconds) }
func (r Rules) RegenDelay() time.Duration       { return seconds(r.RegenDelaySeconds) }
func (r Rules) DropReservation() time.Duration  { return seconds(r.DropReservationSeconds) }
func (r Rules) DropDespawn() time.Duration      { return seconds(r.DropDespawnSeconds) }
func (r Rules) LogoutCombatLock() time.Duration { return seconds(r.LogoutCombatLockSeconds) }

func seconds(n int) time.Duration { return time.Duration(n) * time.Second }

// Card table seat limits (docs/specs/card-tables.md: 2 to 4 seats).
const (
	MinCardTableSeats = 2
	MaxCardTableSeats = 4
)

// CardTableID identifies a card table. 0 is never a valid id.
type CardTableID uint32

// CardTable is one table in the card room. Its game is Shed.
type CardTable struct {
	ID               CardTableID `json:"id"`
	Seats            int         `json:"seats"`
	TurnTimerSeconds int         `json:"turn_timer_s"`
	// Stake is the gold each seated player puts into the pot when a game
	// starts (placeholder amounts, PRD 13 Q2). 0 is a table without stakes.
	Stake int `json:"stake"`
}

// TurnTimer is how long a seat has to move before its turn times out.
func (c CardTable) TurnTimer() time.Duration { return seconds(c.TurnTimerSeconds) }

// Data is the loaded, validated set of tables. Treat it as read-only.
type Data struct {
	Items []Item
	NPCs  []NPCType
	Shop  Shop
	Rules Rules
	// CardTables are the card room's tables, in file order.
	CardTables []CardTable
	// Skills holds the woodcutting, fishing and cooking tables.
	Skills Skills

	itemByID  map[ItemID]int
	itemByKey map[string]int
	npcByID   map[NPCTypeID]int
	npcByKey  map[string]int
	shopPrice map[ItemID]int
}

// Item returns the item with the given id.
func (d *Data) Item(id ItemID) (Item, bool) {
	i, ok := d.itemByID[id]
	if !ok {
		return Item{}, false
	}
	return d.Items[i], true
}

// ItemByKey returns the item with the given key.
func (d *Data) ItemByKey(key string) (Item, bool) {
	i, ok := d.itemByKey[key]
	if !ok {
		return Item{}, false
	}
	return d.Items[i], true
}

// NPC returns the NPC type with the given id.
func (d *Data) NPC(id NPCTypeID) (NPCType, bool) {
	i, ok := d.npcByID[id]
	if !ok {
		return NPCType{}, false
	}
	return d.NPCs[i], true
}

// NPCByKey returns the NPC type with the given key.
func (d *Data) NPCByKey(key string) (NPCType, bool) {
	i, ok := d.npcByKey[key]
	if !ok {
		return NPCType{}, false
	}
	return d.NPCs[i], true
}

// ShopSellPrice returns what the shop charges for one of an item, and false
// if the shop doesn't sell it.
func (d *Data) ShopSellPrice(id ItemID) (int, bool) {
	p, ok := d.shopPrice[id]
	return p, ok
}

// Default loads the embedded tables.
func Default() (*Data, error) {
	sub, err := fs.Sub(embedded, "data")
	if err != nil {
		return nil, fmt.Errorf("gamedata: %w", err)
	}
	return Load(sub)
}

// Load reads items.json, npcs.json, shop.json, rules.json and
// cardtables.json from fsys and
// validates them. Unknown fields are rejected.
func Load(fsys fs.FS) (*Data, error) {
	d := &Data{}
	for _, f := range []struct {
		name string
		v    any
	}{{ItemsFile, &d.Items}, {NPCsFile, &d.NPCs}, {ShopFile, &d.Shop}, {RulesFile, &d.Rules}, {CardTablesFile, &d.CardTables}, {SkillsFile, &d.Skills}} {
		if err := decode(fsys, f.name, f.v); err != nil {
			return nil, err
		}
	}
	if err := d.index(); err != nil {
		return nil, err
	}
	return d, nil
}

func decode(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("gamedata: read %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("gamedata: decode %s: %w", name, err)
	}
	return nil
}

// index validates the tables, resolves item keys to ids and builds lookups.
func (d *Data) index() error {
	d.itemByID = map[ItemID]int{}
	d.itemByKey = map[string]int{}
	for i, it := range d.Items {
		where := fmt.Sprintf("%s: item %d (%q)", ItemsFile, it.ID, it.Key)
		if it.ID == 0 {
			return fmt.Errorf("gamedata: %s: id must be 1 to 65535", where)
		}
		if it.Key == "" {
			return fmt.Errorf("gamedata: %s: empty key", where)
		}
		if j, dup := d.itemByID[it.ID]; dup {
			return fmt.Errorf("gamedata: %s: duplicate id %d (also %q)", where, it.ID, d.Items[j].Key)
		}
		if _, dup := d.itemByKey[it.Key]; dup {
			return fmt.Errorf("gamedata: %s: duplicate key %q", where, it.Key)
		}
		switch it.Kind {
		case KindWeapon, KindArmor:
			if !it.Slot.valid() {
				return fmt.Errorf("gamedata: %s: equipment slot %q is not weapon, head, body or shield", where, it.Slot)
			}
			if (it.Kind == KindWeapon) != (it.Slot == SlotWeapon) {
				return fmt.Errorf("gamedata: %s: kind %s does not match slot %s", where, it.Kind, it.Slot)
			}
		case KindFood, KindLoot:
			if it.Slot != SlotNone {
				return fmt.Errorf("gamedata: %s: %s item has slot %q", where, it.Kind, it.Slot)
			}
		default:
			return fmt.Errorf("gamedata: %s: unknown kind %q", where, it.Kind)
		}
		if it.Kind == KindWeapon && it.AttackSpeed <= 0 {
			return fmt.Errorf("gamedata: %s: weapon needs a positive attack_speed", where)
		}
		if it.Kind == KindFood && it.Heals <= 0 {
			return fmt.Errorf("gamedata: %s: food needs a positive heals", where)
		}
		if err := it.validateTool(where); err != nil {
			return err
		}
		if it.ShopBuysFor < 0 {
			return fmt.Errorf("gamedata: %s: negative shop_buys_for", where)
		}
		d.itemByID[it.ID] = i
		d.itemByKey[it.Key] = i
	}

	d.npcByID = map[NPCTypeID]int{}
	d.npcByKey = map[string]int{}
	for i := range d.NPCs {
		n := &d.NPCs[i]
		where := fmt.Sprintf("%s: npc %d (%q)", NPCsFile, n.ID, n.Key)
		if n.ID == 0 {
			return fmt.Errorf("gamedata: %s: id must be 1 to 65535", where)
		}
		if n.Key == "" {
			return fmt.Errorf("gamedata: %s: empty key", where)
		}
		if j, dup := d.npcByID[n.ID]; dup {
			return fmt.Errorf("gamedata: %s: duplicate id %d (also %q)", where, n.ID, d.NPCs[j].Key)
		}
		if _, dup := d.npcByKey[n.Key]; dup {
			return fmt.Errorf("gamedata: %s: duplicate key %q", where, n.Key)
		}
		if n.Area != AreaAldrichPark && n.Area != AreaSouthFields {
			return fmt.Errorf("gamedata: %s: unknown area %q", where, n.Area)
		}
		if n.Behaviour != Passive && n.Behaviour != Aggressive {
			return fmt.Errorf("gamedata: %s: unknown behaviour %q", where, n.Behaviour)
		}
		if n.Level <= 0 || n.HP <= 0 || n.AttackSpeed <= 0 || n.RespawnSeconds <= 0 || n.MaxAlive < 0 {
			return fmt.Errorf("gamedata: %s: level, hp, attack_speed and respawn_s must be positive and max_alive not negative", where)
		}
		if n.Population < 0 {
			return fmt.Errorf("gamedata: %s: negative population %d", where, n.Population)
		}
		if n.MaxAlive > 0 && n.Population > n.MaxAlive {
			return fmt.Errorf("gamedata: %s: population %d exceeds max_alive %d", where, n.Population, n.MaxAlive)
		}
		if n.GoldMin < 0 || n.GoldMax < n.GoldMin {
			return fmt.Errorf("gamedata: %s: bad gold range %d to %d", where, n.GoldMin, n.GoldMax)
		}
		for k := range n.Drops {
			dr := &n.Drops[k]
			id, err := d.resolve(dr.ItemKey, where+" drop")
			if err != nil {
				return err
			}
			if dr.Chance < 1 || dr.Quantity < 1 {
				return fmt.Errorf("gamedata: %s drop %q: chance and quantity must be at least 1", where, dr.ItemKey)
			}
			dr.Item = id
		}
		d.npcByID[n.ID] = i
		d.npcByKey[n.Key] = i
	}

	d.shopPrice = map[ItemID]int{}
	for k := range d.Shop.Sells {
		e := &d.Shop.Sells[k]
		id, err := d.resolve(e.ItemKey, ShopFile+": entry")
		if err != nil {
			return err
		}
		if _, dup := d.shopPrice[id]; dup {
			return fmt.Errorf("gamedata: %s: entry %q listed twice", ShopFile, e.ItemKey)
		}
		if e.Price <= 0 {
			return fmt.Errorf("gamedata: %s: entry %q needs a positive price", ShopFile, e.ItemKey)
		}
		e.Item = id
		d.shopPrice[id] = e.Price
	}

	if r := d.Rules; r.HouseRentPerMonth < 1 || r.HouseDaysPerMonth < 1 || r.HouseClaimMonths < 1 ||
		r.HouseMaxPrepaidMonths < r.HouseClaimMonths || r.HouseWarningDays < 0 || r.HouseAuctionDays < 1 {
		return fmt.Errorf("gamedata: %s: invalid house rent settings", RulesFile)
	}
	if d.Rules.BankSlots < 1 {
		return fmt.Errorf("gamedata: %s: bank_slots %d must be at least 1", RulesFile, d.Rules.BankSlots)
	}
	if d.Rules.BankDistanceTiles < 0 {
		return fmt.Errorf("gamedata: %s: bank_distance_tiles %d must not be negative", RulesFile, d.Rules.BankDistanceTiles)
	}

	kit := &d.Rules.StartingKit
	kit.Equipped = nil
	used := map[Slot]string{}
	for _, key := range kit.EquippedKeys {
		id, err := d.resolve(key, RulesFile+": starting kit equipped")
		if err != nil {
			return err
		}
		it, _ := d.Item(id)
		if !it.Kind.IsEquipment() {
			return fmt.Errorf("gamedata: %s: starting kit equipped item %q is not equipment", RulesFile, key)
		}
		if other, taken := used[it.Slot]; taken {
			return fmt.Errorf("gamedata: %s: starting kit equips %q and %q in slot %s", RulesFile, other, key, it.Slot)
		}
		used[it.Slot] = key
		kit.Equipped = append(kit.Equipped, id)
	}
	for k := range kit.Inventory {
		s := &kit.Inventory[k]
		id, err := d.resolve(s.ItemKey, RulesFile+": starting kit inventory")
		if err != nil {
			return err
		}
		if s.Quantity < 1 {
			return fmt.Errorf("gamedata: %s: starting kit item %q needs a quantity of at least 1", RulesFile, s.ItemKey)
		}
		s.Item = id
	}
	if err := d.validateCardTables(); err != nil {
		return err
	}
	return d.validateSkills()
}

// validateCardTables checks every card table has a unique non-zero id, 2 to 4
// seats, a positive turn timer and a stake that is not negative.
func (d *Data) validateCardTables() error {
	seen := map[CardTableID]bool{}
	for _, c := range d.CardTables {
		where := fmt.Sprintf("%s: table %d", CardTablesFile, c.ID)
		if c.ID == 0 {
			return fmt.Errorf("gamedata: %s: id must be at least 1", where)
		}
		if seen[c.ID] {
			return fmt.Errorf("gamedata: %s: duplicate id %d", where, c.ID)
		}
		seen[c.ID] = true
		if c.Seats < MinCardTableSeats || c.Seats > MaxCardTableSeats {
			return fmt.Errorf("gamedata: %s: seats %d must be %d to %d", where, c.Seats, MinCardTableSeats, MaxCardTableSeats)
		}
		if c.TurnTimerSeconds <= 0 {
			return fmt.Errorf("gamedata: %s: turn_timer_s %d must be positive", where, c.TurnTimerSeconds)
		}
		if c.Stake < 0 {
			return fmt.Errorf("gamedata: %s: stake %d must not be negative", where, c.Stake)
		}
	}
	return nil
}

func (d *Data) resolve(key, where string) (ItemID, error) {
	i, ok := d.itemByKey[key]
	if !ok {
		return 0, fmt.Errorf("gamedata: %s: unknown item %q", where, key)
	}
	return d.Items[i].ID, nil
}
