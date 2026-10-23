// Package movement is the server-authoritative movement system.
package movement

import (
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

// Config holds movement tuning.
type Config struct {
	// MaxSpeed in world units per second. PLACEHOLDER: the real value is TBD
	// (not decided in the PRD).
	MaxSpeed float32
}

// DefaultConfig returns placeholder values.
func DefaultConfig() Config { return Config{MaxSpeed: 6} }

// MoveCommand asks to move a player to a desired position.
type MoveCommand struct {
	Player world.PlayerID
	To     world.Vec2
}

func (c MoveCommand) Target() world.PlayerID { return c.Player }

// System clamps desired moves to the maximum speed per tick.
type System struct{ cfg Config }

// New creates the movement system.
func New(cfg Config) *System { return &System{cfg: cfg} }

func (s *System) Name() string { return "movement" }

// Handle records the desired position; it is never applied unclamped.
// Non-finite targets (NaN, ±Inf) are dropped so they can't corrupt Pos.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	if m, ok := cmd.(MoveCommand); ok && finite(m.To) {
		p.Desired, p.HasDesired = m.To, true
	}
}

func finite(v world.Vec2) bool {
	x, y := float64(v.X), float64(v.Y)
	return !math.IsNaN(x) && !math.IsInf(x, 0) && !math.IsNaN(y) && !math.IsInf(y, 0)
}

// Tick moves each player toward its desired position by at most
// MaxSpeed*dt, so over-speed moves and teleports are clamped.
func (s *System) Tick(w *world.World, dt time.Duration) {
	// Distances are computed in float64 so huge float32 targets don't overflow.
	maxStep := float64(s.cfg.MaxSpeed) * dt.Seconds()
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
		if dist <= maxStep {
			p.Pos = p.Desired
			p.HasDesired = false
			continue
		}
		k := maxStep / dist
		p.Pos.X = float32(float64(p.Pos.X) + dx*k)
		p.Pos.Y = float32(float64(p.Pos.Y) + dy*k)
	}
}
