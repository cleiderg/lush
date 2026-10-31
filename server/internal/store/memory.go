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
	audit  []Entry

	nextPot int64
	pots    map[int64]*memPot // open card pots

	banks map[int64]map[int32]int64 // player id -> item id -> banked qty
}

var _ Players = (*Memory)(nil)

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{nextID: 1, byID: map[int64]Player{}, byHash: map[string]int64{}}
}

// CreateGuest validates the holdings and every entry before creating
// anything, so a failure leaves the store as it was.
func (m *Memory) CreateGuest(ctx context.Context, tokenHash string, start Holdings, entries []Entry) (Player, error) {
	if err := ctx.Err(); err != nil {
		return Player{}, fmt.Errorf("create guest: %w", err)
	}
	if err := start.validate(); err != nil {
		return Player{}, fmt.Errorf("create guest: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byHash[tokenHash]; ok {
		return Player{}, fmt.Errorf("create guest: %w", ErrAlreadyExists)
	}
	logged := make([]Entry, len(entries))
	for i, e := range entries {
		if _, ok := m.byID[e.Counterparty]; e.Counterparty != 0 && !ok {
			return Player{}, fmt.Errorf("create guest: audit counterparty %d: %w", e.Counterparty, ErrNotFound)
		}
		if err := checkPayload(e); err != nil {
			return Player{}, fmt.Errorf("create guest: %w", err)
		}
		e.Actor = m.nextID
		logged[i] = e
	}
	p := Player{ID: m.nextID, TokenHash: tokenHash, Holdings: start, Version: 1}
	m.nextID++
	m.byID[p.ID] = p
	m.byHash[tokenHash] = p.ID
	m.audit = append(m.audit, logged...)
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
	if p.XP < 0 {
		return Player{}, fmt.Errorf("save player %d: xp %d: %w", p.ID, p.XP, ErrInvalid)
	}
	cur.X, cur.Z, cur.XP = p.X, p.Z, p.XP
	cur.Version++
	m.byID[cur.ID] = cur
	return cur, nil
}

// ApplyEconomy validates every change and entry before changing anything, so
// a failure leaves the store as it was.
func (m *Memory) ApplyEconomy(ctx context.Context, changes []EconomyChange, entries []Entry) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("apply economy: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkEconomyLocked(changes, entries); err != nil {
		return fmt.Errorf("apply economy: %w", err)
	}
	m.applyEconomyLocked(changes, entries)
	return nil
}

// checkEconomyLocked checks what ApplyEconomy checks, changing nothing.
func (m *Memory) checkEconomyLocked(changes []EconomyChange, entries []Entry) error {
	if err := distinctPlayers(changes); err != nil {
		return err
	}
	for _, c := range changes {
		if _, ok := m.byID[c.PlayerID]; !ok {
			return fmt.Errorf("player %d: %w", c.PlayerID, ErrNotFound)
		}
		if err := c.Holdings.validate(); err != nil {
			return fmt.Errorf("player %d: %w", c.PlayerID, err)
		}
	}
	for _, e := range entries {
		if err := m.checkEntryLocked(e); err != nil {
			return err
		}
	}
	return nil
}

// checkEntryLocked fails, as PGAudit.Append would, on an unknown actor or
// counterparty or a payload that can't be encoded.
func (m *Memory) checkEntryLocked(e Entry) error {
	if _, ok := m.byID[e.Actor]; !ok {
		return fmt.Errorf("audit actor %d: %w", e.Actor, ErrNotFound)
	}
	if _, ok := m.byID[e.Counterparty]; e.Counterparty != 0 && !ok {
		return fmt.Errorf("audit counterparty %d: %w", e.Counterparty, ErrNotFound)
	}
	return checkPayload(e)
}

// applyEconomyLocked writes checked changes and entries.
func (m *Memory) applyEconomyLocked(changes []EconomyChange, entries []Entry) {
	for _, c := range changes {
		p := m.byID[c.PlayerID]
		p.Holdings = c.Holdings
		m.byID[p.ID] = p
	}
	m.audit = append(m.audit, entries...)
}

// AuditEntries returns a copy of the audit entries appended by ApplyEconomy
// and the card pot methods, in order.
func (m *Memory) AuditEntries() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Entry(nil), m.audit...)
}
