package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/ops"
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/world"
)

// fakeConn is an in-memory transport.Conn with a bounded send queue that
// nobody drains, so a small cap models a stuck client.
type fakeConn struct {
	in   chan []byte
	done chan struct{}
	once sync.Once

	mu     sync.Mutex
	cap    int
	frames [][]byte
}

func newFakeConn(queue int) *fakeConn {
	return &fakeConn{in: make(chan []byte, 8), done: make(chan struct{}), cap: queue}
}

func (f *fakeConn) Recv(ctx context.Context) ([]byte, error) {
	select {
	case b := <-f.in:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeConn) Send(frame []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.frames) >= f.cap {
		return false
	}
	f.frames = append(f.frames, frame)
	return true
}

func (f *fakeConn) Close()                { f.once.Do(func() { close(f.done) }) }
func (f *fakeConn) Done() <-chan struct{} { return f.done }

func (f *fakeConn) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.frames)
}

// seen lists the received messages: the countdown value, or -1 for Reload.
func (f *fakeConn) seen(t *testing.T) []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []int
	for _, fr := range f.frames {
		env := protocol.GetRootAsEnvelope(fr, 0)
		var tbl flatbuffers.Table
		env.Msg(&tbl)
		switch env.MsgType() {
		case protocol.MessageRestartCountdown:
			var rc protocol.RestartCountdown
			rc.Init(tbl.Bytes, tbl.Pos)
			out = append(out, int(rc.SecondsRemaining()))
		case protocol.MessageReload:
			out = append(out, -1)
		}
	}
	return out
}

// joinFrames is how many frames a client gets on joining.
const joinFrames = 4

func connect(t *testing.T, w *world.World, sess *session.Server, queue int) *fakeConn {
	t.Helper()
	c := newFakeConn(queue)
	go sess.ServeConn(c)
	c.in <- hello(uint16(protocol.ProtocolVersionCurrent), "")
	waitCount(t, w, c, joinFrames) // Welcome, ItemCatalog, PlayerStats, InventoryState
	c.in <- ping(1)
	waitCount(t, w, c, joinFrames+1) // Pong: the read loop is running and registered
	return c
}

func waitCount(t *testing.T, w *world.World, c *fakeConn, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d frames, have %d", n, c.count())
		}
		w.Tick()
		time.Sleep(time.Millisecond)
	}
}

func runShutdown(t *testing.T, sess *session.Server) {
	t.Helper()
	clock := world.NewFakeClock(time.Unix(0, 0))
	done := make(chan error, 1)
	go func() {
		done <- ops.GracefulShutdown(context.Background(), clock, 3*time.Second, ops.ShutdownHooks{
			Announce: sess.BroadcastRestartCountdown,
			Save: func(ctx context.Context) error {
				sess.BroadcastReload("server restarting")
				return sess.SaveAll(ctx)
			},
		})
	}()
	for clock.TickerCount() == 0 {
		select {
		case err := <-done:
			t.Fatalf("shutdown ended early: %v", err)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	for i := 0; i < 3; i++ {
		clock.Advance(time.Second)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish on the fake clock's schedule")
	}
}

func newShutdownRig() (*world.World, *session.Server) {
	clock := world.NewFakeClock(time.Unix(0, 0))
	w := world.New(world.DefaultConfig())
	sess := session.New(session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, MaxMsgPerSec: 1000}, w, clock, store.NewMemory(), nil)
	_ = w.Register(sess)
	return w, sess
}

func TestRestartCountdownBroadcastBeforeReload(t *testing.T) {
	w, sess := newShutdownRig()
	a, b := connect(t, w, sess, 64), connect(t, w, sess, 64)
	runShutdown(t, sess)
	want := []int{3, 2, 1, -1}
	for name, c := range map[string]*fakeConn{"a": a, "b": b} {
		got := c.seen(t)
		if len(got) != len(want) {
			t.Fatalf("%s got %v, want %v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s got %v, want %v", name, got, want)
			}
		}
	}
}

func TestRestartCountdownFullQueueDoesNotBlockOthers(t *testing.T) {
	w, sess := newShutdownRig()
	stuck := connect(t, w, sess, joinFrames+1) // full after the join frames and Pong
	ok := connect(t, w, sess, 64)
	runShutdown(t, sess)
	if got := ok.seen(t); len(got) != 4 || got[0] != 3 || got[1] != 2 || got[2] != 1 || got[3] != -1 {
		t.Fatalf("healthy session got %v", got)
	}
	if got := stuck.seen(t); len(got) != 0 {
		t.Fatalf("stuck session should have dropped everything, got %v", got)
	}
}
