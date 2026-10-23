package ops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
	"github.com/getsentry/sentry-go"
)

type fakeTransport struct {
	mu        sync.Mutex
	events    []*sentry.Event
	flushed   bool
	flushedAt int
}

func (f *fakeTransport) Configure(sentry.ClientOptions) {}
func (f *fakeTransport) SendEvent(e *sentry.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}
func (f *fakeTransport) Flush(time.Duration) bool { return f.FlushWithContext(context.Background()) }
func (f *fakeTransport) FlushWithContext(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushed, f.flushedAt = true, len(f.events)
	return true
}
func (f *fakeTransport) Close() {}

const testDSN = "https://secretkey@sentry.invalid/1"

func TestNoDSNNoClient(t *testing.T) { // AC1
	cfg, err := LoadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	tr := &fakeTransport{}
	r, err := NewReporter(cfg, "x", tr)
	if err != nil || r != nil {
		t.Fatalf("want nil reporter, got %v, %v", r, err)
	}
	// Nil reporter is inert and passes the handler through.
	r.Recovered("boom")
	r.Flush(context.Background())
	h := slog.NewTextHandler(&strings.Builder{}, nil)
	if r.WrapHandler(h) != slog.Handler(h) {
		t.Fatal("handler wrapped without a DSN")
	}
	if len(tr.events) != 0 || tr.flushed {
		t.Fatal("transport used without a DSN")
	}
}

func TestReporterCapturesPanicAndErrorLog(t *testing.T) { // AC2 (ops half)
	cfg, _ := LoadConfig(func(k string) string {
		return map[string]string{"LUSH_SENTRY_DSN": testDSN, "LUSH_ENV": "staging"}[k]
	})
	tr := &fakeTransport{}
	r, err := NewReporter(cfg, "v1", tr)
	if err != nil || r == nil {
		t.Fatal(err)
	}
	log := slog.New(r.WrapHandler(slog.NewTextHandler(&strings.Builder{}, nil)))
	log.Warn("ignored")
	log.With("a", 1).Error("db down", "err", "x")
	func() {
		defer func() { r.Recovered(recover()) }()
		panic("kaboom")
	}()
	if len(tr.events) != 2 {
		t.Fatalf("want 2 events, got %d", len(tr.events))
	}
	for _, e := range tr.events {
		if e.Environment != "staging" {
			t.Errorf("environment %q", e.Environment)
		}
	}
	if tr.events[0].Message != "db down" || tr.events[0].Tags["a"] != "1" {
		t.Errorf("log event %+v", tr.events[0])
	}
}

func TestShutdownFlushes(t *testing.T) { // AC3
	cfg := Config{SentryDSN: testDSN, Env: "production"}
	tr := &fakeTransport{}
	r, _ := NewReporter(cfg, "v", tr)
	r.hub.CaptureMessage("pending")
	err := GracefulShutdown(context.Background(), world.NewFakeClock(time.Unix(0, 0)), 0, ShutdownHooks{
		Save:  func(context.Context) error { return fmt.Errorf("save failed") },
		Flush: r.Flush,
	})
	if err == nil {
		t.Fatal("save error lost")
	}
	if !tr.flushed || tr.flushedAt != 1 {
		t.Fatalf("not flushed: %+v", tr)
	}
}

func TestConfigRedactsSecrets(t *testing.T) { // AC4
	cfg, _ := LoadConfig(func(k string) string {
		return map[string]string{"LUSH_SENTRY_DSN": testDSN, "LUSH_DATABASE_URL": "postgres://u:hunter2@h/db"}[k]
	})
	var sb strings.Builder
	slog.New(slog.NewJSONHandler(&sb, nil)).Info("cfg", "config", cfg)
	for _, s := range []string{cfg.String(), fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), fmt.Sprint(&cfg), sb.String()} {
		if strings.Contains(s, "secretkey") || strings.Contains(s, "sentry.invalid") || strings.Contains(s, "hunter2") {
			t.Fatalf("secret leaked: %s", s)
		}
	}
}
