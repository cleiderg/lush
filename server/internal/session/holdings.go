package session

import (
	"fmt"
	"math"
	"slices"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// equipSlots pairs the store's equipment columns with the world's slots.
var equipSlots = [store.EquipSlots]gamedata.Slot{
	store.EquipWeapon: gamedata.SlotWeapon,
	store.EquipHead:   gamedata.SlotHead,
	store.EquipBody:   gamedata.SlotBody,
	store.EquipShield: gamedata.SlotShield,
}

// fromHoldings builds a world inventory with d's slot count from stored
// holdings. It fails if a stored item doesn't fit or isn't a valid id.
func fromHoldings(d *gamedata.Data, h store.Holdings) (world.Inventory, error) {
	inv := world.NewInventory(d)
	if h.Gold < 0 || h.Gold > math.MaxInt {
		return world.Inventory{}, fmt.Errorf("gold %d out of range", h.Gold)
	}
	inv.Gold = int(h.Gold)
	for i, st := range h.Inventory {
		if st == (store.Stack{}) {
			continue
		}
		if i >= len(inv.Slots) {
			return world.Inventory{}, fmt.Errorf("inventory slot %d beyond %d slots", i, len(inv.Slots))
		}
		if st.Item <= 0 || st.Item > math.MaxUint16 || st.Qty <= 0 || st.Qty > math.MaxInt {
			return world.Inventory{}, fmt.Errorf("inventory slot %d: item %d qty %d out of range", i, st.Item, st.Qty)
		}
		inv.Slots[i] = world.Stack{Item: gamedata.ItemID(st.Item), Qty: int(st.Qty)}
	}
	for i, id := range h.Equipment {
		if id < 0 || id > math.MaxUint16 {
			return world.Inventory{}, fmt.Errorf("equipment %s: item %d out of range", store.EquipSlot(i), id)
		}
		*equipRef(&inv.Equipment, equipSlots[i]) = gamedata.ItemID(id)
	}
	return inv, nil
}

// toHoldings converts a world inventory for the store. It fails if the
// inventory has more occupied slots than the store keeps.
func toHoldings(inv world.Inventory) (store.Holdings, error) {
	var h store.Holdings
	h.Gold = int64(inv.Gold)
	for i, st := range inv.Slots {
		if st.Empty() {
			continue
		}
		if i >= store.InventorySlots {
			return store.Holdings{}, fmt.Errorf("inventory slot %d beyond the stored %d", i, store.InventorySlots)
		}
		h.Inventory[i] = store.Stack{Item: int32(st.Item), Qty: int64(st.Qty)}
	}
	for i, slot := range equipSlots {
		h.Equipment[i] = int32(inv.Equipment.Get(slot))
	}
	return h, nil
}

func equipRef(e *world.Equipment, slot gamedata.Slot) *gamedata.ItemID {
	switch slot {
	case gamedata.SlotWeapon:
		return &e.Weapon
	case gamedata.SlotHead:
		return &e.Head
	case gamedata.SlotBody:
		return &e.Body
	default:
		return &e.Shield
	}
}

// auditPayload is the JSON detail of an economy audit entry.
type auditPayload struct {
	Reason string `json:"reason"`
	Item   int    `json:"item,omitempty"`
	Qty    int    `json:"qty,omitempty"`
	Gold   int    `json:"gold,omitempty"`
}

// auditEntries turns world changes into audit entries, one per change. The
// store fills in the actor.
func auditEntries(changes []world.Change, reason string) []store.Entry {
	out := make([]store.Entry, len(changes))
	for i, c := range changes {
		out[i] = store.Entry{Kind: c.Kind.String(),
			Payload: auditPayload{Reason: reason, Item: int(c.Item), Qty: c.Qty, Gold: c.Gold}}
	}
	return out
}

func cloneInventory(inv world.Inventory) world.Inventory {
	inv.Slots = slices.Clone(inv.Slots)
	return inv
}

// sameInventory reports whether a and b show the same InventoryState.
func sameInventory(a, b *world.Inventory) bool {
	return a.Equipment == b.Equipment && slices.Equal(a.Slots, b.Slots)
}

// stats is what PlayerStats carries.
type stats struct {
	hp, maxHP, level, xp, gold int
}

func statsOf(r gamedata.Rules, p *world.Player) stats {
	level := p.CombatLevel(r)
	return stats{hp: p.HP, maxHP: world.MaxHPForLevel(r, level), level: level, xp: p.XP, gold: p.Inv.Gold}
}
