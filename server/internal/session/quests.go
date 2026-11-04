package session

import (
	"context"
	"errors"
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/interest"
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/quest"
	"github.com/cleiderg/lush/server/internal/systems/questevents"
	"github.com/cleiderg/lush/server/internal/systems/talk"
	"github.com/cleiderg/lush/server/internal/world"
)

// SetQuests connects the quest systems (docs/specs/quests.md): joining
// players get their stored quest rows loaded into events and are sent
// QuestCatalog and QuestLog, and every quest state change events reports is
// saved and sent as a new QuestLog. t must have been created with TalkSink as
// its Sink and share its state with events. Register the Server after both
// systems, and call SetQuests before the world starts ticking.
func (s *Server) SetQuests(events *questevents.System, t *talk.System) {
	s.quests, s.talk = events, t
	s.questEng = quest.New(s.data)
	s.questCat = encodeQuestCatalog(s.data)
	t.SetChangeHook(events.Changed)
}

// QuestSystems creates the quest event and talking NPC systems over the
// session's world, connects them with SetQuests and returns them for
// registration. Register both before the Server. Wrap the combat event sink
// with events.Sink and set events.PickedUp as the ground item pick-up hook.
func (s *Server) QuestSystems(tileSize float64, r quest.Rand) (*questevents.System, *talk.System, error) {
	t, err := talk.New(s.data, quest.New(s.data), tileSize, talkSink{s}, s)
	if err != nil {
		return nil, nil, err
	}
	events := questevents.New(s.data, s.w, t, r)
	s.SetQuests(events, t)
	return events, t, nil
}

// TalkSink is the talk.Sink that sends the player each Dialogue, DialogueClose
// and QuestResult.
func (s *Server) TalkSink() talk.Sink { return talkSink{s} }

type talkSink struct{ s *Server }

func (k talkSink) send(to world.PlayerID, msg []byte) {
	if g := k.s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(msg)
	}
}

func (k talkSink) QuestResult(to world.PlayerID, r talk.Result) {
	k.send(to, encodeQuestResult(protocol.QuestResultCode(r)))
}

func (k talkSink) Dialogue(to world.PlayerID, d talk.Dialogue) {
	k.send(to, encodeDialogue(d))
}

func (k talkSink) DialogueClose(to world.PlayerID) { k.send(to, encodeDialogueClose()) }

// QuestCompleted is called when the store has committed a completion and the
// world applied it: it announces the quest to the player and to every player
// in their interest area. The rewards were saved by CommitQuest.
func (k talkSink) QuestCompleted(to world.PlayerID, c quest.Completion, _ []gamedata.ItemAmount) {
	k.s.announceQuest(to, c)
}

// announceQuest sends QuestCompleted to player id and to the players near
// them. Tick goroutine only.
func (s *Server) announceQuest(id world.PlayerID, c quest.Completion) {
	p := s.w.Player(id)
	if p == nil {
		return
	}
	q, ok := s.data.Quest(c.Quest)
	if !ok {
		return
	}
	msg := encodeQuestCompleted(fmt.Sprintf("Guest %d", id), q) // PLACEHOLDER: players have no names yet
	if g := s.live[id]; g != nil && g.conn != nil {
		g.conn.Send(msg)
	}
	s.hits = s.grid.Query(p.Pos, interest.ViewRadius, interest.MaxVisible, id, s.hits[:0])
	for _, h := range s.hits {
		if g := s.live[h.ID]; g != nil && g.conn != nil {
			g.conn.Send(msg)
		}
	}
}

func encodeQuestCompleted(player string, q gamedata.Quest) []byte {
	b := flatbuffers.NewBuilder(256)
	pn, qn := b.CreateString(player), b.CreateString(q.Name)
	rewards := encodeQuestRewards(b, q.Rewards)
	protocol.QuestCompletedStart(b)
	protocol.QuestCompletedAddPlayerName(b, pn)
	protocol.QuestCompletedAddQuestId(b, uint16(q.ID))
	protocol.QuestCompletedAddQuestName(b, qn)
	protocol.QuestCompletedAddRewards(b, rewards)
	return finish(b, protocol.MessageQuestCompleted, protocol.QuestCompletedEnd(b))
}

// CommitQuest implements talk.Committer. It first queues the player's pending
// economy events, then the completion as one store CompleteQuest, which gives
// the rewards, takes the quest items, marks the quest complete and writes the
// audit entries in one transaction. The result goes back to the talk system
// through Resolve. A completion that fails because the inventory has no room
// also tells the player QuestResult InventoryFull. Called on the tick
// goroutine; never blocks.
func (s *Server) CommitQuest(req talk.CommitRequest) {
	resolve := func(ok bool) {
		if s.talk != nil {
			s.talk.Resolve(req.ID, ok)
		}
	}
	q, hasQuest := s.data.Quest(req.Quest)
	g, p := s.live[req.Player], s.w.Player(req.Player)
	if !hasQuest || g == nil || p == nil || s.questStore == nil {
		s.log.Error("commit quest: no quest, live player or quest store", "player", req.Player, "quest", req.Quest)
		resolve(false)
		return
	}
	c := store.QuestCompletion{PlayerID: g.storeID, Quest: q.Key, Points: int64(req.QuestPoints), Gold: int64(req.Gold)}
	for _, it := range req.Items {
		if d, ok := s.data.Item(it.Item); ok {
			c.Items = append(c.Items, store.QuestItem{Item: int32(it.Item), Qty: int64(it.Quantity), Stackable: d.Stackable()})
		}
	}
	for _, it := range req.Remove {
		c.Remove = append(c.Remove, store.QuestItem{Item: int32(it.Item), Qty: int64(it.Quantity)})
	}
	for _, it := range req.Take {
		if d, ok := s.data.Item(it.Item); ok && !d.QuestItem { // quest items are in Remove already
			c.Remove = append(c.Remove, store.QuestItem{Item: int32(it.Item), Qty: int64(it.Quantity)})
		}
	}
	for _, x := range req.XP {
		switch x.Skill {
		case gamedata.CombatSkill:
			c.XP += int64(x.XP)
		case gamedata.SkillWoodcutting:
			c.Skills.Woodcutting += int64(x.XP) * world.SkillXPScale
		case gamedata.SkillFishing:
			c.Skills.Fishing += int64(x.XP) * world.SkillXPScale
		case gamedata.SkillCooking:
			c.Skills.Cooking += int64(x.XP) * world.SkillXPScale
		}
	}
	var werr error
	earlier := s.appendEconomy(nil, g, p)
	s.saver.submitEconomy(append(earlier, econReq{
		changes: []store.EconomyChange{{PlayerID: g.storeID}},
		write: func(ctx context.Context) error {
			_, werr = s.questStore.CompleteQuest(ctx, c)
			return werr
		},
		done: func(ok bool) {
			if !ok {
				s.log.Warn("quest completion not saved", "store_id", g.storeID, "quest", q.Key, "err", werr)
				if errors.Is(werr, store.ErrNoRoom) {
					s.mu.Lock()
					s.questFull = append(s.questFull, req.Player)
					s.mu.Unlock()
				}
			}
			resolve(ok)
		},
	})...)
}

// loadQuests reads the stored quest rows of player id as records. Rows for
// quests the game data no longer has are dropped. It is empty if the store
// keeps no quests or the quest systems are not connected.
func (s *Server) loadQuests(ctx context.Context, id int64) ([]quest.Record, error) {
	if s.questStore == nil || s.quests == nil {
		return nil, nil
	}
	log, err := s.questStore.LoadQuests(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("player %d: %w", id, err)
	}
	var recs []quest.Record
	for _, row := range log.Quests {
		q, ok := s.data.QuestByKey(row.Quest)
		if !ok {
			s.log.Warn("stored quest not in game data", "store_id", id, "quest", row.Quest)
			continue
		}
		r := quest.Record{Quest: q.ID, Step: int(row.Step), Count: int(row.Count)}
		switch row.State {
		case store.QuestInProgress:
			r.Status = quest.InProgress
		case store.QuestComplete:
			r.Status = quest.Complete
		default:
			continue
		}
		recs = append(recs, r)
	}
	return recs, nil
}

// questRows is the store form of the in-progress records. Completed quests
// are left out: the store takes them through CompleteQuest only.
func (s *Server) questRows(recs []quest.Record) []store.QuestProgress {
	var rows []store.QuestProgress
	for _, r := range recs {
		q, ok := s.data.Quest(r.Quest)
		if !ok || r.Status != quest.InProgress {
			continue
		}
		rows = append(rows, store.QuestProgress{Quest: q.Key, State: store.QuestInProgress,
			Step: int32(r.Step), Count: int32(r.Count)})
	}
	return rows
}

// sendQuestLog sends g the QuestLog for its player's current quest state.
func (s *Server) sendQuestLog(g *guest, recs []quest.Record) {
	g.conn.Send(encodeQuestLog(s.bld, recs, s.questPoints(recs)))
}

func (s *Server) questPoints(recs []quest.Record) int {
	st, err := s.questEng.Decode(recs)
	if err != nil {
		return 0
	}
	return s.questEng.QuestPoints(&st)
}

// flushQuests saves each quest state change since the last call and sends the
// player an updated QuestLog. Tick goroutine only.
func (s *Server) flushQuests() {
	if s.quests == nil {
		return
	}
	s.mu.Lock()
	full := s.questFull
	s.questFull = nil
	s.mu.Unlock()
	for _, id := range full {
		if g := s.live[id]; g != nil && g.conn != nil {
			g.conn.Send(encodeQuestResult(protocol.QuestResultCodeInventoryFull))
		}
	}
	for _, ch := range s.quests.Drain() {
		g := s.live[ch.Player]
		if g == nil {
			continue
		}
		if s.questStore != nil {
			s.saveQuests(g.storeID, s.questRows(ch.Records))
		}
		if g.conn != nil {
			s.sendQuestLog(g, ch.Records)
		}
	}
}

// saveQuests queues the upsert of rows behind the player's other writes. A
// failed save is logged, not retried: the next change saves the whole state.
func (s *Server) saveQuests(storeID int64, rows []store.QuestProgress) {
	if len(rows) == 0 {
		return
	}
	s.saver.submitEconomy(econReq{
		changes: []store.EconomyChange{{PlayerID: storeID}},
		write:   func(ctx context.Context) error { return s.questStore.SaveQuests(ctx, storeID, rows) },
		done: func(ok bool) {
			if !ok {
				s.log.Error("save quests failed", "store_id", storeID)
			}
		},
	})
}

func encodeQuestCatalog(d *gamedata.Data) []byte {
	b := flatbuffers.NewBuilder(2048)
	defs := make([]flatbuffers.UOffsetT, len(d.Quests))
	for i, q := range d.Quests {
		defs[i] = encodeQuestDef(b, d, q)
	}
	protocol.QuestCatalogStartQuestsVector(b, len(defs))
	for i := len(defs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(defs[i])
	}
	quests := b.EndVector(len(defs))
	protocol.QuestCatalogStart(b)
	protocol.QuestCatalogAddQuests(b, quests)
	return finish(b, protocol.MessageQuestCatalog, protocol.QuestCatalogEnd(b))
}

var questSkills = map[gamedata.Skill]protocol.QuestSkill{
	gamedata.CombatSkill:      protocol.QuestSkillCombat,
	gamedata.SkillWoodcutting: protocol.QuestSkillWoodcutting,
	gamedata.SkillFishing:     protocol.QuestSkillFishing,
	gamedata.SkillCooking:     protocol.QuestSkillCooking,
}

func encodeQuestDef(b *flatbuffers.Builder, d *gamedata.Data, q gamedata.Quest) flatbuffers.UOffsetT {
	name, desc := b.CreateString(q.Name), b.CreateString(q.Description)

	var reqs []flatbuffers.UOffsetT
	for _, l := range q.Requirements.Levels {
		protocol.QuestRequirementStart(b)
		protocol.QuestRequirementAddKind(b, protocol.QuestRequirementKindLevel)
		protocol.QuestRequirementAddSkill(b, questSkills[l.Skill])
		protocol.QuestRequirementAddLevel(b, clampU8(l.Level))
		reqs = append(reqs, protocol.QuestRequirementEnd(b))
	}
	for _, k := range q.Requirements.Quests {
		r, _ := d.QuestByKey(k)
		protocol.QuestRequirementStart(b)
		protocol.QuestRequirementAddKind(b, protocol.QuestRequirementKindQuest)
		protocol.QuestRequirementAddQuestId(b, uint16(r.ID))
		reqs = append(reqs, protocol.QuestRequirementEnd(b))
	}
	protocol.QuestDefStartRequirementsVector(b, len(reqs))
	for i := len(reqs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(reqs[i])
	}
	requirements := b.EndVector(len(reqs))

	steps := make([]flatbuffers.UOffsetT, len(q.Steps))
	for i, st := range q.Steps {
		steps[i] = b.CreateString(stepText(d, st))
	}
	protocol.QuestDefStartStepsVector(b, len(steps))
	for i := len(steps) - 1; i >= 0; i-- {
		b.PrependUOffsetT(steps[i])
	}
	stepsVec := b.EndVector(len(steps))

	rewards := encodeQuestRewards(b, q.Rewards)
	start, finish := questNPCs(d, q)

	protocol.QuestDefStart(b)
	protocol.QuestDefAddId(b, uint16(q.ID))
	protocol.QuestDefAddName(b, name)
	protocol.QuestDefAddDescription(b, desc)
	protocol.QuestDefAddRequirements(b, requirements)
	protocol.QuestDefAddSteps(b, stepsVec)
	protocol.QuestDefAddRewards(b, rewards)
	protocol.QuestDefAddStartNpc(b, uint16(start))
	protocol.QuestDefAddFinishNpc(b, uint16(finish))
	return protocol.QuestDefEnd(b)
}

func encodeQuestRewards(b *flatbuffers.Builder, r gamedata.QuestRewards) flatbuffers.UOffsetT {
	items := make([]flatbuffers.UOffsetT, len(r.Items))
	for i, it := range r.Items {
		protocol.QuestItemRewardStart(b)
		protocol.QuestItemRewardAddItemId(b, uint16(it.Item))
		protocol.QuestItemRewardAddCount(b, clampU16(it.Quantity))
		items[i] = protocol.QuestItemRewardEnd(b)
	}
	protocol.QuestRewardsStartItemsVector(b, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		b.PrependUOffsetT(items[i])
	}
	itemsVec := b.EndVector(len(items))

	xps := make([]flatbuffers.UOffsetT, len(r.XP))
	for i, x := range r.XP {
		protocol.QuestXpRewardStart(b)
		protocol.QuestXpRewardAddSkill(b, questSkills[x.Skill])
		protocol.QuestXpRewardAddXp(b, clampU32(x.XP))
		xps[i] = protocol.QuestXpRewardEnd(b)
	}
	protocol.QuestRewardsStartXpVector(b, len(xps))
	for i := len(xps) - 1; i >= 0; i-- {
		b.PrependUOffsetT(xps[i])
	}
	xpVec := b.EndVector(len(xps))

	protocol.QuestRewardsStart(b)
	protocol.QuestRewardsAddQuestPoints(b, clampU8(r.QuestPoints))
	protocol.QuestRewardsAddGold(b, clampU32(r.Gold))
	protocol.QuestRewardsAddItems(b, itemsVec)
	protocol.QuestRewardsAddXp(b, xpVec)
	return protocol.QuestRewardsEnd(b)
}

// questNPCs is the NPC type that starts the quest (the one whose dialogue has
// a reply that starts it) and the one that finishes it (the NPC of its last
// talk or bring step, else the starter).
func questNPCs(d *gamedata.Data, q gamedata.Quest) (start, finish gamedata.NPCTypeID) {
	for _, dl := range d.Dialogue {
		for _, n := range dl.Nodes {
			for _, r := range n.Replies {
				if r.StartQuest == q.Key {
					start = dl.NPC
				}
			}
		}
	}
	finish = start
	if len(q.Steps) > 0 {
		if st := q.Steps[len(q.Steps)-1]; st.Kind == gamedata.StepTalk || st.Kind == gamedata.StepBringItems {
			finish = st.NPC
		}
	}
	return start, finish
}

// stepText is the one-line description of a quest step shown in the log.
func stepText(d *gamedata.Data, st gamedata.QuestStep) string {
	npc := func(id gamedata.NPCTypeID, key string) string {
		if n, ok := d.NPC(id); ok {
			return n.Name
		}
		return key
	}
	item := func(it gamedata.ItemAmount) string {
		name := it.ItemKey
		if def, ok := d.Item(it.Item); ok {
			name = def.Name
		}
		if it.Quantity > 1 {
			return fmt.Sprintf("%d %s", it.Quantity, name)
		}
		return name
	}
	switch st.Kind {
	case gamedata.StepTalk:
		return "Talk to " + npc(st.NPC, st.NPCKey) + "."
	case gamedata.StepBringItems:
		s := ""
		for i, it := range st.Items {
			if i > 0 {
				s += ", "
			}
			s += item(it)
		}
		return "Bring " + s + " to " + npc(st.NPC, st.NPCKey) + "."
	case gamedata.StepKill:
		return fmt.Sprintf("Defeat %d %s.", st.Count, npc(st.NPC, st.NPCKey))
	case gamedata.StepReachLevel:
		return fmt.Sprintf("Reach %s level %d.", st.Skill, st.Level)
	case gamedata.StepQuestItem:
		return "Get " + item(gamedata.ItemAmount{ItemKey: st.ItemKey, Item: st.Item, Quantity: 1}) +
			" from " + npc(st.SourceNPC, st.SourceNPCKey) + "."
	}
	return ""
}

func encodeQuestLog(b *flatbuffers.Builder, recs []quest.Record, points int) []byte {
	b.Reset()
	rows := make([]flatbuffers.UOffsetT, len(recs))
	for i, r := range recs {
		protocol.QuestProgressStart(b)
		protocol.QuestProgressAddQuestId(b, uint16(r.Quest))
		switch r.Status {
		case quest.InProgress:
			protocol.QuestProgressAddState(b, protocol.QuestStateInProgress)
			protocol.QuestProgressAddStep(b, clampU8(r.Step))
			protocol.QuestProgressAddCount(b, clampU16(r.Count))
		case quest.Complete:
			protocol.QuestProgressAddState(b, protocol.QuestStateComplete)
		}
		rows[i] = protocol.QuestProgressEnd(b)
	}
	protocol.QuestLogStartQuestsVector(b, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		b.PrependUOffsetT(rows[i])
	}
	vec := b.EndVector(len(rows))
	protocol.QuestLogStart(b)
	protocol.QuestLogAddQuests(b, vec)
	protocol.QuestLogAddQuestPoints(b, clampU16(points))
	return finish(b, protocol.MessageQuestLog, protocol.QuestLogEnd(b))
}

func encodeStrings(b *flatbuffers.Builder, ss []string, start func(*flatbuffers.Builder, int) flatbuffers.UOffsetT) flatbuffers.UOffsetT {
	offs := make([]flatbuffers.UOffsetT, len(ss))
	for i, s := range ss {
		offs[i] = b.CreateString(s)
	}
	start(b, len(offs))
	for i := len(offs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(offs[i])
	}
	return b.EndVector(len(offs))
}

func encodeDialogue(d talk.Dialogue) []byte {
	b := flatbuffers.NewBuilder(256)
	lines := encodeStrings(b, d.Lines, protocol.DialogueStartLinesVector)
	replies := encodeStrings(b, d.Replies, protocol.DialogueStartRepliesVector)
	protocol.DialogueStart(b)
	protocol.DialogueAddNpcId(b, uint16(d.NPC))
	protocol.DialogueAddLines(b, lines)
	protocol.DialogueAddReplies(b, replies)
	return finish(b, protocol.MessageDialogue, protocol.DialogueEnd(b))
}

func encodeDialogueClose() []byte {
	b := flatbuffers.NewBuilder(32)
	protocol.DialogueCloseStart(b)
	return finish(b, protocol.MessageDialogueClose, protocol.DialogueCloseEnd(b))
}

func encodeQuestResult(code protocol.QuestResultCode) []byte {
	b := flatbuffers.NewBuilder(32)
	protocol.QuestResultStart(b)
	protocol.QuestResultAddCode(b, code)
	return finish(b, protocol.MessageQuestResult, protocol.QuestResultEnd(b))
}
