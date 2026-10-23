package store

import (
	"context"
	"fmt"
	"sync"
)

// Memory is a concurrency-safe in-memory Players implementation.
type Memory struct {
	mu     sync.Mutex
	nextID int64
	byID   map[int64]Player
	byHash map[string]int64
}

var _ Players = (*Memory)(nil)

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{nextID: 1, byID: map[int64]Player{}, byHash: map[string]int64{}}
}

func (m *Memory) CreateGuest(ctx context.Context, tokenHash string) (Player, error) {
	if err := ctx.Err(); err != nil {
		return Player{}, fmt.Errorf("create guest: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byHash[tokenHash]; ok {
		return Player{}, fmt.Errorf("create guest: %w", ErrAlreadyExists)
	}
	p := Player{ID: m.nextID, TokenHash: tokenHash, Version: 1}
	m.nextID++
	m.byID[p.ID] = p
	m.byHash[tokenHash] = p.ID
	return p, nil
}

func (m *Memory) LoadByTokenHash(ctx context.Context, tokenHash string) (Player, error) {
	if err := ctx.Err(); err != nil {
		return Player{}, fmt.Errorf("load player by token: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byHash[tokenHash]
	if !ok {
		return Player{}, fmt.Errorf("load player by token: %w", ErrNotFound)
	}
	return m.byID[id], nil
}

func (m *Memory) Save(ctx context.Context, p Player) (Player, error) {
	if err := ctx.Err(); err != nil {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.byID[p.ID]
	if !ok {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, ErrNotFound)
	}
	if cur.Version != p.Version {
		return Player{}, fmt.Errorf("save player %d: have version %d, stored %d: %w", p.ID, p.Version, cur.Version, ErrStaleVersion)
	}
	cur.X, cur.Z = p.X, p.Z
	cur.Version++
	m.byID[cur.ID] = cur
	return cur, nil
}
