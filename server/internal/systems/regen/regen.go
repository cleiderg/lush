// Package regen is player health regeneration (docs/specs/gameplay.md,
// Combat): Rules.RegenHP every Rules.RegenInterval, starting Rules.RegenDelay
// after the player last dealt or took damage, never above max HP.
package regen

import (
	"fmt"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// System restores player HP on a fixed schedule. Heals fall at
// LastCombat + delay + k × interval for k = 0, 1, 2, ...; a player who has
// never been in combat heals every interval from when the system first saw them.
type System struct {
	rules    gamedata.Rules
	clock    world.Clock
	delay    time.Duration
	interval time.Duration
	state    map[world.PlayerID]*playerState
	gen      uint64
}

type playerState struct {
	checked   time.Time // heals up to and including this time are applied
	firstSeen time.Time // schedule anchor when LastCombat is zero
	gen       uint64
}

// New creates the system. It reports an error if the rules can't drive a
// schedule (non-positive RegenHP or interval, negative delay).
func New(rules gamedata.Rules, clock world.Clock) (*System, error) {
	if rules.RegenHP <= 0 || rules.RegenIntervalSeconds <= 0 || rules.RegenDelaySeconds < 0 {
		return nil, fmt.Errorf("regen: need positive regen_hp and regen_interval_s and non-negative regen_delay_s, got %d, %d, %d",
			rules.RegenHP, rules.RegenIntervalSeconds, rules.RegenDelaySeconds)
	}
	return &System{
		rules:    rules,
		clock:    clock,
		delay:    rules.RegenDelay(),
		interval: rules.RegenInterval(),
		state:    make(map[world.PlayerID]*playerState),
	}, nil
}

func (s *System) Name() string { return "regen" }

// Handle does nothing: regeneration takes no commands.
func (s *System) Handle(world.Command, *world.Player) {}

// Tick applies every heal due since the previous Tick.
func (s *System) Tick(w *world.World, _ time.Duration) {
	now := s.clock.Now()
	s.gen++
	players := w.Players()
	for _, p := range players {
		st := s.state[p.ID]
		if st == nil {
			st = &playerState{checked: now, firstSeen: now}
			s.state[p.ID] = st
		}
		st.gen = s.gen
		if now.After(st.checked) {
			if n := s.heals(p.LastCombat, st, now); n > 0 {
				p.Heal(s.rules, n*s.rules.RegenHP)
			}
			st.checked = now
		}
	}
	if len(s.state) > len(players) {
		for id, st := range s.state {
			if st.gen != s.gen {
				delete(s.state, id)
			}
		}
	}
}

// heals counts schedule points in (st.checked, now].
func (s *System) heals(lastCombat time.Time, st *playerState, now time.Time) int {
	start := st.firstSeen
	if !lastCombat.IsZero() {
		start = lastCombat.Add(s.delay)
	}
	return s.upTo(start, now) - s.upTo(start, st.checked)
}

// upTo counts schedule points start + k × interval that are at or before t.
func (s *System) upTo(start, t time.Time) int {
	if t.Before(start) {
		return 0
	}
	return int(t.Sub(start)/s.interval) + 1
}
