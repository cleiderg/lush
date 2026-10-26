package main

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// stats is shared by all bots.
type stats struct {
	bytes     atomic.Uint64 // bytes received
	snapshots atomic.Uint64
	chats     atomic.Uint64
	waves     atomic.Uint64 // wave emotes sent
	items     atomic.Uint64 // ItemActions sent
	invStates atomic.Uint64 // InventoryState messages received
	lost      atomic.Uint64 // bots that lost an established connection

	mu   sync.Mutex
	rtts []time.Duration
}

func (s *stats) addRTT(d time.Duration) {
	s.mu.Lock()
	s.rtts = append(s.rtts, d)
	s.mu.Unlock()
}

func (s *stats) rttPercentiles() (p50, p99 float64) {
	s.mu.Lock()
	r := append([]time.Duration(nil), s.rtts...)
	s.mu.Unlock()
	return percentileMs(r, 50), percentileMs(r, 99)
}

// percentileMs returns the p-th percentile (nearest rank) of d in milliseconds.
func percentileMs(d []time.Duration, p int) float64 {
	if len(d) == 0 {
		return 0
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	i := int(math.Ceil(float64(p)/100*float64(len(d)))) - 1
	return float64(d[max(i, 0)]) / float64(time.Millisecond)
}
