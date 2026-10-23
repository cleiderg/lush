package main

import (
	"testing"
	"time"
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
	if _, ok := decode(encodeHello()); !ok {
		t.Error("hello should decode")
	}
	if _, ok := decode([]byte{1, 2, 3}); ok {
		t.Error("garbage should not decode")
	}
}
