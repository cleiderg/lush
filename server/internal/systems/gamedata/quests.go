package gamedata

import (
	"fmt"
)

// File names for the quest tables (docs/specs/quests.md).
const (
	QuestsFile   = "quests.json"
	DialogueFile = "dialogue.json"
)

// Quest limits (docs/specs/quests.md: no quest longer than 4 steps, up to 4 reply buttons).
const (
	MaxQuestSteps      = 4
	MaxDialogueReplies = 4
)

// CombatSkill is the skill name for combat level and combat XP in quest data.
const CombatSkill Skill = "combat"

// QuestID identifies a quest. 0 is never a valid id.
type QuestID uint16

// StepKind is what a quest step asks of the player.
type StepKind string

const (
	StepTalk       StepKind = "talk"        // talk to an NPC
	StepBringItems StepKind = "bring_items" // bring items to an NPC (they are taken)
	StepKill       StepKind = "kill"        // kill a count of an NPC type
	StepReachLevel StepKind = "reach_level" // reach a level in a skill
	StepQuestItem  StepKind = "quest_item"  // get a quest item from an NPC's drop
)

// LevelReq is a skill level, as a quest requirement or a reach_level target.
type LevelReq struct {
	Skill Skill `json:"skill"`
	Level int   `json:"level"`
}

// QuestRequirements are what a player needs before starting a quest.
type QuestRequirements struct {
	Levels []LevelReq `json:"levels"`
	Quests []string   `json:"quests"` // keys of quests that must be complete
}

// ItemAmount is a quantity of one item.
type ItemAmount struct {
	ItemKey  string `json:"item"`
	Quantity int    `json:"quantity"`
	Item     ItemID `json:"-"` // resolved from ItemKey by Load
}

// QuestStep is one step. Which fields apply depends on Kind.
type QuestStep struct {
	Kind StepKind `json:"kind"`
	// NPCKey is the talking NPC (talk, bring_items) or the NPC type to kill (kill).
	NPCKey string    `json:"npc"`
	NPC    NPCTypeID `json:"-"`
	// Items are taken by bring_items.
	Items []ItemAmount `json:"items"`
	// Count is how many kills (kill).
	Count int `json:"count"`
	// Skill and Level are the target of reach_level.
	Skill Skill `json:"skill"`
	Level int   `json:"level"`
	// ItemKey is the quest item of a quest_item step; SourceNPCKey is the NPC
	// type that drops it, one kill in Chance while the player is on this step.
	ItemKey      string    `json:"item"`
	Item         ItemID    `json:"-"`
	SourceNPCKey string    `json:"source_npc"`
	SourceNPC    NPCTypeID `json:"-"`
	Chance       int       `json:"chance"`
}

// SkillXP is an XP reward in one skill (or combat).
type SkillXP struct {
	Skill Skill `json:"skill"`
	XP    int   `json:"xp"`
}

// QuestRewards are given when the last step completes. Gold and items go
// through the economy audit log.
type QuestRewards struct {
	QuestPoints int          `json:"quest_points"`
	Gold        int          `json:"gold"`
	Items       []ItemAmount `json:"items"`
	XP          []SkillXP    `json:"xp"`
}

// Quest is one row of quests.json.
type Quest struct {
	ID           QuestID           `json:"id"`
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Requirements QuestRequirements `json:"requirements"`
	Steps        []QuestStep       `json:"steps"`
	Rewards      QuestRewards      `json:"rewards"`
}

// QuestState is the condition a dialogue node can test.
type QuestState string

const (
	QuestNotStarted QuestState = "not_started"
	QuestOnStep     QuestState = "step"
	QuestComplete   QuestState = "complete"
)

// DialogueCond makes a node an entry point for players in a given quest state.
type DialogueCond struct {
	Quest string     `json:"quest"`
	State QuestState `json:"state"`
	Step  int        `json:"step"` // 0-based; only with state "step"
}

// DialogueReply is one button. Next is empty when the reply ends the talk.
type DialogueReply struct {
	Text       string `json:"text"`
	Next       string `json:"next"`
	StartQuest string `json:"start_quest"` // key of the quest this reply starts
	FinishTalk bool   `json:"finish_talk"` // finishes the talk or bring step the player is on
}

// DialogueNode is some lines of text and up to 4 replies. A node with When is
// an entry point: the first one whose condition holds opens the talk.
type DialogueNode struct {
	ID      string          `json:"id"`
	When    *DialogueCond   `json:"when"`
	Text    []string        `json:"text"`
	Replies []DialogueReply `json:"replies"`
}

// Dialogue is one talking NPC's conversation tree. Start opens the talk when
// no conditional node matches.
type Dialogue struct {
	NPCKey string         `json:"npc"`
	NPC    NPCTypeID      `json:"-"`
	Start  string         `json:"start"`
	Nodes  []DialogueNode `json:"nodes"`
}

// Quest returns the quest with the given id.
func (d *Data) Quest(id QuestID) (Quest, bool) {
	i, ok := d.questByID[id]
	if !ok {
		return Quest{}, false
	}
	return d.Quests[i], true
}

// QuestByKey returns the quest with the given key.
func (d *Data) QuestByKey(key string) (Quest, bool) {
	i, ok := d.questByKey[key]
	if !ok {
		return Quest{}, false
	}
	return d.Quests[i], true
}

// DialogueFor returns the conversation of the talking NPC type with the given id.
func (d *Data) DialogueFor(id NPCTypeID) (Dialogue, bool) {
	i, ok := d.dialogueByNPC[id]
	if !ok {
		return Dialogue{}, false
	}
	return d.Dialogue[i], true
}

// Node returns the node with the given id.
func (dl Dialogue) Node(id string) (DialogueNode, bool) {
	for _, n := range dl.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return DialogueNode{}, false
}

// questSkill reports whether s can be named in quest data: combat or a skill from skills.md.
func questSkill(s Skill) bool {
	return s == CombatSkill || s == SkillWoodcutting || s == SkillFishing || s == SkillCooking
}

// validateTalker checks a talkable NPC type has a spot and no combat data.
func (n NPCType) validateTalker(where string) error {
	if n.Spot == "" {
		return fmt.Errorf("gamedata: %s: talkable npc needs a spot", where)
	}
	if n.Area != "" || n.SpawnZone != "" || n.Level != 0 || n.HP != 0 || n.Stats != (Stats{}) ||
		n.AttackSpeed != 0 || n.Behaviour != "" || n.RespawnSeconds != 0 || n.Population != 0 ||
		n.MaxAlive != 0 || n.GoldMin != 0 || n.GoldMax != 0 || len(n.Drops) != 0 {
		return fmt.Errorf("gamedata: %s: talkable npc must have no combat stats, population or drops", where)
	}
	return nil
}

func (d *Data) questLevel(where string, r LevelReq) error {
	if !questSkill(r.Skill) {
		return fmt.Errorf("gamedata: %s: unknown skill %q", where, r.Skill)
	}
	max := MaxSkillLevel
	if r.Skill == CombatSkill {
		max = d.Rules.MaxCombatLevel
	}
	if r.Level < MinSkillLevel || r.Level > max {
		return fmt.Errorf("gamedata: %s: %s level %d must be %d to %d", where, r.Skill, r.Level, MinSkillLevel, max)
	}
	return nil
}

func (d *Data) itemAmounts(where string, list []ItemAmount) error {
	for k := range list {
		a := &list[k]
		id, err := d.resolve(a.ItemKey, where)
		if err != nil {
			return err
		}
		if a.Quantity < 1 {
			return fmt.Errorf("gamedata: %s: item %q needs a quantity of at least 1", where, a.ItemKey)
		}
		a.Item = id
	}
	return nil
}

// talker resolves a talkable NPC key.
func (d *Data) talker(key string) (NPCTypeID, bool) {
	i, ok := d.talkerByKey[key]
	if !ok {
		return 0, false
	}
	return d.Talkers[i].ID, true
}

// validateQuests checks quests.json: unique ids and keys, known item, NPC, skill
// and quest keys, at most MaxQuestSteps steps, and no cycle in the requirements.
func (d *Data) validateQuests() error {
	d.questByID = map[QuestID]int{}
	d.questByKey = map[string]int{}
	for i, q := range d.Quests {
		where := fmt.Sprintf("%s: quest %d (%q)", QuestsFile, q.ID, q.Key)
		if q.ID == 0 || q.Key == "" || q.Name == "" || q.Description == "" {
			return fmt.Errorf("gamedata: %s: id, key, name and description are required", where)
		}
		if _, dup := d.questByID[q.ID]; dup {
			return fmt.Errorf("gamedata: %s: duplicate id", where)
		}
		if _, dup := d.questByKey[q.Key]; dup {
			return fmt.Errorf("gamedata: %s: duplicate key", where)
		}
		d.questByID[q.ID] = i
		d.questByKey[q.Key] = i
	}
	for i := range d.Quests {
		q := &d.Quests[i]
		where := fmt.Sprintf("%s: quest %q", QuestsFile, q.Key)
		for _, r := range q.Requirements.Levels {
			if err := d.questLevel(where+" requirement", r); err != nil {
				return err
			}
		}
		for _, k := range q.Requirements.Quests {
			if _, ok := d.questByKey[k]; !ok {
				return fmt.Errorf("gamedata: %s: requires unknown quest %q", where, k)
			}
		}
		if len(q.Steps) == 0 || len(q.Steps) > MaxQuestSteps {
			return fmt.Errorf("gamedata: %s: %d steps, want 1 to %d", where, len(q.Steps), MaxQuestSteps)
		}
		for s := range q.Steps {
			if err := d.validateStep(fmt.Sprintf("%s step %d", where, s+1), &q.Steps[s]); err != nil {
				return err
			}
		}
		r := &q.Rewards
		if r.QuestPoints < 0 || r.Gold < 0 {
			return fmt.Errorf("gamedata: %s: negative reward", where)
		}
		if err := d.itemAmounts(where+" reward", r.Items); err != nil {
			return err
		}
		for _, x := range r.XP {
			if !questSkill(x.Skill) {
				return fmt.Errorf("gamedata: %s: reward in unknown skill %q", where, x.Skill)
			}
			if x.XP < 1 {
				return fmt.Errorf("gamedata: %s: %s xp reward must be positive", where, x.Skill)
			}
		}
	}
	return d.checkQuestCycles()
}

func (d *Data) validateStep(where string, s *QuestStep) error {
	switch s.Kind {
	case StepTalk, StepBringItems:
		id, ok := d.talker(s.NPCKey)
		if !ok {
			return fmt.Errorf("gamedata: %s: unknown talking npc %q", where, s.NPCKey)
		}
		s.NPC = id
		if s.Kind == StepBringItems {
			if len(s.Items) == 0 {
				return fmt.Errorf("gamedata: %s: bring_items needs items", where)
			}
			return d.itemAmounts(where, s.Items)
		}
	case StepKill:
		i, ok := d.npcByKey[s.NPCKey]
		if !ok {
			return fmt.Errorf("gamedata: %s: unknown npc %q", where, s.NPCKey)
		}
		if s.Count < 1 {
			return fmt.Errorf("gamedata: %s: kill count must be at least 1", where)
		}
		s.NPC = d.NPCs[i].ID
	case StepReachLevel:
		return d.questLevel(where, LevelReq{s.Skill, s.Level})
	case StepQuestItem:
		id, err := d.resolve(s.ItemKey, where)
		if err != nil {
			return err
		}
		if !d.Items[d.itemByKey[s.ItemKey]].QuestItem {
			return fmt.Errorf("gamedata: %s: item %q is not a quest item", where, s.ItemKey)
		}
		i, ok := d.npcByKey[s.SourceNPCKey]
		if !ok {
			return fmt.Errorf("gamedata: %s: unknown source npc %q", where, s.SourceNPCKey)
		}
		if s.Chance < 1 {
			return fmt.Errorf("gamedata: %s: chance must be at least 1", where)
		}
		s.Item, s.SourceNPC = id, d.NPCs[i].ID
	default:
		return fmt.Errorf("gamedata: %s: unknown step kind %q", where, s.Kind)
	}
	return nil
}

// checkQuestCycles rejects quests that require each other, directly or not.
func (d *Data) checkQuestCycles() error {
	const (
		visiting = 1
		done     = 2
	)
	mark := map[string]int{}
	var visit func(key string) error
	visit = func(key string) error {
		switch mark[key] {
		case visiting:
			return fmt.Errorf("gamedata: %s: quest %q requires itself through a cycle", QuestsFile, key)
		case done:
			return nil
		}
		mark[key] = visiting
		for _, r := range d.Quests[d.questByKey[key]].Requirements.Quests {
			if err := visit(r); err != nil {
				return err
			}
		}
		mark[key] = done
		return nil
	}
	for _, q := range d.Quests {
		if err := visit(q.Key); err != nil {
			return err
		}
	}
	return nil
}

// validateDialogue checks dialogue.json: every talking NPC has a conversation,
// replies number 1 to MaxDialogueReplies, quest keys and steps exist, and every
// node is reachable from the start or a conditional entry, with no dangling next.
func (d *Data) validateDialogue() error {
	d.dialogueByNPC = map[NPCTypeID]int{}
	for i := range d.Dialogue {
		dl := &d.Dialogue[i]
		where := fmt.Sprintf("%s: npc %q", DialogueFile, dl.NPCKey)
		id, ok := d.talker(dl.NPCKey)
		if !ok {
			return fmt.Errorf("gamedata: %s: unknown talking npc", where)
		}
		if _, dup := d.dialogueByNPC[id]; dup {
			return fmt.Errorf("gamedata: %s: listed twice", where)
		}
		dl.NPC = id
		d.dialogueByNPC[id] = i
		if err := d.validateTree(where, dl); err != nil {
			return err
		}
	}
	for _, t := range d.Talkers {
		if _, ok := d.dialogueByNPC[t.ID]; !ok {
			return fmt.Errorf("gamedata: %s: talking npc %q has no dialogue", DialogueFile, t.Key)
		}
	}
	return nil
}

func (d *Data) validateTree(where string, dl *Dialogue) error {
	byID := map[string]int{}
	for i, n := range dl.Nodes {
		if n.ID == "" {
			return fmt.Errorf("gamedata: %s: node without id", where)
		}
		if _, dup := byID[n.ID]; dup {
			return fmt.Errorf("gamedata: %s: duplicate node %q", where, n.ID)
		}
		byID[n.ID] = i
	}
	if _, ok := byID[dl.Start]; !ok {
		return fmt.Errorf("gamedata: %s: start node %q does not exist", where, dl.Start)
	}
	for _, n := range dl.Nodes {
		nw := fmt.Sprintf("%s node %q", where, n.ID)
		if len(n.Text) == 0 {
			return fmt.Errorf("gamedata: %s: needs at least one line of text", nw)
		}
		if len(n.Replies) == 0 || len(n.Replies) > MaxDialogueReplies {
			return fmt.Errorf("gamedata: %s: %d replies, want 1 to %d", nw, len(n.Replies), MaxDialogueReplies)
		}
		if err := d.validateCond(nw, dl, n.When); err != nil {
			return err
		}
		for _, r := range n.Replies {
			if r.Text == "" {
				return fmt.Errorf("gamedata: %s: reply without text", nw)
			}
			if r.Next != "" {
				if _, ok := byID[r.Next]; !ok {
					return fmt.Errorf("gamedata: %s: reply %q leads to unknown node %q", nw, r.Text, r.Next)
				}
			}
			if r.StartQuest != "" {
				qi, ok := d.questByKey[r.StartQuest]
				if !ok {
					return fmt.Errorf("gamedata: %s: reply %q starts unknown quest %q", nw, r.Text, r.StartQuest)
				}
				if first := d.Quests[qi].Steps[0]; first.Kind != StepTalk || first.NPC != dl.NPC {
					return fmt.Errorf("gamedata: %s: quest %q does not start with a talk to this npc", nw, r.StartQuest)
				}
			}
		}
	}
	// Walk from the start and every conditional entry node.
	seen := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, r := range dl.Nodes[byID[id]].Replies {
			if r.Next != "" {
				walk(r.Next)
			}
		}
	}
	walk(dl.Start)
	for _, n := range dl.Nodes {
		if n.When != nil {
			walk(n.ID)
		}
	}
	for _, n := range dl.Nodes {
		if !seen[n.ID] {
			return fmt.Errorf("gamedata: %s: node %q is unreachable", where, n.ID)
		}
	}
	return nil
}

func (d *Data) validateCond(where string, dl *Dialogue, c *DialogueCond) error {
	if c == nil {
		return nil
	}
	qi, ok := d.questByKey[c.Quest]
	if !ok {
		return fmt.Errorf("gamedata: %s: condition on unknown quest %q", where, c.Quest)
	}
	q := d.Quests[qi]
	switch c.State {
	case QuestNotStarted, QuestComplete:
		if c.Step != 0 {
			return fmt.Errorf("gamedata: %s: step only goes with state %q", where, QuestOnStep)
		}
	case QuestOnStep:
		if c.Step < 0 || c.Step >= len(q.Steps) {
			return fmt.Errorf("gamedata: %s: quest %q has no step %d", where, c.Quest, c.Step)
		}
		if st := q.Steps[c.Step]; (st.Kind == StepTalk || st.Kind == StepBringItems) && st.NPC != dl.NPC {
			return fmt.Errorf("gamedata: %s: step %d of %q is for another npc", where, c.Step, c.Quest)
		}
	default:
		return fmt.Errorf("gamedata: %s: unknown quest state %q", where, c.State)
	}
	return nil
}
