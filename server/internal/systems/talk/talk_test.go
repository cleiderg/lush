package talk

import (
	"bytes"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/quest"
	"github.com/cleiderg/lush/server/internal/world"
)

const sharedTalkers = "../../../../shared/map/talkers.json"

type sink struct {
	results   []Result
	dialogues []Dialogue
	closed    int
	completed []quest.Completion
	taken     [][]gamedata.ItemAmount
}

func (s *sink) QuestResult(_ world.PlayerID, r Result) { s.results = append(s.results, r) }
func (s *sink) Dialogue(_ world.PlayerID, d Dialogue)  { s.dialogues = append(s.dialogues, d) }
func (s *sink) DialogueClose(world.PlayerID)           { s.closed++ }
func (s *sink) QuestCompleted(_ world.PlayerID, c quest.Completion, take []gamedata.ItemAmount) {
	s.completed = append(s.completed, c)
	s.taken = append(s.taken, take)
}

type committer struct{ reqs []CommitRequest }

func (c *committer) CommitQuest(r CommitRequest) { c.reqs = append(c.reqs, r) }

type fixture struct {
	cm   *committer
	t    *testing.T
	d    *gamedata.Data
	eng  *quest.Engine
	sys  *System
	sink *sink
	w    *world.World
	p    *world.Player
}

func setup(t *testing.T) *fixture {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	eng := quest.New(d)
	sk := &sink{}
	cm := &committer{}
	sys, err := New(d, eng, 1, sk, cm)
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(world.DefaultConfig())
	if err := w.Register(sys); err != nil {
		t.Fatal(err)
	}
	p := &world.Player{ID: 1, Inv: world.NewInventory(d)}
	if err := w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	w.Tick() // spawns the talkers
	return &fixture{cm, t, d, eng, sys, sk, w, p}
}

func (f *fixture) npcOf(key string) *world.NPC {
	f.t.Helper()
	nt, ok := f.d.NPCByKey(key)
	if !ok {
		f.t.Fatalf("no npc %q", key)
	}
	for _, n := range f.w.NPCs() {
		if n.Type == world.NPCTypeID(nt.ID) {
			return n
		}
	}
	f.t.Fatalf("npc %q not in the world", key)
	return nil
}

func (f *fixture) item(key string) gamedata.ItemID {
	it, ok := f.d.ItemByKey(key)
	if !ok {
		f.t.Fatalf("no item %q", key)
	}
	return it.ID
}

func (f *fixture) talk(n *world.NPC) {
	f.w.Enqueue(TalkCommand{Player: f.p.ID, NPC: n.ID})
	f.w.Tick()
}

func (f *fixture) reply(i int) {
	f.w.Enqueue(ReplyCommand{Player: f.p.ID, Index: i})
	f.w.Tick()
}

func (f *fixture) last() Dialogue { return f.sink.dialogues[len(f.sink.dialogues)-1] }

func TestEmbeddedTalkersMatchShared(t *testing.T) {
	shared, err := os.ReadFile(sharedTalkers)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embeddedTalkers, shared) {
		t.Fatal("talkers.json differs from shared/map/talkers.json; refresh it from the repo root with: " +
			"cp shared/map/talkers.json server/internal/systems/talk/talkers.json")
	}
}

func TestEverySpotResolves(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Talkers) == 0 {
		t.Fatal("no talking NPC types")
	}
	if _, err := resolve(d, embeddedTalkers); err != nil {
		t.Fatal(err)
	}
	missing := []byte(`{"version":1,"units":"decimetres","talkers":[{"id":"librarian","x":1,"z":2}]}`)
	if _, err := resolve(d, missing); err == nil || !strings.Contains(err.Error(), "not in talkers.json") {
		t.Fatalf("missing spot: err = %v", err)
	}
}

func TestTalkersStayPut(t *testing.T) {
	f := setup(t)
	d := f.d
	if got := len(f.w.NPCs()); got != len(d.Talkers) {
		t.Fatalf("%d NPCs, want one per talking type (%d)", got, len(d.Talkers))
	}
	type snap struct {
		id  world.NPCID
		pos world.Vec2
	}
	var want []snap
	for _, n := range f.w.NPCs() {
		tt, _ := d.NPC(gamedata.NPCTypeID(n.Type))
		spot, _ := resolve(d, embeddedTalkers)
		if n.Pos != spot[tt.ID] || n.Home != n.Pos {
			t.Fatalf("%s at %v home %v, want spot %v", tt.Key, n.Pos, n.Home, spot[tt.ID])
		}
		want = append(want, snap{n.ID, n.Pos})
	}
	for range 5000 {
		f.w.Tick()
	}
	if len(f.w.NPCs()) != len(want) {
		t.Fatalf("%d NPCs after ticks, want %d", len(f.w.NPCs()), len(want))
	}
	for _, s := range want {
		n := f.w.NPC(s.id)
		if n == nil || n.Pos != s.pos || n.Home != s.pos || n.HP != 0 || n.HasGoal {
			t.Fatalf("npc %d moved or changed: %+v, want at %v", s.id, n, s.pos)
		}
	}
}

func TestTalkRefusals(t *testing.T) {
	f := setup(t)
	lib := f.npcOf("librarian")
	rabbit, ok := f.d.NPCByKey("rabbit")
	if !ok {
		t.Fatal("no rabbit")
	}
	r := &world.NPC{Type: world.NPCTypeID(rabbit.ID), HP: rabbit.HP}
	if _, err := f.w.AddNPC(r); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		pos  world.Vec2
		npc  world.NPCID
		want Result
	}{
		{"beyond 2 tiles", world.Vec2{X: lib.Pos.X + 2.5}, lib.ID, TooFar},
		{"non-talking npc", r.Pos, r.ID, NotTalkable},
		{"no such npc", lib.Pos, 0xFFF0, NotTalkable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.sink.results, f.sink.dialogues = nil, nil
			f.p.Pos = tt.pos
			f.w.Enqueue(TalkCommand{Player: f.p.ID, NPC: tt.npc})
			f.w.Tick()
			if len(f.sink.results) != 1 || f.sink.results[0] != tt.want || len(f.sink.dialogues) != 0 {
				t.Fatalf("results %v dialogues %v, want [%v]", f.sink.results, f.sink.dialogues, tt.want)
			}
		})
	}
}

func TestTalkOpensNodeForQuestState(t *testing.T) {
	f := setup(t)
	lib := f.npcOf("librarian")
	f.p.Pos = world.Vec2{X: lib.Pos.X + 2, Y: lib.Pos.Y} // exactly 2 tiles
	f.talk(lib)
	if len(f.sink.results) != 0 || len(f.sink.dialogues) != 1 {
		t.Fatalf("results %v dialogues %v", f.sink.results, f.sink.dialogues)
	}
	nd, _ := mustDialogue(t, f.d, "librarian").Node("greeting")
	if got := f.last(); got.NPC != lib.ID || strings.Join(got.Lines, "|") != strings.Join(nd.Text, "|") || len(got.Replies) != 2 {
		t.Fatalf("dialogue = %+v", got)
	}
}

func mustDialogue(t *testing.T, d *gamedata.Data, key string) (dl gamedata.Dialogue) {
	t.Helper()
	nt, _ := d.NPCByKey(key)
	dl, ok := d.DialogueFor(nt.ID)
	if !ok {
		t.Fatalf("no dialogue for %q", key)
	}
	return dl
}

func TestOverdueFirstAndLastTalk(t *testing.T) {
	f := setup(t)
	q, _ := f.d.QuestByKey("overdue")
	lib := f.npcOf("librarian")
	f.p.Pos = lib.Pos

	// First talk: accept, then confirm.
	f.talk(lib)
	f.reply(0) // I'll find it: starts the quest
	if got := f.sys.State(f.p.ID).Get(q.ID); got.Status != quest.InProgress || got.Step != 0 {
		t.Fatalf("after start: %+v", got)
	}
	want, _ := mustDialogue(t, f.d, "librarian").Node("thanks")
	if got := f.last(); strings.Join(got.Lines, "|") != strings.Join(want.Text, "|") {
		t.Fatalf("next node after start = %+v", got)
	}
	f.reply(0) // On my way: finishes the talk step
	if got := f.sys.State(f.p.ID).Get(q.ID); got.Status != quest.InProgress || got.Step != 1 {
		t.Fatalf("after first talk: %+v", got)
	}
	if f.sink.closed == 0 {
		t.Fatal("dialogue not closed")
	}
	f.talk(lib)
	hunting, _ := mustDialogue(t, f.d, "librarian").Node("hunting")
	if got := f.last(); strings.Join(got.Lines, "|") != strings.Join(hunting.Text, "|") {
		t.Fatalf("step 1 node = %+v", got)
	}
	f.reply(0)

	// The crow's book arrives; the last talk hands it in.
	book := f.item("overdue_book")
	if _, err := f.p.Inv.Add(f.d, book, 1); err != nil {
		t.Fatal(err)
	}
	if res := f.eng.GotItem(f.sys.State(f.p.ID), book, env{f.p, f.d}); !res.Advanced {
		t.Fatal("quest item step did not advance")
	}
	goldBefore := f.p.Inv.Gold
	xpBefore := f.p.XP
	f.talk(lib)
	ret, _ := mustDialogue(t, f.d, "librarian").Node("return")
	if got := f.last(); strings.Join(got.Lines, "|") != strings.Join(ret.Text, "|") {
		t.Fatalf("step 2 node = %+v", got)
	}
	f.reply(0)
	if len(f.cm.reqs) != 1 {
		t.Fatalf("commit requests = %d", len(f.cm.reqs))
	}
	f.sys.Resolve(f.cm.reqs[0].ID, true)
	f.w.Tick()

	if len(f.sink.completed) != 1 {
		t.Fatalf("completions = %d", len(f.sink.completed))
	}
	c := f.sink.completed[0]
	if c.Quest != q.ID || c.Name != "Overdue" || c.Gold != q.Rewards.Gold || c.QuestPoints != q.Rewards.QuestPoints ||
		len(c.Items) != 1 || c.Items[0].Item != f.item("library_card") {
		t.Fatalf("completion = %+v", c)
	}
	if tk := f.sink.taken[0]; len(tk) != 1 || tk[0].Item != book {
		t.Fatalf("taken = %+v, want the Overdue book", tk)
	}
	if f.p.Inv.Count(book) != 0 {
		t.Fatal("Overdue book still carried")
	}
	if f.p.Inv.Count(f.item("library_card")) != 1 || f.p.Inv.Gold != goldBefore+q.Rewards.Gold || f.p.XP <= xpBefore {
		t.Fatalf("rewards not given: gold %d xp %d", f.p.Inv.Gold, f.p.XP)
	}
	if got := f.sys.State(f.p.ID).Get(q.ID); got.Status != quest.Complete {
		t.Fatalf("final state %+v", got)
	}
	var reasons []string
	for _, e := range f.p.PendingEconomy() {
		reasons = append(reasons, e.Reason)
	}
	if strings.Join(reasons, ",") != ReasonHandIn+","+ReasonReward {
		t.Fatalf("economy events = %v", reasons)
	}
	f.talk(lib)
	done, _ := mustDialogue(t, f.d, "librarian").Node("complete")
	if got := f.last(); strings.Join(got.Lines, "|") != strings.Join(done.Text, "|") {
		t.Fatalf("complete node = %+v", got)
	}
}

// atLastStep starts quest key and puts the player on its last step.
func (f *fixture) atLastStep(key string, npc string) *world.NPC {
	f.t.Helper()
	q, _ := f.d.QuestByKey(key)
	n := f.npcOf(npc)
	f.p.Pos = n.Pos
	st := f.sys.State(f.p.ID)
	if _, err := f.eng.Start(st, q.ID, env{f.p, f.d}); err != nil {
		f.t.Fatal(err)
	}
	for st.Get(q.ID).Step < len(q.Steps)-1 {
		switch s := q.Steps[st.Get(q.ID).Step]; s.Kind {
		case gamedata.StepTalk:
			f.eng.Talk(st, s.NPC, env{f.p, f.d})
		case gamedata.StepQuestItem:
			f.eng.GotItem(st, s.Item, env{f.p, f.d})
		default:
			f.t.Fatalf("unsupported step %v", s.Kind)
		}
	}
	return n
}

func (f *fixture) finishOverdue() {
	f.t.Helper()
	lib := f.atLastStep("overdue", "librarian")
	if _, err := f.p.Inv.Add(f.d, f.item("overdue_book"), 1); err != nil {
		f.t.Fatal(err)
	}
	f.talk(lib)
	f.reply(0)
}

func TestCompletionInventoryFull(t *testing.T) {
	f := setup(t)
	pad := f.item("overdue_book")
	q, _ := f.d.QuestByKey("overdue")
	lib := f.atLastStep("overdue", "librarian")
	if _, err := f.p.Inv.Add(f.d, pad, 1); err != nil {
		t.Fatal(err)
	}
	// Every slot taken, and no room in the gold balance either: the reward
	// cannot be given. (The book's own slot frees up when it is handed in.)
	for _, it := range f.d.Items {
		if it.ID == pad || it.Stackable() {
			continue
		}
		for {
			if _, err := f.p.Inv.Add(f.d, it.ID, 1); err != nil {
				break
			}
		}
	}
	f.p.Inv.Gold = math.MaxInt - 10
	f.talk(lib)
	f.reply(0)
	if len(f.cm.reqs) != 0 {
		t.Fatalf("commit sent with a full inventory: %+v", f.cm.reqs)
	}
	if n := len(f.sink.results); n == 0 || f.sink.results[n-1] != InventoryFull {
		t.Fatalf("results = %v", f.sink.results)
	}
	if got := f.sys.State(f.p.ID).Get(q.ID); got.Status != quest.InProgress || got.Step != 2 {
		t.Fatalf("state %+v", got)
	}
	if f.p.Inv.Count(pad) != 1 || f.p.Inv.Locked() || f.p.Inv.Gold != math.MaxInt-10 {
		t.Fatal("inventory changed or locked")
	}
}

func TestCompletionCommitRequest(t *testing.T) {
	f := setup(t)
	q, _ := f.d.QuestByKey("overdue")
	goldBefore := f.p.Inv.Gold
	f.finishOverdue()
	if len(f.cm.reqs) != 1 {
		t.Fatalf("requests = %d", len(f.cm.reqs))
	}
	r := f.cm.reqs[0]
	if r.Player != f.p.ID || r.Quest != q.ID || r.QuestPoints != 1 || r.Gold != 150 {
		t.Fatalf("request = %+v", r)
	}
	if len(r.XP) != 1 || r.XP[0].Skill != gamedata.CombatSkill || r.XP[0].XP != 500 {
		t.Fatalf("xp = %+v", r.XP)
	}
	if len(r.Items) != 1 || r.Items[0].Item != f.item("library_card") {
		t.Fatalf("items = %+v", r.Items)
	}
	if len(r.Remove) != 1 || r.Remove[0].Item != f.item("overdue_book") {
		t.Fatalf("remove = %+v", r.Remove)
	}
	// Nothing else changes before Resolve.
	if !f.p.Inv.Locked() || f.p.Inv.Gold != goldBefore || f.p.Inv.Count(f.item("overdue_book")) != 1 ||
		f.p.Inv.Count(f.item("library_card")) != 0 || len(f.sink.completed) != 0 {
		t.Fatal("state changed before Resolve")
	}
	if got := f.sys.State(f.p.ID).Get(q.ID); got.Status != quest.InProgress || got.Step != 2 {
		t.Fatalf("state %+v", got)
	}
}

func TestResolveSuccessAndFailure(t *testing.T) {
	for _, ok := range []bool{true, false} {
		f := setup(t)
		q, _ := f.d.QuestByKey("overdue")
		f.finishOverdue()
		f.sys.Resolve(f.cm.reqs[0].ID, ok)
		f.w.Tick()
		if f.p.Inv.Locked() {
			t.Fatalf("ok=%v: inventory still locked", ok)
		}
		got := f.sys.State(f.p.ID).Get(q.ID)
		if ok {
			if got.Status != quest.Complete || f.p.Inv.Count(f.item("overdue_book")) != 0 ||
				f.p.Inv.Count(f.item("library_card")) != 1 || f.p.Inv.Gold != 150 ||
				len(f.sink.completed) != 1 || f.sink.completed[0].Quest != q.ID {
				t.Fatalf("success not applied: %+v gold %d", got, f.p.Inv.Gold)
			}
		} else if got.Status != quest.InProgress || got.Step != 2 || f.p.Inv.Count(f.item("overdue_book")) != 1 ||
			f.p.Inv.Gold != 0 || f.p.Inv.Count(f.item("library_card")) != 0 || len(f.sink.completed) != 0 {
			t.Fatalf("failure changed things: %+v", got)
		}
	}
}

func TestTalkWhilePendingSendsNoSecondRequest(t *testing.T) {
	f := setup(t)
	f.finishOverdue()
	lib := f.npcOf("librarian")
	f.talk(lib)
	f.reply(0)
	f.talk(lib)
	f.reply(0)
	if len(f.cm.reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(f.cm.reqs))
	}
	// The pending commit stops the talk, not the inventory lock.
	for _, r := range f.sink.results {
		if r == InventoryFull {
			t.Fatalf("results = %v", f.sink.results)
		}
	}
	f.sys.Resolve(f.cm.reqs[0].ID, true)
	f.sys.Resolve(f.cm.reqs[0].ID, true) // repeated id ignored
	f.w.Tick()
	if len(f.sink.completed) != 1 || f.p.Inv.Gold != 150 {
		t.Fatalf("completed %d gold %d", len(f.sink.completed), f.p.Inv.Gold)
	}
}

func TestFishFryHandInRecorded(t *testing.T) {
	f := setup(t)
	f.p.AddSkillXP(world.Fishing, 1_000_000*world.SkillXPScale)
	f.p.AddSkillXP(world.Cooking, 1_000_000*world.SkillXPScale)
	cook := f.atLastStep("fish_fry", "food_truck_cook")
	trout := f.item("trout")
	if _, err := f.p.Inv.Add(f.d, trout, 5); err != nil {
		t.Fatal(err)
	}
	f.talk(cook)
	f.reply(0)
	if len(f.cm.reqs) != 1 {
		t.Fatalf("requests = %d", len(f.cm.reqs))
	}
	r := f.cm.reqs[0]
	if len(r.Take) != 1 || r.Take[0].Item != trout || r.Take[0].Quantity != 5 {
		t.Fatalf("take = %+v", r.Take)
	}
	f.sys.Resolve(r.ID, true)
	f.w.Tick()
	var hand []world.Change
	for _, e := range f.p.PendingEconomy() {
		if e.Reason == ReasonHandIn {
			hand = append(hand, e.Changes...)
		}
	}
	n := 0
	for _, c := range hand {
		if c.Kind == world.ChangeRemove && c.Item == trout {
			n += c.Qty
		}
	}
	if n != 5 {
		t.Fatalf("recorded hand-in changes = %+v", hand)
	}
	if f.p.Inv.Count(trout) != 0 {
		t.Fatal("trout still carried")
	}
}

func BenchmarkTick(b *testing.B) {
	d, err := gamedata.Default()
	if err != nil {
		b.Fatal(err)
	}
	sys, err := New(d, quest.New(d), 1, &sink{}, &committer{})
	if err != nil {
		b.Fatal(err)
	}
	w := world.New(world.DefaultConfig())
	if err := w.Register(sys); err != nil {
		b.Fatal(err)
	}
	for i := 1; i <= 100; i++ {
		if err := w.AddPlayer(&world.Player{ID: world.PlayerID(i), Inv: world.NewInventory(d)}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for range b.N {
		w.Tick()
	}
}

// Progress made in another quest while a completion is being saved (a kill
// counted on the shared state) survives Resolve.
func TestResolveKeepsOtherQuestProgress(t *testing.T) {
	f := setup(t)
	f.p.AddXP(f.d.Rules, 1_000_000)
	ct, _ := f.d.QuestByKey("coyote_trouble")
	st := f.sys.State(f.p.ID)
	if _, err := f.eng.Start(st, ct.ID, env{f.p, f.d}); err != nil {
		t.Fatal(err)
	}
	ranger, _ := f.d.NPCByKey("park_ranger")
	f.eng.Talk(st, ranger.ID, env{f.p, f.d})
	f.finishOverdue()
	if len(f.cm.reqs) != 1 {
		t.Fatalf("requests = %d", len(f.cm.reqs))
	}
	coyote, _ := f.d.NPCByKey("coyote")
	f.eng.Kill(f.sys.State(f.p.ID), coyote.ID, env{f.p, f.d})
	f.sys.Resolve(f.cm.reqs[0].ID, true)
	f.w.Tick()
	q, _ := f.d.QuestByKey("overdue")
	if got := f.sys.State(f.p.ID).Get(q.ID); got.Status != quest.Complete {
		t.Fatalf("overdue %+v", got)
	}
	if got := f.sys.State(f.p.ID).Get(ct.ID); got.Status != quest.InProgress || got.Step != 1 || got.Count != 1 {
		t.Fatalf("coyote trouble %+v, want step 1 count 1", got)
	}
}
