package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/store"
)

func TestMigrate(t *testing.T) {
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

	// Start from an empty database.
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS players, goose_db_version CASCADE"); err != nil {
		t.Fatal(err)
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate should be a no-op: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id > 0").Scan(&n); err != nil || n != 1 {
		t.Fatalf("applied migrations = %d, err %v; want 1", n, err)
	}

	const ins = "INSERT INTO players (token_hash) VALUES ($1)"
	if _, err := pool.Exec(ctx, ins, []byte("hash-a")); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := pool.Exec(ctx, ins, []byte("hash-a")); err == nil {
		t.Fatal("duplicate token hash insert succeeded")
	}
}
