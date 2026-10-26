// Package items is the item action system (docs/specs/gameplay.md, Items):
// equipping, unequipping and eating, checked against the server's rules.
// Every change to a player's holdings is recorded in their economy outbox for
// the session to save with an audit entry.
package items

import (
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// Outbox reasons, one per action.
const (
	ReasonEquip   = "equip"
	ReasonUnequip = "unequip"
	ReasonEat     = "eat"
)

// EquipCommand asks to wear the item in inventory slot Slot.
type EquipCommand struct {
	Player world.PlayerID
	Slot   int
}

func (c EquipCommand) Target() world.PlayerID { return c.Player }

// UnequipCommand asks to move the item worn in Slot into the inventory.
type UnequipCommand struct {
	Player world.PlayerID
	Slot   gamedata.Slot
}

func (c UnequipCommand) Target() world.PlayerID { return c.Player }

// EatCommand asks to eat the food in inventory slot Slot.
type EatCommand struct {
	Player world.PlayerID
	Slot   int
}

func (c EatCommand) Target() world.PlayerID { return c.Player }

// System applies item commands. Invalid requests are ignored and change
// nothing.
type System struct {
	data *gamedata.Data
}

// New creates the item system reading items and rules from d.
func New(d *gamedata.Data) *System { return &System{data: d} }

func (s *System) Name() string { return "items" }

// Handle applies an equip, unequip or eat command for p.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case EquipCommand:
		s.equip(p, c.Slot)
	case UnequipCommand:
		s.unequip(p, c.Slot)
	case EatCommand:
		s.eat(p, c.Slot)
	}
}

// Tick does nothing: every item action happens when its command is handled.
func (s *System) Tick(*world.World, time.Duration) {}

func (s *System) equip(p *world.Player, index int) {
	ch, err := p.Inv.Equip(s.data, index, p.CombatLevel(s.data.Rules))
	if err != nil {
		return
	}
	p.RecordEconomy(ReasonEquip, ch)
}

func (s *System) unequip(p *world.Player, slot gamedata.Slot) {
	ch, err := p.Inv.Unequip(slot)
	if err != nil {
		return
	}
	p.RecordEconomy(ReasonUnequip, ch)
}

// eat removes one food item from slot index, heals by its Heals value up to
// max HP and delays the player's next attack by Rules.EatAttackDelayTicks.
// Food is eaten even at full HP. Like equipping, eating needs the item's
// level (gamedata.Item.LevelToEquip).
func (s *System) eat(p *world.Player, index int) {
	if index < 0 || index >= len(p.Inv.Slots) {
		return
	}
	it, ok := s.data.Item(p.Inv.Slots[index].Item)
	if !ok || it.Kind != gamedata.KindFood || p.CombatLevel(s.data.Rules) < it.LevelToEquip {
		return
	}
	ch, err := p.Inv.RemoveAt(index)
	if err != nil {
		return
	}
	p.Heal(s.data.Rules, it.Heals)
	p.AttackDelay = s.data.Rules.EatAttackDelayTicks
	p.RecordEconomy(ReasonEat, ch)
}
