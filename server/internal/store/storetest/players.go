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
		c, err := s.CreateGuest(ctx, "h1")
		if err != nil {
			t.Fatal(err)
		}
		if c.TokenHash != "h1" || c.Version < 1 {
			t.Fatalf("bad created record %+v", c)
		}
		l, err := s.LoadByTokenHash(ctx, "h1")
		if err != nil || l != c {
			t.Fatalf("load = %+v, %v; want %+v", l, err, c)
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
		if _, err := s.CreateGuest(ctx, "h1"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateGuest(ctx, "h1"); err == nil {
			t.Fatal("duplicate CreateGuest succeeded")
		}
	})

	t.Run("distinct ids", func(t *testing.T) {
		s := factory(t)
		a, _ := s.CreateGuest(ctx, "a")
		b, _ := s.CreateGuest(ctx, "b")
		if a.ID == b.ID {
			t.Fatalf("same id %d", a.ID)
		}
	})

	t.Run("save increments version and stores position", func(t *testing.T) {
		s := factory(t)
		p, _ := s.CreateGuest(ctx, "h1")
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
		p, _ := s.CreateGuest(ctx, "h1")
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
		p, _ := s.CreateGuest(ctx, "h1")
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
}
