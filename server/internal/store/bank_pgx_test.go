package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/store/storetest"
)

// bankPool returns a migrated, truncated pool on LUSH_TEST_DATABASE_URL, or
// skips the test without it.
func bankPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("LUSH_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LUSH_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE players RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestPGBank(t *testing.T) {
	pool := bankPool(t)
	ctx := context.Background()
	storetest.RunBank(t, func(t *testing.T) (storetest.BankStore, func() []store.Entry) {
		if _, err := pool.Exec(ctx, "TRUNCATE players RESTART IDENTITY CASCADE"); err != nil {
			t.Fatal(err)
		}
		return store.NewPGPlayers(pool), func() []store.Entry {
			rows, err := pool.Query(ctx, `SELECT kind, actor_player_id, coalesce(counterparty_player_id, 0)
				FROM economy_audit ORDER BY id`)
			if err != nil {
				t.Fatal(err)
			}
			var out []store.Entry
			var e store.Entry
			if _, err := pgx.ForEachRow(rows, []any{&e.Kind, &e.Actor, &e.Counterparty}, func() error {
				out = append(out, e)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return out
		}
	})
}

// TestPGBankAuditFailureRollsBack makes the economy_audit insert fail, after
// the bank, inventory and equipment rows are written, and checks that no row
// of any of them changed.
func TestPGBankAuditFailureRollsBack(t *testing.T) {
	pool := bankPool(t)
	ctx := context.Background()
	s := store.NewPGPlayers(pool)
	var h store.Holdings
	h.Inventory[0] = store.Stack{Item: 1, Qty: 10}
	h.Equipment[store.EquipWeapon] = 7
	p, err := s.CreateGuest(ctx, "a", h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 200,
		Moves: []store.BankMove{{Deposit: true, Slot: 0, Item: 1, Qty: 4}}}); err != nil {
		t.Fatal(err)
	}

	snapshot := func() string {
		var out string
		err := pool.QueryRow(ctx, `SELECT concat_ws('|',
			(SELECT string_agg(concat_ws(',', id, gold, version, updated_at), ';' ORDER BY id) FROM players),
			(SELECT string_agg(concat_ws(',', player_id, item_id, quantity), ';' ORDER BY player_id, item_id) FROM player_bank),
			(SELECT string_agg(concat_ws(',', player_id, slot, item_id, quantity), ';' ORDER BY player_id, slot) FROM player_inventory),
			(SELECT string_agg(concat_ws(',', player_id, slot, item_id), ';' ORDER BY player_id, slot) FROM player_equipment),
			(SELECT string_agg(concat_ws(',', id, kind, actor_player_id, payload), ';' ORDER BY id) FROM economy_audit))`).Scan(&out)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Reject every bank audit row from here on.
	const drop = "ALTER TABLE economy_audit DROP CONSTRAINT IF EXISTS test_no_bank_audit"
	if _, err := pool.Exec(ctx, drop); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		"ALTER TABLE economy_audit ADD CONSTRAINT test_no_bank_audit CHECK (kind NOT LIKE 'bank%') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), drop) })

	before := snapshot()
	for _, tc := range []struct {
		name  string
		moves []store.BankMove
	}{
		{"deposit", []store.BankMove{
			{Deposit: true, Slot: 0, Item: 1, Qty: 6},
			{Deposit: true, Equipment: true, Slot: int(store.EquipWeapon), Item: 7, Qty: 1},
		}},
		{"withdraw", []store.BankMove{{Slot: 3, Item: 1, Qty: 4}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 200, Moves: tc.moves})
			if err == nil {
				t.Fatal("transfer succeeded with the audit insert failing")
			}
			if after := snapshot(); after != before {
				t.Fatalf("rows changed:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

// TestPGBankConcurrentWithdraw races two withdrawals of the same bank stack
// and checks that the item total across bank and inventory never changes:
// when both can't be paid, exactly one commits.
func TestPGBankConcurrentWithdraw(t *testing.T) {
	pool := bankPool(t)
	ctx := context.Background()
	s := store.NewPGPlayers(pool)
	const total = 10
	for round := range 20 {
		// Odd rounds take 7 twice (only one fits), even rounds 5 twice (both fit).
		take := int64(7)
		if round%2 == 0 {
			take = 5
		}
		var h store.Holdings
		h.Inventory[0] = store.Stack{Item: 1, Qty: total}
		p, err := s.CreateGuest(ctx, fmt.Sprintf("race-%d", round), h, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 200,
			Moves: []store.BankMove{{Deposit: true, Slot: 0, Item: 1, Qty: total}}}); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, _, errs[i] = s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 200,
					Moves: []store.BankMove{{Slot: 5 + i, Item: 1, Qty: take}}})
			}()
		}
		close(start)
		wg.Wait()

		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case !errors.Is(err, store.ErrInvalid):
				t.Fatalf("round %d: unexpected error %v", round, err)
			}
		}
		wantOK := 1
		if 2*take <= total {
			wantOK = 2
		}
		if ok != wantOK {
			t.Fatalf("round %d: %d withdrawals of %d committed, want %d (errs %v)", round, ok, take, wantOK, errs)
		}

		var banked, held, audits int64
		if err := pool.QueryRow(ctx, `SELECT
			coalesce((SELECT sum(quantity) FROM player_bank WHERE player_id = $1 AND item_id = 1), 0)::bigint,
			coalesce((SELECT sum(quantity) FROM player_inventory WHERE player_id = $1 AND item_id = 1), 0)::bigint,
			(SELECT count(*) FROM economy_audit WHERE actor_player_id = $1 AND kind = $2)`,
			p.ID, store.BankWithdrawKind).Scan(&banked, &held, &audits); err != nil {
			t.Fatal(err)
		}
		if banked+held != total || held != int64(ok)*take || audits != int64(ok) {
			t.Fatalf("round %d: banked %d + held %d (want total %d, held %d), %d withdraw audit rows (want %d)",
				round, banked, held, total, int64(ok)*take, audits, ok)
		}
	}
}
