package session

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
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
	skills  store.SkillXP
	ver     int64 // version the player was loaded with
}

// econReq is one ApplyEconomy ready for the store: an economy outbox event
// (one player's holdings right after it and its audit entries) or a trade
// (both players' holdings and audit entries in one transaction).
type econReq struct {
	changes []store.EconomyChange
	entries []store.Entry
	// write, if set, writes the request instead of ApplyEconomy; changes
	// still name the players it waits behind.
	write func(ctx context.Context) error
	// done, if set, is told whether the write committed. A request with done
	// is never retried: a failure is final and does not hold back the
	// players' later writes. Called on the saver goroutine.
	done func(ok bool)
}

// econEvent is the econReq for one player's outbox event.
func econEvent(storeID int64, h store.Holdings, entries []store.Entry) econReq {
	return econReq{changes: []store.EconomyChange{{PlayerID: storeID, Holdings: h}}, entries: entries}
}

// blocks reports whether r names any player in blocked.
func (r *econReq) blocks(blocked map[int64]bool) bool {
	for _, c := range r.changes {
		if blocked[c.PlayerID] {
			return true
		}
	}
	return false
}

func (r *econReq) block(blocked map[int64]bool) {
	for _, c := range r.changes {
		blocked[c.PlayerID] = true
	}
}

// saver writes player positions and economy events to the store on its own
// goroutine, so the tick never waits on the database. Saves are serialized,
// and each uses the version returned by that player's last successful save, so
// periodic, disconnect and shutdown saves never conflict with each other.
// Economy writes are kept in one queue and written one ApplyEconomy each, in
// order for every player they name; a failed one stays queued, holding back
// the later writes of every player it names, until it is retried.
type saver struct {
	store store.Players
	log   *slog.Logger

	mu       sync.Mutex
	idle     *sync.Cond
	pending  map[int64]saveReq // latest request per player, not yet written
	failed   map[int64]saveReq // last write failed; retried at the next submit
	vers     map[int64]int64   // version from the last successful save
	econ     []econReq         // economy writes, oldest first, not yet written
	econFail map[int64]bool    // a write naming this player failed; retried at the next submit
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
		econFail: map[int64]bool{},
		wake:     make(chan struct{}, 1), ctx: ctx, stop: cancel, done: make(chan struct{})}
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

// submitEconomy queues economy writes after the ones already queued, and
// re-queues earlier failures. It never blocks on the store.
func (s *saver) submitEconomy(reqs ...econReq) {
	if len(reqs) == 0 {
		return
	}
	s.mu.Lock()
	s.requeueFailedLocked()
	s.econ = append(s.econ, reqs...)
	s.mu.Unlock()
	s.signal()
}

func (s *saver) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// takeReadyLocked removes and returns, in order, the queued economy writes
// that don't wait behind a failed or held-back write of a player they name.
// With take false it only reports whether there are any.
func (s *saver) takeReadyLocked(take bool) (ready []econReq, any bool) {
	blocked := maps.Clone(s.econFail)
	rest := s.econ[:0:0]
	for _, r := range s.econ {
		if r.blocks(blocked) {
			r.block(blocked)
			rest = append(rest, r)
			continue
		}
		if !take {
			return nil, true
		}
		ready = append(ready, r)
	}
	if take {
		s.econ = rest
	}
	return ready, len(ready) > 0
}

func (s *saver) econReadyLocked() bool {
	_, ok := s.takeReadyLocked(false)
	return ok
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
	econ, _ := s.takeReadyLocked(true)
	s.inflight = true
	s.mu.Unlock()

	for _, r := range batch {
		s.write(r)
	}
	s.writeEconomy(econ)
	return true
}

// writeEconomy writes economy writes in order. A failed write without a done
// callback, and every later one naming a player it names, goes back ahead of
// anything queued since, to be retried at the next submit. A failed write
// with a done callback is reported and dropped.
func (s *saver) writeEconomy(q []econReq) {
	failed := map[int64]bool{}
	var back []econReq
	for _, r := range q {
		if r.blocks(failed) {
			r.block(failed)
			back = append(back, r)
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, saveTimeout)
		var err error
		if r.write != nil {
			err = r.write(ctx)
		} else {
			err = s.store.ApplyEconomy(ctx, r.changes, r.entries)
		}
		cancel()
		if err != nil {
			s.log.Error("apply economy", "store_id", r.changes[0].PlayerID, "players", len(r.changes), "retry", r.done == nil, "err", err)
		}
		if r.done != nil {
			r.done(err == nil)
			continue
		}
		if err != nil {
			r.block(failed)
			back = append(back, r)
		}
	}
	if len(back) == 0 {
		return
	}
	s.mu.Lock()
	s.econ = append(back, s.econ...)
	maps.Copy(s.econFail, failed)
	s.mu.Unlock()
}

func (s *saver) write(r saveReq) {
	s.mu.Lock()
	ver := r.ver
	if v := s.vers[r.storeID]; v > ver {
		ver = v
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(s.ctx, saveTimeout)
	rec, err := s.store.Save(ctx, store.Player{ID: r.storeID, X: r.x, Z: r.z, XP: r.xp, Skills: r.skills, Version: ver})
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
