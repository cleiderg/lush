package main

import (
	"context"
	"sync"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// pipeConn is an in-memory transport.Conn: the test feeds frames in and reads
// what the session sent.
type pipeConn struct {
	in   chan []byte
	done chan struct{}
	once sync.Once

	mu  sync.Mutex
	out [][]byte
}

func newPipeConn() *pipeConn { return &pipeConn{in: make(chan []byte, 8), done: make(chan struct{})} }

func (c *pipeConn) Recv(ctx context.Context) ([]byte, error) {
	select {
	case b := <-c.in:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *pipeConn) Send(frame []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.out = append(c.out, frame)
	return true
}

func (c *pipeConn) Close()                { c.once.Do(func() { close(c.done) }) }
func (c *pipeConn) Done() <-chan struct{} { return c.done }

// take returns the first unread message of type typ, ticking w until it comes.
func (c *pipeConn) take(t *testing.T, w *world.World, typ protocol.Message) flatbuffers.Table {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for i, fr := range c.out {
			env := protocol.GetRootAsEnvelope(fr, 0)
			var tbl flatbuffers.Table
			if env.MsgType() == typ && env.Msg(&tbl) {
				c.out = c.out[i+1:]
				c.mu.Unlock()
				return tbl
			}
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		w.Tick()
	}
	t.Fatalf("never got message %v", typ)
	return flatbuffers.Table{}
}

func wireEnvelope(typ protocol.Message, build func(b *flatbuffers.Builder) flatbuffers.UOffsetT) []byte {
	b := flatbuffers.NewBuilder(64)
	msg := build(b)
	protocol.EnvelopeStart(b)
	protocol.EnvelopeAddMsgType(b, typ)
	protocol.EnvelopeAddMsg(b, msg)
	b.Finish(protocol.EnvelopeEnd(b))
	return b.FinishedBytes()
}

// AC4 (#621): gameSystems sets the session's furniture system with the
// housing.json stalls, so a thrift ShopBuy sent by a client is answered: Ok at
// each area's stall, TooFar from a spot far from every stall.
func TestGameSystemsWireThriftStalls(t *testing.T) {
	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	mover, grid, err := newMover(5, 0)
	if err != nil {
		t.Fatal(err)
	}
	clock := world.NewFakeClock(time.Unix(0, 0))
	w := world.New(world.DefaultConfig())
	players := store.NewMemory()
	sess := session.New(session.Config{Data: data}, w, clock, players, nil)
	t.Cleanup(sess.Close)
	lookup, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	systems, err := gameSystems(mover, data, lookup, grid, 5, w.TickInterval(), clock, sess, players)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range systems {
		if err := w.RegisterEvery(s.sys, s.every); err != nil {
			t.Fatal(err)
		}
	}
	seed, err := loadHousingSeed()
	if err != nil {
		t.Fatal(err)
	}
	var item gamedata.ItemID
	for _, it := range data.Items {
		if _, ok := data.ThriftSellPrice(it.ID); ok {
			item = it.ID
			break
		}
	}

	c := newPipeConn()
	t.Cleanup(c.Close)
	go sess.ServeConn(c)
	c.in <- wireEnvelope(protocol.MessageHello, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		tok := b.CreateString("")
		protocol.HelloStart(b)
		protocol.HelloAddProtocolVersion(b, uint16(protocol.ProtocolVersionCurrent))
		protocol.HelloAddGuestToken(b, tok)
		return protocol.HelloEnd(b)
	})
	var welcome protocol.Welcome
	tbl := c.take(t, w, protocol.MessageWelcome)
	welcome.Init(tbl.Bytes, tbl.Pos)
	w.Tick()
	p := w.Player(world.PlayerID(welcome.PlayerId()))
	if p == nil {
		t.Fatal("player not in the world")
	}

	buy := func(pos world.Vec2) protocol.ShopResultCode {
		t.Helper()
		p.Pos = pos
		p.Inv.Gold = 100000
		c.in <- wireEnvelope(protocol.MessageShopBuy, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
			protocol.ShopBuyStart(b)
			protocol.ShopBuyAddItemId(b, uint16(item))
			protocol.ShopBuyAddQuantity(b, 1)
			protocol.ShopBuyAddShop(b, protocol.ShopKindThrift)
			return protocol.ShopBuyEnd(b)
		})
		tbl := c.take(t, w, protocol.MessageShopResult)
		var sr protocol.ShopResult
		sr.Init(tbl.Bytes, tbl.Pos)
		return sr.Code()
	}
	for i, st := range seed.Stalls {
		if got := buy(st); got != protocol.ShopResultCodeOk {
			t.Fatalf("at stall %d: %v, want Ok", i, protocol.EnumNamesShopResultCode[got])
		}
	}
	if got := buy(world.Vec2{X: 0, Y: 0}); got != protocol.ShopResultCodeTooFar {
		t.Fatalf("far from every stall: %v, want TooFar", protocol.EnumNamesShopResultCode[got])
	}
}
