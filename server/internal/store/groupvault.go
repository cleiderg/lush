package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Audit kinds written by the vault methods. The payload has the group id and
// the gold moved.
const (
	GroupVaultDepositKind  = "group_vault_deposit"
	GroupVaultWithdrawKind = "group_vault_withdraw"
)

// DoorLogKeep is how long door log entries are kept (groups spec, Doors).
const DoorLogKeep = 7 * 24 * time.Hour

// DoorAction is what happened to a door.
type DoorAction string

// The logged door actions.
const (
	DoorOpen   DoorAction = "open"
	DoorClose  DoorAction = "close"
	DoorLock   DoorAction = "lock"
	DoorUnlock DoorAction = "unlock"
	DoorBreak  DoorAction = "break"
)

// DoorLogEntry is one door log row. Player is 0 when unknown, or when the
// player has since been deleted.
type DoorLogEntry struct {
	Group  int64
	Hall   string
	Door   string
	Action DoorAction
	Player int64
	At     time.Time
}

// GroupVault moves gold between players and group vaults and keeps the door
// log. Gold moves are single transactions with an audit entry.
type GroupVault interface {
	// DepositGroupVault, in one transaction, moves gold from the player to the
	// group's vault and appends an audit entry. An unknown player or group
	// fails with ErrNotFound; gold <= 0 or more than the player has with
	// ErrInvalid. On any error nothing changes. It returns the new vault
	// balance.
	DepositGroupVault(ctx context.Context, group, player, gold int64) (int64, error)
	// WithdrawGroupVault is the reverse; a vault with less than gold fails
	// with ErrInvalid. Concurrent withdrawals are serialised on the group
	// row, so the vault never goes negative.
	WithdrawGroupVault(ctx context.Context, group, player, gold int64) (int64, error)
	// AppendDoorLog saves door log entries in the order given.
	AppendDoorLog(ctx context.Context, entries []DoorLogEntry) error
	// LoadDoorLog returns a group's entries at or after since, oldest first.
	LoadDoorLog(ctx context.Context, group int64, since time.Time) ([]DoorLogEntry, error)
	// PruneDoorLog deletes entries older than DoorLogKeep before now and
	// returns how many.
	PruneDoorLog(ctx context.Context, now time.Time) (int64, error)
}

var _ GroupVault = (*PGPlayers)(nil)

func (s *PGPlayers) DepositGroupVault(ctx context.Context, group, player, gold int64) (int64, error) {
	return s.moveVault(ctx, "deposit", GroupVaultDepositKind, group, player, gold)
}

func (s *PGPlayers) WithdrawGroupVault(ctx context.Context, group, player, gold int64) (int64, error) {
	return s.moveVault(ctx, "withdraw", GroupVaultWithdrawKind, group, player, -gold)
}

// moveVault adds delta to the vault and subtracts it from the player, with
// both rows locked (player first, then group, always in that order).
func (s *PGPlayers) moveVault(ctx context.Context, op, kind string, group, player, delta int64) (int64, error) {
	amount := delta
	if amount < 0 {
		amount = -amount
	}
	var vault int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if amount <= 0 {
			return fmt.Errorf("gold %d: %w", amount, ErrInvalid)
		}
		var gold int64
		err := tx.QueryRow(ctx, `SELECT gold FROM players WHERE id = $1 FOR UPDATE`, player).Scan(&gold)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock player: %w", err)
		}
		err = tx.QueryRow(ctx, `SELECT vault_gold FROM groups WHERE group_id = $1 FOR UPDATE`, group).Scan(&vault)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock group: %w", err)
		}
		if gold-delta < 0 || vault+delta < 0 {
			return fmt.Errorf("player gold %d, vault gold %d, move %d: %w", gold, vault, delta, ErrInvalid)
		}
		vault += delta
		if _, err := tx.Exec(ctx,
			`UPDATE players SET gold = gold - $2, updated_at = now() WHERE id = $1`, player, delta); err != nil {
			return classify(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE groups SET vault_gold = $2 WHERE group_id = $1`, group, vault); err != nil {
			return classify(err)
		}
		return PGAudit{}.Append(ctx, tx, Entry{Kind: kind, Actor: player,
			Payload: map[string]any{"group": group, "gold": amount}})
	})
	if err != nil {
		return 0, fmt.Errorf("%s group %d vault, player %d: %w", op, group, player, err)
	}
	return vault, nil
}

func (s *PGPlayers) AppendDoorLog(ctx context.Context, entries []DoorLogEntry) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, e := range entries {
			var player *int64
			if e.Player != 0 {
				player = &e.Player
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO group_door_log (group_id, hall, door, action, player_id, at)
				 VALUES ($1, $2, $3, $4, $5, $6)`,
				e.Group, e.Hall, e.Door, string(e.Action), player, e.At); err != nil {
				return classify(err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("append %d door log entries: %w", len(entries), err)
	}
	return nil
}

func (s *PGPlayers) LoadDoorLog(ctx context.Context, group int64, since time.Time) ([]DoorLogEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT group_id, hall, door, action, COALESCE(player_id, 0), at FROM group_door_log
		 WHERE group_id = $1 AND at >= $2 ORDER BY at, log_id`, group, since)
	if err != nil {
		return nil, fmt.Errorf("load door log of group %d: %w", group, err)
	}
	var out []DoorLogEntry
	var e DoorLogEntry
	if _, err := pgx.ForEachRow(rows, []any{&e.Group, &e.Hall, &e.Door, &e.Action, &e.Player, &e.At}, func() error {
		out = append(out, e)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("load door log of group %d: %w", group, err)
	}
	return out, nil
}

func (s *PGPlayers) PruneDoorLog(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM group_door_log WHERE at < $1`, now.Add(-DoorLogKeep))
	if err != nil {
		return 0, fmt.Errorf("prune door log: %w", err)
	}
	return tag.RowsAffected(), nil
}
