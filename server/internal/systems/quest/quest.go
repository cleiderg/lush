// Package quest is the quest engine (docs/specs/quests.md): per-player quest
// state, and the rules that move it forward on game events, using the quest
// tables in gamedata.
//
// It takes plain values and a small read-only view of the player (Env). It
// never sees or changes a World or Player: callers apply the results (items
// to take, rewards to give, items to remove) and write the audit log.
package quest

import (
	"errors"
	"fmt"
	"sort"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

// Status is where a player is in one quest.
type Status uint8

const (
	NotStarted Status = iota
	InProgress
	Complete
)

// Progress is a player's state in one quest. Step is the 0-based current
// step and Count the kills so far on a kill step; both are 0 unless
// InProgress.
type Progress struct {
	Status Status
	Step   int
	Count  int
}

// Record is the plain stored form of one quest's progress. Quests that are
// not started have no record.
type Record struct {
	Quest  gamedata.QuestID `json:"quest"`
	Status Status           `json:"status"`
	Step   int              `json:"step"`
	Count  int              `json:"count"`
}

// State is one player's quest progress.
type State struct {
	m map[gamedata.QuestID]Progress
}

// Get returns the progress in quest id (NotStarted if none).
func (s *State) Get(id gamedata.QuestID) Progress { return s.m[id] }

func (s *State) set(id gamedata.QuestID, p Progress) {
	if s.m == nil {
		s.m = map[gamedata.QuestID]Progress{}
	}
	if p.Status == NotStarted {
		delete(s.m, id)
		return
	}
	s.m[id] = p
}

// Encode returns the state as plain records, sorted by quest id.
func (s *State) Encode() []Record {
	out := make([]Record, 0, len(s.m))
	for id, p := range s.m {
		out = append(out, Record{Quest: id, Status: p.Status, Step: p.Step, Count: p.Count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Quest < out[j].Quest })
	return out
}

// Decode rebuilds a State from records, checking each against the quest
// tables. A record that is not valid (unknown quest, repeated quest, step out
// of range) is an error.
func (e *Engine) Decode(recs []Record) (State, error) {
	var s State
	for _, r := range recs {
		q, ok := e.d.Quest(r.Quest)
		if !ok {
			return State{}, fmt.Errorf("decode quests: unknown quest %d", r.Quest)
		}
		if _, dup := s.m[r.Quest]; dup {
			return State{}, fmt.Errorf("decode quests: quest %d twice", r.Quest)
		}
		p := Progress{Status: r.Status, Step: r.Step, Count: r.Count}
		switch r.Status {
		case InProgress:
			if r.Step < 0 || r.Step >= len(q.Steps) || r.Count < 0 {
				return State{}, fmt.Errorf("decode quests: quest %d: bad step %d count %d", r.Quest, r.Step, r.Count)
			}
		case Complete:
			if r.Step != 0 || r.Count != 0 {
				return State{}, fmt.Errorf("decode quests: quest %d: complete with step data", r.Quest)
			}
		default:
			return State{}, fmt.Errorf("decode quests: quest %d: bad status %d", r.Quest, r.Status)
		}
		s.set(r.Quest, p)
	}
	return s, nil
}

// QuestPoints is the total quest points of the completed quests.
func (e *Engine) QuestPoints(s *State) int {
	n := 0
	for id, p := range s.m {
		if q, ok := e.d.Quest(id); ok && p.Status == Complete {
			n += q.Rewards.QuestPoints
		}
	}
	return n
}

// Env is the read-only view of the player the engine needs.
type Env interface {
	// SkillLevel is the level in a skill; for gamedata.CombatSkill, the combat level.
	SkillLevel(gamedata.Skill) int
	// ItemCount is how many of an item the player carries.
	ItemCount(gamedata.ItemID) int
}

// Rand is the random source for quest drops. *math/rand/v2.Rand satisfies it.
type Rand interface {
	IntN(n int) int // uniform in [0, n)
}

// Completion is what finishing a quest gives and takes. The caller applies
// all of it as one transaction and writes gold and items to the audit log.
type Completion struct {
	Quest       gamedata.QuestID
	Name        string
	QuestPoints int
	Gold        int
	Items       []gamedata.ItemAmount
	XP          []gamedata.SkillXP
	// Remove are the quest items the player holds that go now.
	Remove []gamedata.ItemAmount
	// TotalPoints is the player's quest points including this quest.
	TotalPoints int
}

// Result is what an event did.
type Result struct {
	// Advanced reports that the event moved a quest to its next step or
	// counted a kill.
	Advanced bool
	// Take are items to remove from the player (bring_items), all or none.
	Take []gamedata.ItemAmount
	// Completed lists quests the event finished.
	Completed []Completion
}

// Engine applies events to quest states.
type Engine struct {
	d *gamedata.Data
}

// New returns an engine over the given tables.
func New(d *gamedata.Data) *Engine { return &Engine{d: d} }

// Errors from CanStart and Start.
var (
	ErrUnknownQuest = errors.New("unknown quest")
	ErrAlreadyBegun = errors.New("quest already started or complete")
	ErrRequirements = errors.New("quest requirements not met")
)

// CanStart reports why the player can't start quest id, or nil.
func (e *Engine) CanStart(s *State, id gamedata.QuestID, env Env) error {
	q, ok := e.d.Quest(id)
	if !ok {
		return ErrUnknownQuest
	}
	if s.Get(id).Status != NotStarted {
		return ErrAlreadyBegun
	}
	for _, l := range q.Requirements.Levels {
		if env.SkillLevel(l.Skill) < l.Level {
			return fmt.Errorf("%w: %s %d", ErrRequirements, l.Skill, l.Level)
		}
	}
	for _, k := range q.Requirements.Quests {
		r, ok := e.d.QuestByKey(k)
		if !ok || s.Get(r.ID).Status != Complete {
			return fmt.Errorf("%w: quest %s", ErrRequirements, k)
		}
	}
	return nil
}

// Start begins quest id at its first step. If that step is already met (a
// level reached) the result says so.
func (e *Engine) Start(s *State, id gamedata.QuestID, env Env) (Result, error) {
	if err := e.CanStart(s, id, env); err != nil {
		return Result{}, err
	}
	s.set(id, Progress{Status: InProgress})
	res := Result{Advanced: true}
	e.settle(s, id, env, &res)
	return res, nil
}

// current returns the quest and its current step for a quest in progress.
func (e *Engine) current(s *State, id gamedata.QuestID) (gamedata.Quest, gamedata.QuestStep, bool) {
	p := s.Get(id)
	q, ok := e.d.Quest(id)
	if !ok || p.Status != InProgress || p.Step >= len(q.Steps) {
		return gamedata.Quest{}, gamedata.QuestStep{}, false
	}
	return q, q.Steps[p.Step], true
}

// each calls f for every quest in progress, in quest id order.
func (e *Engine) each(s *State, f func(q gamedata.Quest, p Progress, st gamedata.QuestStep)) {
	for _, r := range s.Encode() {
		if q, st, ok := e.current(s, r.Quest); ok {
			f(q, s.Get(r.Quest), st)
		}
	}
}

// advance moves quest id to its next step, or completes it, then settles
// steps that the player already meets.
func (e *Engine) advance(s *State, q gamedata.Quest, env Env, res *Result) {
	p := s.Get(q.ID)
	res.Advanced = true
	p.Step++
	p.Count = 0
	s.set(q.ID, p)
	e.settle(s, q.ID, env, res)
}

// settle finishes the quest if it is past its last step, and passes reach_level
// steps the player already meets.
func (e *Engine) settle(s *State, id gamedata.QuestID, env Env, res *Result) {
	for {
		_, st, ok := e.current(s, id)
		if !ok {
			break
		}
		if st.Kind != gamedata.StepReachLevel || env.SkillLevel(st.Skill) < st.Level {
			return
		}
		p := s.Get(id)
		p.Step++
		p.Count = 0
		s.set(id, p)
		res.Advanced = true
	}
	q, ok := e.d.Quest(id)
	if !ok || s.Get(id).Status != InProgress {
		return
	}
	s.set(id, Progress{Status: Complete})
	c := Completion{
		Quest:       id,
		Name:        q.Name,
		QuestPoints: q.Rewards.QuestPoints,
		Gold:        q.Rewards.Gold,
		Items:       append([]gamedata.ItemAmount(nil), q.Rewards.Items...),
		XP:          append([]gamedata.SkillXP(nil), q.Rewards.XP...),
		TotalPoints: e.QuestPoints(s),
	}
	seen := map[gamedata.ItemID]bool{}
	for _, st := range q.Steps {
		var items []gamedata.ItemAmount
		if st.Kind == gamedata.StepQuestItem {
			items = []gamedata.ItemAmount{{ItemKey: st.ItemKey, Item: st.Item}}
		}
		items = append(items, st.Items...)
		for _, it := range items {
			if d, ok := e.d.Item(it.Item); !ok || !d.QuestItem || seen[it.Item] {
				continue
			}
			seen[it.Item] = true
			// Items taken by this very event are already gone; the caller
			// removes only what it still finds.
			if n := env.ItemCount(it.Item); n > 0 {
				c.Remove = append(c.Remove, gamedata.ItemAmount{ItemKey: it.ItemKey, Item: it.Item, Quantity: n})
			}
		}
	}
	res.Completed = append(res.Completed, c)
}

// Talk is the player talking to an NPC (the talk reply of a dialogue). It
// advances every quest whose current step is a talk to that NPC type.
func (e *Engine) Talk(s *State, npc gamedata.NPCTypeID, env Env) Result {
	var res Result
	e.each(s, func(q gamedata.Quest, _ Progress, st gamedata.QuestStep) {
		if st.Kind == gamedata.StepTalk && st.NPC == npc {
			e.advance(s, q, env, &res)
		}
	})
	return res
}

// HandOver is the player handing items to an NPC. A bring_items step takes
// all of its items or none: if the player lacks any, nothing changes. The
// caller removes Result.Take.
func (e *Engine) HandOver(s *State, npc gamedata.NPCTypeID, env Env) Result {
	var res Result
	e.each(s, func(q gamedata.Quest, _ Progress, st gamedata.QuestStep) {
		if st.Kind != gamedata.StepBringItems || st.NPC != npc {
			return
		}
		for _, it := range st.Items {
			if env.ItemCount(it.Item) < it.Quantity {
				return
			}
		}
		res.Take = append(res.Take, st.Items...)
		e.advance(s, q, &afterTake{env: env, take: st.Items}, &res)
	})
	return res
}

// afterTake is an Env as it will be once the handed-over items are removed.
type afterTake struct {
	env  Env
	take []gamedata.ItemAmount
}

func (a *afterTake) SkillLevel(sk gamedata.Skill) int { return a.env.SkillLevel(sk) }
func (a *afterTake) ItemCount(id gamedata.ItemID) int {
	n := a.env.ItemCount(id)
	for _, t := range a.take {
		if t.Item == id {
			n -= t.Quantity
		}
	}
	return n
}

// FinishTalk is a dialogue reply that finishes the talk or bring step the
// player is on with this NPC: it hands items over where the step asks for
// them, and talks otherwise.
func (e *Engine) FinishTalk(s *State, npc gamedata.NPCTypeID, env Env) Result {
	res := e.HandOver(s, npc, env)
	if res.Advanced {
		return res
	}
	return e.Talk(s, npc, env)
}

// Kill is the player killing an NPC of type npc. It counts toward every kill
// step on that type and advances when the count is reached.
func (e *Engine) Kill(s *State, npc gamedata.NPCTypeID, env Env) Result {
	var res Result
	e.each(s, func(q gamedata.Quest, p Progress, st gamedata.QuestStep) {
		if st.Kind != gamedata.StepKill || st.NPC != npc {
			return
		}
		p.Count++
		res.Advanced = true
		if p.Count >= st.Count {
			e.advance(s, q, env, &res)
			return
		}
		s.set(q.ID, p)
	})
	return res
}

// SkillLevel is the player reaching a level in a skill (or combat). It
// advances reach_level steps for that skill that the level meets.
func (e *Engine) SkillLevel(s *State, skill gamedata.Skill, level int, env Env) Result {
	var res Result
	e.each(s, func(q gamedata.Quest, _ Progress, st gamedata.QuestStep) {
		if st.Kind == gamedata.StepReachLevel && st.Skill == skill && level >= st.Level {
			e.advance(s, q, env, &res)
		}
	})
	return res
}

// GotItem is the player getting a quest item (picking up the drop or a ground
// spawn). It advances quest_item steps for that item.
func (e *Engine) GotItem(s *State, item gamedata.ItemID, env Env) Result {
	var res Result
	e.each(s, func(q gamedata.Quest, _ Progress, st gamedata.QuestStep) {
		if st.Kind == gamedata.StepQuestItem && st.Item == item {
			e.advance(s, q, env, &res)
		}
	})
	return res
}

// Drops rolls the quest items a kill of npc gives this player. A quest item
// drops only for a player on the step that wants it: the quest_item step
// itself, or the bring_items step after it when the player has lost the item.
// It never drops an item the player already holds. Chance is 1 in Chance; at
// 1 it always drops and the source is not consulted.
func (e *Engine) Drops(s *State, npc gamedata.NPCTypeID, env Env, r Rand) []gamedata.ItemAmount {
	var out []gamedata.ItemAmount
	e.each(s, func(q gamedata.Quest, p Progress, st gamedata.QuestStep) {
		for _, src := range e.sources(q, p.Step, st) {
			if src.SourceNPC != npc || env.ItemCount(src.Item) > 0 {
				continue
			}
			if src.Chance > 1 && r.IntN(src.Chance) != 0 {
				continue
			}
			out = append(out, gamedata.ItemAmount{ItemKey: src.ItemKey, Item: src.Item, Quantity: 1})
		}
	})
	return out
}

// sources are the quest_item steps that supply what the current step needs.
func (e *Engine) sources(q gamedata.Quest, step int, st gamedata.QuestStep) []gamedata.QuestStep {
	switch st.Kind {
	case gamedata.StepQuestItem:
		return []gamedata.QuestStep{st}
	case gamedata.StepBringItems:
		var out []gamedata.QuestStep
		for _, prev := range q.Steps[:step] {
			if prev.Kind != gamedata.StepQuestItem {
				continue
			}
			for _, it := range st.Items {
				if it.Item == prev.Item {
					out = append(out, prev)
				}
			}
		}
		return out
	}
	return nil
}
