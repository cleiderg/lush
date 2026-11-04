// Package talk is the talking NPC system (docs/specs/quests.md, Talking
// NPCs): it stands one non-combat NPC per talking type at its fixed spot,
// opens their dialogue for a player within 2 tiles, and runs the replies,
// which start quests and finish talk or bring steps through the quest engine.
//
// It does no I/O. Results and dialogue go to a Sink; the session turns them
// into messages. Gold and items a reply gives or takes are one event in the
// player's economy outbox.
package talk

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/quest"
	"github.com/cleiderg/lush/server/internal/world"
)

// The embedded copy must match shared/map/talkers.json; refresh it from the
// repo root with: cp shared/map/talkers.json server/internal/systems/talk/talkers.json
//
//go:embed talkers.json
var embeddedTalkers []byte

// Outbox reasons.
const (
	ReasonHandIn = "quest_hand_in"
	ReasonReward = "quest_reward"
)

// ReachTiles is how close a player must be to talk (docs/specs/quests.md).
const ReachTiles = 2

// Result matches protocol QuestResultCode.
type Result uint8

const (
	Ok                 Result = 0
	TooFar             Result = 1
	NotTalkable        Result = 2
	RequirementsNotMet Result = 3
	InventoryFull      Result = 4
)

// TalkCommand asks for Player to talk to the NPC NPC.
type TalkCommand struct {
	Player world.PlayerID
	NPC    world.NPCID
}

func (c TalkCommand) Target() world.PlayerID { return c.Player }

// ReplyCommand is Player picking reply Index in their open dialogue.
type ReplyCommand struct {
	Player world.PlayerID
	Index  int
}

func (c ReplyCommand) Target() world.PlayerID { return c.Player }

// Dialogue is a node as shown to a player.
type Dialogue struct {
	NPC     world.NPCID
	Lines   []string
	Replies []string
}

// Sink receives what the system tells players. It is called on the tick
// goroutine and must not block.
type Sink interface {
	// QuestResult reports a refused Talk or reply.
	QuestResult(to world.PlayerID, r Result)
	Dialogue(to world.PlayerID, d Dialogue)
	DialogueClose(to world.PlayerID)
	// QuestCompleted reports a finished quest; take lists the items the
	// finishing step took from the player.
	QuestCompleted(to world.PlayerID, c quest.Completion, take []gamedata.ItemAmount)
}

// CommitID names one pending quest completion.
type CommitID uint64

// CommitRequest is one quest completion for the store to save as a single
// transaction: the quest, its rewards, and the items it takes. Gold and items
// go to the economy audit log (HandIn and Reward are the changes to record).
type CommitRequest struct {
	ID          CommitID
	Player      world.PlayerID
	Quest       gamedata.QuestID
	QuestPoints int
	Gold        int
	Items       []gamedata.ItemAmount
	XP          []gamedata.SkillXP
	// Take are the items the finishing bring step takes.
	Take []gamedata.ItemAmount
	// Remove are the quest items removed on completion, including any the
	// finishing step takes.
	Remove []gamedata.ItemAmount
	HandIn []world.Change // economy changes of Take and Remove
	Reward []world.Change // economy changes of Gold and Items
}

// Committer saves a request off the tick, then calls System.Resolve with its
// ID. The player's inventory is locked when it is called. It is called on the
// tick goroutine and must not block.
type Committer interface {
	CommitQuest(req CommitRequest)
}

type inflight struct {
	req    CommitRequest
	player *world.Player
	inv    world.Inventory
	post   []quest.Record // quest state once the commit succeeds
	done   quest.Completion
}

type resolution struct {
	id CommitID
	ok bool
}

type talkerFile struct {
	Version int    `json:"version"`
	Units   string `json:"units"`
	Talkers []struct {
		ID string `json:"id"`
		X  int    `json:"x"`
		Z  int    `json:"z"`
	} `json:"talkers"`
}

// resolve returns each talking NPC type's position in metres, from the spots
// file. A type whose spot is missing is an error.
func resolve(d *gamedata.Data, raw []byte) (map[gamedata.NPCTypeID]world.Vec2, error) {
	var f talkerFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("talk: talkers.json: %w", err)
	}
	if f.Units != "decimetres" {
		return nil, fmt.Errorf("talk: talkers.json: unsupported units %q", f.Units)
	}
	byID := map[string]world.Vec2{}
	for _, t := range f.Talkers {
		byID[t.ID] = world.Vec2{X: float32(t.X) / 10, Y: float32(t.Z) / 10}
	}
	out := map[gamedata.NPCTypeID]world.Vec2{}
	for _, t := range d.Talkers {
		pos, ok := byID[t.Spot]
		if !ok {
			return nil, fmt.Errorf("talk: talking npc %q: spot %q is not in talkers.json", t.Key, t.Spot)
		}
		out[t.ID] = pos
	}
	return out, nil
}

type conversation struct {
	npc  world.NPCID
	typ  gamedata.NPCTypeID
	node string
}

// System is the talking NPC system.
type System struct {
	data  *gamedata.Data
	eng   *quest.Engine
	sink  Sink
	cm    Committer
	reach float64 // metres
	spots map[gamedata.NPCTypeID]world.Vec2

	spawned bool
	pending []world.Command
	states  map[world.PlayerID]*quest.State
	convs   map[world.PlayerID]conversation

	nextID     CommitID
	commits    map[CommitID]*inflight
	committing map[world.PlayerID]CommitID

	rmu      sync.Mutex
	resolved []resolution
}

// New creates the system. tileSize is the length of a tile in metres.
func New(d *gamedata.Data, eng *quest.Engine, tileSize float64, sink Sink, cm Committer) (*System, error) {
	if d == nil || eng == nil || sink == nil || cm == nil {
		return nil, errors.New("talk: nil dependency")
	}
	if !(tileSize > 0) || math.IsInf(tileSize, 0) {
		return nil, errors.New("talk: tile size must be positive and finite")
	}
	spots, err := resolve(d, embeddedTalkers)
	if err != nil {
		return nil, err
	}
	return &System{
		data: d, eng: eng, sink: sink, cm: cm,
		reach:  ReachTiles * tileSize,
		spots:  spots,
		states: map[world.PlayerID]*quest.State{},
		convs:  map[world.PlayerID]conversation{},

		commits:    map[CommitID]*inflight{},
		committing: map[world.PlayerID]CommitID{},
	}, nil
}

func (s *System) Name() string { return "talk" }

// State returns player id's quest state, creating an empty one. Systems that
// count kills or items use the same state.
func (s *System) State(id world.PlayerID) *quest.State {
	st := s.states[id]
	if st == nil {
		st = &quest.State{}
		s.states[id] = st
	}
	return st
}

// SetState replaces player id's quest state (when they load in).
func (s *System) SetState(id world.PlayerID, st quest.State) { s.states[id] = &st }

// Forget drops player id's state and open dialogue (when they leave).
func (s *System) Forget(id world.PlayerID) {
	delete(s.states, id)
	delete(s.convs, id)
}

// Resolve reports the outcome of commit id. The next Tick applies it. Safe
// for concurrent use; unknown or repeated ids are ignored.
func (s *System) Resolve(id CommitID, ok bool) {
	s.rmu.Lock()
	s.resolved = append(s.resolved, resolution{id: id, ok: ok})
	s.rmu.Unlock()
}

// Handle queues the command; it needs the world, so it is applied in Tick.
func (s *System) Handle(cmd world.Command, _ *world.Player) {
	switch cmd.(type) {
	case TalkCommand, ReplyCommand:
		s.pending = append(s.pending, cmd)
	}
}

// Tick spawns the talkers on the first tick, then applies queued commands.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.rmu.Lock()
	done := s.resolved
	s.resolved = nil
	s.rmu.Unlock()
	for _, r := range done {
		s.finish(w, r)
	}
	if !s.spawned {
		s.spawned = true
		for _, t := range s.data.Talkers {
			pos := s.spots[t.ID]
			// HP stays 0: a talking NPC has nothing to lose in a fight.
			if _, err := w.AddNPC(&world.NPC{Type: world.NPCTypeID(t.ID), Pos: pos, Home: pos}); err != nil {
				continue // id range full; nothing sensible to do on the tick
			}
		}
	}
	cmds := s.pending
	s.pending = nil
	for _, c := range cmds {
		p := w.Player(c.Target())
		if p == nil {
			continue
		}
		switch c := c.(type) {
		case TalkCommand:
			s.talk(w, p, c.NPC)
		case ReplyCommand:
			s.reply(w, p, c.Index)
		}
	}
}

func (s *System) near(p *world.Player, n *world.NPC) bool {
	return math.Hypot(float64(p.Pos.X-n.Pos.X), float64(p.Pos.Y-n.Pos.Y)) <= s.reach
}

type env struct {
	p *world.Player
	d *gamedata.Data
}

func (e env) SkillLevel(sk gamedata.Skill) int {
	switch sk {
	case gamedata.CombatSkill:
		return e.p.CombatLevel(e.d.Rules)
	case gamedata.SkillWoodcutting:
		return e.p.SkillLevel(world.Woodcutting)
	case gamedata.SkillFishing:
		return e.p.SkillLevel(world.Fishing)
	case gamedata.SkillCooking:
		return e.p.SkillLevel(world.Cooking)
	}
	return 0
}
func (e env) ItemCount(i gamedata.ItemID) int { return e.p.Inv.Count(i) }

func (s *System) talk(w *world.World, p *world.Player, id world.NPCID) {
	delete(s.convs, p.ID)
	n := w.NPC(id)
	if n == nil {
		s.sink.QuestResult(p.ID, NotTalkable)
		return
	}
	t, ok := s.data.NPC(gamedata.NPCTypeID(n.Type))
	if !ok || !t.Talkable {
		s.sink.QuestResult(p.ID, NotTalkable)
		return
	}
	if !s.near(p, n) {
		s.sink.QuestResult(p.ID, TooFar)
		return
	}
	dl, ok := s.data.DialogueFor(t.ID)
	if !ok {
		s.sink.QuestResult(p.ID, NotTalkable)
		return
	}
	s.show(p.ID, conversation{npc: id, typ: t.ID}, dl, s.entry(dl, s.State(p.ID)))
}

// entry is the first conditional node the quest state satisfies, else Start.
func (s *System) entry(dl gamedata.Dialogue, st *quest.State) string {
	for _, nd := range dl.Nodes {
		c := nd.When
		if c == nil {
			continue
		}
		q, ok := s.data.QuestByKey(c.Quest)
		if !ok {
			continue
		}
		pr := st.Get(q.ID)
		switch c.State {
		case gamedata.QuestNotStarted:
			if pr.Status == quest.NotStarted {
				return nd.ID
			}
		case gamedata.QuestOnStep:
			if pr.Status == quest.InProgress && pr.Step == c.Step {
				return nd.ID
			}
		case gamedata.QuestComplete:
			if pr.Status == quest.Complete {
				return nd.ID
			}
		}
	}
	return dl.Start
}

func (s *System) show(pid world.PlayerID, cv conversation, dl gamedata.Dialogue, node string) {
	nd, ok := dl.Node(node)
	if !ok {
		delete(s.convs, pid)
		s.sink.DialogueClose(pid)
		return
	}
	cv.node = node
	s.convs[pid] = cv
	d := Dialogue{NPC: cv.npc, Lines: nd.Text}
	for _, r := range nd.Replies {
		d.Replies = append(d.Replies, r.Text)
	}
	s.sink.Dialogue(pid, d)
}

func (s *System) end(pid world.PlayerID) {
	delete(s.convs, pid)
	s.sink.DialogueClose(pid)
}

func (s *System) reply(w *world.World, p *world.Player, idx int) {
	cv, ok := s.convs[p.ID]
	if !ok {
		return
	}
	if _, busy := s.committing[p.ID]; busy {
		s.end(p.ID) // a completion is being saved; nothing more to do yet
		return
	}
	n := w.NPC(cv.npc)
	dl, hasDl := s.data.DialogueFor(cv.typ)
	if n == nil || !hasDl {
		s.end(p.ID)
		return
	}
	if !s.near(p, n) {
		s.end(p.ID)
		s.sink.QuestResult(p.ID, TooFar)
		return
	}
	nd, _ := dl.Node(cv.node)
	if len(nd.Replies) == 0 { // continue button
		s.end(p.ID)
		return
	}
	if idx < 0 || idx >= len(nd.Replies) {
		return
	}
	r := nd.Replies[idx]
	if r.StartQuest != "" {
		if code := s.act(p, func(st *quest.State, e quest.Env) (quest.Result, error) {
			q, ok := s.data.QuestByKey(r.StartQuest)
			if !ok {
				return quest.Result{}, quest.ErrUnknownQuest
			}
			if st.Get(q.ID).Status != quest.NotStarted {
				return quest.Result{}, nil // already begun: carry on
			}
			return s.eng.Start(st, q.ID, e)
		}); code != Ok {
			s.end(p.ID)
			s.sink.QuestResult(p.ID, code)
			return
		}
	}
	if r.FinishTalk {
		if code := s.act(p, func(st *quest.State, e quest.Env) (quest.Result, error) {
			return s.eng.FinishTalk(st, cv.typ, e), nil
		}); code != Ok {
			s.end(p.ID)
			s.sink.QuestResult(p.ID, code)
			return
		}
	}
	if r.Next == "" {
		s.end(p.ID)
		return
	}
	s.show(p.ID, cv, dl, r.Next)
}

// act runs a quest engine call and applies its result to the player as one
// step: items taken and rewards given together, or nothing changes at all
// (quest state included).
func (s *System) act(p *world.Player, f func(*quest.State, quest.Env) (quest.Result, error)) Result {
	st := s.State(p.ID)
	snap := st.Encode()
	rollback := func() {
		if old, err := s.eng.Decode(snap); err == nil {
			*st = old
		}
	}
	res, err := f(st, env{p: p, d: s.data})
	if err != nil {
		rollback()
		if errors.Is(err, quest.ErrRequirements) {
			return RequirementsNotMet
		}
		return NotTalkable
	}
	inv := p.Inv.Clone()
	var hand, reward []world.Change
	take := func(list []gamedata.ItemAmount, into *[]world.Change) error {
		for _, it := range list {
			ch, err := inv.Remove(it.Item, it.Quantity)
			if err != nil {
				return err
			}
			*into = append(*into, ch...)
		}
		return nil
	}
	if err := take(res.Take, &hand); err != nil {
		rollback()
		return InventoryFull
	}
	for _, c := range res.Completed {
		if err := take(c.Remove, &hand); err != nil {
			rollback()
			return InventoryFull
		}
		for _, it := range c.Items {
			ch, err := inv.Add(s.data, it.Item, it.Quantity)
			if err != nil {
				rollback()
				return InventoryFull
			}
			reward = append(reward, ch...)
		}
		if c.Gold > 0 {
			ch, err := inv.AddGold(c.Gold)
			if err != nil {
				rollback()
				return InventoryFull
			}
			reward = append(reward, ch...)
		}
	}
	if len(res.Completed) > 0 {
		s.beginCommit(p, snap, rollback, res, inv, hand, reward)
		return Ok
	}
	p.Inv = inv
	if len(hand) > 0 {
		p.RecordEconomy(ReasonHandIn, hand)
	}
	if len(reward) > 0 {
		p.RecordEconomy(ReasonReward, reward)
	}
	return Ok
}

// beginCommit holds a finished quest back: the quest state returns to its last
// step and the inventory locks until the store confirms. inv, hand and reward
// are the inventory and economy changes the completion would make. Only the
// first completion is committed; the caller's talk repeats for any other.
func (s *System) beginCommit(p *world.Player, snap []quest.Record, rollback func(), res quest.Result,
	inv world.Inventory, hand, reward []world.Change) {
	c := res.Completed[0]
	remove := append([]gamedata.ItemAmount(nil), c.Remove...)
	for _, it := range res.Take {
		if d, ok := s.data.Item(it.Item); ok && d.QuestItem {
			remove = append(remove, it)
		}
	}
	post := s.State(p.ID).Encode()
	rollback()
	// Keep only the first completed quest in the post state.
	for _, o := range res.Completed[1:] {
		post = withRecord(post, o.Quest, snap)
	}
	s.nextID++
	req := CommitRequest{ID: s.nextID, Player: p.ID, Quest: c.Quest, QuestPoints: c.QuestPoints, Gold: c.Gold,
		Items: append([]gamedata.ItemAmount(nil), c.Items...), XP: append([]gamedata.SkillXP(nil), c.XP...),
		Take: append([]gamedata.ItemAmount(nil), res.Take...), Remove: remove,
		HandIn: hand, Reward: reward}
	s.commits[req.ID] = &inflight{req: req, player: p, inv: inv, post: post, done: c}
	s.committing[p.ID] = req.ID
	p.Inv.SetLocked(true)
	out := req
	out.HandIn = append([]world.Change(nil), hand...)
	out.Reward = append([]world.Change(nil), reward...)
	s.cm.CommitQuest(out)
}

// withRecord sets quest q in recs to its record in orig, or drops it.
func withRecord(recs []quest.Record, q gamedata.QuestID, orig []quest.Record) []quest.Record {
	var out []quest.Record
	for _, r := range recs {
		if r.Quest != q {
			out = append(out, r)
		}
	}
	for _, r := range orig {
		if r.Quest == q {
			out = append(out, r)
		}
	}
	return out
}

// finish applies the outcome of a commit: on success the quest completes and
// the rewards reach the live player; either way the inventory unlocks.
func (s *System) finish(w *world.World, r resolution) {
	c := s.commits[r.id]
	if c == nil {
		return
	}
	delete(s.commits, r.id)
	delete(s.committing, c.req.Player)
	p := c.player
	if w.Player(c.req.Player) != p {
		return // left the world; the store has the result
	}
	p.Inv.SetLocked(false)
	if !r.ok {
		return
	}
	inv := c.inv
	inv.SetLocked(false)
	p.Inv = inv
	// Only the completed quest's record comes from the saved state, so
	// progress other systems made meanwhile (kills, items) is kept.
	if st, err := s.eng.Decode(withRecord(s.State(p.ID).Encode(), c.req.Quest, c.post)); err == nil {
		s.SetState(p.ID, st)
	}
	if len(c.req.HandIn) > 0 {
		p.RecordEconomy(ReasonHandIn, c.req.HandIn)
	}
	if len(c.req.Reward) > 0 {
		p.RecordEconomy(ReasonReward, c.req.Reward)
	}
	for _, x := range c.req.XP {
		switch x.Skill {
		case gamedata.CombatSkill:
			p.AddXP(s.data.Rules, x.XP)
		case gamedata.SkillWoodcutting:
			p.AddSkillXP(world.Woodcutting, x.XP*world.SkillXPScale)
		case gamedata.SkillFishing:
			p.AddSkillXP(world.Fishing, x.XP*world.SkillXPScale)
		case gamedata.SkillCooking:
			p.AddSkillXP(world.Cooking, x.XP*world.SkillXPScale)
		}
	}
	s.sink.QuestCompleted(p.ID, c.done, c.req.Take)
}
