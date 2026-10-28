// Package movement is the server-authoritative movement system.
package movement

import (
	"fmt"
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

// Config holds movement tuning.
type Config struct {
	// MaxSpeed in world units per second. There is deliberately no default:
	// the walk speed is not decided in the PRD, so the caller must supply it.
	MaxSpeed float32
	// RunSpeed in world units per second while the player is running (D-044).
	// Zero means no separate run speed: running moves at MaxSpeed. When set
	// it must be finite and at least MaxSpeed.
	RunSpeed float32
	// Collision, if set, blocks moves into blocked cells (sliding along walls).
	Collision *world.Grid
}

// MoveCommand asks to move a player to a desired position.
type MoveCommand struct {
	Player world.PlayerID
	To     world.Vec2
	// Run asks to move at the run speed rather than the walk speed.
	Run bool
}

func (c MoveCommand) Target() world.PlayerID { return c.Player }

// System clamps desired moves to the maximum speed per tick.
type System struct{ cfg Config }

// New creates the movement system. MaxSpeed must be positive and finite;
// RunSpeed must be zero (run at MaxSpeed) or finite and at least MaxSpeed.
func New(cfg Config) (*System, error) {
	v := float64(cfg.MaxSpeed)
	if !(v > 0) || math.IsInf(v, 0) {
		return nil, fmt.Errorf("movement: max speed %v must be positive and finite", cfg.MaxSpeed)
	}
	r := float64(cfg.RunSpeed)
	if r != 0 && (!(r >= v) || math.IsInf(r, 0)) {
		return nil, fmt.Errorf("movement: run speed %v must be 0 or finite and at least max speed %v", cfg.RunSpeed, cfg.MaxSpeed)
	}
	if r == 0 {
		cfg.RunSpeed = cfg.MaxSpeed
	}
	return &System{cfg: cfg}, nil
}

func (s *System) Name() string { return "movement" }

// Handle records the desired position; it is never applied unclamped.
// Non-finite targets (NaN, ±Inf) are dropped so they can't corrupt Pos.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	if m, ok := cmd.(MoveCommand); ok && finite(m.To) {
		p.Desired, p.HasDesired, p.Running = m.To, true, m.Run
	}
}

func finite(v world.Vec2) bool {
	x, y := float64(v.X), float64(v.Y)
	return !math.IsNaN(x) && !math.IsInf(x, 0) && !math.IsNaN(y) && !math.IsInf(y, 0)
}

// Tick moves each player toward its desired position by at most
// MaxSpeed*dt (RunSpeed*dt while running), so over-speed moves and
// teleports are clamped.
func (s *System) Tick(w *world.World, dt time.Duration) {
	// Distances are computed in float64 so huge float32 targets don't overflow.
	walkStep := float64(s.cfg.MaxSpeed) * dt.Seconds()
	runStep := float64(s.cfg.RunSpeed) * dt.Seconds()
	for _, p := range w.Players() {
		if !p.HasDesired {
			p.Anim = world.AnimIdle
			continue
		}
		dx := float64(p.Desired.X) - float64(p.Pos.X)
		dy := float64(p.Desired.Y) - float64(p.Pos.Y)
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist == 0 {
			p.HasDesired = false
			p.Anim = world.AnimIdle
			continue
		}
		p.Facing = float32(math.Atan2(dy, dx))
		p.Anim = world.AnimWalk
		maxStep := walkStep
		if p.Running {
			maxStep = runStep
		}
		next := p.Desired
		if dist > maxStep {
			k := maxStep / dist
			next = world.Vec2{X: float32(float64(p.Pos.X) + dx*k), Y: float32(float64(p.Pos.Y) + dy*k)}
		}
		if s.cfg.Collision != nil {
			next = s.slide(p.Pos, next)
			if next == p.Pos { // wall in the way: stop pushing
				p.HasDesired = false
				p.Anim = world.AnimIdle
				continue
			}
		}
		p.Pos = next
		if p.Pos == p.Desired {
			p.HasDesired = false
		}
	}
}

// slide returns where a move from → to ends up: the target if free, else the
// X-only or Y-only part of the move, else from. The move is subdivided so a
// step never jumps over a cell.
func (s *System) slide(from, to world.Vec2) world.Vec2 {
	g := s.cfg.Collision
	dx, dy := float64(to.X-from.X), float64(to.Y-from.Y)
	n := int(math.Ceil(math.Max(math.Abs(dx), math.Abs(dy))/(g.CellSize()/2))) + 1
	cur := from
	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		want := world.Vec2{X: float32(float64(from.X) + dx*t), Y: float32(float64(from.Y) + dy*t)}
		if i == n {
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
