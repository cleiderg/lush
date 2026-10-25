package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/cleiderg/lush/db/migrations"
	"github.com/cleiderg/lush/server/internal/store"
)

// latestMigration is the version of the newest file in db/migrations.
const latestMigration = 3

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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS player_equipment, player_inventory, economy_audit, players, goose_db_version CASCADE"); err != nil {
		t.Fatal(err)
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate should be a no-op: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id > 0").Scan(&n); err != nil || n != latestMigration {
		t.Fatalf("applied migrations = %d, err %v; want %d", n, err, latestMigration)
	}

	const ins = "INSERT INTO players (token_hash) VALUES ($1)"
	if _, err := pool.Exec(ctx, ins, []byte("hash-a")); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := pool.Exec(ctx, ins, []byte("hash-a")); err == nil {
		t.Fatal("duplicate token hash insert succeeded")
	}
}

// TestMigrateProgressDownUp rolls the newest migration back to the previous
// version and applies it again, then checks its constraints.
func TestMigrateProgressDownUp(t *testing.T) {
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
		t.Fatalf("up: %v", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, latestMigration-1); err != nil {
		t.Fatalf("down to %d: %v", latestMigration-1, err)
	}
	if v, err := provider.GetDBVersion(ctx); err != nil || v != latestMigration-1 {
		t.Fatalf("version after down = %d, err %v; want %d", v, err, latestMigration-1)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('player_inventory') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("player_inventory exists after down = %v, err %v", exists, err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if v, err := provider.GetDBVersion(ctx); err != nil || v != latestMigration {
		t.Fatalf("version after up again = %d, err %v; want %d", v, err, latestMigration)
	}

	var id int64
	if err := pool.QueryRow(ctx, "INSERT INTO players (token_hash) VALUES ('p') RETURNING id").Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		sql  string
		ok   bool
	}{
		{"gold zero", "UPDATE players SET gold = 0 WHERE id = $1", true},
		{"gold negative", "UPDATE players SET gold = -1 WHERE id = $1", false},
		{"xp negative", "UPDATE players SET combat_xp = -1 WHERE id = $1", false},
		{"slot 0", "INSERT INTO player_inventory VALUES ($1, 0, 1, 1)", true},
		{"slot 27", "INSERT INTO player_inventory VALUES ($1, 27, 1, 5)", true},
		{"slot 28", "INSERT INTO player_inventory VALUES ($1, 28, 1, 1)", false},
		{"slot -1", "INSERT INTO player_inventory VALUES ($1, -1, 1, 1)", false},
		{"quantity negative", "INSERT INTO player_inventory VALUES ($1, 5, 1, -1)", false},
		{"quantity zero", "INSERT INTO player_inventory VALUES ($1, 5, 1, 0)", false},
		{"duplicate slot", "INSERT INTO player_inventory VALUES ($1, 0, 2, 1)", false},
		{"weapon", "INSERT INTO player_equipment VALUES ($1, 'weapon', 1)", true},
		{"head", "INSERT INTO player_equipment VALUES ($1, 'head', 1)", true},
		{"body", "INSERT INTO player_equipment VALUES ($1, 'body', 1)", true},
		{"shield", "INSERT INTO player_equipment VALUES ($1, 'shield', 1)", true},
		{"unknown equipment slot", "INSERT INTO player_equipment VALUES ($1, 'ring', 1)", false},
		{"equipment item zero", "UPDATE player_equipment SET item_id = 0 WHERE player_id = $1", false},
	} {
		_, err := pool.Exec(ctx, tc.sql, id)
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: accepted, want rejected", tc.name)
		}
	}
}
