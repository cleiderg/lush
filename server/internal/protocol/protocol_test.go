package protocol

import (
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
)

func finish(b *flatbuffers.Builder, typ Message, msg flatbuffers.UOffsetT) []byte {
	EnvelopeStart(b)
	EnvelopeAddMsgType(b, typ)
	EnvelopeAddMsg(b, msg)
	b.Finish(EnvelopeEnd(b))
	return b.FinishedBytes()
}

// open decodes the envelope and returns the union table for typ.
func open(t *testing.T, buf []byte, typ Message) flatbuffers.Table {
	t.Helper()
	env := GetRootAsEnvelope(buf, 0)
	if env.MsgType() != typ {
		t.Fatalf("message type = %v, want %v", env.MsgType(), typ)
	}
	var tbl flatbuffers.Table
	if !env.Msg(&tbl) {
		t.Fatal("envelope has no message")
	}
	return tbl
}

func buildSnapshot(n int) []byte {
	b := flatbuffers.NewBuilder(1024)
	SnapshotStartEntitiesVector(b, n)
	for i := n - 1; i >= 0; i-- {
		CreateEntity(b, uint16(i), int16(i*10-500), int16(-i*7), uint8(i), uint8(i%4))
	}
	ents := b.EndVector(n)
	SnapshotStart(b)
	SnapshotAddTick(b, 12345)
	SnapshotAddLastInputSeq(b, 678)
	SnapshotAddEntities(b, ents)
	return finish(b, MessageSnapshot, SnapshotEnd(b))
}

func TestVersionConstant(t *testing.T) {
	if ProtocolVersionCurrent != 15 {
		t.Fatalf("ProtocolVersionCurrent = %d, want 15", ProtocolVersionCurrent)
	}
}

func TestSnapshotSize(t *testing.T) {
	buf := buildSnapshot(101)
	if len(buf) >= 1200 {
		t.Fatalf("snapshot with 101 entities is %d bytes, want < 1200", len(buf))
	}
	t.Logf("snapshot with 101 entities: %d bytes", len(buf))
}

func TestSnapshotSize100(t *testing.T) {
	buf := buildSnapshot(100)
	if len(buf) >= 1200 {
		t.Fatalf("snapshot with 100 entities is %d bytes, want < 1200", len(buf))
	}
}

// Each extra Snapshot entity adds exactly 8 bytes, so Entity stays 8 bytes.
func TestEntityIsEightBytes(t *testing.T) {
	if d := len(buildSnapshot(51)) - len(buildSnapshot(50)); d != 8 {
		t.Fatalf("one more entity adds %d bytes, want 8", d)
	}
}

func TestRoundTripHello(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	tok := b.CreateString("guest-abc")
	HelloStart(b)
	HelloAddProtocolVersion(b, uint16(ProtocolVersionCurrent))
	HelloAddGuestToken(b, tok)
	buf := finish(b, MessageHello, HelloEnd(b))

	var m Hello
	tbl := open(t, buf, MessageHello)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.ProtocolVersion() != uint16(ProtocolVersionCurrent) || string(m.GuestToken()) != "guest-abc" {
		t.Fatalf("hello mismatch: %d %q", m.ProtocolVersion(), m.GuestToken())
	}
}

func TestRoundTripWelcome(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	tok := b.CreateString("tok")
	WelcomeStart(b)
	WelcomeAddPlayerId(b, 42)
	WelcomeAddGuestToken(b, tok)
	WelcomeAddSpawn(b, CreateVec2(b, 1.5, -2.5))
	WelcomeAddTickRate(b, 20)
	WelcomeAddWalkSpeed(b, 4)
	WelcomeAddRunSpeed(b, 7)
	WelcomeAddServerTimeMs(b, 1798761600000)
	buf := finish(b, MessageWelcome, WelcomeEnd(b))

	var m Welcome
	tbl := open(t, buf, MessageWelcome)
	m.Init(tbl.Bytes, tbl.Pos)
	sp := m.Spawn(nil)
	if m.PlayerId() != 42 || string(m.GuestToken()) != "tok" || sp.X() != 1.5 || sp.Z() != -2.5 || m.TickRate() != 20 ||
		m.WalkSpeed() != 4 || m.RunSpeed() != 7 || m.ServerTimeMs() != 1798761600000 {
		t.Fatalf("welcome mismatch")
	}
}

func TestRoundTripInput(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	InputStart(b)
	InputAddSeq(b, 99)
	InputAddMove(b, CreateVec2(b, 0.5, -1))
	InputAddFacing(b, 3.25)
	InputAddActions(b, uint8(ActionFlagsAttack|ActionFlagsJump|ActionFlagsRun))
	buf := finish(b, MessageInput, InputEnd(b))

	var m Input
	tbl := open(t, buf, MessageInput)
	m.Init(tbl.Bytes, tbl.Pos)
	mv := m.Move(nil)
	if m.Seq() != 99 || mv.X() != 0.5 || mv.Z() != -1 || m.Facing() != 3.25 ||
		ActionFlags(m.Actions()) != ActionFlagsAttack|ActionFlagsJump|ActionFlagsRun {
		t.Fatalf("input mismatch")
	}
}

func TestRoundTripSnapshot(t *testing.T) {
	buf := buildSnapshot(101)
	var m Snapshot
	tbl := open(t, buf, MessageSnapshot)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.Tick() != 12345 || m.LastInputSeq() != 678 || m.EntitiesLength() != 101 {
		t.Fatalf("snapshot header mismatch")
	}
	var e Entity
	for i := 0; i < 101; i++ {
		if !m.Entities(&e, i) {
			t.Fatalf("entity %d missing", i)
		}
		if e.Id() != uint16(i) || e.X() != int16(i*10-500) || e.Z() != int16(-i*7) ||
			e.Rotation() != uint8(i) || e.Animation() != uint8(i%4) {
			t.Fatalf("entity %d mismatch", i)
		}
	}
}

func TestRoundTripChat(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	txt := b.CreateString("hello anteaters")
	ChatSendStart(b)
	ChatSendAddText(b, txt)
	buf := finish(b, MessageChatSend, ChatSendEnd(b))
	var s ChatSend
	tbl := open(t, buf, MessageChatSend)
	s.Init(tbl.Bytes, tbl.Pos)
	if string(s.Text()) != "hello anteaters" {
		t.Fatalf("chat send mismatch")
	}

	b = flatbuffers.NewBuilder(64)
	txt = b.CreateString("zot")
	ChatMessageStart(b)
	ChatMessageAddSpeakerId(b, 7)
	ChatMessageAddText(b, txt)
	buf = finish(b, MessageChatMessage, ChatMessageEnd(b))
	var c ChatMessage
	tbl = open(t, buf, MessageChatMessage)
	c.Init(tbl.Bytes, tbl.Pos)
	if c.SpeakerId() != 7 || string(c.Text()) != "zot" {
		t.Fatalf("chat message mismatch")
	}
}

func TestRoundTripPingPong(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	PingStart(b)
	PingAddToken(b, 0xdeadbeef)
	buf := finish(b, MessagePing, PingEnd(b))
	var p Ping
	tbl := open(t, buf, MessagePing)
	p.Init(tbl.Bytes, tbl.Pos)
	if p.Token() != 0xdeadbeef {
		t.Fatalf("ping mismatch")
	}

	b = flatbuffers.NewBuilder(32)
	PongStart(b)
	PongAddToken(b, 0xcafef00d)
	buf = finish(b, MessagePong, PongEnd(b))
	var q Pong
	tbl = open(t, buf, MessagePong)
	q.Init(tbl.Bytes, tbl.Pos)
	if q.Token() != 0xcafef00d {
		t.Fatalf("pong mismatch")
	}
}

func TestRoundTripReload(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	r := b.CreateString("protocol mismatch")
	ReloadStart(b)
	ReloadAddReason(b, r)
	buf := finish(b, MessageReload, ReloadEnd(b))
	var m Reload
	tbl := open(t, buf, MessageReload)
	m.Init(tbl.Bytes, tbl.Pos)
	if string(m.Reason()) != "protocol mismatch" {
		t.Fatalf("reload mismatch")
	}
}

func TestRoundTripRestartCountdown(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	RestartCountdownStart(b)
	RestartCountdownAddSecondsRemaining(b, 42)
	buf := finish(b, MessageRestartCountdown, RestartCountdownEnd(b))
	var m RestartCountdown
	tbl := open(t, buf, MessageRestartCountdown)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.SecondsRemaining() != 42 {
		t.Fatalf("seconds_remaining = %d, want 42", m.SecondsRemaining())
	}
}

func TestRoundTripEmoteSend(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	EmoteSendStart(b)
	EmoteSendAddEmote(b, EmoteWave)
	buf := finish(b, MessageEmoteSend, EmoteSendEnd(b))
	var m EmoteSend
	tbl := open(t, buf, MessageEmoteSend)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.Emote() != EmoteWave {
		t.Fatalf("emote = %v, want Wave", m.Emote())
	}
}

func TestRoundTripWaveAnimation(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	SnapshotStartEntitiesVector(b, 1)
	CreateEntity(b, 7, 10, 20, 30, uint8(AnimationWave))
	ents := b.EndVector(1)
	SnapshotStart(b)
	SnapshotAddEntities(b, ents)
	buf := finish(b, MessageSnapshot, SnapshotEnd(b))
	var m Snapshot
	tbl := open(t, buf, MessageSnapshot)
	m.Init(tbl.Bytes, tbl.Pos)
	var e Entity
	if !m.Entities(&e, 0) || Animation(e.Animation()) != AnimationWave {
		t.Fatalf("animation = %d, want Wave", e.Animation())
	}
}

func TestRoundTripPlayerStats(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	PlayerStatsStart(b)
	PlayerStatsAddHp(b, 7)
	PlayerStatsAddMaxHp(b, 39)
	PlayerStatsAddCombatLevel(b, 30)
	PlayerStatsAddXp(b, 13363)
	PlayerStatsAddGold(b, 4000000000)
	buf := finish(b, MessagePlayerStats, PlayerStatsEnd(b))
	var m PlayerStats
	tbl := open(t, buf, MessagePlayerStats)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.Hp() != 7 || m.MaxHp() != 39 || m.CombatLevel() != 30 || m.Xp() != 13363 || m.Gold() != 4000000000 {
		t.Fatalf("player stats mismatch: %d %d %d %d %d", m.Hp(), m.MaxHp(), m.CombatLevel(), m.Xp(), m.Gold())
	}
}

func TestRoundTripInventoryState(t *testing.T) {
	// 28 slots: slot 0 holds 3 Boba (id 9), slot 5 holds 250 acorns (id 11),
	// everything else is empty.
	want := make([][2]uint32, 28)
	want[0] = [2]uint32{9, 3}
	want[5] = [2]uint32{11, 250}
	b := flatbuffers.NewBuilder(256)
	InventoryStateStartSlotsVector(b, len(want))
	for i := len(want) - 1; i >= 0; i-- {
		CreateInventorySlot(b, uint16(want[i][0]), want[i][1])
	}
	slots := b.EndVector(len(want))
	InventoryStateStart(b)
	InventoryStateAddSlots(b, slots)
	InventoryStateAddWeapon(b, 1)
	InventoryStateAddHead(b, uint16(EmptyItemId))
	InventoryStateAddBody(b, 4)
	InventoryStateAddShield(b, uint16(EmptyItemId))
	buf := finish(b, MessageInventoryState, InventoryStateEnd(b))

	var m InventoryState
	tbl := open(t, buf, MessageInventoryState)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.SlotsLength() != 28 {
		t.Fatalf("slots = %d, want 28", m.SlotsLength())
	}
	var s InventorySlot
	for i, w := range want {
		if !m.Slots(&s, i) {
			t.Fatalf("slot %d missing", i)
		}
		if uint32(s.ItemId()) != w[0] || s.Quantity() != w[1] {
			t.Fatalf("slot %d = (%d, %d), want %v", i, s.ItemId(), s.Quantity(), w)
		}
	}
	if !m.Slots(&s, 1) || s.ItemId() != uint16(EmptyItemId) {
		t.Fatalf("slot 1 should be empty")
	}
	if m.Weapon() != 1 || m.Head() != uint16(EmptyItemId) || m.Body() != 4 || m.Shield() != uint16(EmptyItemId) {
		t.Fatalf("equipment mismatch: %d %d %d %d", m.Weapon(), m.Head(), m.Body(), m.Shield())
	}
}

type itemDef struct {
	id                          uint16
	name                        string
	kind                        ItemKind
	slot                        EquipSlot
	level                       uint8
	accuracy, strength, defence int16
	speed                       uint8
	heal                        uint16
	buysFor                     uint32
	sells                       bool
	sellsFor                    uint32
	toolSkill                   ToolSkill
	toolLevel, cookingLevel     uint8
}

func TestRoundTripItemCatalog(t *testing.T) {
	want := []itemDef{
		{1, "Wooden stick", ItemKindWeapon, EquipSlotWeapon, 1, 3, 2, 0, 4, 0, 2, true, 5, ToolSkillNone, 0, 0},
		{8, "Robot plating", ItemKindArmor, EquipSlotBody, 12, -2, 0, 14, 0, 0, 150, false, 0, ToolSkillNone, 0, 0},
		{9, "Boba", ItemKindFood, EquipSlotNone, 1, 0, 0, 0, 0, 4, 1, true, 5, ToolSkillNone, 0, 0},
		{11, "Acorn", ItemKindLoot, EquipSlotNone, 0, 0, 0, 0, 0, 0, 1, false, 0, ToolSkillNone, 0, 0},
		{7, "Bike helmet", ItemKindArmor, EquipSlotHead, 10, 0, 0, 6, 0, 0, 30, true, 80, ToolSkillNone, 0, 0},
		{5, "Cardboard shield", ItemKindArmor, EquipSlotShield, 1, 0, 0, 3, 0, 0, 3, true, 10, ToolSkillNone, 0, 0},
		{20, "Steel hatchet", ItemKindWeapon, EquipSlotWeapon, 5, 8, 9, 0, 5, 0, 70, true, 200, ToolSkillWoodcutting, 15, 0},
		{22, "Small net", ItemKindLoot, EquipSlotNone, 0, 0, 0, 0, 0, 0, 5, true, 20, ToolSkillFishing, 1, 0},
		{30, "Raw trout", ItemKindLoot, EquipSlotNone, 0, 0, 0, 0, 0, 0, 8, false, 0, ToolSkillNone, 0, 15},
	}
	b := flatbuffers.NewBuilder(512)
	offs := make([]flatbuffers.UOffsetT, len(want))
	for i, w := range want {
		name := b.CreateString(w.name)
		ItemDefStart(b)
		ItemDefAddId(b, w.id)
		ItemDefAddName(b, name)
		ItemDefAddKind(b, w.kind)
		ItemDefAddEquipSlot(b, w.slot)
		ItemDefAddLevelToEquip(b, w.level)
		ItemDefAddAccuracy(b, w.accuracy)
		ItemDefAddStrength(b, w.strength)
		ItemDefAddDefence(b, w.defence)
		ItemDefAddAttackSpeed(b, w.speed)
		ItemDefAddHeal(b, w.heal)
		ItemDefAddShopBuysFor(b, w.buysFor)
		ItemDefAddShopSells(b, w.sells)
		ItemDefAddShopSellsFor(b, w.sellsFor)
		ItemDefAddToolSkill(b, w.toolSkill)
		ItemDefAddToolLevel(b, w.toolLevel)
		ItemDefAddCookingLevel(b, w.cookingLevel)
		offs[i] = ItemDefEnd(b)
	}
	ItemCatalogStartItemsVector(b, len(offs))
	for i := len(offs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(offs[i])
	}
	items := b.EndVector(len(offs))
	ItemCatalogStart(b)
	ItemCatalogAddItems(b, items)
	buf := finish(b, MessageItemCatalog, ItemCatalogEnd(b))

	var m ItemCatalog
	tbl := open(t, buf, MessageItemCatalog)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.ItemsLength() != len(want) {
		t.Fatalf("items = %d, want %d", m.ItemsLength(), len(want))
	}
	var d ItemDef
	for i, w := range want {
		if !m.Items(&d, i) {
			t.Fatalf("item %d missing", i)
		}
		got := itemDef{d.Id(), string(d.Name()), d.Kind(), d.EquipSlot(), d.LevelToEquip(),
			d.Accuracy(), d.Strength(), d.Defence(), d.AttackSpeed(), d.Heal(),
			d.ShopBuysFor(), d.ShopSells(), d.ShopSellsFor(), d.ToolSkill(), d.ToolLevel(), d.CookingLevel()}
		if got != w {
			t.Fatalf("item %d = %+v, want %+v", i, got, w)
		}
	}
}

type entityInfo struct {
	id                         uint16
	kind                       EntityKind
	npcType                    uint16
	level                      uint8
	weapon, head, body, shield uint16
	totalLevel                 uint16
}

func TestRoundTripEntityInfo(t *testing.T) {
	for _, w := range []entityInfo{
		{42, EntityKindPlayer, 0, 30, 1, 7, 8, 5, 87},
		{43, EntityKindPlayer, 0, 3, 0, 0, 0, 0, 4},
		{300, EntityKindNpc, 6, 20, uint16(EmptyItemId), uint16(EmptyItemId), uint16(EmptyItemId), uint16(EmptyItemId), 0},
		{65535, EntityKindNpc, 65535, 255, 65535, 65535, 65535, 65535, 65535},
	} {
		b := flatbuffers.NewBuilder(64)
		EntityInfoStart(b)
		EntityInfoAddId(b, w.id)
		EntityInfoAddKind(b, w.kind)
		EntityInfoAddNpcType(b, w.npcType)
		EntityInfoAddCombatLevel(b, w.level)
		EntityInfoAddWeapon(b, w.weapon)
		EntityInfoAddHead(b, w.head)
		EntityInfoAddBody(b, w.body)
		EntityInfoAddShield(b, w.shield)
		EntityInfoAddTotalLevel(b, w.totalLevel)
		buf := finish(b, MessageEntityInfo, EntityInfoEnd(b))

		var m EntityInfo
		tbl := open(t, buf, MessageEntityInfo)
		m.Init(tbl.Bytes, tbl.Pos)
		got := entityInfo{m.Id(), m.Kind(), m.NpcType(), m.CombatLevel(), m.Weapon(), m.Head(), m.Body(), m.Shield(), m.TotalLevel()}
		if got != w {
			t.Fatalf("entity info = %+v, want %+v", got, w)
		}
	}
}

func TestRoundTripEntityHealth(t *testing.T) {
	want := [][2]int{{1, 255}, {300, 128}, {65535, 0}}
	b := flatbuffers.NewBuilder(64)
	EntityHealthStartEntriesVector(b, len(want))
	for i := len(want) - 1; i >= 0; i-- {
		CreateHealthRatio(b, uint16(want[i][0]), uint8(want[i][1]))
	}
	entries := b.EndVector(len(want))
	EntityHealthStart(b)
	EntityHealthAddEntries(b, entries)
	buf := finish(b, MessageEntityHealth, EntityHealthEnd(b))

	var m EntityHealth
	tbl := open(t, buf, MessageEntityHealth)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.EntriesLength() != len(want) {
		t.Fatalf("entries = %d, want %d", m.EntriesLength(), len(want))
	}
	var h HealthRatio
	for i, w := range want {
		if !m.Entries(&h, i) || int(h.Id()) != w[0] || int(h.Ratio()) != w[1] {
			t.Fatalf("entry %d = (%d, %d), want %v", i, h.Id(), h.Ratio(), w)
		}
	}
}

type itemAction struct {
	action    ItemActionKind
	slot      uint8
	equipSlot EquipSlot
}

func TestRoundTripItemAction(t *testing.T) {
	for _, w := range []itemAction{
		{ItemActionKindEquip, 0, EquipSlotNone},
		{ItemActionKindEat, 27, EquipSlotNone},
		{ItemActionKindUnequip, 0, EquipSlotShield},
		{ItemActionKindUnequip, 255, EquipSlotWeapon},
	} {
		b := flatbuffers.NewBuilder(64)
		ItemActionStart(b)
		ItemActionAddAction(b, w.action)
		ItemActionAddSlot(b, w.slot)
		ItemActionAddEquipSlot(b, w.equipSlot)
		buf := finish(b, MessageItemAction, ItemActionEnd(b))

		var m ItemAction
		tbl := open(t, buf, MessageItemAction)
		m.Init(tbl.Bytes, tbl.Pos)
		if got := (itemAction{m.Action(), m.Slot(), m.EquipSlot()}); got != w {
			t.Fatalf("item action = %+v, want %+v", got, w)
		}
	}
}

type groundItem struct {
	id, itemID uint16
	quantity   uint32
	x, z       int16
}

func TestRoundTripGroundItems(t *testing.T) {
	want := []groundItem{
		{1, 11, 1, 0, 0},
		{300, 13, 4294967295, -32768, 32767},
		{65535, 65535, 25, 1234, -987},
	}
	b := flatbuffers.NewBuilder(64)
	GroundItemsStartItemsVector(b, len(want))
	for i := len(want) - 1; i >= 0; i-- {
		w := want[i]
		CreateGroundItem(b, w.id, w.itemID, w.quantity, w.x, w.z)
	}
	items := b.EndVector(len(want))
	GroundItemsStart(b)
	GroundItemsAddItems(b, items)
	buf := finish(b, MessageGroundItems, GroundItemsEnd(b))

	var m GroundItems
	tbl := open(t, buf, MessageGroundItems)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.ItemsLength() != len(want) {
		t.Fatalf("items = %d, want %d", m.ItemsLength(), len(want))
	}
	var g GroundItem
	for i, w := range want {
		if !m.Items(&g, i) {
			t.Fatalf("item %d missing", i)
		}
		if got := (groundItem{g.Id(), g.ItemId(), g.Quantity(), g.X(), g.Z()}); got != w {
			t.Fatalf("item %d = %+v, want %+v", i, got, w)
		}
	}
}

// Each extra ground item adds exactly 12 bytes.
func TestGroundItemIsTwelveBytes(t *testing.T) {
	build := func(n int) int {
		b := flatbuffers.NewBuilder(64)
		GroundItemsStartItemsVector(b, n)
		for i := 0; i < n; i++ {
			CreateGroundItem(b, uint16(i), 1, 1, 0, 0)
		}
		items := b.EndVector(n)
		GroundItemsStart(b)
		GroundItemsAddItems(b, items)
		return len(finish(b, MessageGroundItems, GroundItemsEnd(b)))
	}
	if d := build(51) - build(50); d != 12 {
		t.Fatalf("one more ground item adds %d bytes, want 12", d)
	}
}

func TestRoundTripPickUp(t *testing.T) {
	for _, id := range []uint16{0, 1, 65535} {
		b := flatbuffers.NewBuilder(32)
		PickUpStart(b)
		PickUpAddGroundItemId(b, id)
		buf := finish(b, MessagePickUp, PickUpEnd(b))

		var m PickUp
		tbl := open(t, buf, MessagePickUp)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.GroundItemId() != id {
			t.Fatalf("ground item id = %d, want %d", m.GroundItemId(), id)
		}
	}
}

func TestRoundTripShopBuy(t *testing.T) {
	for _, w := range [][2]uint32{{1, 1}, {10, 28}, {65535, 4294967295}} {
		b := flatbuffers.NewBuilder(32)
		ShopBuyStart(b)
		ShopBuyAddItemId(b, uint16(w[0]))
		ShopBuyAddQuantity(b, w[1])
		buf := finish(b, MessageShopBuy, ShopBuyEnd(b))

		var m ShopBuy
		tbl := open(t, buf, MessageShopBuy)
		m.Init(tbl.Bytes, tbl.Pos)
		if uint32(m.ItemId()) != w[0] || m.Quantity() != w[1] {
			t.Fatalf("shop buy = (%d, %d), want %v", m.ItemId(), m.Quantity(), w)
		}
	}
}

func TestRoundTripShopSell(t *testing.T) {
	for _, w := range [][2]uint32{{0, 1}, {27, 500}, {255, 4294967295}} {
		b := flatbuffers.NewBuilder(32)
		ShopSellStart(b)
		ShopSellAddSlot(b, uint8(w[0]))
		ShopSellAddQuantity(b, w[1])
		buf := finish(b, MessageShopSell, ShopSellEnd(b))

		var m ShopSell
		tbl := open(t, buf, MessageShopSell)
		m.Init(tbl.Bytes, tbl.Pos)
		if uint32(m.Slot()) != w[0] || m.Quantity() != w[1] {
			t.Fatalf("shop sell = (%d, %d), want %v", m.Slot(), m.Quantity(), w)
		}
	}
}

func TestRoundTripShopResult(t *testing.T) {
	for _, code := range []ShopResultCode{
		ShopResultCodeOk, ShopResultCodeTooFar, ShopResultCodeNotEnoughGold,
		ShopResultCodeInventoryFull, ShopResultCodeItemNotSold,
	} {
		b := flatbuffers.NewBuilder(32)
		ShopResultStart(b)
		ShopResultAddCode(b, code)
		buf := finish(b, MessageShopResult, ShopResultEnd(b))

		var m ShopResult
		tbl := open(t, buf, MessageShopResult)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Code() != code {
			t.Fatalf("shop result = %v, want %v", m.Code(), code)
		}
	}
}

func TestRoundTripAttackTarget(t *testing.T) {
	for _, id := range []uint16{0, 300, 65535} {
		b := flatbuffers.NewBuilder(32)
		AttackTargetStart(b)
		AttackTargetAddTargetId(b, id)
		buf := finish(b, MessageAttackTarget, AttackTargetEnd(b))

		var m AttackTarget
		tbl := open(t, buf, MessageAttackTarget)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.TargetId() != id {
			t.Fatalf("target id = %d, want %d", m.TargetId(), id)
		}
	}
}

type hit struct {
	attacker, target uint16
	damage           uint8
	died             bool
}

func buildCombatHit(tick uint32, hits []hit) []byte {
	b := flatbuffers.NewBuilder(256)
	CombatHitStartHitsVector(b, len(hits))
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		CreateHit(b, h.attacker, h.target, h.damage, h.died)
	}
	vec := b.EndVector(len(hits))
	CombatHitStart(b)
	CombatHitAddTick(b, tick)
	CombatHitAddHits(b, vec)
	return finish(b, MessageCombatHit, CombatHitEnd(b))
}

func TestRoundTripCombatHit(t *testing.T) {
	want := []hit{{1, 300, 3, false}, {300, 1, 0, false}, {2, 300, 255, true}, {65535, 65535, 0, true}}
	buf := buildCombatHit(123456, want)

	var m CombatHit
	tbl := open(t, buf, MessageCombatHit)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.Tick() != 123456 {
		t.Fatalf("tick = %d, want 123456", m.Tick())
	}
	if m.HitsLength() != len(want) {
		t.Fatalf("hits = %d, want %d", m.HitsLength(), len(want))
	}
	var h Hit
	for i, w := range want {
		if !m.Hits(&h, i) {
			t.Fatalf("hit %d missing", i)
		}
		got := hit{h.Attacker(), h.Target(), h.Damage(), h.Died()}
		if got != w {
			t.Fatalf("hit %d = %+v, want %+v", i, got, w)
		}
	}
}

// A CombatHit with 20 hits fits the networking spec's datagram budget, and
// each hit is a fixed 6-byte struct.
func TestCombatHitSize(t *testing.T) {
	hits := make([]hit, 20)
	for i := range hits {
		hits[i] = hit{uint16(i), uint16(1000 + i), uint8(i), i%5 == 0}
	}
	buf := buildCombatHit(99999, hits)
	if len(buf) >= 1200 {
		t.Fatalf("CombatHit with 20 hits is %d bytes, want < 1200", len(buf))
	}
	t.Logf("CombatHit with 20 hits: %d bytes", len(buf))
	// Compare even counts: the vector length is 4-byte aligned, so an odd
	// count can add 2 bytes of padding.
	if d := len(buildCombatHit(1, hits[:12])) - len(buildCombatHit(1, hits[:10])); d != 12 {
		t.Fatalf("two more hits add %d bytes, want 12", d)
	}
}

func TestRoundTripNpcCatalog(t *testing.T) {
	type npc struct {
		id    uint16
		name  string
		level uint8
	}
	want := []npc{{1, "Squirrel", 1}, {6, "Feral Anteater", 20}, {65535, "", 255}}
	b := flatbuffers.NewBuilder(128)
	offs := make([]flatbuffers.UOffsetT, len(want))
	for i, w := range want {
		name := b.CreateString(w.name)
		NpcDefStart(b)
		NpcDefAddId(b, w.id)
		NpcDefAddName(b, name)
		NpcDefAddCombatLevel(b, w.level)
		offs[i] = NpcDefEnd(b)
	}
	NpcCatalogStartNpcsVector(b, len(offs))
	for i := len(offs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(offs[i])
	}
	vec := b.EndVector(len(offs))
	NpcCatalogStart(b)
	NpcCatalogAddNpcs(b, vec)
	buf := finish(b, MessageNpcCatalog, NpcCatalogEnd(b))

	var m NpcCatalog
	tbl := open(t, buf, MessageNpcCatalog)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.NpcsLength() != len(want) {
		t.Fatalf("npcs = %d, want %d", m.NpcsLength(), len(want))
	}
	var d NpcDef
	for i, w := range want {
		if !m.Npcs(&d, i) {
			t.Fatalf("npc %d missing", i)
		}
		got := npc{d.Id(), string(d.Name()), d.CombatLevel()}
		if got != w {
			t.Fatalf("npc %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestRoundTripTradeRequest(t *testing.T) {
	for _, id := range []uint16{0, 300, 65535} {
		b := flatbuffers.NewBuilder(32)
		TradeRequestStart(b)
		TradeRequestAddTargetId(b, id)
		buf := finish(b, MessageTradeRequest, TradeRequestEnd(b))

		var m TradeRequest
		tbl := open(t, buf, MessageTradeRequest)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.TargetId() != id {
			t.Fatalf("target id = %d, want %d", m.TargetId(), id)
		}
	}
}

func TestRoundTripTradeOffer(t *testing.T) {
	for _, w := range []struct {
		slot uint8
		qty  uint32
	}{{0, 1}, {27, 5000}, {255, 4294967295}} {
		b := flatbuffers.NewBuilder(32)
		TradeOfferStart(b)
		TradeOfferAddSlot(b, w.slot)
		TradeOfferAddQuantity(b, w.qty)
		buf := finish(b, MessageTradeOffer, TradeOfferEnd(b))

		var m TradeOffer
		tbl := open(t, buf, MessageTradeOffer)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Slot() != w.slot || m.Quantity() != w.qty {
			t.Fatalf("offer = %d x%d, want %d x%d", m.Slot(), m.Quantity(), w.slot, w.qty)
		}
	}
}

func TestRoundTripTradeRemove(t *testing.T) {
	for _, idx := range []uint8{0, 7, 255} {
		b := flatbuffers.NewBuilder(32)
		TradeRemoveStart(b)
		TradeRemoveAddIndex(b, idx)
		buf := finish(b, MessageTradeRemove, TradeRemoveEnd(b))

		var m TradeRemove
		tbl := open(t, buf, MessageTradeRemove)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Index() != idx {
			t.Fatalf("index = %d, want %d", m.Index(), idx)
		}
	}
}

func TestRoundTripTradeSetGold(t *testing.T) {
	for _, g := range []uint32{0, 100, 4294967295} {
		b := flatbuffers.NewBuilder(32)
		TradeSetGoldStart(b)
		TradeSetGoldAddGold(b, g)
		buf := finish(b, MessageTradeSetGold, TradeSetGoldEnd(b))

		var m TradeSetGold
		tbl := open(t, buf, MessageTradeSetGold)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Gold() != g {
			t.Fatalf("gold = %d, want %d", m.Gold(), g)
		}
	}
}

func TestRoundTripTradeAccept(t *testing.T) {
	for _, s := range []uint8{1, 2} {
		b := flatbuffers.NewBuilder(32)
		TradeAcceptStart(b)
		TradeAcceptAddScreen(b, s)
		buf := finish(b, MessageTradeAccept, TradeAcceptEnd(b))

		var m TradeAccept
		tbl := open(t, buf, MessageTradeAccept)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Screen() != s {
			t.Fatalf("screen = %d, want %d", m.Screen(), s)
		}
	}
}

func TestRoundTripTradeCancel(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	TradeCancelStart(b)
	buf := finish(b, MessageTradeCancel, TradeCancelEnd(b))

	var m TradeCancel
	tbl := open(t, buf, MessageTradeCancel)
	m.Init(tbl.Bytes, tbl.Pos)
}

func TestRoundTripTradeRequestNotice(t *testing.T) {
	for _, id := range []uint16{0, 300, 65535} {
		b := flatbuffers.NewBuilder(32)
		TradeRequestNoticeStart(b)
		TradeRequestNoticeAddFromId(b, id)
		buf := finish(b, MessageTradeRequestNotice, TradeRequestNoticeEnd(b))

		var m TradeRequestNotice
		tbl := open(t, buf, MessageTradeRequestNotice)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.FromId() != id {
			t.Fatalf("from id = %d, want %d", m.FromId(), id)
		}
	}
}

type tradeItem struct {
	itemID uint16
	qty    uint32
}

type tradeState struct {
	partner                   uint16
	screen                    uint8
	mine                      []tradeItem
	myGold                    uint32
	theirs                    []tradeItem
	partnerGold               uint32
	myAccepted, theirAccepted bool
}

func buildTradeItems(b *flatbuffers.Builder, items []tradeItem) flatbuffers.UOffsetT {
	b.StartVector(8, len(items), 4)
	for i := len(items) - 1; i >= 0; i-- {
		CreateTradeItem(b, items[i].itemID, items[i].qty)
	}
	return b.EndVector(len(items))
}

func buildTradeState(s tradeState) []byte {
	b := flatbuffers.NewBuilder(256)
	mine := buildTradeItems(b, s.mine)
	theirs := buildTradeItems(b, s.theirs)
	TradeStateStart(b)
	TradeStateAddPartnerId(b, s.partner)
	TradeStateAddScreen(b, s.screen)
	TradeStateAddMyItems(b, mine)
	TradeStateAddMyGold(b, s.myGold)
	TradeStateAddPartnerItems(b, theirs)
	TradeStateAddPartnerGold(b, s.partnerGold)
	TradeStateAddMyAccepted(b, s.myAccepted)
	TradeStateAddPartnerAccepted(b, s.theirAccepted)
	return finish(b, MessageTradeState, TradeStateEnd(b))
}

func TestRoundTripTradeState(t *testing.T) {
	cases := []tradeState{
		{300, 1, []tradeItem{{1, 1}, {12, 250}, {65535, 4294967295}}, 100, []tradeItem{{7, 3}}, 0, true, false},
		{65535, 2, nil, 4294967295, []tradeItem{{2, 1}, {3, 2}}, 55, false, true},
		{1, 2, []tradeItem{{9, 9}}, 0, nil, 4294967295, true, true},
	}
	for ci, w := range cases {
		buf := buildTradeState(w)
		var m TradeState
		tbl := open(t, buf, MessageTradeState)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.PartnerId() != w.partner || m.Screen() != w.screen || m.MyGold() != w.myGold ||
			m.PartnerGold() != w.partnerGold || m.MyAccepted() != w.myAccepted ||
			m.PartnerAccepted() != w.theirAccepted {
			t.Fatalf("case %d: got partner=%d screen=%d gold=%d/%d accepted=%v/%v, want %+v", ci,
				m.PartnerId(), m.Screen(), m.MyGold(), m.PartnerGold(), m.MyAccepted(), m.PartnerAccepted(), w)
		}
		check := func(side string, n int, get func(*TradeItem, int) bool, want []tradeItem) {
			if n != len(want) {
				t.Fatalf("case %d: %s items = %d, want %d", ci, side, n, len(want))
			}
			var it TradeItem
			for i, wi := range want {
				if !get(&it, i) {
					t.Fatalf("case %d: %s item %d missing", ci, side, i)
				}
				if got := (tradeItem{it.ItemId(), it.Quantity()}); got != wi {
					t.Fatalf("case %d: %s item %d = %+v, want %+v", ci, side, i, got, wi)
				}
			}
		}
		check("my", m.MyItemsLength(), m.MyItems, w.mine)
		check("partner", m.PartnerItemsLength(), m.PartnerItems, w.theirs)
	}
}

// A full trade window (28 items each side) stays under the datagram budget,
// and each offered item is a fixed 8-byte struct.
func TestTradeStateSize(t *testing.T) {
	items := make([]tradeItem, 28)
	for i := range items {
		items[i] = tradeItem{uint16(i + 1), uint32(i * 1000)}
	}
	buf := buildTradeState(tradeState{300, 2, items, 4294967295, items, 4294967295, true, true})
	if len(buf) >= 1200 {
		t.Fatalf("TradeState with 2x28 items is %d bytes, want < 1200", len(buf))
	}
	t.Logf("TradeState with 2x28 items: %d bytes", len(buf))
	small := buildTradeState(tradeState{mine: items[:10]})
	big := buildTradeState(tradeState{mine: items[:12]})
	if d := len(big) - len(small); d != 16 {
		t.Fatalf("two more items add %d bytes, want 16", d)
	}
}

func TestRoundTripTradeResult(t *testing.T) {
	codes := []TradeResultCode{
		TradeResultCodeCompleted, TradeResultCodeCancelled, TradeResultCodeTooFar,
		TradeResultCodeInPvpZone, TradeResultCodePartnerBusy, TradeResultCodeOfferNotHeld,
		TradeResultCodeInventoryFull,
	}
	for _, code := range codes {
		b := flatbuffers.NewBuilder(32)
		TradeResultStart(b)
		TradeResultAddCode(b, code)
		buf := finish(b, MessageTradeResult, TradeResultEnd(b))

		var m TradeResult
		tbl := open(t, buf, MessageTradeResult)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Code() != code {
			t.Fatalf("code = %v, want %v", m.Code(), code)
		}
	}
}

func TestRoundTripCardTableSit(t *testing.T) {
	for _, id := range []uint16{0, 3, 65535} {
		b := flatbuffers.NewBuilder(32)
		CardTableSitStart(b)
		CardTableSitAddTableId(b, id)
		buf := finish(b, MessageCardTableSit, CardTableSitEnd(b))

		var m CardTableSit
		tbl := open(t, buf, MessageCardTableSit)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.TableId() != id {
			t.Fatalf("table id = %d, want %d", m.TableId(), id)
		}
	}
}

func TestRoundTripCardTableLeave(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	CardTableLeaveStart(b)
	buf := finish(b, MessageCardTableLeave, CardTableLeaveEnd(b))

	var m CardTableLeave
	tbl := open(t, buf, MessageCardTableLeave)
	m.Init(tbl.Bytes, tbl.Pos)
}

type cardTableState struct {
	tableID    uint16
	seats      []uint16
	inProgress bool
	turnSeat   uint8
	reversed   bool
	ticksLeft  uint32
	hasWinner  bool
	winnerSeat uint8
	ready      []bool
}

func TestRoundTripCardTableState(t *testing.T) {
	cases := []cardTableState{
		// 4 seats, one empty, game over with a winner.
		{7, []uint16{300, 0, 12, 65535}, false, 3, true, 0, true, 2, []bool{true, false, false, true}},
		{65535, []uint16{1, 2, 3, 4}, true, 1, false, 4294967295, false, 0, []bool{true, true, true, true}},
		{0, []uint16{0, 0}, false, 0, false, 0, false, 0, []bool{false, false}},
	}
	for ci, w := range cases {
		b := flatbuffers.NewBuilder(64)
		CardTableStateStartSeatsVector(b, len(w.seats))
		for i := len(w.seats) - 1; i >= 0; i-- {
			b.PrependUint16(w.seats[i])
		}
		seats := b.EndVector(len(w.seats))
		CardTableStateStartReadyVector(b, len(w.ready))
		for i := len(w.ready) - 1; i >= 0; i-- {
			b.PrependBool(w.ready[i])
		}
		ready := b.EndVector(len(w.ready))
		CardTableStateStart(b)
		CardTableStateAddTableId(b, w.tableID)
		CardTableStateAddSeats(b, seats)
		CardTableStateAddInProgress(b, w.inProgress)
		CardTableStateAddTurnSeat(b, w.turnSeat)
		CardTableStateAddReversed(b, w.reversed)
		CardTableStateAddTurnTicksLeft(b, w.ticksLeft)
		CardTableStateAddHasWinner(b, w.hasWinner)
		CardTableStateAddWinnerSeat(b, w.winnerSeat)
		CardTableStateAddReady(b, ready)
		buf := finish(b, MessageCardTableState, CardTableStateEnd(b))

		var m CardTableState
		tbl := open(t, buf, MessageCardTableState)
		m.Init(tbl.Bytes, tbl.Pos)
		got := cardTableState{m.TableId(), nil, m.InProgress(), m.TurnSeat(), m.Reversed(),
			m.TurnTicksLeft(), m.HasWinner(), m.WinnerSeat(), nil}
		for i := 0; i < m.SeatsLength(); i++ {
			got.seats = append(got.seats, m.Seats(i))
		}
		for i := 0; i < m.ReadyLength(); i++ {
			got.ready = append(got.ready, m.Ready(i))
		}
		if !reflect.DeepEqual(got, w) {
			t.Fatalf("case %d: got %+v, want %+v", ci, got, w)
		}
	}
}

func TestRoundTripCardTableResult(t *testing.T) {
	codes := []CardTableResultCode{
		CardTableResultCodeOk, CardTableResultCodeUnknownTable, CardTableResultCodeTableFull,
		CardTableResultCodeAlreadySeated, CardTableResultCodeNotSeated, CardTableResultCodeInProgress,
		CardTableResultCodeNotYourTurn, CardTableResultCodeIllegalMove, CardTableResultCodeNoGame,
		CardTableResultCodeNotEnoughGold, CardTableResultCodeTooFar,
	}
	for i, code := range codes {
		if int(code) != i {
			t.Fatalf("%v = %d, want %d (new codes go at the end)", code, code, i)
		}
	}
	if len(codes) != len(EnumNamesCardTableResultCode) {
		t.Fatalf("CardTableResultCode has %d values, want %d", len(EnumNamesCardTableResultCode), len(codes))
	}
	for _, code := range codes {
		b := flatbuffers.NewBuilder(32)
		CardTableResultStart(b)
		CardTableResultAddCode(b, code)
		buf := finish(b, MessageCardTableResult, CardTableResultEnd(b))

		var m CardTableResult
		tbl := open(t, buf, MessageCardTableResult)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Code() != code {
			t.Fatalf("code = %v, want %v", m.Code(), code)
		}
	}
}

// A CardTableState sent before this field existed has no ready list; new
// code reads it as empty.
func TestCardTableStateWithoutReady(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	CardTableStateStart(b)
	CardTableStateAddTableId(b, 2)
	buf := finish(b, MessageCardTableState, CardTableStateEnd(b))

	var m CardTableState
	tbl := open(t, buf, MessageCardTableState)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.TableId() != 2 || m.ReadyLength() != 0 {
		t.Fatalf("table id %d, ready length %d", m.TableId(), m.ReadyLength())
	}
}

// New union members go at the end of Message, so older ids keep their values.
func TestCardTableMessageIDs(t *testing.T) {
	want := []Message{
		MessageCardTableResult, MessageCardTableReady, MessageCardTableCatalog,
		MessageShedPlay, MessageShedDraw, MessageShedState, MessageShedHand,
	}
	for i, m := range want {
		if int(m) != 38+i {
			t.Fatalf("%v = %d, want %d", m, m, 38+i)
		}
	}
	if len(EnumNamesMessage) < 45 {
		t.Fatalf("Message has %d values, want at least 45", len(EnumNamesMessage))
	}
}

func TestRoundTripCardTableReady(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	CardTableReadyStart(b)
	buf := finish(b, MessageCardTableReady, CardTableReadyEnd(b))

	var m CardTableReady
	tbl := open(t, buf, MessageCardTableReady)
	m.Init(tbl.Bytes, tbl.Pos)
}

type cardTableInfo struct {
	id    uint16
	seats uint8
	stake uint32
}

func buildCardTableCatalog(tables []cardTableInfo) []byte {
	b := flatbuffers.NewBuilder(64)
	CardTableCatalogStartTablesVector(b, len(tables))
	for i := len(tables) - 1; i >= 0; i-- {
		CreateCardTableInfo(b, tables[i].id, tables[i].seats, tables[i].stake)
	}
	vec := b.EndVector(len(tables))
	CardTableCatalogStart(b)
	CardTableCatalogAddTables(b, vec)
	return finish(b, MessageCardTableCatalog, CardTableCatalogEnd(b))
}

func TestRoundTripCardTableCatalog(t *testing.T) {
	want := []cardTableInfo{{1, 2, 0}, {2, 4, 100}, {65535, 255, 4294967295}}
	buf := buildCardTableCatalog(want)

	var m CardTableCatalog
	tbl := open(t, buf, MessageCardTableCatalog)
	m.Init(tbl.Bytes, tbl.Pos)
	var got []cardTableInfo
	var info CardTableInfo
	for i := 0; i < m.TablesLength(); i++ {
		if !m.Tables(&info, i) {
			t.Fatalf("table %d missing", i)
		}
		got = append(got, cardTableInfo{info.Id(), info.Seats(), info.Stake()})
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Each extra catalog table adds exactly 8 bytes, so CardTableInfo stays 8 bytes.
func TestCardTableInfoIsEightBytes(t *testing.T) {
	one := buildCardTableCatalog([]cardTableInfo{{1, 2, 3}})
	two := buildCardTableCatalog([]cardTableInfo{{1, 2, 3}, {4, 5, 6}})
	if d := len(two) - len(one); d != 8 {
		t.Fatalf("one more table adds %d bytes, want 8", d)
	}
}

func TestShedEnums(t *testing.T) {
	if len(EnumNamesShedColour) != 5 || ShedColourNone != 0 || ShedColourFour != 4 {
		t.Fatalf("ShedColour: %d values, None %d, Four %d", len(EnumNamesShedColour), ShedColourNone, ShedColourFour)
	}
	if len(EnumNamesShedSymbol) != 15 || ShedSymbolNine != 9 || ShedSymbolSkip != 10 ||
		ShedSymbolReverse != 11 || ShedSymbolDrawTwo != 12 || ShedSymbolWild != 13 || ShedSymbolWildDrawFour != 14 {
		t.Fatalf("ShedSymbol values changed: %v", EnumNamesShedSymbol)
	}
}

type shedCard struct {
	colour ShedColour
	symbol ShedSymbol
}

func TestRoundTripShedPlay(t *testing.T) {
	for _, w := range []struct {
		card   shedCard
		chosen ShedColour
	}{
		{shedCard{ShedColourOne, ShedSymbolSeven}, ShedColourNone},
		{shedCard{ShedColourFour, ShedSymbolDrawTwo}, ShedColourNone},
		{shedCard{ShedColourNone, ShedSymbolWild}, ShedColourThree},
		{shedCard{ShedColourNone, ShedSymbolWildDrawFour}, ShedColourFour},
	} {
		b := flatbuffers.NewBuilder(32)
		ShedPlayStart(b)
		ShedPlayAddCard(b, CreateShedCard(b, w.card.colour, w.card.symbol))
		ShedPlayAddChosenColour(b, w.chosen)
		buf := finish(b, MessageShedPlay, ShedPlayEnd(b))

		var m ShedPlay
		tbl := open(t, buf, MessageShedPlay)
		m.Init(tbl.Bytes, tbl.Pos)
		c := m.Card(nil)
		if c == nil {
			t.Fatal("card missing")
		}
		if got := (shedCard{c.Colour(), c.Symbol()}); got != w.card || m.ChosenColour() != w.chosen {
			t.Fatalf("got %+v %v, want %+v %v", got, m.ChosenColour(), w.card, w.chosen)
		}
	}
}

func TestRoundTripShedDraw(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	ShedDrawStart(b)
	buf := finish(b, MessageShedDraw, ShedDrawEnd(b))

	var m ShedDraw
	tbl := open(t, buf, MessageShedDraw)
	m.Init(tbl.Bytes, tbl.Pos)
}

type shedState struct {
	tableID    uint16
	top        shedCard
	colour     ShedColour
	handCounts []uint8
	drawPile   uint8
}

func TestRoundTripShedState(t *testing.T) {
	cases := []shedState{
		{3, shedCard{ShedColourTwo, ShedSymbolZero}, ShedColourTwo, []uint8{7, 7}, 93},
		{65535, shedCard{ShedColourNone, ShedSymbolWildDrawFour}, ShedColourFour, []uint8{1, 0, 12, 108}, 0},
		{0, shedCard{ShedColourThree, ShedSymbolReverse}, ShedColourThree, []uint8{255, 255, 255}, 255},
	}
	for ci, w := range cases {
		b := flatbuffers.NewBuilder(64)
		counts := b.CreateByteVector(w.handCounts)
		ShedStateStart(b)
		ShedStateAddTableId(b, w.tableID)
		ShedStateAddTopCard(b, CreateShedCard(b, w.top.colour, w.top.symbol))
		ShedStateAddColour(b, w.colour)
		ShedStateAddHandCounts(b, counts)
		ShedStateAddDrawPile(b, w.drawPile)
		buf := finish(b, MessageShedState, ShedStateEnd(b))

		var m ShedState
		tbl := open(t, buf, MessageShedState)
		m.Init(tbl.Bytes, tbl.Pos)
		c := m.TopCard(nil)
		if c == nil {
			t.Fatalf("case %d: top card missing", ci)
		}
		got := shedState{m.TableId(), shedCard{c.Colour(), c.Symbol()}, m.Colour(), nil, m.DrawPile()}
		for i := 0; i < m.HandCountsLength(); i++ {
			got.handCounts = append(got.handCounts, m.HandCounts(i))
		}
		if !reflect.DeepEqual(got, w) {
			t.Fatalf("case %d: got %+v, want %+v", ci, got, w)
		}
	}
}

func buildShedHand(tableID uint16, cards []shedCard) []byte {
	b := flatbuffers.NewBuilder(64)
	ShedHandStartCardsVector(b, len(cards))
	for i := len(cards) - 1; i >= 0; i-- {
		CreateShedCard(b, cards[i].colour, cards[i].symbol)
	}
	vec := b.EndVector(len(cards))
	ShedHandStart(b)
	ShedHandAddTableId(b, tableID)
	ShedHandAddCards(b, vec)
	return finish(b, MessageShedHand, ShedHandEnd(b))
}

func TestRoundTripShedHand(t *testing.T) {
	for ci, w := range [][]shedCard{
		{
			{ShedColourOne, ShedSymbolZero}, {ShedColourTwo, ShedSymbolNine}, {ShedColourThree, ShedSymbolSkip},
			{ShedColourFour, ShedSymbolReverse}, {ShedColourOne, ShedSymbolDrawTwo},
			{ShedColourNone, ShedSymbolWild}, {ShedColourNone, ShedSymbolWildDrawFour},
		},
		{{ShedColourFour, ShedSymbolFive}},
		nil,
	} {
		buf := buildShedHand(uint16(ci+1), w)

		var m ShedHand
		tbl := open(t, buf, MessageShedHand)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.TableId() != uint16(ci+1) {
			t.Fatalf("case %d: table id %d", ci, m.TableId())
		}
		var got []shedCard
		var c ShedCard
		for i := 0; i < m.CardsLength(); i++ {
			if !m.Cards(&c, i) {
				t.Fatalf("case %d: card %d missing", ci, i)
			}
			got = append(got, shedCard{c.Colour(), c.Symbol()})
		}
		if !reflect.DeepEqual(got, w) {
			t.Fatalf("case %d: got %+v, want %+v", ci, got, w)
		}
	}
}

// Each extra card in a hand adds exactly 2 bytes, so ShedCard stays 2 bytes.
func TestShedCardIsTwoBytes(t *testing.T) {
	hand := func(n int) []byte {
		cards := make([]shedCard, n)
		for i := range cards {
			cards[i] = shedCard{ShedColourOne, ShedSymbolOne}
		}
		return buildShedHand(1, cards)
	}
	// Compare two even counts so vector padding doesn't hide the size.
	if d := len(hand(20)) - len(hand(10)); d != 20 {
		t.Fatalf("ten more cards add %d bytes, want 20", d)
	}
}

// New union members go at the end of Message, so older ids keep their values.
func TestSkillMessageIDs(t *testing.T) {
	want := []Message{
		MessageSkillStats, MessageSkillAction, MessageResourceState, MessageLevelUp, MessageSkillResult,
	}
	for i, m := range want {
		if int(m) != 45+i {
			t.Fatalf("%v = %d, want %d", m, m, 45+i)
		}
	}
}

func TestSkillEnums(t *testing.T) {
	if AnimationIdle != 0 || AnimationWalk != 1 || AnimationWave != 2 ||
		AnimationChop != 3 || AnimationFish != 4 || AnimationCook != 5 || len(EnumNamesAnimation) != 6 {
		t.Fatalf("Animation values changed: %v", EnumNamesAnimation)
	}
	if SkillWoodcutting != 0 || SkillFishing != 1 || SkillCooking != 2 || len(EnumNamesSkill) != 3 {
		t.Fatalf("Skill values changed: %v", EnumNamesSkill)
	}
	if SkillTargetTree != 0 || SkillTargetFishingSpot != 1 || SkillTargetGrill != 2 || len(EnumNamesSkillTarget) != 3 {
		t.Fatalf("SkillTarget values changed: %v", EnumNamesSkillTarget)
	}
}

func TestRoundTripSkillAnimations(t *testing.T) {
	for _, a := range []Animation{AnimationChop, AnimationFish, AnimationCook} {
		b := flatbuffers.NewBuilder(64)
		SnapshotStartEntitiesVector(b, 1)
		CreateEntity(b, 7, 10, 20, 30, uint8(a))
		ents := b.EndVector(1)
		SnapshotStart(b)
		SnapshotAddEntities(b, ents)
		buf := finish(b, MessageSnapshot, SnapshotEnd(b))

		var m Snapshot
		tbl := open(t, buf, MessageSnapshot)
		m.Init(tbl.Bytes, tbl.Pos)
		var e Entity
		if !m.Entities(&e, 0) || Animation(e.Animation()) != a {
			t.Fatalf("animation = %d, want %v", e.Animation(), a)
		}
	}
}

type skillStats struct {
	wcLevel uint8
	wcXP    uint32
	fLevel  uint8
	fXP     uint32
	cLevel  uint8
	cXP     uint32
}

func TestRoundTripSkillStats(t *testing.T) {
	for ci, w := range []skillStats{
		{1, 0, 1, 0, 1, 0},
		{15, 23375, 5, 3880, 20, 45125},
		{30, 4294967295, 30, 1, 30, 1342340},
	} {
		b := flatbuffers.NewBuilder(64)
		SkillStatsStart(b)
		SkillStatsAddWoodcuttingLevel(b, w.wcLevel)
		SkillStatsAddWoodcuttingXp(b, w.wcXP)
		SkillStatsAddFishingLevel(b, w.fLevel)
		SkillStatsAddFishingXp(b, w.fXP)
		SkillStatsAddCookingLevel(b, w.cLevel)
		SkillStatsAddCookingXp(b, w.cXP)
		buf := finish(b, MessageSkillStats, SkillStatsEnd(b))

		var m SkillStats
		tbl := open(t, buf, MessageSkillStats)
		m.Init(tbl.Bytes, tbl.Pos)
		got := skillStats{m.WoodcuttingLevel(), m.WoodcuttingXp(), m.FishingLevel(), m.FishingXp(),
			m.CookingLevel(), m.CookingXp()}
		if got != w {
			t.Fatalf("case %d: got %+v, want %+v", ci, got, w)
		}
	}
}

func TestRoundTripSkillAction(t *testing.T) {
	for _, w := range []struct {
		target SkillTarget
		id     uint16
	}{
		{SkillTargetTree, 0}, {SkillTargetTree, 65535}, {SkillTargetFishingSpot, 3}, {SkillTargetGrill, 0},
	} {
		b := flatbuffers.NewBuilder(32)
		SkillActionStart(b)
		SkillActionAddTarget(b, w.target)
		SkillActionAddTargetId(b, w.id)
		buf := finish(b, MessageSkillAction, SkillActionEnd(b))

		var m SkillAction
		tbl := open(t, buf, MessageSkillAction)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Target() != w.target || m.TargetId() != w.id {
			t.Fatalf("got %v %d, want %v %d", m.Target(), m.TargetId(), w.target, w.id)
		}
	}
}

type fishingSpot struct {
	id        uint16
	candidate uint8
}

func buildResourceState(felled []uint16, spots []fishingSpot) []byte {
	b := flatbuffers.NewBuilder(64)
	ResourceStateStartFelledTreesVector(b, len(felled))
	for i := len(felled) - 1; i >= 0; i-- {
		b.PrependUint16(felled[i])
	}
	trees := b.EndVector(len(felled))
	ResourceStateStartFishingSpotsVector(b, len(spots))
	for i := len(spots) - 1; i >= 0; i-- {
		CreateFishingSpotState(b, spots[i].id, spots[i].candidate)
	}
	fish := b.EndVector(len(spots))
	ResourceStateStart(b)
	ResourceStateAddFelledTrees(b, trees)
	ResourceStateAddFishingSpots(b, fish)
	return finish(b, MessageResourceState, ResourceStateEnd(b))
}

func TestRoundTripResourceState(t *testing.T) {
	for ci, w := range []struct {
		felled []uint16
		spots  []fishingSpot
	}{
		{[]uint16{1, 7, 65535}, []fishingSpot{{1, 0}, {2, 3}, {65535, 255}}},
		{nil, []fishingSpot{{4, 1}}},
		{[]uint16{0}, nil},
	} {
		buf := buildResourceState(w.felled, w.spots)

		var m ResourceState
		tbl := open(t, buf, MessageResourceState)
		m.Init(tbl.Bytes, tbl.Pos)
		var felled []uint16
		for i := 0; i < m.FelledTreesLength(); i++ {
			felled = append(felled, m.FelledTrees(i))
		}
		var spots []fishingSpot
		var s FishingSpotState
		for i := 0; i < m.FishingSpotsLength(); i++ {
			if !m.FishingSpots(&s, i) {
				t.Fatalf("case %d: spot %d missing", ci, i)
			}
			spots = append(spots, fishingSpot{s.Id(), s.Candidate()})
		}
		if !reflect.DeepEqual(felled, w.felled) || !reflect.DeepEqual(spots, w.spots) {
			t.Fatalf("case %d: got %v %+v, want %v %+v", ci, felled, spots, w.felled, w.spots)
		}
	}
}

// Each extra fishing spot adds exactly 4 bytes, so FishingSpotState stays 4 bytes.
func TestFishingSpotStateIsFourBytes(t *testing.T) {
	one := buildResourceState(nil, []fishingSpot{{1, 2}})
	two := buildResourceState(nil, []fishingSpot{{1, 2}, {3, 4}})
	if d := len(two) - len(one); d != 4 {
		t.Fatalf("one more spot adds %d bytes, want 4", d)
	}
}

func TestRoundTripLevelUp(t *testing.T) {
	for _, w := range []struct {
		id    uint16
		skill Skill
		level uint8
	}{
		{1, SkillWoodcutting, 2}, {500, SkillFishing, 15}, {65535, SkillCooking, 30},
	} {
		b := flatbuffers.NewBuilder(32)
		LevelUpStart(b)
		LevelUpAddEntityId(b, w.id)
		LevelUpAddSkill(b, w.skill)
		LevelUpAddLevel(b, w.level)
		buf := finish(b, MessageLevelUp, LevelUpEnd(b))

		var m LevelUp
		tbl := open(t, buf, MessageLevelUp)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.EntityId() != w.id || m.Skill() != w.skill || m.Level() != w.level {
			t.Fatalf("got %d %v %d, want %+v", m.EntityId(), m.Skill(), m.Level(), w)
		}
	}
}

func TestRoundTripSkillResult(t *testing.T) {
	codes := []SkillResultCode{
		SkillResultCodeStarted, SkillResultCodeSuccess, SkillResultCodeBurnt, SkillResultCodeNeedTool,
		SkillResultCodeNeedLevel, SkillResultCodeInventoryFull, SkillResultCodeResourceGone,
		SkillResultCodeNothingToCook, SkillResultCodeOutOfRange, SkillResultCodeStopped,
	}
	for i, code := range codes {
		if int(code) != i {
			t.Fatalf("%v = %d, want %d (new codes go at the end)", code, code, i)
		}
	}
	if len(codes) != len(EnumNamesSkillResultCode) {
		t.Fatalf("SkillResultCode has %d values, want %d", len(EnumNamesSkillResultCode), len(codes))
	}
	for i, code := range codes {
		skill := Skill(i % 3)
		item := uint16(100 + i)
		level := uint8(i * 3)
		b := flatbuffers.NewBuilder(32)
		SkillResultStart(b)
		SkillResultAddSkill(b, skill)
		SkillResultAddCode(b, code)
		SkillResultAddItemId(b, item)
		SkillResultAddLevel(b, level)
		buf := finish(b, MessageSkillResult, SkillResultEnd(b))

		var m SkillResult
		tbl := open(t, buf, MessageSkillResult)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Skill() != skill || m.Code() != code || m.ItemId() != item || m.Level() != level {
			t.Fatalf("got %v %v %d %d, want %v %v %d %d",
				m.Skill(), m.Code(), m.ItemId(), m.Level(), skill, code, item, level)
		}
	}
}

func TestBankMessageIDs(t *testing.T) {
	want := []Message{
		MessageBankOpen, MessageBankClose, MessageBankState, MessageBankDeposit, MessageBankWithdraw,
		MessageBankDepositInventory, MessageBankDepositEquipment, MessageBankResult,
	}
	for i, m := range want {
		if int(m) != 50+i {
			t.Fatalf("%v = %d, want %d", m, m, 50+i)
		}
	}
	if len(EnumNamesMessage) != 59 {
		t.Fatalf("Message has %d values, want 59 (NONE plus 58 messages)", len(EnumNamesMessage))
	}
}

// Messages with no fields: BankOpen, BankClose, BankDepositInventory and
// BankDepositEquipment.
func TestRoundTripEmptyBankMessages(t *testing.T) {
	for _, w := range []struct {
		typ   Message
		build func(*flatbuffers.Builder) flatbuffers.UOffsetT
	}{
		{MessageBankOpen, func(b *flatbuffers.Builder) flatbuffers.UOffsetT { BankOpenStart(b); return BankOpenEnd(b) }},
		{MessageBankClose, func(b *flatbuffers.Builder) flatbuffers.UOffsetT { BankCloseStart(b); return BankCloseEnd(b) }},
		{MessageBankDepositInventory, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
			BankDepositInventoryStart(b)
			return BankDepositInventoryEnd(b)
		}},
		{MessageBankDepositEquipment, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
			BankDepositEquipmentStart(b)
			return BankDepositEquipmentEnd(b)
		}},
	} {
		b := flatbuffers.NewBuilder(16)
		buf := finish(b, w.typ, w.build(b))
		open(t, buf, w.typ)
	}
}

type bankSlot struct {
	item uint16
	qty  uint64
}

func buildBankState(slots []bankSlot) []byte {
	b := flatbuffers.NewBuilder(4096)
	BankStateStartSlotsVector(b, len(slots))
	for i := len(slots) - 1; i >= 0; i-- {
		CreateBankSlot(b, slots[i].item, slots[i].qty)
	}
	vec := b.EndVector(len(slots))
	BankStateStart(b)
	BankStateAddSlots(b, vec)
	return finish(b, MessageBankState, BankStateEnd(b))
}

func TestRoundTripBankState(t *testing.T) {
	full := make([]bankSlot, 200)
	for i := range full {
		full[i] = bankSlot{uint16(i + 1), uint64(i) * 123456789}
	}
	full[0].qty = 1
	full[198] = bankSlot{65535, 1<<53 + 1}  // beyond a JS number
	full[199] = bankSlot{65534, ^uint64(0)} // the largest quantity
	for ci, slots := range [][]bankSlot{full, {{7, 4294967296}}, nil} {
		buf := buildBankState(slots)

		var m BankState
		tbl := open(t, buf, MessageBankState)
		m.Init(tbl.Bytes, tbl.Pos)
		var got []bankSlot
		var s BankSlot
		for i := 0; i < m.SlotsLength(); i++ {
			if !m.Slots(&s, i) {
				t.Fatalf("case %d: slot %d missing", ci, i)
			}
			got = append(got, bankSlot{s.ItemId(), s.Quantity()})
		}
		if !reflect.DeepEqual(got, slots) {
			t.Fatalf("case %d: got %+v, want %+v", ci, got, slots)
		}
	}
}

// Each extra bank slot adds exactly 16 bytes.
func TestBankSlotIsSixteenBytes(t *testing.T) {
	one := buildBankState([]bankSlot{{1, 2}})
	two := buildBankState([]bankSlot{{1, 2}, {3, 4}})
	if d := len(two) - len(one); d != 16 {
		t.Fatalf("one more slot adds %d bytes, want 16", d)
	}
}

func TestRoundTripBankDepositWithdraw(t *testing.T) {
	if BankAmountAll != 4294967295 || len(EnumNamesBankAmount) != 1 {
		t.Fatalf("BankAmount values changed: %v", EnumNamesBankAmount)
	}
	for _, w := range []struct {
		item   uint16
		amount uint32
	}{
		{1, 1}, {42, 5}, {65535, 1234}, {300, uint32(BankAmountAll)},
	} {
		b := flatbuffers.NewBuilder(32)
		BankDepositStart(b)
		BankDepositAddItemId(b, w.item)
		BankDepositAddAmount(b, w.amount)
		buf := finish(b, MessageBankDeposit, BankDepositEnd(b))
		var d BankDeposit
		tbl := open(t, buf, MessageBankDeposit)
		d.Init(tbl.Bytes, tbl.Pos)
		if d.ItemId() != w.item || d.Amount() != w.amount {
			t.Fatalf("deposit: got %d %d, want %+v", d.ItemId(), d.Amount(), w)
		}

		b = flatbuffers.NewBuilder(32)
		BankWithdrawStart(b)
		BankWithdrawAddItemId(b, w.item)
		BankWithdrawAddAmount(b, w.amount)
		buf = finish(b, MessageBankWithdraw, BankWithdrawEnd(b))
		var wd BankWithdraw
		tbl = open(t, buf, MessageBankWithdraw)
		wd.Init(tbl.Bytes, tbl.Pos)
		if wd.ItemId() != w.item || wd.Amount() != w.amount {
			t.Fatalf("withdraw: got %d %d, want %+v", wd.ItemId(), wd.Amount(), w)
		}
	}
}

func TestRoundTripBankResult(t *testing.T) {
	codes := []BankResultCode{
		BankResultCodeOk, BankResultCodeTooFar, BankResultCodeInCombat, BankResultCodeInPvpZone,
		BankResultCodeBankFull, BankResultCodeInventoryFull, BankResultCodeNotEnoughItems,
		BankResultCodeBusy, BankResultCodeNotOpen, BankResultCodeSaveFailed,
	}
	for i, code := range codes {
		if int(code) != i {
			t.Fatalf("%v = %d, want %d (new codes go at the end)", code, code, i)
		}
	}
	if len(codes) != len(EnumNamesBankResultCode) {
		t.Fatalf("BankResultCode has %d values, want %d", len(EnumNamesBankResultCode), len(codes))
	}
	for _, code := range codes {
		b := flatbuffers.NewBuilder(16)
		BankResultStart(b)
		BankResultAddCode(b, code)
		buf := finish(b, MessageBankResult, BankResultEnd(b))
		var m BankResult
		tbl := open(t, buf, MessageBankResult)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Code() != code {
			t.Fatalf("got %v, want %v", m.Code(), code)
		}
	}
}

func TestToolSkillValues(t *testing.T) {
	if ToolSkillNone != 0 || ToolSkillWoodcutting != 1 || ToolSkillFishing != 2 || len(EnumNamesToolSkill) != 3 {
		t.Fatalf("ToolSkill values changed: %v", EnumNamesToolSkill)
	}
	if BankResultCodeSaveFailed != 9 {
		t.Fatalf("BankResultCodeSaveFailed = %d, want 9", BankResultCodeSaveFailed)
	}
}

// An ItemDef built without the tool and cooking fields reads as not a tool
// and not a raw fish.
func TestItemDefDefaultsToNotATool(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	ItemDefStart(b)
	ItemDefAddId(b, 3)
	b.Finish(ItemDefEnd(b))
	d := GetRootAsItemDef(b.FinishedBytes(), 0)
	if d.ToolSkill() != ToolSkillNone || d.ToolLevel() != 0 || d.CookingLevel() != 0 {
		t.Fatalf("got %v %d %d, want None 0 0", d.ToolSkill(), d.ToolLevel(), d.CookingLevel())
	}
}

type treeKindDef struct {
	kind  string
	level uint8
}

type fishingSpotKindDef struct {
	kind  string
	level uint8
	tool  uint16
}

func TestRoundTripSkillCatalog(t *testing.T) {
	if MessageSkillCatalog != 58 {
		t.Fatalf("MessageSkillCatalog = %d, want 58 (new messages go at the end)", MessageSkillCatalog)
	}
	trees := []treeKindDef{{"tree", 1}, {"oak", 15}, {"eucalyptus", 25}}
	spots := []fishingSpotKindDef{{"net", 1, 22}, {"rod", 5, 65535}}
	b := flatbuffers.NewBuilder(256)
	treeOffs := make([]flatbuffers.UOffsetT, len(trees))
	for i, w := range trees {
		kind := b.CreateString(w.kind)
		TreeKindDefStart(b)
		TreeKindDefAddKind(b, kind)
		TreeKindDefAddLevel(b, w.level)
		treeOffs[i] = TreeKindDefEnd(b)
	}
	spotOffs := make([]flatbuffers.UOffsetT, len(spots))
	for i, w := range spots {
		kind := b.CreateString(w.kind)
		FishingSpotKindDefStart(b)
		FishingSpotKindDefAddKind(b, kind)
		FishingSpotKindDefAddLevel(b, w.level)
		FishingSpotKindDefAddToolItemId(b, w.tool)
		spotOffs[i] = FishingSpotKindDefEnd(b)
	}
	SkillCatalogStartTreesVector(b, len(treeOffs))
	for i := len(treeOffs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(treeOffs[i])
	}
	treeVec := b.EndVector(len(treeOffs))
	SkillCatalogStartFishingSpotsVector(b, len(spotOffs))
	for i := len(spotOffs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(spotOffs[i])
	}
	spotVec := b.EndVector(len(spotOffs))
	SkillCatalogStart(b)
	SkillCatalogAddTrees(b, treeVec)
	SkillCatalogAddFishingSpots(b, spotVec)
	buf := finish(b, MessageSkillCatalog, SkillCatalogEnd(b))

	var m SkillCatalog
	tbl := open(t, buf, MessageSkillCatalog)
	m.Init(tbl.Bytes, tbl.Pos)
	var gotTrees []treeKindDef
	var td TreeKindDef
	for i := 0; i < m.TreesLength(); i++ {
		if !m.Trees(&td, i) {
			t.Fatalf("tree %d missing", i)
		}
		gotTrees = append(gotTrees, treeKindDef{string(td.Kind()), td.Level()})
	}
	var gotSpots []fishingSpotKindDef
	var fd FishingSpotKindDef
	for i := 0; i < m.FishingSpotsLength(); i++ {
		if !m.FishingSpots(&fd, i) {
			t.Fatalf("fishing spot %d missing", i)
		}
		gotSpots = append(gotSpots, fishingSpotKindDef{string(fd.Kind()), fd.Level(), fd.ToolItemId()})
	}
	if !reflect.DeepEqual(gotTrees, trees) || !reflect.DeepEqual(gotSpots, spots) {
		t.Fatalf("got %+v %+v, want %+v %+v", gotTrees, gotSpots, trees, spots)
	}
}
