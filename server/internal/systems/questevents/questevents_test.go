package questevents_test

import (
	"bytes"
	"math/rand/v2"
	"os"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat"
	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/grounditems"
	"github.com/cleiderg/lush/server/internal/systems/quest"
	"github.com/cleiderg/lush/server/internal/systems/questevents"
	"github.com/cleiderg/lush/server/internal/world"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// sink records the kills the wrapped sink passes on.
type sink struct{ kills []combat.Kill }

func (s *sink) Hit(combat.Hit)          {}
func (s *sink) PlayerDied(combat.Death) {}
func (s *sink) NPCKilled(k combat.Kill) { s.kills = append(s.kills, k) }

type fixture struct {
	t     *testing.T
	d     *gamedata.Data
	w     *world.World
	qe    *questevents.System
	inner *sink
	sink  *questevents.Sink
}

func newFixture(t *testing.T, d *gamedata.Data, seed uint64) *fixture {
	t.Helper()
	w := world.New(world.DefaultConfig())
	qe := questevents.New(d, w, rand.New(rand.NewPCG(seed, seed+1)))
	if err := w.Register(qe); err != nil {
		t.Fatal(err)
	}
	inner := &sink{}
	return &fixture{t, d, w, qe, inner, qe.Sink(inner)}
}

func defaultData(t *testing.T) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *fixture) player(id world.PlayerID, recs ...quest.Record) *world.Player {
	f.t.Helper()
	p := &world.Player{ID: id, Inv: world.NewInventory(f.d)}
	if err := f.w.AddPlayer(p); err != nil {
		f.t.Fatal(err)
	}
	if err := f.qe.Load(p, recs); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) quest(key string) gamedata.QuestID {
	q, ok := f.d.QuestByKey(key)
	if !ok {
		f.t.Fatalf("no quest %q", key)
	}
	return q.ID
}

func (f *fixture) npc(key string) world.NPCTypeID {
	n, ok := f.d.NPCByKey(key)
	if !ok {
		f.t.Fatalf("no npc %q", key)
	}
	return world.NPCTypeID(n.ID)
}

func (f *fixture) item(key string) gamedata.ItemID {
	it, ok := f.d.ItemByKey(key)
	if !ok {
		f.t.Fatalf("no item %q", key)
	}
	return it.ID
}

func (f *fixture) kill(killer world.PlayerID, npc string) combat.Kill {
	f.sink.NPCKilled(combat.Kill{NPC: 1, Type: f.npc(npc), Killer: killer})
	return f.inner.kills[len(f.inner.kills)-1]
}

func TestKillEvents(t *testing.T) {
	d := defaultData(t)
	f := newFixture(t, d, 1)
	ct := f.quest("coyote_trouble")
	p := f.player(1, quest.Record{Quest: ct, Status: quest.InProgress, Step: 1})

	for i := 0; i < 4; i++ {
		f.kill(1, "coyote")
	}
	f.kill(1, "crow")
	f.kill(1, "rabbit")
	if got := f.qe.Progress(p.ID, ct); got != (quest.Progress{Status: quest.InProgress, Step: 1, Count: 4}) {
		t.Fatalf("after 4 coyotes and other kills: %+v", got)
	}
	f.kill(1, "coyote")
	if got := f.qe.Progress(p.ID, ct); got != (quest.Progress{Status: quest.InProgress, Step: 2}) {
		t.Fatalf("after 5 coyotes: %+v", got)
	}
}

func TestKillCreditsOnlyKiller(t *testing.T) {
	d := defaultData(t)
	f := newFixture(t, d, 1)
	ct := f.quest("coyote_trouble")
	rec := quest.Record{Quest: ct, Status: quest.InProgress, Step: 1}
	f.player(1, rec)
	f.player(2, rec)
	f.kill(1, "coyote")
	if got := f.qe.Progress(2, ct).Count; got != 0 {
		t.Fatalf("bystander count = %d", got)
	}
	if got := f.qe.Progress(1, ct).Count; got != 1 {
		t.Fatalf("killer count = %d", got)
	}
}

func TestLoadedStateCounts(t *testing.T) {
	d := defaultData(t)
	f := newFixture(t, d, 1)
	ct := f.quest("coyote_trouble")
	f.player(1, quest.Record{Quest: ct, Status: quest.InProgress, Step: 1, Count: 3})
	f.kill(1, "coyote")
	if got := f.qe.Progress(1, ct); got.Step != 1 || got.Count != 4 {
		t.Fatalf("after 1: %+v", got)
	}
	f.kill(1, "coyote")
	if got := f.qe.Progress(1, ct); got.Step != 2 {
		t.Fatalf("after 2: %+v", got)
	}
}

func TestLoadRejectsBadRecords(t *testing.T) {
	f := newFixture(t, defaultData(t), 1)
	p := &world.Player{ID: 1, Inv: world.NewInventory(f.d)}
	if err := f.qe.Load(p, []quest.Record{{Quest: 999, Status: quest.InProgress}}); err == nil {
		t.Fatal("want error")
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

func TestLevelEvents(t *testing.T) {
	d := loadWithQuest(t, `{"id": 9, "key": "grind", "name": "Grind", "description": "Level up.",
	  "requirements": {}, "steps": [
	    {"kind": "reach_level", "skill": "fishing", "level": 10},
	    {"kind": "reach_level", "skill": "combat", "level": 8}],
	  "rewards": {"quest_points": 3}}`)
	f := newFixture(t, d, 1)
	p := f.player(1, quest.Record{Quest: 9, Status: quest.InProgress})

	// Cooking levels, and fishing just short of 10, change nothing.
	p.SkillXP[world.Cooking] = formula.XPForLevel(20) * world.SkillXPScale
	p.SkillXP[world.Fishing] = (formula.XPForLevel(10)*world.SkillXPScale - 1)
	f.w.Tick()
	if got := f.qe.Progress(1, 9); got.Step != 0 || got.Status != quest.InProgress {
		t.Fatalf("before level 10: %+v", got)
	}
	if got := f.qe.Drain(); len(got) != 0 {
		t.Fatalf("changes before level 10: %+v", got)
	}

	p.AddSkillXP(world.Fishing, 1)
	f.w.Tick()
	if got := f.qe.Progress(1, 9); got.Step != 1 {
		t.Fatalf("at fishing 10: %+v", got)
	}

	p.AddXP(d.Rules, formula.XPForLevel(60)*100)
	if p.CombatLevel(d.Rules) < 8 {
		t.Fatal("test setup: combat level too low")
	}
	f.w.Tick()
	if got := f.qe.Progress(1, 9); got.Status != quest.Complete {
		t.Fatalf("at combat 8: %+v", got)
	}
	ch := f.qe.Drain()
	if len(ch) != 1 || len(ch[0].Completed) != 1 || ch[0].Completed[0].Quest != 9 {
		t.Fatalf("drain = %+v", ch)
	}
}

func TestQuestDrops(t *testing.T) {
	d := defaultData(t)
	book, fang := "overdue_book", "alphas_fang"
	overdue := func(f *fixture) quest.Record {
		return quest.Record{Quest: f.quest("overdue"), Status: quest.InProgress, Step: 1}
	}

	// Count the Overdue book over many seeded kills: it drops only on the
	// step, at 1 in 4 (never the other way round).
	t.Run("book only on its step", func(t *testing.T) {
		f := newFixture(t, d, 7)
		off := f.player(1) // not on the quest
		on := f.player(2, overdue(f))
		_ = off
		_ = on
		var offDrops, onDrops int
		for i := 0; i < 200; i++ {
			for _, st := range f.kill(1, "crow").Items {
				if st.Item == f.item(book) {
					offDrops++
				}
			}
			k := f.kill(2, "crow")
			for _, st := range k.Items {
				if st.Item == f.item(book) {
					onDrops++
				}
			}
			// The book is held after a drop; the player drops again only
			// once it is gone.
			if f.qe.Progress(2, f.quest("overdue")).Step != 1 {
				t.Fatal("kill moved the quest")
			}
		}
		if offDrops != 0 {
			t.Fatalf("book dropped %d times off the quest", offDrops)
		}
		if onDrops < 20 || onDrops > 90 { // 200 × 1/4 = 50
			t.Fatalf("book dropped %d times in 200 kills, want about 50", onDrops)
		}
	})

	t.Run("fang always from the alpha on its step", func(t *testing.T) {
		f := newFixture(t, d, 3)
		ct := f.quest("coyote_trouble")
		f.player(1, quest.Record{Quest: ct, Status: quest.InProgress, Step: 2})
		f.player(2, quest.Record{Quest: ct, Status: quest.InProgress, Step: 1})
		f.player(3)
		for i := 0; i < 20; i++ {
			if !hasItem(f.kill(1, "coyote_alpha").Items, f.item(fang)) {
				t.Fatal("fang did not drop on its step")
			}
			if hasItem(f.kill(2, "coyote_alpha").Items, f.item(fang)) || hasItem(f.kill(3, "coyote_alpha").Items, f.item(fang)) {
				t.Fatal("fang dropped off its step")
			}
		}
	})
}

func hasItem(st []world.Stack, id gamedata.ItemID) bool {
	for _, s := range st {
		if s.Item == id {
			return true
		}
	}
	return false
}

func TestPickUpGetItem(t *testing.T) {
	d := defaultData(t)
	f := newFixture(t, d, 1)
	ground := grounditems.New(d, world.NewFakeClock(t0), 1)
	ground.SetPickupHook(f.qe.PickedUp)
	od := f.quest("overdue")
	p := f.player(1, quest.Record{Quest: od, Status: quest.InProgress, Step: 1})

	// Another item changes nothing.
	other, err := ground.Spawn(f.item("alphas_fang"), 1, p.Pos, 1, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ground.PickUp(p, other); err != nil {
		t.Fatal(err)
	}
	if got := f.qe.Progress(1, od).Step; got != 1 {
		t.Fatalf("step after other item = %d", got)
	}

	id, err := ground.Spawn(f.item("overdue_book"), 1, p.Pos, 1, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ground.PickUp(p, id); err != nil {
		t.Fatal(err)
	}
	if got := f.qe.Progress(1, od).Step; got != 2 {
		t.Fatalf("step after book = %d, want 2", got)
	}
}

func TestDrainReportsOnce(t *testing.T) {
	d := defaultData(t)
	f := newFixture(t, d, 1)
	ct := f.quest("coyote_trouble")
	rec := quest.Record{Quest: ct, Status: quest.InProgress, Step: 1}
	f.player(1, rec)
	f.player(2, rec)
	f.player(3)

	f.kill(1, "coyote")
	f.kill(1, "coyote")
	f.kill(3, "rabbit")
	f.w.Tick()
	want := []questevents.Change{{Player: 1, Records: []quest.Record{{Quest: ct, Status: quest.InProgress, Step: 1, Count: 2}}}}
	if got := f.qe.Drain(); !reflect.DeepEqual(got, want) {
		t.Fatalf("drain = %+v, want %+v", got, want)
	}
	if got := f.qe.Drain(); len(got) != 0 {
		t.Fatalf("second drain = %+v", got)
	}
	f.w.Tick()
	if got := f.qe.Drain(); len(got) != 0 {
		t.Fatalf("drain after idle tick = %+v", got)
	}
	f.kill(2, "coyote")
	f.kill(1, "coyote")
	got := f.qe.Drain()
	if len(got) != 2 || got[0].Player != 2 || got[1].Player != 1 {
		t.Fatalf("drain = %+v", got)
	}
}

func BenchmarkTick(b *testing.B) {
	d, err := gamedata.Default()
	if err != nil {
		b.Fatal(err)
	}
	w := world.New(world.DefaultConfig())
	qe := questevents.New(d, w, rand.New(rand.NewPCG(1, 2)))
	for i := 1; i <= 200; i++ {
		p := &world.Player{ID: world.PlayerID(i), Inv: world.NewInventory(d)}
		if err := w.AddPlayer(p); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		qe.Tick(w, 0)
	}
}
