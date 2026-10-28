// Package npc moves NPCs: each tick it walks every NPC with a goal toward it
// at walk speed, respecting the collision grid and the safe circle, and sends
// an NPC that strays past its leash back home (docs/specs/gameplay.md, Combat).
package npc

import (
	"fmt"
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// Config holds NPC movement tuning. There are no defaults: every value comes
// from the server config or the gameplay data files.
type Config struct {
	// WalkSpeed in metres per second (D-044, the server's walk speed).
	WalkSpeed float32
	// LeashTiles is gamedata Rules.LeashTiles: an NPC farther than this from
	// home gives up and walks home.
	LeashTiles int
	// TileSize is the length of one gameplay tile in metres, used to turn
	// tile distances from the rules into metres.
	TileSize float64
	// Collision, if set, blocks moves into blocked cells (sliding along walls).
	Collision *world.Grid
	// Areas is required: NPCs never enter its safe circle.
	Areas *areas.Lookup
}

// safeMargin keeps an NPC walking around the safe circle just outside its
// boundary, which InSafeCircle counts as inside.
const safeMargin = 0.01 // metres

// System is the NPC movement system.
type System struct {
	cfg   Config
	leash float64 // metres
}

// New creates the NPC system.
func New(cfg Config) (*System, error) {
	if v := float64(cfg.WalkSpeed); !(v > 0) || math.IsInf(v, 0) {
		return nil, fmt.Errorf("npc: walk speed %v must be positive and finite", cfg.WalkSpeed)
	}
	if cfg.LeashTiles <= 0 {
		return nil, fmt.Errorf("npc: leash %d tiles must be positive", cfg.LeashTiles)
	}
	if !(cfg.TileSize > 0) || math.IsInf(cfg.TileSize, 0) {
		return nil, fmt.Errorf("npc: tile size %v must be positive and finite", cfg.TileSize)
	}
	if cfg.Areas == nil {
		return nil, fmt.Errorf("npc: areas lookup is required")
	}
	return &System{cfg: cfg, leash: float64(cfg.LeashTiles) * cfg.TileSize}, nil
}

func (s *System) Name() string { return "npc" }

// Handle does nothing: players don't command NPCs.
func (s *System) Handle(world.Command, *world.Player) {}

// Tick leashes and then steps every NPC.
func (s *System) Tick(w *world.World, dt time.Duration) {
	for _, n := range w.NPCs() {
		s.Leash(n)
		s.Step(n, dt)
	}
}

// Leash sets home as the goal of an NPC more than the leash distance from
// home, and marks it as returning until it gets there. A returning NPC keeps
// home as its goal.
func (s *System) Leash(n *world.NPC) {
	if n.Returning || dist(n.Pos, n.Home) > s.leash {
		n.Goal, n.HasGoal, n.Returning = n.Home, true, true
	}
}

// Step walks n toward its goal by at most WalkSpeed*dt. A move into a wall
// slides along it; a move into the safe circle walks around its edge instead.
// When no move gets the NPC closer, it stops and drops the goal.
func (s *System) Step(n *world.NPC, dt time.Duration) {
	if !n.HasGoal {
		n.Anim = world.AnimIdle
		return
	}
	d := dist(n.Pos, n.Goal)
	if d == 0 || math.IsNaN(d) || math.IsInf(d, 0) {
		s.arrive(n)
		return
	}
	maxStep := float64(s.cfg.WalkSpeed) * dt.Seconds()
	next := n.Goal
	if d > maxStep {
		next = toward(n.Pos, n.Goal, maxStep)
	}
	next = s.slide(n.Pos, next)
	ok := dist(next, n.Goal) < d
	if s.cfg.Areas.InSafeCircle(next) {
		next, ok = s.around(n.Pos, n.Goal, maxStep)
		next = s.slide(n.Pos, next)
	}
	if !ok || next == n.Pos || s.cfg.Areas.InSafeCircle(next) {
		n.HasGoal = false
		n.Anim = world.AnimIdle
		return
	}
	n.Facing = float32(math.Atan2(float64(next.Y-n.Pos.Y), float64(next.X-n.Pos.X)))
	n.Anim = world.AnimWalk
	n.Pos = next
	if n.Pos == n.Goal {
		s.arrive(n)
	}
}

func (s *System) arrive(n *world.NPC) {
	n.HasGoal = false
	n.Anim = world.AnimIdle
	if n.Pos == n.Home {
		n.Returning = false
	}
}

// around returns a step of arc length step from pos around the safe circle's
// centre, in the direction that brings it closer to goal, kept just outside
// the circle. ok is false when going around brings it no closer (pos is the
// point of the edge nearest the goal).
func (s *System) around(pos, goal world.Vec2, step float64) (next world.Vec2, ok bool) {
	c := s.cfg.Areas.Spawn()
	cx, cy := float64(c.X), float64(c.Y)
	px, py := float64(pos.X)-cx, float64(pos.Y)-cy
	r := math.Hypot(px, py)
	if r == 0 {
		return pos, false
	}
	gx, gy := float64(goal.X)-cx, float64(goal.Y)-cy
	a := step / r
	if px*gy-py*gx < 0 { // goal is clockwise of pos
		a = -a
	}
	sin, cos := math.Sincos(a)
	nx, ny := px*cos-py*sin, px*sin+py*cos
	// Compare at the same radius so only the rotation counts as progress.
	if !(math.Hypot(nx-gx, ny-gy) < math.Hypot(px-gx, py-gy)) {
		return pos, false
	}
	if min := s.cfg.Areas.SafeRadius() + safeMargin; r < min {
		nx, ny = nx*min/r, ny*min/r
	}
	return world.Vec2{X: float32(cx + nx), Y: float32(cy + ny)}, true
}

// slide returns where a move from → to ends up: the target if free, else the
// X-only or Y-only part of the move, else from. The move is subdivided so a
// step never jumps over a cell. Without a collision grid it returns to.
func (s *System) slide(from, to world.Vec2) world.Vec2 {
	g := s.cfg.Collision
	if g == nil {
		return to
	}
	dx, dy := float64(to.X-from.X), float64(to.Y-from.Y)
	steps := int(math.Ceil(math.Max(math.Abs(dx), math.Abs(dy))/(g.CellSize()/2))) + 1
	cur := from
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		want := world.Vec2{X: float32(float64(from.X) + dx*t), Y: float32(float64(from.Y) + dy*t)}
		if i == steps {
			want = to
		}
		switch {
		case !g.Blocked(want):
			cur = want
		case !g.Blocked(world.Vec2{X: want.X, Y: cur.Y}):
			cur.X = want.X
		case !g.Blocked(world.Vec2{X: cur.X, Y: want.Y}):
			cur.Y = want.Y
		default:
			return cur
		}
	}
	return cur
}

func toward(from, to world.Vec2, step float64) world.Vec2 {
	dx, dy := float64(to.X)-float64(from.X), float64(to.Y)-float64(from.Y)
	k := step / math.Hypot(dx, dy)
	return world.Vec2{X: float32(float64(from.X) + dx*k), Y: float32(float64(from.Y) + dy*k)}
}

func dist(a, b world.Vec2) float64 {
	return math.Hypot(float64(a.X)-float64(b.X), float64(a.Y)-float64(b.Y))
}
