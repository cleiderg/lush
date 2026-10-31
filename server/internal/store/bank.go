package store

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"
)

// BankItem is one bank slot: every item stacks in the bank, so a player's
// bank holds at most one BankItem per item id (skills spec, Bank).
type BankItem struct {
	Item int32
	Qty  int64
}

// BankMove moves Qty of Item between one inventory or equipment slot and the
// bank.
type BankMove struct {
	// Deposit moves from the slot into the bank; otherwise the move withdraws
	// from the bank into the slot.
	Deposit bool
	// Equipment says Slot is an EquipSlot; otherwise it is an inventory slot.
	Equipment bool
	Slot      int
	Item      int32
	// Qty is positive, and 1 for an equipment slot.
	Qty int64
}

// BankTransfer is one deposit or withdrawal request of one player: every move
// in it is applied in order, in one transaction, or none is.
type BankTransfer struct {
	PlayerID int64
	Moves    []BankMove
	// SlotLimit is the most distinct items the bank may hold after the
	// transfer (game data, 200 in milestone 9). A transfer that leaves more
	// distinct items than this, and more than the bank held before, fails.
	SlotLimit int
}

// BankEntryKinds are the audit kinds a bank transfer writes, one entry per move.
const (
	BankDepositKind  = "bank_deposit"
	BankWithdrawKind = "bank_withdraw"
)

// Bank stores every player's bank.
type Bank interface {
	// LoadBank returns the player's bank by item id, or an error wrapping
	// ErrNotFound for an unknown player.
	LoadBank(ctx context.Context, player int64) ([]BankItem, error)
	// TransferBank re-reads the player's stored inventory, equipment and
	// bank, applies t's moves to them, writes the result and appends one
	// audit entry per move with Actor set to the player, all in one
	// transaction, and returns the new holdings and bank. A move that takes
	// more than a side holds, puts an item in a slot holding another one, or
	// leaves the bank over t.SlotLimit fails the transfer with an error
	// wrapping ErrInvalid (ErrNotFound for an unknown player), and on any
	// error nothing changes.
	TransferBank(ctx context.Context, t BankTransfer) (Holdings, []BankItem, error)
}

var (
	_ Bank = (*Memory)(nil)
	_ Bank = (*PGPlayers)(nil)
)

// applyBankMoves applies t's moves to h and bank, which it changes in place,
// and returns the audit entries. Both stores check a transfer with it, so
// they agree on every rule. On error h and bank may be partly changed; the
// caller discards them.
func applyBankMoves(h *Holdings, bank map[int32]int64, t BankTransfer) ([]Entry, error) {
	if len(t.Moves) == 0 {
		return nil, fmt.Errorf("no moves: %w", ErrInvalid)
	}
	if t.SlotLimit < 0 {
		return nil, fmt.Errorf("slot limit %d: %w", t.SlotLimit, ErrInvalid)
	}
	before := len(bank)
	entries := make([]Entry, 0, len(t.Moves))
	for i, mv := range t.Moves {
		if err := applyBankMove(h, bank, mv); err != nil {
			return nil, fmt.Errorf("move %d: %w", i, err)
		}
		entries = append(entries, bankEntry(t.PlayerID, mv))
	}
	if len(bank) > t.SlotLimit && len(bank) > before {
		return nil, fmt.Errorf("bank holds %d items, limit %d: %w", len(bank), t.SlotLimit, ErrInvalid)
	}
	return entries, nil
}

// applyBankMove applies one move, checking both sides.
func applyBankMove(h *Holdings, bank map[int32]int64, mv BankMove) error {
	if mv.Item <= 0 || mv.Qty <= 0 {
		return fmt.Errorf("item %d qty %d: %w", mv.Item, mv.Qty, ErrInvalid)
	}
	if mv.Equipment {
		if mv.Slot < 0 || mv.Slot >= int(EquipSlots) {
			return fmt.Errorf("equipment slot %d: %w", mv.Slot, ErrInvalid)
		}
		if mv.Qty != 1 {
			return fmt.Errorf("equipment slot %s qty %d: %w", EquipSlot(mv.Slot), mv.Qty, ErrInvalid)
		}
		held := &h.Equipment[mv.Slot]
		if mv.Deposit {
			if *held != mv.Item {
				return fmt.Errorf("equipment slot %s holds item %d, not %d: %w", EquipSlot(mv.Slot), *held, mv.Item, ErrInvalid)
			}
			*held = 0
			return bankAdd(bank, mv.Item, 1)
		}
		if *held != 0 {
			return fmt.Errorf("equipment slot %s holds item %d: %w", EquipSlot(mv.Slot), *held, ErrInvalid)
		}
		if err := bankTake(bank, mv.Item, 1); err != nil {
			return err
		}
		*held = mv.Item
		return nil
	}

	if mv.Slot < 0 || mv.Slot >= InventorySlots {
		return fmt.Errorf("inventory slot %d: %w", mv.Slot, ErrInvalid)
	}
	st := &h.Inventory[mv.Slot]
	if mv.Deposit {
		if st.Item != mv.Item || st.Qty < mv.Qty {
			return fmt.Errorf("inventory slot %d holds %d of item %d, not %d of %d: %w",
				mv.Slot, st.Qty, st.Item, mv.Qty, mv.Item, ErrInvalid)
		}
		if st.Qty -= mv.Qty; st.Qty == 0 {
			*st = Stack{}
		}
		return bankAdd(bank, mv.Item, mv.Qty)
	}
	if *st != (Stack{}) && st.Item != mv.Item {
		return fmt.Errorf("inventory slot %d holds item %d: %w", mv.Slot, st.Item, ErrInvalid)
	}
	if st.Qty > math.MaxInt64-mv.Qty {
		return fmt.Errorf("inventory slot %d overflows: %w", mv.Slot, ErrInvalid)
	}
	if err := bankTake(bank, mv.Item, mv.Qty); err != nil {
		return err
	}
	st.Item = mv.Item
	st.Qty += mv.Qty
	return nil
}

func bankAdd(bank map[int32]int64, item int32, qty int64) error {
	if bank[item] > math.MaxInt64-qty {
		return fmt.Errorf("bank item %d overflows: %w", item, ErrInvalid)
	}
	bank[item] += qty
	return nil
}

func bankTake(bank map[int32]int64, item int32, qty int64) error {
	have := bank[item]
	if have < qty {
		return fmt.Errorf("bank holds %d of item %d, not %d: %w", have, item, qty, ErrInvalid)
	}
	if have == qty {
		delete(bank, item)
	} else {
		bank[item] = have - qty
	}
	return nil
}

// bankEntry is the audit entry for one move.
func bankEntry(player int64, mv BankMove) Entry {
	kind := BankWithdrawKind
	if mv.Deposit {
		kind = BankDepositKind
	}
	payload := map[string]any{"item": mv.Item, "qty": mv.Qty}
	if mv.Equipment {
		payload["equipment_slot"] = EquipSlot(mv.Slot).String()
	} else {
		payload["inventory_slot"] = mv.Slot
	}
	return Entry{Kind: kind, Actor: player, Payload: payload}
}

// bankItems returns bank as BankItems by item id.
func bankItems(bank map[int32]int64) []BankItem {
	out := make([]BankItem, 0, len(bank))
	for _, item := range slices.Sorted(maps.Keys(bank)) {
		out = append(out, BankItem{Item: item, Qty: bank[item]})
	}
	return out
}

// ---- Memory ----

func (m *Memory) LoadBank(ctx context.Context, player int64) ([]BankItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load bank %d: %w", player, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[player]; !ok {
		return nil, fmt.Errorf("load bank %d: %w", player, ErrNotFound)
	}
	return bankItems(m.banks[player]), nil
}

// TransferBank applies the moves to copies and stores them only if every
// check passes, so a failure leaves the store as it was.
func (m *Memory) TransferBank(ctx context.Context, t BankTransfer) (Holdings, []BankItem, error) {
	if err := ctx.Err(); err != nil {
		return Holdings{}, nil, fmt.Errorf("bank transfer %d: %w", t.PlayerID, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byID[t.PlayerID]
	if !ok {
		return Holdings{}, nil, fmt.Errorf("bank transfer %d: %w", t.PlayerID, ErrNotFound)
	}
	h := p.Holdings
	bank := maps.Clone(m.banks[t.PlayerID])
	if bank == nil {
		bank = map[int32]int64{}
	}
	entries, err := applyBankMoves(&h, bank, t)
	if err != nil {
		return Holdings{}, nil, fmt.Errorf("bank transfer %d: %w", t.PlayerID, err)
	}
	if m.banks == nil {
		m.banks = map[int64]map[int32]int64{}
	}
	m.banks[t.PlayerID] = bank
	m.applyEconomyLocked([]EconomyChange{{PlayerID: t.PlayerID, Holdings: h}}, entries)
	return h, bankItems(bank), nil
}

// ---- Postgres ----

func (s *PGPlayers) LoadBank(ctx context.Context, player int64) ([]BankItem, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("load bank %d: %w", player, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only; nothing to undo

	var one int
	err = tx.QueryRow(ctx, `SELECT 1 FROM players WHERE id = $1`, player).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load bank %d: %w", player, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("load bank %d: %w", player, err)
	}
	bank, err := loadBank(ctx, tx, player)
	if err != nil {
		return nil, fmt.Errorf("load bank %d: %w", player, err)
	}
	return bankItems(bank), nil
}

// loadBank reads player's bank rows.
func loadBank(ctx context.Context, q querier, player int64) (map[int32]int64, error) {
	rows, err := q.Query(ctx, `SELECT item_id, quantity FROM player_bank WHERE player_id = $1`, player)
	if err != nil {
		return nil, fmt.Errorf("bank: %w", err)
	}
	bank := map[int32]int64{}
	var it BankItem
	if _, err := pgx.ForEachRow(rows, []any{&it.Item, &it.Qty}, func() error {
		bank[it.Item] = it.Qty
		return nil
	}); err != nil {
		return nil, fmt.Errorf("bank: %w", err)
	}
	return bank, nil
}

// TransferBank locks the player row, so concurrent transfers and economy
// writes for the player run one after another, then re-reads the stored
// holdings and bank, applies the moves and writes the holdings, the changed
// bank rows and the audit entries, all in one transaction.
func (s *PGPlayers) TransferBank(ctx context.Context, t BankTransfer) (Holdings, []BankItem, error) {
	var h Holdings
	var bank map[int32]int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		h = Holdings{}
		err := tx.QueryRow(ctx, `SELECT gold FROM players WHERE id = $1 FOR UPDATE`, t.PlayerID).Scan(&h.Gold)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock player: %w", err)
		}
		if err := loadItems(ctx, tx, t.PlayerID, &h); err != nil {
			return err
		}
		if bank, err = loadBank(ctx, tx, t.PlayerID); err != nil {
			return err
		}
		old := maps.Clone(bank)
		entries, err := applyBankMoves(&h, bank, t)
		if err != nil {
			return err
		}
		if err := writeBank(ctx, tx, t.PlayerID, old, bank); err != nil {
			return classify(err)
		}
		return writeEconomy(ctx, tx, []EconomyChange{{PlayerID: t.PlayerID, Holdings: h}}, entries)
	})
	if err != nil {
		return Holdings{}, nil, fmt.Errorf("bank transfer %d: %w", t.PlayerID, err)
	}
	return h, bankItems(bank), nil
}

// writeBank writes the bank rows that differ between old and bank.
func writeBank(ctx context.Context, tx pgx.Tx, player int64, old, bank map[int32]int64) error {
	var gone, items []int32
	var qtys []int64
	for item := range old {
		if _, ok := bank[item]; !ok {
			gone = append(gone, item)
		}
	}
	for item, qty := range bank {
		if old[item] != qty {
			items = append(items, item)
			qtys = append(qtys, qty)
		}
	}
	slices.Sort(gone)
	if len(gone) > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM player_bank WHERE player_id = $1 AND item_id = ANY($2::integer[])`, player, gone); err != nil {
			return fmt.Errorf("bank: %w", err)
		}
	}
	if len(items) > 0 {
		if _, err := tx.Exec(ctx,
			`INSERT INTO player_bank (player_id, item_id, quantity)
			 SELECT $1::bigint, i, q FROM unnest($2::integer[], $3::bigint[]) AS t (i, q)
			 ON CONFLICT (player_id, item_id) DO UPDATE SET quantity = EXCLUDED.quantity`,
			player, items, qtys); err != nil {
			return fmt.Errorf("bank: %w", err)
		}
	}
	return nil
}
