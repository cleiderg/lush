// Package questevents connects the quest engine (package quest) to the world
// (docs/specs/quests.md). It keeps each player's quest state and feeds it the
// game events that move quests on:
//
//   - kills: Sink wraps the combat event sink, so the killer's quests count
//     the kill and the NPC's quest drops join the loot (the session spawns
//     them on the ground reserved for the killer, like any other drop);
//   - levels: Tick compares each player's combat and skill levels with the
//     last ones it saw, so every source of XP is covered;
//   - pick-ups: PickedUp is the grounditems pick-up hook.
//
// Players join with Load (their stored records) and leave with Forget. Drain
// reports the players whose quest state changed since the last Drain, once.
// Finished quests are reported in Change.Completed for whoever commits the
// rewards; this package does not.
//
// It does no I/O and uses no clock. Only the tick goroutine may call it.
package questevents

import (
	"fmt"
	"slices"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/quest"
	"github.com/cleiderg/lush/server/internal/world"
)

// Players looks up players by id. *world.World implements it.
type Players interface {
	Player(id world.PlayerID) *world.Player
}

// Change is one player's quest state after it changed.
type Change struct {
	Player world.PlayerID
	// Records is the player's whole quest state, sorted by quest id.
	Records []quest.Record
	// Completed are quests finished since the last Drain.
	Completed []quest.Completion
}

type entry struct {
	state     quest.State
	combat    int
	skills    [world.NumSkills]int
	dirty     bool
	completed []quest.Completion
}

// System is the quest event system. Register it with the world; it needs no
// commands.
type System struct {
	data    *gamedata.Data
	engine  *quest.Engine
	players Players
	rand    quest.Rand

	entries map[world.PlayerID]*entry
	order   []world.PlayerID // players with changes, in the order they changed
}

// New creates the system. rand drives quest drop chances.
func New(d *gamedata.Data, players Players, r quest.Rand) *System {
	return &System{
		data:    d,
		engine:  quest.New(d),
		players: players,
		rand:    r,
		entries: make(map[world.PlayerID]*entry),
	}
}

func (s *System) Name() string                        { return "quest_events" }
func (s *System) Handle(world.Command, *world.Player) {}

// Load gives joining player p their stored quest records. It replaces any
// state the player had. A bad record is an error and changes nothing.
func (s *System) Load(p *world.Player, recs []quest.Record) error {
	st, err := s.engine.Decode(recs)
	if err != nil {
		return fmt.Errorf("load quests for player %d: %w", p.ID, err)
	}
	e := s.newEntry(p)
	e.state = st
	s.entries[p.ID] = e
	return nil
}

// Forget drops a leaving player's state and any change not yet drained.
func (s *System) Forget(id world.PlayerID) {
	delete(s.entries, id)
	s.order = slices.DeleteFunc(s.order, func(o world.PlayerID) bool { return o == id })
}

// Records is the player's quest state as plain records.
func (s *System) Records(id world.PlayerID) []quest.Record {
	if e := s.entries[id]; e != nil {
		return e.state.Encode()
	}
	return nil
}

// Progress is the player's progress in one quest.
func (s *System) Progress(id world.PlayerID, q gamedata.QuestID) quest.Progress {
	if e := s.entries[id]; e != nil {
		return e.state.Get(q)
	}
	return quest.Progress{}
}

// Drain returns the players whose quest state changed since the last call,
// in the order they changed, and forgets the changes.
func (s *System) Drain() []Change {
	if len(s.order) == 0 {
		return nil
	}
	out := make([]Change, 0, len(s.order))
	for _, id := range s.order {
		e := s.entries[id]
		if e == nil || !e.dirty {
			continue
		}
		out = append(out, Change{Player: id, Records: e.state.Encode(), Completed: e.completed})
		e.dirty = false
		e.completed = nil
	}
	s.order = s.order[:0]
	return out
}

// Tick sends a level event for every combat or skill level a player has
// gained since the last tick.
func (s *System) Tick(w *world.World, _ time.Duration) {
	rules := s.data.Rules
	for _, p := range w.Players() {
		e := s.entry(p)
		if lv := p.CombatLevel(rules); lv != e.combat {
			e.combat = lv
			if lv > 0 {
				s.apply(p, e, s.engine.SkillLevel(&e.state, gamedata.CombatSkill, lv, s.env(p)))
			}
		}
		for sk := world.Skill(0); sk < world.NumSkills; sk++ {
			if lv := p.SkillLevel(sk); lv != e.skills[sk] {
				e.skills[sk] = lv
				s.apply(p, e, s.engine.SkillLevel(&e.state, gamedata.Skill(sk.String()), lv, s.env(p)))
			}
		}
	}
}

// PickedUp tells the system p picked qty of item off the ground. It is the
// grounditems pick-up hook.
func (s *System) PickedUp(p *world.Player, item gamedata.ItemID, _ int) {
	e := s.entry(p)
	s.apply(p, e, s.engine.GotItem(&e.state, item, s.env(p)))
}

func (s *System) entry(p *world.Player) *entry {
	e := s.entries[p.ID]
	if e == nil {
		e = s.newEntry(p)
		s.entries[p.ID] = e
	}
	return e
}

func (s *System) newEntry(p *world.Player) *entry {
	e := &entry{combat: p.CombatLevel(s.data.Rules)}
	for sk := world.Skill(0); sk < world.NumSkills; sk++ {
		e.skills[sk] = p.SkillLevel(sk)
	}
	return e
}

func (s *System) apply(p *world.Player, e *entry, r quest.Result) {
	if len(r.Completed) > 0 {
		e.completed = append(e.completed, r.Completed...)
	}
	if !r.Advanced && len(r.Completed) == 0 {
		return
	}
	if !e.dirty {
		e.dirty = true
		s.order = append(s.order, p.ID)
	}
}

// Sink wraps a combat.EventSink: a kill by a player counts toward the killer's
// quests, and the quest items the kill drops for the killer are added to the
// Kill's Items before the inner sink sees it.
type Sink struct {
	combat.EventSink
	s *System
}

// Sink returns the combat event sink that feeds this system and then inner.
func (s *System) Sink(inner combat.EventSink) *Sink { return &Sink{EventSink: inner, s: s} }

// NPCKilled implements combat.EventSink.
func (k *Sink) NPCKilled(kill combat.Kill) {
	if p := k.s.players.Player(kill.Killer); p != nil {
		e := k.s.entry(p)
		env := k.s.env(p)
		typ := gamedata.NPCTypeID(kill.Type)
		// Drops first: they depend on the step the player is on now.
		for _, d := range k.s.engine.Drops(&e.state, typ, env, k.s.rand) {
			kill.Items = append(kill.Items, world.Stack{Item: d.Item, Qty: d.Quantity})
		}
		k.s.apply(p, e, k.s.engine.Kill(&e.state, typ, env))
	}
	k.EventSink.NPCKilled(kill)
}

// env is the engine's read-only view of a player.
type env struct {
	p     *world.Player
	rules gamedata.Rules
}

func (s *System) env(p *world.Player) env { return env{p, s.data.Rules} }

func (v env) SkillLevel(sk gamedata.Skill) int {
	switch sk {
	case gamedata.CombatSkill:
		return v.p.CombatLevel(v.rules)
	case gamedata.SkillWoodcutting:
		return v.p.SkillLevel(world.Woodcutting)
	case gamedata.SkillFishing:
		return v.p.SkillLevel(world.Fishing)
	case gamedata.SkillCooking:
		return v.p.SkillLevel(world.Cooking)
	}
	return 0
}

func (v env) ItemCount(id gamedata.ItemID) int { return v.p.Inv.Count(id) }
