// Package emote is the emote system. Only Wave exists (PRD 4.1).
package emote

import (
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

// Config holds emote timing in world ticks. Values come from Q4 (#96,
// docs/specs/gameplay.md): a wave shows for 2 s (40 ticks at 20 Hz), a new
// wave is ignored while one is showing, and walking ends it.
type Config struct {
	WaveDurationTicks int // ticks Anim stays AnimWave
	WaveCooldownTicks int // ticks after a wave starts during which new waves are ignored
}

// DefaultConfig returns the Q4 values.
func DefaultConfig() Config { return Config{WaveDurationTicks: 40, WaveCooldownTicks: 40} }

// WaveCommand asks a player to wave.
type WaveCommand struct{ Player world.PlayerID }

func (c WaveCommand) Target() world.PlayerID { return c.Player }

type state struct{ showing, cooldown int }

// System shows waves as the player's animation.
//
// Register it after the movement system: movement resets Anim each tick and
// this system then overrides it, and uses AnimWalk to detect walking.
type System struct {
	cfg    Config
	active map[world.PlayerID]*state
}

// New creates the emote system.
func New(cfg Config) *System {
	return &System{cfg: cfg, active: map[world.PlayerID]*state{}}
}

func (s *System) Name() string { return "emote" }

// Handle starts a wave unless one is on cooldown or the player is walking.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	if _, ok := cmd.(WaveCommand); !ok || s.cfg.WaveDurationTicks <= 0 {
		return
	}
	st := s.active[p.ID]
	if st != nil && st.cooldown > 0 {
		return
	}
	if p.HasDesired {
		return
	}
	if st == nil {
		st = &state{}
		s.active[p.ID] = st
	}
	st.showing, st.cooldown = s.cfg.WaveDurationTicks, s.cfg.WaveCooldownTicks
}

// Tick applies AnimWave while a wave shows and ends it when the player walks.
func (s *System) Tick(w *world.World, _ time.Duration) {
	for id, st := range s.active {
		p := w.Player(id)
		if p == nil {
			delete(s.active, id)
			continue
		}
		if st.showing > 0 {
			if p.Anim == world.AnimWalk {
				st.showing = 0
			} else {
				p.Anim = world.AnimWave
				st.showing--
			}
		}
		if st.cooldown > 0 {
			st.cooldown--
		}
		if st.showing == 0 && st.cooldown == 0 {
			delete(s.active, id)
		}
	}
}
