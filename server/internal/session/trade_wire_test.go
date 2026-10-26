package session

import (
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/world"
)

func tradeRequestFrame(target uint16) []byte {
	return frame(protocol.MessageTradeRequest, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeRequestStart(b)
		protocol.TradeRequestAddTargetId(b, target)
		return protocol.TradeRequestEnd(b)
	})
}

func tradeOfferFrame(slot byte, qty uint32) []byte {
	return frame(protocol.MessageTradeOffer, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeOfferStart(b)
		protocol.TradeOfferAddSlot(b, slot)
		protocol.TradeOfferAddQuantity(b, qty)
		return protocol.TradeOfferEnd(b)
	})
}

func tradeRemoveFrame(index byte) []byte {
	return frame(protocol.MessageTradeRemove, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeRemoveStart(b)
		protocol.TradeRemoveAddIndex(b, index)
		return protocol.TradeRemoveEnd(b)
	})
}

func tradeSetGoldFrame(gold uint32) []byte {
	return frame(protocol.MessageTradeSetGold, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeSetGoldStart(b)
		protocol.TradeSetGoldAddGold(b, gold)
		return protocol.TradeSetGoldEnd(b)
	})
}

func tradeAcceptFrame(screen byte) []byte {
	return frame(protocol.MessageTradeAccept, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeAcceptStart(b)
		protocol.TradeAcceptAddScreen(b, screen)
		return protocol.TradeAcceptEnd(b)
	})
}

func tradeCancelFrame() []byte {
	return frame(protocol.MessageTradeCancel, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeCancelStart(b)
		return protocol.TradeCancelEnd(b)
	})
}

// AC1: the six trade messages decode into the trade commands; values the
// protocol doesn't define are ignored.
func TestDecodeTradeCommands(t *testing.T) {
	const id = world.PlayerID(7)
	tests := []struct {
		name  string
		frame []byte
		want  world.Command
	}{
		{"request", tradeRequestFrame(9), trade.RequestCommand{Player: id, Partner: 9}},
		{"request max id", tradeRequestFrame(65535), trade.RequestCommand{Player: id, Partner: 65535}},
		{"request nobody", tradeRequestFrame(0), nil},
		{"offer", tradeOfferFrame(3, 12), trade.OfferCommand{Player: id, Slot: 3, Quantity: 12}},
		{"offer max", tradeOfferFrame(255, 4294967295), trade.OfferCommand{Player: id, Slot: 255, Quantity: 4294967295}},
		{"offer nothing", tradeOfferFrame(3, 0), nil},
		{"remove", tradeRemoveFrame(2), trade.RemoveCommand{Player: id, Index: 2}},
		{"set gold", tradeSetGoldFrame(500), trade.SetGoldCommand{Player: id, Gold: 500}},
		{"set gold zero", tradeSetGoldFrame(0), trade.SetGoldCommand{Player: id, Gold: 0}},
		{"accept screen 1", tradeAcceptFrame(1), trade.AcceptCommand{Player: id, Screen: trade.ScreenOffer}},
		{"accept screen 2", tradeAcceptFrame(2), trade.AcceptCommand{Player: id, Screen: trade.ScreenConfirm}},
		{"accept screen 0", tradeAcceptFrame(0), nil},
		{"accept screen 3", tradeAcceptFrame(3), nil},
		{"cancel", tradeCancelFrame(), trade.CancelCommand{Player: id}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := decode(tc.frame)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.command(id); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("command = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// AC1: truncated trade frames are errors, never panics.
func TestDecodeMalformedTradeFrames(t *testing.T) {
	for _, f := range [][]byte{tradeRequestFrame(1), tradeOfferFrame(1, 1), tradeRemoveFrame(1),
		tradeSetGoldFrame(1), tradeAcceptFrame(1), tradeCancelFrame()} {
		for n := 0; n < len(f); n++ {
			m, err := decode(f[:n])
			if err == nil {
				m.command(1) // must not panic either
			}
		}
	}
}

func envelopeOf(t *testing.T, buf []byte, typ protocol.Message) flatbuffers.Table {
	t.Helper()
	env := protocol.GetRootAsEnvelope(buf, 0)
	var tbl flatbuffers.Table
	if env.MsgType() != typ || !env.Msg(&tbl) {
		t.Fatalf("got %v, want %v", env.MsgType(), typ)
	}
	return tbl
}

func TestEncodeTradeRequestNotice(t *testing.T) {
	tbl := envelopeOf(t, encodeTradeRequestNotice(42), protocol.MessageTradeRequestNotice)
	var n protocol.TradeRequestNotice
	n.Init(tbl.Bytes, tbl.Pos)
	if n.FromId() != 42 {
		t.Fatalf("from = %d, want 42", n.FromId())
	}
}

func TestEncodeTradeResult(t *testing.T) {
	for r := trade.Completed; r <= trade.InventoryFull; r++ {
		tbl := envelopeOf(t, encodeTradeResult(r), protocol.MessageTradeResult)
		var tr protocol.TradeResult
		tr.Init(tbl.Bytes, tbl.Pos)
		if got := protocol.EnumNamesTradeResultCode[tr.Code()]; got == "" || tr.Code() != protocol.TradeResultCode(r) {
			t.Fatalf("%v: code %v", r, tr.Code())
		}
	}
	if protocol.TradeResultCodeCompleted != protocol.TradeResultCode(trade.Completed) ||
		protocol.TradeResultCodeCancelled != protocol.TradeResultCode(trade.Cancelled) ||
		protocol.TradeResultCodeInventoryFull != protocol.TradeResultCode(trade.InventoryFull) {
		t.Fatal("trade results don't match TradeResultCode")
	}
}

func TestEncodeTradeState(t *testing.T) {
	v := trade.View{
		Partner:  5,
		Screen:   trade.ScreenConfirm,
		Mine:     trade.Offer{Items: []trade.Item{{Slot: 2, Item: 11, Qty: 3}, {Slot: 4, Item: 12, Qty: 1}}, Gold: 70},
		Partners: trade.Offer{Gold: 9},
		// Accepted flags differ so a swap would show.
		MyAccepted: true,
	}
	b := flatbuffers.NewBuilder(0)
	encodeTradeState(b, trade.View{Partner: 1}) // the builder is reused
	tbl := envelopeOf(t, encodeTradeState(b, v), protocol.MessageTradeState)
	var ts protocol.TradeState
	ts.Init(tbl.Bytes, tbl.Pos)
	if ts.PartnerId() != 5 || ts.Screen() != 2 || ts.MyGold() != 70 || ts.PartnerGold() != 9 ||
		!ts.MyAccepted() || ts.PartnerAccepted() || ts.PartnerItemsLength() != 0 {
		t.Fatalf("TradeState = partner %d screen %d gold %d/%d accepted %v/%v partner items %d",
			ts.PartnerId(), ts.Screen(), ts.MyGold(), ts.PartnerGold(), ts.MyAccepted(), ts.PartnerAccepted(), ts.PartnerItemsLength())
	}
	var got []trade.Item
	var it protocol.TradeItem
	for i := 0; i < ts.MyItemsLength(); i++ {
		ts.MyItems(&it, i)
		got = append(got, trade.Item{Item: gamedata.ItemID(it.ItemId()), Qty: int(it.Quantity())})
	}
	if want := []trade.Item{{Item: 11, Qty: 3}, {Item: 12, Qty: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("my items = %+v, want %+v", got, want)
	}
}
