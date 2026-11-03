// Package furniture holds the two ways to get furniture from NPC-style
// stations (docs/specs/housing.md, Economy hooks): the workbench, which turns
// logs and gold into one piece of furniture straight away with no skill or
// level check, and the Campus Thrift stall, which sells furniture at fixed
// prices and buys it back for a third. Every success is one event in the
// player's economy outbox with its own reason, and every request gets a result
// code through a Sink. A request that doesn't return Ok changes nothing.
package furniture

import (
	"fmt"
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// Outbox reasons.
const (
	ReasonCraft      = "workbench_craft"
	ReasonThriftBuy  = "thrift_buy"
	ReasonThriftSell = "thrift_sell"
)

// Result is the outcome of a request.
type Result uint8

const (
	Ok            Result = iota
	TooFar               // beyond reach of every station
	UnknownRecipe        // craft: no recipe makes that item
	MissingInputs        // craft: not enough of an input item
	NotEnoughGold
	InventoryFull
	ItemNotSold // thrift: the stall doesn't sell (buy) or buy (sell) that
)

var resultNames = [...]string{"ok", "too_far", "unknown_recipe", "missing_inputs", "not_enough_gold", "inventory_full", "item_not_sold"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return fmt.Sprintf("Result(%d)", uint8(r))
}

// Sink receives the result of every request; the session implements it. It is
// called on the tick goroutine and must not block.
type Sink interface {
	FurnitureResult(to world.PlayerID, r Result)
}

// CraftCommand asks to craft one Output at a workbench.
type CraftCommand struct {
	Player world.PlayerID
	Output gamedata.ItemID
}

func (c CraftCommand) Target() world.PlayerID { return c.Player }

// BuyCommand asks to buy Quantity of Item from the thrift stall.
type BuyCommand struct {
	Player   world.PlayerID
	Item     gamedata.ItemID
	Quantity int
}

func (c BuyCommand) Target() world.PlayerID { return c.Player }

// SellCommand asks to sell up to Quantity items from inventory slot Slot.
type SellCommand struct {
	Player   world.PlayerID
	Slot     int
	Quantity int
}

func (c SellCommand) Target() world.PlayerID { return c.Player }

// System applies workbench and thrift commands.
type System struct {
	data        *gamedata.Data
	workbenches []world.Vec2
	stalls      []world.Vec2
	reach       float64 // metres
	sink        Sink
}

// New creates the system. workbenches and stalls are the station positions;
// reach is the distance in metres within which a player can use one (the
// boundary counts as inside).
func New(d *gamedata.Data, workbenches, stalls []world.Vec2, reach float64, sink Sink) *System {
	return &System{data: d, workbenches: workbenches, stalls: stalls, reach: reach, sink: sink}
}

func (s *System) Name() string { return "furniture" }

// Handle applies a command for p and reports its result.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case CraftCommand:
		s.sink.FurnitureResult(p.ID, s.craft(p, c.Output))
	case BuyCommand:
		s.sink.FurnitureResult(p.ID, s.buy(p, c.Item, c.Quantity))
	case SellCommand:
		s.sink.FurnitureResult(p.ID, s.sell(p, c.Slot, c.Quantity))
	}
}

// Tick does nothing: every request is settled when its command is handled.
func (s *System) Tick(*world.World, time.Duration) {}

func (s *System) near(pos world.Vec2, stations []world.Vec2) bool {
	for _, st := range stations {
		dx := float64(pos.X) - float64(st.X)
		dy := float64(pos.Y) - float64(st.Y)
		if dx*dx+dy*dy <= s.reach*s.reach {
			return true
		}
	}
	return false
}

func (s *System) recipe(output gamedata.ItemID) (gamedata.WorkbenchRecipe, bool) {
	for _, r := range s.data.Workbench {
		if r.Output == output {
			return r, true
		}
	}
	return gamedata.WorkbenchRecipe{}, false
}

func (s *System) craft(p *world.Player, output gamedata.ItemID) Result {
	if !s.near(p.Pos, s.workbenches) {
		return TooFar
	}
	r, ok := s.recipe(output)
	if !ok {
		return UnknownRecipe
	}
	for _, in := range r.Inputs {
		if p.Inv.Count(in.Item) < in.Quantity {
			return MissingInputs
		}
	}
	if p.Inv.Gold < r.Gold {
		return NotEnoughGold
	}
	// Work on a copy so a failure part way changes nothing.
	inv := p.Inv.Clone()
	var changes []world.Change
	for _, in := range r.Inputs {
		c, err := inv.Remove(in.Item, in.Quantity)
		if err != nil {
			return MissingInputs
		}
		changes = append(changes, c...)
	}
	if r.Gold > 0 {
		c, err := inv.SpendGold(r.Gold)
		if err != nil {
			return NotEnoughGold
		}
		changes = append(changes, c...)
	}
	c, err := inv.Add(s.data, r.Output, 1)
	if err != nil {
		return InventoryFull
	}
	p.Inv = inv
	p.RecordEconomy(ReasonCraft, append(changes, c...))
	return Ok
}

func (s *System) buy(p *world.Player, item gamedata.ItemID, qty int) Result {
	if !s.near(p.Pos, s.stalls) {
		return TooFar
	}
	price, ok := s.data.ThriftSellPrice(item)
	if !ok || qty <= 0 || price <= 0 {
		return ItemNotSold
	}
	if qty > p.Inv.Gold/price {
		return NotEnoughGold
	}
	inv := p.Inv.Clone()
	added, err := inv.Add(s.data, item, qty)
	if err != nil {
		return InventoryFull
	}
	spent, err := inv.SpendGold(qty * price)
	if err != nil {
		return NotEnoughGold
	}
	p.Inv = inv
	p.RecordEconomy(ReasonThriftBuy, append(added, spent...))
	return Ok
}

func (s *System) sell(p *world.Player, index, qty int) Result {
	if !s.near(p.Pos, s.stalls) {
		return TooFar
	}
	if index < 0 || index >= len(p.Inv.Slots) || qty <= 0 {
		return ItemNotSold
	}
	slot := p.Inv.Slots[index]
	if slot.Empty() {
		return ItemNotSold
	}
	price, ok := s.data.ThriftBuyPrice(slot.Item)
	if !ok || price <= 0 {
		return ItemNotSold
	}
	qty = min(qty, slot.Qty)
	if qty > math.MaxInt/price {
		return ItemNotSold
	}
	inv := p.Inv.Clone()
	removed, err := inv.RemoveFromSlot(index, qty)
	if err != nil {
		return ItemNotSold
	}
	paid, err := inv.AddGold(qty * price)
	if err != nil {
		return ItemNotSold
	}
	p.Inv = inv
	p.RecordEconomy(ReasonThriftSell, append(removed, paid...))
	return Ok
}
