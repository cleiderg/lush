package session_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/chat"
	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/transport"
	"github.com/cleiderg/lush/server/internal/world"
)

const interval = 5 * time.Second

type saveRec struct {
	p   store.Player // as passed to Save
	err error
}

// recStore records every Save and can fail or block them.
type recStore struct {
	store.Players
	mu    sync.Mutex
	saves []saveRec
	fail  error
	block chan struct{}
}

func (s *recStore) Save(ctx context.Context, p store.Player) (store.Player, error) {
	s.mu.Lock()
	fail, block := s.fail, s.block
	s.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return store.Player{}, ctx.Err()
		}
	}
	var out store.Player
	err := fail
	if err == nil {
		out, err = s.Players.Save(ctx, p)
	}
	s.mu.Lock()
	s.saves = append(s.saves, saveRec{p, err})
	s.mu.Unlock()
	return out, err
}

func (s *recStore) set(fail error, block chan struct{}) {
	s.mu.Lock()
	s.fail, s.block = fail, block
	s.mu.Unlock()
}

func (s *recStore) snapshot() []saveRec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]saveRec(nil), s.saves...)
}

// count returns the successful and failed saves for a store id.
func (s *recStore) count(id int64) (ok, bad int) {
	for _, r := range s.snapshot() {
		if r.p.ID == id {
			if r.err == nil {
				ok++
			} else {
				bad++
			}
		}
	}
	return
}

func (s *recStore) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; saves: %+v", what, s.snapshot())
		}
		time.Sleep(time.Millisecond)
	}
}

type saveRig struct {
	*rig
	clock *world.FakeClock
	st    *recStore
}

func newSaveRig(t *testing.T) *saveRig {
	t.Helper()
	clock := world.NewFakeClock(time.Unix(0, 0))
	w := world.New(world.DefaultConfig())
	st := &recStore{Players: store.NewMemory()}
	c := cfg()
	c.SaveInterval = interval
	sess := session.New(c, w, clock, st, nil)
	t.Cleanup(sess.Close)
	mv, err := movement.New(movement.Config{MaxSpeed: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []world.System{mv, chat.New(chat.DefaultConfig(), clock, sess), sess} {
		if err := w.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(transport.NewWebSocket(&stats{}).HTTPHandler(sess.ServeConn))
	t.Cleanup(srv.Close)
	r := &rig{t: t, w: w, sess: sess, url: "ws" + strings.TrimPrefix(srv.URL, "http"), stats: &stats{}}
	return &saveRig{r, clock, st}
}

// join connects a new guest and returns its session id, store id and client.
func (r *saveRig) join() (world.PlayerID, int64, *client) {
	r.t.Helper()
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	id, tok := r.expectWelcome(c)
	sp, err := r.st.Players.LoadByTokenHash(context.Background(), sha(tok))
	if err != nil {
		r.t.Fatal(err)
	}
	return world.PlayerID(id), sp.ID, c
}

// intervalPasses advances the clock one save interval and runs a tick.
func (r *saveRig) intervalPasses() {
	r.clock.Advance(interval)
	r.w.Tick()
}

// tickUntilGone ticks until the player has left the world.
func (r *saveRig) tickUntilGone(id world.PlayerID) {
	r.t.Helper()
	for i := 0; i < 2000 && r.w.Player(id) != nil; i++ {
		r.w.Tick()
		time.Sleep(time.Millisecond)
	}
	if r.w.Player(id) != nil {
		r.t.Fatal("player never left")
	}
}

func TestOneSavePerPlayerPerInterval(t *testing.T) {
	r := newSaveRig(t)
	_, a, _ := r.join()
	_, b, _ := r.join()
	r.w.Tick() // arms the interval

	for n := 1; n <= 3; n++ {
		r.intervalPasses()
		r.st.waitFor(t, "periodic saves", func() bool {
			oa, _ := r.st.count(a)
			ob, _ := r.st.count(b)
			return oa == n && ob == n
		})
		r.w.Tick() // same interval: no extra save
		r.w.Tick()
	}
	time.Sleep(20 * time.Millisecond)
	if oa, _ := r.st.count(a); oa != 3 {
		t.Fatalf("player a saved %d times in 3 intervals", oa)
	}
}

func TestDisconnectSavesFinalPosition(t *testing.T) {
	r := newSaveRig(t)
	id, sid, c := r.join()
	c.send(input(1, 1, 0))
	for i := 0; i < 200 && r.w.Player(id).Pos.X < 3; i++ {
		r.w.Tick()
		time.Sleep(time.Millisecond)
	}
	c.send(input(2, 0, 0))
	time.Sleep(50 * time.Millisecond)
	r.w.Tick()
	r.w.Tick()
	want := r.w.Player(id).Pos
	if want.X <= 0.5 {
		t.Fatalf("player did not move: %+v", want)
	}
	c.c.CloseNow()
	for i := 0; i < 2000 && r.w.Player(id) != nil; i++ {
		if p := r.w.Player(id); p != nil {
			want = p.Pos // last position before the leave is applied
		}
		r.w.Tick()
		time.Sleep(time.Millisecond)
	}
	r.st.waitFor(t, "logout save", func() bool { ok, _ := r.st.count(sid); return ok == 1 })
	got := r.st.snapshot()[0].p
	if got.X != want.X || got.Z != want.Y {
		t.Fatalf("saved %v,%v, want player's position at leave %v,%v", got.X, got.Z, want.X, want.Y)
	}
}

func TestSavesInterleaveWithoutStaleVersion(t *testing.T) {
	r := newSaveRig(t)
	id, sid, c := r.join()
	r.w.Tick()

	// Hold the first periodic save in flight while a second interval and the
	// disconnect queue behind it.
	block := make(chan struct{})
	r.st.set(nil, block)
	r.intervalPasses()
	r.intervalPasses()
	c.c.CloseNow()
	r.tickUntilGone(id)
	close(block)

	// Reconnect with the same guest and run a shutdown save too.
	r.st.waitFor(t, "disconnect save", func() bool { ok, _ := r.st.count(sid); return ok >= 2 })
	for _, rec := range r.st.snapshot() {
		if rec.err != nil {
			t.Fatalf("save failed: %v", rec.err)
		}
	}
	if err := r.sess.SaveAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	prev := int64(0)
	for _, rec := range r.st.snapshot() {
		if rec.p.Version <= prev {
			t.Fatalf("versions not increasing: %+v", r.st.snapshot())
		}
		prev = rec.p.Version
	}
}

func TestSaveAllSavesEveryPlayerOnShutdown(t *testing.T) {
	r := newSaveRig(t)
	var ids []int64
	for i := 0; i < 3; i++ {
		_, sid, _ := r.join()
		ids = append(ids, sid)
	}
	if err := r.sess.SaveAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	// SaveAll returned: everything is already written, no waiting.
	for _, sid := range ids {
		if ok, _ := r.st.count(sid); ok != 1 {
			t.Fatalf("player %d saved %d times, want 1", sid, ok)
		}
	}
}

func TestSaveAllReportsFailure(t *testing.T) {
	r := newSaveRig(t)
	r.join()
	r.st.set(errors.New("db down"), nil)
	if err := r.sess.SaveAll(context.Background()); err == nil {
		t.Fatal("SaveAll succeeded although the store failed")
	}
}

func TestStoreFailureDoesNotStallTicksAndRetries(t *testing.T) {
	r := newSaveRig(t)
	_, sid, _ := r.join()
	r.w.Tick()

	// A failing store: logged, ticks continue, next interval retries.
	r.st.set(errors.New("db down"), nil)
	r.intervalPasses()
	r.st.waitFor(t, "failed save", func() bool { _, bad := r.st.count(sid); return bad == 1 })
	r.st.set(nil, nil)
	r.intervalPasses()
	r.st.waitFor(t, "retry", func() bool { ok, _ := r.st.count(sid); return ok == 1 })

	// A blocked store: the tick is not held up.
	block := make(chan struct{})
	r.st.set(nil, block)
	r.intervalPasses()
	before := r.w.TickCount()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			r.w.Tick()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ticks stalled behind a blocked store")
	}
	if r.w.TickCount() != before+100 {
		t.Fatalf("tick count %d, want %d", r.w.TickCount(), before+100)
	}
	close(block)
	r.st.waitFor(t, "save after unblock", func() bool { ok, _ := r.st.count(sid); return ok == 2 })
}
