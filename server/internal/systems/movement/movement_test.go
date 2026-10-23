package movement_test

import (
	"math"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/world"
)

func setup(tb testing.TB, n int) *world.World {
	tb.Helper()
	w := world.New(world.DefaultConfig())
	sys, err := movement.New(movement.Config{MaxSpeed: 6}) // test value: 0.3 units per 20 Hz tick
	if err != nil {
		tb.Fatal(err)
	}
	if err := w.Register(sys); err != nil {
		tb.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		_ = w.AddPlayer(&world.Player{ID: world.PlayerID(i)})
	}
	return w
}

func TestMovementClamp(t *testing.T) {
	tests := []struct {
		name    string
		to      world.Vec2
		want    world.Vec2
		anim    world.Anim
		arrived bool
	}{
		{"normal move", world.Vec2{X: 0.2, Y: 0}, world.Vec2{X: 0.2, Y: 0}, world.AnimWalk, true},
		{"over-speed move", world.Vec2{X: 3, Y: 0}, world.Vec2{X: 0.3, Y: 0}, world.AnimWalk, false},
		{"teleport attempt", world.Vec2{X: 3000, Y: 4000}, world.Vec2{X: 0.18, Y: 0.24}, world.AnimWalk, false},
		{"huge finite target", world.Vec2{X: math.MaxFloat32, Y: 0}, world.Vec2{X: 0.3, Y: 0}, world.AnimWalk, false},
		{"NaN target ignored", world.Vec2{X: float32(math.NaN()), Y: 0}, world.Vec2{}, world.AnimIdle, true},
		{"Inf target ignored", world.Vec2{X: 0, Y: float32(math.Inf(-1))}, world.Vec2{}, world.AnimIdle, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setup(t, 1)
			w.Enqueue(movement.MoveCommand{Player: 1, To: tc.to})
			w.Tick()
			p := w.Player(1)
			if math.Abs(float64(p.Pos.X-tc.want.X)) > 1e-4 || math.Abs(float64(p.Pos.Y-tc.want.Y)) > 1e-4 {
				t.Fatalf("pos = %+v, want %+v", p.Pos, tc.want)
			}
			if p.Anim != tc.anim {
				t.Fatalf("anim = %v, want %v", p.Anim, tc.anim)
			}
			if p.HasDesired == tc.arrived {
				t.Fatalf("HasDesired = %v", p.HasDesired)
			}
		})
	}
}

func TestNewRejectsBadSpeed(t *testing.T) {
	for _, v := range []float32{0, -1, float32(math.NaN()), float32(math.Inf(1))} {
		if _, err := movement.New(movement.Config{MaxSpeed: v}); err == nil {
			t.Errorf("MaxSpeed %v accepted", v)
		}
	}
}

func TestMovementContinuesToTarget(t *testing.T) {
	w := setup(t, 1)
	w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 1, Y: 0}})
	for i := 0; i < 4; i++ {
		w.Tick()
	}
	if p := w.Player(1); p.Pos.X != 1 || p.Anim != world.AnimWalk {
		t.Fatalf("pos %+v anim %v", p.Pos, p.Anim)
	}
	w.Tick()
	if w.Player(1).Anim != world.AnimIdle {
		t.Fatal("should be idle after arriving")
	}
}

func BenchmarkMove1000Players(b *testing.B) {
	w := setup(b, 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for id := 1; id <= 1000; id++ {
			w.Enqueue(movement.MoveCommand{Player: world.PlayerID(id), To: world.Vec2{X: float32(i), Y: float32(id)}})
		}
		w.Tick()
	}
}

// gridRows builds a collision.bin for tests: 1 m cells, origin (0,0).
func gridRows(tb testing.TB, rows ...string) *world.Grid {
	tb.Helper()
	w, h := len(rows[0]), len(rows)
	b := make([]byte, 22, 22+(w*h+7)/8)
	copy(b, "LCOL")
	b[4] = 1
	b[6], b[8] = 10, 64
	b[10], b[12] = byte(w), byte(h)
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

func TestMovementCollision(t *testing.T) {
	tests := []struct {
		name  string
		rows  []string
		start world.Vec2
		to    world.Vec2
		check func(p world.Vec2) bool
	}{
		{"head-on wall stops", []string{
			"...#..", "...#..", "...#..", "...#..", "...#..", "...#.."},
			world.Vec2{X: 2.5, Y: 2.5}, world.Vec2{X: 5.5, Y: 2.5},
			func(p world.Vec2) bool { return p.X < 3 && p.X > 2.9 && p.Y == 2.5 }},
		{"diagonal slides along wall", []string{
			"......", "......", "......", "######", "......", "......"},
			world.Vec2{X: 1.5, Y: 2.5}, world.Vec2{X: 4.5, Y: 5.5},
			func(p world.Vec2) bool { return p.X > 4.4 && p.Y < 3 && p.Y >= 2.5 }},
		{"corner slides around single cell", []string{
			"......", "......", "......", "...#..", "......", "......"},
			world.Vec2{X: 2.5, Y: 2.5}, world.Vec2{X: 3.5, Y: 3.5},
			func(p world.Vec2) bool { return p.X == 3.5 && p.Y < 3 }},
		{"enclosed stays put", []string{
			"###", "#.#", "###"},
			world.Vec2{X: 1.5, Y: 1.5}, world.Vec2{X: 1.9, Y: 1.9},
			func(p world.Vec2) bool { return p.X >= 1 && p.X < 2 && p.Y >= 1 && p.Y < 2 }},
		{"leaving the grid is blocked", []string{
			"...", "...", "..."},
			world.Vec2{X: 2.5, Y: 1.5}, world.Vec2{X: 6, Y: 1.5},
			func(p world.Vec2) bool { return p.X < 3 && p.Y == 1.5 }},
		{"open ground unaffected", []string{
			"....", "....", "...."},
			world.Vec2{X: 0.5, Y: 0.5}, world.Vec2{X: 3.5, Y: 2.5},
			func(p world.Vec2) bool { return p == world.Vec2{X: 3.5, Y: 2.5} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := world.New(world.DefaultConfig())
			sys, err := movement.New(movement.Config{MaxSpeed: 6, Collision: gridRows(t, tc.rows...)})
			if err != nil {
				t.Fatal(err)
			}
			_ = w.Register(sys)
			_ = w.AddPlayer(&world.Player{ID: 1, Pos: tc.start})
			w.Enqueue(movement.MoveCommand{Player: 1, To: tc.to})
			for i := 0; i < 60; i++ {
				w.Tick()
			}
			if p := w.Player(1).Pos; !tc.check(p) {
				t.Fatalf("final pos %+v", p)
			}
		})
	}
}
