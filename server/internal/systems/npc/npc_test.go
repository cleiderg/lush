package npc_test

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/npc"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// walkSpeed is the D-044 walk speed the server passes in (LUSH_MOVE_SPEED).
const walkSpeed = 4

func lookup(tb testing.TB) *areas.Lookup {
	tb.Helper()
	l, err := areas.Load()
	if err != nil {
		tb.Fatal(err)
	}
	return l
}

func rules(tb testing.TB) gamedata.Rules {
	tb.Helper()
	d, err := gamedata.Default()
	if err != nil {
		tb.Fatal(err)
	}
	return d.Rules
}

// gridRows builds a collision grid for tests: 1 m cells, west/north edge at
// origin, '#' blocked.
func gridRows(tb testing.TB, origin world.Vec2, rows ...string) *world.Grid {
	tb.Helper()
	w, h := len(rows[0]), len(rows)
	b := make([]byte, 22, 22+(w*h+7)/8)
	copy(b, "LCOL")
	b[4] = 1
	b[6], b[8] = 10, 64
	binary.LittleEndian.PutUint16(b[10:], uint16(w))
	binary.LittleEndian.PutUint16(b[12:], uint16(h))
	binary.LittleEndian.PutUint32(b[14:], uint32(int32(origin.X*10)))
	binary.LittleEndian.PutUint32(b[18:], uint32(int32(origin.Y*10)))
	b = append(b, make([]byte, (w*h+7)/8)...)
	for r, row := range rows {
		for c, ch := range row {
			if ch == '#' {
				i := r*w + c
				b[22+i>>3] |= 1 << (uint(i) & 7)
			}
		}
	}
	g, err := world.ParseGrid(b)
	if err != nil {
		tb.Fatal(err)
	}
	return g
}

func newSystem(tb testing.TB, grid *world.Grid) *npc.System {
	tb.Helper()
	s, err := npc.New(npc.Config{WalkSpeed: walkSpeed, LeashTiles: rules(tb).LeashTiles, TileSize: 1, Collision: grid, Areas: lookup(tb)})
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

func dist(a, b world.Vec2) float64 {
	return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y))
}

func TestNewRejectsBadConfig(t *testing.T) {
	l := lookup(t)
	good := npc.Config{WalkSpeed: walkSpeed, LeashTiles: 12, TileSize: 1, Areas: l}
	tests := []struct {
		name string
		edit func(*npc.Config)
	}{
		{"zero speed", func(c *npc.Config) { c.WalkSpeed = 0 }},
		{"negative speed", func(c *npc.Config) { c.WalkSpeed = -1 }},
		{"NaN speed", func(c *npc.Config) { c.WalkSpeed = float32(math.NaN()) }},
		{"infinite speed", func(c *npc.Config) { c.WalkSpeed = float32(math.Inf(1)) }},
		{"zero leash", func(c *npc.Config) { c.LeashTiles = 0 }},
		{"zero tile size", func(c *npc.Config) { c.TileSize = 0 }},
		{"no areas", func(c *npc.Config) { c.Areas = nil }},
	}
	if _, err := npc.New(good); err != nil {
		t.Fatalf("good config: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.edit(&c)
			if _, err := npc.New(c); err == nil {
				t.Fatal("New accepted a bad config")
			}
		})
	}
}

// TestStepSpeed checks that a step covers at most WalkSpeed*dt and lands
// exactly on a goal within reach.
func TestStepSpeed(t *testing.T) {
	s := newSystem(t, nil)
	dt := 50 * time.Millisecond // 20 Hz
	tests := []struct {
		name      string
		pos, goal world.Vec2
		want      world.Vec2
		hasGoal   bool
	}{
		{"far goal clamped", world.Vec2{X: 100, Y: 100}, world.Vec2{X: 110, Y: 100}, world.Vec2{X: 100.2, Y: 100}, true},
		{"near goal reached", world.Vec2{X: 100, Y: 100}, world.Vec2{X: 100.1, Y: 100}, world.Vec2{X: 100.1, Y: 100}, false},
		{"at goal", world.Vec2{X: 100, Y: 100}, world.Vec2{X: 100, Y: 100}, world.Vec2{X: 100, Y: 100}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := &world.NPC{Pos: tc.pos, Home: tc.pos, Goal: tc.goal, HasGoal: true}
			s.Step(n, dt)
			if dist(n.Pos, tc.want) > 1e-4 || n.HasGoal != tc.hasGoal {
				t.Fatalf("pos %v hasGoal %v, want %v %v", n.Pos, n.HasGoal, tc.want, tc.hasGoal)
			}
			if n.HasGoal && n.Anim != world.AnimWalk {
				t.Fatalf("anim %v while walking", n.Anim)
			}
		})
	}
}

// TestLeashWalksHome (AC2): an NPC more than rules.leash_tiles from home sets
// home as its goal and walks there at walk speed, sliding along walls.
func TestLeashWalksHome(t *testing.T) {
	leash := float64(rules(t).LeashTiles)
	origin := world.Vec2{X: 100, Y: 100}
	grid := gridRows(t, origin,
		"##############################",
		"#............................#",
		"#............................#",
		"#............................#",
		"#.........#######............#",
		"#............................#",
		"#............................#",
		"##############################",
	)
	tests := []struct {
		name      string
		grid      *world.Grid
		pos, home world.Vec2
	}{
		{"open ground", nil, world.Vec2{X: 130, Y: 120}, world.Vec2{X: 110, Y: 105}},
		{"slides along a wall", grid, world.Vec2{X: 114.5, Y: 106.5}, world.Vec2{X: 101.5, Y: 101.5}},
		{"straight along a corridor", grid, world.Vec2{X: 128.5, Y: 102.5}, world.Vec2{X: 101.5, Y: 102.5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start := dist(tc.pos, tc.home)
			if start <= leash {
				t.Fatalf("test setup: start %.1f m from home is within the %.0f tile leash", start, leash)
			}
			w := world.New(world.DefaultConfig())
			s := newSystem(t, tc.grid)
			if err := w.Register(s); err != nil {
				t.Fatal(err)
			}
			n := &world.NPC{Type: 1, Pos: tc.pos, Home: tc.home, HP: 3}
			if _, err := w.AddNPC(n); err != nil {
				t.Fatal(err)
			}
			maxStep := walkSpeed*w.TickInterval().Seconds() + 1e-4
			w.Tick()
			if n.Goal != tc.home || !n.Returning {
				t.Fatalf("after leash: goal %v returning %v, want home %v", n.Goal, n.Returning, tc.home)
			}
			for i := 0; n.Pos != tc.home; i++ {
				if i > 2000 {
					t.Fatalf("never reached home: at %v, home %v", n.Pos, tc.home)
				}
				prev := n.Pos
				w.Tick()
				if d := dist(prev, n.Pos); d > maxStep {
					t.Fatalf("tick %d moved %.3f m, max %.3f", i, d, maxStep)
				}
				if tc.grid != nil && tc.grid.Blocked(n.Pos) {
					t.Fatalf("tick %d: inside a wall at %v", i, n.Pos)
				}
			}
			w.Tick()
			if n.Returning || n.HasGoal || n.Anim != world.AnimIdle {
				t.Fatalf("at home: returning %v hasGoal %v anim %v", n.Returning, n.HasGoal, n.Anim)
			}
		})
	}
}

// TestWithinLeashKeepsGoal checks an NPC inside the leash keeps its own goal.
func TestWithinLeashKeepsGoal(t *testing.T) {
	s := newSystem(t, nil)
	home := world.Vec2{X: 200, Y: 200}
	goal := world.Vec2{X: 205, Y: 200}
	n := &world.NPC{Pos: home, Home: home, Goal: goal, HasGoal: true}
	s.Leash(n)
	if n.Goal != goal || n.Returning {
		t.Fatalf("goal %v returning %v, want own goal", n.Goal, n.Returning)
	}
}

// TestSafeCircleAvoided (AC3): an NPC whose straight path to its goal crosses
// the safe circle walks around it and is outside the circle on every tick.
func TestSafeCircleAvoided(t *testing.T) {
	l := lookup(t)
	c, r := l.Spawn(), float32(l.SafeRadius())
	at := func(dx, dy float32) world.Vec2 { return world.Vec2{X: c.X + dx, Y: c.Y + dy} }
	tests := []struct {
		name      string
		pos, goal world.Vec2
		leashed   bool // goal is home, set by the leash
		reach     bool
	}{
		{"through the centre", at(-r-20, 0), at(r+20, 0), false, true},
		{"off-centre chord", at(-r-15, -10), at(r+15, 12), false, true},
		{"diagonal", at(-r, -r), at(r, r), false, true},
		{"leashed home across the circle", at(-r-20, 3), at(r+20, -3), true, true},
		{"goal inside the circle", at(0, r+10), c, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if l.InSafeCircle(tc.pos) {
				t.Fatal("test setup: start inside the safe circle")
			}
			// A leashed NPC is driven by the system's Tick; the others step
			// directly, since the leash would otherwise send them home.
			w := world.New(world.DefaultConfig())
			s := newSystem(t, nil)
			n := &world.NPC{Pos: tc.pos, Home: tc.pos, Goal: tc.goal, HasGoal: true}
			if tc.leashed {
				n.Home, n.HasGoal = tc.goal, false
			}
			if _, err := w.AddNPC(n); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2000; i++ {
				if tc.leashed {
					s.Tick(w, w.TickInterval())
				} else {
					s.Step(n, w.TickInterval())
				}
				if l.InSafeCircle(n.Pos) {
					t.Fatalf("tick %d: inside the safe circle at %v", i, n.Pos)
				}
				if !n.HasGoal {
					break
				}
			}
			if n.HasGoal {
				t.Fatalf("still walking after 2000 ticks at %v", n.Pos)
			}
			if got := n.Pos == tc.goal; got != tc.reach {
				t.Fatalf("ended at %v (goal %v), reached=%v want %v", n.Pos, tc.goal, got, tc.reach)
			}
			if !tc.reach && dist(n.Pos, c) > float64(r)+1 {
				t.Fatalf("stopped at %v, %.2f m from spawn; want at the circle's edge", n.Pos, dist(n.Pos, c))
			}
		})
	}
}

// TestCafeNotEntered: an NPC walking toward a goal inside the Student Center
// café stops at its edge and never steps inside.
func TestCafeNotEntered(t *testing.T) {
	l := lookup(t)
	tests := []struct {
		name      string
		pos, goal world.Vec2
	}{
		{"from the south door", world.Vec2{X: 45, Y: -340}, world.Vec2{X: 45, Y: -351}},
		{"from the west", world.Vec2{X: 30, Y: -351}, world.Vec2{X: 45, Y: -351}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !l.InInterior(tc.goal) || l.InInterior(tc.pos) {
				t.Fatal("test setup: want the goal inside the café and the start outside")
			}
			w := world.New(world.DefaultConfig())
			s := newSystem(t, nil)
			n := &world.NPC{Pos: tc.pos, Home: tc.pos, Goal: tc.goal, HasGoal: true}
			for i := 0; i < 200 && n.HasGoal; i++ {
				s.Step(n, w.TickInterval())
				if l.NoCombat(n.Pos) {
					t.Fatalf("tick %d: inside the café at %v", i, n.Pos)
				}
			}
			if n.HasGoal {
				t.Fatalf("still walking at %v", n.Pos)
			}
			if d := dist(n.Pos, tc.goal); d > 15 || d < 3 {
				t.Errorf("stopped at %v, %.2f m from the goal; want at the café's edge", n.Pos, d)
			}
		})
	}
}

func TestHandleIgnoresCommands(t *testing.T) {
	s := newSystem(t, nil)
	p := &world.Player{ID: 1, Pos: world.Vec2{X: 1}}
	s.Handle(nil, p)
	if p.Pos != (world.Vec2{X: 1}) {
		t.Fatal("Handle changed the player")
	}
	if s.Name() != "npc" {
		t.Fatalf("name %q", s.Name())
	}
}

func BenchmarkTick1000NPCs(b *testing.B) {
	w := world.New(world.DefaultConfig())
	s := newSystem(b, nil)
	for i := 0; i < 1000; i++ {
		home := world.Vec2{X: float32(200 + i%40), Y: float32(200 + i/40)}
		if _, err := w.AddNPC(&world.NPC{Pos: home, Home: home}); err != nil {
			b.Fatal(err)
		}
	}
	dt := w.TickInterval()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j, n := range w.NPCs() {
			if !n.HasGoal {
				n.Goal, n.HasGoal = world.Vec2{X: n.Home.X + float32(j%7) - 3, Y: n.Home.Y + 3}, true
			}
		}
		s.Tick(w, dt)
	}
}
