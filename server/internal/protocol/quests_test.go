package protocol

import (
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
)

func buildRewards(b *flatbuffers.Builder) flatbuffers.UOffsetT {
	QuestItemRewardStart(b)
	QuestItemRewardAddItemId(b, 301)
	QuestItemRewardAddCount(b, 1)
	it := QuestItemRewardEnd(b)
	QuestRewardsStartItemsVector(b, 1)
	b.PrependUOffsetT(it)
	items := b.EndVector(1)
	QuestXpRewardStart(b)
	QuestXpRewardAddSkill(b, QuestSkillCooking)
	QuestXpRewardAddXp(b, 1500)
	xp := QuestXpRewardEnd(b)
	QuestRewardsStartXpVector(b, 1)
	b.PrependUOffsetT(xp)
	xps := b.EndVector(1)
	QuestRewardsStart(b)
	QuestRewardsAddQuestPoints(b, 1)
	QuestRewardsAddGold(b, 250)
	QuestRewardsAddItems(b, items)
	QuestRewardsAddXp(b, xps)
	return QuestRewardsEnd(b)
}

func checkRewards(t *testing.T, r *QuestRewards) {
	t.Helper()
	var it QuestItemReward
	var xp QuestXpReward
	if r.QuestPoints() != 1 || r.Gold() != 250 || r.ItemsLength() != 1 || r.XpLength() != 1 ||
		!r.Items(&it, 0) || it.ItemId() != 301 || it.Count() != 1 ||
		!r.Xp(&xp, 0) || xp.Skill() != QuestSkillCooking || xp.Xp() != 1500 {
		t.Fatal("rewards mismatch")
	}
}

func TestRoundTripQuestCatalog(t *testing.T) {
	b := flatbuffers.NewBuilder(512)
	name := b.CreateString("Overdue")
	desc := b.CreateString("Return the book.")
	s1 := b.CreateString("Find the book.")
	s2 := b.CreateString("Return it.")
	QuestDefStartStepsVector(b, 2)
	b.PrependUOffsetT(s2)
	b.PrependUOffsetT(s1)
	steps := b.EndVector(2)
	QuestRequirementStart(b)
	QuestRequirementAddKind(b, QuestRequirementKindLevel)
	QuestRequirementAddSkill(b, QuestSkillCombat)
	QuestRequirementAddLevel(b, 5)
	r1 := QuestRequirementEnd(b)
	QuestRequirementStart(b)
	QuestRequirementAddKind(b, QuestRequirementKindQuest)
	QuestRequirementAddQuestId(b, 2)
	r2 := QuestRequirementEnd(b)
	QuestDefStartRequirementsVector(b, 2)
	b.PrependUOffsetT(r2)
	b.PrependUOffsetT(r1)
	reqs := b.EndVector(2)
	rw := buildRewards(b)
	QuestDefStart(b)
	QuestDefAddId(b, 1)
	QuestDefAddName(b, name)
	QuestDefAddDescription(b, desc)
	QuestDefAddRequirements(b, reqs)
	QuestDefAddSteps(b, steps)
	QuestDefAddRewards(b, rw)
	QuestDefAddStartNpc(b, 7)
	QuestDefAddFinishNpc(b, 8)
	q := QuestDefEnd(b)
	QuestCatalogStartQuestsVector(b, 1)
	b.PrependUOffsetT(q)
	qs := b.EndVector(1)
	QuestCatalogStart(b)
	QuestCatalogAddQuests(b, qs)
	buf := finish(b, MessageQuestCatalog, QuestCatalogEnd(b))

	var m QuestCatalog
	tbl := open(t, buf, MessageQuestCatalog)
	m.Init(tbl.Bytes, tbl.Pos)
	var d QuestDef
	if m.QuestsLength() != 1 || !m.Quests(&d, 0) {
		t.Fatal("no quest")
	}
	if d.Id() != 1 || string(d.Name()) != "Overdue" || string(d.Description()) != "Return the book." ||
		d.StepsLength() != 2 || string(d.Steps(1)) != "Return it." ||
		d.StartNpc() != 7 || d.FinishNpc() != 8 || d.RequirementsLength() != 2 {
		t.Fatal("quest mismatch")
	}
	var rq QuestRequirement
	d.Requirements(&rq, 0)
	if rq.Kind() != QuestRequirementKindLevel || rq.Skill() != QuestSkillCombat || rq.Level() != 5 {
		t.Fatal("req 0 mismatch")
	}
	d.Requirements(&rq, 1)
	if rq.Kind() != QuestRequirementKindQuest || rq.QuestId() != 2 {
		t.Fatal("req 1 mismatch")
	}
	r := d.Rewards(nil)
	checkRewards(t, r)
}

func TestRoundTripQuestLog(t *testing.T) {
	b := flatbuffers.NewBuilder(128)
	QuestProgressStart(b)
	QuestProgressAddQuestId(b, 1)
	QuestProgressAddState(b, QuestStateInProgress)
	QuestProgressAddStep(b, 2)
	QuestProgressAddCount(b, 3)
	p1 := QuestProgressEnd(b)
	QuestProgressStart(b)
	QuestProgressAddQuestId(b, 2)
	p2 := QuestProgressEnd(b)
	QuestLogStartQuestsVector(b, 2)
	b.PrependUOffsetT(p2)
	b.PrependUOffsetT(p1)
	v := b.EndVector(2)
	QuestLogStart(b)
	QuestLogAddQuests(b, v)
	QuestLogAddQuestPoints(b, 4)
	buf := finish(b, MessageQuestLog, QuestLogEnd(b))

	var m QuestLog
	tbl := open(t, buf, MessageQuestLog)
	m.Init(tbl.Bytes, tbl.Pos)
	var p QuestProgress
	m.Quests(&p, 0)
	if m.QuestsLength() != 2 || m.QuestPoints() != 4 || p.QuestId() != 1 ||
		p.State() != QuestStateInProgress || p.Step() != 2 || p.Count() != 3 {
		t.Fatal("log mismatch")
	}
	m.Quests(&p, 1)
	if p.State() != QuestStateNotStarted || p.Step() != 0 || p.Count() != 0 {
		t.Fatal("default progress mismatch")
	}
}

func TestRoundTripTalkAndDialogue(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	TalkStart(b)
	TalkAddNpcId(b, 42)
	buf := finish(b, MessageTalk, TalkEnd(b))
	var tk Talk
	tbl := open(t, buf, MessageTalk)
	tk.Init(tbl.Bytes, tbl.Pos)
	if tk.NpcId() != 42 {
		t.Fatal("talk mismatch")
	}

	b = flatbuffers.NewBuilder(64)
	DialogueReplyStart(b)
	DialogueReplyAddIndex(b, 3)
	buf = finish(b, MessageDialogueReply, DialogueReplyEnd(b))
	var dr DialogueReply
	tbl = open(t, buf, MessageDialogueReply)
	dr.Init(tbl.Bytes, tbl.Pos)
	if dr.Index() != 3 {
		t.Fatal("reply mismatch")
	}

	b = flatbuffers.NewBuilder(256)
	l1 := b.CreateString("Hello.")
	l2 := b.CreateString("Need help?")
	DialogueStartLinesVector(b, 2)
	b.PrependUOffsetT(l2)
	b.PrependUOffsetT(l1)
	lines := b.EndVector(2)
	var rs []flatbuffers.UOffsetT
	for _, s := range []string{"a", "b", "c", "d"} {
		rs = append(rs, b.CreateString(s))
	}
	DialogueStartRepliesVector(b, 4)
	for i := 3; i >= 0; i-- {
		b.PrependUOffsetT(rs[i])
	}
	replies := b.EndVector(4)
	DialogueStart(b)
	DialogueAddNpcId(b, 42)
	DialogueAddLines(b, lines)
	DialogueAddReplies(b, replies)
	buf = finish(b, MessageDialogue, DialogueEnd(b))
	var d Dialogue
	tbl = open(t, buf, MessageDialogue)
	d.Init(tbl.Bytes, tbl.Pos)
	if d.NpcId() != 42 || d.LinesLength() != 2 || string(d.Lines(1)) != "Need help?" ||
		d.RepliesLength() != 4 || string(d.Replies(3)) != "d" {
		t.Fatal("dialogue mismatch")
	}

	b = flatbuffers.NewBuilder(16)
	DialogueCloseStart(b)
	buf = finish(b, MessageDialogueClose, DialogueCloseEnd(b))
	open(t, buf, MessageDialogueClose)
}

func TestRoundTripQuestCompletedAndResult(t *testing.T) {
	b := flatbuffers.NewBuilder(256)
	pn := b.CreateString("Cleider")
	qn := b.CreateString("Overdue")
	rw := buildRewards(b)
	QuestCompletedStart(b)
	QuestCompletedAddPlayerName(b, pn)
	QuestCompletedAddQuestId(b, 1)
	QuestCompletedAddQuestName(b, qn)
	QuestCompletedAddRewards(b, rw)
	buf := finish(b, MessageQuestCompleted, QuestCompletedEnd(b))
	var m QuestCompleted
	tbl := open(t, buf, MessageQuestCompleted)
	m.Init(tbl.Bytes, tbl.Pos)
	if string(m.PlayerName()) != "Cleider" || string(m.QuestName()) != "Overdue" || m.QuestId() != 1 {
		t.Fatal("completed mismatch")
	}
	checkRewards(t, m.Rewards(nil))

	for _, code := range []QuestResultCode{QuestResultCodeTooFar, QuestResultCodeNotTalkable,
		QuestResultCodeRequirementsNotMet, QuestResultCodeInventoryFull} {
		b = flatbuffers.NewBuilder(16)
		QuestResultStart(b)
		QuestResultAddCode(b, code)
		buf = finish(b, MessageQuestResult, QuestResultEnd(b))
		var r QuestResult
		tbl = open(t, buf, MessageQuestResult)
		r.Init(tbl.Bytes, tbl.Pos)
		if r.Code() != code {
			t.Fatalf("code = %v, want %v", r.Code(), code)
		}
	}
}

// Buffers written before quest_item and talkable existed read them as false.
func TestQuestFlagsDefaultFalse(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	ItemDefStart(b)
	ItemDefAddId(b, 9)
	b.Finish(ItemDefEnd(b))
	if GetRootAsItemDef(b.FinishedBytes(), 0).QuestItem() {
		t.Fatal("quest_item should default to false")
	}
	b = flatbuffers.NewBuilder(64)
	NpcDefStart(b)
	NpcDefAddId(b, 9)
	b.Finish(NpcDefEnd(b))
	if GetRootAsNpcDef(b.FinishedBytes(), 0).Talkable() {
		t.Fatal("talkable should default to false")
	}
	b = flatbuffers.NewBuilder(64)
	ItemDefStart(b)
	ItemDefAddQuestItem(b, true)
	b.Finish(ItemDefEnd(b))
	if !GetRootAsItemDef(b.FinishedBytes(), 0).QuestItem() {
		t.Fatal("quest_item true lost")
	}
	b = flatbuffers.NewBuilder(64)
	NpcDefStart(b)
	NpcDefAddTalkable(b, true)
	b.Finish(NpcDefEnd(b))
	if !GetRootAsNpcDef(b.FinishedBytes(), 0).Talkable() {
		t.Fatal("talkable true lost")
	}
}
