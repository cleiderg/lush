package loot_test

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/loot"
	"github.com/cleiderg/lush/server/internal/world"
)

// formula.Rand must satisfy loot.Rand, so combat can pass its source on.
var _ loot.Rand = formula.Rand(nil)

func loadData(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func npcID(t testing.TB, d *gamedata.Data, key string) gamedata.NPCTypeID {
	t.Helper()
	n, ok := d.NPCByKey(key)
	if !ok {
		t.Fatalf("no npc %q", key)
	}
	return n.ID
}

func itemID(t testing.TB, d *gamedata.Data, key string) gamedata.ItemID {
	t.Helper()
	it, ok := d.ItemByKey(key)
	if !ok {
		t.Fatalf("no item %q", key)
	}
	return it.ID
}

func TestRollDeterministic(t *testing.T) {
	d := loadData(t)
	for _, npc := range d.NPCs {
		t.Run(npc.Key, func(t *testing.T) {
			a := rand.New(rand.NewPCG(11, 22))
			b := rand.New(rand.NewPCG(11, 22))
			for i := range 200 {
				la, errA := loot.Roll(d, npc.ID, a)
				lb, errB := loot.Roll(d, npc.ID, b)
				if errA != nil || errB != nil {
					t.Fatalf("roll %d: %v, %v", i, errA, errB)
				}
				if !reflect.DeepEqual(la, lb) {
					t.Fatalf("roll %d: %+v != %+v", i, la, lb)
				}
			}
		})
	}
}

func TestGoldRange(t *testing.T) {
	d := loadData(t)
	if len(d.NPCs) != 6 {
		t.Fatalf("want 6 shipped NPC types, got %d", len(d.NPCs))
	}
	for _, npc := range d.NPCs {
		t.Run(npc.Key, func(t *testing.T) {
			r := rand.New(rand.NewPCG(3, 4))
			seen := map[int]bool{}
			for range 10000 {
				l, err := loot.Roll(d, npc.ID, r)
				if err != nil {
					t.Fatal(err)
				}
				if l.Gold < npc.GoldMin || l.Gold > npc.GoldMax {
					t.Fatalf("gold %d outside %d..%d", l.Gold, npc.GoldMin, npc.GoldMax)
				}
				seen[l.Gold] = true
			}
			if !seen[npc.GoldMin] || !seen[npc.GoldMax] {
				t.Errorf("ends of %d..%d not both seen (min %v, max %v)",
					npc.GoldMin, npc.GoldMax, seen[npc.GoldMin], seen[npc.GoldMax])
			}
		})
	}
}

func TestDropRates(t *testing.T) {
	const rolls = 10000
	d := loadData(t)
	tests := []struct {
		npc, item string
		qty       int
		rate      float64
		tol       float64
	}{
		{"squirrel", "acorn", 1, 0.5, 0.03},
		{"crow", "shiny_trinket", 1, 1.0 / 8, 0.02},
		{"coyote", "coyote_pelt", 1, 0.5, 0.03},
		{"coyote", "leather_cap", 1, 1.0 / 20, 0.01},
		{"rogue_delivery_robot", "scrap_metal", 1, 0.5, 0.03},
		{"rogue_delivery_robot", "bike_lock", 1, 1.0 / 32, 0.01},
		{"rogue_delivery_robot", "robot_plating", 1, 1.0 / 64, 0.006},
		{"coyote_alpha", "coyote_pelt", 3, 1, 0},
		{"coyote_alpha", "bike_helmet", 1, 1.0 / 8, 0.02},
	}
	for _, tc := range tests {
		t.Run(tc.npc+"/"+tc.item, func(t *testing.T) {
			id, item := npcID(t, d, tc.npc), itemID(t, d, tc.item)
			r := rand.New(rand.NewPCG(5, 6))
			hits := 0
			for range rolls {
				l, err := loot.Roll(d, id, r)
				if err != nil {
					t.Fatal(err)
				}
				n := 0
				for _, s := range l.Items {
					if s.Item == item {
						n++
						if s.Qty != tc.qty {
							t.Fatalf("%s stack qty %d, want %d", tc.item, s.Qty, tc.qty)
						}
					}
				}
				if n > 1 {
					t.Fatalf("%d %s stacks in one kill", n, tc.item)
				}
				hits += n
			}
			got := float64(hits) / rolls
			if math.Abs(got-tc.rate) > tc.tol {
				t.Errorf("%s dropped in %.4f of kills, want %.4f ± %.4f", tc.item, got, tc.rate, tc.tol)
			}
		})
	}
}

// TestDropsIndependent checks that two drops on one NPC roll separately: how
// often both land is the product of their rates.
func TestDropsIndependent(t *testing.T) {
	const rolls = 20000
	d := loadData(t)
	id := npcID(t, d, "coyote")
	pelt, leather := itemID(t, d, "coyote_pelt"), itemID(t, d, "leather_cap")
	r := rand.New(rand.NewPCG(7, 8))
	both := 0
	for range rolls {
		l, err := loot.Roll(d, id, r)
		if err != nil {
			t.Fatal(err)
		}
		var gotPelt, gotCap bool
		for _, s := range l.Items {
			gotPelt = gotPelt || s.Item == pelt
			gotCap = gotCap || s.Item == leather
		}
		if gotPelt && gotCap {
			both++
		}
	}
	if got, want := float64(both)/rolls, 0.5/20; math.Abs(got-want) > 0.006 {
		t.Errorf("both dropped in %.4f of kills, want %.4f", got, want)
	}
}

func TestCoyoteAlphaAlwaysDropsPelts(t *testing.T) {
	d := loadData(t)
	id, pelt := npcID(t, d, "coyote_alpha"), itemID(t, d, "coyote_pelt")
	r := rand.New(rand.NewPCG(9, 10))
	for i := range 10000 {
		l, err := loot.Roll(d, id, r)
		if err != nil {
			t.Fatal(err)
		}
		if len(l.Items) == 0 || l.Items[0] != (world.Stack{Item: pelt, Qty: 3}) {
			t.Fatalf("kill %d: items %+v, want 3 coyote pelts first", i, l.Items)
		}
	}
}

func TestUnknownNPC(t *testing.T) {
	d := loadData(t)
	for _, id := range []gamedata.NPCTypeID{0, 7, 999} {
		l, err := loot.Roll(d, id, rand.New(rand.NewPCG(1, 1)))
		if err == nil {
			t.Errorf("npc %d: no error", id)
		}
		if l.Gold != 0 || len(l.Items) != 0 {
			t.Errorf("npc %d: got loot %+v", id, l)
		}
	}
}

func BenchmarkRoll(b *testing.B) {
	d := loadData(b)
	id := npcID(b, d, "rogue_delivery_robot")
	r := rand.New(rand.NewPCG(1, 2))
	for b.Loop() {
		if _, err := loot.Roll(d, id, r); err != nil {
			b.Fatal(err)
		}
	}
}
