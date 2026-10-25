package main

import (
	_ "embed"
	"fmt"

	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/world"
)

// collisionData is a copy of shared/map/collision.bin, embedded so the
// deployed binary needs no extra file. TestEmbeddedGridMatchesShared keeps it
// in sync; refresh with: cp shared/map/collision.bin server/cmd/server/collision.bin
//
//go:embed collision.bin
var collisionData []byte

// newMover parses the embedded collision grid and builds the movement system
// that enforces it. The grid is returned for other systems that need it.
func newMover(maxSpeed float32) (*movement.System, *world.Grid, error) {
	grid, err := world.ParseGrid(collisionData)
	if err != nil {
		return nil, nil, fmt.Errorf("embedded collision grid: %w", err)
	}
	mover, err := movement.New(movement.Config{MaxSpeed: maxSpeed, Collision: grid})
	if err != nil {
		return nil, nil, err
	}
	return mover, grid, nil
}
