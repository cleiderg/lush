package combat_test

import (
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/combat"
	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/loot"
	"github.com/cleiderg/lush/server/internal/world"
)

// kills returns the recorded kill events.
func (r *recorder) kills() []combat.Kill {
	var out []combat.Kill
	for _, e := range r.events {
		if e.Kill != nil {
			out = append(out, *e.Kill)
		}
	}
	return out
}

// killSeed returns the first PCG seed (s, s+1) whose first damage roll for
// att is above 0, so a one-HP NPC dies on the first swing.
func killSeed(t *testing.T, att formula.Attack) uint64 {
	t.Helper()
	for s := uint64(1); s < 1000; s++ {
		if formula.RollDamage(rand.New(rand.NewPCG(s, s+1)), att) > 0 {
			return s
		}
	}
	t.Fatal("no seed gives a hit")
	return 0
}

// TestKillLoot checks that a player's killing hit rolls the NPC's loot with
// the RNG passed to combat.New: the killer's gold rises by exactly the
// rolled gold, with one npc_drop outbox event holding that change, and the
// Kill event carries the rolled item stacks and the death position (none for
// an NPC without item drops, such as a rabbit).
func TestKillLoot(t *testing.T) {
	tests := []struct {
		npc       string
		wantItems bool // whether the NPC can drop items at all
	}{
		{"rabbit", false},
		{"squirrel", true},
		{"crow", true},
		{"coyote", true},
		{"rogue_delivery_robot", true},
		{"coyote_alpha", true},
	}
	for _, tt := range tests {
		t.Run(tt.npc, func(t *testing.T) {
			const level = 30
			d, err := gamedata.Default()
			if err != nil {
				t.Fatal(err)
			}
			nt, _ := d.NPCByKey(tt.npc)
			weapon, _ := d.ItemByKey("bike_lock")
			bonus, _ := (&world.Equipment{Weapon: weapon.ID}).Bonuses(d)
			att := formula.Attack{AttackerLevel: level, Accuracy: bonus.Accuracy, Strength: bonus.Strength, DefenderLevel: nt.Level, Defence: nt.Defence}
			seed := killSeed(t, att)

			// Replay the rolls: the killing swing, then the loot.
			r := rand.New(rand.NewPCG(seed, seed+1))
			formula.RollDamage(r, att)
			want, err := loot.Roll(d, nt.ID, r)
			if err != nil {
				t.Fatal(err)
			}

			f := newFixture(t, rand.New(rand.NewPCG(seed, seed+1)), false)
			p := f.player(t, 1, level, f.field(0, 0), "bike_lock")
			p.Inv.Gold = 50
			pos := f.field(1, 0)
			n := f.npc(t, tt.npc, pos)
			n.HP = 1
			f.attack(p, n)
			f.combatTicks(1)

			if p.Inv.Gold != 50+want.Gold {
				t.Errorf("gold = %d, want 50 + %d", p.Inv.Gold, want.Gold)
			}
			ev := p.DrainEconomy()
			wantCh := []world.Change{{Kind: world.ChangeGold, Gold: want.Gold}}
			if len(ev) != 1 || ev[0].Reason != combat.ReasonNPCDrop || !reflect.DeepEqual(ev[0].Changes, wantCh) {
				t.Fatalf("outbox = %+v, want one %q event with %+v", ev, combat.ReasonNPCDrop, wantCh)
			}
			if ev[0].Inv.Gold != p.Inv.Gold {
				t.Errorf("event inventory gold = %d, want %d", ev[0].Inv.Gold, p.Inv.Gold)
			}
			if combat.ReasonNPCDrop != "npc_drop" {
				t.Errorf("ReasonNPCDrop = %q", combat.ReasonNPCDrop)
			}

			wantKill := combat.Kill{NPC: n.ID, Type: n.Type, Pos: pos, Killer: p.ID, Items: want.Items}
			kills := f.rec.kills()
			if len(kills) != 1 || !reflect.DeepEqual(kills[0], wantKill) {
				t.Fatalf("kills = %+v, want [%+v]", kills, wantKill)
			}
			if !tt.wantItems && len(kills[0].Items) != 0 {
				t.Errorf("%s dropped items %+v", tt.npc, kills[0].Items)
			}
		})
	}
}

// TestKillLootItems checks the Kill event's stacks with fixed rolls: a
// coyote alpha always drops its 3 pelts (chance 1), and with every roll at
// its maximum no other drop lands; a rabbit drops no stacks at all.
func TestKillLootItems(t *testing.T) {
	tests := []struct {
		npc  string
		want func(f *fixture) []world.Stack
	}{
		{"rabbit", func(*fixture) []world.Stack { return nil }},
		{"coyote_alpha", func(f *fixture) []world.Stack {
			return []world.Stack{{Item: f.item(t, "coyote_pelt"), Qty: 3}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.npc, func(t *testing.T) {
			f := newFixture(t, alwaysMax, false)
			p := f.player(t, 1, 30, f.field(0, 0), "bike_lock")
			pos := f.field(1, 1)
			n := f.npc(t, tt.npc, pos)
			n.HP = 1
			f.attack(p, n)
			f.combatTicks(1)
			kills := f.rec.kills()
			if len(kills) != 1 {
				t.Fatalf("kills = %+v, want one", kills)
			}
			if kills[0].Pos != pos {
				t.Errorf("kill position = %v, want %v", kills[0].Pos, pos)
			}
			if want := tt.want(f); !reflect.DeepEqual(kills[0].Items, want) {
				t.Errorf("kill items = %+v, want %+v", kills[0].Items, want)
			}
		})
	}
}

// TestKillLootLockedInventory checks that a kill by a player whose inventory
// is locked (a trade being saved) changes no gold and records nothing at
// once, while the Kill event still carries the stacks; the gold is credited
// as one npc_drop event on the first combat tick after the unlock, and never
// again.
func TestKillLootLockedInventory(t *testing.T) {
	f := newFixture(t, alwaysMax, false)
	alpha, _ := f.d.NPCByKey("coyote_alpha")
	p := f.player(t, 1, 30, f.field(0, 0), "bike_lock")
	p.Inv.Gold = 7
	n := f.npc(t, "coyote_alpha", f.field(1, 0))
	n.HP = 1
	p.Inv.SetLocked(true)
	f.attack(p, n)
	f.combatTicks(1)

	kills := f.rec.kills()
	pelts := []world.Stack{{Item: f.item(t, "coyote_pelt"), Qty: 3}}
	if len(kills) != 1 || !reflect.DeepEqual(kills[0].Items, pelts) {
		t.Fatalf("kills = %+v, want one with %+v", kills, pelts)
	}
	check := func(when string, gold, events int) {
		t.Helper()
		if p.Inv.Gold != gold {
			t.Errorf("%s: gold = %d, want %d", when, p.Inv.Gold, gold)
		}
		if got := len(p.PendingEconomy()); got != events {
			t.Errorf("%s: outbox has %d events, want %d", when, got, events)
		}
	}
	check("on the kill", 7, 0)
	f.combatTicks(2)
	check("still locked", 7, 0)

	p.Inv.SetLocked(false)
	for range combatEvery - 1 {
		f.w.Tick()
	}
	check("unlocked, before the combat tick", 7, 0)
	f.w.Tick()
	gold := alpha.GoldMax // alwaysMax rolls the top of the range
	check("first combat tick after unlock", 7+gold, 1)
	ev := p.PendingEconomy()[0]
	want := []world.Change{{Kind: world.ChangeGold, Gold: gold}}
	if ev.Reason != combat.ReasonNPCDrop || !reflect.DeepEqual(ev.Changes, want) {
		t.Errorf("event = %+v, want %q with %+v", ev, combat.ReasonNPCDrop, want)
	}
	f.combatTicks(3)
	check("later", 7+gold, 1)
}

// TestOwedGoldForgottenOnLeave checks gold held for a locked killer who then
// leaves is not paid to a newcomer reusing their id.
func TestOwedGoldForgottenOnLeave(t *testing.T) {
	f := newFixture(t, alwaysMax, false)
	p := f.player(t, 1, 30, f.field(0, 0), "bike_lock")
	n := f.npc(t, "rabbit", f.field(1, 0))
	p.Inv.SetLocked(true)
	f.attack(p, n)
	f.combatTicks(1)
	f.w.RemovePlayer(p.ID)
	f.combatTicks(1)
	q := f.player(t, 1, 30, f.field(0, 0), "")
	f.combatTicks(2)
	if q.Inv.Gold != 0 || len(q.PendingEconomy()) != 0 {
		t.Errorf("newcomer got gold %d, events %+v", q.Inv.Gold, q.PendingEconomy())
	}
}
