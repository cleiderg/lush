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
	if ProtocolVersionCurrent != 3 {
		t.Fatalf("ProtocolVersionCurrent = %d, want 3", ProtocolVersionCurrent)
	}
}

func TestSnapshotSize(t *testing.T) {
	buf := buildSnapshot(101)
	if len(buf) >= 1200 {
		t.Fatalf("snapshot with 101 entities is %d bytes, want < 1200", len(buf))
	}
	t.Logf("snapshot with 101 entities: %d bytes", len(buf))
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
