package cook_test

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/cook"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/gather"
	"github.com/cleiderg/lush/server/internal/world"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// Grill at tile (-32, -96): shared/map/skills.json has it at (-31.5, -95.5) m.
const gx, gy = -32, -96

// scripted is a random source: Float64 answers come from f.
type scripted struct {
	f     func() float64
	calls int
}

func (r *scripted) Float64() float64 {
	r.calls++
	if r.f == nil {
		return 0.99
	}
	return r.f()
}
func (r *scripted) IntN(n int) int { return n - 1 }

type cooked struct {
	item    gamedata.ItemID
	burnt   bool
	xp      int
	leveled bool
	level   int
}

type rec struct {
	results []cook.Result
	levels  []int
	stops   []cook.StopReason
	cooked  []cooked
}

func (s *rec) StartResult(_ world.PlayerID, r cook.Result, level int) {
	s.results = append(s.results, r)
	s.levels = append(s.levels, level)
}
func (s *rec) Stopped(_ world.PlayerID, w cook.StopReason) { s.stops = append(s.stops, w) }
func (s *rec) Cooked(_ world.PlayerID, item gamedata.ItemID, burnt bool, xp int, leveled bool, level int) {
	s.cooked = append(s.cooked, cooked{item, burnt, xp, leveled, level})
}

type env struct {
	t     *testing.T
	d     *gamedata.Data
	w     *world.World
	clock *world.FakeClock
	cook  *cook.System
	gath  *gather.System
	rand  *scripted
	sink  *rec
	p     *world.Player
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	l, err := gather.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, d: d, w: world.New(world.DefaultConfig()), clock: world.NewFakeClock(t0), rand: &scripted{}, sink: &rec{}}
	e.gath, err = gather.New(d, l, e.clock, e.rand, nil, gather.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	e.cook, err = cook.New(d, l, e.clock, e.rand, e.sink, cook.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	e.cook.LinkGather(e.gath)
	for _, s := range []world.System{e.gath, e.cook} {
		if err := e.w.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	e.p = &world.Player{ID: 1, Inv: world.NewInventory(d)}
	if err := e.w.AddPlayer(e.p); err != nil {
		t.Fatal(err)
	}
	e.at(gx, gy)
	return e
}

func (e *env) item(key string) gamedata.ItemID {
	e.t.Helper()
	it, ok := e.d.ItemByKey(key)
	if !ok {
		e.t.Fatalf("no item %q", key)
	}
	return it.ID
}

func (e *env) give(keys ...string) {
	e.t.Helper()
	for _, k := range keys {
		if _, err := e.p.Inv.Add(e.d, e.item(k), 1); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *env) level(lvl int) {
	e.p.SkillXP[world.Cooking] = formula.XPForLevel(lvl) * world.SkillXPScale
}

func (e *env) at(tx, ty float32) { e.p.Pos = world.Vec2{X: tx + 0.5, Y: ty + 0.5} }

func (e *env) start() cook.Result {
	e.t.Helper()
	n := len(e.sink.results)
	e.w.Enqueue(cook.StartCommand{Player: e.p.ID})
	e.w.Tick()
	if len(e.sink.results) != n+1 {
		e.t.Fatalf("%d results, want 1", len(e.sink.results)-n)
	}
	return e.sink.results[n]
}

func (e *env) ticks(n int) {
	for range n {
		e.w.Tick()
	}
}

func (e *env) combatTicks(n int) { e.ticks(12 * n) }

func (e *env) align() {
	for e.w.TickCount()%12 != 0 {
		e.w.Tick()
	}
}

func (e *env) slot(i int) gamedata.ItemID { return e.p.Inv.Slots[i].Item }

func TestStart(t *testing.T) {
	tests := []struct {
		name      string
		tx, ty    float32
		inv       []string
		lvl       int
		combat    bool
		want      cook.Result
		wantLevel int
	}{
		{"same tile", gx, gy, []string{"raw_shrimp"}, 1, false, cook.Started, 0},
		{"orthogonal", gx + 1, gy, []string{"raw_shrimp"}, 1, false, cook.Started, 0},
		{"diagonal", gx - 1, gy + 1, []string{"raw_shrimp"}, 1, false, cook.Started, 0},
		{"two away", gx + 2, gy, []string{"raw_shrimp"}, 1, false, cook.TooFar, 0},
		{"far diagonal", gx + 2, gy + 2, []string{"raw_shrimp"}, 1, false, cook.TooFar, 0},
		{"no fish", gx, gy, []string{"shrimp", "burnt_fish"}, 1, false, cook.NothingToCook, 0},
		{"level too low reports lowest", gx, gy, []string{"raw_trout", "raw_sardine"}, 1 + 0, false, cook.LevelTooLow, 5},
		{"one cookable of two", gx, gy, []string{"raw_trout", "raw_shrimp"}, 1, false, cook.Started, 0},
		{"in combat", gx, gy, []string{"raw_shrimp"}, 1, true, cook.InCombat, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.at(tt.tx, tt.ty)
			e.give(tt.inv...)
			e.level(tt.lvl)
			if tt.combat {
				e.p.LastCombat = t0
			}
			if got := e.start(); got != tt.want {
				t.Fatalf("result %v, want %v", got, tt.want)
			}
			if e.sink.levels[0] != tt.wantLevel {
				t.Fatalf("level %d, want %d", e.sink.levels[0], tt.wantLevel)
			}
			if (e.p.Anim == world.AnimCook) != (tt.want == cook.Started) {
				t.Fatalf("anim %v for result %v", e.p.Anim, tt.want)
			}
		})
	}
}

func TestAttemptsAndOutbox(t *testing.T) {
	e := newEnv(t)
	e.give("raw_trout", "shrimp", "raw_shrimp", "raw_sardine", "raw_shrimp")
	e.level(5) // trout (15) is skipped
	// Burn the first attempt only.
	rolls := 0
	e.rand.f = func() float64 { rolls++; return map[int]float64{1: 0.0}[rolls] + 0.99*b2f(rolls != 1) }
	e.align()
	e.start()
	for n := 1; n <= 3; n++ {
		e.combatTicks(3)
		if len(e.sink.cooked) != n-1 {
			t.Fatalf("attempt before the 4th combat tick (round %d): %d", n, len(e.sink.cooked))
		}
		e.combatTicks(1)
		if len(e.sink.cooked) != n {
			t.Fatalf("round %d: %d attempts, want %d", n, len(e.sink.cooked), n)
		}
	}
	want := []string{"burnt_fish", "sardine", "shrimp"}
	for i, k := range []int{2, 3, 4} {
		if e.slot(k) != e.item(want[i]) {
			t.Errorf("slot %d holds %d, want %s", k, e.slot(k), want[i])
		}
	}
	if e.slot(0) != e.item("raw_trout") || e.slot(1) != e.item("shrimp") {
		t.Error("skipped or cooked slots changed")
	}
	b, s1, s2 := e.sink.cooked[0], e.sink.cooked[1], e.sink.cooked[2]
	if !b.burnt || b.xp != 0 || b.item != e.item("burnt_fish") {
		t.Errorf("burnt: %+v", b)
	}
	if s1.burnt || s1.xp != 400 || s2.xp != 300 {
		t.Errorf("xp: %+v %+v", s1, s2)
	}
	if got := e.p.SkillXP[world.Cooking]; got != formula.XPForLevel(5)*10+700 {
		t.Errorf("skill xp %d", got)
	}
	ev := e.p.PendingEconomy()
	if len(ev) != 3 {
		t.Fatalf("%d outbox events, want 3", len(ev))
	}
	for _, x := range ev {
		if x.Reason != cook.ReasonCook || len(x.Changes) != 2 {
			t.Errorf("event %+v", x)
		}
	}
	if last := e.sink.stops; len(last) != 1 || last[0] != cook.StoppedNoFish {
		t.Errorf("stops %v", last)
	}
	if e.p.Anim != world.AnimIdle {
		t.Errorf("anim %v after stop", e.p.Anim)
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func TestLevelUp(t *testing.T) {
	e := newEnv(t)
	e.give("raw_shrimp")
	e.p.SkillXP[world.Cooking] = formula.XPForLevel(2)*10 - 5
	e.start()
	e.combatTicks(4)
	if len(e.sink.cooked) != 1 || !e.sink.cooked[0].leveled || e.sink.cooked[0].level != 2 {
		t.Fatalf("%+v", e.sink.cooked)
	}
}

func TestBurnChanceLine(t *testing.T) {
	tests := []struct {
		level int
		want  float64
	}{{1, 0.5}, {8, 0.25}, {15, 0}, {16, 0}, {30, 0}}
	for _, tt := range tests {
		// raw shrimp: 50% at level 1, stops burning at 15.
		got := world.BurnChance(0.5, 1, 15, tt.level)
		if d := got - tt.want; d > 1e-9 || d < -1e-9 {
			t.Errorf("level %d: %v, want %v", tt.level, got, tt.want)
		}
	}
	if world.BurnChance(0.5, 15, 30, 15) != 0.5 || world.BurnChance(0.5, 15, 30, 22) <= 0 {
		t.Error("trout line")
	}
}

func TestBurnRollUsesChance(t *testing.T) {
	// Level 8 shrimp burn chance is 0.25: a roll of 0.24 burns, 0.26 cooks.
	for _, tt := range []struct {
		roll  float64
		burnt bool
	}{{0.24, true}, {0.26, false}} {
		e := newEnv(t)
		e.give("raw_shrimp")
		e.level(8)
		e.rand.f = func() float64 { return tt.roll }
		e.start()
		e.combatTicks(4)
		if len(e.sink.cooked) != 1 || e.sink.cooked[0].burnt != tt.burnt {
			t.Fatalf("roll %v: %+v", tt.roll, e.sink.cooked)
		}
	}
}

func TestStops(t *testing.T) {
	t.Run("moved", func(t *testing.T) {
		e := newEnv(t)
		e.give("raw_shrimp", "raw_shrimp")
		e.start()
		e.p.Pos.X += 0.5
		e.w.Tick()
		if len(e.sink.stops) != 1 || e.sink.stops[0] != cook.StoppedMoved || e.p.Anim == world.AnimCook {
			t.Fatalf("%v anim %v", e.sink.stops, e.p.Anim)
		}
		e.combatTicks(8)
		if len(e.sink.cooked) != 0 {
			t.Fatal("cooked after moving")
		}
	})
	t.Run("combat", func(t *testing.T) {
		e := newEnv(t)
		e.give("raw_shrimp")
		e.start()
		e.p.LastCombat = t0
		e.w.Tick()
		if len(e.sink.stops) != 1 || e.sink.stops[0] != cook.StoppedCombat || e.p.Anim == world.AnimCook {
			t.Fatalf("%v", e.sink.stops)
		}
	})
	t.Run("fish dropped", func(t *testing.T) {
		e := newEnv(t)
		e.give("raw_shrimp")
		e.start()
		e.p.Inv.RemoveAt(0)
		e.combatTicks(4)
		if len(e.sink.stops) != 1 || e.sink.stops[0] != cook.StoppedNoFish {
			t.Fatalf("%v", e.sink.stops)
		}
	})
	t.Run("stop command", func(t *testing.T) {
		e := newEnv(t)
		e.give("raw_shrimp")
		e.start()
		e.w.Enqueue(cook.StopCommand{Player: 1})
		e.w.Tick()
		if len(e.sink.stops) != 1 || e.sink.stops[0] != cook.StoppedByPlayer || e.cook.Cooking(1) {
			t.Fatalf("%v", e.sink.stops)
		}
	})
}

func TestGatherSwitch(t *testing.T) {
	e := newEnv(t)
	e.give("raw_shrimp", "small_net")
	// Gathering -> cooking: net-1 is at tile (-83, -18), far from the grill, so
	// move the player there, start gathering, then back to the grill.
	e.at(-83, -18)
	e.w.Enqueue(gather.StartCommand{Player: 1, Place: "net-1"})
	e.w.Tick()
	if _, ok := e.gath.Gathering(1); !ok || e.p.Anim != world.AnimFish {
		t.Fatal("not fishing")
	}
	e.at(gx, gy)
	e.p.Pos = world.Vec2{X: e.p.Pos.X, Y: e.p.Pos.Y}
	// Cooking stops gathering.
	if got := e.start(); got != cook.Started {
		t.Fatal(got)
	}
	if _, ok := e.gath.Gathering(1); ok {
		t.Fatal("still gathering after cooking started")
	}
	if e.p.Anim != world.AnimCook {
		t.Fatalf("anim %v", e.p.Anim)
	}
	// Gathering stops cooking.
	e.at(-83, -18)
	e.w.Enqueue(gather.StartCommand{Player: 1, Place: "net-1"})
	e.w.Tick()
	if e.cook.Cooking(1) {
		t.Fatal("still cooking after gathering started")
	}
	if e.sink.stops[len(e.sink.stops)-1] != cook.StoppedGather {
		t.Fatalf("%v", e.sink.stops)
	}
	if e.p.Anim != world.AnimFish {
		t.Fatalf("anim %v", e.p.Anim)
	}
}

func TestAnimCookMatchesProtocol(t *testing.T) {
	if world.AnimCook != 5 {
		t.Fatalf("AnimCook = %d, want 5", world.AnimCook)
	}
}

func BenchmarkTick(b *testing.B) {
	d, _ := gamedata.Default()
	l, _ := gather.DefaultLayout()
	w := world.New(world.DefaultConfig())
	s, _ := cook.New(d, l, world.NewFakeClock(t0), rand.New(rand.NewPCG(1, 2)), nil, cook.DefaultConfig())
	_ = w.Register(s)
	raw, _ := d.ItemByKey("raw_shrimp")
	for i := 1; i <= 1000; i++ {
		p := &world.Player{ID: world.PlayerID(i), Inv: world.NewInventory(d), Pos: world.Vec2{X: -31.5, Y: -95.5}}
		_, _ = p.Inv.Add(d, raw.ID, 1)
		_ = w.AddPlayer(p)
		w.Enqueue(cook.StartCommand{Player: p.ID})
	}
	w.Tick()
	b.ResetTimer()
	for range b.N {
		w.Tick()
	}
}
