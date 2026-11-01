package pvp_test

import (
	"maps"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

const combatEvery = 12 // world ticks per combat tick

// fixedRand rolls the same values every time: Float64 returns f, and IntN
// returns n-1 (the max hit) when max is set, else 0.
type fixedRand struct {
	f   float64
	max bool
}

func (r fixedRand) Float64() float64 { return r.f }
func (r fixedRand) IntN(n int) int {
	if r.max {
		return n - 1
	}
	return 0
}

var alwaysMax = fixedRand{f: 0, max: true}

// timedHit is a hit with the combat tick (1-based) it happened on.
type timedHit struct {
	Tick int
	pvp.Hit
}

type recorder struct {
	w     *world.World
	hits  []timedHit
	drops []pvp.Drop
}

func (r *recorder) PvPHit(h pvp.Hit) {
	r.hits = append(r.hits, timedHit{int(r.w.TickCount()) / combatEvery, h})
}
func (r *recorder) PvPDrop(d pvp.Drop) { r.drops = append(r.drops, d) }

type combatFixture struct {
	d     *gamedata.Data
	zones *areas.Lookup
	w     *world.World
	clock *world.FakeClock
	sys   *pvp.Combat
	rec   *recorder
}

var (
	zoneA     = world.Vec2{X: 250.5, Y: -350.5} // in the PvP zone
	zoneRight = world.Vec2{X: 251.5, Y: -350.5} // one tile east of zoneA
	zoneDiag  = world.Vec2{X: 251.5, Y: -349.5} // one tile diagonally from zoneA
	zoneFar   = world.Vec2{X: 252.5, Y: -350.5} // two tiles east of zoneA
)

func newCombat(t testing.TB, d *gamedata.Data, z *areas.Lookup, r formula.Rand) *combatFixture {
	t.Helper()
	if z == nil {
		var err error
		if z, err = areas.Load(); err != nil {
			t.Fatal(err)
		}
	}
	f := &combatFixture{d: d, zones: z, w: world.New(world.DefaultConfig()), clock: world.NewFakeClock(time.Unix(1000, 0))}
	f.rec = &recorder{w: f.w}
	sys, err := pvp.NewCombat(d, z, 1, f.clock, r, f.rec, f.rec)
	if err != nil {
		t.Fatal(err)
	}
	f.sys = sys
	if err := f.w.RegisterEvery(sys, combatEvery); err != nil {
		t.Fatal(err)
	}
	return f
}

// player adds a player at level with full HP, an empty inventory wearing
// weapon (0 for unarmed) and the given body armour.
func (f *combatFixture) player(t testing.TB, id world.PlayerID, level int, pos world.Vec2, weapon, body gamedata.ItemID) *world.Player {
	t.Helper()
	p := &world.Player{ID: id, Pos: pos, Inv: world.NewInventory(f.d), XP: formula.XPForLevel(level)}
	p.Inv.Equipment = world.Equipment{Weapon: weapon, Body: body}
	p.HP = p.MaxHP(f.d.Rules)
	if err := f.w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// combatTicks runs n combat ticks' worth of world ticks.
func (f *combatFixture) combatTicks(n int) {
	for range n * combatEvery {
		f.w.Tick()
	}
}

func hitTicks(hits []timedHit) []int {
	out := []int{}
	for _, h := range hits {
		out = append(out, h.Tick)
	}
	return out
}

// TestCombatTiming checks that attacks land only on combat ticks, first on
// the combat tick after the command and then every attack-speed combat ticks
// (the weapon's, or Rules.UnarmedAttackSpeed with none).
func TestCombatTiming(t *testing.T) {
	d := loadData(t)
	tests := []struct {
		name   string
		weapon string
		want   []int
	}{
		{"unarmed", "", []int{1, 1 + d.Rules.UnarmedAttackSpeed, 1 + 2*d.Rules.UnarmedAttackSpeed}},
		{"bronze dagger, speed 3", "bronze_dagger", []int{1, 4, 7, 10}},
		{"bike lock, speed 5", "bike_lock", []int{1, 6, 11}},
	}
	if d.Rules.UnarmedAttackSpeed != 4 {
		t.Fatalf("UnarmedAttackSpeed = %d, want the spec's 4", d.Rules.UnarmedAttackSpeed)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCombat(t, d, nil, rand.New(rand.NewPCG(1, 2)))
			var weapon gamedata.ItemID
			if tt.weapon != "" {
				weapon = id(t, d, tt.weapon)
			}
			f.player(t, 1, 15, zoneA, weapon, 0)
			f.player(t, 2, 15, zoneRight, 0, 0).HP = 1000 // never dies here
			f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
			for wt := 1; wt <= 12*combatEvery; wt++ {
				before := len(f.rec.hits)
				f.w.Tick()
				if len(f.rec.hits) != before && wt%combatEvery != 0 {
					t.Fatalf("attack on world tick %d, not a combat tick", wt)
				}
			}
			if got := hitTicks(f.rec.hits); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("attacks on combat ticks %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCombatRefused checks range (diagonals included) and that CanAttack
// refusals (outside the zone, in the safe circle, outside the level gap)
// stop every attack.
func TestCombatRefused(t *testing.T) {
	d := loadData(t)
	campus, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	overlap, err := areas.Parse([]byte(overlapAreas))
	if err != nil {
		t.Fatal(err)
	}
	safeInPvP := overlap.Spawn()
	nextToSafe := world.Vec2{X: safeInPvP.X + 1, Y: safeInPvP.Y}
	tests := []struct {
		name               string
		zones              *areas.Lookup
		attLevel, tgtLevel int
		attPos, tgtPos     world.Vec2
		wantHit            bool
	}{
		{"adjacent", campus, 10, 10, zoneA, zoneRight, true},
		{"diagonal", campus, 10, 10, zoneA, zoneDiag, true},
		{"same tile", campus, 10, 10, zoneA, zoneA, true},
		{"two tiles away", campus, 10, 10, zoneA, zoneFar, false},
		{"5 levels apart", campus, 15, 10, zoneA, zoneRight, true},
		{"6 levels apart", campus, 16, 10, zoneA, zoneRight, false},
		{"6 levels apart, other way", campus, 4, 10, zoneA, zoneRight, false},
		{"both outside the zone", campus, 10, 10, campus.Spawn(), world.Vec2{X: campus.Spawn().X + 1, Y: campus.Spawn().Y}, false},
		{"attacker in safe circle inside zone", overlap, 10, 10, safeInPvP, nextToSafe, false},
		{"target in safe circle inside zone", overlap, 10, 10, nextToSafe, safeInPvP, false},
		{"attacker in cafe inside zone", overlap, 10, 10, cafeInPvP, nextToCafe, false},
		{"target in cafe inside zone", overlap, 10, 10, nextToCafe, cafeInPvP, false},
		{"both next to cafe inside zone", overlap, 10, 10, nextToCafe, world.Vec2{X: nextToCafe.X + 1, Y: nextToCafe.Y}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCombat(t, d, tt.zones, alwaysMax)
			a := f.player(t, 1, tt.attLevel, tt.attPos, 0, 0)
			v := f.player(t, 2, tt.tgtLevel, tt.tgtPos, 0, 0)
			hp, xp := v.HP, a.XP
			f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
			f.combatTicks(9)
			if got := len(f.rec.hits) > 0; got != tt.wantHit {
				t.Fatalf("attacked = %v, want %v (hits %+v)", got, tt.wantHit, f.rec.hits)
			}
			if !tt.wantHit && (v.HP != hp || a.XP != xp || !a.LastCombat.IsZero() || !v.LastCombat.IsZero()) {
				t.Errorf("refused attack changed state: HP %d→%d, XP %d→%d", hp, v.HP, xp, a.XP)
			}
		})
	}
}

// TestCombatOnlyOnCommand checks that nobody attacks without a command, and
// that attacking oneself is ignored.
func TestCombatOnlyOnCommand(t *testing.T) {
	d := loadData(t)
	f := newCombat(t, d, nil, alwaysMax)
	f.player(t, 1, 10, zoneA, 0, 0)
	f.player(t, 2, 10, zoneRight, 0, 0)
	f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 1})
	f.combatTicks(5)
	if len(f.rec.hits) != 0 {
		t.Errorf("hits %+v, want none", f.rec.hits)
	}
}

// TestCombatTargets covers AC2: a new command replaces the target,
// ClearTarget stops attacking, and a target that died or left is dropped.
func TestCombatTargets(t *testing.T) {
	d := loadData(t)
	tests := []struct {
		name string
		// after runs after the first attack on player 2.
		after    func(f *combatFixture)
		wantTo   []world.PlayerID // targets of the attacks after the first
		wantKeep bool             // player 1 still has a target at the end
	}{
		{"keeps attacking", func(*combatFixture) {}, []world.PlayerID{2, 2}, true},
		{"new command replaces target", func(f *combatFixture) {
			f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 3})
		}, []world.PlayerID{3, 3}, true},
		{"cleared", func(f *combatFixture) { f.sys.ClearTarget(1) }, nil, false},
		{"cleared then attack again", func(f *combatFixture) {
			f.sys.ClearTarget(1)
			f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
		}, []world.PlayerID{2, 2}, true},
		{"target left the world", func(f *combatFixture) { f.w.RemovePlayer(2) }, nil, false},
		{"target died elsewhere", func(f *combatFixture) { f.w.Player(2).HP = 0 }, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCombat(t, d, nil, fixedRand{f: 0.999}) // always misses
			f.player(t, 1, 10, zoneA, 0, 0)
			f.player(t, 2, 10, zoneRight, 0, 0)
			f.player(t, 3, 10, zoneDiag, 0, 0)
			f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
			f.combatTicks(1)
			if len(f.rec.hits) != 1 || f.rec.hits[0].Target != 2 {
				t.Fatalf("first hits = %+v, want one on player 2", f.rec.hits)
			}
			tt.after(f)
			f.combatTicks(2 * d.Rules.UnarmedAttackSpeed)
			var to []world.PlayerID
			for _, h := range f.rec.hits[1:] {
				if h.Attacker != 1 {
					t.Errorf("unexpected attacker in %+v", h)
				}
				to = append(to, h.Target)
			}
			if !reflect.DeepEqual(to, tt.wantTo) {
				t.Errorf("later attacks on %v, want %v", to, tt.wantTo)
			}
			if _, ok := f.sys.TargetOf(1); ok != tt.wantKeep {
				t.Errorf("has target = %v, want %v", ok, tt.wantKeep)
			}
		})
	}
}

// TestCombatAttackerLeft checks that an attacker who leaves the world loses
// their target, so they don't resume attacking when their id rejoins.
func TestCombatAttackerLeft(t *testing.T) {
	d := loadData(t)
	f := newCombat(t, d, nil, fixedRand{f: 0.999})
	a := f.player(t, 1, 10, zoneA, 0, 0)
	f.player(t, 2, 10, zoneRight, 0, 0)
	f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
	f.combatTicks(1)
	f.w.RemovePlayer(1)
	f.combatTicks(1)
	if err := f.w.AddPlayer(a); err != nil {
		t.Fatal(err)
	}
	f.combatTicks(2 * d.Rules.UnarmedAttackSpeed)
	if len(f.rec.hits) != 1 {
		t.Errorf("hits %+v, want only the one before leaving", f.rec.hits)
	}
	if _, ok := f.sys.TargetOf(1); ok {
		t.Error("rejoined attacker still has a target")
	}
}

// TestCombatCooldownSurvivesRetarget checks that switching or clearing the
// target doesn't reset the attack timer.
func TestCombatCooldownSurvivesRetarget(t *testing.T) {
	d := loadData(t)
	f := newCombat(t, d, nil, fixedRand{f: 0.999})
	f.player(t, 1, 10, zoneA, 0, 0)
	f.player(t, 2, 10, zoneRight, 0, 0)
	f.player(t, 3, 10, zoneDiag, 0, 0)
	f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
	f.combatTicks(1)
	f.sys.ClearTarget(1)
	f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 3})
	f.combatTicks(8)
	if got, want := hitTicks(f.rec.hits), []int{1, 5, 9}; !reflect.DeepEqual(got, want) {
		t.Errorf("attacks on combat ticks %v, want %v", got, want)
	}
}

// TestCombatHit checks HP loss, XP, MarkCombat on both players, and that
// equipment bonuses on both sides feed the hit chance and max hit.
func TestCombatHit(t *testing.T) {
	d := loadData(t)
	stick, hoodie, lock := id(t, d, "wooden_stick"), id(t, d, "hoodie"), id(t, d, "bike_lock")
	stats := func(key string) gamedata.Stats {
		it, _ := d.ItemByKey(key)
		return it.Stats
	}
	// Between the stick's hit chance against no armour and against a hoodie.
	bare := formula.HitChance(10, stats("wooden_stick").Accuracy, 10, 0)
	armoured := formula.HitChance(10, stats("wooden_stick").Accuracy, 10, stats("hoodie").Defence)
	between := (bare + armoured) / 2
	tests := []struct {
		name         string
		weapon, body gamedata.ItemID
		r            fixedRand
		wantDmg      int
	}{
		{"max hit unarmed", 0, 0, alwaysMax, formula.MaxHit(10, 0)},
		{"max hit with stick", stick, 0, alwaysMax, formula.MaxHit(10, stats("wooden_stick").Strength)},
		{"max hit with bike lock", lock, 0, alwaysMax, formula.MaxHit(10, stats("bike_lock").Strength)},
		{"min hit", stick, 0, fixedRand{f: 0}, 1},
		{"miss", stick, 0, fixedRand{f: 0.999}, 0},
		{"hits unarmoured target", stick, 0, fixedRand{f: between, max: true}, formula.MaxHit(10, stats("wooden_stick").Strength)},
		{"misses armoured target", stick, hoodie, fixedRand{f: between, max: true}, 0},
	}
	if !(armoured < bare) {
		t.Fatalf("hoodie doesn't lower hit chance: %v vs %v", armoured, bare)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newCombat(t, d, nil, tt.r)
			a := f.player(t, 1, 10, zoneA, tt.weapon, 0)
			v := f.player(t, 2, 10, zoneRight, 0, tt.body)
			hp, xp := v.HP, a.XP
			f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
			f.combatTicks(1)
			want := []timedHit{{1, pvp.Hit{Attacker: 1, Target: 2, Damage: tt.wantDmg}}}
			if !reflect.DeepEqual(f.rec.hits, want) {
				t.Fatalf("hits = %+v, want %+v", f.rec.hits, want)
			}
			if v.HP != hp-tt.wantDmg {
				t.Errorf("target HP = %d, want %d", v.HP, hp-tt.wantDmg)
			}
			if a.XP != xp+d.Rules.XPPerDamage*tt.wantDmg {
				t.Errorf("attacker XP = %d, want %d", a.XP, xp+d.Rules.XPPerDamage*tt.wantDmg)
			}
			now := f.clock.Now()
			if !a.LastCombat.Equal(now) || !v.LastCombat.Equal(now) {
				t.Errorf("LastCombat = %v / %v, want %v on both", a.LastCombat, v.LastCombat, now)
			}
		})
	}
}

// TestCombatEatDelay checks that an AttackDelay set by eating skips the next
// due attack: it is decremented instead, and the attack follows one combat
// tick later.
func TestCombatEatDelay(t *testing.T) {
	d := loadData(t)
	if d.Rules.EatAttackDelayTicks != 1 {
		t.Fatalf("EatAttackDelayTicks = %d, want the spec's 1", d.Rules.EatAttackDelayTicks)
	}
	f := newCombat(t, d, nil, rand.New(rand.NewPCG(7, 7)))
	a := f.player(t, 1, 10, zoneA, 0, 0)
	f.player(t, 2, 10, zoneRight, 0, 0).HP = 1000
	f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
	f.combatTicks(2) // attacks on 1; next due on 5
	a.AttackDelay = d.Rules.EatAttackDelayTicks
	f.combatTicks(2) // 3, 4: not due, delay untouched
	if a.AttackDelay != 1 {
		t.Fatalf("AttackDelay = %d before the attack was due, want 1", a.AttackDelay)
	}
	f.combatTicks(1) // 5: due, skipped
	if a.AttackDelay != 0 || len(f.rec.hits) != 1 {
		t.Fatalf("on the due tick: AttackDelay %d, hits %d; want 0 and 1", a.AttackDelay, len(f.rec.hits))
	}
	f.combatTicks(5)
	if got, want := hitTicks(f.rec.hits), []int{1, 6, 10}; !reflect.DeepEqual(got, want) {
		t.Errorf("attacks on combat ticks %v, want %v", got, want)
	}
}

// TestCombatFight checks the hit events of a short seeded fight in which
// both players attack until one dies.
func TestCombatFight(t *testing.T) {
	d := loadData(t)
	f := newCombat(t, d, nil, rand.New(rand.NewPCG(1, 1)))
	a := f.player(t, 1, 10, zoneA, id(t, d, "bronze_dagger"), 0)
	v := f.player(t, 2, 10, zoneRight, 0, id(t, d, "hoodie"))
	v.HP = 5
	f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
	f.w.Enqueue(pvp.AttackCommand{Player: 2, Victim: 1})
	f.combatTicks(30)

	// Replay the same rolls to get the expected events.
	r := rand.New(rand.NewPCG(1, 1))
	dagger, _ := d.ItemByKey("bronze_dagger")
	hoodie, _ := d.ItemByKey("hoodie")
	hpA, hpV := a.MaxHP(d.Rules), 5
	var want []timedHit
	nextA, nextV := 1, 1
	for tick := 1; tick <= 30; tick++ {
		if tick == nextA {
			dmg := min(formula.RollDamage(r, formula.Attack{AttackerLevel: 10, Accuracy: dagger.Stats.Accuracy, Strength: dagger.Stats.Strength, DefenderLevel: 10, Defence: hoodie.Stats.Defence}), hpV)
			hpV -= dmg
			want = append(want, timedHit{tick, pvp.Hit{Attacker: 1, Target: 2, Damage: dmg, Died: hpV == 0}})
			nextA += dagger.AttackSpeed
			if hpV == 0 {
				break
			}
		}
		if tick == nextV {
			dmg := min(formula.RollDamage(r, formula.Attack{AttackerLevel: 10, DefenderLevel: 10}), hpA)
			hpA -= dmg
			want = append(want, timedHit{tick, pvp.Hit{Attacker: 2, Target: 1, Damage: dmg, Died: hpA == 0}})
			nextV += d.Rules.UnarmedAttackSpeed
		}
	}
	if len(want) < 3 || !want[len(want)-1].Died {
		t.Fatalf("seed doesn't give a short fight ending in a death: %+v", want)
	}
	if !reflect.DeepEqual(f.rec.hits, want) {
		t.Errorf("hits =\n  %+v\nwant\n  %+v", f.rec.hits, want)
	}
	if a.HP != hpA {
		t.Errorf("winner HP = %d, want %d", a.HP, hpA)
	}
	if _, ok := f.sys.TargetOf(1); ok {
		t.Error("killer still targets the dead player")
	}
	if _, ok := f.sys.TargetOf(2); ok {
		t.Error("dead player still has a target")
	}
}

// deathFixture sets up player 2 with mixed holdings, one hit from death at
// the hands of player 1.
func deathFixture(t *testing.T) (*combatFixture, *world.Player, *world.Player) {
	t.Helper()
	d := loadData(t)
	f := newCombat(t, d, nil, alwaysMax)
	a := f.player(t, 1, 10, zoneA, 0, 0)
	v := f.player(t, 2, 12, zoneDiag, id(t, d, "bike_lock"), id(t, d, "robot_plating"))
	v.Inv.Equipment.Head = id(t, d, "bike_helmet")
	v.Inv.Gold = 321
	v.Inv.Slots[0] = world.Stack{Item: id(t, d, "shiny_trinket"), Qty: 4}
	v.Inv.Slots[3] = world.Stack{Item: id(t, d, "burrito"), Qty: 1}
	v.Inv.Slots[7] = world.Stack{Item: id(t, d, "scrap_metal"), Qty: 9}
	v.HP = 1
	v.Desired, v.HasDesired = zoneFar, true
	f.player(t, 3, 10, zoneRight, 0, 0) // also attacking player 2
	return f, a, v
}

func sumStacks(m map[gamedata.ItemID]int, stacks []world.Stack) map[gamedata.ItemID]int {
	m = maps.Clone(m)
	for _, s := range stacks {
		m[s.Item] += s.Qty
	}
	return m
}

func checkRespawn(t *testing.T, f *combatFixture, v *world.Player) {
	t.Helper()
	if v.Pos != f.zones.Spawn() {
		t.Errorf("respawned at %v, want spawn %v", v.Pos, f.zones.Spawn())
	}
	if v.HasDesired {
		t.Error("dead player still walking")
	}
	if v.HP != v.MaxHP(f.d.Rules) {
		t.Errorf("HP after respawn = %d, want %d", v.HP, v.MaxHP(f.d.Rules))
	}
	for _, id := range []world.PlayerID{1, 3} {
		if _, ok := f.sys.TargetOf(id); ok {
			t.Errorf("player %d still targets the dead player", id)
		}
	}
}

// TestCombatDeath checks a PvP death: ApplyDeath on the same tick, its
// changes in the economy outbox under ReasonPvPDeath, the dropped stacks in
// the drop sink owned by the killer at the death position, conservation of
// holdings, and the respawn.
func TestCombatDeath(t *testing.T) {
	f, a, v := deathFixture(t)
	before := holdings(v.Inv)
	deathPos, gold := v.Pos, v.Inv.Gold
	f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
	f.w.Enqueue(pvp.AttackCommand{Player: 3, Victim: 2})
	f.combatTicks(1)

	want := []timedHit{{1, pvp.Hit{Attacker: 1, Target: 2, Damage: 1, Died: true}}}
	if !reflect.DeepEqual(f.rec.hits, want) {
		t.Fatalf("hits = %+v, want %+v (player 3 must not hit the respawned player)", f.rec.hits, want)
	}
	if len(f.rec.drops) != 1 {
		t.Fatalf("drops = %+v, want one", f.rec.drops)
	}
	drop := f.rec.drops[0]
	if drop.Owner != a.ID || drop.Pos != deathPos {
		t.Errorf("drop owner %d at %v, want %d at %v", drop.Owner, drop.Pos, a.ID, deathPos)
	}
	if got := sumStacks(holdings(v.Inv), drop.Stacks); !maps.Equal(got, before) {
		t.Errorf("kept + dropped = %v, want %v", got, before)
	}
	if n := len(holdings(v.Inv)); n == 0 || len(drop.Stacks) == 0 {
		t.Errorf("expected both kept (%d kinds) and dropped (%d stacks) items", n, len(drop.Stacks))
	}
	if v.Inv.Gold != gold {
		t.Errorf("gold = %d, want %d", v.Inv.Gold, gold)
	}

	events := v.PendingEconomy()
	if len(events) != 1 || events[0].Reason != pvp.ReasonPvPDeath {
		t.Fatalf("economy events = %+v, want one %q", events, pvp.ReasonPvPDeath)
	}
	if len(events[0].Changes) != len(drop.Stacks) {
		t.Errorf("%d changes for %d dropped stacks", len(events[0].Changes), len(drop.Stacks))
	}
	for i, c := range events[0].Changes {
		if c.Item != drop.Stacks[i].Item || c.Qty != drop.Stacks[i].Qty {
			t.Errorf("change %d = %+v, dropped %+v", i, c, drop.Stacks[i])
		}
	}
	if !reflect.DeepEqual(events[0].Inv, v.Inv) {
		t.Errorf("outbox inventory differs from the player's")
	}
	checkRespawn(t, f, v)

	// The killer doesn't attack again; the respawned player is out of reach.
	f.combatTicks(10)
	if len(f.rec.hits) != 1 {
		t.Errorf("attacks after the death: %+v", f.rec.hits[1:])
	}
}

// TestCombatDeathLocked checks a death while the dead player's inventory is
// locked by a trade being saved: nothing is dropped or recorded, nothing is
// lost, and the player still respawns with full HP.
func TestCombatDeathLocked(t *testing.T) {
	f, _, v := deathFixture(t)
	v.Inv.SetLocked(true)
	before := holdings(v.Inv)
	gold := v.Inv.Gold
	f.w.Enqueue(pvp.AttackCommand{Player: 1, Victim: 2})
	f.combatTicks(1)

	if len(f.rec.hits) != 1 || !f.rec.hits[0].Died {
		t.Fatalf("hits = %+v, want one killing hit", f.rec.hits)
	}
	var dropped []world.Stack
	for _, d := range f.rec.drops {
		dropped = append(dropped, d.Stacks...)
	}
	if got := sumStacks(holdings(v.Inv), dropped); !maps.Equal(got, before) {
		t.Errorf("held + dropped = %v, want %v", got, before)
	}
	if v.Inv.Gold != gold {
		t.Errorf("gold = %d, want %d", v.Inv.Gold, gold)
	}
	if ev := v.PendingEconomy(); len(ev) != 0 {
		t.Errorf("economy events %+v, want none", ev)
	}
	if !v.Inv.Locked() {
		t.Error("inventory unlocked by the death")
	}
	checkRespawn(t, f, v)
}

func TestNewCombatRejects(t *testing.T) {
	d := loadData(t)
	z, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	clock := world.NewFakeClock(time.Unix(0, 0))
	rec := &recorder{}
	for _, size := range []float64{0, -1} {
		if _, err := pvp.NewCombat(d, z, size, clock, alwaysMax, rec, rec); err == nil {
			t.Errorf("tile size %v accepted", size)
		}
	}
	if _, err := pvp.NewCombat(d, z, 1, clock, nil, rec, rec); err == nil {
		t.Error("nil rand accepted")
	}
}

func BenchmarkCombatTick(b *testing.B) {
	d := loadData(b)
	f := newCombat(b, d, nil, rand.New(rand.NewPCG(1, 1)))
	const n = 200
	for i := range n {
		pos := world.Vec2{X: zoneA.X + float32(i%20), Y: zoneA.Y + float32(i/20)}
		f.player(b, world.PlayerID(i+1), 10, pos, 0, 0).HP = 1 << 30
	}
	for i := range n {
		f.w.Enqueue(pvp.AttackCommand{Player: world.PlayerID(i + 1), Victim: world.PlayerID((i+1)%n + 1)})
	}
	f.w.Tick()
	b.ReportAllocs()
	for b.Loop() {
		f.sys.Tick(f.w, 0)
	}
}
