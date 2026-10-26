package session_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/world"
)

// spyStore records what the session hands the store and can block or fail loads.
type spyStore struct {
	store.Players
	creates atomic.Int32
	hashes  chan string // every hash seen, buffered
	block   chan struct{}
	failErr error
	inLoad  atomic.Int32 // loads currently parked on block
}

func newSpy() *spyStore {
	return &spyStore{Players: store.NewMemory(), hashes: make(chan string, 64)}
}

func (s *spyStore) CreateGuest(ctx context.Context, h string, kit store.Holdings, entries []store.Entry) (store.Player, error) {
	s.creates.Add(1)
	s.hashes <- h
	return s.Players.CreateGuest(ctx, h, kit, entries)
}

func (s *spyStore) LoadByTokenHash(ctx context.Context, h string) (store.Player, error) {
	s.hashes <- h
	if s.failErr != nil {
		return store.Player{}, s.failErr
	}
	if s.block != nil {
		s.inLoad.Add(1)
		defer s.inLoad.Add(-1)
		select {
		case <-s.block:
		case <-ctx.Done():
			return store.Player{}, ctx.Err()
		}
	}
	return s.Players.LoadByTokenHash(ctx, h)
}

func sha(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func cfg() session.Config {
	return session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Spawn: world.Vec2{X: 7, Y: 9}}
}

func welcomeSpawn(t *testing.T, r *rig, c *client) (uint16, float32, float32) {
	t.Helper()
	typ, tbl := r.next(c)
	if typ != protocol.MessageWelcome {
		t.Fatalf("got %v, want Welcome", typ)
	}
	var w protocol.Welcome
	w.Init(tbl.Bytes, tbl.Pos)
	return w.PlayerId(), w.Spawn(nil).X(), w.Spawn(nil).Z()
}

func TestRestartResumesStoredPlayerAtStoredPosition(t *testing.T) {
	spy := newSpy()
	v := uint16(protocol.ProtocolVersionCurrent)
	r1 := newRigStore(t, 16, cfg(), spy)
	c1 := r1.dial(hello(v, ""))
	_, tok := r1.expectWelcome(c1)
	sp, err := spy.Players.LoadByTokenHash(context.Background(), sha(tok))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spy.Players.Save(context.Background(), store.Player{ID: sp.ID, X: 40, Z: 50, Version: sp.Version}); err != nil {
		t.Fatal(err)
	}

	// "Restart": a new world and session over the same store.
	r2 := newRigStore(t, 16, cfg(), spy)
	before := spy.creates.Load()
	c2 := r2.dial(hello(v, tok))
	_, x, z := welcomeSpawn(t, r2, c2)
	if x != 40 || z != 50 {
		t.Fatalf("resumed at %v,%v, want 40,50", x, z)
	}
	if spy.creates.Load() != before {
		t.Fatal("resume created a new stored player")
	}
	got, err := spy.Players.LoadByTokenHash(context.Background(), sha(tok))
	if err != nil || got.ID != sp.ID {
		t.Fatalf("store id %d err %v, want %d", got.ID, err, sp.ID)
	}
}

func TestOnlyTokenHashReachesStore(t *testing.T) {
	spy := newSpy()
	r := newRigStore(t, 16, cfg(), spy)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	_, tok := r.expectWelcome(c)
	c2 := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), tok))
	r.expectWelcome(c2)
	close(spy.hashes)
	for h := range spy.hashes {
		if h == tok || h != sha(tok) {
			t.Fatalf("store saw %q, want only sha256(token)", h)
		}
	}
}

func TestUnknownTokenCreatesPlayerAtSpawn(t *testing.T) {
	spy := newSpy()
	r := newRigStore(t, 16, cfg(), spy)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), "stale-or-forged"))
	_, x, z := welcomeSpawn(t, r, c)
	if x != 7 || z != 9 {
		t.Fatalf("spawned at %v,%v, want 7,9", x, z)
	}
	if spy.creates.Load() != 1 {
		t.Fatalf("creates = %d, want 1", spy.creates.Load())
	}
}

func TestReclaimedSessionIdResumesUnderNewId(t *testing.T) {
	spy := newSpy()
	c0 := cfg()
	c0.MaxGuests = 2
	r := newRigStore(t, 16, c0, spy)
	v := uint16(protocol.ProtocolVersionCurrent)
	a := r.dial(hello(v, ""))
	idA, tokA := r.expectWelcome(a)
	b := r.dial(hello(v, ""))
	idB, _ := r.expectWelcome(b)

	a.c.Close(websocket.StatusNormalClosure, "")
	for deadline := time.Now().Add(5 * time.Second); len(r.w.Players()) != 1; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("first guest never left")
		}
		r.clock.Advance(time.Second) // past the logout lock
		r.w.Tick()
	}
	d := r.dial(hello(v, ""))
	if id, _ := r.expectWelcome(d); id != idA {
		t.Fatalf("new guest id %d, want reclaimed %d", id, idA)
	}
	b.c.Close(websocket.StatusNormalClosure, "")
	for deadline := time.Now().Add(5 * time.Second); len(r.w.Players()) != 1; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("guests never left")
		}
		r.clock.Advance(time.Second)
		r.w.Tick()
	}
	// A's session id now belongs to someone else; A resumes the same stored player.
	want, err := spy.Players.LoadByTokenHash(context.Background(), sha(tokA))
	if err != nil {
		t.Fatal(err)
	}
	creates := spy.creates.Load()
	a2 := r.dial(hello(v, tokA))
	id, tok := r.expectWelcome(a2)
	if tok != tokA || spy.creates.Load() != creates {
		t.Fatal("old token did not resume its stored player")
	}
	if id != idB {
		t.Fatalf("resumed under id %d, want the id %d freed by the other guest", id, idB)
	}
	if id == idA {
		t.Fatalf("resumed under the reclaimed id %d", id)
	}
	if got, _ := spy.Players.LoadByTokenHash(context.Background(), sha(tokA)); got.ID != want.ID {
		t.Fatal("store id changed")
	}
}

func TestBlockedStoreDoesNotStallTicks(t *testing.T) {
	spy := newSpy()
	c := cfg()
	c.HelloTimeout = time.Minute // the parked load must outlast the checks below
	r := newRigStore(t, 16, c, spy)
	v := uint16(protocol.ProtocolVersionCurrent)
	a := r.dial(hello(v, ""))
	_, tok := r.expectWelcome(a)
	<-spy.hashes // a's CreateGuest

	spy.block = make(chan struct{})
	defer close(spy.block)
	r.dial(hello(v, tok))
	select {
	case <-spy.hashes: // the handshake is now inside LoadByTokenHash
	case <-time.After(5 * time.Second):
		t.Fatal("handshake never reached the store")
	}

	// Tick on another goroutine so a tick stuck behind the store fails the
	// test instead of hanging it.
	start := r.w.TickCount()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			r.w.Tick()
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ticks stalled while the store blocked")
	}
	if got := r.w.TickCount(); got != start+5 {
		t.Fatalf("tick count %d, want %d", got, start+5)
	}
	r.waitFor(a, protocol.MessageSnapshot)
	if spy.inLoad.Load() != 1 {
		t.Fatal("store load returned before the checks; they proved nothing")
	}
}

// syncBuf is a goroutine-safe log sink.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestStoreErrorEndsConnection(t *testing.T) {
	spy := newSpy()
	spy.failErr = errors.New("db down")
	logs := &syncBuf{}
	r := newRigLog(t, 16, cfg(), spy, slog.New(slog.NewTextHandler(logs, nil)))
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), "some-token"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := c.c.Read(ctx); err == nil {
		t.Fatal("expected the connection to end")
	}
	if n := len(r.w.Players()); n != 0 {
		t.Fatalf("%d players in world, want 0", n)
	}
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(logs.String(), "db down"); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("store error not logged; log: %q", logs.String())
		}
	}
}

// testGrid is a 64x64 collision grid of 1 m cells from (0,0) with one
// blocked cell, (40,50).
func testGrid(t *testing.T) *world.Grid {
	t.Helper()
	const n = 64
	b := make([]byte, 22+n*n/8)
	copy(b, "LCOL")
	b[4], b[6], b[8], b[10], b[12] = 1, 10, 64, n, n
	i := 50*n + 40
	b[22+i>>3] |= 1 << (uint(i) & 7)
	g, err := world.ParseGrid(b)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestResumeOnBlockedSavedPositionUsesSpawn(t *testing.T) {
	tests := []struct {
		name         string
		x, z         float32
		wantX, wantZ float32
	}{
		{"walkable kept", 20.5, 30.5, 20.5, 30.5},
		{"blocked cell", 40.5, 50.5, 7, 9},
		{"outside grid", 100, 100, 7, 9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spy := newSpy()
			v := uint16(protocol.ProtocolVersionCurrent)
			c := cfg()
			c.Collision = testGrid(t)
			r1 := newRigStore(t, 16, c, spy)
			c1 := r1.dial(hello(v, ""))
			_, tok := r1.expectWelcome(c1)
			sp, err := spy.Players.LoadByTokenHash(context.Background(), sha(tok))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := spy.Players.Save(context.Background(), store.Player{ID: sp.ID, X: tc.x, Z: tc.z, Version: sp.Version}); err != nil {
				t.Fatal(err)
			}
			r2 := newRigStore(t, 16, c, spy)
			c2 := r2.dial(hello(v, tok))
			_, x, z := welcomeSpawn(t, r2, c2)
			if x != tc.wantX || z != tc.wantZ {
				t.Fatalf("rejoined at %v,%v, want %v,%v", x, z, tc.wantX, tc.wantZ)
			}
		})
	}
}
