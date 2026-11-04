package talk

import (
	"bytes"
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

type fixture struct {
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
	sys, err := New(d, eng, 1, sk)
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
	return &fixture{t, d, eng, sys, sk, w, p}
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

func BenchmarkTick(b *testing.B) {
	d, err := gamedata.Default()
	if err != nil {
		b.Fatal(err)
	}
	sys, err := New(d, quest.New(d), 1, &sink{})
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
