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
	walkSpeed  = 4.0 // m/s, PRD D-044; only used to steer back inside the circle
)

type bot struct {
	id    int
	cfg   config
	st    *stats
	rng   *rand.Rand
	self  uint16
	x, z  float64 // metres from the spawn point
	mu    sync.Mutex // guards sentP (written by play, read by the read goroutine)
	sentP map[uint32]time.Time
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
		if m, ok := decode(data); ok {
			switch m.typ {
			case protocol.MessageWelcome:
				b.self = m.playerID
				return conn, nil
			case protocol.MessageReload:
				conn.CloseNow()
				return nil, fmt.Errorf("server asked to reload: %s", m.text)
			}
		}
		// A Snapshot may race ahead of Welcome; keep waiting.
	}
}

// play sends inputs, chat and pings and reads everything the server sends.
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
	nextChat := time.Now().Add(time.Duration(20+b.rng.Intn(21)) * time.Second)
	nextPing := time.Now().Add(time.Duration(b.rng.Int63n(int64(pingEvery))))
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
			// Steer back toward the spawn once outside the circle.
			if now.After(turnAt) || math.Hypot(b.x, b.z) > b.cfg.radius {
				dirX, dirZ = b.pickDir()
				if math.Hypot(b.x, b.z) > b.cfg.radius {
					dirX, dirZ = -b.x, -b.z
					n := math.Hypot(dirX, dirZ)
					dirX, dirZ = dirX/n, dirZ/n
				}
				turnAt = now.Add(b.walkTime())
			}
			b.x += dirX * walkSpeed * inputEvery.Seconds()
			b.z += dirZ * walkSpeed * inputEvery.Seconds()
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
	m, ok := decode(data)
	if !ok {
		return
	}
	switch m.typ {
	case protocol.MessageSnapshot:
		b.st.snapshots.Add(1)
	case protocol.MessageChatMessage:
		b.st.chats.Add(1)
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
