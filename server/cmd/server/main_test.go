package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

// syncBuffer lets the test read logs while run's goroutines write them.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitTickers(t *testing.T, c *world.FakeClock, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.TickerCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d tickers, have %d", n, c.TickerCount())
		}
		time.Sleep(time.Millisecond)
	}
}

// TestRunShutdown drives run end to end on a fake clock: SIGTERM (ctx
// cancel) starts the countdown, the save hook runs after it, and run returns
// exit code 0. Every log line is JSON and the startup line carries the version.
func TestRunShutdown(t *testing.T) {
	old := version
	version = "test-1.2.3"
	defer func() { version = old }()

	env := map[string]string{"LUSH_LISTEN_ADDR": "127.0.0.1:0"}
	clock := world.NewFakeClock(time.Unix(0, 0))
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, clock, out) }()

	waitTickers(t, clock, 2) // tick loop + metrics sampler
	cancel()
	waitTickers(t, clock, 3) // countdown ticker
	clock.Advance(4 * time.Second)
	select {
	case code := <-done:
		t.Fatalf("exited with %d before the 5 s development countdown elapsed", code)
	default:
	}
	clock.Advance(time.Second)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code %d, want 0\n%s", code, out)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("run did not return after the countdown\n%s", out)
	}

	var msgs []string
	var countdown []float64
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		msg, _ := rec["msg"].(string)
		msgs = append(msgs, msg)
		switch msg {
		case "starting":
			if rec["version"] != "test-1.2.3" {
				t.Errorf("startup log version = %v", rec["version"])
			}
		case "restart countdown":
			countdown = append(countdown, rec["seconds"].(float64))
		}
	}
	want := []string{"starting", "shutdown requested", "restart countdown", "restart countdown",
		"restart countdown", "restart countdown", "restart countdown", "saving players", "bye"}
	if strings.Join(msgs, ",") != strings.Join(want, ",") {
		t.Errorf("log messages %v, want %v", msgs, want)
	}
	if len(countdown) != 5 || countdown[0] != 5 || countdown[4] != 1 {
		t.Errorf("countdown %v, want 5..1", countdown)
	}
}

func TestRunBadConfig(t *testing.T) {
	env := map[string]string{"LUSH_TICK_RATE": "fast"}
	code := run(context.Background(), func(k string) string { return env[k] }, world.NewFakeClock(time.Unix(0, 0)), &syncBuffer{})
	if code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
}

// TestRunFlushesSentryOnExit: an error logged on the way out (here the HTTP
// listener failing) reaches Sentry before run returns and main exits.
func TestRunFlushesSentryOnExit(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	sentrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
	}))
	defer sentrySrv.Close()
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	env := map[string]string{
		"LUSH_LISTEN_ADDR": busy.Addr().String(),
		"LUSH_SENTRY_DSN":  strings.Replace(sentrySrv.URL, "http://", "http://key@", 1) + "/1",
	}
	code := run(context.Background(), func(k string) string { return env[k] }, world.NewFakeClock(time.Unix(0, 0)), &syncBuffer{})
	if code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || !strings.Contains(bodies[0], "http server failed") {
		t.Fatalf("sentry got %q, want the http server failure", bodies)
	}
}
