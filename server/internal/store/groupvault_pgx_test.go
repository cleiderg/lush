package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cleiderg/lush/server/internal/store"
)

func vaultFixture(t *testing.T, gold int64, n int) (*store.PGPlayers, *pgxpool.Pool, int64, []int64) {
	t.Helper()
	s, pool, ids := groupFixture(t, gold+store.GroupFoundCost, n)
	g, err := s.FoundGroup(context.Background(), store.FoundGroup{Founder: ids[0], Name: "Anteaters", Banner: 1, Cost: store.GroupFoundCost, Now: groupT0})
	if err != nil {
		t.Fatal(err)
	}
	return s, pool, g.ID, ids
}

func vaultOf(t *testing.T, s *store.PGPlayers, group int64) int64 {
	t.Helper()
	gs, err := s.LoadGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gs {
		if g.ID == group {
			return g.VaultGold
		}
	}
	t.Fatalf("group %d missing", group)
	return 0
}

func TestPGGroupVaultMoves(t *testing.T) {
	s, pool, group, ids := vaultFixture(t, 100, 2)
	ctx := context.Background()
	p := ids[0]

	if v, err := s.DepositGroupVault(ctx, group, p, 60); err != nil || v != 60 {
		t.Fatalf("deposit = %d, %v", v, err)
	}
	if goldOf(t, pool, p) != 40 || vaultOf(t, s, group) != 60 || auditCount(t, pool, store.GroupVaultDepositKind) != 1 {
		t.Fatal("deposit did not move gold with one audit row")
	}
	if v, err := s.WithdrawGroupVault(ctx, group, p, 25); err != nil || v != 35 {
		t.Fatalf("withdraw = %d, %v", v, err)
	}
	if goldOf(t, pool, p) != 65 || vaultOf(t, s, group) != 35 || auditCount(t, pool, store.GroupVaultWithdrawKind) != 1 {
		t.Fatal("withdraw did not move gold with one audit row")
	}

	// Failures change nothing.
	for name, f := range map[string]func() error{
		"deposit over player gold": func() error { _, err := s.DepositGroupVault(ctx, group, p, 66); return err },
		"withdraw over vault":      func() error { _, err := s.WithdrawGroupVault(ctx, group, p, 36); return err },
		"deposit zero":             func() error { _, err := s.DepositGroupVault(ctx, group, p, 0); return err },
		"withdraw negative":        func() error { _, err := s.WithdrawGroupVault(ctx, group, p, -5); return err },
		"deposit negative":         func() error { _, err := s.DepositGroupVault(ctx, group, p, -5); return err },
	} {
		if err := f(); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := s.DepositGroupVault(ctx, 999, p, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown group err = %v", err)
	}
	if _, err := s.WithdrawGroupVault(ctx, group, 999, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown player err = %v", err)
	}
	if goldOf(t, pool, p) != 65 || vaultOf(t, s, group) != 35 ||
		auditCount(t, pool, store.GroupVaultDepositKind) != 1 || auditCount(t, pool, store.GroupVaultWithdrawKind) != 1 {
		t.Fatal("failed moves changed state")
	}
}

func TestPGGroupVaultRace(t *testing.T) {
	s, pool, group, ids := vaultFixture(t, 100, 3)
	ctx := context.Background()
	if _, err := s.DepositGroupVault(ctx, group, ids[0], 100); err != nil {
		t.Fatal(err)
	}
	// Two withdrawals of 80 from a vault of 100: at most one can win.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.WithdrawGroupVault(ctx, group, ids[i+1], 80)
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("loser err = %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d withdrawals succeeded, want 1", wins)
	}
	if v := vaultOf(t, s, group); v != 20 {
		t.Fatalf("vault = %d, want 20", v)
	}
	if goldOf(t, pool, ids[1])+goldOf(t, pool, ids[2]) != 2*(100+store.GroupFoundCost)+80 {
		t.Fatal("gold not conserved")
	}
}

func TestPGDoorLog(t *testing.T) {
	s, _, group, ids := vaultFixture(t, 0, 2)
	ctx := context.Background()
	now := groupT0
	e := func(door string, a store.DoorAction, p int64, at time.Time) store.DoorLogEntry {
		return store.DoorLogEntry{Group: group, Hall: "hall1", Door: door, Action: a, Player: p, At: at}
	}
	old := e("outer", store.DoorLock, ids[0], now.Add(-8*24*time.Hour))
	edge := e("outer", store.DoorOpen, ids[0], now.Add(-6*24*time.Hour))
	mid := e("inner", store.DoorUnlock, ids[1], now.Add(-time.Hour))
	last := e("inner", store.DoorBreak, 0, now)
	// Saved out of order across two calls; loaded in time order.
	if err := s.AppendDoorLog(ctx, []store.DoorLogEntry{last, old}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendDoorLog(ctx, []store.DoorLogEntry{mid, edge}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadDoorLog(ctx, group, now.Add(-30*24*time.Hour))
	if err != nil || len(got) != 4 {
		t.Fatalf("load = %v, %v", got, err)
	}
	for i, want := range []store.DoorLogEntry{old, edge, mid, last} {
		if got[i].Door != want.Door || got[i].Action != want.Action || got[i].Player != want.Player ||
			got[i].Hall != want.Hall || !got[i].At.Equal(want.At) {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want)
		}
	}
	if got, _ := s.LoadDoorLog(ctx, group, now.Add(-24*time.Hour)); len(got) != 2 {
		t.Fatalf("last 24h = %d entries, want 2", len(got))
	}
	if n, err := s.PruneDoorLog(ctx, now); err != nil || n != 1 {
		t.Fatalf("prune = %d, %v; want 1", n, err)
	}
	got, _ = s.LoadDoorLog(ctx, group, time.Time{})
	if len(got) != 3 || got[0].Action != store.DoorOpen {
		t.Fatalf("after prune = %+v", got)
	}
	if err := s.AppendDoorLog(ctx, []store.DoorLogEntry{{Group: group, Hall: "h", Door: "d", Action: "smash", At: now}}); err == nil {
		t.Error("unknown door action accepted")
	}
}
