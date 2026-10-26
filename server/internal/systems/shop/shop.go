// Package shop is the Campus Market NPC shop system (docs/specs/gameplay.md,
// NPC shop): a player within Rules.ShopDistanceTiles of the stall buys the
// items the shop sells and sells any item the shop buys, at the fixed prices
// in the game data. Every purchase or sale is one event in the player's
// economy outbox, and every request gets a result code through a Sink.
package shop

import (
	"fmt"
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// Outbox reasons.
const (
	ReasonBuy  = "shop_buy"
	ReasonSell = "shop_sell"
)

// Result is the outcome of a request. The values match protocol
// ShopResultCode.
type Result uint8

const (
	Ok            Result = 0
	TooFar        Result = 1
	NotEnoughGold Result = 2
	InventoryFull Result = 3
	ItemNotSold   Result = 4
)

var resultNames = [...]string{"ok", "too_far", "not_enough_gold", "inventory_full", "item_not_sold"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return fmt.Sprintf("Result(%d)", uint8(r))
}

// BuyCommand asks to buy Quantity of Item from the shop.
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

// Sink receives the result of every buy and sell request; the session
// implements it to send ShopResult. It is called on the tick goroutine and
// must not block.
type Sink interface {
	ShopResult(to world.PlayerID, r Result)
}

// System applies shop commands. A request that doesn't return Ok changes
// nothing.
type System struct {
	data  *gamedata.Data
	stall world.Vec2
	reach float64 // metres
	sink  Sink
}

// New creates the shop system. The stall position comes from l, the prices
// and Rules.ShopDistanceTiles from d; tileSize is the length of a tile in
// metres (the collision grid's cell size).
func New(d *gamedata.Data, l *areas.Lookup, tileSize float64, sink Sink) *System {
	return &System{
		data:  d,
		stall: l.Shop(),
		reach: float64(d.Rules.ShopDistanceTiles) * tileSize,
		sink:  sink,
	}
}

func (s *System) Name() string { return "shop" }

// Handle applies a buy or sell command for p and reports its result.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case BuyCommand:
		s.sink.ShopResult(p.ID, s.buy(p, c.Item, c.Quantity))
	case SellCommand:
		s.sink.ShopResult(p.ID, s.sell(p, c.Slot, c.Quantity))
	}
}

// Tick does nothing: every request is settled when its command is handled.
func (s *System) Tick(*world.World, time.Duration) {}

// InReach reports whether pos is within the shop distance of the stall (the
// boundary counts as inside).
func (s *System) InReach(pos world.Vec2) bool {
	dx := float64(pos.X) - float64(s.stall.X)
	dy := float64(pos.Y) - float64(s.stall.Y)
	return dx*dx+dy*dy <= s.reach*s.reach
}

func (s *System) buy(p *world.Player, item gamedata.ItemID, qty int) Result {
	if !s.InReach(p.Pos) {
		return TooFar
	}
	price, ok := s.data.ShopSellPrice(item)
	if !ok || qty <= 0 {
		return ItemNotSold
	}
	if qty > p.Inv.Gold/price {
		return NotEnoughGold
	}
	// Work on a copy so a failure part way changes nothing.
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
	p.RecordEconomy(ReasonBuy, append(added, spent...))
	return Ok
}

func (s *System) sell(p *world.Player, index, qty int) Result {
	if !s.InReach(p.Pos) {
		return TooFar
	}
	if index < 0 || index >= len(p.Inv.Slots) || qty <= 0 {
		return ItemNotSold
	}
	slot := p.Inv.Slots[index]
	it, ok := s.data.Item(slot.Item)
	if slot.Empty() || !ok || it.ShopBuysFor <= 0 {
		return ItemNotSold
	}
	qty = min(qty, slot.Qty)
	if qty > math.MaxInt/it.ShopBuysFor {
		return ItemNotSold
	}
	inv := p.Inv.Clone()
	removed, err := inv.RemoveFromSlot(index, qty)
	if err != nil {
		return ItemNotSold
	}
	// A balance that can't hold the payment is refused like an item the
	// shop won't buy; no other result code fits.
	paid, err := inv.AddGold(qty * it.ShopBuysFor)
	if err != nil {
		return ItemNotSold
	}
	p.Inv = inv
	p.RecordEconomy(ReasonSell, append(removed, paid...))
	return Ok
}
