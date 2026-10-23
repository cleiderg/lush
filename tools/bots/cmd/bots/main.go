// Command bots connects many simulated players to a Lush server and writes a
// JSON summary. See tools/bots/README.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var (
		wsURL    = flag.String("url", "ws://127.0.0.1:8080/ws", "server WebSocket URL")
		count    = flag.Int("bots", 50, "number of bots")
		steady   = flag.Duration("duration", 20*time.Second, "steady period after all bots are connected")
		ramp     = flag.Duration("ramp", 5*time.Second, "time over which connections are spread")
		radius   = flag.Float64("radius", 25, "bots wander within this many metres of the spawn point")
		out      = flag.String("out", "", "write the JSON summary here (default stdout)")
		metrics  = flag.String("metrics", "", "metrics URL (default derived from -url)")
		strict   = flag.Bool("strict", false, "exit 1 on any failed connection, lost connection or dropped frame")
		seedFlag = flag.Int64("seed", 0, "random seed (0 = time)")
	)
	flag.Parse()
	if *count <= 0 || *radius <= 0 {
		fmt.Fprintln(os.Stderr, "bots: -bots and -radius must be positive")
		os.Exit(2)
	}
	mURL := *metrics
	if mURL == "" {
		var err error
		if mURL, err = metricsURL(*wsURL); err != nil {
			fmt.Fprintln(os.Stderr, "bots:", err)
			os.Exit(2)
		}
	}
	seed := *seedFlag
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	sum := run(ctx, config{url: *wsURL, metricsURL: mURL, bots: *count, steady: *steady,
		ramp: *ramp, radius: *radius, seed: seed})

	data, _ := json.MarshalIndent(sum, "", "  ")
	data = append(data, '\n')
	if *out == "" {
		os.Stdout.Write(data)
	} else if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "bots:", err)
		os.Exit(1)
	}
	if problems := sum.problems(*count); len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "bots: "+strings.Join(problems, "; "))
		if *strict {
			os.Exit(1)
		}
	}
}

type config struct {
	url, metricsURL string
	bots            int
	steady, ramp    time.Duration
	radius          float64
	seed            int64
}

// summary is the JSON document the bots write. CI reads these field names.
type summary struct {
	Bots                    int             `json:"bots"`
	ConnectedAtEnd          int             `json:"connected_at_end"`
	FailedToConnect         int             `json:"failed_to_connect"`
	DisconnectedEarly       int             `json:"disconnected_early"`
	DownloadKBPerSPerPlayer float64         `json:"download_kb_per_s_per_player"`
	RTTMsP50                float64         `json:"rtt_ms_p50"`
	RTTMsP99                float64         `json:"rtt_ms_p99"`
	SteadySeconds           float64         `json:"steady_seconds"`
	SnapshotsReceived       uint64          `json:"snapshots_received"`
	ChatsReceived           uint64          `json:"chats_received"`
	ServerMaxDroppedPerS    float64         `json:"server_max_dropped_per_s"`
	Server                  json.RawMessage `json:"server"`
}

func (s *summary) problems(want int) []string {
	var p []string
	if s.FailedToConnect > 0 {
		p = append(p, fmt.Sprintf("%d bot(s) failed to connect", s.FailedToConnect))
	}
	if s.ConnectedAtEnd < want {
		p = append(p, fmt.Sprintf("only %d of %d bots connected at end", s.ConnectedAtEnd, want))
	}
	if s.ServerMaxDroppedPerS > 0 {
		p = append(p, fmt.Sprintf("server dropped frames (max %.2f/s)", s.ServerMaxDroppedPerS))
	}
	if len(s.Server) == 0 {
		p = append(p, "could not read server /metrics")
	}
	return p
}

func run(ctx context.Context, cfg config) *summary {
	botCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	st := &stats{}
	var wg sync.WaitGroup
	var live, failed atomic.Int64
	var settled sync.WaitGroup // every bot has either connected or failed
	settled.Add(cfg.bots)
	for i := 0; i < cfg.bots; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			delay := time.Duration(int64(cfg.ramp) * int64(i) / int64(cfg.bots))
			select {
			case <-time.After(delay):
			case <-botCtx.Done():
				settled.Done()
				return
			}
			newBot(i, cfg, st).run(botCtx, &live, &failed, settled.Done)
		}(i)
	}
	settled.Wait()

	// Steady period: the byte counters only cover this window.
	var bytes0, snaps0 uint64
	var steadySecs float64
	var maxDrop float64
	if botCtx.Err() == nil {
		bytes0, snaps0 = st.bytes.Load(), st.snapshots.Load()
		start := time.Now()
		tick := time.NewTicker(time.Second)
		end := time.After(cfg.steady)
	loop:
		for {
			select {
			case <-tick.C:
				if m := fetchMetrics(cfg.metricsURL); m != nil && m.DroppedPerS > maxDrop {
					maxDrop = m.DroppedPerS
				}
			case <-end:
				break loop
			case <-botCtx.Done():
				break loop
			}
		}
		tick.Stop()
		steadySecs = time.Since(start).Seconds()
	}
	bytes1, snaps1 := st.bytes.Load(), st.snapshots.Load()
	connected := int(live.Load())
	raw, m := fetchMetricsRaw(cfg.metricsURL)
	if m != nil && m.DroppedPerS > maxDrop {
		maxDrop = m.DroppedPerS
	}
	cancel()
	wg.Wait()

	sum := &summary{
		Bots: cfg.bots, ConnectedAtEnd: connected, FailedToConnect: int(failed.Load()),
		DisconnectedEarly: int(st.lost.Load()), SteadySeconds: steadySecs,
		SnapshotsReceived: snaps1 - snaps0, ChatsReceived: st.chats.Load(),
		ServerMaxDroppedPerS: maxDrop, Server: raw,
	}
	if steadySecs > 0 && connected > 0 {
		sum.DownloadKBPerSPerPlayer = float64(bytes1-bytes0) / 1024 / steadySecs / float64(connected)
	}
	sum.RTTMsP50, sum.RTTMsP99 = st.rttPercentiles()
	return sum
}

// serverMetrics is the part of /metrics the bots look at.
type serverMetrics struct {
	DroppedPerS float64 `json:"dropped_per_s"`
}

func fetchMetrics(u string) *serverMetrics {
	_, m := fetchMetricsRaw(u)
	return m
}

// fetchMetricsRaw returns the /metrics body verbatim (nil on error) and the
// decoded fields the bots use.
func fetchMetricsRaw(u string) (json.RawMessage, *serverMetrics) {
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(u)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var m serverMetrics
	if json.Unmarshal(body, &m) != nil {
		return nil, nil
	}
	return json.RawMessage(strings.TrimSpace(string(body))), &m
}

// metricsURL turns ws(s)://host/ws into http(s)://host/metrics.
func metricsURL(ws string) (string, error) {
	u, err := url.Parse(ws)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	default:
		return "", fmt.Errorf("-url must be ws:// or wss://, got %q", ws)
	}
	u.Path, u.RawQuery = "/metrics", ""
	return u.String(), nil
}
