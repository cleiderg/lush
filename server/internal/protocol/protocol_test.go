package protocol

import (
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
	if ProtocolVersionCurrent != 5 {
		t.Fatalf("ProtocolVersionCurrent = %d, want 5", ProtocolVersionCurrent)
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
	buf := finish(b, MessageWelcome, WelcomeEnd(b))

	var m Welcome
	tbl := open(t, buf, MessageWelcome)
	m.Init(tbl.Bytes, tbl.Pos)
	sp := m.Spawn(nil)
	if m.PlayerId() != 42 || string(m.GuestToken()) != "tok" || sp.X() != 1.5 || sp.Z() != -2.5 || m.TickRate() != 20 {
		t.Fatalf("welcome mismatch")
	}
}

func TestRoundTripInput(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	InputStart(b)
	InputAddSeq(b, 99)
	InputAddMove(b, CreateVec2(b, 0.5, -1))
	InputAddFacing(b, 3.25)
	InputAddActions(b, uint8(ActionFlagsAttack|ActionFlagsJump))
	buf := finish(b, MessageInput, InputEnd(b))

	var m Input
	tbl := open(t, buf, MessageInput)
	m.Init(tbl.Bytes, tbl.Pos)
	mv := m.Move(nil)
	if m.Seq() != 99 || mv.X() != 0.5 || mv.Z() != -1 || m.Facing() != 3.25 ||
		ActionFlags(m.Actions()) != ActionFlagsAttack|ActionFlagsJump {
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
}

func TestRoundTripItemCatalog(t *testing.T) {
	want := []itemDef{
		{1, "Wooden stick", ItemKindWeapon, EquipSlotWeapon, 1, 3, 2, 0, 4, 0, 2, true, 5},
		{8, "Robot plating", ItemKindArmor, EquipSlotBody, 12, -2, 0, 14, 0, 0, 150, false, 0},
		{9, "Boba", ItemKindFood, EquipSlotNone, 1, 0, 0, 0, 0, 4, 1, true, 5},
		{11, "Acorn", ItemKindLoot, EquipSlotNone, 0, 0, 0, 0, 0, 0, 1, false, 0},
		{7, "Bike helmet", ItemKindArmor, EquipSlotHead, 10, 0, 0, 6, 0, 0, 30, true, 80},
		{5, "Cardboard shield", ItemKindArmor, EquipSlotShield, 1, 0, 0, 3, 0, 0, 3, true, 10},
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
			d.ShopBuysFor(), d.ShopSells(), d.ShopSellsFor()}
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
}

func TestRoundTripEntityInfo(t *testing.T) {
	for _, w := range []entityInfo{
		{42, EntityKindPlayer, 0, 30, 1, 7, 8, 5},
		{300, EntityKindNpc, 6, 20, uint16(EmptyItemId), uint16(EmptyItemId), uint16(EmptyItemId), uint16(EmptyItemId)},
		{65535, EntityKindNpc, 65535, 255, 65535, 65535, 65535, 65535},
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
		buf := finish(b, MessageEntityInfo, EntityInfoEnd(b))

		var m EntityInfo
		tbl := open(t, buf, MessageEntityInfo)
		m.Init(tbl.Bytes, tbl.Pos)
		got := entityInfo{m.Id(), m.Kind(), m.NpcType(), m.CombatLevel(), m.Weapon(), m.Head(), m.Body(), m.Shield()}
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
