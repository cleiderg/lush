package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/store"
)

func TestMemoryAuditOrder(t *testing.T) {
	var a store.MemoryAudit
	for _, k := range []string{"a", "b", "c"} {
		if err := a.Append(context.Background(), nil, store.Entry{Kind: k, Actor: 1}); err != nil {
			t.Fatal(err)
		}
	}
	got := a.Entries()
	if len(got) != 3 || got[0].Kind != "a" || got[1].Kind != "b" || got[2].Kind != "c" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestPGAudit(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS player_equipment, player_inventory, economy_audit, players, goose_db_version CASCADE"); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var actor, other int64
	const ins = "INSERT INTO players (token_hash) VALUES ($1) RETURNING id"
	if err := pool.QueryRow(ctx, ins, []byte("a")).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, ins, []byte("b")).Scan(&other); err != nil {
		t.Fatal(err)
	}
	count := func(kind string) int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM economy_audit WHERE kind = $1", kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var audit store.PGAudit

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Append(ctx, tx, store.Entry{Kind: "committed", Actor: actor, Counterparty: other, Payload: map[string]any{"item": 7}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count("committed"); n != 1 {
		t.Fatalf("committed entries = %d, want 1", n)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Append(ctx, tx, store.Entry{Kind: "rolledback", Actor: actor}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count("rolledback"); n != 0 {
		t.Fatalf("rolled back entries = %d, want 0", n)
	}

	if err := audit.Append(ctx, pool, store.Entry{Kind: "pool", Actor: actor}); err != nil {
		t.Fatalf("append via pool: %v", err)
	}
	if err := audit.Append(ctx, pool, store.Entry{Kind: "bad", Actor: actor + 1000}); err == nil {
		t.Fatal("unknown actor accepted")
	}
}
