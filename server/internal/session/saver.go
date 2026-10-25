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

// saver writes player positions to the store on its own goroutine, so the
// tick never waits on the database. Saves are serialized, and each uses the
// version returned by that player's last successful save, so periodic,
// disconnect and shutdown saves never conflict with each other.
type saver struct {
	store store.Players
	log   *slog.Logger

	mu       sync.Mutex
	idle     *sync.Cond
	pending  map[int64]saveReq // latest request per player, not yet written
	failed   map[int64]saveReq // last write failed; retried at the next submit
	vers     map[int64]int64   // version from the last successful save
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
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *saver) requeueFailedLocked() {
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
	if len(s.pending) == 0 {
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
	s.inflight = true
	s.mu.Unlock()

	for _, r := range batch {
		s.write(r)
	}
	return true
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

// flush retries failed saves, waits for everything queued to be written, and
// returns an error if any save is still failing.
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
	select {
	case s.wake <- struct{}{}:
	default:
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for (len(s.pending) > 0 || s.inflight) && ctx.Err() == nil && !s.closed {
		s.idle.Wait()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wait for saves: %w", err)
	}
	if n := len(s.failed); n > 0 {
		return fmt.Errorf("%d player saves failed", n)
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
