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
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/grounditems"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/transport"
	"github.com/cleiderg/lush/server/internal/world"
)

// MaxGuestID is the highest session id a guest can get: one below the start
// of the world's NPC id range (#142), where AddPlayer refuses player ids.
const MaxGuestID = int(world.DefaultNPCIDMin) - 1

// maxInfoPerTick caps the EntityInfo frames sent to one client per tick, so a
// crowd entering view (up to interest.MaxVisible at once) leaves room in the
// send queue for the Snapshot; the rest go on the following ticks.
const maxInfoPerTick = transport.DefaultSendQueue / 2

// Config holds session tuning.
type Config struct {
	TickRate     int
	Spawn        world.Vec2    // where new guests appear
	Collision    *world.Grid   // if set, a saved position on a blocked cell rejoins at Spawn
	MoveSpeed    float32       // world units per second at full stick; must match the movement system
	RunSpeed     float32       // the same while the Input's Run flag is set (0 = MoveSpeed); both go out in Welcome (D-044)
	HelloTimeout time.Duration // how long a connection may stay silent before Hello
	// MaxMsgPerSec disconnects clients that send faster than this (0 = 120).
	MaxMsgPerSec int
	// MaxGuests caps session ids (0 or above MaxGuestID = MaxGuestID, so guest
	// ids never reach the NPC id range). When every id is taken, the guest that
	// has been offline longest loses its session id and the id is reused; that
	// player gets a new id on its next Hello.
	MaxGuests int
	// SaveInterval is how often live players' positions are saved
	// (0 = DefaultSaveInterval).
	SaveInterval time.Duration
	// Data is the item table and rules: the starting kit, max HP and the
	// ItemCatalog sent to clients (nil = gamedata.Default()).
	Data *gamedata.Data
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
	Run          bool // the Run action flag: move at RunSpeed

	conn transport.Conn // inputs from a replaced connection are ignored
}

func (c InputCommand) Target() world.PlayerID { return c.Player }

// AttackCommand is a client AttackTarget naming a player id. Handle passes
// it to the PvP combat system if Victim is a player in the world.
type AttackCommand struct {
	Player world.PlayerID
	Victim world.PlayerID
}

func (c AttackCommand) Target() world.PlayerID { return c.Player }

type guest struct {
	// Guarded by Server.mu.
	id      world.PlayerID
	storeID int64
	ver     int64      // store version at load; immutable
	pos     world.Vec2 // last position, restored on resume
	xp      int        // combat XP, restored on resume
	inv     world.Inventory
	conns   int           // connections that completed the handshake and have not ended
	idle    *list.Element // in Server.idle while conns == 0 and out of the world

	// Tick-goroutine only.
	conn    transport.Conn
	lastSeq uint32
	goneAt  time.Time // when the last connection ended, while waiting out the logout lock
	// What the client last got in PlayerStats and InventoryState.
	sentStats stats
	sentInv   world.Inventory
	// What the client last got in EntityInfo, for entities in its view; an
	// entity leaving view is forgotten, so it gets EntityInfo again on return.
	known map[world.PlayerID]knownEnt
}

// knownEnt is an entity's last sent EntityInfo and the tick it was last in view.
type knownEnt struct {
	info entityInfo
	tick uint64
}

type op struct {
	join  bool
	g     *guest
	conn  transport.Conn
	token string // raw token, only carried to Welcome
}

// Server is the session layer. It implements world.System, chat.Sink,
// shop.Sink, trade.Sink, trade.Committer, pvp.HitSink, pvp.DropSink and
// transport.Handler (via ServeConn).
type Server struct {
	cfg   Config
	w     *world.World
	log   *slog.Logger
	clock world.Clock
	store store.Players
	data  *gamedata.Data

	catalog  []byte                 // encoded ItemCatalog, shared by every connection
	npcCat   []byte                 // encoded NpcCatalog, shared by every connection
	newToken func() (string, error) // replaced by tests

	mu     sync.Mutex
	guests map[int64]*guest // by store id; session ids for known players
	nextID uint32
	idle   *list.List // offline guests, longest offline first
	ops    []op
	conns  map[transport.Conn]struct{}

	saver *saver

	// Tick-goroutine only.
	trades   *trade.System       // set by SetTrades; nil without trading
	pvp      *pvp.Combat         // set by SetPvP; nil without PvP
	ground   *grounditems.System // set by SetGroundItems; where PvP drops spawn
	pvpHits  []pvp.Hit           // this tick's PvP swings, sent as CombatHit
	seen     []pvp.Hit           // reused: the hits one player can see
	live     map[world.PlayerID]*guest
	gone     map[world.PlayerID]*guest // live players with no connection, waiting out the logout lock
	nextSave time.Time
	saves    []saveReq
	econ     []econReq
	grid     *interest.Grid
	hits     []interest.Hit
	ents     []snapEnt
	npcsIn   map[world.PlayerID]uint64 // NPC ids in grid, with the last tick they were in the world
	infoLeft int                       // EntityInfo frames the current client may still get this tick
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
	if cfg.MaxGuests <= 0 || cfg.MaxGuests > MaxGuestID {
		cfg.MaxGuests = MaxGuestID
	}
	if cfg.TickRate <= 0 {
		cfg.TickRate = int(time.Second / w.TickInterval())
	}
	if cfg.SaveInterval <= 0 {
		cfg.SaveInterval = DefaultSaveInterval
	}
	if cfg.RunSpeed <= 0 {
		cfg.RunSpeed = cfg.MoveSpeed
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	data := cfg.Data
	if data == nil {
		d, err := gamedata.Default()
		if err != nil {
			// The embedded tables are validated by gamedata's tests; without
			// them every new guest fails to get its kit and is refused.
			log.Error("load game data", "err", err)
			d = &gamedata.Data{}
		}
		data = d
	}
	return &Server{saver: newSaver(players, log), cfg: cfg, w: w, log: log, clock: clock, store: players,
		data: data, catalog: encodeItemCatalog(data), npcCat: encodeNpcCatalog(data), newToken: newToken,
		guests: map[int64]*guest{}, idle: list.New(), conns: map[transport.Conn]struct{}{},
		live: map[world.PlayerID]*guest{}, gone: map[world.PlayerID]*guest{}, grid: interest.NewGrid(), npcsIn: map[world.PlayerID]uint64{}, bld: flatbuffers.NewBuilder(1024)}
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
		g.conns-- // the tick marks g idle once it has left the world
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
				MoveX: m.moveX, MoveZ: m.moveZ, Facing: m.facing, Run: m.run, conn: c})
		case protocol.MessageChatSend:
			s.w.Enqueue(chat.SendCommand{Player: g.id, Text: m.chatText})
		case protocol.MessageEmoteSend:
			// Unknown emote values are dropped, not fatal.
			if m.emote == protocol.EmoteWave {
				s.w.Enqueue(emote.WaveCommand{Player: g.id})
			}
		case protocol.MessagePing:
			c.Send(encodePong(m.pingToken))
		case protocol.MessageAttackTarget:
			// Only player ids go to PvP; 0 and NPC ids are ignored (PvE is
			// not wired here). Handle checks that the player exists.
			if m.target != 0 && int(m.target) <= s.cfg.MaxGuests {
				s.w.Enqueue(AttackCommand{Player: g.id, Victim: world.PlayerID(m.target)})
			}
		case protocol.MessageItemAction, protocol.MessageShopBuy, protocol.MessageShopSell,
			protocol.MessageTradeRequest, protocol.MessageTradeOffer, protocol.MessageTradeRemove,
			protocol.MessageTradeSetGold, protocol.MessageTradeAccept, protocol.MessageTradeCancel:
			if cmd := m.command(g.id); cmd != nil {
				s.w.Enqueue(cmd)
			}
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
// player. Only the token's SHA-256 reaches the store. A created player gets
// the starting kit and gold in the same store transaction that creates it,
// with an audit entry for each, so a failure leaves no player without its kit.
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
	inv, changes, err := world.NewStartingInventory(s.data)
	if err != nil {
		return sp, "", false, err
	}
	kit, err := toHoldings(inv)
	if err != nil {
		return sp, "", false, fmt.Errorf("starting kit: %w", err)
	}
	tok, err = s.newToken()
	if err != nil {
		return sp, "", false, err
	}
	sp, err = s.store.CreateGuest(ctx, hashToken(tok), kit, auditEntries(changes, "starting_kit"))
	if err != nil {
		return sp, "", false, fmt.Errorf("create guest: %w", err)
	}
	return sp, tok, true, nil
}

// guestLocked returns the session guest for a stored player, assigning a
// session id if it has none. When every id is in use it drops the id of the
// guest offline longest and reuses it; only guests already out of the world
// are offline. It returns nil if all guests are online.
// Caller holds s.mu.
func (s *Server) guestLocked(sp store.Player, fresh bool) (*guest, error) {
	if g := s.guests[sp.ID]; g != nil {
		return g, nil
	}
	inv, err := fromHoldings(s.data, sp.Holdings)
	if err != nil {
		return nil, fmt.Errorf("load player %d: %w", sp.ID, err)
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
		// Saved inside a building, in water or off the map (e.g. before
		// collision existed): rejoin at spawn rather than stuck.
		if s.cfg.Collision != nil && s.cfg.Collision.Blocked(pos) {
			pos = s.cfg.Spawn
		}
	}
	g := &guest{id: id, storeID: sp.ID, ver: sp.Version, pos: pos, xp: int(sp.XP), inv: inv}
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
// full-speed travel ahead (at RunSpeed when the Run flag is set, else
// MoveSpeed), and the movement system clamps and applies it.
// An AttackCommand whose victim is another player in the world goes to the
// PvP combat system; any other is ignored.
func (s *Server) Handle(cmd world.Command, p *world.Player) {
	if a, ok := cmd.(AttackCommand); ok {
		if s.pvp != nil && a.Victim != p.ID && s.w.Player(a.Victim) != nil {
			s.pvp.Handle(pvp.AttackCommand{Player: p.ID, Victim: a.Victim}, p)
		}
		return
	}
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
	p.Running = in.Run
	if mx == 0 && mz == 0 {
		p.HasDesired = false
		if f := float64(in.Facing); !math.IsNaN(f) && !math.IsInf(f, 0) {
			p.Facing = in.Facing
		}
		return
	}
	speed := s.cfg.MoveSpeed
	if in.Run {
		speed = s.cfg.RunSpeed
	}
	reach := float64(speed) * s.w.TickInterval().Seconds()
	p.Desired = world.Vec2{X: p.Pos.X + float32(mx*reach), Y: p.Pos.Y + float32(mz*reach)}
	p.HasDesired = true
}

// Tick applies joins and leaves, removes disconnected players whose logout
// lock has passed and who have no trade being saved, then sends every connected player a snapshot.
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

	now := s.clock.Now()
	lock := s.data.Rules.LogoutCombatLock()
	for id, g := range s.gone {
		p := w.Player(id)
		if p != nil && (now.Before(g.goneAt.Add(lock)) ||
			!p.LastCombat.IsZero() && now.Before(p.LastCombat.Add(lock))) {
			continue // closing the tab must not end a fight early
		}
		if p != nil && s.trades != nil && s.trades.Committing(id) {
			continue // the trade's result must reach the player first
		}
		s.remove(g)
	}

	s.econ = s.econ[:0]
	for id, g := range s.live {
		if p := w.Player(id); p != nil {
			s.econ = s.appendEconomy(s.econ, g, p)
		}
	}
	s.saver.submitEconomy(s.econ...)

	if !now.Before(s.nextSave) {
		if !s.nextSave.IsZero() {
			s.saver.submit(s.collectSaves())
		}
		s.nextSave = now.Add(s.cfg.SaveInterval)
	}

	tick := w.TickCount()
	for _, p := range w.Players() {
		s.grid.Set(p.ID, p.Pos)
	}
	for _, n := range w.NPCs() {
		s.grid.Set(world.PlayerID(n.ID), n.Pos)
		s.npcsIn[world.PlayerID(n.ID)] = tick
	}
	for id, t := range s.npcsIn {
		if t != tick { // removed from the world since
			s.grid.Remove(id)
			delete(s.npcsIn, id)
		}
	}
	for id, g := range s.live {
		if g.conn == nil {
			continue
		}
		p := w.Player(id)
		if p == nil {
			continue
		}
		s.sendChanged(g, p)
		s.hits = s.grid.Query(p.Pos, interest.ViewRadius, interest.MaxVisible, id, s.hits[:0])
		s.ents = append(s.ents[:0], playerEnt(p))
		s.infoLeft = maxInfoPerTick
		s.sendEntityInfo(g, id, s.playerInfo(p), tick)
		for _, h := range s.hits {
			if o := w.Player(h.ID); o != nil {
				s.ents = append(s.ents, playerEnt(o))
				s.sendEntityInfo(g, h.ID, s.playerInfo(o), tick)
			} else if n := w.NPC(world.NPCID(h.ID)); n != nil {
				s.ents = append(s.ents, npcEnt(n))
				s.sendEntityInfo(g, h.ID, s.npcInfo(n), tick)
			}
		}
		for eid, k := range g.known {
			if k.tick != tick {
				delete(g.known, eid) // left view
			}
		}
		s.sendCombatHits(g, tick)
		g.conn.Send(encodeSnapshot(s.bld, tick, g.lastSeq, s.ents))
	}
	clear(s.pvpHits)
	s.pvpHits = s.pvpHits[:0]
}

// sendCombatHits sends g one CombatHit with this tick's PvP hits whose
// attacker or target is in s.ents (the player and what they see), if any.
func (s *Server) sendCombatHits(g *guest, tick uint64) {
	if len(s.pvpHits) == 0 {
		return
	}
	s.seen = s.seen[:0]
	for _, h := range s.pvpHits {
		if s.inView(h.Attacker) || s.inView(h.Target) {
			s.seen = append(s.seen, h)
		}
	}
	if len(s.seen) > 0 {
		g.conn.Send(encodeCombatHit(s.bld, tick, s.seen))
	}
}

// sendEntityInfo sends g an EntityInfo for entity id if the client has not
// had one since it entered view, or in has changed, and marks id in view.
// Past this tick's budget, or when the send queue is full, the EntityInfo is
// not recorded as sent, so it goes out on a later tick.
func (s *Server) sendEntityInfo(g *guest, id world.PlayerID, in entityInfo, tick uint64) {
	k, ok := g.known[id]
	if !ok || k.info != in {
		if s.infoLeft <= 0 || !g.conn.Send(encodeEntityInfo(s.bld, id, in)) {
			s.infoLeft = 0
			if ok {
				g.known[id] = knownEnt{info: k.info, tick: tick}
			}
			return
		}
		s.infoLeft--
	}
	g.known[id] = knownEnt{info: in, tick: tick}
}

// playerInfo is p's EntityInfo: combat level and worn equipment.
func (s *Server) playerInfo(p *world.Player) entityInfo {
	return entityInfo{kind: protocol.EntityKindPlayer, level: p.CombatLevel(s.data.Rules), equip: p.Inv.Equipment}
}

// npcInfo is n's EntityInfo: NPC type and the type's combat level.
func (s *Server) npcInfo(n *world.NPC) entityInfo {
	t, _ := s.data.NPC(gamedata.NPCTypeID(n.Type))
	return entityInfo{kind: protocol.EntityKindNpc, npcType: uint16(n.Type), level: t.Level}
}

// inView reports whether id is in s.ents.
func (s *Server) inView(id world.PlayerID) bool {
	for _, e := range s.ents {
		if e.id == id {
			return true
		}
	}
	return false
}

func (s *Server) join(g *guest, c transport.Conn, token string) {
	if g.conn != nil && g.conn != c {
		go g.conn.Close() // same token reconnected: replace the old connection
	}
	g.conn = c
	g.lastSeq = 0                           // input sequence numbers are per connection
	g.known = map[world.PlayerID]knownEnt{} // the new connection has no EntityInfo yet
	if p := s.w.Player(g.id); p != nil && s.live[g.id] == g {
		// Still in the world from the previous connection, or waiting out
		// the logout lock after it ended: take over the same player.
		delete(s.gone, g.id)
		c.Send(encodeWelcome(g.id, token, p.Pos, s.cfg.TickRate, s.cfg.MoveSpeed, s.cfg.RunSpeed))
		s.sendInitial(g, p)
		return
	}
	s.mu.Lock()
	p := &world.Player{ID: g.id, Pos: g.pos, XP: g.xp, Inv: cloneInventory(g.inv)}
	s.mu.Unlock()
	p.HP = p.MaxHP(s.data.Rules)
	if err := s.w.AddPlayer(p); err != nil {
		s.log.Error("add player", "player", g.id, "err", err)
		g.conn = nil
		go c.Close()
		return
	}
	s.live[g.id] = g
	c.Send(encodeWelcome(g.id, token, p.Pos, s.cfg.TickRate, s.cfg.MoveSpeed, s.cfg.RunSpeed))
	s.sendInitial(g, p)
	s.log.Info("player joined", "player", g.id, "transport", "websocket")
}

// sendInitial sends a newly connected client the item catalog and NPC
// catalog, then the player's stats and inventory.
func (s *Server) sendInitial(g *guest, p *world.Player) {
	g.conn.Send(s.catalog)
	g.conn.Send(s.npcCat)
	g.sentStats = statsOf(s.data.Rules, p)
	g.conn.Send(encodePlayerStats(s.bld, g.sentStats))
	g.sentInv = cloneInventory(p.Inv)
	g.conn.Send(encodeInventoryState(s.bld, &p.Inv))
}

// sendChanged sends PlayerStats and InventoryState if they differ from what
// the client last got.
func (s *Server) sendChanged(g *guest, p *world.Player) {
	if st := statsOf(s.data.Rules, p); st != g.sentStats {
		g.sentStats = st
		g.conn.Send(encodePlayerStats(s.bld, st))
	}
	if !sameInventory(&g.sentInv, &p.Inv) {
		g.sentInv.Slots = append(g.sentInv.Slots[:0], p.Inv.Slots...)
		g.sentInv.Equipment, g.sentInv.Gold = p.Inv.Equipment, p.Inv.Gold
		g.conn.Send(encodeInventoryState(s.bld, &p.Inv))
	}
}

// leave handles a connection that ended. The player stays in the world, seen
// by others, until the logout lock has passed (see Tick).
func (s *Server) leave(g *guest, c transport.Conn) {
	if g.conn != c {
		// A newer connection already took over, or the join failed.
		s.idleIfGone(g)
		return
	}
	g.conn = nil
	g.goneAt = s.clock.Now()
	s.gone[g.id] = g
	s.log.Info("player disconnected", "player", g.id)
}

// remove takes a disconnected player out of the world and saves it.
func (s *Server) remove(g *guest) {
	s.mu.Lock()
	if p := s.w.Player(g.id); p != nil {
		g.pos, g.xp, g.inv = p.Pos, p.XP, cloneInventory(p.Inv)
	}
	req := saveReq{storeID: g.storeID, x: g.pos.X, z: g.pos.Y, xp: int64(g.xp), ver: g.ver}
	s.mu.Unlock()
	if p := s.w.Player(g.id); p != nil {
		s.saver.submitEconomy(s.appendEconomy(nil, g, p)...)
	}
	s.saver.submit([]saveReq{req})
	s.w.RemovePlayer(g.id)
	s.grid.Remove(g.id)
	delete(s.live, g.id)
	delete(s.gone, g.id)
	s.idleIfGone(g)
	s.log.Info("player left", "player", g.id)
}

// idleIfGone marks g offline, so its session id may be reused, once it has
// no connections and is out of the world.
func (s *Server) idleIfGone(g *guest) {
	if s.live[g.id] == g {
		return
	}
	s.mu.Lock()
	if g.conns == 0 && g.idle == nil {
		g.idle = s.idle.PushBack(g)
	}
	s.mu.Unlock()
}

// Deliver implements chat.Sink. Called on the tick goroutine.
func (s *Server) Deliver(to world.PlayerID, m chat.Message) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeChat(m.From, m.Text))
	}
}

// ShopResult implements shop.Sink: it sends the result to the client that
// asked. Called on the tick goroutine, before this Tick sends the changed
// PlayerStats and InventoryState.
func (s *Server) ShopResult(to world.PlayerID, r shop.Result) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeShopResult(r))
	}
}

// appendEconomy drains p's economy outbox and appends one store write per
// event, except completed trades, which CommitTrade already saved: the holdings from the event's inventory copy and an audit entry per
// change, tagged with the event's reason. Tick goroutine only.
func (s *Server) appendEconomy(dst []econReq, g *guest, p *world.Player) []econReq {
	for _, ev := range p.DrainEconomy() {
		if ev.Reason == trade.ReasonTrade {
			continue // saved by the trade's own transaction (CommitTrade)
		}
		h, err := toHoldings(ev.Inv)
		if err != nil {
			s.log.Error("economy event", "player", g.id, "store_id", g.storeID, "reason", ev.Reason, "err", err)
			continue
		}
		entries := auditEntries(ev.Changes, ev.Reason)
		for i := range entries {
			entries[i].Actor = g.storeID
		}
		dst = append(dst, econEvent(g.storeID, h, entries))
	}
	return dst
}

// collectSaves copies (store id, position, XP, version) of every live player into
// a reused buffer. Tick goroutine only; the saver copies what it keeps.
func (s *Server) collectSaves() []saveReq {
	s.saves = s.saves[:0]
	for id, g := range s.live {
		if p := s.w.Player(id); p != nil {
			s.saves = append(s.saves, saveReq{storeID: g.storeID, x: p.Pos.X, z: p.Pos.Y, xp: int64(p.XP), ver: g.ver})
		}
	}
	return s.saves
}

// SaveAll drains the economy outbox of every player in the world, including
// disconnected ones waiting out the logout lock, saves every one of them and
// waits until all pending saves and economy writes are
// written. The caller must have stopped the world tick first, because it reads
// world state from the calling goroutine. It returns an error if ctx ends
// first or any save failed.
func (s *Server) SaveAll(ctx context.Context) error {
	s.econ = s.econ[:0]
	for id, g := range s.live {
		if p := s.w.Player(id); p != nil {
			s.econ = s.appendEconomy(s.econ, g, p)
		}
	}
	s.saver.submitEconomy(s.econ...)
	s.saver.submit(s.collectSaves())
	return s.saver.flush(ctx)
}

// Close stops the saver after its current write. Call SaveAll first to keep
// pending saves.
func (s *Server) Close() { s.saver.close() }

// ---- PvP ----

// SetPvP connects the PvP combat system, so that AttackTarget naming a
// player reaches it. Call it before the world starts ticking.
func (s *Server) SetPvP(c *pvp.Combat) { s.pvp = c }

// SetGroundItems connects the ground item system that PvP drops spawn in.
// Call it before the world starts ticking.
func (s *Server) SetGroundItems(g *grounditems.System) { s.ground = g }

// PvPHit implements pvp.HitSink: it keeps the hit for this tick's CombatHit.
// Called on the tick goroutine, before this Tick.
func (s *Server) PvPHit(h pvp.Hit) { s.pvpHits = append(s.pvpHits, h) }

// PvPDrop implements pvp.DropSink: it spawns each dropped stack as a ground
// item at the death position, reserved for the killer, on the tick of the
// death. The victim's pvp_death outbox event, which removed the same stacks,
// is written by this Tick as one audited transaction. Called on the tick
// goroutine.
func (s *Server) PvPDrop(d pvp.Drop) {
	if s.ground == nil {
		s.log.Error("pvp drop: no ground item system", "owner", d.Owner, "stacks", len(d.Stacks))
		return
	}
	now := s.clock.Now()
	for _, st := range d.Stacks {
		if _, err := s.ground.Spawn(st.Item, st.Qty, d.Pos, d.Owner, now); err != nil {
			s.log.Error("pvp drop", "owner", d.Owner, "item", st.Item, "qty", st.Qty, "err", err)
		}
	}
}

// ---- trades ----

// SetTrades connects the trade system, so that CommitTrade can report results
// to it and a disconnected player is not removed while their trade is being
// saved. Call it before the world starts ticking.
func (s *Server) SetTrades(t *trade.System) { s.trades = t }

// TradeRequestNotice implements trade.Sink. Called on the tick goroutine.
func (s *Server) TradeRequestNotice(to, from world.PlayerID) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeTradeRequestNotice(from))
	}
}

// TradeState implements trade.Sink. Called on the tick goroutine.
func (s *Server) TradeState(to world.PlayerID, v trade.View) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeTradeState(s.bld, v))
	}
}

// TradeResult implements trade.Sink. Called on the tick goroutine, before
// this Tick sends the changed PlayerStats and InventoryState.
func (s *Server) TradeResult(to world.PlayerID, r trade.Result) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeTradeResult(r))
	}
}

// CommitTrade implements trade.Committer. It first queues both players'
// pending economy events, then the trade as one ApplyEconomy holding both
// players' resulting holdings and a "trade" audit entry per change, with
// Actor and Counterparty set to the two players. The saver writes it after
// every earlier write of either player, and the result goes back to the trade
// system through Resolve. Called on the tick goroutine; never blocks.
func (s *Server) CommitTrade(req trade.CommitRequest) {
	resolve := func(ok bool) {
		if s.trades != nil {
			s.trades.Resolve(req.ID, ok)
		}
	}
	ga, gb := s.live[req.A.Player], s.live[req.B.Player]
	pa, pb := s.w.Player(req.A.Player), s.w.Player(req.B.Player)
	if ga == nil || gb == nil || pa == nil || pb == nil {
		s.log.Error("commit trade: player not live", "a", req.A.Player, "b", req.B.Player)
		resolve(false)
		return
	}
	ha, errA := toHoldings(req.A.Inv)
	hb, errB := toHoldings(req.B.Inv)
	if err := errors.Join(errA, errB); err != nil {
		s.log.Error("commit trade", "a", ga.storeID, "b", gb.storeID, "err", err)
		resolve(false)
		return
	}
	entries := tradeEntries(nil, req.A.Changes, ga.storeID, gb.storeID)
	entries = tradeEntries(entries, req.B.Changes, gb.storeID, ga.storeID)
	earlier := s.appendEconomy(s.appendEconomy(nil, ga, pa), gb, pb)
	s.saver.submitEconomy(append(earlier, econReq{
		changes: []store.EconomyChange{{PlayerID: ga.storeID, Holdings: ha}, {PlayerID: gb.storeID, Holdings: hb}},
		entries: entries,
		done:    resolve,
	})...)
}

// tradeEntries appends an audit entry of kind "trade" per change, made by
// actor with counterparty.
func tradeEntries(dst []store.Entry, changes []world.Change, actor, counterparty int64) []store.Entry {
	for _, c := range changes {
		dst = append(dst, store.Entry{Kind: trade.ReasonTrade, Actor: actor, Counterparty: counterparty,
			Payload: auditPayload{Reason: trade.ReasonTrade, Change: c.Kind.String(), Item: int(c.Item), Qty: c.Qty, Gold: c.Gold}})
	}
	return dst
}
