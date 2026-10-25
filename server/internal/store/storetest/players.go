// Package storetest holds the contract tests every store.Players implementation must pass.
package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cleiderg/lush/server/internal/store"
)

// RunPlayers runs the Players contract against fresh stores from factory.
func RunPlayers(t *testing.T, factory func(t *testing.T) store.Players) {
	ctx := context.Background()

	t.Run("create and load", func(t *testing.T) {
		s := factory(t)
		c, err := s.CreateGuest(ctx, "h1", store.Holdings{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.TokenHash != "h1" || c.Version < 1 {
			t.Fatalf("bad created record %+v", c)
		}
		if c.XP != 0 || c.Holdings != (store.Holdings{}) {
			t.Fatalf("new guest has progress %+v, want 0 XP and empty holdings", c)
		}
		l, err := s.LoadByTokenHash(ctx, "h1")
		if err != nil || l != c {
			t.Fatalf("load = %+v, %v; want %+v", l, err, c)
		}
	})

	t.Run("create with holdings", func(t *testing.T) {
		s := factory(t)
		want := SampleHoldings()
		c, err := s.CreateGuest(ctx, "h1", want, []store.Entry{{Kind: "item_create", Payload: map[string]any{"item": 1}}})
		if err != nil {
			t.Fatal(err)
		}
		if c.Holdings != want || c.XP != 0 || c.Version != 1 {
			t.Fatalf("created %+v, want holdings %+v at version 1", c, want)
		}
		l, err := s.LoadByTokenHash(ctx, "h1")
		if err != nil || l != c {
			t.Fatalf("load = %+v, %v; want %+v", l, err, c)
		}
	})

	t.Run("create failure creates nothing", func(t *testing.T) {
		bad := SampleHoldings()
		bad.Inventory[27] = store.Stack{Item: 2, Qty: -1}
		for _, tc := range []struct {
			name    string
			start   store.Holdings
			entries []store.Entry
			wantErr error
		}{
			{"invalid holding", bad, []store.Entry{{Kind: "item_create"}}, store.ErrInvalid},
			{"unknown counterparty", SampleHoldings(), []store.Entry{{Kind: "item_create"}, {Kind: "trade", Counterparty: 999}}, store.ErrNotFound},
			{"unencodable payload", SampleHoldings(), []store.Entry{{Kind: "item_create", Payload: func() {}}}, nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := factory(t)
				_, err := s.CreateGuest(ctx, "h1", tc.start, tc.entries)
				if err == nil || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if _, err := s.LoadByTokenHash(ctx, "h1"); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("load after failed create: err = %v, want ErrNotFound", err)
				}
				// The hash is still free.
				if _, err := s.CreateGuest(ctx, "h1", store.Holdings{}, nil); err != nil {
					t.Fatalf("create after failed create: %v", err)
				}
			})
		}
	})

	t.Run("load unknown", func(t *testing.T) {
		_, err := factory(t).LoadByTokenHash(ctx, "nope")
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("duplicate hash", func(t *testing.T) {
		s := factory(t)
		if _, err := s.CreateGuest(ctx, "h1", store.Holdings{}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateGuest(ctx, "h1", store.Holdings{}, nil); err == nil {
			t.Fatal("duplicate CreateGuest succeeded")
		}
	})

	t.Run("distinct ids", func(t *testing.T) {
		s := factory(t)
		a, _ := s.CreateGuest(ctx, "a", store.Holdings{}, nil)
		b, _ := s.CreateGuest(ctx, "b", store.Holdings{}, nil)
		if a.ID == b.ID {
			t.Fatalf("same id %d", a.ID)
		}
	})

	t.Run("save increments version and stores position", func(t *testing.T) {
		s := factory(t)
		p, _ := s.CreateGuest(ctx, "h1", store.Holdings{}, nil)
		p.X, p.Z = 12.5, -3
		saved, err := s.Save(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Version != p.Version+1 || saved.X != 12.5 || saved.Z != -3 {
			t.Fatalf("saved = %+v", saved)
		}
		l, _ := s.LoadByTokenHash(ctx, "h1")
		if l != saved {
			t.Fatalf("loaded %+v, want %+v", l, saved)
		}
	})

	t.Run("stale save rejected and changes nothing", func(t *testing.T) {
		s := factory(t)
		p, _ := s.CreateGuest(ctx, "h1", store.Holdings{}, nil)
		first := p
		first.X = 1
		saved, err := s.Save(ctx, first)
		if err != nil {
			t.Fatal(err)
		}
		stale := p // old version
		stale.X, stale.Z = 99, 99
		if _, err := s.Save(ctx, stale); !errors.Is(err, store.ErrStaleVersion) {
			t.Fatalf("err = %v, want ErrStaleVersion", err)
		}
		future := saved
		future.Version += 5
		if _, err := s.Save(ctx, future); !errors.Is(err, store.ErrStaleVersion) {
			t.Fatalf("future err = %v, want ErrStaleVersion", err)
		}
		l, _ := s.LoadByTokenHash(ctx, "h1")
		if l != saved {
			t.Fatalf("loaded %+v, want unchanged %+v", l, saved)
		}
	})

	t.Run("concurrent saves: exactly one wins", func(t *testing.T) {
		s := factory(t)
		p, _ := s.CreateGuest(ctx, "h1", store.Holdings{}, nil)
		const n = 16
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins := 0
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				q := p
				q.X = float32(i)
				if _, err := s.Save(ctx, q); err == nil {
					mu.Lock()
					wins++
					mu.Unlock()
				} else if !errors.Is(err, store.ErrStaleVersion) {
					t.Errorf("err = %v", err)
				}
			}(i)
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("wins = %d, want 1", wins)
		}
	})

	t.Run("save stores xp", func(t *testing.T) {
		s := factory(t)
		p, _ := s.CreateGuest(ctx, "h1", store.Holdings{}, nil)
		p.XP = 1234
		saved, err := s.Save(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if saved.XP != 1234 {
			t.Fatalf("saved XP = %d, want 1234", saved.XP)
		}
		l, _ := s.LoadByTokenHash(ctx, "h1")
		if l != saved {
			t.Fatalf("loaded %+v, want %+v", l, saved)
		}
	})

	t.Run("stale save does not store xp", func(t *testing.T) {
		s := factory(t)
		p, _ := s.CreateGuest(ctx, "h1", store.Holdings{}, nil)
		saved, err := s.Save(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		stale := p
		stale.XP = 500
		if _, err := s.Save(ctx, stale); !errors.Is(err, store.ErrStaleVersion) {
			t.Fatalf("err = %v, want ErrStaleVersion", err)
		}
		if l, _ := s.LoadByTokenHash(ctx, "h1"); l != saved {
			t.Fatalf("loaded %+v, want unchanged %+v", l, saved)
		}
	})

	t.Run("save never changes holdings", func(t *testing.T) {
		s := factory(t)
		p, _ := s.CreateGuest(ctx, "h1", store.Holdings{}, nil)
		want := SampleHoldings()
		if err := s.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: p.ID, Holdings: want}}, nil); err != nil {
			t.Fatal(err)
		}
		forged := p
		forged.X, forged.XP = 7, 10
		forged.Gold = 999999
		forged.Inventory[0] = store.Stack{Item: 99, Qty: 99}
		forged.Inventory[27] = store.Stack{Item: 98, Qty: 1}
		forged.Equipment[store.EquipShield] = 97
		saved, err := s.Save(ctx, forged)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Holdings != want || saved.X != 7 || saved.XP != 10 {
			t.Fatalf("saved = %+v, want holdings %+v", saved, want)
		}
		l, _ := s.LoadByTokenHash(ctx, "h1")
		if l != saved {
			t.Fatalf("loaded %+v, want %+v", l, saved)
		}
		// Empty holdings passed to Save do not clear stored ones either.
		blank := saved
		blank.Holdings = store.Holdings{}
		if _, err := s.Save(ctx, blank); err != nil {
			t.Fatal(err)
		}
		if l, _ := s.LoadByTokenHash(ctx, "h1"); l.Holdings != want {
			t.Fatalf("holdings after blank save = %+v, want %+v", l.Holdings, want)
		}
	})

	t.Run("apply economy round trip", func(t *testing.T) {
		s := factory(t)
		a, _ := s.CreateGuest(ctx, "a", store.Holdings{}, nil)
		b, _ := s.CreateGuest(ctx, "b", store.Holdings{}, nil)
		ha := SampleHoldings()
		var hb store.Holdings
		hb.Gold = 40
		hb.Inventory[27] = store.Stack{Item: 11, Qty: 1 << 40}
		hb.Equipment[store.EquipHead] = 5
		err := s.ApplyEconomy(ctx,
			[]store.EconomyChange{{PlayerID: b.ID, Holdings: hb}, {PlayerID: a.ID, Holdings: ha}},
			[]store.Entry{{Kind: "trade", Actor: a.ID, Counterparty: b.ID, Payload: map[string]any{"gold": 10}}})
		if err != nil {
			t.Fatal(err)
		}
		la, _ := s.LoadByTokenHash(ctx, "a")
		lb, _ := s.LoadByTokenHash(ctx, "b")
		if la.Holdings != ha || lb.Holdings != hb {
			t.Fatalf("loaded a %+v b %+v; want %+v and %+v", la.Holdings, lb.Holdings, ha, hb)
		}
		// Replacing holdings clears slots no longer occupied.
		if err := s.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: a.ID}}, nil); err != nil {
			t.Fatal(err)
		}
		if la, _ := s.LoadByTokenHash(ctx, "a"); la.Holdings != (store.Holdings{}) {
			t.Fatalf("holdings after clear = %+v, want empty", la.Holdings)
		}
	})

	t.Run("apply economy keeps version so the next save succeeds", func(t *testing.T) {
		s := factory(t)
		p, _ := s.CreateGuest(ctx, "h1", store.Holdings{}, nil)
		if err := s.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: p.ID, Holdings: SampleHoldings()}},
			[]store.Entry{{Kind: "item_create", Actor: p.ID}}); err != nil {
			t.Fatal(err)
		}
		l, _ := s.LoadByTokenHash(ctx, "h1")
		if l.Version != p.Version {
			t.Fatalf("version after ApplyEconomy = %d, want %d", l.Version, p.Version)
		}
		p.X = 3 // the saver still holds the version from before the economy write
		saved, err := s.Save(ctx, p)
		if err != nil {
			t.Fatalf("save after ApplyEconomy: %v", err)
		}
		if saved.Holdings != SampleHoldings() {
			t.Fatalf("save after ApplyEconomy lost holdings: %+v", saved.Holdings)
		}
	})

	t.Run("apply economy failure changes nothing", func(t *testing.T) {
		good := SampleHoldings()
		negQty := good
		negQty.Inventory[3] = store.Stack{Item: 2, Qty: -1}
		negGold := good
		negGold.Gold = -1
		noItem := good
		noItem.Inventory[4] = store.Stack{Qty: 2}
		negEquip := good
		negEquip.Equipment[store.EquipBody] = -3
		for _, tc := range []struct {
			name    string
			second  store.Holdings
			entries func(a, b int64) []store.Entry
			wantErr error
		}{
			{"negative quantity", negQty, nil, store.ErrInvalid},
			{"negative gold", negGold, nil, store.ErrInvalid},
			{"quantity without item", noItem, nil, store.ErrInvalid},
			{"negative equipment item", negEquip, nil, store.ErrInvalid},
			{"unknown audit actor", good, func(a, b int64) []store.Entry {
				return []store.Entry{{Kind: "trade", Actor: a}, {Kind: "trade", Actor: a + b + 100}}
			}, store.ErrNotFound},
			{"unknown audit counterparty", good, func(a, b int64) []store.Entry {
				return []store.Entry{{Kind: "trade", Actor: a, Counterparty: a + b + 100}}
			}, store.ErrNotFound},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := factory(t)
				a, _ := s.CreateGuest(ctx, "a", store.Holdings{}, nil)
				b, _ := s.CreateGuest(ctx, "b", store.Holdings{}, nil)
				var entries []store.Entry
				if tc.entries != nil {
					entries = tc.entries(a.ID, b.ID)
				}
				err := s.ApplyEconomy(ctx,
					[]store.EconomyChange{{PlayerID: a.ID, Holdings: good}, {PlayerID: b.ID, Holdings: tc.second}},
					entries)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if la, _ := s.LoadByTokenHash(ctx, "a"); la != a {
					t.Fatalf("a changed: %+v, want %+v", la, a)
				}
				if lb, _ := s.LoadByTokenHash(ctx, "b"); lb != b {
					t.Fatalf("b changed: %+v, want %+v", lb, b)
				}
			})
		}
	})

	t.Run("apply economy unknown player or duplicate", func(t *testing.T) {
		s := factory(t)
		a, _ := s.CreateGuest(ctx, "a", store.Holdings{}, nil)
		err := s.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: a.ID, Holdings: SampleHoldings()}, {PlayerID: a.ID + 100}}, nil)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown player err = %v, want ErrNotFound", err)
		}
		err = s.ApplyEconomy(ctx, []store.EconomyChange{{PlayerID: a.ID, Holdings: SampleHoldings()}, {PlayerID: a.ID}}, nil)
		if !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("duplicate player err = %v, want ErrInvalid", err)
		}
		if la, _ := s.LoadByTokenHash(ctx, "a"); la != a {
			t.Fatalf("a changed: %+v, want %+v", la, a)
		}
	})
}

// SampleHoldings returns holdings that use the first and last inventory slots,
// a stack and every equipment slot.
func SampleHoldings() store.Holdings {
	var h store.Holdings
	h.Gold = 100
	h.Inventory[0] = store.Stack{Item: 9, Qty: 3}
	h.Inventory[13] = store.Stack{Item: 12, Qty: 250}
	h.Inventory[27] = store.Stack{Item: 2, Qty: 1}
	h.Equipment[store.EquipWeapon] = 1
	h.Equipment[store.EquipHead] = 7
	h.Equipment[store.EquipBody] = 4
	h.Equipment[store.EquipShield] = 5
	return h
}
