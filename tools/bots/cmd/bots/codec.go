package main

import (
	"math"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/tools/bots/internal/protocol"
)

func finish(b *flatbuffers.Builder, typ protocol.Message, msg flatbuffers.UOffsetT) []byte {
	protocol.EnvelopeStart(b)
	protocol.EnvelopeAddMsgType(b, typ)
	protocol.EnvelopeAddMsg(b, msg)
	b.Finish(protocol.EnvelopeEnd(b))
	return append([]byte(nil), b.FinishedBytes()...)
}

func encodeHello() []byte {
	b := flatbuffers.NewBuilder(64)
	protocol.HelloStart(b)
	protocol.HelloAddProtocolVersion(b, uint16(protocol.ProtocolVersionCurrent))
	return finish(b, protocol.MessageHello, protocol.HelloEnd(b))
}

func encodeInput(b *flatbuffers.Builder, seq uint32, x, z float64) []byte {
	b.Reset()
	protocol.InputStart(b)
	protocol.InputAddSeq(b, seq)
	protocol.InputAddMove(b, protocol.CreateVec2(b, float32(x), float32(z)))
	protocol.InputAddFacing(b, float32(math.Atan2(z, x)))
	return finish(b, protocol.MessageInput, protocol.InputEnd(b))
}

func encodeChat(b *flatbuffers.Builder, text string) []byte {
	b.Reset()
	t := b.CreateString(text)
	protocol.ChatSendStart(b)
	protocol.ChatSendAddText(b, t)
	return finish(b, protocol.MessageChatSend, protocol.ChatSendEnd(b))
}

func encodePing(b *flatbuffers.Builder, tok uint32) []byte {
	b.Reset()
	protocol.PingStart(b)
	protocol.PingAddToken(b, tok)
	return finish(b, protocol.MessagePing, protocol.PingEnd(b))
}

func encodeWave(b *flatbuffers.Builder) []byte {
	b.Reset()
	protocol.EmoteSendStart(b)
	protocol.EmoteSendAddEmote(b, protocol.EmoteWave)
	return finish(b, protocol.MessageEmoteSend, protocol.EmoteSendEnd(b))
}

func encodeItemAction(b *flatbuffers.Builder, a itemAction) []byte {
	b.Reset()
	protocol.ItemActionStart(b)
	protocol.ItemActionAddAction(b, a.kind)
	protocol.ItemActionAddSlot(b, a.slot)
	protocol.ItemActionAddEquipSlot(b, a.equipSlot)
	return finish(b, protocol.MessageItemAction, protocol.ItemActionEnd(b))
}

// inbound is a decoded server message; only the fields for typ are set.
type inbound struct {
	typ          protocol.Message
	playerID     uint16
	spawn        [2]float64 // Welcome, metres
	token        uint32
	text         string
	hasSelf      bool      // Snapshot contains the entity self
	selfX, selfZ int16     // its position in decimetres
	inv          inventory // InventoryState
}

// decode parses a server frame; self is this bot's player id, looked up in
// Snapshots. FlatBuffers readers panic on malformed data, so a panic becomes
// ok=false.
func decode(buf []byte, self uint16) (m inbound, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	env := protocol.GetRootAsEnvelope(buf, 0)
	m.typ = env.MsgType()
	var tbl flatbuffers.Table
	if !env.Msg(&tbl) {
		return m, false
	}
	switch m.typ {
	case protocol.MessageWelcome:
		var w protocol.Welcome
		w.Init(tbl.Bytes, tbl.Pos)
		m.playerID = w.PlayerId()
		if v := w.Spawn(nil); v != nil {
			m.spawn = [2]float64{float64(v.X()), float64(v.Z())}
		}
	case protocol.MessageSnapshot:
		var s protocol.Snapshot
		s.Init(tbl.Bytes, tbl.Pos)
		var e protocol.Entity
		for i := 0; i < s.EntitiesLength(); i++ { // the server lists the player first
			if s.Entities(&e, i) && e.Id() == self {
				m.hasSelf, m.selfX, m.selfZ = true, e.X(), e.Z()
				break
			}
		}
	case protocol.MessageInventoryState:
		var is protocol.InventoryState
		is.Init(tbl.Bytes, tbl.Pos)
		m.inv = inventory{known: true, weapon: is.Weapon(), slots: make([]uint16, is.SlotsLength())}
		var sl protocol.InventorySlot
		for i := range m.inv.slots {
			if is.Slots(&sl, i) {
				m.inv.slots[i] = sl.ItemId()
			}
		}
	case protocol.MessagePong:
		var p protocol.Pong
		p.Init(tbl.Bytes, tbl.Pos)
		m.token = p.Token()
	case protocol.MessageReload:
		var r protocol.Reload
		r.Init(tbl.Bytes, tbl.Pos)
		m.text = string(r.Reason())
	}
	return m, true
}
