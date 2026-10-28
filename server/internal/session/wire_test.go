package session

import (
	"math"
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/grounditems"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/world"
)

func frame(typ protocol.Message, build func(b *flatbuffers.Builder) flatbuffers.UOffsetT) []byte {
	b := flatbuffers.NewBuilder(64)
	return finish(b, typ, build(b))
}

func itemActionFrame(kind protocol.ItemActionKind, slot byte, eq protocol.EquipSlot) []byte {
	return frame(protocol.MessageItemAction, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ItemActionStart(b)
		protocol.ItemActionAddAction(b, kind)
		protocol.ItemActionAddSlot(b, slot)
		protocol.ItemActionAddEquipSlot(b, eq)
		return protocol.ItemActionEnd(b)
	})
}

func shopBuyFrame(item uint16, qty uint32) []byte {
	return frame(protocol.MessageShopBuy, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShopBuyStart(b)
		protocol.ShopBuyAddItemId(b, item)
		protocol.ShopBuyAddQuantity(b, qty)
		return protocol.ShopBuyEnd(b)
	})
}

func shopSellFrame(slot byte, qty uint32) []byte {
	return frame(protocol.MessageShopSell, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShopSellStart(b)
		protocol.ShopSellAddSlot(b, slot)
		protocol.ShopSellAddQuantity(b, qty)
		return protocol.ShopSellEnd(b)
	})
}

// AC1: ItemAction, ShopBuy and ShopSell decode into the item and shop
// commands; values the protocol doesn't define are ignored.
func TestDecodeItemAndShopCommands(t *testing.T) {
	const id = world.PlayerID(7)
	tests := []struct {
		name  string
		frame []byte
		want  world.Command
	}{
		{"equip", itemActionFrame(protocol.ItemActionKindEquip, 4, protocol.EquipSlotNone), items.EquipCommand{Player: id, Slot: 4}},
		{"eat", itemActionFrame(protocol.ItemActionKindEat, 27, protocol.EquipSlotNone), items.EatCommand{Player: id, Slot: 27}},
		{"unequip weapon", itemActionFrame(protocol.ItemActionKindUnequip, 0, protocol.EquipSlotWeapon), items.UnequipCommand{Player: id, Slot: gamedata.SlotWeapon}},
		{"unequip head", itemActionFrame(protocol.ItemActionKindUnequip, 0, protocol.EquipSlotHead), items.UnequipCommand{Player: id, Slot: gamedata.SlotHead}},
		{"unequip body", itemActionFrame(protocol.ItemActionKindUnequip, 0, protocol.EquipSlotBody), items.UnequipCommand{Player: id, Slot: gamedata.SlotBody}},
		{"unequip shield", itemActionFrame(protocol.ItemActionKindUnequip, 0, protocol.EquipSlotShield), items.UnequipCommand{Player: id, Slot: gamedata.SlotShield}},
		{"buy", shopBuyFrame(12, 3), shop.BuyCommand{Player: id, Item: 12, Quantity: 3}},
		{"buy max quantity", shopBuyFrame(65535, 4294967295), shop.BuyCommand{Player: id, Item: 65535, Quantity: 4294967295}},
		{"sell", shopSellFrame(5, 9), shop.SellCommand{Player: id, Slot: 5, Quantity: 9}},
		{"unknown action", itemActionFrame(protocol.ItemActionKind(9), 0, protocol.EquipSlotNone), nil},
		{"unequip nothing", itemActionFrame(protocol.ItemActionKindUnequip, 0, protocol.EquipSlotNone), nil},
		{"unequip unknown slot", itemActionFrame(protocol.ItemActionKindUnequip, 0, protocol.EquipSlot(42)), nil},
		{"other message", frame(protocol.MessagePing, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
			protocol.PingStart(b)
			return protocol.PingEnd(b)
		}), nil},
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

func pickUpFrame(id uint16) []byte {
	return frame(protocol.MessagePickUp, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.PickUpStart(b)
		protocol.PickUpAddGroundItemId(b, id)
		return protocol.PickUpEnd(b)
	})
}

// #242 AC3: PickUp decodes into a ground item pick-up for the sending player;
// ground item 0 (never used) is ignored, and truncated frames never panic.
func TestDecodePickUp(t *testing.T) {
	const id = world.PlayerID(7)
	tests := []struct {
		name  string
		frame []byte
		want  world.Command
	}{
		{"pick up", pickUpFrame(42), grounditems.PickUpCommand{Player: id, ID: 42}},
		{"max id", pickUpFrame(65535), grounditems.PickUpCommand{Player: id, ID: 65535}},
		{"id 0", pickUpFrame(0), nil},
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
	f := pickUpFrame(42)
	for n := 0; n < len(f); n++ {
		if m, err := decode(f[:n]); err == nil {
			m.command(id)
		}
	}
}

// AC1: truncated frames of each message are errors, never panics.
func TestDecodeMalformedItemAndShopFrames(t *testing.T) {
	for _, f := range [][]byte{
		itemActionFrame(protocol.ItemActionKindEquip, 1, protocol.EquipSlotNone),
		shopBuyFrame(1, 1),
		shopSellFrame(1, 1),
	} {
		for n := 0; n < len(f); n++ {
			m, err := decode(f[:n])
			if err == nil {
				m.command(1) // must not panic either
			}
		}
	}
}

func TestEncodeShopResult(t *testing.T) {
	for _, r := range []shop.Result{shop.Ok, shop.TooFar, shop.NotEnoughGold, shop.InventoryFull, shop.ItemNotSold} {
		env := protocol.GetRootAsEnvelope(encodeShopResult(r), 0)
		var tbl flatbuffers.Table
		if env.MsgType() != protocol.MessageShopResult || !env.Msg(&tbl) {
			t.Fatalf("%v: not a ShopResult", r)
		}
		var sr protocol.ShopResult
		sr.Init(tbl.Bytes, tbl.Pos)
		if want := protocol.EnumValuesShopResultCode[map[shop.Result]string{
			shop.Ok: "Ok", shop.TooFar: "TooFar", shop.NotEnoughGold: "NotEnoughGold",
			shop.InventoryFull: "InventoryFull", shop.ItemNotSold: "ItemNotSold"}[r]]; sr.Code() != want {
			t.Fatalf("%v: code %v, want %v", r, sr.Code(), want)
		}
	}
}

// AC4: a Snapshot with 100 entities, players and NPCs, fits the networking
// spec's ~1,200 byte datagram budget.
func TestSnapshotOf100EntitiesUnderBudget(t *testing.T) {
	ents := make([]snapEnt, 0, 100)
	for i := 0; i < 50; i++ {
		ents = append(ents, playerEnt(&world.Player{ID: world.PlayerID(i + 1), Pos: world.Vec2{X: -3000, Y: 3000}, Facing: 3, Anim: world.AnimWalk}))
		ents = append(ents, npcEnt(&world.NPC{ID: world.DefaultNPCIDMax - world.NPCID(i), Pos: world.Vec2{X: 3000, Y: -3000}, Facing: 1, Anim: world.AnimWalk}))
	}
	b := flatbuffers.NewBuilder(1024)
	buf := encodeSnapshot(b, math.MaxUint32, math.MaxUint32, ents)
	if len(buf) >= 1200 {
		t.Fatalf("snapshot of %d entities is %d bytes, want under 1200", len(ents), len(buf))
	}
	env := protocol.GetRootAsEnvelope(buf, 0)
	var tbl flatbuffers.Table
	env.Msg(&tbl)
	var s protocol.Snapshot
	s.Init(tbl.Bytes, tbl.Pos)
	var e protocol.Entity
	if s.EntitiesLength() != 100 || !s.Entities(&e, 1) || e.Id() != uint16(world.DefaultNPCIDMax) {
		t.Fatalf("decoded %d entities, second id %d", s.EntitiesLength(), e.Id())
	}
	t.Logf("snapshot of 100 entities: %d bytes", len(buf))
}
