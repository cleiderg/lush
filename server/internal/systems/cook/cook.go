// Package cook is the cooking half of the skills system
// (docs/specs/skills.md, Cooking; D-051): a player within range of the grill
// who carries raw fish keeps cooking one fish every Skills.CookIntervalTicks
// combat ticks until no cookable raw fish is left, the player moves or combat
// starts.
//
// Fish are cooked in inventory slot order, skipping any the player lacks the
// Cooking level for. Each attempt replaces the raw fish in its slot with the
// cooked item or the burnt fish and records an economy outbox event. All
// rolls use the injected random source.
//
// Register it after the movement system (every world tick: movement resets
// the animation each tick, so this system sets it again). Link it with the
// gathering system using LinkGather so each stops the other. It does no I/O
// and reads time only from the injected world.Clock.
package cook

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/gather"
	"github.com/cleiderg/lush/server/internal/world"
)

// ReasonCook is the outbox reason of a cooked or burnt fish.
const ReasonCook = "cook"

// Result is the outcome of a start request.
type Result uint8

const (
	Started       Result = 0
	TooFar        Result = 1
	NothingToCook Result = 2 // no raw fish in the inventory
	LevelTooLow   Result = 3 // raw fish only above the player's level; see the level reported
	InCombat      Result = 4
)

var resultNames = [...]string{"started", "too_far", "nothing_to_cook", "level_too_low", "in_combat"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return fmt.Sprintf("Result(%d)", uint8(r))
}

// StopReason is why cooking ended after it had started.
type StopReason uint8

const (
	StoppedByPlayer StopReason = iota // a StopCommand
	StoppedNoFish                     // no cookable raw fish is left
	StoppedMoved                      // the player moved
	StoppedCombat                     // combat started
	StoppedGather                     // the player started gathering
	StoppedLocked                     // the inventory is locked
)

var stopNames = [...]string{"player", "no_fish", "moved", "combat", "gather", "locked"}

func (r StopReason) String() string {
	if int(r) < len(stopNames) {
		return stopNames[r]
	}
	return fmt.Sprintf("StopReason(%d)", uint8(r))
}

// StartCommand asks Player to start cooking at the grill.
type StartCommand struct{ Player world.PlayerID }

func (c StartCommand) Target() world.PlayerID { return c.Player }

// StopCommand asks Player to stop cooking.
type StopCommand struct{ Player world.PlayerID }

func (c StopCommand) Target() world.PlayerID { return c.Player }

// Sink receives what the player must be told; the session implements it.
// It is called on the tick goroutine and must not block. A nil Sink is fine.
type Sink interface {
	// StartResult is sent for every StartCommand. level is the lowest Cooking
	// level needed for LevelTooLow and 0 otherwise.
	StartResult(to world.PlayerID, r Result, level int)
	// Stopped is sent when cooking that had started ends.
	Stopped(to world.PlayerID, why StopReason)
	// Cooked is sent for each attempt: item is the cooked or burnt item,
	// xpTenths the XP given (0 for a burnt fish), and whether the player
	// reached a new level (then level is the new one).
	Cooked(to world.PlayerID, item gamedata.ItemID, burnt bool, xpTenths int, leveled bool, level int)
}

// Config tunes the system.
type Config struct {
	// TicksPerCombatTick is how many world ticks make one combat tick (12).
	TicksPerCombatTick int
	// TileSize is the length of a tile in metres.
	TileSize float64
}

// DefaultConfig is the 20 Hz world with a combat tick every 12th tick.
func DefaultConfig() Config { return Config{TicksPerCombatTick: 12, TileSize: gather.TileSize} }

type cooker struct {
	pos        world.Vec2
	lastCombat time.Time
	ticks      int // combat ticks since the last attempt
}

// System runs cooking.
type System struct {
	data   *gamedata.Data
	clock  world.Clock
	rand   formula.Rand
	sink   Sink
	cfg    Config
	grill  world.Vec2
	recipe map[gamedata.ItemID]*gamedata.Recipe
	gather *gather.System

	active map[world.PlayerID]*cooker
	order  []world.PlayerID // cookers in start order, for deterministic ticking
}

// New creates the cooking system with the grill at l.Grill.
func New(d *gamedata.Data, l gather.Layout, clock world.Clock, r formula.Rand, sink Sink, cfg Config) (*System, error) {
	if cfg.TicksPerCombatTick < 1 || cfg.TileSize <= 0 {
		return nil, errors.New("cook: bad config")
	}
	s := &System{
		data: d, clock: clock, rand: r, sink: sink, cfg: cfg, grill: l.Grill,
		recipe: map[gamedata.ItemID]*gamedata.Recipe{},
		active: map[world.PlayerID]*cooker{},
	}
	for i := range d.Skills.Cooking {
		s.recipe[d.Skills.Cooking[i].Raw] = &d.Skills.Cooking[i]
	}
	return s, nil
}

// LinkGather makes starting to cook stop gathering and starting to gather stop
// cooking.
func (s *System) LinkGather(g *gather.System) {
	s.gather = g
	g.OnStart(func(p *world.Player) { s.stop(p, StoppedGather) })
}

func (s *System) Name() string { return "cook" }

// Cooking reports whether p is cooking.
func (s *System) Cooking(id world.PlayerID) bool {
	_, ok := s.active[id]
	return ok
}

func (s *System) tile(v world.Vec2) (int, int) {
	return int(math.Floor(float64(v.X) / s.cfg.TileSize)), int(math.Floor(float64(v.Y) / s.cfg.TileSize))
}

// inRange reports whether pos is within Skills.RangeTiles tiles of the grill,
// the diagonals included when Skills.RangeDiagonal is set.
func (s *System) inRange(pos world.Vec2) bool {
	px, py := s.tile(pos)
	ax, ay := s.tile(s.grill)
	dx, dy := max(px-ax, ax-px), max(py-ay, ay-py)
	r := s.data.Skills.RangeTiles
	if s.data.Skills.RangeDiagonal {
		return max(dx, dy) <= r
	}
	return dx+dy <= r
}

func (s *System) inCombat(p *world.Player) bool {
	return !p.LastCombat.IsZero() && s.clock.Now().Before(p.LastCombat.Add(s.data.Rules.LogoutCombatLock()))
}

// next returns the inventory index of the first raw fish p can cook, or -1.
// needed is the lowest level any raw fish p carries needs (0 if none).
func (s *System) next(p *world.Player) (index, needed int) {
	lvl := p.SkillLevel(world.Cooking)
	index = -1
	for i, st := range p.Inv.Slots {
		r := s.recipe[st.Item]
		if st.Empty() || r == nil {
			continue
		}
		if lvl >= r.Level {
			return i, 0
		}
		if needed == 0 || r.Level < needed {
			needed = r.Level
		}
	}
	return index, needed
}

// Handle starts or stops cooking for p.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch cmd.(type) {
	case StopCommand:
		s.stop(p, StoppedByPlayer)
	case StartCommand:
		r, level := s.start(p)
		if s.sink != nil {
			s.sink.StartResult(p.ID, r, level)
		}
	}
}

func (s *System) start(p *world.Player) (Result, int) {
	if !s.inRange(p.Pos) {
		return TooFar, 0
	}
	if s.inCombat(p) {
		return InCombat, 0
	}
	i, needed := s.next(p)
	if i < 0 {
		if needed > 0 {
			return LevelTooLow, needed
		}
		return NothingToCook, 0
	}
	if _, was := s.active[p.ID]; was {
		s.stop(p, StoppedByPlayer)
	}
	if s.gather != nil {
		s.gather.Handle(gather.StopCommand{Player: p.ID}, p)
	}
	s.active[p.ID] = &cooker{pos: p.Pos, lastCombat: p.LastCombat}
	s.order = append(s.order, p.ID)
	p.Anim = world.AnimCook
	return Started, 0
}

// stop ends p's cooking, if any, and tells the sink.
func (s *System) stop(p *world.Player, why StopReason) {
	if _, ok := s.active[p.ID]; !ok {
		return
	}
	s.drop(p.ID)
	if p.Anim == world.AnimCook {
		p.Anim = world.AnimIdle
	}
	if s.sink != nil {
		s.sink.Stopped(p.ID, why)
	}
}

func (s *System) drop(id world.PlayerID) {
	delete(s.active, id)
	for i, o := range s.order {
		if o == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

// Tick runs every cooker: it stops those whose player moved or fought, sets
// the animation of the rest and, on a combat tick, makes the attempts due.
func (s *System) Tick(w *world.World, _ time.Duration) {
	combatTick := w.TickCount()%uint64(s.cfg.TicksPerCombatTick) == 0
	// Iterate a copy: stops edit s.order.
	for _, id := range append([]world.PlayerID(nil), s.order...) {
		c := s.active[id]
		if c == nil {
			continue
		}
		p := w.Player(id)
		if p == nil {
			s.drop(id)
			continue
		}
		switch {
		case p.Pos != c.pos || p.HasDesired:
			s.stop(p, StoppedMoved)
			continue
		case !p.LastCombat.Equal(c.lastCombat):
			s.stop(p, StoppedCombat)
			continue
		}
		p.Anim = world.AnimCook
		if !combatTick {
			continue
		}
		c.ticks++
		if c.ticks >= s.data.Skills.CookIntervalTicks {
			c.ticks = 0
			s.attempt(p)
		}
	}
}

// attempt cooks the first cookable raw fish.
func (s *System) attempt(p *world.Player) {
	i, _ := s.next(p)
	if i < 0 {
		s.stop(p, StoppedNoFish)
		return
	}
	r := s.recipe[p.Inv.Slots[i].Item]
	lvl := p.SkillLevel(world.Cooking)
	burnt := s.rand.Float64() < world.BurnChance(r.BurnChance, r.Level, r.StopsBurningLevel, lvl)
	out := r.Cooked
	if burnt {
		out = r.Burnt
	}
	changes, err := p.Inv.ReplaceAt(i, out)
	if err != nil {
		s.stop(p, StoppedLocked) // a locked inventory ends cooking
		return
	}
	p.RecordEconomy(ReasonCook, changes)
	var tenths int
	var leveled bool
	level := lvl
	// OSRS default: only a cooked fish gives XP, a burnt one gives none.
	if !burnt {
		tenths = int(math.Round(r.XP * world.SkillXPScale))
		leveled, level = p.AddSkillXP(world.Cooking, tenths)
	}
	if s.sink != nil {
		s.sink.Cooked(p.ID, out, burnt, tenths, leveled, level)
	}
	if j, _ := s.next(p); j < 0 {
		s.stop(p, StoppedNoFish)
	}
}
