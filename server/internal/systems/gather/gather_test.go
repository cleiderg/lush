package gather_test

import (
	"bytes"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/gather"
	"github.com/cleiderg/lush/server/internal/world"
)

const sharedDir = "../../../../shared/map/"

var t0 = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// scripted is a random source whose answers the test controls and counts.
type scripted struct {
	f      func() float64
	i      func(n int) int
	floats int
}

func (r *scripted) Float64() float64 {
	r.floats++
	if r.f == nil {
		return 0
	}
	return r.f()
}

func (r *scripted) IntN(n int) int {
	if r.i == nil {
		return n - 1 // never falls, always the latest/last choice
	}
	return r.i(n)
}

type rec struct {
	results []gather.Result
	stops   []gather.StopReason
	gains   int
	levels  int
}

func (s *rec) StartResult(_ world.PlayerID, r gather.Result) { s.results = append(s.results, r) }
func (s *rec) Stopped(_ world.PlayerID, w gather.StopReason) { s.stops = append(s.stops, w) }
func (s *rec) Gained(_ world.PlayerID, _ world.Skill, _ int, leveled bool, _ int) {
	s.gains++
	if leveled {
		s.levels++
	}
}

type env struct {
	t     *testing.T
	d     *gamedata.Data
	w     *world.World
	clock *world.FakeClock
	sys   *gather.System
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
	e.sys, err = gather.New(d, l, e.clock, e.rand, e.sink, gather.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.w.Register(e.sys); err != nil {
		t.Fatal(err)
	}
	e.p = &world.Player{ID: 1, Inv: world.NewInventory(d)}
	if err := e.w.AddPlayer(e.p); err != nil {
		t.Fatal(err)
	}
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

func (e *env) give(key string) {
	e.t.Helper()
	if _, err := e.p.Inv.Add(e.d, e.item(key), 1); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) level(s world.Skill, lvl int) {
	e.p.SkillXP[s] = formula.XPForLevel(lvl) * world.SkillXPScale
	if got := e.p.SkillLevel(s); got != lvl {
		e.t.Fatalf("level %d gives skill level %d", lvl, got)
	}
}

// at puts the player on the tile (tx, ty), at its centre.
func (e *env) at(tx, ty float32) { e.p.Pos = world.Vec2{X: tx + 0.5, Y: ty + 0.5} }

// start enqueues a start command, runs one world tick and returns its result.
func (e *env) start(id string) gather.Result {
	e.t.Helper()
	n := len(e.sink.results)
	e.w.Enqueue(gather.StartCommand{Player: e.p.ID, Place: id})
	e.w.Tick()
	if len(e.sink.results) != n+1 {
		e.t.Fatalf("start %q: %d results, want 1", id, len(e.sink.results)-n)
	}
	return e.sink.results[n]
}

func (e *env) ticks(n int) {
	for range n {
		e.w.Tick()
	}
}

// combatTicks runs n combat ticks (12 world ticks each).
func (e *env) combatTicks(n int) { e.ticks(12 * n) }

func (e *env) count(key string) int { return e.p.Inv.Count(e.item(key)) }

// alignToCombatTick runs world ticks until one has just been a combat tick, so
// combatTicks counts whole combat ticks from here.
func (e *env) alignToCombatTick() {
	for e.w.TickCount()%12 != 0 {
		e.w.Tick()
	}
}

// Tree 1 is at tile (32, 6); net-1 is at tile (-83, -18).
const treeID = "tree-1"

func (e *env) nearTree() { e.at(32, 6) }

func TestEmbeddedMapMatchesShared(t *testing.T) {
	shared, err := os.ReadFile(sharedDir + "skills.json")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := os.ReadFile("skills.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embedded, shared) {
		t.Fatal("server/internal/systems/gather/skills.json differs from shared/map/skills.json; " +
			"refresh it from the repo root with: cp shared/map/skills.json server/internal/systems/gather/skills.json")
	}
}

func TestTileSizeMatchesCollisionGrid(t *testing.T) {
	g, err := world.LoadGrid(sharedDir + "collision.bin")
	if err != nil {
		t.Fatal(err)
	}
	if g.CellSize() != gather.TileSize {
		t.Fatalf("collision cell size %v m, TileSize %v m", g.CellSize(), gather.TileSize)
	}
}

func TestStartRange(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		tx, ty float32
		want   gather.Result
	}{
		{"same tile", treeID, 32, 6, gather.Started},
		{"orthogonal", treeID, 33, 6, gather.Started},
		{"diagonal", treeID, 31, 5, gather.Started},
		{"two tiles away", treeID, 34, 6, gather.TooFar},
		{"far diagonal", treeID, 34, 8, gather.TooFar},
		{"unknown id", "tree-99", 32, 6, gather.NoSuchTarget},
		{"empty id", "", 32, 6, gather.NoSuchTarget},
		{"pond itself is no target", "pond", 32, 6, gather.NoSuchTarget},
		{"inactive spot", "net-4", -83, -19, gather.NoSuchTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.give("bronze_hatchet")
			e.give("small_net")
			e.at(tt.tx, tt.ty)
			if got := e.start(tt.id); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			_, gathering := e.sys.Gathering(1)
			if gathering != (tt.want == gather.Started) {
				t.Fatalf("gathering = %v after %v", gathering, tt.want)
			}
		})
	}
}

func TestStartRangeOnSpot(t *testing.T) {
	e := newEnv(t)
	e.give("small_net")
	e.at(-84, -19) // diagonal of net-1's tile (-83, -18)
	if got := e.start("net-1"); got != gather.Started {
		t.Fatalf("got %v", got)
	}
}

func TestStartRequirements(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *env)
		id    string
		want  gather.Result
	}{
		{"no hatchet", func(e *env) {}, treeID, gather.NoTool},
		{"hatchet in inventory", func(e *env) { e.give("bronze_hatchet") }, treeID, gather.Started},
		{"hatchet equipped", func(e *env) { e.p.Inv.Equipment.Weapon = e.item("bronze_hatchet") }, treeID, gather.Started},
		{"a dagger is no hatchet", func(e *env) { e.p.Inv.Equipment.Weapon = e.item("bronze_dagger") }, treeID, gather.NoTool},
		{"oak needs level 15", func(e *env) { e.give("bronze_hatchet") }, "oak-1", gather.LevelTooLow},
		{"oak at level 15", func(e *env) { e.give("bronze_hatchet"); e.level(world.Woodcutting, 15) }, "oak-1", gather.Started},
		{"steel hatchet alone below its level", func(e *env) { e.give("steel_hatchet") }, treeID, gather.LevelTooLow},
		{"steel hatchet at level 15", func(e *env) { e.give("steel_hatchet"); e.level(world.Woodcutting, 15) }, treeID, gather.Started},
		{"no net", func(e *env) {}, "net-1", gather.NoTool},
		{"rod is no net", func(e *env) { e.give("fishing_rod"); e.level(world.Fishing, 5) }, "net-1", gather.NoTool},
		{"net", func(e *env) { e.give("small_net") }, "net-1", gather.Started},
		{"rod spot needs level 5", func(e *env) { e.give("fishing_rod") }, "rod-1", gather.LevelTooLow},
		{"rod spot, no rod", func(e *env) { e.level(world.Fishing, 5) }, "rod-1", gather.NoTool},
		{"rod spot", func(e *env) { e.give("fishing_rod"); e.level(world.Fishing, 5) }, "rod-1", gather.Started},
		{"full inventory", func(e *env) {
			e.give("bronze_hatchet")
			for e.p.Inv.FreeSlots() > 0 {
				e.give("logs")
			}
		}, treeID, gather.InventoryFull},
		{"in combat", func(e *env) { e.give("bronze_hatchet"); e.p.LastCombat = t0 }, treeID, gather.InCombat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			tt.setup(e)
			if tt.id == "net-1" || tt.id == "rod-1" {
				e.at(-83, -18)
				if tt.id == "rod-1" { // rod-1 at (-87.0, -15.9): tile (-87, -16)
					e.at(-87, -16)
				}
			} else if tt.id == "oak-1" {
				e.at(-147, -39)
			} else {
				e.nearTree()
			}
			if got := e.start(tt.id); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStartAfterCombatLockIsAllowed(t *testing.T) {
	e := newEnv(t)
	e.give("bronze_hatchet")
	e.nearTree()
	e.p.LastCombat = t0
	e.clock.Advance(e.d.Rules.LogoutCombatLock() + time.Second)
	if got := e.start(treeID); got != gather.Started {
		t.Fatalf("got %v", got)
	}
}

// TestAttemptEveryFourCombatTicks uses a seeded source and counts its rolls.
func TestAttemptEveryFourCombatTicks(t *testing.T) {
	e := newEnv(t)
	cr := &countingRand{Rand: rand.New(rand.NewPCG(1, 2))}
	e = rebuild(t, e, cr)
	e.give("bronze_hatchet")
	e.nearTree()
	e.alignToCombatTick()
	if got := e.start(treeID); got != gather.Started {
		t.Fatal(got)
	}
	base := cr.floats
	for i := 1; i <= 8; i++ {
		e.combatTicks(1)
		want := i / 4 // rolls so far: one per 4 combat ticks
		if got := cr.floats - base; got != want {
			t.Fatalf("after %d combat ticks: %d rolls, want %d", i, got, want)
		}
	}
}

type countingRand struct {
	*rand.Rand
	floats int
}

func (c *countingRand) Float64() float64 { c.floats++; return c.Rand.Float64() }

// rebuild replaces the env's system with one on a different random source.
func rebuild(t *testing.T, e *env, r formula.Rand) *env {
	t.Helper()
	l, err := gather.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	n := &env{t: t, d: e.d, w: world.New(world.DefaultConfig()), clock: e.clock, rand: e.rand, sink: e.sink}
	n.sys, err = gather.New(e.d, l, e.clock, r, e.sink, gather.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := n.w.Register(n.sys); err != nil {
		t.Fatal(err)
	}
	n.p = &world.Player{ID: 1, Inv: world.NewInventory(e.d)}
	if err := n.w.AddPlayer(n.p); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSuccessAddsItemXPAndOutboxEvent(t *testing.T) {
	e := newEnv(t)
	e.give("bronze_hatchet")
	e.nearTree()
	e.start(treeID)
	e.p.DrainEconomy()
	e.combatTicks(4)
	if got := e.count("logs"); got != 1 {
		t.Fatalf("%d logs, want 1", got)
	}
	if got := e.p.SkillXP[world.Woodcutting]; got != 250 {
		t.Fatalf("skill XP %d tenths, want 250", got)
	}
	ev := e.p.PendingEconomy()
	if len(ev) != 1 || ev[0].Reason != gather.ReasonGather ||
		len(ev[0].Changes) != 1 || ev[0].Changes[0] != (world.Change{Kind: world.ChangeAdd, Item: e.item("logs"), Qty: 1}) {
		t.Fatalf("outbox = %+v", ev)
	}
	if e.p.XP != 0 {
		t.Fatalf("combat XP %d, want 0", e.p.XP)
	}
	if e.sink.gains != 1 {
		t.Fatalf("%d gains told, want 1", e.sink.gains)
	}
}

func TestFailedRollGivesNothing(t *testing.T) {
	e := newEnv(t)
	e.rand.f = func() float64 { return 0.999 }
	e.give("bronze_hatchet")
	e.nearTree()
	e.start(treeID)
	e.p.DrainEconomy()
	e.combatTicks(12)
	if e.count("logs") != 0 || e.p.SkillXP[world.Woodcutting] != 0 || len(e.p.PendingEconomy()) != 0 {
		t.Fatal("a failed roll changed holdings or XP")
	}
}

func TestSuccessChanceUsesLevelAndToolBonus(t *testing.T) {
	// Tree: 0.25 + 0.5*(level-1)/29; steel hatchet adds 0.10.
	tests := []struct {
		name  string
		tool  string
		level int
		roll  float64
		want  int
	}{
		{"level 1 under chance", "bronze_hatchet", 1, 0.24, 1},
		{"level 1 over chance", "bronze_hatchet", 1, 0.26, 0},
		{"level 15 chance 0.4914", "bronze_hatchet", 15, 0.49, 1},
		{"level 15 over", "bronze_hatchet", 15, 0.50, 0},
		{"steel bonus", "steel_hatchet", 15, 0.59, 1},
		{"steel bonus over", "steel_hatchet", 15, 0.60, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.give(tt.tool)
			e.level(world.Woodcutting, tt.level)
			e.nearTree()
			e.rand.f = func() float64 { return tt.roll }
			e.start(treeID)
			e.combatTicks(4)
			if got := e.count("logs"); got != tt.want {
				t.Fatalf("%d logs, want %d", got, tt.want)
			}
		})
	}
}

func TestRodSpotTriesTroutFirst(t *testing.T) {
	tests := []struct {
		name  string
		level int
		rolls []float64 // answers in order; the chance at the level decides each
		want  map[string]int
	}{
		{"below 20: sardine only", 5, []float64{0}, map[string]int{"raw_sardine": 1}},
		{"level 20, trout roll wins", 20, []float64{0}, map[string]int{"raw_trout": 1}},
		{"level 20, trout fails then sardine", 20, []float64{0.99, 0}, map[string]int{"raw_sardine": 1}},
		{"level 20, both fail", 20, []float64{0.99, 0.99}, map[string]int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.give("fishing_rod")
			e.level(world.Fishing, tt.level)
			e.at(-87, -16)
			i := 0
			e.rand.f = func() float64 {
				v := tt.rolls[min(i, len(tt.rolls)-1)]
				i++
				return v
			}
			e.start("rod-1")
			e.combatTicks(4)
			for _, key := range []string{"raw_sardine", "raw_trout"} {
				if got := e.count(key); got != tt.want[key] {
					t.Fatalf("%s = %d, want %d", key, got, tt.want[key])
				}
			}
		})
	}
}

func TestRodXPPerFish(t *testing.T) {
	e := newEnv(t)
	e.give("fishing_rod")
	e.level(world.Fishing, 20)
	base := e.p.SkillXP[world.Fishing]
	e.at(-87, -16)
	e.start("rod-1")
	e.combatTicks(4) // roll 0: trout
	if got := e.p.SkillXP[world.Fishing] - base; got != 500 {
		t.Fatalf("trout XP %d tenths, want 500", got)
	}
}

func TestTreeFallsStumpRegrows(t *testing.T) {
	e := newEnv(t)
	var gotN int
	fall := false
	e.rand.i = func(n int) int {
		gotN = n
		if fall {
			return 0
		}
		return n - 1
	}
	e.give("bronze_hatchet")
	e.nearTree()
	e.start(treeID)

	e.combatTicks(4) // a log, and the tree stays
	if gotN != 8 {
		t.Fatalf("fall roll was 1 in %d, want 1 in 8", gotN)
	}
	if !e.sys.TreeStanding(treeID) {
		t.Fatal("tree fell on a missed fall roll")
	}
	fall = true
	e.combatTicks(4)
	if e.sys.TreeStanding(treeID) {
		t.Fatal("tree should have fallen")
	}
	if e.count("logs") != 2 {
		t.Fatalf("%d logs, want 2", e.count("logs"))
	}
	if _, ok := e.sys.Gathering(1); ok {
		t.Fatal("still gathering at a stump")
	}
	if got := e.sink.stops[len(e.sink.stops)-1]; got != gather.StoppedFelled {
		t.Fatalf("stop reason %v", got)
	}
	if got := e.start(treeID); got != gather.Felled {
		t.Fatalf("chopping a stump: %v", got)
	}
	e.clock.Advance(14 * time.Second)
	if got := e.start(treeID); got != gather.Felled {
		t.Fatalf("chopping a stump at 14 s: %v", got)
	}
	e.clock.Advance(time.Second)
	if !e.sys.TreeStanding(treeID) {
		t.Fatal("tree should stand after its 15 s respawn")
	}
	if got := e.start(treeID); got != gather.Started {
		t.Fatalf("chopping a regrown tree: %v", got)
	}
}

func TestFelledTreeStopsOtherChoppers(t *testing.T) {
	e := newEnv(t)
	other := &world.Player{ID: 2, Inv: world.NewInventory(e.d)}
	other.Pos = world.Vec2{X: 32.5, Y: 6.5}
	if err := e.w.AddPlayer(other); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Inv.Add(e.d, e.item("bronze_hatchet"), 1); err != nil {
		t.Fatal(err)
	}
	e.give("bronze_hatchet")
	e.nearTree()
	e.w.Enqueue(gather.StartCommand{Player: 1, Place: treeID})
	e.w.Enqueue(gather.StartCommand{Player: 2, Place: treeID})
	e.w.Tick()
	e.rand.i = func(int) int { return 0 }
	e.combatTicks(4)
	for _, id := range []world.PlayerID{1, 2} {
		if _, ok := e.sys.Gathering(id); ok {
			t.Fatalf("player %d still chopping a felled tree", id)
		}
	}
}

func TestFishingSpotsMove(t *testing.T) {
	tests := []struct {
		name  string
		delay int // the answer to the move-time roll, in seconds above the minimum
		secs  time.Duration
	}{
		{"shortest wait", 0, 180 * time.Second},
		{"longest wait", 120, 300 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.rand.i = func(n int) int {
				if n == 121 { // 180 to 300 s in whole seconds
					return tt.delay
				}
				return 0
			}
			// The delay was rolled at construction with the default source,
			// so rebuild with this one.
			e = rebuild(t, e, e.rand)
			e.rand.i = func(n int) int {
				if n == 121 {
					return tt.delay
				}
				return 0
			}
			before := e.sys.ActiveSpots(nil)
			if len(before) != 4 {
				t.Fatalf("%d spots out, want 4", len(before))
			}
			e.clock.Advance(tt.secs - time.Second)
			e.w.Tick()
			if got := e.sys.ActiveSpots(nil); !samePlaces(got, before) {
				t.Fatalf("spots moved early: %v", got)
			}
			e.clock.Advance(time.Second)
			e.w.Tick()
			after := e.sys.ActiveSpots(nil)
			if samePlaces(after, before) {
				t.Fatal("spots did not move")
			}
			for i := range before {
				if after[i].Kind != before[i].Kind {
					t.Fatalf("spot %d changed kind: %s to %s", i, before[i].ID, after[i].ID)
				}
				if after[i].ID == before[i].ID {
					t.Fatalf("spot %d stayed at %s", i, after[i].ID)
				}
			}
			seen := map[string]bool{}
			for _, p := range after {
				if seen[p.ID] {
					t.Fatalf("two spots at %s", p.ID)
				}
				seen[p.ID] = true
			}
		})
	}
}

func samePlaces(a, b []gather.Place) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

func TestFishingStopsWhenSpotMoves(t *testing.T) {
	e := newEnv(t)
	e.give("small_net")
	e.at(-83, -18)
	e.start("net-1")
	e.clock.Advance(301 * time.Second)
	e.w.Tick()
	if _, ok := e.sys.Gathering(1); ok {
		t.Fatal("still fishing a spot that moved away")
	}
	if got := e.sink.stops[len(e.sink.stops)-1]; got != gather.StoppedSpotMoved {
		t.Fatalf("stop reason %v", got)
	}
}

func TestGatheringStops(t *testing.T) {
	tests := []struct {
		name string
		do   func(e *env)
		want gather.StopReason
	}{
		{"player moves", func(e *env) { e.p.Pos.X += 1 }, gather.StoppedMoved},
		{"player asks to move", func(e *env) { e.p.Desired, e.p.HasDesired = world.Vec2{X: 40, Y: 6}, true }, gather.StoppedMoved},
		{"combat starts", func(e *env) { e.p.MarkCombat(e.clock.Now()) }, gather.StoppedCombat},
		{"stop command", func(e *env) { e.w.Enqueue(gather.StopCommand{Player: 1}) }, gather.StoppedByPlayer},
		{"hatchet dropped", func(e *env) { e.p.Inv.Remove(e.item("bronze_hatchet"), 1) }, gather.StoppedNoTool},
		{"inventory fills", func(e *env) {
			for e.p.Inv.FreeSlots() > 0 {
				e.give("logs")
			}
		}, gather.StoppedFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.give("bronze_hatchet")
			e.nearTree()
			e.start(treeID)
			e.combatTicks(1)
			if _, ok := e.sys.Gathering(1); !ok {
				t.Fatal("not gathering before the event")
			}
			tt.do(e)
			e.combatTicks(5) // long enough for an attempt to be due
			if _, ok := e.sys.Gathering(1); ok {
				t.Fatal("still gathering")
			}
			if got := e.sink.stops[len(e.sink.stops)-1]; got != tt.want {
				t.Fatalf("stop reason %v, want %v", got, tt.want)
			}
			if e.p.Anim == world.AnimChop || e.p.Anim == world.AnimFish {
				t.Fatalf("anim %d after stopping", e.p.Anim)
			}
		})
	}
}

func TestStopsWhenLastSlotFills(t *testing.T) {
	e := newEnv(t)
	e.give("bronze_hatchet")
	e.nearTree()
	for e.p.Inv.FreeSlots() > 1 {
		e.give("logs")
	}
	e.start(treeID)
	e.combatTicks(4)
	if e.p.Inv.FreeSlots() != 0 {
		t.Fatal("last slot not filled")
	}
	if _, ok := e.sys.Gathering(1); ok {
		t.Fatal("still gathering with a full inventory")
	}
}

func TestAnimOnlyWhileGathering(t *testing.T) {
	if world.AnimChop != 3 || world.AnimFish != 4 {
		t.Fatalf("AnimChop = %d, AnimFish = %d; protocol wants 3 and 4", world.AnimChop, world.AnimFish)
	}
	for _, tt := range []struct {
		name string
		id   string
		tool string
		x, y float32
		anim world.Anim
	}{
		{"chop", treeID, "bronze_hatchet", 32, 6, world.AnimChop},
		{"fish", "net-1", "small_net", -83, -18, world.AnimFish},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.give(tt.tool)
			e.at(tt.x, tt.y)
			if e.p.Anim != world.AnimIdle {
				t.Fatal("not idle before")
			}
			e.start(tt.id)
			for range 30 {
				e.w.Tick()
				if e.p.Anim != tt.anim {
					t.Fatalf("anim %d while gathering, want %d", e.p.Anim, tt.anim)
				}
			}
			e.w.Enqueue(gather.StopCommand{Player: 1})
			e.w.Tick()
			if e.p.Anim != world.AnimIdle {
				t.Fatalf("anim %d after stopping", e.p.Anim)
			}
		})
	}
}

func TestParseLayoutRejects(t *testing.T) {
	for name, in := range map[string]string{
		"bad json":      `{`,
		"version":       `{"version":2,"units":"decimetres"}`,
		"units":         `{"version":1,"units":"metres"}`,
		"missing z":     `{"version":1,"units":"decimetres","trees":[{"id":"a","kind":"tree","x":1}]}`,
		"duplicate ids": `{"version":1,"units":"decimetres","trees":[{"id":"a","kind":"tree","x":1,"z":1},{"id":"a","kind":"tree","x":2,"z":2}]}`,
	} {
		if _, err := gather.ParseLayout([]byte(in)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func BenchmarkTick1000Gatherers(b *testing.B) {
	d, err := gamedata.Default()
	if err != nil {
		b.Fatal(err)
	}
	l, _ := gather.DefaultLayout()
	w := world.New(world.DefaultConfig())
	sys, err := gather.New(d, l, world.NewFakeClock(t0), rand.New(rand.NewPCG(1, 2)), nil, gather.DefaultConfig())
	if err != nil {
		b.Fatal(err)
	}
	_ = w.Register(sys)
	axe, _ := d.ItemByKey("bronze_hatchet")
	for i := 1; i <= 1000; i++ {
		p := &world.Player{ID: world.PlayerID(i), Pos: world.Vec2{X: 32.5, Y: 6.5}, Inv: world.NewInventory(d)}
		_, _ = p.Inv.Add(d, axe.ID, 1)
		_ = w.AddPlayer(p)
		w.Enqueue(gather.StartCommand{Player: p.ID, Place: "tree-1"})
	}
	w.Tick()
	b.ResetTimer()
	for b.Loop() {
		w.Tick()
		for _, p := range w.Players() {
			p.DrainEconomy()
			p.Inv = world.NewInventory(d)
			_, _ = p.Inv.Add(d, axe.ID, 1)
		}
	}
}
