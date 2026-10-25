package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/store/storetest"
)

func TestMemoryPlayers(t *testing.T) {
	storetest.RunPlayers(t, func(*testing.T) store.Players { return store.NewMemory() })
}

func TestMemoryApplyEconomyAudit(t *testing.T) {
	ctx := context.Background()
	m := store.NewMemory()
	a, _ := m.CreateGuest(ctx, "a", store.Holdings{}, nil)
	b, _ := m.CreateGuest(ctx, "b", store.Holdings{}, nil)
	ok := []store.Entry{{Kind: "trade", Actor: a.ID, Counterparty: b.ID}}
	if err := m.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: a.ID, Holdings: storetest.SampleHoldings()}}, ok); err != nil {
		t.Fatal(err)
	}
	bad := []store.Entry{{Kind: "trade", Actor: a.ID}, {Kind: "trade", Actor: 999}}
	if err := m.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: b.ID}}, bad); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	got := m.AuditEntries()
	if len(got) != 1 || got[0].Kind != "trade" || got[0].Actor != a.ID || got[0].Counterparty != b.ID {
		t.Fatalf("audit = %+v, want only the committed entry", got)
	}
}
