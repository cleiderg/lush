package movement_test

import (
	"math"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/world"
)

func setup(n int) *world.World {
	w := world.New(world.DefaultConfig())
	_ = w.Register(movement.New(movement.Config{MaxSpeed: 6})) // 0.3 units per 20 Hz tick
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := setup(1)
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

func TestMovementContinuesToTarget(t *testing.T) {
	w := setup(1)
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
	w := setup(1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for id := 1; id <= 1000; id++ {
			w.Enqueue(movement.MoveCommand{Player: world.PlayerID(id), To: world.Vec2{X: float32(i), Y: float32(id)}})
		}
		w.Tick()
	}
}
