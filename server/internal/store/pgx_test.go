package store_test

import (
	"context"
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
		if _, err := pool.Exec(ctx, "TRUNCATE players RESTART IDENTITY"); err != nil {
			t.Fatal(err)
		}
		return store.NewPGPlayers(pool)
	})
}
