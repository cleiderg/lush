package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/store/storetest"
)

func TestMemoryHousing(t *testing.T) {
	storetest.RunHousing(t, func(*testing.T) (storetest.HousingStore, func() []store.Entry) {
		m := store.NewMemory()
		return m, func() []store.Entry {
			out := m.AuditEntries()
			for i := range out {
				out[i].Payload = nil
			}
			return out
		}
	})
}

func TestPGHousing(t *testing.T) {
	pool := bankPool(t)
	ctx := context.Background()
	storetest.RunHousing(t, func(t *testing.T) (storetest.HousingStore, func() []store.Entry) {
		if _, err := pool.Exec(ctx, "TRUNCATE players, housing_areas RESTART IDENTITY CASCADE"); err != nil {
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
