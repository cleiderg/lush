package movement_test

import (
	"math"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/world"
)

// Test values matching D-044 (walk 4, run 7); at 20 Hz that is 0.2 and 0.35
// units per tick.
const (
	testWalk = 4
	testRun  = 7
	testDt   = 1.0 / 20
)

func TestNewRunSpeed(t *testing.T) {
	tests := []struct {
		name string
		max  float32
		run  float32
		ok   bool
	}{
		{"zero means run at max speed", 4, 0, true},
		{"run above max", 4, 7, true},
		{"run equal to max", 4, 4, true},
		{"negative", 4, -1, false},
		{"NaN", 4, float32(math.NaN()), false},
		{"+Inf", 4, float32(math.Inf(1)), false},
		{"-Inf", 4, float32(math.Inf(-1)), false},
		{"positive below max", 4, 3, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := movement.New(movement.Config{MaxSpeed: tc.max, RunSpeed: tc.run})
			if (err == nil) != tc.ok {
				t.Fatalf("New(max %v, run %v) err = %v, want ok %v", tc.max, tc.run, err, tc.ok)
			}
		})
	}
}

func runWorld(tb testing.TB, cfg movement.Config, start world.Vec2) *world.World {
	tb.Helper()
	w := world.New(world.DefaultConfig())
	sys, err := movement.New(cfg)
	if err != nil {
		tb.Fatal(err)
	}
	if err := w.Register(sys); err != nil {
		tb.Fatal(err)
	}
	if err := w.AddPlayer(&world.Player{ID: 1, Pos: start}); err != nil {
		tb.Fatal(err)
	}
	return w
}

func TestRunSpeedPerTick(t *testing.T) {
	tests := []struct {
		name     string
		runSpeed float32
		run      bool
		step     float64
	}{
		{"walking", testRun, false, testWalk * testDt},
		{"running", testRun, true, testRun * testDt},
		{"running without run speed uses max speed", 0, true, testWalk * testDt},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := runWorld(t, movement.Config{MaxSpeed: testWalk, RunSpeed: tc.runSpeed}, world.Vec2{})
			w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 300, Y: 400}, Run: tc.run})
			prev := w.Player(1).Pos
			for i := 0; i < 5; i++ {
				w.Tick()
				p := w.Player(1)
				if p.Running != tc.run {
					t.Fatalf("Running = %v, want %v", p.Running, tc.run)
				}
				d := math.Hypot(float64(p.Pos.X-prev.X), float64(p.Pos.Y-prev.Y))
				if d > tc.step+1e-5 || d < tc.step-1e-4 {
					t.Fatalf("tick %d step = %v, want %v", i, d, tc.step)
				}
				prev = p.Pos
			}
		})
	}
}

func TestRunThenWalkUpdatesRunning(t *testing.T) {
	w := runWorld(t, movement.Config{MaxSpeed: testWalk, RunSpeed: testRun}, world.Vec2{})
	w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 100}, Run: true})
	w.Tick()
	w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 100}})
	w.Tick()
	p := w.Player(1)
	if p.Running {
		t.Fatal("Running still set after a walking move")
	}
	if want := float32((testRun + testWalk) * testDt); math.Abs(float64(p.Pos.X-want)) > 1e-4 {
		t.Fatalf("pos.X = %v, want %v", p.Pos.X, want)
	}
}

func TestRunSlidesAlongWall(t *testing.T) {
	g := gridRows(t, "......", "......", "......", "######", "......", "......")
	w := runWorld(t, movement.Config{MaxSpeed: testWalk, RunSpeed: testRun, Collision: g}, world.Vec2{X: 1.5, Y: 2.5})
	w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 4.5, Y: 5.5}, Run: true})
	prev := w.Player(1).Pos
	for i := 0; i < 60; i++ {
		w.Tick()
		p := w.Player(1).Pos
		if d := math.Hypot(float64(p.X-prev.X), float64(p.Y-prev.Y)); d > testRun*testDt+1e-5 {
			t.Fatalf("tick %d step = %v, over run speed", i, d)
		}
		if g.Blocked(p) {
			t.Fatalf("tick %d in blocked cell %+v", i, p)
		}
		prev = p
	}
	if p := w.Player(1).Pos; !(p.X > 4.4 && p.Y < 3 && p.Y >= 2.5) {
		t.Fatalf("final pos %+v, want slid along wall", p)
	}
}

func BenchmarkRun1000Players(b *testing.B) {
	w := world.New(world.DefaultConfig())
	sys, err := movement.New(movement.Config{MaxSpeed: testWalk, RunSpeed: testRun})
	if err != nil {
		b.Fatal(err)
	}
	_ = w.Register(sys)
	for id := 1; id <= 1000; id++ {
		_ = w.AddPlayer(&world.Player{ID: world.PlayerID(id)})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for id := 1; id <= 1000; id++ {
			w.Enqueue(movement.MoveCommand{Player: world.PlayerID(id), To: world.Vec2{X: float32(i), Y: float32(id)}, Run: id%2 == 0})
		}
		w.Tick()
	}
}
