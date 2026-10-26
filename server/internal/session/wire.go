package session

import (
	"fmt"
	"math"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/world"
)

func finish(b *flatbuffers.Builder, typ protocol.Message, msg flatbuffers.UOffsetT) []byte {
	protocol.EnvelopeStart(b)
	protocol.EnvelopeAddMsgType(b, typ)
	protocol.EnvelopeAddMsg(b, msg)
	b.Finish(protocol.EnvelopeEnd(b))
	// The builder is reused, so hand out a copy.
	return append([]byte(nil), b.FinishedBytes()...)
}

func encodeWelcome(id world.PlayerID, token string, spawn world.Vec2, tickRate int) []byte {
	b := flatbuffers.NewBuilder(128)
	tok := b.CreateString(token)
	protocol.WelcomeStart(b)
	protocol.WelcomeAddPlayerId(b, uint16(id))
	protocol.WelcomeAddGuestToken(b, tok)
	protocol.WelcomeAddSpawn(b, protocol.CreateVec2(b, spawn.X, spawn.Y))
	protocol.WelcomeAddTickRate(b, byte(min(tickRate, 255)))
	return finish(b, protocol.MessageWelcome, protocol.WelcomeEnd(b))
}

func encodeReload(reason string) []byte {
	b := flatbuffers.NewBuilder(64)
	r := b.CreateString(reason)
	protocol.ReloadStart(b)
	protocol.ReloadAddReason(b, r)
	return finish(b, protocol.MessageReload, protocol.ReloadEnd(b))
}

func encodeRestartCountdown(seconds uint16) []byte {
	b := flatbuffers.NewBuilder(32)
	protocol.RestartCountdownStart(b)
	protocol.RestartCountdownAddSecondsRemaining(b, seconds)
	return finish(b, protocol.MessageRestartCountdown, protocol.RestartCountdownEnd(b))
}

func encodePong(token uint32) []byte {
	b := flatbuffers.NewBuilder(32)
	protocol.PongStart(b)
	protocol.PongAddToken(b, token)
	return finish(b, protocol.MessagePong, protocol.PongEnd(b))
}

func encodeChat(speaker world.PlayerID, text string) []byte {
	b := flatbuffers.NewBuilder(160)
	t := b.CreateString(text)
	protocol.ChatMessageStart(b)
	protocol.ChatMessageAddSpeakerId(b, uint16(speaker))
	protocol.ChatMessageAddText(b, t)
	return finish(b, protocol.MessageChatMessage, protocol.ChatMessageEnd(b))
}

func encodeShopResult(r shop.Result) []byte {
	b := flatbuffers.NewBuilder(32)
	protocol.ShopResultStart(b)
	protocol.ShopResultAddCode(b, protocol.ShopResultCode(r))
	return finish(b, protocol.MessageShopResult, protocol.ShopResultEnd(b))
}

// decimetres converts world units (metres) to the wire's int16 decimetres.
func decimetres(v float32) int16 {
	d := math.Round(float64(v) * 10)
	return int16(math.Max(math.MinInt16, math.Min(math.MaxInt16, d)))
}

func rotationByte(rad float32) uint8 {
	turns := math.Mod(float64(rad)/(2*math.Pi), 1)
	if turns < 0 {
		turns++
	}
	return uint8(int(turns*256) & 0xff)
}

// encodeSnapshot builds a Snapshot of ents. b is reused across calls.
func encodeSnapshot(b *flatbuffers.Builder, tick uint64, lastSeq uint32, ents []*world.Player) []byte {
	b.Reset()
	protocol.SnapshotStartEntitiesVector(b, len(ents))
	for i := len(ents) - 1; i >= 0; i-- {
		e := ents[i]
		protocol.CreateEntity(b, uint16(e.ID), decimetres(e.Pos.X), decimetres(e.Pos.Y),
			rotationByte(e.Facing), uint8(e.Anim))
	}
	vec := b.EndVector(len(ents))
	protocol.SnapshotStart(b)
	protocol.SnapshotAddTick(b, uint32(tick))
	protocol.SnapshotAddLastInputSeq(b, lastSeq)
	protocol.SnapshotAddEntities(b, vec)
	return finish(b, protocol.MessageSnapshot, protocol.SnapshotEnd(b))
}

// message is a decoded inbound envelope; only the field for typ is set.
type message struct {
	typ       protocol.Message
	version   uint16
	helloTok  string
	seq       uint32
	moveX     float32
	moveZ     float32
	facing    float32
	chatText  string
	emote     protocol.Emote
	pingToken uint32
	// ItemAction, ShopBuy and ShopSell.
	itemAction protocol.ItemActionKind
	slot       int // inventory slot index
	equipSlot  protocol.EquipSlot
	itemID     uint16
	quantity   uint32
}

// gameSlots maps wire equipment slots to game data slots. EquipSlotNone is
// absent: nothing is worn there.
var gameSlots = map[protocol.EquipSlot]gamedata.Slot{
	protocol.EquipSlotWeapon: gamedata.SlotWeapon,
	protocol.EquipSlotHead:   gamedata.SlotHead,
	protocol.EquipSlotBody:   gamedata.SlotBody,
	protocol.EquipSlotShield: gamedata.SlotShield,
}

// command turns a decoded ItemAction, ShopBuy or ShopSell into the world
// command for player id. It returns nil for other messages and for values
// the protocol doesn't define (an unknown action or equipment slot); those
// are ignored, not fatal. Everything else is checked by the systems.
func (m message) command(id world.PlayerID) world.Command {
	switch m.typ {
	case protocol.MessageItemAction:
		switch m.itemAction {
		case protocol.ItemActionKindEquip:
			return items.EquipCommand{Player: id, Slot: m.slot}
		case protocol.ItemActionKindEat:
			return items.EatCommand{Player: id, Slot: m.slot}
		case protocol.ItemActionKindUnequip:
			if slot, ok := gameSlots[m.equipSlot]; ok {
				return items.UnequipCommand{Player: id, Slot: slot}
			}
		}
	case protocol.MessageShopBuy:
		return shop.BuyCommand{Player: id, Item: gamedata.ItemID(m.itemID), Quantity: int(m.quantity)}
	case protocol.MessageShopSell:
		return shop.SellCommand{Player: id, Slot: m.slot, Quantity: int(m.quantity)}
	}
	return nil
}

// decode parses an inbound frame and reads every field it uses, so that all
// reads of client bytes happen here. FlatBuffers readers panic on malformed
// data, so any panic becomes an error.
func decode(buf []byte) (m message, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("malformed frame: %v", r)
		}
	}()
	env := protocol.GetRootAsEnvelope(buf, 0)
	m.typ = env.MsgType()
	var tbl flatbuffers.Table
	if !env.Msg(&tbl) {
		return m, fmt.Errorf("envelope without message")
	}
	switch m.typ {
	case protocol.MessageHello:
		var h protocol.Hello
		h.Init(tbl.Bytes, tbl.Pos)
		m.version, m.helloTok = h.ProtocolVersion(), string(h.GuestToken())
	case protocol.MessageInput:
		var in protocol.Input
		in.Init(tbl.Bytes, tbl.Pos)
		m.seq, m.facing = in.Seq(), in.Facing()
		if v := in.Move(nil); v != nil {
			m.moveX, m.moveZ = v.X(), v.Z()
		}
	case protocol.MessageChatSend:
		var ch protocol.ChatSend
		ch.Init(tbl.Bytes, tbl.Pos)
		m.chatText = string(ch.Text())
	case protocol.MessageEmoteSend:
		var es protocol.EmoteSend
		es.Init(tbl.Bytes, tbl.Pos)
		m.emote = es.Emote()
	case protocol.MessagePing:
		var p protocol.Ping
		p.Init(tbl.Bytes, tbl.Pos)
		m.pingToken = p.Token()
	case protocol.MessageItemAction:
		var ia protocol.ItemAction
		ia.Init(tbl.Bytes, tbl.Pos)
		m.itemAction, m.slot, m.equipSlot = ia.Action(), int(ia.Slot()), ia.EquipSlot()
	case protocol.MessageShopBuy:
		var sb protocol.ShopBuy
		sb.Init(tbl.Bytes, tbl.Pos)
		m.itemID, m.quantity = sb.ItemId(), sb.Quantity()
	case protocol.MessageShopSell:
		var ss protocol.ShopSell
		ss.Init(tbl.Bytes, tbl.Pos)
		m.slot, m.quantity = int(ss.Slot()), ss.Quantity()
	}
	return m, nil
}

// clampU16 and friends saturate a count into a wire field.
func clampU16(v int) uint16 { return uint16(min(max(v, 0), math.MaxUint16)) }
func clampU8(v int) uint8   { return uint8(min(max(v, 0), math.MaxUint8)) }
func clampU32(v int) uint32 { return uint32(min(max(v, 0), math.MaxUint32)) }
func clampI16(v int) int16  { return int16(min(max(v, math.MinInt16), math.MaxInt16)) }

func encodePlayerStats(b *flatbuffers.Builder, st stats) []byte {
	b.Reset()
	protocol.PlayerStatsStart(b)
	protocol.PlayerStatsAddHp(b, clampU16(st.hp))
	protocol.PlayerStatsAddMaxHp(b, clampU16(st.maxHP))
	protocol.PlayerStatsAddCombatLevel(b, clampU8(st.level))
	protocol.PlayerStatsAddXp(b, clampU32(st.xp))
	protocol.PlayerStatsAddGold(b, clampU32(st.gold))
	return finish(b, protocol.MessagePlayerStats, protocol.PlayerStatsEnd(b))
}

func encodeInventoryState(b *flatbuffers.Builder, inv *world.Inventory) []byte {
	b.Reset()
	protocol.InventoryStateStartSlotsVector(b, len(inv.Slots))
	for i := len(inv.Slots) - 1; i >= 0; i-- {
		s := inv.Slots[i]
		protocol.CreateInventorySlot(b, uint16(s.Item), clampU32(s.Qty))
	}
	slots := b.EndVector(len(inv.Slots))
	protocol.InventoryStateStart(b)
	protocol.InventoryStateAddSlots(b, slots)
	protocol.InventoryStateAddWeapon(b, uint16(inv.Equipment.Weapon))
	protocol.InventoryStateAddHead(b, uint16(inv.Equipment.Head))
	protocol.InventoryStateAddBody(b, uint16(inv.Equipment.Body))
	protocol.InventoryStateAddShield(b, uint16(inv.Equipment.Shield))
	return finish(b, protocol.MessageInventoryState, protocol.InventoryStateEnd(b))
}

var wireItemKinds = map[gamedata.ItemKind]protocol.ItemKind{
	gamedata.KindLoot:   protocol.ItemKindLoot,
	gamedata.KindWeapon: protocol.ItemKindWeapon,
	gamedata.KindArmor:  protocol.ItemKindArmor,
	gamedata.KindFood:   protocol.ItemKindFood,
}

var wireEquipSlots = map[gamedata.Slot]protocol.EquipSlot{
	gamedata.SlotNone:   protocol.EquipSlotNone,
	gamedata.SlotWeapon: protocol.EquipSlotWeapon,
	gamedata.SlotHead:   protocol.EquipSlotHead,
	gamedata.SlotBody:   protocol.EquipSlotBody,
	gamedata.SlotShield: protocol.EquipSlotShield,
}

// encodeItemCatalog builds an ItemCatalog of every item in d, in table order.
func encodeItemCatalog(d *gamedata.Data) []byte {
	b := flatbuffers.NewBuilder(4096)
	defs := make([]flatbuffers.UOffsetT, len(d.Items))
	for i, it := range d.Items {
		name := b.CreateString(it.Name)
		price, sells := d.ShopSellPrice(it.ID)
		protocol.ItemDefStart(b)
		protocol.ItemDefAddId(b, uint16(it.ID))
		protocol.ItemDefAddName(b, name)
		protocol.ItemDefAddKind(b, wireItemKinds[it.Kind])
		protocol.ItemDefAddEquipSlot(b, wireEquipSlots[it.Slot])
		protocol.ItemDefAddLevelToEquip(b, clampU8(it.LevelToEquip))
		protocol.ItemDefAddAccuracy(b, clampI16(it.Accuracy))
		protocol.ItemDefAddStrength(b, clampI16(it.Strength))
		protocol.ItemDefAddDefence(b, clampI16(it.Defence))
		protocol.ItemDefAddAttackSpeed(b, clampU8(it.AttackSpeed))
		protocol.ItemDefAddHeal(b, clampU16(it.Heals))
		protocol.ItemDefAddShopBuysFor(b, clampU32(it.ShopBuysFor))
		protocol.ItemDefAddShopSells(b, sells)
		protocol.ItemDefAddShopSellsFor(b, clampU32(price))
		defs[i] = protocol.ItemDefEnd(b)
	}
	protocol.ItemCatalogStartItemsVector(b, len(defs))
	for i := len(defs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(defs[i])
	}
	items := b.EndVector(len(defs))
	protocol.ItemCatalogStart(b)
	protocol.ItemCatalogAddItems(b, items)
	return finish(b, protocol.MessageItemCatalog, protocol.ItemCatalogEnd(b))
}
