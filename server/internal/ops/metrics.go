package ops

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const tickWindow = 1200 // 60 s of ticks at 20 Hz

// Metrics collects the numbers served at /metrics. All methods are safe for
// concurrent use.
type Metrics struct {
	version string
	players atomic.Int64
	bytes   atomic.Uint64
	dropped atomic.Uint64

	mu        sync.Mutex
	ring      [tickWindow]time.Duration
	n         int // total recorded
	lastAt    time.Time
	lastBytes uint64
	lastDrop  uint64
	outKBps   float64
	dropPS    float64
	tickHz    float64
	tickCount uint64
	lastTicks uint64
}

// NewMetrics returns a collector reporting the given build version.
func NewMetrics(version string) *Metrics { return &Metrics{version: version} }

// SetPlayers sets the players-online gauge.
func (m *Metrics) SetPlayers(n int) { m.players.Store(int64(n)) }

// AddBytesOut counts bytes written to clients.
func (m *Metrics) AddBytesOut(n int) { m.bytes.Add(uint64(n)) }

// AddDropped counts frames dropped because a send queue was full.
func (m *Metrics) AddDropped(n int) { m.dropped.Add(uint64(n)) }

// RecordTick records how long one world tick took.
func (m *Metrics) RecordTick(d time.Duration) {
	m.mu.Lock()
	m.ring[m.n%tickWindow] = d
	m.n++
	m.tickCount++
	m.mu.Unlock()
}

// Sample recomputes the per-second rates from the counters. Call it about
// once a second with the current time.
func (m *Metrics) Sample(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, d := m.bytes.Load(), m.dropped.Load()
	if !m.lastAt.IsZero() {
		if dt := now.Sub(m.lastAt).Seconds(); dt > 0 {
			m.outKBps = float64(b-m.lastBytes) / 1024 / dt
			m.dropPS = float64(d-m.lastDrop) / dt
			m.tickHz = float64(m.tickCount-m.lastTicks) / dt
		}
	}
	m.lastAt, m.lastBytes, m.lastDrop, m.lastTicks = now, b, d, m.tickCount
}

// Snapshot is the /metrics JSON document.
type Snapshot struct {
	Version     string  `json:"version"`
	Players     int64   `json:"players"`
	TickHz      float64 `json:"tick_hz"`
	TickMsP50   float64 `json:"tick_ms_p50"`
	TickMsP99   float64 `json:"tick_ms_p99"`
	TickMsMax   float64 `json:"tick_ms_max"`
	OutKBPerS   float64 `json:"out_kb_per_s"`
	DroppedPerS float64 `json:"dropped_per_s"`
	HeapMB      float64 `json:"heap_mb"`
}

// Snapshot returns the current numbers.
func (m *Metrics) Snapshot() Snapshot {
	m.mu.Lock()
	cnt := m.n
	if cnt > tickWindow {
		cnt = tickWindow
	}
	w := make([]time.Duration, cnt)
	copy(w, m.ring[:cnt])
	s := Snapshot{Version: m.version, Players: m.players.Load(), TickHz: m.tickHz,
		OutKBPerS: m.outKBps, DroppedPerS: m.dropPS}
	m.mu.Unlock()

	if cnt > 0 {
		sort.Slice(w, func(i, j int) bool { return w[i] < w[j] })
		s.TickMsP50 = ms(w[(cnt-1)*50/100])
		s.TickMsP99 = ms(w[(cnt-1)*99/100])
		s.TickMsMax = ms(w[cnt-1])
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s.HeapMB = float64(mem.HeapAlloc) / (1024 * 1024)
	return s
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// Handler returns the HTTP mux serving /metrics and /healthz.
func (m *Metrics) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m.Snapshot())
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
