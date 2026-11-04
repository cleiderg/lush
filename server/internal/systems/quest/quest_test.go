package quest_test

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"os"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/quest"
)

type env struct {
	levels map[gamedata.Skill]int
	items  map[gamedata.ItemID]int
}

func (e *env) SkillLevel(s gamedata.Skill) int { return e.levels[s] }
func (e *env) ItemCount(i gamedata.ItemID) int { return e.items[i] }
func (e *env) take(list []gamedata.ItemAmount) {
	for _, t := range list {
		e.items[t.Item] -= t.Quantity
	}
}

func newEnv() *env {
	return &env{levels: map[gamedata.Skill]int{}, items: map[gamedata.ItemID]int{}}
}

type fixture struct {
	t *testing.T
	d *gamedata.Data
	e *quest.Engine
}

func setup(t *testing.T) fixture {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return fixture{t, d, quest.New(d)}
}

func (f fixture) q(key string) gamedata.Quest {
	q, ok := f.d.QuestByKey(key)
	if !ok {
		f.t.Fatalf("no quest %q", key)
	}
	return q
}

func (f fixture) npc(key string) gamedata.NPCTypeID {
	for _, n := range f.d.NPCs {
		if n.Key == key {
			return n.ID
		}
	}
	for _, n := range f.d.Talkers {
		if n.Key == key {
			return n.ID
		}
	}
	f.t.Fatalf("no npc %q", key)
	return 0
}

func (f fixture) item(key string) gamedata.ItemID {
	for _, it := range f.d.Items {
		if it.Key == key {
			return it.ID
		}
	}
	f.t.Fatalf("no item %q", key)
	return 0
}

func TestRoundTrip(t *testing.T) {
	f := setup(t)
	var s quest.State
	e := newEnv()
	e.levels[gamedata.CombatSkill] = 20
	// overdue complete, coyote trouble on a kill step with a count, fish fry not started.
	complete := f.q("overdue")
	for _, r := range []quest.Record{
		{Quest: complete.ID, Status: quest.Complete},
		{Quest: f.q("coyote_trouble").ID, Status: quest.InProgress, Step: 1, Count: 3},
	} {
		got, err := f.e.Decode([]quest.Record{r})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Encode(), []quest.Record{r}) {
			t.Errorf("round trip of %+v = %+v", r, got.Encode())
		}
	}
	recs := []quest.Record{
		{Quest: complete.ID, Status: quest.Complete},
		{Quest: f.q("coyote_trouble").ID, Status: quest.InProgress, Step: 1, Count: 3},
	}
	s, err := f.e.Decode(recs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Encode(), recs) {
		t.Errorf("Encode = %+v, want %+v", s.Encode(), recs)
	}
	fish := f.q("fish_fry").ID
	if got := s.Get(fish); got.Status != quest.NotStarted {
		t.Errorf("fish fry = %+v, want not started", got)
	}
	if got := s.Get(f.q("coyote_trouble").ID); got != (quest.Progress{Status: quest.InProgress, Step: 1, Count: 3}) {
		t.Errorf("coyote = %+v", got)
	}
	for _, bad := range [][]quest.Record{
		{{Quest: 999, Status: quest.Complete}},
		{{Quest: complete.ID, Status: quest.InProgress, Step: 9}},
		{{Quest: complete.ID, Status: quest.Complete}, {Quest: complete.ID, Status: quest.Complete}},
		{{Quest: complete.ID, Status: quest.NotStarted}},
	} {
		if _, err := f.e.Decode(bad); err == nil {
			t.Errorf("Decode(%+v) succeeded", bad)
		}
	}
}

func TestStartRequirements(t *testing.T) {
	f := setup(t)
	tests := []struct {
		name   string
		quest  string
		levels map[gamedata.Skill]int
		ok     bool
	}{
		{"overdue open to all", "overdue", nil, true},
		{"coyote at 14", "coyote_trouble", map[gamedata.Skill]int{gamedata.CombatSkill: 14}, false},
		{"coyote at 15", "coyote_trouble", map[gamedata.Skill]int{gamedata.CombatSkill: 15}, true},
		{"fish fry no skills", "fish_fry", nil, false},
		{"fish fry fishing only", "fish_fry", map[gamedata.Skill]int{"fishing": 15, "cooking": 14}, false},
		{"fish fry cooking only", "fish_fry", map[gamedata.Skill]int{"fishing": 14, "cooking": 15}, false},
		{"fish fry both", "fish_fry", map[gamedata.Skill]int{"fishing": 15, "cooking": 15}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s quest.State
			e := newEnv()
			for k, v := range tc.levels {
				e.levels[k] = v
			}
			id := f.q(tc.quest).ID
			_, err := f.e.Start(&s, id, e)
			if tc.ok != (err == nil) {
				t.Fatalf("Start err = %v, want ok=%v", err, tc.ok)
			}
			if !tc.ok && !errors.Is(err, quest.ErrRequirements) {
				t.Errorf("err = %v, want ErrRequirements", err)
			}
			if got := s.Get(id).Status; tc.ok != (got == quest.InProgress) {
				t.Errorf("status = %v", got)
			}
		})
	}
	var s quest.State
	id := f.q("overdue").ID
	if _, err := f.e.Start(&s, id, newEnv()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Start(&s, id, newEnv()); !errors.Is(err, quest.ErrAlreadyBegun) {
		t.Errorf("second start = %v", err)
	}
}

func TestOverdueFromStartToComplete(t *testing.T) {
	f := setup(t)
	var s quest.State
	e := newEnv()
	id := f.q("overdue").ID
	lib, crow, book := f.npc("librarian"), f.npc("crow"), f.item("overdue_book")
	if _, err := f.e.Start(&s, id, e); err != nil {
		t.Fatal(err)
	}
	// Events for other steps change nothing.
	for name, res := range map[string]quest.Result{
		"kill":     f.e.Kill(&s, crow, e),
		"handover": f.e.HandOver(&s, lib, e),
		"item":     f.e.GotItem(&s, book, e),
		"level":    f.e.SkillLevel(&s, "fishing", 99, e),
		"wrongnpc": f.e.Talk(&s, f.npc("park_ranger"), e),
	} {
		if res.Advanced || s.Get(id).Step != 0 {
			t.Errorf("%s moved step 0: %+v", name, res)
		}
	}
	if res := f.e.Talk(&s, lib, e); !res.Advanced || s.Get(id).Step != 1 {
		t.Fatalf("talk: %+v step %d", res, s.Get(id).Step)
	}
	// On the item step, talking and handing over do nothing.
	if f.e.Talk(&s, lib, e).Advanced || f.e.HandOver(&s, lib, e).Advanced || f.e.Kill(&s, crow, e).Advanced {
		t.Error("item step moved on other events")
	}
	e.items[book] = 1
	if res := f.e.GotItem(&s, book, e); !res.Advanced || s.Get(id).Step != 2 {
		t.Fatalf("got item: %+v", res)
	}
	// All or none: handing over with the book gone takes nothing.
	e.items[book] = 0
	if res := f.e.HandOver(&s, lib, e); res.Advanced || len(res.Take) != 0 || s.Get(id).Step != 2 {
		t.Fatalf("handover without book: %+v", res)
	}
	e.items[book] = 1
	res := f.e.FinishTalk(&s, lib, e)
	if !res.Advanced || len(res.Take) != 1 || res.Take[0].Item != book || len(res.Completed) != 1 {
		t.Fatalf("finish: %+v", res)
	}
	if s.Get(id).Status != quest.Complete {
		t.Errorf("status = %v", s.Get(id).Status)
	}
	// Overdue's rewards (AC5).
	c := res.Completed[0]
	if c.QuestPoints != 1 || c.Gold != 150 || c.TotalPoints != 1 ||
		len(c.XP) != 1 || c.XP[0] != (gamedata.SkillXP{Skill: gamedata.CombatSkill, XP: 500}) ||
		len(c.Items) != 1 || c.Items[0].Item != f.item("library_card") || c.Items[0].Quantity != 1 {
		t.Errorf("completion = %+v", c)
	}
	if len(c.Remove) != 0 {
		t.Errorf("remove = %+v, book was already taken", c.Remove)
	}
}

func TestCoyoteTroubleFromStartToComplete(t *testing.T) {
	f := setup(t)
	var s quest.State
	e := newEnv()
	e.levels[gamedata.CombatSkill] = 15
	id := f.q("coyote_trouble").ID
	ranger, coyote, alpha, fang := f.npc("park_ranger"), f.npc("coyote"), f.npc("coyote_alpha"), f.item("alphas_fang")
	if _, err := f.e.Start(&s, id, e); err != nil {
		t.Fatal(err)
	}
	if f.e.Kill(&s, coyote, e).Advanced {
		t.Fatal("kill counted on talk step")
	}
	f.e.Talk(&s, ranger, e)
	for i := 1; i <= 4; i++ {
		f.e.Kill(&s, f.npc("crow"), e) // wrong type
		f.e.Kill(&s, alpha, e)
		f.e.Kill(&s, coyote, e)
		if p := s.Get(id); p.Step != 1 || p.Count != i {
			t.Fatalf("after %d kills: %+v", i, p)
		}
	}
	f.e.Kill(&s, coyote, e)
	if p := s.Get(id); p.Step != 2 || p.Count != 0 {
		t.Fatalf("after 5 kills: %+v", p)
	}
	if f.e.Kill(&s, coyote, e).Advanced {
		t.Error("kill counted on the fang step")
	}
	e.items[fang] = 1
	f.e.GotItem(&s, fang, e)
	if s.Get(id).Step != 3 {
		t.Fatalf("step = %d", s.Get(id).Step)
	}
	res := f.e.FinishTalk(&s, ranger, e)
	if len(res.Completed) != 1 || res.Completed[0].QuestPoints != 2 || res.Completed[0].Gold != 400 {
		t.Fatalf("finish: %+v", res)
	}
}

func TestFishFryFromStartToComplete(t *testing.T) {
	f := setup(t)
	var s quest.State
	e := newEnv()
	e.levels["fishing"], e.levels["cooking"] = 15, 15
	id := f.q("fish_fry").ID
	cook, trout := f.npc("food_truck_cook"), f.item("trout")
	if _, err := f.e.Start(&s, id, e); err != nil {
		t.Fatal(err)
	}
	e.items[trout] = 5
	if f.e.HandOver(&s, cook, e).Advanced {
		t.Fatal("handed over on the talk step")
	}
	f.e.FinishTalk(&s, cook, e) // talk step: the cook is talked to, nothing taken
	if s.Get(id).Step != 1 {
		t.Fatalf("step = %d", s.Get(id).Step)
	}
	e.items[trout] = 4
	if res := f.e.HandOver(&s, cook, e); res.Advanced || len(res.Take) != 0 {
		t.Fatalf("4 trout accepted: %+v", res)
	}
	e.items[trout] = 5
	res := f.e.HandOver(&s, cook, e)
	if len(res.Completed) != 1 || len(res.Take) != 1 || res.Take[0].Quantity != 5 {
		t.Fatalf("handover: %+v", res)
	}
	want := []gamedata.SkillXP{{Skill: "cooking", XP: 1500}, {Skill: "fishing", XP: 1000}}
	if !reflect.DeepEqual(res.Completed[0].XP, want) {
		t.Errorf("xp = %+v", res.Completed[0].XP)
	}
}

func TestTotalPointsAfterAllQuests(t *testing.T) {
	f := setup(t)
	var recs []quest.Record
	for _, q := range f.d.Quests {
		recs = append(recs, quest.Record{Quest: q.ID, Status: quest.Complete})
	}
	s, err := f.e.Decode(recs)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.e.QuestPoints(&s); got != 4 {
		t.Errorf("points = %d, want 4", got)
	}
	// Completing the last quest reports the running total.
	s2, _ := f.e.Decode(recs[:2])
	e := newEnv()
	e.levels["fishing"], e.levels["cooking"] = 15, 15
	id := f.q("fish_fry").ID
	s2.Encode()
	f.e.Start(&s2, id, e)
	f.e.Talk(&s2, f.npc("food_truck_cook"), e)
	e.items[f.item("trout")] = 5
	res := f.e.HandOver(&s2, f.npc("food_truck_cook"), e)
	if len(res.Completed) != 1 || res.Completed[0].TotalPoints != 4 {
		t.Errorf("completed = %+v", res.Completed)
	}
}

func TestOverdueBookDrops(t *testing.T) {
	f := setup(t)
	crow, book := f.npc("crow"), f.item("overdue_book")
	const kills = 10000
	count := func(s *quest.State, e *env, r *rand.Rand) int {
		n := 0
		for i := 0; i < kills; i++ {
			for _, it := range f.e.Drops(s, crow, e, r) {
				if it.Item == book {
					n++
				}
			}
		}
		return n
	}
	r := rand.New(rand.NewPCG(1, 2))
	id := f.q("overdue").ID
	e := newEnv()

	var none quest.State
	if n := count(&none, e, r); n != 0 {
		t.Errorf("not started: %d drops", n)
	}
	var s quest.State
	f.e.Start(&s, id, e)
	if n := count(&s, e, r); n != 0 {
		t.Errorf("on talk step: %d drops", n)
	}
	f.e.Talk(&s, f.npc("librarian"), e)
	n := count(&s, e, r)
	if n < kills*20/100 || n > kills*30/100 {
		t.Errorf("on item step: %d drops in %d kills, want 20-30%%", n, kills)
	}
	// Holding it: no second book.
	e.items[book] = 1
	if n := count(&s, e, r); n != 0 {
		t.Errorf("holding the book: %d drops", n)
	}
	// Lost it while on the bring step: it drops again from the same source.
	f.e.GotItem(&s, book, e)
	e.items[book] = 0
	if n := count(&s, e, r); n < kills*20/100 || n > kills*30/100 {
		t.Errorf("lost book on bring step: %d drops", n)
	}
	// Other sources never drop it.
	if got := f.e.Drops(&s, f.npc("coyote"), e, r); len(got) != 0 {
		t.Errorf("coyote dropped %+v", got)
	}
	// Complete: no more.
	e.items[book] = 1
	f.e.FinishTalk(&s, f.npc("librarian"), e)
	e.items[book] = 0
	if n := count(&s, e, r); n != 0 {
		t.Errorf("after completion: %d drops", n)
	}
}

func TestAlphaFangAlwaysDrops(t *testing.T) {
	f := setup(t)
	alpha, fang := f.npc("coyote_alpha"), f.item("alphas_fang")
	if alpha != 6 {
		t.Fatalf("alpha id = %d, want 6", alpha)
	}
	e := newEnv()
	e.levels[gamedata.CombatSkill] = 15
	var s quest.State
	id := f.q("coyote_trouble").ID
	f.e.Start(&s, id, e)
	r := rand.New(rand.NewPCG(3, 4))
	f.e.Talk(&s, f.npc("park_ranger"), e)
	if got := f.e.Drops(&s, alpha, e, r); len(got) != 0 {
		t.Fatalf("fang dropped on the kill step: %+v", got)
	}
	for i := 0; i < 5; i++ {
		f.e.Kill(&s, f.npc("coyote"), e)
	}
	for i := 0; i < 200; i++ {
		got := f.e.Drops(&s, alpha, e, r)
		if len(got) != 1 || got[0].Item != fang || got[0].Quantity != 1 {
			t.Fatalf("kill %d: drops %+v", i, got)
		}
	}
	e.items[fang] = 1
	if got := f.e.Drops(&s, alpha, e, r); len(got) != 0 {
		t.Errorf("holding fang: %+v", got)
	}
}

func TestCompletionRemovesHeldQuestItems(t *testing.T) {
	f := setup(t)
	var s quest.State
	e := newEnv()
	id := f.q("overdue").ID
	book := f.item("overdue_book")
	f.e.Start(&s, id, e)
	f.e.Talk(&s, f.npc("librarian"), e)
	e.items[book] = 2 // a spare copy besides the one handed over
	res := f.e.GotItem(&s, book, e)
	_ = res
	got := f.e.HandOver(&s, f.npc("librarian"), e)
	if len(got.Completed) != 1 {
		t.Fatalf("%+v", got)
	}
	r := got.Completed[0].Remove
	if len(r) != 1 || r[0].Item != book || r[0].Quantity != 1 {
		t.Errorf("remove = %+v, want the 1 spare book", r)
	}
}

func BenchmarkDrops(b *testing.B) {
	d, err := gamedata.Default()
	if err != nil {
		b.Fatal(err)
	}
	en := quest.New(d)
	var s quest.State
	e := newEnv()
	e.levels[gamedata.CombatSkill] = 20
	for _, q := range d.Quests {
		en.Start(&s, q.ID, e)
	}
	r := rand.New(rand.NewPCG(1, 2))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		en.Drops(&s, 3, e, r)
	}
}

// loadWithQuest loads the starter data plus one extra quest, as JSON.
func loadWithQuest(t *testing.T, extra string) *gamedata.Data {
	t.Helper()
	fsys := fstest.MapFS{}
	ents, err := os.ReadDir("../gamedata/data")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		b, err := os.ReadFile("../gamedata/data/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == gamedata.QuestsFile {
			b = append(bytes.TrimRight(b, " \n]"), []byte(","+extra+"]")...)
		}
		fsys[e.Name()] = &fstest.MapFile{Data: b}
	}
	d, err := gamedata.Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReachLevelStep(t *testing.T) {
	d := loadWithQuest(t, `{"id": 9, "key": "grind", "name": "Grind", "description": "Level up.",
	  "requirements": {}, "steps": [
	    {"kind": "reach_level", "skill": "fishing", "level": 10},
	    {"kind": "reach_level", "skill": "cooking", "level": 5}],
	  "rewards": {"quest_points": 3}}`)
	en := quest.New(d)
	e := newEnv()
	e.levels["cooking"] = 6 // already met, so it passes without an event
	var s quest.State
	if _, err := en.Start(&s, 9, e); err != nil {
		t.Fatal(err)
	}
	if en.SkillLevel(&s, "cooking", 50, e).Advanced || en.SkillLevel(&s, "fishing", 9, e).Advanced {
		t.Fatal("wrong skill or too low advanced")
	}
	e.levels["fishing"] = 10
	res := en.SkillLevel(&s, "fishing", 10, e)
	if len(res.Completed) != 1 || res.Completed[0].TotalPoints != 3 || s.Get(9).Status != quest.Complete {
		t.Fatalf("res = %+v state %+v", res, s.Get(9))
	}
}
