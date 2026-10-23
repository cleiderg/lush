package session_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/cleiderg/lush/server/internal/ops"
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
)

type fakeTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (f *fakeTransport) Configure(sentry.ClientOptions) {}
func (f *fakeTransport) SendEvent(e *sentry.Event) {
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
}
func (f *fakeTransport) Flush(time.Duration) bool              { return true }
func (f *fakeTransport) FlushWithContext(context.Context) bool { return true }
func (f *fakeTransport) Close()                                {}
func (f *fakeTransport) snapshot() []*sentry.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*sentry.Event(nil), f.events...)
}

// panicStore panics when a known token is looked up.
type panicStore struct{ store.Players }

func (panicStore) LoadByTokenHash(context.Context, string) (store.Player, error) {
	panic("store exploded")
}

func TestSessionPanicAndErrorLogReportedServerKeepsServing(t *testing.T) { // AC2
	tr := &fakeTransport{}
	rep, err := ops.NewReporter(ops.Config{SentryDSN: "https://k@sentry.invalid/1", Env: "staging"}, "t", tr)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(rep.WrapHandler(slog.NewTextHandler(io.Discard, nil)))
	r := newRigLog(t, 16, session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, OnPanic: rep.Recovered},
		panicStore{store.NewMemory()}, log)

	// Hello with a token hits the panicking store on the connection goroutine.
	bad := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), "some-token"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := bad.c.Read(ctx); err == nil {
		t.Fatal("panicking connection stayed open")
	}
	log.Error("something broke", "k", "v")

	evs := tr.snapshot()
	if len(evs) != 2 {
		t.Fatalf("want 2 events (panic, error log), got %d", len(evs))
	}
	for _, e := range evs {
		if e.Environment != "staging" {
			t.Errorf("environment %q", e.Environment)
		}
	}

	// A fresh guest (no token) on the same server still gets a Welcome.
	good := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	r.w.Tick()
	if typ, _ := r.next(good); typ != protocol.MessageWelcome {
		t.Fatalf("got %v, want Welcome", typ)
	}
}
