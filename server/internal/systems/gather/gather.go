// Package gather is the gathering half of the skills system
// (docs/specs/skills.md, Gathering; D-051): a player next to a tree or a
// fishing spot, with the level and the tool, keeps chopping or fishing on
// its own, one attempt every Skills.GatherIntervalTicks combat ticks, until
// the inventory is full, the tree falls, the spot moves, the player moves or
// combat starts.
//
// Every roll is made here with the injected random source. A success puts one
// item in the inventory, records an economy outbox event as a pick-up does,
// and adds the table's XP.
//
// Register it after the movement system with Register (every world tick:
// movement resets the animation each tick, so this system sets it again) and
// give it the number of world ticks in a combat tick. It does no I/O and
// reads time only from the injected world.Clock.
package gather

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// ReasonGather is the outbox reason of a gathered item.
const ReasonGather = "gather"

// Result is the outcome of a start request.
type Result uint8

const (
	Started       Result = 0
	NoSuchTarget  Result = 1 // unknown id, or a fishing spot that isn't out right now
	TooFar        Result = 2
	NoTool        Result = 3
	LevelTooLow   Result = 4
	InventoryFull Result = 5
	InCombat      Result = 6
	Felled        Result = 7 // the tree is a stump
)

var resultNames = [...]string{"started", "no_such_target", "too_far", "no_tool", "level_too_low",
	"inventory_full", "in_combat", "felled"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return fmt.Sprintf("Result(%d)", uint8(r))
}

// StopReason is why gathering ended after it had started.
type StopReason uint8

const (
	StoppedByPlayer  StopReason = iota // a StopCommand or a new start elsewhere
	StoppedFull                        // the inventory is full
	StoppedFelled                      // the tree fell
	StoppedMoved                       // the player moved
	StoppedCombat                      // combat started
	StoppedSpotMoved                   // the fishing spot moved away
	StoppedNoTool                      // the tool is gone
	StoppedLevel                       // the level no longer reaches (never in practice; levels only rise)
)

var stopNames = [...]string{"player", "inventory_full", "felled", "moved", "combat", "spot_moved", "no_tool", "level"}

func (r StopReason) String() string {
	if int(r) < len(stopNames) {
		return stopNames[r]
	}
	return fmt.Sprintf("StopReason(%d)", uint8(r))
}

// StartCommand asks Player to start gathering at the tree or fishing spot with
// the map id Place (for example "oak-3" or "rod-2").
type StartCommand struct {
	Player world.PlayerID
	Place  string
}

func (c StartCommand) Target() world.PlayerID { return c.Player }

// StopCommand asks Player to stop gathering.
type StopCommand struct{ Player world.PlayerID }

func (c StopCommand) Target() world.PlayerID { return c.Player }

// Sink receives what the player must be told; the session implements it.
// It is called on the tick goroutine and must not block. A nil Sink is fine.
type Sink interface {
	// StartResult is sent for every StartCommand.
	StartResult(to world.PlayerID, r Result)
	// Stopped is sent when gathering that had started ends by itself or by a
	// StopCommand.
	Stopped(to world.PlayerID, why StopReason)
	// Gained is sent for each success: the skill, the XP in tenths, and
	// whether the player reached a new level (then level is the new one).
	Gained(to world.PlayerID, s world.Skill, xpTenths int, leveled bool, level int)
}

// Config tunes the system.
type Config struct {
	// TicksPerCombatTick is how many world ticks make one combat tick (12).
	TicksPerCombatTick int
	// TileSize is the length of a tile in metres.
	TileSize float64
}

// DefaultConfig is the 20 Hz world with a combat tick every 12th tick.
func DefaultConfig() Config { return Config{TicksPerCombatTick: 12, TileSize: TileSize} }

type tree struct {
	Place
	def *gamedata.Tree
	// regrow is when a felled tree stands again; zero while it stands.
	regrow time.Time
}

type spotKind struct {
	def    *gamedata.FishingSpot
	slots  []*slot
	places []Place
}

// slot is one fishing spot out at the pond; it holds a candidate position
// and moves to another one of its kind every so often.
type slot struct {
	at     int // index into spotKind.places
	moveAt time.Time
}

// target is what a gatherer works at.
type target struct {
	id    string
	tree  *tree
	spot  *spotKind
	place Place
}

type gatherer struct {
	target     target
	pos        world.Vec2
	lastCombat time.Time
	ticks      int // combat ticks since the last attempt
}

// System runs gathering.
type System struct {
	data  *gamedata.Data
	clock world.Clock
	rand  formula.Rand
	sink  Sink
	cfg   Config

	trees   map[string]*tree
	spots   map[string]*spotKind // by spot kind (net, rod)
	byPlace map[string]*spotKind // by candidate id
	kinds   []*spotKind          // deterministic order
	active  map[world.PlayerID]*gatherer
	order   []world.PlayerID // gatherers in start order, for deterministic ticking
}

// New creates the gathering system from the layout and the skill tables in d.
// Every tree kind and spot kind in the layout needs a row in the tables.
func New(d *gamedata.Data, l Layout, clock world.Clock, r formula.Rand, sink Sink, cfg Config) (*System, error) {
	if cfg.TicksPerCombatTick < 1 || cfg.TileSize <= 0 {
		return nil, errors.New("gather: bad config")
	}
	s := &System{
		data: d, clock: clock, rand: r, sink: sink, cfg: cfg,
		trees:   map[string]*tree{},
		spots:   map[string]*spotKind{},
		byPlace: map[string]*spotKind{},
		active:  map[world.PlayerID]*gatherer{},
	}
	for _, p := range l.Trees {
		var def *gamedata.Tree
		for i := range d.Skills.Trees {
			if d.Skills.Trees[i].Key == p.Kind {
				def = &d.Skills.Trees[i]
			}
		}
		if def == nil {
			return nil, fmt.Errorf("gather: tree %q: no %q row in the tree table", p.ID, p.Kind)
		}
		s.trees[p.ID] = &tree{Place: p, def: def}
	}
	now := clock.Now()
	for _, p := range l.Spots {
		k := s.spots[p.Kind]
		if k == nil {
			var def *gamedata.FishingSpot
			for i := range d.Skills.FishingSpots {
				if d.Skills.FishingSpots[i].Key == p.Kind+"_spot" {
					def = &d.Skills.FishingSpots[i]
				}
			}
			if def == nil {
				return nil, fmt.Errorf("gather: spot %q: no %q row in the fishing table", p.ID, p.Kind+"_spot")
			}
			k = &spotKind{def: def}
			s.spots[p.Kind] = k
			s.kinds = append(s.kinds, k)
		}
		k.places = append(k.places, p)
		s.byPlace[p.ID] = k
	}
	for kind, k := range s.spots {
		n := l.Active[kind]
		if n < 1 || n > len(k.places) {
			return nil, fmt.Errorf("gather: %d active %q spots of %d candidates", n, kind, len(k.places))
		}
		for i := 0; i < n; i++ {
			k.slots = append(k.slots, &slot{at: i, moveAt: now.Add(s.moveDelay())})
		}
	}
	return s, nil
}

func (s *System) Name() string { return "gather" }

// moveDelay is a random time between the spot move bounds.
func (s *System) moveDelay() time.Duration {
	lo, hi := s.data.Skills.SpotMoveMin(), s.data.Skills.SpotMoveMax()
	span := int64(hi-lo) / int64(time.Second)
	return lo + time.Duration(s.rand.IntN(int(span)+1))*time.Second
}

// Gathering reports whether p is gathering, and at which map id.
func (s *System) Gathering(id world.PlayerID) (string, bool) {
	g, ok := s.active[id]
	if !ok {
		return "", false
	}
	return g.target.id, true
}

// TreeStanding reports whether the tree with the map id stands (false for a
// stump or an unknown id).
func (s *System) TreeStanding(id string) bool {
	t := s.trees[id]
	return t != nil && s.standing(t)
}

func (s *System) standing(t *tree) bool {
	return t.regrow.IsZero() || !s.clock.Now().Before(t.regrow)
}

// ActiveSpots appends to dst the fishing spots out at the pond right now, kinds
// in file order, and returns it.
func (s *System) ActiveSpots(dst []Place) []Place {
	for _, k := range s.kinds {
		for _, sl := range k.slots {
			dst = append(dst, k.places[sl.at])
		}
	}
	return dst
}

func (s *System) spotOut(k *spotKind, id string) (Place, bool) {
	for _, sl := range k.slots {
		if p := k.places[sl.at]; p.ID == id {
			return p, true
		}
	}
	return Place{}, false
}

// tile is the tile a position is in.
func (s *System) tile(v world.Vec2) (int, int) {
	return int(math.Floor(float64(v.X) / s.cfg.TileSize)), int(math.Floor(float64(v.Y) / s.cfg.TileSize))
}

// inRange reports whether pos is within Skills.RangeTiles tiles of at, the
// diagonals included when Skills.RangeDiagonal is set.
func (s *System) inRange(pos, at world.Vec2) bool {
	px, py := s.tile(pos)
	ax, ay := s.tile(at)
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

// tool is the best usable tool p carries for skill sk at skill level lvl: the
// highest bonus among inventory items and the weapon in hand, and whether
// p carries any tool for the skill at all. want, if not zero, is the only item
// that counts.
func (s *System) tool(p *world.Player, sk gamedata.Skill, want gamedata.ItemID, lvl int) (bonus float64, usable, carried bool) {
	consider := func(id gamedata.ItemID) {
		if id == 0 || (want != 0 && id != want) {
			return
		}
		it, ok := s.data.Item(id)
		if !ok || it.ToolSkill != sk {
			return
		}
		carried = true
		if it.ToolLevel <= lvl && (!usable || it.ToolBonus > bonus) {
			bonus, usable = it.ToolBonus, true
		}
	}
	for _, st := range p.Inv.Slots {
		consider(st.Item)
	}
	consider(p.Inv.Equipment.Weapon)
	return bonus, usable, carried
}

// requirements checks the target's level and tool for p and returns the
// skill, level and tool bonus.
func (s *System) requirements(p *world.Player, t target) (sk world.Skill, lvl int, bonus float64, res Result) {
	if t.tree != nil {
		sk, lvl = world.Woodcutting, p.SkillLevel(world.Woodcutting)
		if lvl < t.tree.def.Level {
			return sk, lvl, 0, LevelTooLow
		}
		b, usable, carried := s.tool(p, gamedata.SkillWoodcutting, 0, lvl)
		if !carried {
			return sk, lvl, 0, NoTool
		}
		if !usable {
			return sk, lvl, 0, LevelTooLow
		}
		return sk, lvl, b, Started
	}
	sk, lvl = world.Fishing, p.SkillLevel(world.Fishing)
	if lvl < t.spot.def.Level {
		return sk, lvl, 0, LevelTooLow
	}
	b, usable, carried := s.tool(p, gamedata.SkillFishing, t.spot.def.Tool, lvl)
	if !carried {
		return sk, lvl, 0, NoTool
	}
	if !usable {
		return sk, lvl, 0, LevelTooLow
	}
	return sk, lvl, b, Started
}

func (s *System) resolve(id string) (target, Result) {
	if t, ok := s.trees[id]; ok {
		if !s.standing(t) {
			return target{}, Felled
		}
		return target{id: id, tree: t, place: t.Place}, Started
	}
	if k, ok := s.byPlace[id]; ok {
		if pl, out := s.spotOut(k, id); out {
			return target{id: id, spot: k, place: pl}, Started
		}
	}
	return target{}, NoSuchTarget
}

// Handle starts or stops gathering for p.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case StopCommand:
		s.stop(p, StoppedByPlayer)
	case StartCommand:
		r := s.start(p, c.Place)
		if s.sink != nil {
			s.sink.StartResult(p.ID, r)
		}
	}
}

func (s *System) start(p *world.Player, id string) Result {
	t, r := s.resolve(id)
	if r != Started {
		return r
	}
	if !s.inRange(p.Pos, t.place.Pos) {
		return TooFar
	}
	if _, _, _, r := s.requirements(p, t); r != Started {
		return r
	}
	if s.inCombat(p) {
		return InCombat
	}
	if p.Inv.FreeSlots() == 0 {
		return InventoryFull
	}
	if _, was := s.active[p.ID]; was {
		s.stop(p, StoppedByPlayer)
	}
	s.active[p.ID] = &gatherer{target: t, pos: p.Pos, lastCombat: p.LastCombat}
	s.order = append(s.order, p.ID)
	p.Anim = s.anim(t)
	return Started
}

func (s *System) anim(t target) world.Anim {
	if t.tree != nil {
		return world.AnimChop
	}
	return world.AnimFish
}

// stop ends p's gathering, if any, and tells the sink.
func (s *System) stop(p *world.Player, why StopReason) {
	if _, ok := s.active[p.ID]; !ok {
		return
	}
	s.drop(p.ID)
	if p.Anim == world.AnimChop || p.Anim == world.AnimFish {
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

// Tick moves the fishing spots, then runs every gatherer: it stops those whose
// player moved or fought, sets the animation of the rest and, on a combat
// tick, makes the attempts that are due.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.moveSpots(w)
	combatTick := w.TickCount()%uint64(s.cfg.TicksPerCombatTick) == 0
	// Iterate a copy: stops edit s.order.
	for _, id := range append([]world.PlayerID(nil), s.order...) {
		g := s.active[id]
		if g == nil {
			continue
		}
		p := w.Player(id)
		if p == nil {
			s.drop(id)
			continue
		}
		switch {
		case g.target.tree != nil && !s.standing(g.target.tree):
			s.stop(p, StoppedFelled)
			continue
		case p.Pos != g.pos || p.HasDesired:
			s.stop(p, StoppedMoved)
			continue
		case !p.LastCombat.Equal(g.lastCombat):
			s.stop(p, StoppedCombat)
			continue
		}
		p.Anim = s.anim(g.target)
		if !combatTick {
			continue
		}
		g.ticks++
		if g.ticks >= s.data.Skills.GatherIntervalTicks {
			g.ticks = 0
			s.attempt(p, g)
		}
	}
}

// moveSpots moves each fishing spot whose time is up to another candidate
// position of its kind that no spot holds, and stops whoever was fishing it.
func (s *System) moveSpots(w *world.World) {
	now := s.clock.Now()
	for _, k := range s.kinds {
		for _, sl := range k.slots {
			if now.Before(sl.moveAt) {
				continue
			}
			sl.moveAt = now.Add(s.moveDelay())
			free := make([]int, 0, len(k.places))
			for i := range k.places {
				if !k.held(i) {
					free = append(free, i)
				}
			}
			if len(free) == 0 {
				continue
			}
			old := k.places[sl.at].ID
			sl.at = free[s.rand.IntN(len(free))]
			for _, id := range append([]world.PlayerID(nil), s.order...) {
				if g := s.active[id]; g != nil && g.target.id == old {
					if p := w.Player(id); p != nil {
						s.stop(p, StoppedSpotMoved)
					} else {
						s.drop(id)
					}
				}
			}
		}
	}
}

func (k *spotKind) held(i int) bool {
	for _, sl := range k.slots {
		if sl.at == i {
			return true
		}
	}
	return false
}

// attempt is one chop or cast.
func (s *System) attempt(p *world.Player, g *gatherer) {
	t := g.target
	if p.Inv.FreeSlots() == 0 {
		s.stop(p, StoppedFull)
		return
	}
	sk, lvl, bonus, res := s.requirements(p, t)
	if res != Started {
		why := StoppedNoTool
		if res == LevelTooLow {
			why = StoppedLevel
		}
		s.stop(p, why)
		return
	}
	var item gamedata.ItemID
	var xp float64
	if t.tree != nil {
		d := t.tree.def
		if s.rand.Float64() >= world.GatherChance(d.ChanceLow, d.ChanceHigh, bonus, lvl) {
			return
		}
		item, xp = d.Log, d.XP
	} else {
		d := t.spot.def
		chance := world.GatherChance(d.ChanceLow, d.ChanceHigh, bonus, lvl)
		// Best fish the level allows first, then the next one down.
		for i := len(d.Catches) - 1; i >= 0; i-- {
			c := d.Catches[i]
			if lvl >= c.Level && s.rand.Float64() < chance {
				item, xp = c.Item, c.XP
				break
			}
		}
		if item == 0 {
			return
		}
	}
	changes, err := p.Inv.Add(s.data, item, 1)
	if err != nil {
		if errors.Is(err, world.ErrInventoryFull) {
			s.stop(p, StoppedFull)
		}
		return // a locked inventory skips the attempt
	}
	p.RecordEconomy(ReasonGather, changes)
	tenths := int(math.Round(xp * world.SkillXPScale))
	leveled, level := p.AddSkillXP(sk, tenths)
	if s.sink != nil {
		s.sink.Gained(p.ID, sk, tenths, leveled, level)
	}
	if t.tree != nil && s.rand.IntN(s.data.Skills.TreeFallOneIn) == 0 {
		s.fell(t.tree)
		s.stop(p, StoppedFelled)
		return
	}
	if p.Inv.FreeSlots() == 0 {
		s.stop(p, StoppedFull)
	}
}

// fell turns the tree into a stump until its respawn time passes. Other
// players chopping it stop on their next check in Tick.
func (s *System) fell(t *tree) {
	t.regrow = s.clock.Now().Add(t.def.Respawn())
}
