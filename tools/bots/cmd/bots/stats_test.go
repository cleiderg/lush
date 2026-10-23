package main

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/tools/bots/internal/protocol"
)

func TestPercentileMs(t *testing.T) {
	var d []time.Duration
	for i := 1; i <= 100; i++ {
		d = append(d, time.Duration(i)*time.Millisecond)
	}
	if got := percentileMs(d, 50); got != 50 {
		t.Errorf("p50 = %v", got)
	}
	if got := percentileMs(d, 99); got != 99 {
		t.Errorf("p99 = %v", got)
	}
	if got := percentileMs(nil, 99); got != 0 {
		t.Errorf("empty = %v", got)
	}
}

func TestMetricsURL(t *testing.T) {
	got, err := metricsURL("wss://staging.example.com/ws?x=1")
	if err != nil || got != "https://staging.example.com/metrics" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := metricsURL("http://x/ws"); err == nil {
		t.Error("want error for http scheme")
	}
}

func TestHelloRoundTrip(t *testing.T) {
	if _, ok := decode(encodeHello(), 0); !ok {
		t.Error("hello should decode")
	}
	if _, ok := decode([]byte{1, 2, 3}, 0); ok {
		t.Error("garbage should not decode")
	}
}

func TestSnapshotSelfPosition(t *testing.T) {
	b := flatbuffers.NewBuilder(128)
	protocol.SnapshotStartEntitiesVector(b, 2)
	protocol.CreateEntity(b, 9, -40, 70, 0, 0) // vectors are built back to front
	protocol.CreateEntity(b, 7, 125, -30, 0, 0)
	vec := b.EndVector(2)
	protocol.SnapshotStart(b)
	protocol.SnapshotAddEntities(b, vec)
	buf := finish(b, protocol.MessageSnapshot, protocol.SnapshotEnd(b))

	m, ok := decode(buf, 9)
	if !ok || !m.hasSelf || m.selfX != -40 || m.selfZ != 70 {
		t.Errorf("self 9: got %+v ok=%v", m, ok)
	}
	if m, _ := decode(buf, 3); m.hasSelf {
		t.Error("absent self should not be found")
	}

	bt := &bot{spawn: [2]float64{10, -5}}
	bt.setPos(m.selfX, m.selfZ)
	if x, z := bt.pos(); math.Abs(x-(-14)) > 1e-9 || math.Abs(z-12) > 1e-9 {
		t.Errorf("pos = %v, %v; want -14, 12", x, z)
	}
}

func TestSummaryFields(t *testing.T) {
	s := &summary{Server: json.RawMessage(`{"tick_ms_p99":3.5,"dropped_per_s":0}`)}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"connected_at_end", "failed_to_connect", "download_kb_per_s_per_player",
		"rtt_ms_p50", "rtt_ms_p99", "server"} {
		if _, ok := got[k]; !ok {
			t.Errorf("summary has no %q", k)
		}
	}
	var srv map[string]float64
	if err := json.Unmarshal(got["server"], &srv); err != nil || srv["tick_ms_p99"] != 3.5 {
		t.Errorf("server not copied verbatim: %s, %v", got["server"], err)
	}
}

func TestProblems(t *testing.T) {
	ok := summary{ConnectedAtEnd: 50, Server: json.RawMessage(`{}`)}
	if p := ok.problems(50); len(p) != 0 {
		t.Errorf("clean run: %v", p)
	}
	for name, s := range map[string]summary{
		"failed":  {ConnectedAtEnd: 50, FailedToConnect: 1, Server: json.RawMessage(`{}`)},
		"lost":    {ConnectedAtEnd: 49, Server: json.RawMessage(`{}`)},
		"dropped": {ConnectedAtEnd: 50, ServerMaxDroppedPerS: 0.5, Server: json.RawMessage(`{}`)},
		"metrics": {ConnectedAtEnd: 50},
	} {
		if p := s.problems(50); len(p) == 0 {
			t.Errorf("%s: no problem reported", name)
		}
	}
}
