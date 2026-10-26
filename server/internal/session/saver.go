package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
)

// DefaultSaveInterval is how often live players' positions are saved.
const DefaultSaveInterval = 5 * time.Second

// saveTimeout bounds one store write so a hung database cannot wedge the saver forever.
const saveTimeout = 10 * time.Second

// saveReq is a copy of what one save needs; it holds no world pointers.
type saveReq struct {
	storeID int64
	x, z    float32
	xp      int64
	ver     int64 // version the player was loaded with
}

// econReq is one economy outbox event ready for the store: the player's
// holdings right after it and its audit entries.
type econReq struct {
	storeID  int64
	holdings store.Holdings
	entries  []store.Entry
}

// saver writes player positions and economy events to the store on its own
// goroutine, so the tick never waits on the database. Saves are serialized,
// and each uses the version returned by that player's last successful save, so
// periodic, disconnect and shutdown saves never conflict with each other.
// Economy events are kept in a queue per player and written one ApplyEconomy
// each, in order; a failed one stays at the head of its queue, holding back
// the player's later events, until it is retried.
type saver struct {
	store store.Players
	log   *slog.Logger

	mu       sync.Mutex
	idle     *sync.Cond
	pending  map[int64]saveReq   // latest request per player, not yet written
	failed   map[int64]saveReq   // last write failed; retried at the next submit
	vers     map[int64]int64     // version from the last successful save
	econ     map[int64][]econReq // economy events per player, oldest first, not yet written
	econFail map[int64]bool      // the head of econ[id] failed; retried at the next submit
	inflight bool
	closed   bool

	wake chan struct{}
	ctx  context.Context
	stop context.CancelFunc
	done chan struct{}
}

func newSaver(players store.Players, log *slog.Logger) *saver {
	ctx, cancel := context.WithCancel(context.Background())
	s := &saver{store: players, log: log,
		pending: map[int64]saveReq{}, failed: map[int64]saveReq{}, vers: map[int64]int64{},
		econ: map[int64][]econReq{}, econFail: map[int64]bool{},
		wake: make(chan struct{}, 1), ctx: ctx, stop: cancel, done: make(chan struct{})}
	s.idle = sync.NewCond(&s.mu)
	go s.run()
	return s
}

// submit queues reqs (copying them, so the caller may reuse the slice) and
// re-queues earlier failures. A newer request for a player replaces an older
// one. It never blocks on the store.
func (s *saver) submit(reqs []saveReq) {
	s.mu.Lock()
	s.requeueFailedLocked()
	for _, r := range reqs {
		s.pending[r.storeID] = r
		delete(s.failed, r.storeID)
	}
	s.mu.Unlock()
	s.signal()
}

// submitEconomy queues economy events after the ones already queued for the
// same player, and re-queues earlier failures. It never blocks on the store.
func (s *saver) submitEconomy(reqs []econReq) {
	if len(reqs) == 0 {
		return
	}
	s.mu.Lock()
	s.requeueFailedLocked()
	for _, r := range reqs {
		s.econ[r.storeID] = append(s.econ[r.storeID], r)
	}
	s.mu.Unlock()
	s.signal()
}

func (s *saver) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// econReadyLocked reports whether any player has economy events that are not
// waiting for a retry.
func (s *saver) econReadyLocked() bool {
	for id := range s.econ {
		if !s.econFail[id] {
			return true
		}
	}
	return false
}

func (s *saver) requeueFailedLocked() {
	clear(s.econFail)
	for id, r := range s.failed {
		if _, newer := s.pending[id]; !newer {
			s.pending[id] = r
		}
		delete(s.failed, id)
	}
}

func (s *saver) run() {
	defer close(s.done)
	for {
		select {
		case <-s.wake:
		case <-s.ctx.Done():
			return
		}
		for s.drain() {
		}
	}
}

// drain writes one batch of pending saves. It reports whether there was work.
func (s *saver) drain() bool {
	s.mu.Lock()
	if len(s.pending) == 0 && !s.econReadyLocked() {
		s.inflight = false
		s.idle.Broadcast()
		s.mu.Unlock()
		return false
	}
	batch := make([]saveReq, 0, len(s.pending))
	for id, r := range s.pending {
		batch = append(batch, r)
		delete(s.pending, id)
	}
	econ := map[int64][]econReq{}
	for id, q := range s.econ {
		if !s.econFail[id] {
			econ[id] = q
			delete(s.econ, id)
		}
	}
	s.inflight = true
	s.mu.Unlock()

	for _, r := range batch {
		s.write(r)
	}
	for id, q := range econ {
		s.writeEconomy(id, q)
	}
	return true
}

// writeEconomy writes one player's economy events in order. At the first
// failure it puts that event and the rest back ahead of anything queued since,
// to be retried at the next submit.
func (s *saver) writeEconomy(id int64, q []econReq) {
	for i, r := range q {
		ctx, cancel := context.WithTimeout(s.ctx, saveTimeout)
		err := s.store.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: r.storeID, Holdings: r.holdings}}, r.entries)
		cancel()
		if err != nil {
			s.log.Error("apply economy", "store_id", id, "pending", len(q)-i, "err", err)
			s.mu.Lock()
			s.econ[id] = append(q[i:len(q):len(q)], s.econ[id]...)
			s.econFail[id] = true
			s.mu.Unlock()
			return
		}
	}
}

func (s *saver) write(r saveReq) {
	s.mu.Lock()
	ver := r.ver
	if v := s.vers[r.storeID]; v > ver {
		ver = v
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(s.ctx, saveTimeout)
	rec, err := s.store.Save(ctx, store.Player{ID: r.storeID, X: r.x, Z: r.z, XP: r.xp, Version: ver})
	cancel()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.log.Error("save player", "store_id", r.storeID, "err", err)
		if _, newer := s.pending[r.storeID]; !newer {
			s.failed[r.storeID] = r
		}
		return
	}
	s.vers[r.storeID] = rec.Version
}

// flush retries failed saves and economy writes, waits for everything queued
// to be written, and returns an error if any is still failing.
func (s *saver) flush(ctx context.Context) error {
	stopWatch := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.idle.Broadcast()
		s.mu.Unlock()
	})
	defer stopWatch()

	s.mu.Lock()
	s.requeueFailedLocked()
	s.mu.Unlock()
	s.signal()

	s.mu.Lock()
	defer s.mu.Unlock()
	for (len(s.pending) > 0 || s.econReadyLocked() || s.inflight) && ctx.Err() == nil && !s.closed {
		s.idle.Wait()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wait for saves: %w", err)
	}
	if n := len(s.failed); n > 0 {
		return fmt.Errorf("%d player saves failed", n)
	}
	if n := len(s.econFail); n > 0 {
		return fmt.Errorf("economy writes failed for %d players", n)
	}
	return nil
}

// close stops the goroutine after the write in progress, abandoning anything
// still queued; call flush first to keep it.
func (s *saver) close() {
	s.mu.Lock()
	s.closed = true
	s.idle.Broadcast()
	s.mu.Unlock()
	s.stop()
	<-s.done
}
