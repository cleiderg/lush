package world

import (
	"sync"
	"time"
)

// Clock abstracts time so the tick loop is deterministic under test.
// Nothing in world/ reads the wall clock directly.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker mirrors time.Ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// FakeClock is a manually advanced Clock for tests.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

type fakeTicker struct {
	c        chan time.Time
	period   time.Duration
	next     time.Time
	stopOnce sync.Once
	done     chan struct{}
}

// NewFakeClock returns a FakeClock starting at start.
func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{now: start} }

func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *FakeClock) NewTicker(d time.Duration) Ticker {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTicker{c: make(chan time.Time), period: d, next: f.now.Add(d), done: make(chan struct{})}
	f.tickers = append(f.tickers, t)
	return t
}

// Advance moves time forward, delivering each ticker firing in order. Delivery
// blocks until the receiver takes the tick (or the ticker is stopped).
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	target := f.now.Add(d)
	f.mu.Unlock()
	for {
		f.mu.Lock()
		var next *fakeTicker
		for _, t := range f.tickers {
			if !t.isStopped() && !t.next.After(target) && (next == nil || t.next.Before(next.next)) {
				next = t
			}
		}
		if next == nil {
			f.now = target
			f.mu.Unlock()
			return
		}
		at := next.next
		next.next = at.Add(next.period)
		f.now = at
		f.mu.Unlock()
		select {
		case next.c <- at:
		case <-next.done:
		}
	}
}

func (t *fakeTicker) C() <-chan time.Time { return t.c }

func (t *fakeTicker) Stop() {
	t.stopOnce.Do(func() { close(t.done) })
}

// TickerCount reports how many tickers have been created (test synchronisation).
func (f *FakeClock) TickerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tickers)
}

func (t *fakeTicker) isStopped() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}
