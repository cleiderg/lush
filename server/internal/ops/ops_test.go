package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Config
		err  bool
	}{
		{"defaults", nil, Config{":8080", 20, "", "development", 5 * time.Second, 5, 0, 0}, false},
		{"production default countdown", map[string]string{"LUSH_ENV": "production"}, Config{":8080", 20, "", "production", 60 * time.Second, 5, 0, 0}, false},
		{"overrides", map[string]string{"LUSH_LISTEN_ADDR": ":9", "LUSH_TICK_RATE": "10", "LUSH_DATABASE_URL": "x", "LUSH_ENV": "production", "LUSH_SHUTDOWN_COUNTDOWN": "7s"},
			Config{":9", 10, "x", "production", 7 * time.Second, 5, 0, 0}, false},
		{"bad tick", map[string]string{"LUSH_TICK_RATE": "0"}, Config{}, true},
		{"bad countdown", map[string]string{"LUSH_SHUTDOWN_COUNTDOWN": "soon"}, Config{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LoadConfig(env(tc.env))
			if (err != nil) != tc.err {
				t.Fatalf("err = %v", err)
			}
			if !tc.err && got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestEndpoints(t *testing.T) {
	m := NewMetrics("v1.2.3")
	m.SetPlayers(3)
	for i := 1; i <= 100; i++ {
		m.RecordTick(time.Duration(i) * time.Millisecond)
	}
	m.Sample(time.Unix(0, 0))
	m.AddBytesOut(2048)
	m.AddDropped(4)
	m.Sample(time.Unix(2, 0))
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", resp, err)
	}
	resp.Body.Close()

	resp, err = http.Get(srv.URL + "/metrics")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("metrics: %v %v", resp, err)
	}
	defer resp.Body.Close()
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"players", "tick_hz", "tick_ms_p50", "tick_ms_p99", "tick_ms_max", "out_kb_per_s", "dropped_per_s", "heap_mb", "version"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if raw["version"] != "v1.2.3" || raw["players"].(float64) != 3 || raw["tick_ms_max"].(float64) != 100 ||
		raw["tick_ms_p50"].(float64) != 50 || raw["out_kb_per_s"].(float64) != 1 || raw["dropped_per_s"].(float64) != 2 {
		t.Errorf("unexpected values: %v", raw)
	}
}

func TestGracefulShutdownOrder(t *testing.T) {
	clock := world.NewFakeClock(time.Unix(0, 0))
	var events []string
	var announced []int
	done := make(chan error, 1)
	go func() {
		done <- GracefulShutdown(context.Background(), clock, 5*time.Second, ShutdownHooks{
			Announce: func(r time.Duration) {
				events = append(events, "announce")
				announced = append(announced, int(r.Seconds()))
			},
			Save: func(context.Context) error { events = append(events, "save"); return nil },
		})
	}()
	for clock.TickerCount() == 0 {
		time.Sleep(time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("returned before countdown elapsed")
	default:
	}
	clock.Advance(5 * time.Second)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(announced, []int{5, 4, 3, 2, 1}) {
		t.Errorf("announced %v", announced)
	}
	if events[len(events)-1] != "save" || len(events) != 6 {
		t.Errorf("events %v", events)
	}
}
