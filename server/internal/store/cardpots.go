package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"
)

// CardStake is one player's stake in a card pot.
type CardStake struct {
	PlayerID int64
	Gold     int64
}

// CardPot is an open card pot: the stakes of one staked card game, from the
// transaction that took them until the winner is paid or every stake is
// refunded. Stakes lists the stakes not yet refunded, by player id.
type CardPot struct {
	ID     int64
	Table  int32
	Stakes []CardStake
}

// CardPots stores the card room's gold movements (card-tables spec, Stakes).
// Every method is one transaction that also writes holdings and audit
// entries as ApplyEconomy does: on any error nothing changes.
type CardPots interface {
	// OpenCardPot sets the holdings in changes, appends entries and records
	// an open pot at table with stakes, all in one transaction, and returns
	// the pot's id. Stakes must be positive and name distinct players.
	OpenCardPot(ctx context.Context, table int32, stakes []CardStake, changes []EconomyChange, entries []Entry) (int64, error)
	// PayCardPot sets the holdings in changes (the winner's, with the pot
	// added), appends entries and closes pot, in one transaction. It returns
	// an error wrapping ErrNotFound, and changes nothing, if pot is not open.
	PayCardPot(ctx context.Context, pot int64, changes []EconomyChange, entries []Entry) error
	// RefundCardStake sets the holdings in changes (player's, with their
	// stake added back), appends entries and marks player's stake in pot
	// refunded, closing the pot if no stake is left in it, in one
	// transaction. It returns an error wrapping ErrNotFound, and changes
	// nothing, if pot is not open or holds no stake of player.
	RefundCardStake(ctx context.Context, pot, player int64, changes []EconomyChange, entries []Entry) error
	// OpenCardPots returns every open pot, by id.
	OpenCardPots(ctx context.Context) ([]CardPot, error)
	// RefundOpenCardPots returns every stake left in every open pot to its
	// player's stored gold, appends entry(pot, stake) to the audit log for
	// each, and closes the pot; one transaction per pot. Call it at startup,
	// before any player is loaded. It returns the pots it refunded, by id,
	// and stops at the first pot that fails, which stays open.
	RefundOpenCardPots(ctx context.Context, entry func(CardPot, CardStake) Entry) ([]CardPot, error)
}

var (
	_ CardPots = (*Memory)(nil)
	_ CardPots = (*PGPlayers)(nil)
)

// validateStakes rejects an empty stake list, a stake that isn't positive and
// a player staked twice.
func validateStakes(stakes []CardStake) error {
	if len(stakes) == 0 {
		return fmt.Errorf("no stakes: %w", ErrInvalid)
	}
	seen := make(map[int64]bool, len(stakes))
	for _, s := range stakes {
		if s.Gold <= 0 {
			return fmt.Errorf("player %d stake %d: %w", s.PlayerID, s.Gold, ErrInvalid)
		}
		if seen[s.PlayerID] {
			return fmt.Errorf("player %d staked twice: %w", s.PlayerID, ErrInvalid)
		}
		seen[s.PlayerID] = true
	}
	return nil
}

// ---- Memory ----

// memPot is an in-memory card pot. Refunded stakes are removed from stakes.
type memPot struct {
	table  int32
	stakes []CardStake
}

// OpenCardPot validates everything before writing anything.
func (m *Memory) OpenCardPot(ctx context.Context, table int32, stakes []CardStake, changes []EconomyChange, entries []Entry) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("open card pot: %w", err)
	}
	if err := validateStakes(stakes); err != nil {
		return 0, fmt.Errorf("open card pot: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range stakes {
		if _, ok := m.byID[s.PlayerID]; !ok {
			return 0, fmt.Errorf("open card pot: stake of player %d: %w", s.PlayerID, ErrNotFound)
		}
	}
	if err := m.checkEconomyLocked(changes, entries); err != nil {
		return 0, fmt.Errorf("open card pot: %w", err)
	}
	m.nextPot++
	id := m.nextPot
	if m.pots == nil {
		m.pots = map[int64]*memPot{}
	}
	m.pots[id] = &memPot{table: table, stakes: slices.SortedFunc(slices.Values(stakes), byPlayer)}
	m.applyEconomyLocked(changes, entries)
	return id, nil
}

// PayCardPot validates everything before writing anything.
func (m *Memory) PayCardPot(ctx context.Context, pot int64, changes []EconomyChange, entries []Entry) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("pay card pot %d: %w", pot, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pots[pot] == nil {
		return fmt.Errorf("pay card pot %d: %w", pot, ErrNotFound)
	}
	if err := m.checkEconomyLocked(changes, entries); err != nil {
		return fmt.Errorf("pay card pot %d: %w", pot, err)
	}
	delete(m.pots, pot)
	m.applyEconomyLocked(changes, entries)
	return nil
}

// RefundCardStake validates everything before writing anything.
func (m *Memory) RefundCardStake(ctx context.Context, pot, player int64, changes []EconomyChange, entries []Entry) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("refund card stake %d/%d: %w", pot, player, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.pots[pot]
	i := -1
	if p != nil {
		i = slices.IndexFunc(p.stakes, func(s CardStake) bool { return s.PlayerID == player })
	}
	if i < 0 {
		return fmt.Errorf("refund card stake %d/%d: %w", pot, player, ErrNotFound)
	}
	if err := m.checkEconomyLocked(changes, entries); err != nil {
		return fmt.Errorf("refund card stake %d/%d: %w", pot, player, err)
	}
	p.stakes = slices.Delete(p.stakes, i, i+1)
	if len(p.stakes) == 0 {
		delete(m.pots, pot)
	}
	m.applyEconomyLocked(changes, entries)
	return nil
}

func (m *Memory) OpenCardPots(ctx context.Context) ([]CardPot, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open card pots: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openPotsLocked(), nil
}

func (m *Memory) openPotsLocked() []CardPot {
	out := make([]CardPot, 0, len(m.pots))
	for id, p := range m.pots {
		out = append(out, CardPot{ID: id, Table: p.table, Stakes: slices.Clone(p.stakes)})
	}
	slices.SortFunc(out, func(a, b CardPot) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// RefundOpenCardPots refunds each pot only once every stake in it is valid.
func (m *Memory) RefundOpenCardPots(ctx context.Context, entry func(CardPot, CardStake) Entry) ([]CardPot, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("refund card pots: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var done []CardPot
	for _, pot := range m.openPotsLocked() {
		entries := make([]Entry, len(pot.Stakes))
		for i, s := range pot.Stakes {
			p, ok := m.byID[s.PlayerID]
			if !ok {
				return done, fmt.Errorf("refund card pot %d: player %d: %w", pot.ID, s.PlayerID, ErrNotFound)
			}
			if p.Gold > math.MaxInt64-s.Gold {
				return done, fmt.Errorf("refund card pot %d: player %d gold overflows: %w", pot.ID, s.PlayerID, ErrInvalid)
			}
			entries[i] = entry(pot, s)
			if err := m.checkEntryLocked(entries[i]); err != nil {
				return done, fmt.Errorf("refund card pot %d: %w", pot.ID, err)
			}
		}
		for _, s := range pot.Stakes {
			p := m.byID[s.PlayerID]
			p.Gold += s.Gold
			m.byID[p.ID] = p
		}
		m.audit = append(m.audit, entries...)
		delete(m.pots, pot.ID)
		done = append(done, pot)
	}
	return done, nil
}

func byPlayer(a, b CardStake) int { return cmp.Compare(a.PlayerID, b.PlayerID) }

// ---- Postgres ----

// OpenCardPot inserts the pot and its stakes, then writes the holdings and
// entries, in one transaction.
func (s *PGPlayers) OpenCardPot(ctx context.Context, table int32, stakes []CardStake, changes []EconomyChange, entries []Entry) (int64, error) {
	if err := validateStakes(stakes); err != nil {
		return 0, fmt.Errorf("open card pot: %w", err)
	}
	if err := distinctPlayers(changes); err != nil {
		return 0, fmt.Errorf("open card pot: %w", err)
	}
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO card_pots (table_id) VALUES ($1) RETURNING id`, table).Scan(&id); err != nil {
			return fmt.Errorf("pot: %w", err)
		}
		players := make([]int64, len(stakes))
		golds := make([]int64, len(stakes))
		for i, st := range stakes {
			players[i], golds[i] = st.PlayerID, st.Gold
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO card_pot_stakes (pot_id, player_id, stake)
			 SELECT $1::bigint, p, g FROM unnest($2::bigint[], $3::bigint[]) AS t (p, g)`,
			id, players, golds); err != nil {
			return fmt.Errorf("stakes: %w", classify(err))
		}
		return writeEconomy(ctx, tx, changes, entries)
	})
	if err != nil {
		return 0, fmt.Errorf("open card pot: %w", err)
	}
	return id, nil
}

// PayCardPot closes the pot, then writes the holdings and entries, in one
// transaction.
func (s *PGPlayers) PayCardPot(ctx context.Context, pot int64, changes []EconomyChange, entries []Entry) error {
	if err := distinctPlayers(changes); err != nil {
		return fmt.Errorf("pay card pot %d: %w", pot, err)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE card_pots SET closed_at = now(), closed_by = 'payout' WHERE id = $1 AND closed_at IS NULL`, pot)
		if err != nil {
			return fmt.Errorf("close: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return writeEconomy(ctx, tx, changes, entries)
	})
	if err != nil {
		return fmt.Errorf("pay card pot %d: %w", pot, err)
	}
	return nil
}

// RefundCardStake locks the pot, marks the stake refunded, writes the
// holdings and entries and closes the pot if it is empty, in one transaction.
func (s *PGPlayers) RefundCardStake(ctx context.Context, pot, player int64, changes []EconomyChange, entries []Entry) error {
	if err := distinctPlayers(changes); err != nil {
		return fmt.Errorf("refund card stake %d/%d: %w", pot, player, err)
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockOpenPot(ctx, tx, pot); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE card_pot_stakes SET refunded_at = now()
			 WHERE pot_id = $1 AND player_id = $2 AND refunded_at IS NULL`, pot, player)
		if err != nil {
			return fmt.Errorf("stake: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := writeEconomy(ctx, tx, changes, entries); err != nil {
			return err
		}
		return closeIfRefunded(ctx, tx, pot)
	})
	if err != nil {
		return fmt.Errorf("refund card stake %d/%d: %w", pot, player, err)
	}
	return nil
}

// lockOpenPot locks pot's row, or returns ErrNotFound if it is not open.
func lockOpenPot(ctx context.Context, tx pgx.Tx, pot int64) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM card_pots WHERE id = $1 AND closed_at IS NULL FOR UPDATE`, pot).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock pot: %w", err)
	}
	return nil
}

// closeIfRefunded closes pot if no stake is left in it.
func closeIfRefunded(ctx context.Context, tx pgx.Tx, pot int64) error {
	if _, err := tx.Exec(ctx,
		`UPDATE card_pots SET closed_at = now(), closed_by = 'refund'
		 WHERE id = $1 AND closed_at IS NULL
		   AND NOT EXISTS (SELECT 1 FROM card_pot_stakes WHERE pot_id = $1 AND refunded_at IS NULL)`, pot); err != nil {
		return fmt.Errorf("close pot: %w", err)
	}
	return nil
}

func (s *PGPlayers) OpenCardPots(ctx context.Context) ([]CardPot, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("open card pots: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only; nothing to undo
	pots, err := openPots(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("open card pots: %w", err)
	}
	return pots, nil
}

// openPots reads every open pot with the stakes left in it.
func openPots(ctx context.Context, q querier) ([]CardPot, error) {
	rows, err := q.Query(ctx,
		`SELECT p.id, p.table_id, s.player_id, s.stake
		 FROM card_pots p JOIN card_pot_stakes s ON s.pot_id = p.id
		 WHERE p.closed_at IS NULL AND s.refunded_at IS NULL
		 ORDER BY p.id, s.player_id`)
	if err != nil {
		return nil, err
	}
	var pots []CardPot
	var id int64
	var table int32
	var st CardStake
	_, err = pgx.ForEachRow(rows, []any{&id, &table, &st.PlayerID, &st.Gold}, func() error {
		if n := len(pots); n == 0 || pots[n-1].ID != id {
			pots = append(pots, CardPot{ID: id, Table: table})
		}
		pots[len(pots)-1].Stakes = append(pots[len(pots)-1].Stakes, st)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pots, nil
}

// RefundOpenCardPots refunds one pot per transaction. Players are locked in
// id order, as ApplyEconomy locks them.
func (s *PGPlayers) RefundOpenCardPots(ctx context.Context, entry func(CardPot, CardStake) Entry) ([]CardPot, error) {
	pots, err := s.OpenCardPots(ctx)
	if err != nil {
		return nil, fmt.Errorf("refund card pots: %w", err)
	}
	var done []CardPot
	for _, pot := range pots {
		var refunded CardPot
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockOpenPot(ctx, tx, pot.ID); err != nil {
				if errors.Is(err, ErrNotFound) {
					return nil // closed since it was listed
				}
				return err
			}
			rows, err := tx.Query(ctx,
				`UPDATE card_pot_stakes SET refunded_at = now()
				 WHERE pot_id = $1 AND refunded_at IS NULL RETURNING player_id, stake`, pot.ID)
			if err != nil {
				return fmt.Errorf("stakes: %w", err)
			}
			refunded = CardPot{ID: pot.ID, Table: pot.Table}
			var st CardStake
			if _, err := pgx.ForEachRow(rows, []any{&st.PlayerID, &st.Gold}, func() error {
				refunded.Stakes = append(refunded.Stakes, st)
				return nil
			}); err != nil {
				return fmt.Errorf("stakes: %w", err)
			}
			slices.SortFunc(refunded.Stakes, byPlayer)
			var audit PGAudit
			for _, st := range refunded.Stakes {
				tag, err := tx.Exec(ctx,
					`UPDATE players SET gold = gold + $2, updated_at = now() WHERE id = $1`, st.PlayerID, st.Gold)
				if err != nil {
					return fmt.Errorf("player %d gold: %w", st.PlayerID, classify(err))
				}
				if tag.RowsAffected() == 0 {
					return fmt.Errorf("player %d: %w", st.PlayerID, ErrNotFound)
				}
				if err := audit.Append(ctx, tx, entry(refunded, st)); err != nil {
					return classify(err)
				}
			}
			return closeIfRefunded(ctx, tx, pot.ID)
		})
		if err != nil {
			return done, fmt.Errorf("refund card pot %d: %w", pot.ID, err)
		}
		if len(refunded.Stakes) > 0 {
			done = append(done, refunded)
		}
	}
	return done, nil
}
