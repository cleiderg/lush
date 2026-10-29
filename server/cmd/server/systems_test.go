package main

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/cardroom"
	"github.com/cleiderg/lush/server/internal/systems/combat"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/npc"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/systems/spawner"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// realSystems registers the server's game systems, as run does, on a fresh
// world with a fake clock.
func realSystems(t *testing.T) (*world.World, []scheduled, *areas.Lookup, *gamedata.Data) {
	t.Helper()
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
	t.Cleanup(sess.Close)
	lookup, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	systems, err := gameSystems(mover, data, lookup, grid, 5, w.TickInterval(), clock, sess)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range systems {
		if err := w.RegisterEvery(s.sys, s.every); err != nil {
			t.Fatal(err)
		}
	}
	return w, systems, lookup, data
}

// AC1 (#218): the PvP combat system is registered with RegisterEvery(s, 12).
// AC1 (#241): the NPC system, PvE combat (every 12 ticks, after NPC
// movement) and the spawner (after combat) are registered.
// AC1 (#307): the card room, built from game data, is registered with every
// card table and its stake, before the session.
func TestGameSystemsRegisterItemsShopTradeRegenBeforeSession(t *testing.T) {
	_, systems, _, data := realSystems(t)
	var names []string
	for _, s := range systems {
		names = append(names, s.sys.Name())
		want := 1
		if s.sys.Name() == "pvp_combat" || s.sys.Name() == "pve_combat" {
			want = 12 // the 0.6 s combat tick
		}
		if s.every != want {
			t.Errorf("%s every %d ticks, want %d", s.sys.Name(), s.every, want)
		}
	}
	want := []string{"movement", "npc", "emote", "chat", "items", "shop", "trade", "cardroom", "grounditems",
		"pvp_combat", "pve_combat", "spawner", "regen", "session"}
	if !slices.Equal(names, want) {
		t.Fatalf("systems = %v, want %v", names, want)
	}
	if _, ok := systems[slices.Index(names, "trade")].sys.(*trade.System); !ok {
		t.Fatalf("system %q is %T, want *trade.System", "trade", systems[slices.Index(names, "trade")].sys)
	}
	if _, ok := systems[slices.Index(names, "pvp_combat")].sys.(*pvp.Combat); !ok {
		t.Fatalf("system %q is %T, want *pvp.Combat", "pvp_combat", systems[slices.Index(names, "pvp_combat")].sys)
	}
	cards := systems[slices.Index(names, "cardroom")].sys.(*cardroom.System)
	for _, ct := range data.CardTables {
		if stake, ok := cards.Stake(cardroom.TableID(ct.ID)); !ok || stake != ct.Stake {
			t.Errorf("card table %d: stake %d (hosted %v), want %d", ct.ID, stake, ok, ct.Stake)
		}
		if tb, ok := cards.Table(cardroom.TableID(ct.ID)); !ok || tb.Seats() != ct.Seats {
			t.Errorf("card table %d not hosted with %d seats", ct.ID, ct.Seats)
		}
	}
	for name, typ := range map[string]any{"cardroom": (*cardroom.System)(nil), "npc": (*npc.System)(nil), "pve_combat": (*combat.System)(nil), "spawner": (*spawner.System)(nil)} {
		if got := systems[slices.Index(names, name)].sys; reflect.TypeOf(got) != reflect.TypeOf(typ) {
			t.Errorf("system %q is %T, want %T", name, got, typ)
		}
	}
}

// AC2 (#241): after the first tick of the real systems, every NPC type's
// population (capped by max_alive) is alive in its area.
func TestFirstTickSpawnsEveryNPCPopulation(t *testing.T) {
	w, _, lookup, data := realSystems(t)
	w.Tick()
	area := map[gamedata.Area]areas.Area{gamedata.AreaAldrichPark: areas.AldrichPark, gamedata.AreaSouthFields: areas.SouthFields}
	count := map[world.NPCTypeID]int{}
	for _, n := range w.NPCs() {
		count[n.Type]++
	}
	for _, typ := range data.NPCs {
		want := typ.Population
		if typ.MaxAlive > 0 && want > typ.MaxAlive {
			want = typ.MaxAlive
		}
		if got := count[world.NPCTypeID(typ.ID)]; got != want || want == 0 {
			t.Errorf("%s: %d alive after the first tick, want population %d", typ.Key, got, want)
		}
	}
	for _, n := range w.NPCs() {
		typ := data.NPCs[slices.IndexFunc(data.NPCs, func(d gamedata.NPCType) bool { return world.NPCTypeID(d.ID) == n.Type })]
		if got := lookup.AreaAt(n.Pos); got != area[typ.Area] || n.HP != typ.HP {
			t.Errorf("%s NPC %d at %v is in %v with %d HP, want %v with %d HP", typ.Key, n.ID, n.Pos, got, n.HP, area[typ.Area], typ.HP)
		}
	}
}
