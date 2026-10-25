package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
)

// Entry is one append-only economy audit record.
type Entry struct {
	// Kind names the event, for example "trade" or "item_create".
	Kind string
	// Actor is the player who caused the event.
	Actor int64
	// Counterparty is the other player in a trade, or 0 for none.
	Counterparty int64
	// Payload is any JSON-encodable detail; nil is stored as {}.
	Payload any
}

// Execer is satisfied by both pgx.Tx and *pgxpool.Pool, so callers can append
// inside their own transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Audit appends to the economy audit log. It deliberately has no update or
// delete method.
type Audit interface {
	Append(ctx context.Context, q Execer, entry Entry) error
}

// PGAudit is the Postgres Audit.
type PGAudit struct{}

var _ Audit = PGAudit{}

// Append inserts entry using q. An unknown actor or counterparty id fails.
func (PGAudit) Append(ctx context.Context, q Execer, e Entry) error {
	payload, err := encodePayload(e)
	if err != nil {
		return err
	}
	var counterparty *int64
	if e.Counterparty != 0 {
		counterparty = &e.Counterparty
	}
	_, err = q.Exec(ctx,
		`INSERT INTO economy_audit (kind, actor_player_id, counterparty_player_id, payload)
		 VALUES ($1, $2, $3, $4)`,
		e.Kind, e.Actor, counterparty, payload)
	if err != nil {
		return fmt.Errorf("store: audit append: %w", err)
	}
	return nil
}

// encodePayload returns e.Payload as JSON, {} for nil.
func encodePayload(e Entry) ([]byte, error) {
	if e.Payload == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("store: audit payload: %w", err)
	}
	return b, nil
}

// checkPayload fails, as PGAudit.Append would, if e.Payload can't be encoded.
func checkPayload(e Entry) error {
	_, err := encodePayload(e)
	return err
}

// MemoryAudit records entries in order, for tests in other packages. The q
// argument to Append is ignored and may be nil.
type MemoryAudit struct {
	mu      sync.Mutex
	entries []Entry
}

var _ Audit = (*MemoryAudit)(nil)

// Append records e.
func (m *MemoryAudit) Append(_ context.Context, _ Execer, e Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}

// Entries returns a copy of the recorded entries in append order.
func (m *MemoryAudit) Entries() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Entry(nil), m.entries...)
}
