package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the Postgres SQLSTATE for a unique constraint violation.
const uniqueViolation = "23505"

// PGPlayers is the Postgres-backed Players implementation.
type PGPlayers struct{ pool *pgxpool.Pool }

var _ Players = (*PGPlayers)(nil)

// NewPGPlayers returns a Players store over pool. The schema must already be
// migrated (see Migrate).
func NewPGPlayers(pool *pgxpool.Pool) *PGPlayers { return &PGPlayers{pool: pool} }

func (s *PGPlayers) CreateGuest(ctx context.Context, tokenHash string) (Player, error) {
	p := Player{TokenHash: tokenHash}
	var x, z float64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO players (token_hash, version) VALUES ($1, 1) RETURNING id, x, z, version`,
		[]byte(tokenHash)).Scan(&p.ID, &x, &z, &p.Version)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Player{}, fmt.Errorf("create guest: %w", ErrAlreadyExists)
		}
		return Player{}, fmt.Errorf("create guest: %w", err)
	}
	p.X, p.Z = float32(x), float32(z)
	return p, nil
}

func (s *PGPlayers) LoadByTokenHash(ctx context.Context, tokenHash string) (Player, error) {
	p := Player{TokenHash: tokenHash}
	var x, z float64
	err := s.pool.QueryRow(ctx,
		`SELECT id, x, z, version FROM players WHERE token_hash = $1`,
		[]byte(tokenHash)).Scan(&p.ID, &x, &z, &p.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Player{}, fmt.Errorf("load player by token: %w", ErrNotFound)
	}
	if err != nil {
		return Player{}, fmt.Errorf("load player by token: %w", err)
	}
	p.X, p.Z = float32(x), float32(z)
	return p, nil
}

// Save is one conditional UPDATE: a stale version matches no row and writes
// nothing. A follow-up read tells a stale version from a missing player.
func (s *PGPlayers) Save(ctx context.Context, p Player) (Player, error) {
	var x, z float64
	out := p
	err := s.pool.QueryRow(ctx,
		`UPDATE players SET x = $3, z = $4, version = version + 1, updated_at = now()
		 WHERE id = $1 AND version = $2
		 RETURNING x, z, version`,
		p.ID, p.Version, float64(p.X), float64(p.Z)).Scan(&x, &z, &out.Version)
	if err == nil {
		out.X, out.Z = float32(x), float32(z)
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, err)
	}
	var stored int64
	err = s.pool.QueryRow(ctx, `SELECT version FROM players WHERE id = $1`, p.ID).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, ErrNotFound)
	}
	if err != nil {
		return Player{}, fmt.Errorf("save player %d: %w", p.ID, err)
	}
	return Player{}, fmt.Errorf("save player %d: have version %d, stored %d: %w", p.ID, p.Version, stored, ErrStaleVersion)
}
