// Package store defines player persistence behind interfaces.
package store

import (
	"context"
	"errors"
)

var (
	// ErrNotFound is returned when no record matches the lookup.
	ErrNotFound = errors.New("store: not found")
	// ErrStaleVersion is returned when a save carries a version other than the stored one.
	ErrStaleVersion = errors.New("store: stale version")
	// ErrAlreadyExists is returned when creating a record that already exists.
	ErrAlreadyExists = errors.New("store: already exists")
)

// Player is a persisted player record.
type Player struct {
	ID        int64
	TokenHash string
	X, Z      float32
	// Version is incremented by every successful Save.
	Version int64
}

// Players stores player records.
type Players interface {
	// CreateGuest creates a new player with the given token hash at version 1.
	// It returns an error wrapping ErrAlreadyExists if the hash is taken.
	CreateGuest(ctx context.Context, tokenHash string) (Player, error)
	// LoadByTokenHash returns the player, or an error wrapping ErrNotFound.
	LoadByTokenHash(ctx context.Context, tokenHash string) (Player, error)
	// Save stores the position of p if p.Version matches the stored version,
	// and returns the stored record with the incremented version. Otherwise it
	// returns an error wrapping ErrStaleVersion (or ErrNotFound) and changes nothing.
	Save(ctx context.Context, p Player) (Player, error)
}
