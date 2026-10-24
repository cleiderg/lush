// Package session turns transport connections into players: the Hello/Welcome
// handshake, guest identity, inbound messages as world commands, and the
// per-tick snapshots going back out.
//
// Connection goroutines never touch world state. They enqueue commands on the
// world and queue join/leave operations; the Server's System hooks apply them
// on the tick goroutine.
package session

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sync"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/interest"
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/chat"
	"github.com/cleiderg/lush/server/internal/systems/emote"
	"github.com/cleiderg/lush/server/internal/transport"
	"github.com/cleiderg/lush/server/internal/world"
)

// Config holds session tuning.
type Config struct {
	TickRate     int
	Spawn        world.Vec2    // where new guests appear
	MoveSpeed    float32       // world units per second at full stick; must match the movement system
	HelloTimeout time.Duration // how long a connection may stay silent before Hello
	// MaxMsgPerSec disconnects clients that send faster than this (0 = 120).
	MaxMsgPerSec int
	// MaxGuests caps session ids (0 = 65535, the wire limit). When every id is
	// taken, the guest that has been offline longest loses its session id and
	// the id is reused; that player gets a new id on its next Hello.
	MaxGuests int
	// SaveInterval is how often live players' positions are saved
	// (0 = DefaultSaveInterval).
	SaveInterval time.Duration
	// OnPanic, if set, is told about a panic recovered in a connection
	// goroutine (for error reporting). The connection ends; others go on.
	OnPanic func(recovered any)
}

// InputCommand is a client Input as a world command.
type InputCommand struct {
	Player       world.PlayerID
	Seq          uint32
	MoveX, MoveZ float32
	Facing       float32

	conn transport.Conn // inputs from a replaced connection are ignored
}

func (c InputCommand) Target() world.PlayerID { return c.Player }

type guest struct {
	// Guarded by Server.mu.
	id      world.PlayerID
	storeID int64
	ver     int64         // store version at load; immutable
	pos     world.Vec2    // last position, restored on resume
	conns   int           // connections that completed the handshake and have not ended
	idle    *list.Element // in Server.idle while conns == 0

	// Tick-goroutine only.
	conn    transport.Conn
	lastSeq uint32
}

type op struct {
	join  bool
	g     *guest
	conn  transport.Conn
	token string // raw token, only carried to Welcome
}

// Server is the session layer. It implements world.System, chat.Sink and
// transport.Handler (via ServeConn).
type Server struct {
	cfg   Config
	w     *world.World
	log   *slog.Logger
	clock world.Clock
	store store.Players

	mu     sync.Mutex
	guests map[int64]*guest // by store id; session ids for known players
	nextID uint32
	idle   *list.List // offline guests, longest offline first
	ops    []op
	conns  map[transport.Conn]struct{}

	saver *saver

	// Tick-goroutine only.
	live     map[world.PlayerID]*guest
	nextSave time.Time
	saves    []saveReq
	grid     *interest.Grid
	hits     []interest.Hit
	ents     []*world.Player
	bld      *flatbuffers.Builder
}

// New creates the Server. Register it on w after the movement, emote and chat systems.
func New(cfg Config, w *world.World, clock world.Clock, players store.Players, log *slog.Logger) *Server {
	if cfg.HelloTimeout <= 0 {
		cfg.HelloTimeout = 10 * time.Second
	}
	if cfg.MaxMsgPerSec <= 0 {
		cfg.MaxMsgPerSec = 120
	}
	if cfg.MaxGuests <= 0 || cfg.MaxGuests > math.MaxUint16 {
		cfg.MaxGuests = math.MaxUint16
	}
	if cfg.TickRate <= 0 {
		cfg.TickRate = int(time.Second / w.TickInterval())
	}
	if cfg.SaveInterval <= 0 {
		cfg.SaveInterval = DefaultSaveInterval
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{saver: newSaver(players, log), cfg: cfg, w: w, log: log, clock: clock, store: players,
		guests: map[int64]*guest{}, idle: list.New(), conns: map[transport.Conn]struct{}{},
		live: map[world.PlayerID]*guest{}, grid: interest.NewGrid(), bld: flatbuffers.NewBuilder(1024)}
}

// ---- connection goroutines ----

// ServeConn serves one connection until it ends. It is a transport.Handler.
func (s *Server) ServeConn(c transport.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { // end the read loop when the transport ends the connection
		select {
		case <-c.Done():
			cancel()
		case <-ctx.Done():
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			s.log.Warn("panic in session goroutine", "panic", fmt.Sprint(r))
			if s.cfg.OnPanic != nil {
				s.cfg.OnPanic(r)
			}
		}
	}()

	g := s.handshake(ctx, c)
	if g == nil {
		return
	}
	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.ops = append(s.ops, op{g: g, conn: c})
		if g.conns--; g.conns == 0 {
			g.idle = s.idle.PushBack(g)
		}
		s.mu.Unlock()
	}()

	windowStart, count := time.Now(), 0
	for {
		buf, err := c.Recv(ctx)
		if err != nil {
			return
		}
		if now := time.Now(); now.Sub(windowStart) >= time.Second {
			windowStart, count = now, 0
		}
		if count++; count > s.cfg.MaxMsgPerSec {
			s.log.Warn("client too chatty, disconnecting", "player", g.id)
			return
		}
		m, err := decode(buf)
		if err != nil {
			s.log.Debug("bad frame", "player", g.id, "err", err)
			return
		}
		switch m.typ {
		case protocol.MessageInput:
			s.w.Enqueue(InputCommand{Player: g.id, Seq: m.seq,
				MoveX: m.moveX, MoveZ: m.moveZ, Facing: m.facing, conn: c})
		case protocol.MessageChatSend:
			s.w.Enqueue(chat.SendCommand{Player: g.id, Text: m.chatText})
		case protocol.MessageEmoteSend:
			// Unknown emote values are dropped, not fatal.
			if m.emote == protocol.EmoteWave {
				s.w.Enqueue(emote.WaveCommand{Player: g.id})
			}
		case protocol.MessagePing:
			c.Send(encodePong(m.pingToken))
		}
	}
}

// handshake reads Hello, checks the protocol version, resolves the guest and
// sends Welcome. It returns nil if the connection must end.
func (s *Server) handshake(ctx context.Context, c transport.Conn) *guest {
	hctx, cancel := context.WithTimeout(ctx, s.cfg.HelloTimeout)
	defer cancel()
	buf, err := c.Recv(hctx)
	if err != nil {
		return nil
	}
	m, err := decode(buf)
	if err != nil || m.typ != protocol.MessageHello {
		return nil
	}
	if m.version != uint16(protocol.ProtocolVersionCurrent) {
		c.Send(encodeReload("protocol version mismatch: reload the page"))
		return nil
	}

	// Store calls happen here, on the connection goroutine, outside s.mu.
	sp, token, fresh, err := s.resolve(hctx, m.helloTok)
	if err != nil {
		s.log.Error("resolve guest", "err", err)
		return nil
	}

	s.mu.Lock()
	g, err := s.guestLocked(sp, fresh)
	if err != nil {
		s.mu.Unlock()
		s.log.Error("guest", "err", err)
		return nil
	}
	if g == nil {
		s.mu.Unlock()
		c.Send(encodeReload("server full"))
		return nil
	}
	g.conns++
	if g.idle != nil {
		s.idle.Remove(g.idle)
		g.idle = nil
	}
	// Welcome is sent by the tick once the player is in the world, so any
	// command the client sends after receiving it finds its player.
	s.ops = append(s.ops, op{join: true, g: g, conn: c, token: token})
	s.mu.Unlock()
	return g
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// resolve finds the stored player for a Hello token, or creates one with a
// fresh token when the token is empty or unknown. fresh reports a created
// player. Only the token's SHA-256 reaches the store.
func (s *Server) resolve(ctx context.Context, tok string) (sp store.Player, token string, fresh bool, err error) {
	if tok != "" {
		sp, err = s.store.LoadByTokenHash(ctx, hashToken(tok))
		if err == nil {
			return sp, tok, false, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return sp, "", false, fmt.Errorf("load guest: %w", err)
		}
	}
	tok, err = newToken()
	if err != nil {
		return sp, "", false, err
	}
	sp, err = s.store.CreateGuest(ctx, hashToken(tok))
	if err != nil {
		return sp, "", false, fmt.Errorf("create guest: %w", err)
	}
	return sp, tok, true, nil
}

// guestLocked returns the session guest for a stored player, assigning a
// session id if it has none. When every id is in use it drops the id of the
// guest offline longest and reuses it; that guest's leave is already queued
// ahead of the new join. It returns nil if all guests are online.
// Caller holds s.mu.
func (s *Server) guestLocked(sp store.Player, fresh bool) (*guest, error) {
	if g := s.guests[sp.ID]; g != nil {
		return g, nil
	}
	var id world.PlayerID
	if int(s.nextID) < s.cfg.MaxGuests {
		s.nextID++
		id = world.PlayerID(s.nextID)
	} else if e := s.idle.Front(); e != nil {
		old := s.idle.Remove(e).(*guest)
		old.idle = nil
		delete(s.guests, old.storeID)
		id = old.id
	} else {
		return nil, nil
	}
	pos := s.cfg.Spawn
	if !fresh && sp.Version > 1 { // version 1 has never been saved
		pos = world.Vec2{X: sp.X, Y: sp.Z}
	}
	g := &guest{id: id, storeID: sp.ID, ver: sp.Version, pos: pos}
	s.guests[sp.ID] = g
	return g, nil
}

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// BroadcastReload tells every connected client to reload, e.g. before a restart.
func (s *Server) BroadcastReload(reason string) {
	frame := encodeReload(reason)
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.Send(frame)
	}
}

// BroadcastRestartCountdown tells every connected client how long until the
// restart. A connection with a full send queue drops the frame (counted by the
// transport) and never delays the others.
func (s *Server) BroadcastRestartCountdown(remaining time.Duration) {
	secs := min(max(int64(remaining/time.Second), 0), math.MaxUint16)
	frame := encodeRestartCountdown(uint16(secs))
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.Send(frame)
	}
}

// ---- tick goroutine (world.System) ----

func (s *Server) Name() string { return "session" }

// Handle applies an InputCommand: it sets the movement target one tick of
// full-speed travel ahead, and the movement system clamps and applies it.
func (s *Server) Handle(cmd world.Command, p *world.Player) {
	in, ok := cmd.(InputCommand)
	if !ok {
		return
	}
	g := s.live[p.ID]
	if g == nil || in.conn != g.conn || in.Seq <= g.lastSeq {
		return // replaced connection, stale or reordered
	}
	g.lastSeq = in.Seq

	mx, mz := float64(in.MoveX), float64(in.MoveZ)
	if math.IsNaN(mx) || math.IsNaN(mz) || math.IsInf(mx, 0) || math.IsInf(mz, 0) {
		return
	}
	if l := math.Hypot(mx, mz); l > 1 {
		mx, mz = mx/l, mz/l
	}
	if mx == 0 && mz == 0 {
		p.HasDesired = false
		if f := float64(in.Facing); !math.IsNaN(f) && !math.IsInf(f, 0) {
			p.Facing = in.Facing
		}
		return
	}
	reach := float64(s.cfg.MoveSpeed) * s.w.TickInterval().Seconds()
	p.Desired = world.Vec2{X: p.Pos.X + float32(mx*reach), Y: p.Pos.Y + float32(mz*reach)}
	p.HasDesired = true
}

// Tick applies joins and leaves, then sends every connected player a snapshot.
func (s *Server) Tick(w *world.World, _ time.Duration) {
	s.mu.Lock()
	ops := s.ops
	s.ops = nil
	s.mu.Unlock()
	for _, o := range ops {
		if o.join {
			s.join(o.g, o.conn, o.token)
		} else {
			s.leave(o.g, o.conn)
		}
	}

	if now := s.clock.Now(); !now.Before(s.nextSave) {
		if !s.nextSave.IsZero() {
			s.saver.submit(s.collectSaves())
		}
		s.nextSave = now.Add(s.cfg.SaveInterval)
	}

	for _, p := range w.Players() {
		s.grid.Set(p.ID, p.Pos)
	}
	for id, g := range s.live {
		if g.conn == nil {
			continue
		}
		p := w.Player(id)
		if p == nil {
			continue
		}
		s.hits = s.grid.Query(p.Pos, interest.ViewRadius, interest.MaxVisible, id, s.hits[:0])
		s.ents = append(s.ents[:0], p)
		for _, h := range s.hits {
			if o := w.Player(h.ID); o != nil {
				s.ents = append(s.ents, o)
			}
		}
		g.conn.Send(encodeSnapshot(s.bld, w.TickCount(), g.lastSeq, s.ents))
	}
}

func (s *Server) join(g *guest, c transport.Conn, token string) {
	if g.conn != nil && g.conn != c {
		go g.conn.Close() // same token reconnected: replace the old connection
	}
	g.conn = c
	g.lastSeq = 0 // input sequence numbers are per connection
	if p := s.w.Player(g.id); p != nil && s.live[g.id] == g {
		// Still in the world from the previous connection.
		c.Send(encodeWelcome(g.id, token, p.Pos, s.cfg.TickRate))
		return
	}
	s.mu.Lock()
	pos := g.pos
	s.mu.Unlock()
	if err := s.w.AddPlayer(&world.Player{ID: g.id, Pos: pos}); err != nil {
		s.log.Error("add player", "player", g.id, "err", err)
		g.conn = nil
		go c.Close()
		return
	}
	s.live[g.id] = g
	c.Send(encodeWelcome(g.id, token, pos, s.cfg.TickRate))
	s.log.Info("player joined", "player", g.id, "transport", "websocket")
}

func (s *Server) leave(g *guest, c transport.Conn) {
	if g.conn != c {
		return // a newer connection already took over
	}
	g.conn = nil
	if p := s.w.Player(g.id); p != nil {
		s.mu.Lock()
		g.pos = p.Pos
		s.mu.Unlock()
	}
	s.mu.Lock()
	pos := g.pos
	s.mu.Unlock()
	s.saver.submit([]saveReq{{storeID: g.storeID, x: pos.X, z: pos.Y, ver: g.ver}})
	s.w.RemovePlayer(g.id)
	s.grid.Remove(g.id)
	delete(s.live, g.id)
	s.log.Info("player left", "player", g.id)
}

// Deliver implements chat.Sink. Called on the tick goroutine.
func (s *Server) Deliver(to world.PlayerID, m chat.Message) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeChat(m.From, m.Text))
	}
}

// collectSaves copies (store id, position, version) of every live player into
// a reused buffer. Tick goroutine only; the saver copies what it keeps.
func (s *Server) collectSaves() []saveReq {
	s.saves = s.saves[:0]
	for id, g := range s.live {
		if p := s.w.Player(id); p != nil {
			s.saves = append(s.saves, saveReq{storeID: g.storeID, x: p.Pos.X, z: p.Pos.Y, ver: g.ver})
		}
	}
	return s.saves
}

// SaveAll saves every connected player and waits until all pending saves are
// written. The caller must have stopped the world tick first, because it reads
// world state from the calling goroutine. It returns an error if ctx ends
// first or any save failed.
func (s *Server) SaveAll(ctx context.Context) error {
	s.saver.submit(s.collectSaves())
	return s.saver.flush(ctx)
}

// Close stops the saver after its current write. Call SaveAll first to keep
// pending saves.
func (s *Server) Close() { s.saver.close() }
