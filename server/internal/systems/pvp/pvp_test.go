package pvp_test

import (
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

func loadData(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// withRules returns a copy of d whose rules are changed by f.
func withRules(d *gamedata.Data, f func(r *gamedata.Rules)) *gamedata.Data {
	c := *d
	f(&c.Rules)
	return &c
}

func id(t testing.TB, d *gamedata.Data, key string) gamedata.ItemID {
	t.Helper()
	it, ok := d.ItemByKey(key)
	if !ok {
		t.Fatalf("no item %q", key)
	}
	return it.ID
}

// overlapAreas is a map whose safe circle (radius 10 m around 300, -340) and
// Student Center café (350..360, -320..-312) sit inside the PvP zone, so the
// safe-area refusals are tested on their own.
const overlapAreas = `{"version":1,"units":"decimetres","spawn":{"x":3000,"z":-3400},"safeRadius":100,"shop":{"x":0,"z":0},
"areas":[
 {"id":"aldrich_park","kind":"pve","p":[-1000,-1000,1000,-1000,1000,1000,-1000,1000]},
 {"id":"south_fields","kind":"pve","p":[-3600,2400,-2000,2400,-2000,3200,-3600,3200]},
 {"id":"pvp_zone","kind":"pvp","p":[2000,-4000,4000,-4000,4000,-2800,2200,-2800,2000,-3000]}],
"interiors":[
 {"id":"student_center_cafe","kind":"safe","p":[3500,-3200,3600,-3200,3600,-3120,3500,-3120]}]}`

// Spots in overlapAreas: inside its café, and one tile south of it, outside
// the café but still in the PvP zone.
var (
	cafeInPvP  = world.Vec2{X: 355.5, Y: -312.5}
	nextToCafe = world.Vec2{X: 355.5, Y: -311.5}
)

func TestCanAttack(t *testing.T) {
	d := loadData(t)
	campus, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	overlap, err := areas.Parse([]byte(overlapAreas))
	if err != nil {
		t.Fatal(err)
	}
	var (
		zoneA     = world.Vec2{X: 250, Y: -350}
		zoneB     = world.Vec2{X: 380, Y: -300}
		spawn     = campus.Spawn()
		park      = world.Vec2{X: 40, Y: -100} // Aldrich Park, outside the safe circle
		nowhere   = world.Vec2{X: 500, Y: 500}
		safeInPvP = overlap.Spawn() // inside the PvP polygon, but in the safe circle
		edgeInPvP = world.Vec2{X: 300, Y: -328}
		cafe      = world.Vec2{X: 45, Y: -351} // the campus Student Center café
	)
	tests := []struct {
		name             string
		areas            pvp.AreaLookup
		gap              int // Rules.PvPLevelGap; 0 means the gamedata value
		attacker, target pvp.Fighter
		want             pvp.Verdict
	}{
		{"both in zone, same level", campus, 0, pvp.Fighter{10, zoneA}, pvp.Fighter{10, zoneB}, pvp.Allowed},
		{"both in zone, attacker 5 higher", campus, 0, pvp.Fighter{15, zoneA}, pvp.Fighter{10, zoneB}, pvp.Allowed},
		{"both in zone, attacker 5 lower", campus, 0, pvp.Fighter{5, zoneA}, pvp.Fighter{10, zoneB}, pvp.Allowed},
		{"both in zone, attacker 6 higher", campus, 0, pvp.Fighter{16, zoneA}, pvp.Fighter{10, zoneB}, pvp.OutsideLevelGap},
		{"both in zone, attacker 6 lower", campus, 0, pvp.Fighter{4, zoneA}, pvp.Fighter{10, zoneB}, pvp.OutsideLevelGap},
		{"levels 1 and 30", campus, 0, pvp.Fighter{1, zoneA}, pvp.Fighter{30, zoneB}, pvp.OutsideLevelGap},
		{"gap read from gamedata", campus, 10, pvp.Fighter{20, zoneA}, pvp.Fighter{10, zoneB}, pvp.Allowed},
		{"gap read from gamedata, exceeded", campus, 2, pvp.Fighter{13, zoneA}, pvp.Fighter{10, zoneB}, pvp.OutsideLevelGap},
		{"attacker at spawn", campus, 0, pvp.Fighter{10, spawn}, pvp.Fighter{10, zoneB}, pvp.AttackerNotInZone},
		{"target at spawn", campus, 0, pvp.Fighter{10, zoneA}, pvp.Fighter{10, spawn}, pvp.TargetNotInZone},
		{"attacker in PvE area", campus, 0, pvp.Fighter{10, park}, pvp.Fighter{10, zoneB}, pvp.AttackerNotInZone},
		{"target in PvE area", campus, 0, pvp.Fighter{10, zoneA}, pvp.Fighter{10, park}, pvp.TargetNotInZone},
		{"attacker in no area", campus, 0, pvp.Fighter{10, nowhere}, pvp.Fighter{10, zoneB}, pvp.AttackerNotInZone},
		{"target in no area", campus, 0, pvp.Fighter{10, zoneA}, pvp.Fighter{10, nowhere}, pvp.TargetNotInZone},
		{"both outside", campus, 0, pvp.Fighter{10, spawn}, pvp.Fighter{10, park}, pvp.AttackerNotInZone},
		{"outside zone beats level gap", campus, 0, pvp.Fighter{1, zoneA}, pvp.Fighter{30, park}, pvp.TargetNotInZone},
		{"attacker in safe circle inside zone", overlap, 0, pvp.Fighter{10, safeInPvP}, pvp.Fighter{10, zoneA}, pvp.AttackerNotInZone},
		{"target in safe circle inside zone", overlap, 0, pvp.Fighter{10, zoneA}, pvp.Fighter{10, safeInPvP}, pvp.TargetNotInZone},
		{"both just outside the safe circle in zone", overlap, 0, pvp.Fighter{10, edgeInPvP}, pvp.Fighter{12, zoneA}, pvp.Allowed},
		{"attacker in campus cafe", campus, 0, pvp.Fighter{10, cafe}, pvp.Fighter{10, zoneB}, pvp.AttackerNotInZone},
		{"target in campus cafe", campus, 0, pvp.Fighter{10, zoneA}, pvp.Fighter{10, cafe}, pvp.TargetNotInZone},
		{"attacker in cafe inside zone", overlap, 0, pvp.Fighter{10, cafeInPvP}, pvp.Fighter{10, nextToCafe}, pvp.AttackerNotInZone},
		{"target in cafe inside zone", overlap, 0, pvp.Fighter{10, nextToCafe}, pvp.Fighter{10, cafeInPvP}, pvp.TargetNotInZone},
		{"both just outside the cafe in zone", overlap, 0, pvp.Fighter{10, nextToCafe}, pvp.Fighter{10, zoneA}, pvp.Allowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dd := d
			if tt.gap != 0 {
				dd = withRules(d, func(r *gamedata.Rules) { r.PvPLevelGap = tt.gap })
			}
			if got := pvp.CanAttack(dd, tt.areas, tt.attacker, tt.target); got != tt.want {
				t.Errorf("CanAttack = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanAttackUsesGamedataGap(t *testing.T) {
	d := loadData(t)
	if d.Rules.PvPLevelGap != 5 {
		t.Fatalf("PvPLevelGap = %d, want the spec's 5", d.Rules.PvPLevelGap)
	}
}

func TestSplitOnDeath(t *testing.T) {
	d := loadData(t)
	var (
		stick   = id(t, d, "wooden_stick")  // 2
		dagger  = id(t, d, "bronze_dagger") // 15
		lock    = id(t, d, "bike_lock")     // 60
		hoodie  = id(t, d, "hoodie")        // 5
		shield  = id(t, d, "cardboard_shield")
		cap     = id(t, d, "leather_cap")   // 10
		helmet  = id(t, d, "bike_helmet")   // 30
		plating = id(t, d, "robot_plating") // 150
		boba    = id(t, d, "boba")          // 1
		acorn   = id(t, d, "acorn")         // 1
		trinket = id(t, d, "shiny_trinket") // 25
		pelt    = id(t, d, "coyote_pelt")   // 12
	)
	worn := func(item gamedata.ItemID, s gamedata.Slot) pvp.Piece {
		return pvp.Piece{Item: item, Qty: 1, Worn: s, Slot: -1}
	}
	slot := func(i int, item gamedata.ItemID, qty int) pvp.Piece {
		return pvp.Piece{Item: item, Qty: qty, Slot: i}
	}
	inv := func(eq world.Equipment, gold int, stacks ...world.Stack) world.Inventory {
		s := make([]world.Stack, 28)
		copy(s, stacks)
		return world.Inventory{Slots: s, Equipment: eq, Gold: gold}
	}
	st := func(item gamedata.ItemID, qty int) world.Stack { return world.Stack{Item: item, Qty: qty} }
	empty := world.Stack{}

	tests := []struct {
		name string
		kept int // Rules.PvPItemsKept; -1 means the gamedata value
		inv  world.Inventory
		want pvp.DeathSplit
	}{
		{
			name: "nothing held",
			kept: -1,
			inv:  inv(world.Equipment{}, 0),
			want: pvp.DeathSplit{},
		},
		{
			name: "starting kit: five units, keeps three",
			kept: -1,
			inv:  inv(world.Equipment{Weapon: stick, Body: hoodie}, 100, st(boba, 3)),
			want: pvp.DeathSplit{
				Kept:    []pvp.Piece{worn(hoodie, gamedata.SlotBody), worn(stick, gamedata.SlotWeapon), slot(0, boba, 1)},
				Dropped: []pvp.Piece{slot(0, boba, 2)},
				Gold:    100,
			},
		},
		{
			name: "worn and carried items ranked together by shop price",
			kept: -1,
			inv: inv(world.Equipment{Weapon: dagger, Head: cap, Body: hoodie, Shield: shield}, 7,
				st(plating, 1), empty, st(lock, 1), st(acorn, 50)),
			want: pvp.DeathSplit{
				Kept: []pvp.Piece{slot(0, plating, 1), slot(2, lock, 1), worn(dagger, gamedata.SlotWeapon)},
				Dropped: []pvp.Piece{
					worn(cap, gamedata.SlotHead), worn(hoodie, gamedata.SlotBody),
					worn(shield, gamedata.SlotShield), slot(3, acorn, 50),
				},
				Gold: 7,
			},
		},
		{
			name: "each unit of a stack counts separately",
			kept: -1,
			inv:  inv(world.Equipment{Head: helmet}, 0, st(trinket, 5), st(pelt, 2)),
			want: pvp.DeathSplit{
				Kept:    []pvp.Piece{worn(helmet, gamedata.SlotHead), slot(0, trinket, 2)},
				Dropped: []pvp.Piece{slot(0, trinket, 3), slot(1, pelt, 2)},
			},
		},
		{
			name: "tie on value: lower item id first",
			kept: -1,
			// boba (id 9) and acorn (id 11) are both worth 1.
			inv: inv(world.Equipment{}, 0, st(acorn, 4), st(boba, 1), st(boba, 1)),
			want: pvp.DeathSplit{
				Kept:    []pvp.Piece{slot(1, boba, 1), slot(2, boba, 1), slot(0, acorn, 1)},
				Dropped: []pvp.Piece{slot(0, acorn, 3)},
			},
		},
		{
			name: "tie on item: worn before carried, then lower slot",
			kept: -1,
			inv:  inv(world.Equipment{Weapon: dagger}, 0, empty, st(dagger, 1), st(dagger, 1), st(dagger, 1)),
			want: pvp.DeathSplit{
				Kept:    []pvp.Piece{worn(dagger, gamedata.SlotWeapon), slot(1, dagger, 1), slot(2, dagger, 1)},
				Dropped: []pvp.Piece{slot(3, dagger, 1)},
			},
		},
		{
			name: "kept count read from gamedata",
			kept: 1,
			inv:  inv(world.Equipment{Weapon: lock}, 3, st(trinket, 2)),
			want: pvp.DeathSplit{
				Kept:    []pvp.Piece{worn(lock, gamedata.SlotWeapon)},
				Dropped: []pvp.Piece{slot(0, trinket, 2)},
				Gold:    3,
			},
		},
		{
			name: "keep none",
			kept: 0,
			inv:  inv(world.Equipment{Weapon: lock}, 3, st(trinket, 2)),
			want: pvp.DeathSplit{
				Dropped: []pvp.Piece{worn(lock, gamedata.SlotWeapon), slot(0, trinket, 2)},
				Gold:    3,
			},
		},
		{
			name: "fewer items than kept count",
			kept: -1,
			inv:  inv(world.Equipment{Body: hoodie}, 0, st(pelt, 1)),
			want: pvp.DeathSplit{
				Kept: []pvp.Piece{slot(0, pelt, 1), worn(hoodie, gamedata.SlotBody)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dd := d
			if tt.kept >= 0 {
				dd = withRules(d, func(r *gamedata.Rules) { r.PvPItemsKept = tt.kept })
			}
			got := pvp.SplitOnDeath(dd, tt.inv)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitOnDeath =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}

// TestSplitOnDeathConserves checks, over many random holdings, that gold is
// never dropped, kept plus dropped equals the original holdings exactly, the
// kept count is min(PvPItemsKept, units held), no dropped unit is worth more
// than a kept one, the input is not changed, and the split is deterministic.
func TestSplitOnDeathConserves(t *testing.T) {
	d := loadData(t)
	r := rand.New(rand.NewPCG(1, 189))
	slots := []gamedata.Slot{gamedata.SlotWeapon, gamedata.SlotHead, gamedata.SlotBody, gamedata.SlotShield}
	for n := range 2000 {
		inv := world.Inventory{Slots: make([]world.Stack, d.Rules.InventorySlots), Gold: r.IntN(10_000)}
		for _, s := range slots {
			if r.IntN(2) == 0 {
				continue
			}
			for {
				it := d.Items[r.IntN(len(d.Items))]
				if it.Slot == s {
					*equip(&inv.Equipment, s) = it.ID
					break
				}
			}
		}
		for i := range inv.Slots {
			if r.IntN(3) == 0 {
				it := d.Items[r.IntN(len(d.Items))]
				qty := 1
				if it.Stackable() {
					qty = 1 + r.IntN(20)
				}
				inv.Slots[i] = world.Stack{Item: it.ID, Qty: qty}
			}
		}
		before := clone(inv)

		got := pvp.SplitOnDeath(d, inv)
		if !reflect.DeepEqual(inv, before) {
			t.Fatalf("case %d: SplitOnDeath changed its input", n)
		}
		if again := pvp.SplitOnDeath(d, clone(inv)); !reflect.DeepEqual(got, again) {
			t.Fatalf("case %d: split not deterministic:\n%+v\n%+v", n, got, again)
		}
		if got.Gold != inv.Gold {
			t.Fatalf("case %d: gold kept %d, want all %d", n, got.Gold, inv.Gold)
		}

		// Rebuild per-source quantities from kept and dropped.
		type key struct {
			worn gamedata.Slot
			slot int
		}
		want := map[key]world.Stack{}
		for _, s := range slots {
			if id := inv.Equipment.Get(s); id != 0 {
				want[key{s, -1}] = world.Stack{Item: id, Qty: 1}
			}
		}
		units := 0
		for i, st := range inv.Slots {
			if !st.Empty() {
				want[key{gamedata.SlotNone, i}] = st
			}
		}
		for _, st := range want {
			units += st.Qty
		}
		have := map[key]world.Stack{}
		keptUnits, minKept, maxDropped := 0, int(^uint(0)>>1), -1
		add := func(p pvp.Piece) {
			if p.Qty <= 0 || p.Item == 0 {
				t.Fatalf("case %d: bad piece %+v", n, p)
			}
			k := key{p.Worn, p.Slot}
			h := have[k]
			if h.Item != 0 && h.Item != p.Item {
				t.Fatalf("case %d: piece %+v disagrees on item", n, p)
			}
			have[k] = world.Stack{Item: p.Item, Qty: h.Qty + p.Qty}
		}
		for _, p := range got.Kept {
			add(p)
			keptUnits += p.Qty
			it, _ := d.Item(p.Item)
			minKept = min(minKept, it.ShopBuysFor)
		}
		for _, p := range got.Dropped {
			add(p)
			it, _ := d.Item(p.Item)
			maxDropped = max(maxDropped, it.ShopBuysFor)
		}
		if !reflect.DeepEqual(have, want) {
			t.Fatalf("case %d: kept+dropped = %v, want %v", n, have, want)
		}
		if wantKept := min(d.Rules.PvPItemsKept, units); keptUnits != wantKept {
			t.Fatalf("case %d: kept %d units, want %d", n, keptUnits, wantKept)
		}
		if len(got.Kept) > 0 && maxDropped > minKept {
			t.Fatalf("case %d: dropped an item worth %d but kept one worth %d", n, maxDropped, minKept)
		}
	}
}

func equip(e *world.Equipment, s gamedata.Slot) *gamedata.ItemID {
	switch s {
	case gamedata.SlotWeapon:
		return &e.Weapon
	case gamedata.SlotHead:
		return &e.Head
	case gamedata.SlotBody:
		return &e.Body
	}
	return &e.Shield
}

func clone(inv world.Inventory) world.Inventory {
	c := inv
	c.Slots = append([]world.Stack(nil), inv.Slots...)
	return c
}

func BenchmarkCanAttack(b *testing.B) {
	d := loadData(b)
	a, err := areas.Load()
	if err != nil {
		b.Fatal(err)
	}
	att := pvp.Fighter{Level: 10, Pos: world.Vec2{X: 250, Y: -350}}
	tgt := pvp.Fighter{Level: 12, Pos: world.Vec2{X: 380, Y: -300}}
	for b.Loop() {
		if pvp.CanAttack(d, a, att, tgt) != pvp.Allowed {
			b.Fatal("not allowed")
		}
	}
}

func BenchmarkSplitOnDeath(b *testing.B) {
	d := loadData(b)
	inv := world.Inventory{Slots: make([]world.Stack, d.Rules.InventorySlots), Gold: 50}
	inv.Equipment = world.Equipment{Weapon: id(b, d, "bike_lock"), Head: id(b, d, "bike_helmet")}
	for i := range inv.Slots {
		inv.Slots[i] = world.Stack{Item: d.Items[i%len(d.Items)].ID, Qty: 1}
	}
	for b.Loop() {
		pvp.SplitOnDeath(d, inv)
	}
}
