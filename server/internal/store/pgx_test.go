package store_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/store/storetest"
)

func TestPGPlayers(t *testing.T) {
	url := os.Getenv("LUSH_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LUSH_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	storetest.RunPlayers(t, func(t *testing.T) store.Players {
		if _, err := pool.Exec(ctx, "TRUNCATE players RESTART IDENTITY CASCADE"); err != nil {
			t.Fatal(err)
		}
		return store.NewPGPlayers(pool)
	})
}

// TestPGApplyEconomyAtomic forces failures partway through an economy write and
// checks that no player, item or audit row changed.
func TestPGApplyEconomyAtomic(t *testing.T) {
	url := os.Getenv("LUSH_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LUSH_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE players RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	s := store.NewPGPlayers(pool)
	a, _ := s.CreateGuest(ctx, "a")
	b, _ := s.CreateGuest(ctx, "b")

	// snapshot renders every row the economy write can touch.
	snapshot := func() string {
		var out string
		err := pool.QueryRow(ctx, `SELECT concat_ws('|',
			(SELECT string_agg(concat_ws(',', id, gold, combat_xp, version, updated_at), ';' ORDER BY id) FROM players),
			(SELECT string_agg(concat_ws(',', player_id, slot, item_id, quantity), ';' ORDER BY player_id, slot) FROM player_inventory),
			(SELECT string_agg(concat_ws(',', player_id, slot, item_id), ';' ORDER BY player_id, slot) FROM player_equipment),
			(SELECT string_agg(concat_ws(',', id, kind, actor_player_id, counterparty_player_id, payload), ';' ORDER BY id) FROM economy_audit))`).Scan(&out)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	auditRows := func() int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM economy_audit").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	first := storetest.SampleHoldings()
	if err := s.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: a.ID, Holdings: first}},
		[]store.Entry{{Kind: "item_create", Actor: a.ID}, {Kind: "gold_create", Actor: a.ID, Payload: map[string]any{"gold": 100}}}); err != nil {
		t.Fatal(err)
	}
	if n := auditRows(); n != 2 {
		t.Fatalf("audit rows = %d, want 2", n)
	}
	var kind string
	var cp *int64
	if err := pool.QueryRow(ctx, "SELECT kind, counterparty_player_id FROM economy_audit ORDER BY id LIMIT 1").Scan(&kind, &cp); err != nil || kind != "item_create" || cp != nil {
		t.Fatalf("first audit row = %q %v, err %v", kind, cp, err)
	}

	before := snapshot()
	moved := first
	moved.Gold = 50
	moved.Inventory[1] = store.Stack{Item: 3, Qty: 1}
	bad := first
	bad.Inventory[2] = store.Stack{Item: 3, Qty: -1}

	for _, tc := range []struct {
		name    string
		changes []store.EconomyChange
		entries []store.Entry
		wantErr error
	}{
		// a sorts first, so its update runs before b's insert fails.
		{"negative quantity on second player",
			[]store.EconomyChange{{PlayerID: b.ID, Holdings: bad}, {PlayerID: a.ID, Holdings: moved}},
			[]store.Entry{{Kind: "trade", Actor: a.ID, Counterparty: b.ID}}, store.ErrInvalid},
		// Both players and the first audit entry are written before the
		// second entry fails on its foreign key.
		{"unknown audit actor after writes",
			[]store.EconomyChange{{PlayerID: a.ID, Holdings: moved}, {PlayerID: b.ID, Holdings: first}},
			[]store.Entry{{Kind: "trade", Actor: a.ID, Counterparty: b.ID}, {Kind: "trade", Actor: b.ID + 1000}}, store.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.ApplyEconomy(ctx, tc.changes, tc.entries); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if after := snapshot(); after != before {
				t.Fatalf("rows changed:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}
