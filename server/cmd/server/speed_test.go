package main

import (
	"math"
	"testing"

	"github.com/cleiderg/lush/server/internal/ops"
	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/world"
)

// TestConfiguredWalkAndRunSpeeds (#237 AC2) builds the mover and the session
// config the way run does from the ops config, and checks that a running
// player moves at the configured run speed and a walking one at the walk
// speed, and that the session gets the same speeds for Input and Welcome.
func TestConfiguredWalkAndRunSpeeds(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		walk, run float32
	}{
		{"defaults", nil, 4, 7},
		{"overridden", map[string]string{"LUSH_MOVE_SPEED": "3", "LUSH_RUN_SPEED": "9"}, 3, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ops.LoadConfig(func(k string) string { return tc.env[k] })
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MoveSpeed != tc.walk || cfg.RunSpeed != tc.run {
				t.Fatalf("config speeds %v/%v, want %v/%v", cfg.MoveSpeed, cfg.RunSpeed, tc.walk, tc.run)
			}
			mover, grid, err := newMover(cfg.MoveSpeed, cfg.RunSpeed)
			if err != nil {
				t.Fatal(err)
			}
			sc := sessionConfig(cfg, grid, nil, nil)
			if sc.MoveSpeed != tc.walk || sc.RunSpeed != tc.run {
				t.Fatalf("session speeds %v/%v, want %v/%v", sc.MoveSpeed, sc.RunSpeed, tc.walk, tc.run)
			}

			w := world.New(world.Config{TickRate: cfg.TickRate})
			if err := w.Register(mover); err != nil {
				t.Fatal(err)
			}
			spawn := world.Vec2{X: cfg.SpawnX, Y: cfg.SpawnZ}
			for _, id := range []world.PlayerID{1, 2} {
				if err := w.AddPlayer(&world.Player{ID: id, Pos: spawn}); err != nil {
					t.Fatal(err)
				}
			}
			const ticks = 4
			far := world.Vec2{X: spawn.X, Y: spawn.Y + 1000}
			for i := 0; i < ticks; i++ {
				w.Enqueue(movement.MoveCommand{Player: 1, To: far})
				w.Enqueue(movement.MoveCommand{Player: 2, To: far, Run: true})
				w.Tick()
			}
			secs := w.TickInterval().Seconds() * ticks
			for _, c := range []struct {
				id    world.PlayerID
				speed float32
			}{{1, tc.walk}, {2, tc.run}} {
				p := w.Player(c.id)
				if grid.Blocked(p.Pos) {
					t.Fatalf("player %d at %+v is on a blocked cell", c.id, p.Pos)
				}
				got := math.Hypot(float64(p.Pos.X-spawn.X), float64(p.Pos.Y-spawn.Y))
				want := float64(c.speed) * secs
				if math.Abs(got-want) > 1e-3 {
					t.Errorf("player %d (running %v) moved %.4f m in %v ticks, want %.4f (%v m/s)",
						c.id, p.Running, got, ticks, want, c.speed)
				}
			}
		})
	}
}
