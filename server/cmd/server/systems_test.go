package main

import (
	"slices"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// AC1 (#218): the PvP combat system is registered with RegisterEvery(s, 12).
func TestGameSystemsRegisterItemsShopTradeRegenBeforeSession(t *testing.T) {
	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	mover, grid, err := newMover(5, 0)
	if err != nil {
		t.Fatal(err)
	}
	clock := world.NewFakeClock(time.Unix(0, 0))
	w := world.New(world.DefaultConfig())
	sess := session.New(session.Config{Data: data}, w, clock, store.NewMemory(), nil)
	defer sess.Close()
	lookup, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	systems, err := gameSystems(mover, data, lookup, grid.CellSize(), clock, sess)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range systems {
		if err := w.RegisterEvery(s.sys, s.every); err != nil {
			t.Fatal(err)
		}
		names = append(names, s.sys.Name())
		want := 1
		if s.sys.Name() == "pvp_combat" {
			want = 12 // the 0.6 s combat tick
		}
		if s.every != want {
			t.Errorf("%s every %d ticks, want %d", s.sys.Name(), s.every, want)
		}
	}
	want := []string{"movement", "emote", "chat", "items", "shop", "trade", "grounditems", "pvp_combat", "regen", "session"}
	if !slices.Equal(names, want) {
		t.Fatalf("systems = %v, want %v", names, want)
	}
	if _, ok := systems[slices.Index(names, "trade")].sys.(*trade.System); !ok {
		t.Fatalf("system %q is %T, want *trade.System", "trade", systems[slices.Index(names, "trade")].sys)
	}
	if _, ok := systems[slices.Index(names, "pvp_combat")].sys.(*pvp.Combat); !ok {
		t.Fatalf("system %q is %T, want *pvp.Combat", "pvp_combat", systems[slices.Index(names, "pvp_combat")].sys)
	}
}
