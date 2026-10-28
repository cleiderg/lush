package session

import (
	"fmt"
	"math"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/grounditems"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/systems/trade"
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

func encodeWelcome(id world.PlayerID, token string, spawn world.Vec2, tickRate int, walkSpeed, runSpeed float32) []byte {
	b := flatbuffers.NewBuilder(128)
	tok := b.CreateString(token)
	protocol.WelcomeStart(b)
	protocol.WelcomeAddPlayerId(b, uint16(id))
	protocol.WelcomeAddGuestToken(b, tok)
	protocol.WelcomeAddSpawn(b, protocol.CreateVec2(b, spawn.X, spawn.Y))
	protocol.WelcomeAddTickRate(b, byte(min(tickRate, 255)))
	protocol.WelcomeAddWalkSpeed(b, walkSpeed)
	protocol.WelcomeAddRunSpeed(b, runSpeed)
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

func encodeTradeRequestNotice(from world.PlayerID) []byte {
	b := flatbuffers.NewBuilder(32)
	protocol.TradeRequestNoticeStart(b)
	protocol.TradeRequestNoticeAddFromId(b, uint16(from))
	return finish(b, protocol.MessageTradeRequestNotice, protocol.TradeRequestNoticeEnd(b))
}

func encodeTradeResult(r trade.Result) []byte {
	b := flatbuffers.NewBuilder(32)
	protocol.TradeResultStart(b)
	protocol.TradeResultAddCode(b, protocol.TradeResultCode(r))
	return finish(b, protocol.MessageTradeResult, protocol.TradeResultEnd(b))
}

// encodeTradeState builds the trade window as v's player sees it. b is reused
// across calls.
func encodeTradeState(b *flatbuffers.Builder, v trade.View) []byte {
	b.Reset()
	items := func(o trade.Offer) flatbuffers.UOffsetT {
		protocol.TradeStateStartMyItemsVector(b, len(o.Items))
		for i := len(o.Items) - 1; i >= 0; i-- {
			protocol.CreateTradeItem(b, uint16(o.Items[i].Item), clampU32(o.Items[i].Qty))
		}
		return b.EndVector(len(o.Items))
	}
	mine, theirs := items(v.Mine), items(v.Partners)
	protocol.TradeStateStart(b)
	protocol.TradeStateAddPartnerId(b, uint16(v.Partner))
	protocol.TradeStateAddScreen(b, clampU8(v.Screen))
	protocol.TradeStateAddMyItems(b, mine)
	protocol.TradeStateAddMyGold(b, clampU32(v.Mine.Gold))
	protocol.TradeStateAddPartnerItems(b, theirs)
	protocol.TradeStateAddPartnerGold(b, clampU32(v.Partners.Gold))
	protocol.TradeStateAddMyAccepted(b, v.MyAccepted)
	protocol.TradeStateAddPartnerAccepted(b, v.PartnerAccepted)
	return finish(b, protocol.MessageTradeState, protocol.TradeStateEnd(b))
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

// snapEnt is one entity (player or NPC) in a Snapshot.
type snapEnt struct {
	id     world.PlayerID // entity id; NPC ids sit above guest ids
	pos    world.Vec2
	facing float32
	anim   world.Anim
}

func playerEnt(p *world.Player) snapEnt {
	return snapEnt{id: p.ID, pos: p.Pos, facing: p.Facing, anim: p.Anim}
}

func npcEnt(n *world.NPC) snapEnt {
	return snapEnt{id: world.PlayerID(n.ID), pos: n.Pos, facing: n.Facing, anim: n.Anim}
}

// encodeSnapshot builds a Snapshot of ents. b is reused across calls.
func encodeSnapshot(b *flatbuffers.Builder, tick uint64, lastSeq uint32, ents []snapEnt) []byte {
	b.Reset()
	protocol.SnapshotStartEntitiesVector(b, len(ents))
	for i := len(ents) - 1; i >= 0; i-- {
		e := ents[i]
		protocol.CreateEntity(b, uint16(e.id), decimetres(e.pos.X), decimetres(e.pos.Y),
			rotationByte(e.facing), uint8(e.anim))
	}
	vec := b.EndVector(len(ents))
	protocol.SnapshotStart(b)
	protocol.SnapshotAddTick(b, uint32(tick))
	protocol.SnapshotAddLastInputSeq(b, lastSeq)
	protocol.SnapshotAddEntities(b, vec)
	return finish(b, protocol.MessageSnapshot, protocol.SnapshotEnd(b))
}

// entityInfo is what EntityInfo carries for one entity.
type entityInfo struct {
	kind    protocol.EntityKind
	npcType uint16
	level   int
	equip   world.Equipment
}

func encodeEntityInfo(b *flatbuffers.Builder, id world.PlayerID, in entityInfo) []byte {
	b.Reset()
	protocol.EntityInfoStart(b)
	protocol.EntityInfoAddId(b, uint16(id))
	protocol.EntityInfoAddKind(b, in.kind)
	protocol.EntityInfoAddNpcType(b, in.npcType)
	protocol.EntityInfoAddCombatLevel(b, clampU8(in.level))
	protocol.EntityInfoAddWeapon(b, uint16(in.equip.Weapon))
	protocol.EntityInfoAddHead(b, uint16(in.equip.Head))
	protocol.EntityInfoAddBody(b, uint16(in.equip.Body))
	protocol.EntityInfoAddShield(b, uint16(in.equip.Shield))
	return finish(b, protocol.MessageEntityInfo, protocol.EntityInfoEnd(b))
}

// fullHealth is the HealthRatio of an entity at full health.
const fullHealth = 255

// healthRatio is hp / maxHP scaled to 0..255, rounded down: 0 only when dead
// and 255 only at full health.
func healthRatio(hp, maxHP int) uint8 {
	switch {
	case hp <= 0 || maxHP <= 0:
		return 0
	case hp >= maxHP:
		return fullHealth
	}
	return uint8(max(1, hp*fullHealth/maxHP))
}

// healthEnt is one entry of EntityHealth.
type healthEnt struct {
	id    world.PlayerID
	ratio uint8
}

// encodeEntityHealth builds an EntityHealth of hs. b is reused across calls.
func encodeEntityHealth(b *flatbuffers.Builder, hs []healthEnt) []byte {
	b.Reset()
	protocol.EntityHealthStartEntriesVector(b, len(hs))
	for i := len(hs) - 1; i >= 0; i-- {
		protocol.CreateHealthRatio(b, uint16(hs[i].id), hs[i].ratio)
	}
	vec := b.EndVector(len(hs))
	protocol.EntityHealthStart(b)
	protocol.EntityHealthAddEntries(b, vec)
	return finish(b, protocol.MessageEntityHealth, protocol.EntityHealthEnd(b))
}

// hit is one PvP or PvE swing as CombatHit carries it: attacker and target
// are entity ids (NPC ids sit above guest ids, as in snapEnt).
type hit struct {
	attacker, target world.PlayerID
	damage           int
	died             bool
}

// encodeCombatHit builds a CombatHit of hits for world tick tick. b is reused
// across calls.
func encodeCombatHit(b *flatbuffers.Builder, tick uint64, hits []hit) []byte {
	b.Reset()
	protocol.CombatHitStartHitsVector(b, len(hits))
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		protocol.CreateHit(b, uint16(h.attacker), uint16(h.target), clampU8(h.damage), h.died)
	}
	vec := b.EndVector(len(hits))
	protocol.CombatHitStart(b)
	protocol.CombatHitAddTick(b, uint32(tick))
	protocol.CombatHitAddHits(b, vec)
	return finish(b, protocol.MessageCombatHit, protocol.CombatHitEnd(b))
}

// groundEnt is one GroundItem as the wire carries it.
type groundEnt struct {
	id   grounditems.ID
	item uint16
	qty  uint32
	x, z int16
}

func groundEntOf(it grounditems.Item) groundEnt {
	return groundEnt{id: it.ID, item: uint16(it.Item), qty: clampU32(it.Qty), x: decimetres(it.Pos.X), z: decimetres(it.Pos.Y)}
}

// encodeGroundItems builds a GroundItems of gs. b is reused across calls.
func encodeGroundItems(b *flatbuffers.Builder, gs []groundEnt) []byte {
	b.Reset()
	protocol.GroundItemsStartItemsVector(b, len(gs))
	for i := len(gs) - 1; i >= 0; i-- {
		g := gs[i]
		protocol.CreateGroundItem(b, uint16(g.id), g.item, g.qty, g.x, g.z)
	}
	vec := b.EndVector(len(gs))
	protocol.GroundItemsStart(b)
	protocol.GroundItemsAddItems(b, vec)
	return finish(b, protocol.MessageGroundItems, protocol.GroundItemsEnd(b))
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
	run       bool // Input's Run action flag
	chatText  string
	emote     protocol.Emote
	pingToken uint32
	// ItemAction, ShopBuy and ShopSell.
	itemAction protocol.ItemActionKind
	slot       int // inventory slot index
	equipSlot  protocol.EquipSlot
	itemID     uint16
	quantity   uint32
	// Trade messages; slot and quantity are shared with the above.
	partner uint16 // TradeRequest target
	index   int    // TradeRemove offer index
	gold    uint32 // TradeSetGold
	screen  uint8  // TradeAccept
	target  uint16 // AttackTarget entity id
	ground  uint16 // PickUp ground item id
}

// gameSlots maps wire equipment slots to game data slots. EquipSlotNone is
// absent: nothing is worn there.
var gameSlots = map[protocol.EquipSlot]gamedata.Slot{
	protocol.EquipSlotWeapon: gamedata.SlotWeapon,
	protocol.EquipSlotHead:   gamedata.SlotHead,
	protocol.EquipSlotBody:   gamedata.SlotBody,
	protocol.EquipSlotShield: gamedata.SlotShield,
}

// command turns a decoded ItemAction, ShopBuy, ShopSell, trade or PickUp
// message into the world command for player id. It returns nil for other
// messages and for values the protocol doesn't define (an unknown action or
// equipment slot, a trade request naming no player, an accept of a screen
// other than 1 or 2, an offer of nothing, a pick-up of ground item 0); those
// are ignored, not fatal. Everything else is checked
// by the systems.
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
	case protocol.MessageTradeRequest:
		if m.partner != 0 {
			return trade.RequestCommand{Player: id, Partner: world.PlayerID(m.partner)}
		}
	case protocol.MessageTradeOffer:
		if m.quantity != 0 {
			return trade.OfferCommand{Player: id, Slot: m.slot, Quantity: int(m.quantity)}
		}
	case protocol.MessageTradeRemove:
		return trade.RemoveCommand{Player: id, Index: m.index}
	case protocol.MessageTradeSetGold:
		return trade.SetGoldCommand{Player: id, Gold: int(m.gold)}
	case protocol.MessageTradeAccept:
		if m.screen == trade.ScreenOffer || m.screen == trade.ScreenConfirm {
			return trade.AcceptCommand{Player: id, Screen: int(m.screen)}
		}
	case protocol.MessageTradeCancel:
		return trade.CancelCommand{Player: id}
	case protocol.MessagePickUp:
		if m.ground != 0 {
			return grounditems.PickUpCommand{Player: id, ID: grounditems.ID(m.ground)}
		}
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
		m.run = protocol.ActionFlags(in.Actions())&protocol.ActionFlagsRun != 0
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
	case protocol.MessageTradeRequest:
		var tr protocol.TradeRequest
		tr.Init(tbl.Bytes, tbl.Pos)
		m.partner = tr.TargetId()
	case protocol.MessageTradeOffer:
		var to protocol.TradeOffer
		to.Init(tbl.Bytes, tbl.Pos)
		m.slot, m.quantity = int(to.Slot()), to.Quantity()
	case protocol.MessageTradeRemove:
		var tr protocol.TradeRemove
		tr.Init(tbl.Bytes, tbl.Pos)
		m.index = int(tr.Index())
	case protocol.MessageTradeSetGold:
		var tg protocol.TradeSetGold
		tg.Init(tbl.Bytes, tbl.Pos)
		m.gold = tg.Gold()
	case protocol.MessageTradeAccept:
		var ta protocol.TradeAccept
		ta.Init(tbl.Bytes, tbl.Pos)
		m.screen = ta.Screen()
	case protocol.MessageTradeCancel:
		// No fields.
	case protocol.MessageAttackTarget:
		var at protocol.AttackTarget
		at.Init(tbl.Bytes, tbl.Pos)
		m.target = at.TargetId()
	case protocol.MessagePickUp:
		var pu protocol.PickUp
		pu.Init(tbl.Bytes, tbl.Pos)
		m.ground = pu.GroundItemId()
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

// encodeNpcCatalog builds an NpcCatalog of every NPC type in d, in table order.
func encodeNpcCatalog(d *gamedata.Data) []byte {
	b := flatbuffers.NewBuilder(1024)
	defs := make([]flatbuffers.UOffsetT, len(d.NPCs))
	for i, n := range d.NPCs {
		name := b.CreateString(n.Name)
		protocol.NpcDefStart(b)
		protocol.NpcDefAddId(b, uint16(n.ID))
		protocol.NpcDefAddName(b, name)
		protocol.NpcDefAddCombatLevel(b, clampU8(n.Level))
		defs[i] = protocol.NpcDefEnd(b)
	}
	protocol.NpcCatalogStartNpcsVector(b, len(defs))
	for i := len(defs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(defs[i])
	}
	npcs := b.EndVector(len(defs))
	protocol.NpcCatalogStart(b)
	protocol.NpcCatalogAddNpcs(b, npcs)
	return finish(b, protocol.MessageNpcCatalog, protocol.NpcCatalogEnd(b))
}
