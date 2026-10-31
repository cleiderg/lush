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
const latestMigration = 5

// progressMigration is the version of 00003_player_progress.sql.
const progressMigration = 3

// cardPotsMigration is the version of 00004_card_pots.sql.
const cardPotsMigration = 4

// bankMigration is the version of 00005_player_bank.sql.
const bankMigration = 5

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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS player_bank, card_pot_stakes, card_pots, player_equipment, player_inventory, economy_audit, players, goose_db_version CASCADE"); err != nil {
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

// TestMigrateProgressDownUp rolls the player progress migration back to the
// previous version and applies it again, then checks its constraints.
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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS player_bank, card_pot_stakes, card_pots, player_equipment, player_inventory, economy_audit, players, goose_db_version CASCADE"); err != nil {
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
	if _, err := provider.DownTo(ctx, progressMigration-1); err != nil {
		t.Fatalf("down to %d: %v", progressMigration-1, err)
	}
	if v, err := provider.GetDBVersion(ctx); err != nil || v != progressMigration-1 {
		t.Fatalf("version after down = %d, err %v; want %d", v, err, progressMigration-1)
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

// TestMigrateCardPotsDownUp rolls the card pot migration back and applies it
// again, then checks its constraints.
func TestMigrateCardPotsDownUp(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS player_bank, card_pot_stakes, card_pots, player_equipment, player_inventory, economy_audit, players, goose_db_version CASCADE"); err != nil {
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
	if _, err := provider.DownTo(ctx, cardPotsMigration-1); err != nil {
		t.Fatalf("down to %d: %v", cardPotsMigration-1, err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('card_pots') IS NOT NULL OR to_regclass('card_pot_stakes') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("card pot tables exist after down = %v, err %v", exists, err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}

	var player, pot int64
	if err := pool.QueryRow(ctx, "INSERT INTO players (token_hash) VALUES ('p') RETURNING id").Scan(&player); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO card_pots (table_id) VALUES (1) RETURNING id").Scan(&pot); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		sql  string
		ok   bool
	}{
		{"stake zero", "INSERT INTO card_pot_stakes (pot_id, player_id, stake) VALUES ($1, $2, 0)", false},
		{"stake unknown player", "INSERT INTO card_pot_stakes (pot_id, player_id, stake) VALUES ($1, $2 + 1000, 5)", false},
		{"stake", "INSERT INTO card_pot_stakes (pot_id, player_id, stake) VALUES ($1, $2, 5)", true},
		{"stake twice", "INSERT INTO card_pot_stakes (pot_id, player_id, stake) VALUES ($1, $2, 5)", false},
		{"closed without kind", "UPDATE card_pots SET closed_at = now() WHERE id = $1 AND $2::bigint > 0", false},
		{"kind without closed", "UPDATE card_pots SET closed_by = 'payout' WHERE id = $1 AND $2::bigint > 0", false},
		{"unknown kind", "UPDATE card_pots SET closed_at = now(), closed_by = 'house' WHERE id = $1 AND $2::bigint > 0", false},
		{"closed by refund", "UPDATE card_pots SET closed_at = now(), closed_by = 'refund' WHERE id = $1 AND $2::bigint > 0", true},
	} {
		_, err := pool.Exec(ctx, tc.sql, pot, player)
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: accepted, want rejected", tc.name)
		}
	}
}

// TestMigrateBankDownUp rolls the bank migration back and applies it again,
// then checks its constraints: one positive stack per item per player.
func TestMigrateBankDownUp(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS player_bank, card_pot_stakes, card_pots, player_equipment, player_inventory, economy_audit, players, goose_db_version CASCADE"); err != nil {
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
	if _, err := provider.DownTo(ctx, bankMigration-1); err != nil {
		t.Fatalf("down to %d: %v", bankMigration-1, err)
	}
	if v, err := provider.GetDBVersion(ctx); err != nil || v != bankMigration-1 {
		t.Fatalf("version after down = %d, err %v; want %d", v, err, bankMigration-1)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('player_bank') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("player_bank exists after down = %v, err %v", exists, err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if v, err := provider.GetDBVersion(ctx); err != nil || v != bankMigration {
		t.Fatalf("version after up again = %d, err %v; want %d", v, err, bankMigration)
	}

	var a, b int64
	if err := pool.QueryRow(ctx, "INSERT INTO players (token_hash) VALUES ('a') RETURNING id").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO players (token_hash) VALUES ('b') RETURNING id").Scan(&b); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		sql  string
		ok   bool
	}{
		{"stack", "INSERT INTO player_bank VALUES ($1, 1, 5) RETURNING $2::bigint", true},
		{"any quantity", "INSERT INTO player_bank VALUES ($1, 2, 9000000000000) RETURNING $2::bigint", true},
		{"same item twice", "INSERT INTO player_bank VALUES ($1, 1, 1) RETURNING $2::bigint", false},
		{"same item, other player", "INSERT INTO player_bank VALUES ($2, 1, 1) RETURNING $1::bigint", true},
		{"quantity zero", "INSERT INTO player_bank VALUES ($1, 3, 0) RETURNING $2::bigint", false},
		{"quantity negative", "INSERT INTO player_bank VALUES ($1, 3, -1) RETURNING $2::bigint", false},
		{"item zero", "INSERT INTO player_bank VALUES ($1, 0, 1) RETURNING $2::bigint", false},
		{"unknown player", "INSERT INTO player_bank VALUES ($1 + $2 + 1000, 3, 1)", false},
	} {
		_, err := pool.Exec(ctx, tc.sql, a, b)
		if tc.ok && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: accepted, want rejected", tc.name)
		}
	}
	if _, err := pool.Exec(ctx, "DELETE FROM players WHERE id = $1", a); err != nil {
		t.Fatalf("delete player: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM player_bank WHERE player_id = $1", a).Scan(&n); err != nil || n != 0 {
		t.Fatalf("bank rows of a deleted player = %d, err %v; want 0", n, err)
	}
}
