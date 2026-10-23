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

// inbound is a decoded server message; only the fields for typ are set.
type inbound struct {
	typ      protocol.Message
	playerID uint16
	token    uint32
	text     string
}

// decode parses a server frame. FlatBuffers readers panic on malformed data,
// so a panic becomes ok=false.
func decode(buf []byte) (m inbound, ok bool) {
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
