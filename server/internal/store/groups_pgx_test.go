package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/store"
)

var groupT0 = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

// groupFixture returns a store over a clean database and n players with gold.
func groupFixture(t *testing.T, gold int64, n int) (*store.PGPlayers, *pgxpool.Pool, []int64) {
	t.Helper()
	pool := bankPool(t)
	if _, err := pool.Exec(context.Background(), "TRUNCATE groups RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	s := store.NewPGPlayers(pool)
	ids := make([]int64, n)
	for i := range ids {
		p, err := s.CreateGuest(context.Background(), string(rune('a'+i)), store.Holdings{Gold: gold}, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = p.ID
	}
	return s, pool, ids
}

func goldOf(t *testing.T, pool *pgxpool.Pool, id int64) int64 {
	t.Helper()
	var g int64
	if err := pool.QueryRow(context.Background(), `SELECT gold FROM players WHERE id = $1`, id).Scan(&g); err != nil {
		t.Fatal(err)
	}
	return g
}

func auditCount(t *testing.T, pool *pgxpool.Pool, kind string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM economy_audit WHERE kind = $1`, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPGFoundGroup(t *testing.T) {
	s, pool, ids := groupFixture(t, 600, 3)
	ctx := context.Background()
	found := func(p int64, name string) (store.Group, error) {
		return s.FoundGroup(ctx, store.FoundGroup{Founder: p, Name: name, Banner: 3, Cost: store.GroupFoundCost, Now: groupT0})
	}

	g, err := found(ids[0], "Anteaters")
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "Anteaters" || g.Banner != 3 || g.VaultGold != 0 || g.Leader() != ids[0] || len(g.Members) != 1 {
		t.Fatalf("group = %+v", g)
	}
	if g.Members[0].Rank != store.RankLeader || !g.Members[0].JoinedAt.Equal(groupT0) || !g.Members[0].RankSince.Equal(groupT0) {
		t.Fatalf("founder = %+v", g.Members[0])
	}
	if got := goldOf(t, pool, ids[0]); got != 100 {
		t.Fatalf("founder gold = %d, want 100", got)
	}
	if n := auditCount(t, pool, store.GroupFoundKind); n != 1 {
		t.Fatalf("found audit rows = %d, want 1", n)
	}

	// A retried found by the same player fails and charges nothing.
	if _, err := found(ids[0], "Anteaters II"); !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("retried found err = %v, want ErrAlreadyExists", err)
	}
	if got := goldOf(t, pool, ids[0]); got != 100 {
		t.Fatalf("gold after retry = %d, want 100", got)
	}

	// A taken name (any case) changes nothing.
	if _, err := found(ids[1], "ANTEATERS"); !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("taken name err = %v, want ErrAlreadyExists", err)
	}
	if got := goldOf(t, pool, ids[1]); got != 600 {
		t.Fatalf("gold after taken name = %d, want 600", got)
	}

	// Too little gold changes nothing.
	if _, err := pool.Exec(ctx, `UPDATE players SET gold = 499 WHERE id = $1`, ids[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := found(ids[2], "Poor"); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("poor founder err = %v, want ErrInvalid", err)
	}
	if got := goldOf(t, pool, ids[2]); got != 499 {
		t.Fatalf("poor founder gold = %d, want 499", got)
	}

	gs, err := s.LoadGroups(ctx)
	if err != nil || len(gs) != 1 {
		t.Fatalf("groups = %+v, err %v; want 1", gs, err)
	}
	if n := auditCount(t, pool, store.GroupFoundKind); n != 1 {
		t.Fatalf("found audit rows = %d, want 1", n)
	}
}

func TestPGDisbandGroup(t *testing.T) {
	s, pool, ids := groupFixture(t, 600, 2)
	ctx := context.Background()
	g, err := s.FoundGroup(ctx, store.FoundGroup{Founder: ids[0], Name: "Anteaters", Banner: 1, Cost: store.GroupFoundCost, Now: groupT0})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.JoinGroup(ctx, g.ID, ids[1], groupT0); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE groups SET vault_gold = 1234 WHERE group_id = $1`, g.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.DisbandGroup(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	if got := goldOf(t, pool, ids[0]); got != 100+1234 {
		t.Fatalf("leader gold = %d, want 1334", got)
	}
	if n := auditCount(t, pool, store.GroupDisbandKind); n != 1 {
		t.Fatalf("disband audit rows = %d, want 1", n)
	}
	var groups, members int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM groups), (SELECT count(*) FROM group_members)`).Scan(&groups, &members); err != nil || groups != 0 || members != 0 {
		t.Fatalf("groups %d members %d err %v; want 0 0", groups, members, err)
	}

	// A retried disband moves no gold.
	if err := s.DisbandGroup(ctx, g.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retried disband err = %v, want ErrNotFound", err)
	}
	if got := goldOf(t, pool, ids[0]); got != 1334 {
		t.Fatalf("leader gold after retry = %d, want 1334", got)
	}
	if n := auditCount(t, pool, store.GroupDisbandKind); n != 1 {
		t.Fatalf("disband audit rows after retry = %d, want 1", n)
	}
}

func TestPGGroupMembership(t *testing.T) {
	s, pool, ids := groupFixture(t, 600, 4)
	ctx := context.Background()
	g, err := s.FoundGroup(ctx, store.FoundGroup{Founder: ids[0], Name: "Anteaters", Banner: 1, Cost: store.GroupFoundCost, Now: groupT0})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.FoundGroup(ctx, store.FoundGroup{Founder: ids[3], Name: "Others", Banner: 2, Cost: store.GroupFoundCost, Now: groupT0})
	if err != nil {
		t.Fatal(err)
	}

	// join, rank change with rank-since time.
	t1 := groupT0.Add(time.Hour)
	t2 := groupT0.Add(2 * time.Hour)
	for _, p := range ids[1:3] {
		if err := s.JoinGroup(ctx, g.ID, p, t1); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.JoinGroup(ctx, other.ID, ids[1], t1); !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("second group err = %v, want ErrAlreadyExists", err)
	}
	if err := s.SetGroupRank(ctx, g.ID, ids[1], store.RankOfficer, t2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupRank(ctx, g.ID, ids[1], store.RankLeader, t2); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("set leader err = %v, want ErrInvalid", err)
	}
	if err := s.SetGroupRank(ctx, g.ID, ids[0], store.RankMember, t2); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("demote leader err = %v, want ErrInvalid", err)
	}

	// permissions and last login.
	perms := store.GroupPermissions{MemberKeys: true, OfficerWithdraw: true}
	if err := s.SetGroupPermissions(ctx, g.ID, perms); err != nil {
		t.Fatal(err)
	}
	login := groupT0.Add(48 * time.Hour)
	if err := s.SetLastLogin(ctx, ids[2], login); err != nil {
		t.Fatal(err)
	}
	leadLogin := groupT0.Add(50 * time.Hour)
	if err := s.SetLastLogin(ctx, ids[1], leadLogin); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLastLogin(ctx, ids[3]+100, login); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown player last login err = %v, want ErrNotFound", err)
	}

	// kick with cooldown.
	until := t2.Add(24 * time.Hour)
	if err := s.RemoveGroupMember(ctx, g.ID, ids[2], until); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGroupMember(ctx, g.ID, ids[0], until); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("remove leader err = %v, want ErrInvalid", err)
	}
	if err := s.JoinGroup(ctx, g.ID, ids[2], t2); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("join in cooldown err = %v, want ErrInvalid", err)
	}

	// handover.
	t3 := groupT0.Add(3 * time.Hour)
	if err := s.HandoverGroup(ctx, g.ID, ids[0], ids[1], t3); err != nil {
		t.Fatal(err)
	}
	if err := s.HandoverGroup(ctx, g.ID, ids[0], ids[1], t3); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("repeat handover err = %v, want ErrInvalid", err)
	}

	gs, err := s.LoadGroups(ctx)
	if err != nil || len(gs) != 2 {
		t.Fatalf("groups = %+v, err %v; want 2", gs, err)
	}
	got := gs[0]
	if got.ID != g.ID || got.Permissions != perms || got.Leader() != ids[1] || len(got.Members) != 2 {
		t.Fatalf("group = %+v", got)
	}
	old, lead := got.Members[0], got.Members[1] // ordered by player id
	if old.Player != ids[0] || old.Rank != store.RankOfficer || !old.RankSince.Equal(t3) || !old.JoinedAt.Equal(groupT0) {
		t.Fatalf("old leader = %+v", old)
	}
	if lead.Player != ids[1] || lead.Rank != store.RankLeader || !lead.RankSince.Equal(t3) || !lead.JoinedAt.Equal(t1) || !lead.LastLogin.Equal(leadLogin) {
		t.Fatalf("new leader = %+v", lead)
	}
	if gs[1].ID != other.ID || gs[1].Permissions != (store.GroupPermissions{}) {
		t.Fatalf("other group = %+v", gs[1])
	}

	cd, err := s.LoadLeaveCooldowns(ctx)
	if err != nil || len(cd) != 1 || !cd[ids[2]].Equal(until) {
		t.Fatalf("cooldowns = %v, err %v; want %d until %s", cd, err, ids[2], until)
	}
	var stored time.Time
	if err := pool.QueryRow(ctx, `SELECT last_login_at FROM players WHERE id = $1`, ids[2]).Scan(&stored); err != nil || !stored.Equal(login) {
		t.Fatalf("last login = %s, err %v; want %s", stored, err, login)
	}
	if err := s.JoinGroup(ctx, g.ID, ids[2], until); err != nil {
		t.Fatalf("join after cooldown: %v", err)
	}
}

func TestPGGroupFull(t *testing.T) {
	s, _, ids := groupFixture(t, 600, store.GroupMaxMembers+1)
	ctx := context.Background()
	g, err := s.FoundGroup(ctx, store.FoundGroup{Founder: ids[0], Name: "Anteaters", Banner: 1, Cost: store.GroupFoundCost, Now: groupT0})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ids[1:store.GroupMaxMembers] {
		if err := s.JoinGroup(ctx, g.ID, p, groupT0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.JoinGroup(ctx, g.ID, ids[store.GroupMaxMembers], groupT0); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("join full group err = %v, want ErrInvalid", err)
	}
}
