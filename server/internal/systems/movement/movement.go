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
func (s *System) Handle(cmd world.Command, p *world.Player) {
	if m, ok := cmd.(MoveCommand); ok {
		p.Desired, p.HasDesired = m.To, true
	}
}

// Tick moves each player toward its desired position by at most
// MaxSpeed*dt, so over-speed moves and teleports are clamped.
func (s *System) Tick(w *world.World, dt time.Duration) {
	maxStep := s.cfg.MaxSpeed * float32(dt.Seconds())
	for _, p := range w.Players() {
		if !p.HasDesired {
			p.Anim = world.AnimIdle
			continue
		}
		dx, dy := p.Desired.X-p.Pos.X, p.Desired.Y-p.Pos.Y
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if dist == 0 {
			p.HasDesired = false
			p.Anim = world.AnimIdle
			continue
		}
		p.Facing = float32(math.Atan2(float64(dy), float64(dx)))
		p.Anim = world.AnimWalk
		if dist <= maxStep {
			p.Pos = p.Desired
			p.HasDesired = false
			continue
		}
		k := maxStep / dist
		p.Pos.X += dx * k
		p.Pos.Y += dy * k
	}
}
