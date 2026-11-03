package session

import (
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/furniture"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/world"
)

// CraftCommand is a client WorkbenchCraft. Handle passes it to the furniture
// system; it is dropped without one.
type CraftCommand struct{ furniture.CraftCommand }

// ThriftBuyCommand is a client ShopBuy with the Thrift shop.
type ThriftBuyCommand struct{ furniture.BuyCommand }

// ThriftSellCommand is a client ShopSell with the Thrift shop.
type ThriftSellCommand struct{ furniture.SellCommand }

// SetFurniture connects the furniture system: WorkbenchCraft and ShopBuy and
// ShopSell with the Thrift shop go to it. It must have been created with
// FurnitureSink as its Sink, and must not also be registered on the world,
// because the Server hands it its commands. Call it before the world starts
// ticking.
func (s *Server) SetFurniture(sys *furniture.System) { s.furn = sys }

// FurnitureSink is the furniture.Sink that answers a craft with CraftResult
// and a thrift buy or sell with ShopResult.
func (s *Server) FurnitureSink() furniture.Sink { return furnitureSink{s} }

type furnitureSink struct{ s *Server }

var craftCodes = map[furniture.Result]protocol.CraftResultCode{
	furniture.Ok:            protocol.CraftResultCodeOk,
	furniture.TooFar:        protocol.CraftResultCodeTooFar,
	furniture.UnknownRecipe: protocol.CraftResultCodeUnknownRecipe,
	furniture.MissingInputs: protocol.CraftResultCodeMissingInputs,
	furniture.NotEnoughGold: protocol.CraftResultCodeNotEnoughGold,
	furniture.InventoryFull: protocol.CraftResultCodeInventoryFull,
}

var thriftCodes = map[furniture.Result]protocol.ShopResultCode{
	furniture.Ok:            protocol.ShopResultCodeOk,
	furniture.TooFar:        protocol.ShopResultCodeTooFar,
	furniture.NotEnoughGold: protocol.ShopResultCodeNotEnoughGold,
	furniture.InventoryFull: protocol.ShopResultCodeInventoryFull,
	furniture.ItemNotSold:   protocol.ShopResultCodeItemNotSold,
}

// handleFurniture runs one furniture command; the result it produces is sent
// from FurnitureResult as a CraftResult if craft, else as a ShopResult.
func (s *Server) handleFurniture(cmd world.Command, craft bool, p *world.Player) {
	if s.furn == nil {
		return
	}
	s.furnCraft = craft
	s.furn.Handle(cmd, p)
}

// FurnitureResult implements furniture.Sink. Called on the tick goroutine,
// from handleFurniture. A result with no wire code for its kind (a craft can't
// be ItemNotSold, a buy can't be UnknownRecipe) is dropped.
func (f furnitureSink) FurnitureResult(to world.PlayerID, r furniture.Result) {
	g := f.s.live[to]
	if g == nil || g.conn == nil {
		return
	}
	if f.s.furnCraft {
		if code, ok := craftCodes[r]; ok {
			g.conn.Send(encodeCraftResult(code))
		}
	} else if code, ok := thriftCodes[r]; ok {
		g.conn.Send(encodeShopResult(shop.Result(code)))
	}
}
