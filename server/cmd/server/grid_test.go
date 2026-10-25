package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/ops"
	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/world"
)

// TestEmbeddedGridMatchesShared fails when the embedded copy drifts from the
// shared map's collision grid.
func TestEmbeddedGridMatchesShared(t *testing.T) {
	shared, err := os.ReadFile("../../../shared/map/collision.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(collisionData, shared) {
		t.Fatal("server/cmd/server/collision.bin differs from shared/map/collision.bin; " +
			"refresh it from the repo root with: cp shared/map/collision.bin server/cmd/server/collision.bin")
	}
}

func embeddedGrid(t *testing.T) *world.Grid {
	t.Helper()
	g, err := world.ParseGrid(collisionData)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestDefaultSpawnWalkable checks the default spawn (the map spec's safe
// spawn point) is on a walkable cell of the embedded grid.
func TestDefaultSpawnWalkable(t *testing.T) {
	cfg, err := ops.LoadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpawnX != -22 || cfg.SpawnZ != -76 {
		t.Fatalf("default spawn (%v, %v), want (-22, -76)", cfg.SpawnX, cfg.SpawnZ)
	}
	if embeddedGrid(t).Blocked(world.Vec2{X: cfg.SpawnX, Y: cfg.SpawnZ}) {
		t.Fatalf("default spawn (%v, %v) is on a blocked cell", cfg.SpawnX, cfg.SpawnZ)
	}
}

// TestMoverBlocksEmbeddedGrid walks a player east into the first blocked cell
// east of a walkable one, nearest the spawn, using the mover run wires up.
func TestMoverBlocksEmbeddedGrid(t *testing.T) {
	g := embeddedGrid(t)
	mover, grid, err := newMover(4)
	if err != nil {
		t.Fatal(err)
	}
	if grid == nil {
		t.Fatal("newMover returned no grid")
	}
	cell := float32(g.CellSize())
	// Scan east from the spawn for a walkable cell whose east neighbour is blocked.
	start := world.Vec2{X: ops.DefaultSpawnX, Y: ops.DefaultSpawnZ}
	var wall world.Vec2
	found := false
	for i := 0; i < 1000 && !found; i++ {
		p := world.Vec2{X: start.X + float32(i)*cell, Y: start.Y}
		next := world.Vec2{X: p.X + cell, Y: p.Y}
		if !g.Blocked(p) && g.Blocked(next) {
			start, wall, found = p, next, true
		}
	}
	if !found {
		t.Fatal("no blocked cell east of the spawn in the embedded grid")
	}

	w := world.New(world.DefaultConfig())
	if err := w.Register(mover); err != nil {
		t.Fatal(err)
	}
	if err := w.AddPlayer(&world.Player{ID: 1, Pos: start}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: wall.X + 5*cell, Y: wall.Y}})
		w.Tick()
		if p := w.Player(1).Pos; g.Blocked(p) {
			t.Fatalf("tick %d: player at %+v is inside a blocked cell (wall at %+v)", i, p, wall)
		}
	}
	if p := w.Player(1).Pos; p.X <= start.X {
		t.Fatalf("player at %+v never moved toward the wall from %+v", p, start)
	}
}

// TestRunBadGridExits checks run exits non-zero, with a logged error, when
// the embedded grid does not parse.
func TestRunBadGridExits(t *testing.T) {
	old := collisionData
	collisionData = []byte("not a grid")
	defer func() { collisionData = old }()
	out := &syncBuffer{}
	env := map[string]string{"LUSH_LISTEN_ADDR": "127.0.0.1:0"}
	code := run(context.Background(), func(k string) string { return env[k] }, world.NewFakeClock(time.Unix(0, 0)), out)
	if code == 0 {
		t.Fatalf("exit code 0, want non-zero\n%s", out)
	}
	if !strings.Contains(out.String(), `"level":"ERROR"`) || !strings.Contains(out.String(), "collision grid") {
		t.Fatalf("log should carry the grid error:\n%s", out)
	}
}
