package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/store/storetest"
)

func TestPGQuests(t *testing.T) {
	pool := bankPool(t)
	ctx := context.Background()
	storetest.RunQuests(t, func(t *testing.T) (storetest.QuestStore, func() []store.Entry) {
		if _, err := pool.Exec(ctx, "TRUNCATE players RESTART IDENTITY CASCADE"); err != nil {
			t.Fatal(err)
		}
		return store.NewPGPlayers(pool), func() []store.Entry {
			rows, err := pool.Query(ctx, `SELECT kind, actor_player_id FROM economy_audit ORDER BY id`)
			if err != nil {
				t.Fatal(err)
			}
			var out []store.Entry
			var e store.Entry
			if _, err := pgx.ForEachRow(rows, []any{&e.Kind, &e.Actor}, func() error {
				out = append(out, e)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			return out
		}
	})
}

// TestPGCompleteQuestAuditFailureRollsBack makes the audit insert fail after
// the quest row, points, XP and holdings are written, and checks none stuck.
func TestPGCompleteQuestAuditFailureRollsBack(t *testing.T) {
	pool := bankPool(t)
	ctx := context.Background()
	s := store.NewPGPlayers(pool)
	p, err := s.CreateGuest(ctx, "a", store.Holdings{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE economy_audit ADD CONSTRAINT test_no_quest CHECK (kind NOT LIKE 'quest%') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `ALTER TABLE economy_audit DROP CONSTRAINT IF EXISTS test_no_quest`)
	})
	if _, err := s.CompleteQuest(ctx, store.QuestCompletion{PlayerID: p.ID, Quest: "q", Points: 1, Gold: 5, XP: 9}); err == nil {
		t.Fatal("completion with failing audit succeeded")
	}
	var gold, points, xp, rows int64
	if err := pool.QueryRow(ctx, `SELECT gold, quest_points, combat_xp, (SELECT count(*) FROM player_quests) FROM players WHERE id = $1`, p.ID).
		Scan(&gold, &points, &xp, &rows); err != nil {
		t.Fatal(err)
	}
	if gold != 0 || points != 0 || xp != 0 || rows != 0 {
		t.Fatalf("after failed completion: gold %d points %d xp %d quest rows %d; want all 0", gold, points, xp, rows)
	}
}
