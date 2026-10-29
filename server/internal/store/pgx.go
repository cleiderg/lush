package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres SQLSTATE codes the store maps to its own errors.
const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
	checkViolation      = "23514"
)

// PGPlayers is the Postgres-backed Players implementation.
type PGPlayers struct{ pool *pgxpool.Pool }

var _ Players = (*PGPlayers)(nil)

// NewPGPlayers returns a Players store over pool. The schema must already be
// migrated (see Migrate).
func NewPGPlayers(pool *pgxpool.Pool) *PGPlayers { return &PGPlayers{pool: pool} }

// querier is satisfied by both pgx.Tx and *pgxpool.Pool.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// CreateGuest inserts the player row, its holdings and the audit entries in
// one transaction; the schema's constraints reject an invalid holding.
func (s *PGPlayers) CreateGuest(ctx context.Context, tokenHash string, start Holdings, entries []Entry) (Player, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Player{}, fmt.Errorf("create guest: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	p := Player{TokenHash: tokenHash, Holdings: start}
	var x, z float64
	err = tx.QueryRow(ctx,
		`INSERT INTO players (token_hash, version) VALUES ($1, 1)
		 RETURNING id, x, z, combat_xp, version`,
		[]byte(tokenHash)).Scan(&p.ID, &x, &z, &p.XP, &p.Version)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Player{}, fmt.Errorf("create guest: %w", ErrAlreadyExists)
		}
		return Player{}, fmt.Errorf("create guest: %w", err)
	}
	p.X, p.Z = float32(x), float32(z)
	if err := writeHoldings(ctx, tx, EconomyChange{PlayerID: p.ID, Holdings: start}); err != nil {
		return Player{}, fmt.Errorf("create guest %d: %w", p.ID, classify(err))
	}
	var audit PGAudit
	for _, e := range entries {
		e.Actor = p.ID
		if err := audit.Append(ctx, tx, e); err != nil {
			return Player{}, fmt.Errorf("create guest %d: %w", p.ID, classify(err))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Player{}, fmt.Errorf("create guest: %w", err)
	}
	return p, nil
}

// LoadByTokenHash reads the player row and its holdings in one read-only
// transaction, so it never sees half of an economy write.
func (s *PGPlayers) LoadByTokenHash(ctx context.Context, tokenHash string) (Player, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Player{}, fmt.Errorf("load player by token: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only; nothing to undo

	p := Player{TokenHash: tokenHash}
	var x, z float64
	err = tx.QueryRow(ctx,
		`SELECT id, x, z, combat_xp, gold, version FROM players WHERE token_hash = $1`,
		[]byte(tokenHash)).Scan(&p.ID, &x, &z, &p.XP, &p.Gold, &p.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Player{}, fmt.Errorf("load player by token: %w", ErrNotFound)
	}
	if err != nil {
		return Player{}, fmt.Errorf("load player by token: %w", err)
	}
	p.X, p.Z = float32(x), float32(z)
	if err := loadItems(ctx, tx, p.ID, &p.Holdings); err != nil {
		return Player{}, fmt.Errorf("load player %d: %w", p.ID, err)
	}
	return p, nil
}

// loadItems fills h.Inventory and h.Equipment for player id.
func loadItems(ctx context.Context, q querier, id int64, h *Holdings) error {
	rows, err := q.Query(ctx,
		`SELECT slot, item_id, quantity FROM player_inventory WHERE player_id = $1`, id)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	var slot int16
	var st Stack
	_, err = pgx.ForEachRow(rows, []any{&slot, &st.Item, &st.Qty}, func() error {
		if slot < 0 || int(slot) >= InventorySlots {
			return fmt.Errorf("inventory slot %d out of range", slot)
		}
		h.Inventory[slot] = st
		return nil
	})
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}

	rows, err = q.Query(ctx,
		`SELECT slot, item_id FROM player_equipment WHERE player_id = $1`, id)
	if err != nil {
		return fmt.Errorf("equipment: %w", err)
	}
	var name string
	var item int32
	_, err = pgx.ForEachRow(rows, []any{&name, &item}, func() error {
		for i := EquipSlot(0); i < EquipSlots; i++ {
			if i.String() == name {
				h.Equipment[i] = item
				return nil
			}
		}
		return fmt.Errorf("unknown equipment slot %q", name)
	})
	if err != nil {
		return fmt.Errorf("equipment: %w", err)
	}
	return nil
}

// Save is one conditional UPDATE: a stale version matches no row and writes
// nothing. A follow-up read tells a stale version from a missing player. The
// returned holdings are read in the same transaction as the update.
func (s *PGPlayers) Save(ctx context.Context, p Player) (Player, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	var x, z float64
	out := Player{ID: p.ID, TokenHash: p.TokenHash}
	err = tx.QueryRow(ctx,
		`UPDATE players SET x = $3, z = $4, combat_xp = $5, version = version + 1, updated_at = now()
		 WHERE id = $1 AND version = $2
		 RETURNING x, z, combat_xp, gold, version`,
		p.ID, p.Version, float64(p.X), float64(p.Z), p.XP).Scan(&x, &z, &out.XP, &out.Gold, &out.Version)
	if err == nil {
		out.X, out.Z = float32(x), float32(z)
		if err := loadItems(ctx, tx, p.ID, &out.Holdings); err != nil {
			return Player{}, fmt.Errorf("save player %d: %w", p.ID, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Player{}, fmt.Errorf("save player %d: %w", p.ID, err)
		}
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, classify(err))
	}
	var stored int64
	err = tx.QueryRow(ctx, `SELECT version FROM players WHERE id = $1`, p.ID).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, ErrNotFound)
	}
	if err != nil {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, err)
	}
	return Player{}, fmt.Errorf("save player %d: have version %d, stored %d: %w", p.ID, p.Version, stored, ErrStaleVersion)
}

// ApplyEconomy writes every change and entry in one transaction. Holdings
// rules are enforced by the schema's constraints, so an invalid holding fails
// the transaction wherever it appears. Players are locked in id order so two
// concurrent economy writes over the same players cannot deadlock.
func (s *PGPlayers) ApplyEconomy(ctx context.Context, changes []EconomyChange, entries []Entry) error {
	if err := distinctPlayers(changes); err != nil {
		return fmt.Errorf("apply economy: %w", err)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return writeEconomy(ctx, tx, changes, entries)
	})
	if err != nil {
		return fmt.Errorf("apply economy: %w", err)
	}
	return nil
}

// writeEconomy writes every change, in player id order, and every entry
// inside tx. The caller has checked that changes name distinct players.
func writeEconomy(ctx context.Context, tx pgx.Tx, changes []EconomyChange, entries []Entry) error {
	sorted := slices.SortedFunc(slices.Values(changes), func(a, b EconomyChange) int {
		return cmp.Compare(a.PlayerID, b.PlayerID)
	})
	for _, c := range sorted {
		if err := writeHoldings(ctx, tx, c); err != nil {
			return fmt.Errorf("player %d: %w", c.PlayerID, classify(err))
		}
	}
	var audit PGAudit
	for _, e := range entries {
		if err := audit.Append(ctx, tx, e); err != nil {
			return classify(err)
		}
	}
	return nil
}

// writeHoldings replaces one player's gold, inventory and equipment inside tx.
func writeHoldings(ctx context.Context, tx pgx.Tx, c EconomyChange) error {
	tag, err := tx.Exec(ctx,
		`UPDATE players SET gold = $2, updated_at = now() WHERE id = $1`, c.PlayerID, c.Holdings.Gold)
	if err != nil {
		return fmt.Errorf("gold: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	var slots []int16
	var items []int32
	var qtys []int64
	for i, st := range c.Holdings.Inventory {
		if st != (Stack{}) {
			slots = append(slots, int16(i))
			items = append(items, st.Item)
			qtys = append(qtys, st.Qty)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM player_inventory WHERE player_id = $1`, c.PlayerID); err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	if len(slots) > 0 {
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_inventory (player_id, slot, item_id, quantity)
			 SELECT $1::bigint, s, i, q FROM unnest($2::smallint[], $3::integer[], $4::bigint[]) AS t (s, i, q)`,
			c.PlayerID, slots, items, qtys); err != nil {
			return fmt.Errorf("inventory: %w", err)
		}
	}

	var names []string
	items = nil
	for i, id := range c.Holdings.Equipment {
		if id != 0 {
			names = append(names, EquipSlot(i).String())
			items = append(items, id)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM player_equipment WHERE player_id = $1`, c.PlayerID); err != nil {
		return fmt.Errorf("equipment: %w", err)
	}
	if len(names) > 0 {
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_equipment (player_id, slot, item_id)
			 SELECT $1::bigint, s, i FROM unnest($2::text[], $3::integer[]) AS t (s, i)`,
			c.PlayerID, names, items); err != nil {
			return fmt.Errorf("equipment: %w", err)
		}
	}
	return nil
}

// classify adds ErrInvalid to a failed check constraint and ErrNotFound to a
// foreign key pointing at a missing player, keeping the original error.
func classify(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case checkViolation:
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	case foreignKeyViolation:
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return err
}
