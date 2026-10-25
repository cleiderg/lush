// Package world is the pure simulation core: entities, the fixed tick, the
// command queue and the System registry. It performs no I/O.
package world

import (
	"fmt"
	"sync"
	"time"
)

// PlayerID identifies a player entity.
type PlayerID uint64

// Vec2 is a position in world units.
type Vec2 struct{ X, Y float32 }

// Anim is a player's animation state.
type Anim uint8

const (
	AnimIdle Anim = iota
	AnimWalk
	AnimWave // matches protocol Animation.Wave (2)
)

// Player is an entity in the world. Only the tick may mutate it.
type Player struct {
	ID     PlayerID
	Pos    Vec2
	Facing float32 // radians
	Anim   Anim

	// Desired is the position the player asked to move to; set by the movement
	// system's Handle and consumed (clamped) in its Tick.
	Desired    Vec2
	HasDesired bool

	// Inv is the player's items, equipment and gold. Build it with
	// NewStartingInventory or NewInventory.
	Inv Inventory

	// XP is the player's combat XP total. Change it with AddXP.
	XP int
	// HP is the player's current hitpoints, from 0 to MaxHP.
	HP int
	// LastCombat is when the player last dealt or took damage; zero if never.
	// Health regeneration waits for Rules.RegenDelay after it.
	LastCombat time.Time
}

// Command is a unit of input pushed from another goroutine and applied on the tick.
type Command interface {
	// Target is the player the command acts on.
	Target() PlayerID
}

// System is a feature module plugged into the world.
type System interface {
	Name() string
	// Handle applies a command for player p. Called on the tick goroutine, in
	// enqueue order, before any system Tick of that tick.
	Handle(cmd Command, p *Player)
	// Tick advances the system. dt is the time covered since this system's
	// previous Tick (tick interval times the system's period).
	Tick(w *World, dt time.Duration)
}

// Config holds world tuning values.
type Config struct {
	// TickRate in Hz. Default 20.
	TickRate int
	// NPCIDMin and NPCIDMax bound the ids AddNPC hands out (both inclusive).
	// Players and NPCs share the 16-bit wire entity id space, so AddPlayer
	// rejects ids in this range. Both zero means the default range; a range
	// with NPCIDMin > NPCIDMax is empty.
	NPCIDMin, NPCIDMax NPCID
}

// Default NPC id range: the top quarter of the 16-bit entity id space, leaving
// 1..49151 for players.
const (
	DefaultNPCIDMin NPCID = 0xC000
	DefaultNPCIDMax NPCID = 0xFFFF
)

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{TickRate: 20, NPCIDMin: DefaultNPCIDMin, NPCIDMax: DefaultNPCIDMax}
}

type registered struct {
	sys   System
	every uint64
}

// World holds all simulation state.
type World struct {
	cfg     Config
	players map[PlayerID]*Player
	order   []*Player // insertion order, for deterministic iteration
	systems []registered
	tick    uint64

	npcs     map[NPCID]*NPC
	npcOrder []*NPC // insertion order, for deterministic iteration
	nextNPC  NPCID  // next id AddNPC tries

	qmu   sync.Mutex
	queue []Command
	spare []Command
}

// New creates a World. A non-positive TickRate falls back to the default.
func New(cfg Config) *World {
	if cfg.TickRate <= 0 {
		cfg.TickRate = DefaultConfig().TickRate
	}
	if cfg.NPCIDMin == 0 && cfg.NPCIDMax == 0 {
		cfg.NPCIDMin, cfg.NPCIDMax = DefaultNPCIDMin, DefaultNPCIDMax
	}
	return &World{
		cfg:     cfg,
		players: make(map[PlayerID]*Player),
		npcs:    make(map[NPCID]*NPC),
		nextNPC: cfg.NPCIDMin,
	}
}

// TickInterval is the duration of one tick.
func (w *World) TickInterval() time.Duration { return time.Second / time.Duration(w.cfg.TickRate) }

// TickCount is the number of ticks run so far.
func (w *World) TickCount() uint64 { return w.tick }

// Register adds a system that ticks every world tick.
func (w *World) Register(s System) error { return w.RegisterEvery(s, 1) }

// RegisterEvery adds a system whose Tick runs on every nth world tick.
func (w *World) RegisterEvery(s System, n int) error {
	if n < 1 {
		return fmt.Errorf("register %q: period %d < 1", s.Name(), n)
	}
	for _, r := range w.systems {
		if r.sys.Name() == s.Name() {
			return fmt.Errorf("register %q: already registered", s.Name())
		}
	}
	w.systems = append(w.systems, registered{s, uint64(n)})
	return nil
}

// AddPlayer inserts p. Tick-goroutine only (or before the loop starts).
func (w *World) AddPlayer(p *Player) error {
	if _, ok := w.players[p.ID]; ok {
		return fmt.Errorf("add player %d: already exists", p.ID)
	}
	if w.isNPCID(uint64(p.ID)) {
		return fmt.Errorf("add player %d: id is in the NPC range %d..%d", p.ID, w.cfg.NPCIDMin, w.cfg.NPCIDMax)
	}
	w.players[p.ID] = p
	w.order = append(w.order, p)
	return nil
}

// RemovePlayer deletes a player. Tick-goroutine only.
func (w *World) RemovePlayer(id PlayerID) {
	p, ok := w.players[id]
	if !ok {
		return
	}
	delete(w.players, id)
	for i, q := range w.order {
		if q == p {
			w.order = append(w.order[:i], w.order[i+1:]...)
			break
		}
	}
}

// Player returns the player with id, or nil.
func (w *World) Player(id PlayerID) *Player { return w.players[id] }

// Players returns players in insertion order. The slice must not be modified.
func (w *World) Players() []*Player { return w.order }

// Enqueue adds a command to be applied on the next tick. Safe for concurrent use.
func (w *World) Enqueue(c Command) {
	w.qmu.Lock()
	w.queue = append(w.queue, c)
	w.qmu.Unlock()
}

// Tick runs one simulation step: apply queued commands in order, then tick
// each due system in registration order.
func (w *World) Tick() {
	w.tick++

	w.qmu.Lock()
	cmds := w.queue
	w.queue = w.spare[:0]
	w.qmu.Unlock()

	for _, c := range cmds {
		p := w.players[c.Target()]
		if p == nil {
			continue
		}
		for _, r := range w.systems {
			r.sys.Handle(c, p)
		}
	}
	clear(cmds)
	w.spare = cmds[:0]

	for _, r := range w.systems {
		if w.tick%r.every == 0 {
			r.sys.Tick(w, w.TickInterval()*time.Duration(r.every))
		}
	}
}
