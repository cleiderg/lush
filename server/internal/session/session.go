// Package session turns transport connections into players: the Hello/Welcome
// handshake, guest identity, inbound messages as world commands, and the
// per-tick snapshots going back out.
//
// Connection goroutines never touch world state. They enqueue commands on the
// world and queue join/leave operations; the Server's System hooks apply them
// on the tick goroutine.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sync"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/interest"
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/chat"
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
}

// InputCommand is a client Input as a world command.
type InputCommand struct {
	Player       world.PlayerID
	Seq          uint32
	MoveX, MoveZ float32
	Facing       float32
}

func (c InputCommand) Target() world.PlayerID { return c.Player }

type guest struct {
	// Guarded by Server.mu.
	id    world.PlayerID
	token string
	pos   world.Vec2 // last position, restored on resume

	// Tick-goroutine only.
	conn    transport.Conn
	lastSeq uint32
}

type op struct {
	join bool
	g    *guest
	conn transport.Conn

	welcome []byte // join only
}

// Server is the session layer. It implements world.System, chat.Sink and
// transport.Handler (via ServeConn).
type Server struct {
	cfg   Config
	w     *world.World
	log   *slog.Logger
	clock world.Clock

	mu     sync.Mutex
	guests map[string]*guest // by token
	nextID uint32
	ops    []op
	conns  map[transport.Conn]struct{}

	// Tick-goroutine only.
	live map[world.PlayerID]*guest
	grid *interest.Grid
	hits []interest.Hit
	ents []*world.Player
	bld  *flatbuffers.Builder
}

// New creates the Server. Register it on w after the movement and chat systems.
func New(cfg Config, w *world.World, clock world.Clock, log *slog.Logger) *Server {
	if cfg.HelloTimeout <= 0 {
		cfg.HelloTimeout = 10 * time.Second
	}
	if cfg.MaxMsgPerSec <= 0 {
		cfg.MaxMsgPerSec = 120
	}
	if cfg.TickRate <= 0 {
		cfg.TickRate = int(time.Second / w.TickInterval())
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{cfg: cfg, w: w, log: log, clock: clock,
		guests: map[string]*guest{}, conns: map[transport.Conn]struct{}{},
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
			s.w.Enqueue(InputCommand{Player: g.id, Seq: m.input.Seq(),
				MoveX: m.moveX, MoveZ: m.moveZ, Facing: m.input.Facing()})
		case protocol.MessageChatSend:
			s.w.Enqueue(chat.SendCommand{Player: g.id, Text: m.chatText})
		case protocol.MessagePing:
			c.Send(encodePong(m.ping.Token()))
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
	if m.hello.ProtocolVersion() != uint16(protocol.ProtocolVersionCurrent) {
		c.Send(encodeReload("protocol version mismatch: reload the page"))
		return nil
	}

	s.mu.Lock()
	g, ok := s.guests[m.helloTok]
	if !ok || m.helloTok == "" {
		if s.nextID >= math.MaxUint16 {
			s.mu.Unlock()
			c.Send(encodeReload("server full"))
			return nil
		}
		s.nextID++
		tok, err := newToken()
		if err != nil {
			s.nextID--
			s.mu.Unlock()
			s.log.Error("guest token", "err", err)
			return nil
		}
		g = &guest{id: world.PlayerID(s.nextID), token: tok, pos: s.cfg.Spawn}
		s.guests[g.token] = g
	}
	// Welcome is sent by the tick once the player is in the world, so any
	// command the client sends after receiving it finds its player.
	welcome := encodeWelcome(g.id, g.token, g.pos, s.cfg.TickRate)
	s.ops = append(s.ops, op{join: true, g: g, conn: c, welcome: welcome})
	s.mu.Unlock()
	return g
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
	if g == nil || in.Seq <= g.lastSeq {
		return // stale or reordered
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
			s.join(o.g, o.conn, o.welcome)
		} else {
			s.leave(o.g, o.conn)
		}
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

func (s *Server) join(g *guest, c transport.Conn, welcome []byte) {
	s.mu.Lock()
	pos := g.pos
	s.mu.Unlock()
	if g.conn != nil && g.conn != c {
		go g.conn.Close() // same token reconnected: replace the old connection
	}
	g.conn = c
	c.Send(welcome)
	if _, ok := s.live[g.id]; ok {
		return // still in the world from the previous connection
	}
	if err := s.w.AddPlayer(&world.Player{ID: g.id, Pos: pos}); err != nil {
		s.log.Error("add player", "player", g.id, "err", err)
		go c.Close()
		return
	}
	s.live[g.id] = g
	g.lastSeq = 0
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
