package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/tools/bots/internal/protocol"
)

const (
	inputEvery = 100 * time.Millisecond // 10 Hz
	pingEvery  = 2 * time.Second
)

type bot struct {
	id    int
	cfg   config
	st    *stats
	rng   *rand.Rand
	self  uint16
	spawn [2]float64 // from Welcome, in metres
	mu    sync.Mutex // guards x, z, sentP and inv (shared by play and the read goroutine)
	x, z  float64    // metres from the spawn point, from the latest Snapshot
	sentP map[uint32]time.Time
	inv   inventory // from the latest InventoryState
}

// setPos records the server's position for this bot (Snapshot decimetres).
func (b *bot) setPos(xdm, zdm int16) {
	b.mu.Lock()
	b.x, b.z = float64(xdm)/10-b.spawn[0], float64(zdm)/10-b.spawn[1]
	b.mu.Unlock()
}

func (b *bot) pos() (float64, float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.x, b.z
}

func (b *bot) setInventory(inv inventory) {
	b.mu.Lock()
	b.inv = inv
	b.mu.Unlock()
}

func (b *bot) inventory() inventory {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inv
}

func (b *bot) notePing(tok uint32, t time.Time) {
	b.mu.Lock()
	b.sentP[tok] = t
	b.mu.Unlock()
}

func (b *bot) takePing(tok uint32) (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.sentP[tok]
	delete(b.sentP, tok)
	return t, ok
}

func newBot(id int, cfg config, st *stats) *bot {
	return &bot{id: id, cfg: cfg, st: st, rng: rand.New(rand.NewSource(cfg.seed + int64(id))),
		sentP: map[uint32]time.Time{}}
}

// run connects, plays until ctx ends, and keeps the live/failed counters.
// settled is called exactly once, when the bot has connected or failed.
func (b *bot) run(ctx context.Context, live, failed *atomic.Int64, settled func()) {
	conn, err := b.connect(ctx)
	if err != nil {
		if ctx.Err() == nil {
			failed.Add(1)
		}
		settled()
		return
	}
	defer conn.CloseNow()
	live.Add(1)
	settled()

	_ = b.play(ctx, conn)
	if ctx.Err() == nil {
		live.Add(-1)
		b.st.lost.Add(1)
	}
}

func (b *bot) connect(ctx context.Context) (*websocket.Conn, error) {
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dctx, b.cfg.url, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(1 << 20)
	if err := conn.Write(dctx, websocket.MessageBinary, encodeHello()); err != nil {
		conn.CloseNow()
		return nil, err
	}
	for {
		_, data, err := conn.Read(dctx)
		if err != nil {
			conn.CloseNow()
			return nil, err
		}
		b.st.bytes.Add(uint64(len(data)))
		if m, ok := decode(data, 0); ok {
			switch m.typ {
			case protocol.MessageWelcome:
				b.self, b.spawn = m.playerID, m.spawn
				return conn, nil
			case protocol.MessageReload:
				conn.CloseNow()
				return nil, fmt.Errorf("server asked to reload: %s", m.text)
			}
		}
		// A Snapshot may race ahead of Welcome; keep waiting.
	}
}

// play sends inputs, chat, waves, item actions and pings and reads everything the server sends.
func (b *bot) play(ctx context.Context, conn *websocket.Conn) error {
	readErr := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				readErr <- err
				return
			}
			b.st.bytes.Add(uint64(len(data)))
			b.handle(data)
		}
	}()

	var seq, pingTok uint32
	dirX, dirZ := b.pickDir()
	turnAt := time.Now().Add(b.walkTime())
	// The first chat comes within 20 s so short runs (make smoke) see chat too.
	nextChat := time.Now().Add(time.Duration(b.rng.Int63n(int64(20 * time.Second))))
	nextPing := time.Now().Add(time.Duration(b.rng.Int63n(int64(pingEvery))))
	waves := newSchedule(time.Now(), b.cfg.waveEvery, b.cfg.waveFirst, b.rng)
	items := &itemCycle{sched: newSchedule(time.Now(), b.cfg.itemEvery, b.cfg.itemFirst, b.rng)}
	tick := time.NewTicker(inputEvery)
	defer tick.Stop()
	bld := flatbuffers.NewBuilder(128)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case now := <-tick.C:
			// Steer back toward the spawn once outside the circle. The
			// position is the server's, so speed and walls can't make it drift.
			x, z := b.pos()
			if now.After(turnAt) || math.Hypot(x, z) > b.cfg.radius {
				dirX, dirZ = b.pickDir()
				if n := math.Hypot(x, z); n > b.cfg.radius {
					dirX, dirZ = -x/n, -z/n
				}
				turnAt = now.Add(b.walkTime())
			}
			seq++
			if err := conn.Write(ctx, websocket.MessageBinary, encodeInput(bld, seq, dirX, dirZ)); err != nil {
				return err
			}
			if now.After(nextChat) {
				nextChat = now.Add(time.Duration(20+b.rng.Intn(21)) * time.Second)
				msg := fmt.Sprintf("bot %d says hi", b.id)
				if err := conn.Write(ctx, websocket.MessageBinary, encodeChat(bld, msg)); err != nil {
					return err
				}
			}
			if waves.due(now) {
				waves.done(now)
				if err := conn.Write(ctx, websocket.MessageBinary, encodeWave(bld)); err != nil {
					return err
				}
				b.st.waves.Add(1)
			}
			if a, ok := items.step(now, b.inventory()); ok {
				if err := conn.Write(ctx, websocket.MessageBinary, encodeItemAction(bld, a)); err != nil {
					return err
				}
				b.st.items.Add(1)
			}
			if now.After(nextPing) {
				nextPing = now.Add(pingEvery)
				pingTok++
				b.notePing(pingTok, now)
				if err := conn.Write(ctx, websocket.MessageBinary, encodePing(bld, pingTok)); err != nil {
					return err
				}
			}
		}
	}
}

// handle runs on the read goroutine.
func (b *bot) handle(data []byte) {
	m, ok := decode(data, b.self)
	if !ok {
		return
	}
	switch m.typ {
	case protocol.MessageSnapshot:
		b.st.snapshots.Add(1)
		if m.hasSelf {
			b.setPos(m.selfX, m.selfZ)
		}
	case protocol.MessageChatMessage:
		b.st.chats.Add(1)
	case protocol.MessageInventoryState:
		b.st.invStates.Add(1)
		b.setInventory(m.inv)
	case protocol.MessagePong:
		if t, ok := b.takePing(m.token); ok {
			b.st.addRTT(time.Since(t))
		}
	}
}

func (b *bot) pickDir() (float64, float64) {
	a := b.rng.Float64() * 2 * math.Pi
	return math.Cos(a), math.Sin(a)
}

func (b *bot) walkTime() time.Duration {
	return time.Duration(2+b.rng.Intn(4)) * time.Second
}
