package main

import (
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
