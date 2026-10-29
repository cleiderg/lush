package store_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
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
	a, _ := s.CreateGuest(ctx, "a", store.Holdings{}, nil)
	b, _ := s.CreateGuest(ctx, "b", store.Holdings{}, nil)

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

// TestPGCreateGuestAudit checks that CreateGuest logs its entries against the
// new player and that a failure partway through leaves no rows behind.
func TestPGCreateGuestAudit(t *testing.T) {
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
	counts := func() (players, items, audit int) {
		err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM players),
			(SELECT count(*) FROM player_inventory) + (SELECT count(*) FROM player_equipment),
			(SELECT count(*) FROM economy_audit)`).Scan(&players, &items, &audit)
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	// The player row and its gold are written before the bad slot fails.
	bad := storetest.SampleHoldings()
	bad.Inventory[27] = store.Stack{Item: 2, Qty: -1}
	if _, err := s.CreateGuest(ctx, "h1", bad, []store.Entry{{Kind: "item_create"}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if p, i, a := counts(); p != 0 || i != 0 || a != 0 {
		t.Fatalf("after failed create: %d players, %d items, %d audit rows; want none", p, i, a)
	}

	p, err := s.CreateGuest(ctx, "h1", storetest.SampleHoldings(), []store.Entry{
		{Kind: "item_create", Payload: map[string]any{"item": 1}}, {Kind: "gold", Payload: map[string]any{"gold": 100}}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, "SELECT kind, actor_player_id FROM economy_audit ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var kind string
	var actor int64
	if _, err := pgx.ForEachRow(rows, []any{&kind, &actor}, func() error {
		if actor != p.ID {
			t.Errorf("audit %s actor = %d, want %d", kind, actor, p.ID)
		}
		kinds = append(kinds, kind)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != "item_create" || kinds[1] != "gold" {
		t.Fatalf("audit kinds = %v, want [item_create gold]", kinds)
	}
}

func TestPGCardPots(t *testing.T) {
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
	storetest.RunCardPots(t, func(t *testing.T) (storetest.CardPotStore, func() []store.Entry) {
		if _, err := pool.Exec(ctx, "TRUNCATE players, card_pots RESTART IDENTITY CASCADE"); err != nil {
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

// TestPGCardPotAtomic forces failures after the pot, stake, holdings and
// audit rows are written and checks that no row of any table changed.
func TestPGCardPotAtomic(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "TRUNCATE players, card_pots RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	s := store.NewPGPlayers(pool)
	a, _ := s.CreateGuest(ctx, "a", store.Holdings{Gold: 100}, nil)
	b, _ := s.CreateGuest(ctx, "b", store.Holdings{Gold: 100}, nil)
	snapshot := func() string {
		var out string
		err := pool.QueryRow(ctx, `SELECT concat_ws('|',
			(SELECT string_agg(concat_ws(',', id, gold, version, updated_at), ';' ORDER BY id) FROM players),
			(SELECT string_agg(concat_ws(',', id, kind, actor_player_id, payload), ';' ORDER BY id) FROM economy_audit),
			(SELECT string_agg(concat_ws(',', id, table_id, closed_at, closed_by), ';' ORDER BY id) FROM card_pots),
			(SELECT string_agg(concat_ws(',', pot_id, player_id, stake, refunded_at), ';' ORDER BY pot_id, player_id) FROM card_pot_stakes))`).Scan(&out)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	stakes := []store.CardStake{{PlayerID: a.ID, Gold: 25}, {PlayerID: b.ID, Gold: 25}}
	staked := []store.EconomyChange{{PlayerID: a.ID, Holdings: store.Holdings{Gold: 75}}, {PlayerID: b.ID, Holdings: store.Holdings{Gold: 75}}}
	badEntries := []store.Entry{{Kind: "gold", Actor: a.ID}, {Kind: "gold", Actor: b.ID + 1000}}

	before := snapshot()
	if _, err := s.OpenCardPot(ctx, 1, stakes, staked, badEntries); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("open: err = %v, want ErrNotFound", err)
	}
	if after := snapshot(); after != before {
		t.Fatalf("failed stake commit wrote:\nbefore %s\nafter  %s", before, after)
	}
	var pots int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM card_pots").Scan(&pots); err != nil || pots != 0 {
		t.Fatalf("%d card pots after a failed stake commit, err %v", pots, err)
	}

	pot, err := s.OpenCardPot(ctx, 1, stakes, staked, []store.Entry{{Kind: "gold", Actor: a.ID}, {Kind: "gold", Actor: b.ID}})
	if err != nil {
		t.Fatal(err)
	}
	before = snapshot()
	if err := s.PayCardPot(ctx, pot, []store.EconomyChange{{PlayerID: a.ID, Holdings: store.Holdings{Gold: 125}}}, badEntries); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pay: err = %v, want ErrNotFound", err)
	}
	if after := snapshot(); after != before {
		t.Fatalf("failed payout wrote:\nbefore %s\nafter  %s", before, after)
	}
	var open bool
	if err := pool.QueryRow(ctx, "SELECT closed_at IS NULL FROM card_pots WHERE id = $1", pot).Scan(&open); err != nil || !open {
		t.Fatalf("pot open after failed payout = %v, err %v", open, err)
	}
}
