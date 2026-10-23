package session_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/systems/chat"
	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/transport"
	"github.com/cleiderg/lush/server/internal/world"
)

type stats struct{ bytes, dropped atomic.Int64 }

func (s *stats) AddBytesOut(n int) { s.bytes.Add(int64(n)) }
func (s *stats) AddDropped(n int)  { s.dropped.Add(int64(n)) }

type rig struct {
	t     *testing.T
	w     *world.World
	sess  *session.Server
	url   string
	stats *stats
}

func newRig(t *testing.T, queue int) *rig {
	t.Helper()
	clock := world.NewFakeClock(time.Unix(0, 0))
	w := world.New(world.DefaultConfig())
	st := &stats{}
	sess := session.New(session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second}, w, clock, nil)
	mv, err := movement.New(movement.Config{MaxSpeed: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []world.System{mv, chat.New(chat.DefaultConfig(), clock, sess), sess} {
		if err := w.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(transport.NewWebSocket(st, transport.WithSendQueue(queue)).HTTPHandler(sess.ServeConn))
	t.Cleanup(srv.Close)
	return &rig{t: t, w: w, sess: sess, url: "ws" + strings.TrimPrefix(srv.URL, "http"), stats: st}
}

func envelope(typ protocol.Message, build func(b *flatbuffers.Builder) flatbuffers.UOffsetT) []byte {
	b := flatbuffers.NewBuilder(64)
	msg := build(b)
	protocol.EnvelopeStart(b)
	protocol.EnvelopeAddMsgType(b, typ)
	protocol.EnvelopeAddMsg(b, msg)
	b.Finish(protocol.EnvelopeEnd(b))
	return b.FinishedBytes()
}

func hello(version uint16, token string) []byte {
	return envelope(protocol.MessageHello, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		t := b.CreateString(token)
		protocol.HelloStart(b)
		protocol.HelloAddProtocolVersion(b, version)
		protocol.HelloAddGuestToken(b, t)
		return protocol.HelloEnd(b)
	})
}

func input(seq uint32, x, z float32) []byte {
	return envelope(protocol.MessageInput, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.InputStart(b)
		protocol.InputAddSeq(b, seq)
		protocol.InputAddMove(b, protocol.CreateVec2(b, x, z))
		return protocol.InputEnd(b)
	})
}

func chatSend(text string) []byte {
	return envelope(protocol.MessageChatSend, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		t := b.CreateString(text)
		protocol.ChatSendStart(b)
		protocol.ChatSendAddText(b, t)
		return protocol.ChatSendEnd(b)
	})
}

func ping(token uint32) []byte {
	return envelope(protocol.MessagePing, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.PingStart(b)
		protocol.PingAddToken(b, token)
		return protocol.PingEnd(b)
	})
}

type client struct {
	t *testing.T
	c *websocket.Conn
}

func (r *rig) dial(helloMsg []byte) *client {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, r.url, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { c.CloseNow() })
	cl := &client{r.t, c}
	cl.send(helloMsg)
	return cl
}

func (c *client) send(b []byte) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.c.Write(ctx, websocket.MessageBinary, b); err != nil {
		c.t.Fatal(err)
	}
}

// next reads one envelope, ticking the world while waiting.
func (r *rig) next(c *client) (protocol.Message, flatbuffers.Table) {
	r.t.Helper()
	type res struct {
		data []byte
		err  error
	}
	ch := make(chan res, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		_, d, err := c.c.Read(ctx)
		ch <- res{d, err}
	}()
	for {
		select {
		case x := <-ch:
			if x.err != nil {
				r.t.Fatalf("read: %v", x.err)
			}
			env := protocol.GetRootAsEnvelope(x.data, 0)
			var tbl flatbuffers.Table
			env.Msg(&tbl)
			return env.MsgType(), tbl
		case <-time.After(5 * time.Millisecond):
			r.w.Tick()
		}
	}
}

func (r *rig) expectWelcome(c *client) (uint16, string) {
	r.t.Helper()
	typ, tbl := r.next(c)
	if typ != protocol.MessageWelcome {
		r.t.Fatalf("got message %v, want Welcome", typ)
	}
	var w protocol.Welcome
	w.Init(tbl.Bytes, tbl.Pos)
	return w.PlayerId(), string(w.GuestToken())
}

// waitFor reads until a message of typ arrives.
func (r *rig) waitFor(c *client, typ protocol.Message) flatbuffers.Table {
	r.t.Helper()
	for i := 0; i < 1000; i++ {
		got, tbl := r.next(c)
		if got == typ {
			return tbl
		}
	}
	r.t.Fatalf("never saw message %v", typ)
	return flatbuffers.Table{}
}

func TestVersionMismatchGetsReloadAndDisconnect(t *testing.T) {
	r := newRig(t, 16)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent)+1, ""))
	typ, tbl := r.next(c)
	if typ != protocol.MessageReload {
		t.Fatalf("got %v, want Reload", typ)
	}
	var rl protocol.Reload
	rl.Init(tbl.Bytes, tbl.Pos)
	if len(rl.Reason()) == 0 {
		t.Error("empty reload reason")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := c.c.Read(ctx); err == nil {
		t.Fatal("connection still open after Reload")
	}
	if n := len(r.w.Players()); n != 0 {
		t.Errorf("%d players in world, want 0", n)
	}
}

func TestGuestTokenResumesSamePlayer(t *testing.T) {
	r := newRig(t, 16)
	v := uint16(protocol.ProtocolVersionCurrent)

	c1 := r.dial(hello(v, ""))
	id1, tok := r.expectWelcome(c1)
	if len(tok) < 16 {
		t.Fatalf("token %q too short", tok)
	}
	c2 := r.dial(hello(v, ""))
	id2, tok2 := r.expectWelcome(c2)
	if id2 == id1 || tok2 == tok {
		t.Fatalf("second guest got same identity: %d/%d", id1, id2)
	}

	c1.c.Close(websocket.StatusNormalClosure, "")
	c3 := r.dial(hello(v, tok))
	id3, tok3 := r.expectWelcome(c3)
	if id3 != id1 || tok3 != tok {
		t.Fatalf("resume got id %d token %q, want %d %q", id3, tok3, id1, tok)
	}
}

func TestInputChatPingSnapshot(t *testing.T) {
	r := newRig(t, 16)
	v := uint16(protocol.ProtocolVersionCurrent)
	c := r.dial(hello(v, ""))
	id, _ := r.expectWelcome(c)

	c.send(ping(77))
	tbl := r.waitFor(c, protocol.MessagePong)
	var pong protocol.Pong
	pong.Init(tbl.Bytes, tbl.Pos)
	if pong.Token() != 77 {
		t.Errorf("pong token %d, want 77", pong.Token())
	}

	c.send(input(5, 1, 0))
	c.send(chatSend("hello"))
	var sawChat bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		typ, tbl := r.next(c)
		switch typ {
		case protocol.MessageChatMessage:
			var m protocol.ChatMessage
			m.Init(tbl.Bytes, tbl.Pos)
			if m.SpeakerId() != id || string(m.Text()) != "hello" {
				t.Fatalf("chat = %d %q", m.SpeakerId(), m.Text())
			}
			sawChat = true
		case protocol.MessageSnapshot:
			var s protocol.Snapshot
			s.Init(tbl.Bytes, tbl.Pos)
			var e protocol.Entity
			if s.LastInputSeq() == 5 && sawChat && s.EntitiesLength() == 1 && s.Entities(&e, 0) && e.Id() == id && e.X() > 0 {
				return
			}
		}
	}
	t.Fatal("did not see chat plus a snapshot with the processed input and moved player")
}

func TestSlowClientDoesNotDelayTick(t *testing.T) {
	r := newRig(t, 2)
	v := uint16(protocol.ProtocolVersionCurrent)
	c := r.dial(hello(v, ""))
	r.expectWelcome(c)
	// Crowd the client's view so each snapshot is large; the client never reads again.
	for i := 0; i < 100; i++ {
		_ = r.w.AddPlayer(&world.Player{ID: world.PlayerID(1000 + i), Pos: world.Vec2{X: float32(i % 10), Y: float32(i / 10)}})
	}
	var worst time.Duration
	for i := 0; i < 300000 && r.stats.dropped.Load() == 0; i++ {
		start := time.Now()
		r.w.Tick()
		if d := time.Since(start); d > worst {
			worst = d
		}
	}
	if r.stats.dropped.Load() == 0 {
		t.Fatal("no frames dropped for a client that never reads")
	}
	if worst > 100*time.Millisecond {
		t.Fatalf("slowest tick %v with a stalled client", worst)
	}
}
