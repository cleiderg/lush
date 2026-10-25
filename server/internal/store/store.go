// Package store defines player persistence behind interfaces.
package store

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrNotFound is returned when no record matches the lookup.
	ErrNotFound = errors.New("store: not found")
	// ErrStaleVersion is returned when a save carries a version other than the stored one.
	ErrStaleVersion = errors.New("store: stale version")
	// ErrAlreadyExists is returned when creating a record that already exists.
	ErrAlreadyExists = errors.New("store: already exists")
	// ErrInvalid is returned when an economy change breaks a holdings rule
	// (negative gold, a bad stack, a player listed twice).
	ErrInvalid = errors.New("store: invalid")
)

// InventorySlots is the number of inventory slots (gameplay spec, Items).
const InventorySlots = 28

// EquipSlot indexes Holdings.Equipment.
type EquipSlot int

// The equipment slots (gameplay spec, Items).
const (
	EquipWeapon EquipSlot = iota
	EquipHead
	EquipBody
	EquipShield
	EquipSlots // number of equipment slots
)

// String returns the slot name as stored in the database.
func (s EquipSlot) String() string {
	switch s {
	case EquipWeapon:
		return "weapon"
	case EquipHead:
		return "head"
	case EquipBody:
		return "body"
	case EquipShield:
		return "shield"
	}
	return "unknown"
}

// Stack is the content of one inventory slot. The zero Stack is an empty slot;
// an occupied slot has a positive Item id and a positive Qty.
type Stack struct {
	Item int32
	Qty  int64
}

// Holdings is everything a player owns that the economy moves: gold, the
// inventory and the equipment. It changes only through ApplyEconomy.
type Holdings struct {
	Gold      int64
	Inventory [InventorySlots]Stack
	// Equipment holds an item id per EquipSlot; 0 is an empty slot.
	Equipment [EquipSlots]int32
}

// EconomyChange sets the holdings of one player.
type EconomyChange struct {
	PlayerID int64
	Holdings Holdings
}

// Player is a persisted player record.
type Player struct {
	ID        int64
	TokenHash string
	X, Z      float32
	// XP is combat experience. It is saved with the position by Save.
	XP int64
	// Holdings is read-only to Save; it changes only through ApplyEconomy.
	Holdings
	// Version is incremented by every successful Save. ApplyEconomy leaves it
	// alone, so a periodic save racing an economy write is not rejected.
	Version int64
}

// Players stores player records.
type Players interface {
	// CreateGuest creates a new player with the given token hash at version 1,
	// with no XP and empty holdings.
	// It returns an error wrapping ErrAlreadyExists if the hash is taken.
	CreateGuest(ctx context.Context, tokenHash string) (Player, error)
	// LoadByTokenHash returns the player, or an error wrapping ErrNotFound.
	LoadByTokenHash(ctx context.Context, tokenHash string) (Player, error)
	// Save stores the position and XP of p if p.Version matches the stored
	// version, and returns the stored record with the incremented version.
	// Otherwise it returns an error wrapping ErrStaleVersion (or ErrNotFound)
	// and changes nothing. Save never changes holdings, whatever p.Holdings says.
	Save(ctx context.Context, p Player) (Player, error)
	// ApplyEconomy sets the holdings of every player in changes and appends
	// entries to the economy audit log, all in one transaction: on any error
	// (an unknown player or audit actor, an invalid holding) nothing changes.
	// It does not change Version.
	ApplyEconomy(ctx context.Context, changes []EconomyChange, entries []Entry) error
}

// validate checks the rules both stores enforce on holdings. The Postgres
// schema enforces the same rules with constraints.
func (h Holdings) validate() error {
	if h.Gold < 0 {
		return fmt.Errorf("gold %d: %w", h.Gold, ErrInvalid)
	}
	for i, st := range h.Inventory {
		if st == (Stack{}) {
			continue
		}
		if st.Item <= 0 || st.Qty <= 0 {
			return fmt.Errorf("inventory slot %d: item %d qty %d: %w", i, st.Item, st.Qty, ErrInvalid)
		}
	}
	for i, id := range h.Equipment {
		if id < 0 {
			return fmt.Errorf("equipment slot %s: item %d: %w", EquipSlot(i), id, ErrInvalid)
		}
	}
	return nil
}

// distinctPlayers rejects a change list that names a player twice.
func distinctPlayers(changes []EconomyChange) error {
	seen := make(map[int64]bool, len(changes))
	for _, c := range changes {
		if seen[c.PlayerID] {
			return fmt.Errorf("player %d listed twice: %w", c.PlayerID, ErrInvalid)
		}
		seen[c.PlayerID] = true
	}
	return nil
}
