package spawner_test

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/spawner"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

const tick = 50 * time.Millisecond

type fixture struct {
	d     *gamedata.Data
	areas *areas.Lookup
	grid  *world.Grid
	clock *world.FakeClock
	w     *world.World
	s     *spawner.System
}

// newFixture builds a spawner over the shipped data, areas and collision grid.
func newFixture(tb testing.TB) *fixture {
	tb.Helper()
	d, err := gamedata.Default()
	if err != nil {
		tb.Fatal(err)
	}
	l, err := areas.Load()
	if err != nil {
		tb.Fatal(err)
	}
	g, err := world.LoadGrid("../../../../shared/map/collision.bin")
	if err != nil {
		tb.Fatal(err)
	}
	f := &fixture{d: d, areas: l, grid: g, clock: world.NewFakeClock(time.Unix(1_700_000_000, 0)), w: world.New(world.DefaultConfig())}
	f.s, err = spawner.New(spawner.Config{Data: d, Areas: l, Collision: g, Clock: f.clock, Rand: rand.New(rand.NewPCG(1, 2))})
	if err != nil {
		tb.Fatal(err)
	}
	return f
}

func (f *fixture) tick() { f.s.Tick(f.w, tick) }

// advance moves the clock forward by d and runs one spawner tick.
func (f *fixture) advance(d time.Duration) {
	f.clock.Advance(d)
	f.tick()
}

func (f *fixture) npcType(tb testing.TB, key string) gamedata.NPCType {
	tb.Helper()
	t, ok := f.d.NPCByKey(key)
	if !ok {
		tb.Fatalf("no NPC type %q", key)
	}
	return t
}

func (f *fixture) ofType(t gamedata.NPCType) []*world.NPC {
	var out []*world.NPC
	for _, n := range f.w.NPCs() {
		if n.Type == world.NPCTypeID(t.ID) {
			out = append(out, n)
		}
	}
	return out
}

var wantArea = map[gamedata.Area]areas.Area{
	gamedata.AreaAldrichPark: areas.AldrichPark,
	gamedata.AreaSouthFields: areas.SouthFields,
}

// checkPlaced fails unless n stands on a walkable cell in t's area, outside
// the safe circle, at home and with full HP.
func (f *fixture) checkPlaced(tb testing.TB, t gamedata.NPCType, n *world.NPC) {
	tb.Helper()
	if got := f.areas.AreaAt(n.Pos); got != wantArea[t.Area] {
		tb.Errorf("%s %d at %+v is in area %s, want %s", t.Key, n.ID, n.Pos, got, wantArea[t.Area])
	}
	if f.areas.InSafeCircle(n.Pos) {
		tb.Errorf("%s %d at %+v is in the safe circle", t.Key, n.ID, n.Pos)
	}
	if f.grid.Blocked(n.Pos) {
		tb.Errorf("%s %d at %+v is on a blocked cell", t.Key, n.ID, n.Pos)
	}
	if n.Home != n.Pos || n.HP != t.HP {
		tb.Errorf("%s %d: home %+v hp %d, want home %+v hp %d", t.Key, n.ID, n.Home, n.HP, n.Pos, t.HP)
	}
}

// TestInitialPopulation checks the first tick places each type's population,
// every NPC on a walkable cell inside its area and outside the safe circle.
func TestInitialPopulation(t *testing.T) {
	f := newFixture(t)
	f.tick()
	total := 0
	for _, typ := range f.d.NPCs {
		t.Run(typ.Key, func(t *testing.T) {
			got := f.ofType(typ)
			if len(got) != typ.Population {
				t.Fatalf("%d alive, want population %d", len(got), typ.Population)
			}
			for _, n := range got {
				f.checkPlaced(t, typ, n)
			}
		})
		total += typ.Population
	}
	if len(f.w.NPCs()) != total {
		t.Errorf("%d NPCs in the world, want %d", len(f.w.NPCs()), total)
	}
	// Later ticks with nobody killed add nothing.
	f.advance(time.Hour)
	if len(f.w.NPCs()) != total {
		t.Errorf("after an hour: %d NPCs, want %d", len(f.w.NPCs()), total)
	}
}

// TestRespawnAfterRespawnTime kills one NPC of each type and checks the
// replacement appears exactly when the type's respawn time has passed (a
// rabbit after 15 s), and not a tick before.
func TestRespawnAfterRespawnTime(t *testing.T) {
	tests := []struct {
		key     string
		respawn time.Duration
	}{
		{"rabbit", 15 * time.Second},
		{"squirrel", 20 * time.Second},
		{"crow", 30 * time.Second},
		{"coyote", 45 * time.Second},
		{"rogue_delivery_robot", 60 * time.Second},
		{"coyote_alpha", 3 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			f := newFixture(t)
			typ := f.npcType(t, tt.key)
			f.tick()
			f.advance(time.Second)
			before := f.ofType(typ)
			victim := before[0]
			f.w.RemoveNPC(victim.ID) // killed by combat
			f.tick()                 // the kill's tick starts the timer

			f.advance(tt.respawn - tick)
			if got := len(f.ofType(typ)); got != typ.Population-1 {
				t.Fatalf("%v after the kill: %d alive, want %d", tt.respawn-tick, got, typ.Population-1)
			}
			f.advance(tick)
			after := f.ofType(typ)
			if len(after) != typ.Population {
				t.Fatalf("%v after the kill: %d alive, want %d", tt.respawn, len(after), typ.Population)
			}
			for _, n := range after {
				f.checkPlaced(t, typ, n)
			}
		})
	}
}

// TestSeveralKillsRespawnInOrder checks each kill gets its own timer.
func TestSeveralKillsRespawnInOrder(t *testing.T) {
	f := newFixture(t)
	rabbit := f.npcType(t, "rabbit")
	f.tick()
	f.w.RemoveNPC(f.ofType(rabbit)[0].ID)
	f.tick()
	f.advance(5 * time.Second)
	f.w.RemoveNPC(f.ofType(rabbit)[0].ID)
	f.tick()
	steps := []struct {
		after time.Duration
		want  int
	}{
		{10*time.Second - tick, 10},
		{tick, 11},
		{5*time.Second - tick, 11},
		{tick, 12},
	}
	for i, st := range steps {
		f.advance(st.after)
		if got := len(f.ofType(rabbit)); got != st.want {
			t.Fatalf("step %d: %d rabbits, want %d", i, got, st.want)
		}
	}
}

// TestMaxAliveCoyoteAlpha kills the coyote alpha whenever it is up for half
// an hour and checks at most one is ever alive, and that the spawner does
// not add one while another coyote alpha is alive.
func TestMaxAliveCoyoteAlpha(t *testing.T) {
	f := newFixture(t)
	alpha := f.npcType(t, "coyote_alpha")
	if alpha.MaxAlive != 1 {
		t.Fatalf("coyote alpha max_alive = %d, want 1", alpha.MaxAlive)
	}
	f.tick()
	spawns := 0
	for elapsed := time.Duration(0); elapsed < 30*time.Minute; elapsed += time.Second {
		up := f.ofType(alpha)
		if len(up) > 1 {
			t.Fatalf("at %v: %d coyote alphas alive", elapsed, len(up))
		}
		if len(up) == 1 {
			spawns++
			f.w.RemoveNPC(up[0].ID)
			f.tick()
		}
		f.advance(time.Second)
	}
	if spawns < 9 {
		t.Errorf("coyote alpha spawned %d times in 30 min, want about 10", spawns)
	}

	// Another alpha appears (not placed by the spawner) while the spawner's is
	// dead: when the timer runs out the spawner holds back.
	f = newFixture(t)
	f.tick()
	f.w.RemoveNPC(f.ofType(alpha)[0].ID)
	f.tick()
	other := &world.NPC{Type: world.NPCTypeID(alpha.ID), HP: alpha.HP}
	if _, err := f.w.AddNPC(other); err != nil {
		t.Fatal(err)
	}
	f.advance(alpha.Respawn())
	f.advance(time.Minute)
	if got := len(f.ofType(alpha)); got != 1 {
		t.Fatalf("with another alpha alive: %d alive, want 1", got)
	}
	// Once that one is gone, the delayed replacement comes straight away.
	f.w.RemoveNPC(other.ID)
	f.tick()
	if got := f.ofType(alpha); len(got) != 1 || got[0] == other {
		t.Fatalf("after the other alpha left: %d alive, want the spawner's replacement", len(got))
	}
}

// TestRetriesWhenIDRangeFull checks a replacement that can't get an id waits
// rather than being lost.
func TestRetriesWhenIDRangeFull(t *testing.T) {
	f := newFixture(t)
	total := 0
	for _, typ := range f.d.NPCs {
		total += typ.Population
	}
	cfg := world.DefaultConfig()
	cfg.NPCIDMin, cfg.NPCIDMax = 1<<15, 1<<15+world.NPCID(total)-1
	f.w = world.New(cfg)
	rabbit := f.npcType(t, "rabbit")
	f.tick()
	f.w.RemoveNPC(f.ofType(rabbit)[0].ID)
	f.tick()
	blocker := &world.NPC{Type: 0}
	if _, err := f.w.AddNPC(blocker); err != nil {
		t.Fatal(err)
	}
	f.advance(rabbit.Respawn())
	if got := len(f.ofType(rabbit)); got != rabbit.Population-1 {
		t.Fatalf("id range full: %d rabbits, want %d", got, rabbit.Population-1)
	}
	f.w.RemoveNPC(blocker.ID)
	f.tick()
	if got := len(f.ofType(rabbit)); got != rabbit.Population {
		t.Fatalf("id freed: %d rabbits, want %d", got, rabbit.Population)
	}
}

// blockedGrid is a fully blocked 4x4 collision.bin.
func blockedGrid(tb testing.TB) *world.Grid {
	tb.Helper()
	b := make([]byte, 22, 24)
	copy(b, "LCOL")
	b[4] = 1
	binary.LittleEndian.PutUint16(b[6:], 10)
	binary.LittleEndian.PutUint16(b[10:], 4)
	binary.LittleEndian.PutUint16(b[12:], 4)
	b = append(b, 0xFF, 0xFF)
	g, err := world.ParseGrid(b)
	if err != nil {
		tb.Fatal(err)
	}
	return g
}

func TestNewRejectsBadConfig(t *testing.T) {
	f := newFixture(t)
	r := rand.New(rand.NewPCG(1, 2))
	good := spawner.Config{Data: f.d, Areas: f.areas, Collision: f.grid, Clock: f.clock, Rand: r}
	tests := []struct {
		name string
		edit func(*spawner.Config)
	}{
		{"nil data", func(c *spawner.Config) { c.Data = nil }},
		{"nil areas", func(c *spawner.Config) { c.Areas = nil }},
		{"nil grid", func(c *spawner.Config) { c.Collision = nil }},
		{"nil clock", func(c *spawner.Config) { c.Clock = nil }},
		{"nil rand", func(c *spawner.Config) { c.Rand = nil }},
		{"no walkable cell", func(c *spawner.Config) { c.Collision = blockedGrid(t) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := good
			tt.edit(&cfg)
			if _, err := spawner.New(cfg); err == nil {
				t.Fatal("New succeeded, want error")
			}
		})
	}
}

func TestHandleIgnoresCommands(t *testing.T) {
	f := newFixture(t)
	f.s.Handle(nil, nil)
	if f.s.Name() != "spawner" {
		t.Errorf("Name = %q", f.s.Name())
	}
	if len(f.w.NPCs()) != 0 {
		t.Error("Handle spawned NPCs")
	}
}

// BenchmarkTick measures a steady-state tick with the full population alive.
func BenchmarkTick(b *testing.B) {
	f := newFixture(b)
	f.tick()
	b.ResetTimer()
	for b.Loop() {
		f.tick()
	}
}
