package combat_test

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat"
	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/npc"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

const combatEvery = 12 // world ticks per combat tick

// fixedRand rolls the same values every time: Float64 returns f, and IntN
// returns n-1 (the max hit).
type fixedRand struct{ f float64 }

func (r fixedRand) Float64() float64 { return r.f }
func (r fixedRand) IntN(n int) int   { return n - 1 }

var (
	alwaysMax  = fixedRand{f: 0}      // every swing hits for the max hit
	alwaysMiss = fixedRand{f: 0.9999} // every swing misses
)

// event is one call on the sink, with the world tick it happened on.
type event struct {
	WorldTick uint64
	Hit       *combat.Hit
	Kill      *combat.Kill
	Death     *combat.Death
}

type recorder struct {
	w      *world.World
	events []event
}

func (r *recorder) Hit(h combat.Hit) {
	r.events = append(r.events, event{WorldTick: r.w.TickCount(), Hit: &h})
}
func (r *recorder) NPCKilled(k combat.Kill) {
	r.events = append(r.events, event{WorldTick: r.w.TickCount(), Kill: &k})
}
func (r *recorder) PlayerDied(d combat.Death) {
	r.events = append(r.events, event{WorldTick: r.w.TickCount(), Death: &d})
}

// hits returns the recorded hits, by players (byNPC false) or by NPCs.
func (r *recorder) hits(byNPC bool) []combat.Hit {
	var out []combat.Hit
	for _, e := range r.events {
		if e.Hit != nil && e.Hit.ByNPC == byNPC {
			out = append(out, *e.Hit)
		}
	}
	return out
}

type fixture struct {
	d     *gamedata.Data
	zones *areas.Lookup
	w     *world.World
	clock *world.FakeClock
	sys   *combat.System
	rec   *recorder
}

// newFixture builds a world with the PvE combat system on the combat tick.
// With walk set, the NPC movement system is registered too (every tick,
// before combat), so NPCs follow the goals combat gives them.
func newFixture(t testing.TB, r formula.Rand, walk bool) *fixture {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	z, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{d: d, zones: z, w: world.New(world.DefaultConfig()), clock: world.NewFakeClock(time.Unix(1000, 0))}
	f.rec = &recorder{w: f.w}
	if walk {
		ns, err := npc.New(npc.Config{WalkSpeed: 4, LeashTiles: d.Rules.LeashTiles, TileSize: 1, Areas: z})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.w.Register(ns); err != nil {
			t.Fatal(err)
		}
	}
	sys, err := combat.New(d, z, 1, f.clock, r, f.rec)
	if err != nil {
		t.Fatal(err)
	}
	f.sys = sys
	if err := f.w.RegisterEvery(sys, combatEvery); err != nil {
		t.Fatal(err)
	}
	return f
}

// field is a spot well outside the safe circle, in the middle of a tile.
func (f *fixture) field(dx, dy float32) world.Vec2 {
	s := f.zones.Spawn()
	return world.Vec2{X: float32(int(s.X)) + 100.5 + dx, Y: float32(int(s.Y)) + 0.5 + dy}
}

func (f *fixture) item(t testing.TB, key string) gamedata.ItemID {
	t.Helper()
	if key == "" {
		return 0
	}
	it, ok := f.d.ItemByKey(key)
	if !ok {
		t.Fatalf("no item %q", key)
	}
	return it.ID
}

// player adds a player at level with full HP wearing weapon ("" for none).
func (f *fixture) player(t testing.TB, id world.PlayerID, level int, pos world.Vec2, weapon string) *world.Player {
	t.Helper()
	p := &world.Player{ID: id, Pos: pos, Inv: world.NewInventory(f.d), XP: formula.XPForLevel(level)}
	p.Inv.Equipment = world.Equipment{Weapon: f.item(t, weapon)}
	p.HP = p.MaxHP(f.d.Rules)
	if err := f.w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// npc adds an NPC of type key with full HP at pos, home there.
func (f *fixture) npc(t testing.TB, key string, pos world.Vec2) *world.NPC {
	t.Helper()
	nt, ok := f.d.NPCByKey(key)
	if !ok {
		t.Fatalf("no NPC %q", key)
	}
	n := &world.NPC{Type: world.NPCTypeID(nt.ID), Pos: pos, Home: pos, HP: nt.HP}
	if _, err := f.w.AddNPC(n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) attack(p *world.Player, n *world.NPC) {
	f.w.Enqueue(combat.AttackCommand{Player: p.ID, NPC: n.ID})
}

// combatTicks runs n combat ticks' worth of world ticks.
func (f *fixture) combatTicks(n int) {
	for range n * combatEvery {
		f.w.Tick()
	}
}

func TestNewRejectsBadArgs(t *testing.T) {
	d, _ := gamedata.Default()
	z, _ := areas.Load()
	c := world.NewFakeClock(time.Unix(0, 0))
	r := rand.New(rand.NewPCG(1, 2))
	rec := &recorder{}
	tests := []struct {
		name string
		fn   func() (*combat.System, error)
	}{
		{"nil data", func() (*combat.System, error) { return combat.New(nil, z, 1, c, r, rec) }},
		{"nil zones", func() (*combat.System, error) { return combat.New(d, nil, 1, c, r, rec) }},
		{"nil clock", func() (*combat.System, error) { return combat.New(d, z, 1, nil, r, rec) }},
		{"nil rand", func() (*combat.System, error) { return combat.New(d, z, 1, c, nil, rec) }},
		{"nil sink", func() (*combat.System, error) { return combat.New(d, z, 1, c, r, nil) }},
		{"zero tile", func() (*combat.System, error) { return combat.New(d, z, 0, c, r, rec) }},
	}
	for _, tt := range tests {
		if _, err := tt.fn(); err == nil {
			t.Errorf("%s: no error", tt.name)
		}
	}
}

// TestAttackTimingAndRange checks that player attacks happen only on combat
// ticks, first on the combat tick after the command and then every
// attack-speed combat ticks, and only within Rules.AttackRangeTiles
// (diagonals included).
func TestAttackTimingAndRange(t *testing.T) {
	tests := []struct {
		name   string
		weapon string
		dx, dy float32
		want   []uint64 // combat ticks (1-based) with a player swing
	}{
		{"unarmed, adjacent", "", 1, 0, []uint64{1, 5, 9}},
		{"unarmed, same tile", "", 0, 0, []uint64{1, 5, 9}},
		{"unarmed, diagonal", "", 1, 1, []uint64{1, 5, 9}},
		{"bronze dagger, speed 3", "bronze_dagger", 0, -1, []uint64{1, 4, 7, 10}},
		{"bike lock, speed 5", "bike_lock", -1, 1, []uint64{1, 6, 11}},
		{"two tiles away", "", 2, 0, nil},
		{"two tiles diagonal", "", 2, 2, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, alwaysMiss, false)
			if f.d.Rules.UnarmedAttackSpeed != 4 || f.d.Rules.AttackRangeTiles != 1 || !f.d.Rules.AttackRangeDiagonal {
				t.Fatalf("rules changed from the spec: %+v", f.d.Rules)
			}
			p := f.player(t, 1, 10, f.field(0, 0), tt.weapon)
			n := f.npc(t, "rabbit", f.field(tt.dx, tt.dy))
			f.attack(p, n)
			f.combatTicks(11)

			var got []uint64
			for _, e := range f.rec.events {
				if e.Hit == nil || e.Hit.ByNPC {
					continue
				}
				if e.WorldTick%combatEvery != 0 {
					t.Errorf("swing on world tick %d, not a combat tick", e.WorldTick)
				}
				got = append(got, e.WorldTick/combatEvery)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("swing combat ticks = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDamageAndXP checks hits against formula.RollDamage with the same
// seeded rolls and equipment bonuses from Equipment.Bonuses (NPC stats from
// the NPC table), the XP gained and both sides' last-combat time.
func TestDamageAndXP(t *testing.T) {
	tests := []struct {
		name   string
		level  int
		weapon string
		npc    string
	}{
		{"level 1 unarmed vs coyote", 1, "", "coyote"},
		{"level 10 stick vs robot", 10, "wooden_stick", "rogue_delivery_robot"},
		{"level 30 bike lock vs alpha", 30, "bike_lock", "coyote_alpha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const seed1, seed2 = 7, 11
			f := newFixture(t, rand.New(rand.NewPCG(seed1, seed2)), false)
			p := f.player(t, 1, tt.level, f.field(0, 0), tt.weapon)
			p.Inv.Equipment.Body = f.item(t, "hoodie")
			n := f.npc(t, tt.npc, f.field(1, 0))
			nt, _ := f.d.NPCByKey(tt.npc)
			hp0, xp0 := p.HP, p.XP
			f.attack(p, n)
			f.combatTicks(1)

			// Replay the rolls: the player swings first, then the NPC fights back.
			r := rand.New(rand.NewPCG(seed1, seed2))
			att, _ := p.Inv.Equipment.Bonuses(f.d)
			pd := formula.RollDamage(r, formula.Attack{AttackerLevel: tt.level, Accuracy: att.Accuracy, Strength: att.Strength, DefenderLevel: nt.Level, Defence: nt.Defence})
			nd := formula.RollDamage(r, formula.Attack{AttackerLevel: nt.Level, Accuracy: nt.Accuracy, Strength: nt.Strength, DefenderLevel: tt.level, Defence: att.Defence})

			want := []combat.Hit{{Player: p.ID, NPC: n.ID, Damage: pd}, {Player: p.ID, NPC: n.ID, ByNPC: true, Damage: nd}}
			got := append(f.rec.hits(false), f.rec.hits(true)...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("hits = %+v, want %+v", got, want)
			}
			if n.HP != nt.HP-pd {
				t.Errorf("NPC HP = %d, want %d", n.HP, nt.HP-pd)
			}
			if p.HP != hp0-nd {
				t.Errorf("player HP = %d, want %d", p.HP, hp0-nd)
			}
			if gain := p.XP - xp0; gain != pd*f.d.Rules.XPPerDamage {
				t.Errorf("XP gained = %d, want %d", gain, pd*f.d.Rules.XPPerDamage)
			}
			now := f.clock.Now()
			if !p.LastCombat.Equal(now) || !n.LastCombat.Equal(now) {
				t.Errorf("last combat = %v / %v, want %v", p.LastCombat, n.LastCombat, now)
			}
		})
	}
}

// TestMaxHitDamage checks a guaranteed max hit deals formula.MaxHit and
// gives Rules.XPPerDamage XP per point.
func TestMaxHitDamage(t *testing.T) {
	f := newFixture(t, alwaysMax, false)
	p := f.player(t, 1, 30, f.field(0, 0), "bike_lock")
	n := f.npc(t, "coyote_alpha", f.field(1, 1))
	nt, _ := f.d.NPCByKey("coyote_alpha")
	f.attack(p, n)
	xp0 := p.XP
	f.combatTicks(1)
	att, _ := p.Inv.Equipment.Bonuses(f.d)
	max := formula.MaxHit(30, att.Strength)
	if n.HP != nt.HP-max {
		t.Errorf("NPC HP = %d, want %d", n.HP, nt.HP-max)
	}
	if p.XP-xp0 != max*4 {
		t.Errorf("XP gained = %d, want %d", p.XP-xp0, max*4)
	}
}

// TestPassiveFightsBackOnlyWhenAttacked checks a passive NPC next to a
// player leaves them alone until hit, then fights back.
func TestPassiveFightsBackOnlyWhenAttacked(t *testing.T) {
	f := newFixture(t, alwaysMiss, false)
	p := f.player(t, 1, 10, f.field(0, 0), "")
	n := f.npc(t, "squirrel", f.field(1, 0))
	f.combatTicks(5)
	if len(f.rec.events) != 0 {
		t.Fatalf("passive NPC made events before being attacked: %+v", f.rec.events)
	}
	if _, ok := f.sys.NPCTargetOf(n.ID); ok {
		t.Fatal("passive NPC has a target before being attacked")
	}
	f.attack(p, n)
	f.combatTicks(1)
	if got, ok := f.sys.NPCTargetOf(n.ID); !ok || got != p.ID {
		t.Fatalf("NPC target = %v, %v; want player %d", got, ok, p.ID)
	}
	if len(f.rec.hits(true)) != 1 {
		t.Errorf("NPC hits = %d, want 1 (it fights back)", len(f.rec.hits(true)))
	}
}

// TestAggression checks an aggressive NPC targets a player within
// Rules.AggressionRangeTiles (but not in the safe circle), and a passive one
// never does.
func TestAggression(t *testing.T) {
	f0 := newFixture(t, alwaysMiss, false)
	reach := float32(f0.d.Rules.AggressionRangeTiles)
	if reach != 4 {
		t.Fatalf("AggressionRangeTiles = %v, want the spec's 4", reach)
	}
	tests := []struct {
		name   string
		npc    string
		dx, dy float32
		want   bool
	}{
		{"aggressive, at range", "coyote", reach, 0, true},
		{"aggressive, diagonal at range", "rogue_delivery_robot", reach, -reach, true},
		{"aggressive, adjacent", "coyote", 1, 0, true},
		{"aggressive, out of range", "coyote", reach + 1, 0, false},
		{"passive, adjacent", "rabbit", 1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, alwaysMiss, false)
			p := f.player(t, 1, 5, f.field(0, 0), "")
			n := f.npc(t, tt.npc, f.field(tt.dx, tt.dy))
			f.combatTicks(1)
			got, ok := f.sys.NPCTargetOf(n.ID)
			if ok != tt.want || (ok && got != p.ID) {
				t.Errorf("NPC target = %v, %v; want targeted=%v", got, ok, tt.want)
			}
		})
	}

	t.Run("player in safe circle", func(t *testing.T) {
		f := newFixture(t, alwaysMiss, false)
		s, r := f.zones.Spawn(), float32(f.zones.SafeRadius())
		f.player(t, 1, 5, world.Vec2{X: s.X + r - 1, Y: s.Y}, "")
		n := f.npc(t, "coyote", world.Vec2{X: s.X + r + 1, Y: s.Y})
		f.combatTicks(3)
		if _, ok := f.sys.NPCTargetOf(n.ID); ok {
			t.Error("aggressive NPC targeted a player in the safe circle")
		}
		if len(f.rec.events) != 0 {
			t.Errorf("events = %+v, want none", f.rec.events)
		}
	})
}

// TestNPCWalksToTarget checks an aggressive NPC walks toward its target with
// the NPC system's goal step and attacks once in range.
func TestNPCWalksToTarget(t *testing.T) {
	f := newFixture(t, alwaysMiss, true)
	p := f.player(t, 1, 5, f.field(0, 0), "")
	n := f.npc(t, "coyote", f.field(4, 0))
	f.combatTicks(1)
	if !n.HasGoal || n.Goal != p.Pos {
		t.Fatalf("NPC goal = %v (has %v), want the player's position %v", n.Goal, n.HasGoal, p.Pos)
	}
	f.combatTicks(3)
	if d := n.Pos.X - p.Pos.X; d > 1.5 {
		t.Errorf("NPC is %.2f m from the player, want within attack range", d)
	}
	if len(f.rec.hits(true)) == 0 {
		t.Error("NPC never attacked after walking to the player")
	}
}

// TestLeash checks an NPC whose fight takes it past Rules.LeashTiles from
// home drops its target and walks home.
func TestLeash(t *testing.T) {
	f := newFixture(t, alwaysMiss, true)
	leash := float32(f.d.Rules.LeashTiles)
	p := f.player(t, 1, 5, f.field(leash+3, 0), "")
	n := f.npc(t, "rabbit", f.field(leash+2, 0))
	n.Home = f.field(0, 0)
	f.attack(p, n)
	f.combatTicks(1)
	if _, ok := f.sys.NPCTargetOf(n.ID); ok {
		t.Error("NPC past its leash kept its target")
	}
	if !n.Returning || !n.HasGoal || n.Goal != n.Home {
		t.Errorf("NPC returning=%v goal=%v (has %v), want walking home to %v", n.Returning, n.Goal, n.HasGoal, n.Home)
	}
	if len(f.rec.hits(true)) != 0 {
		t.Error("leashed NPC attacked")
	}

	// It chases while inside the leash, then gives up past it.
	f = newFixture(t, alwaysMiss, true)
	n = f.npc(t, "coyote", f.field(0, 0))
	p = f.player(t, 1, 5, f.field(3, 0), "")
	f.combatTicks(1)
	if _, ok := f.sys.NPCTargetOf(n.ID); !ok {
		t.Fatal("aggressive NPC did not target the player")
	}
	for range 10 {
		p.Pos.X += 3 // the player runs away faster than the NPC follows
		f.combatTicks(1)
	}
	if _, ok := f.sys.NPCTargetOf(n.ID); ok {
		t.Error("NPC chased past its leash")
	}
	if !n.Returning {
		t.Error("NPC past its leash is not returning home")
	}
}

// TestNoAttackInSafeCircle checks that neither a player nor an NPC can start
// an attack while the player stands in the safe circle.
func TestNoAttackInSafeCircle(t *testing.T) {
	f := newFixture(t, alwaysMiss, false)
	s, r := f.zones.Spawn(), float32(f.zones.SafeRadius())
	in := world.Vec2{X: s.X + r - 0.2, Y: s.Y}
	out := world.Vec2{X: s.X + r + 0.3, Y: s.Y}
	if !f.zones.InSafeCircle(in) || f.zones.InSafeCircle(out) {
		t.Fatal("test positions are on the wrong sides of the safe circle")
	}
	p := f.player(t, 1, 5, in, "")
	n := f.npc(t, "rabbit", out)
	f.attack(p, n)
	f.combatTicks(3)
	if len(f.rec.events) != 0 {
		t.Fatalf("events in the safe circle: %+v", f.rec.events)
	}
	if _, ok := f.sys.TargetOf(p.ID); !ok {
		t.Fatal("target dropped; it should resume once the player leaves the circle")
	}
	p.Pos = f.field(0, 0)
	n.Pos = f.field(1, 0)
	f.combatTicks(1)
	if len(f.rec.hits(false)) != 1 {
		t.Errorf("player hits after leaving the circle = %d, want 1", len(f.rec.hits(false)))
	}
}

// TestNPCKilled checks an NPC at 0 HP is removed and a kill event carries its
// type, position and killer.
func TestNPCKilled(t *testing.T) {
	f := newFixture(t, alwaysMax, false)
	p := f.player(t, 1, 30, f.field(0, 0), "bike_lock")
	pos := f.field(1, 0)
	n := f.npc(t, "rabbit", pos)
	id, typ := n.ID, n.Type
	f.attack(p, n)
	f.combatTicks(1)
	if f.w.NPC(id) != nil {
		t.Fatal("dead NPC is still in the world")
	}
	want := combat.Kill{NPC: id, Type: typ, Pos: pos, Killer: p.ID}
	var kills []combat.Kill
	for _, e := range f.rec.events {
		if e.Kill != nil {
			kills = append(kills, *e.Kill)
		}
	}
	if len(kills) != 1 || !reflect.DeepEqual(kills[0], want) {
		t.Errorf("kills = %+v, want [%+v]", kills, want)
	}
	if _, ok := f.sys.TargetOf(p.ID); ok {
		t.Error("killer still targets the dead NPC")
	}
	if len(f.rec.hits(true)) != 0 {
		t.Error("dead NPC fought back")
	}
}

// TestPlayerDiesToNPC checks a player at 0 HP respawns at the spawn point
// with full HP and an unchanged inventory and gold.
func TestPlayerDiesToNPC(t *testing.T) {
	f := newFixture(t, alwaysMax, false)
	p := f.player(t, 1, 1, f.field(0, 0), "wooden_stick")
	p.Inv.Equipment.Body = f.item(t, "hoodie")
	if _, err := p.Inv.Add(f.d, f.item(t, "boba"), 3); err != nil {
		t.Fatal(err)
	}
	p.Inv.Gold = 123
	before := p.Inv.Clone()
	p.HP = 1
	pos := f.field(1, 0)
	p.Pos = pos
	n := f.npc(t, "coyote_alpha", f.field(2, 0))
	f.combatTicks(1)

	if p.Pos != f.zones.Spawn() {
		t.Errorf("player at %v, want spawn %v", p.Pos, f.zones.Spawn())
	}
	if p.HP != p.MaxHP(f.d.Rules) {
		t.Errorf("HP = %d, want full %d", p.HP, p.MaxHP(f.d.Rules))
	}
	if !reflect.DeepEqual(p.Inv, before) {
		t.Errorf("inventory changed:\n got %+v\nwant %+v", p.Inv, before)
	}
	if len(p.DrainEconomy()) != 0 {
		t.Error("a PvE death recorded economy changes")
	}
	var deaths []combat.Death
	for _, e := range f.rec.events {
		if e.Death != nil {
			deaths = append(deaths, *e.Death)
		}
	}
	want := combat.Death{Player: p.ID, Killer: n.ID, Pos: pos}
	if len(deaths) != 1 || deaths[0] != want {
		t.Errorf("deaths = %+v, want [%+v]", deaths, want)
	}
	if _, ok := f.sys.NPCTargetOf(n.ID); ok {
		t.Error("NPC still targets the respawned player")
	}
}

// TestShortFightEvents checks the full event stream of a short fight: a
// level 1 player with a wooden stick kills a rabbit, which fights back.
func TestShortFightEvents(t *testing.T) {
	f := newFixture(t, alwaysMax, false)
	p := f.player(t, 1, 1, f.field(0, 0), "wooden_stick")
	pos := f.field(1, 1)
	n := f.npc(t, "rabbit", pos)
	pid, nid := p.ID, n.ID
	f.attack(p, n)
	f.combatTicks(12)

	ph := func(tick uint64, died bool) event {
		return event{WorldTick: tick * combatEvery, Hit: &combat.Hit{Player: pid, NPC: nid, Damage: 1, Died: died}}
	}
	nh := func(tick uint64) event {
		return event{WorldTick: tick * combatEvery, Hit: &combat.Hit{Player: pid, NPC: nid, ByNPC: true, Damage: 1}}
	}
	want := []event{
		ph(1, false), nh(1),
		ph(5, false), nh(5),
		ph(9, true),
		{WorldTick: 9 * combatEvery, Kill: &combat.Kill{NPC: nid, Type: n.Type, Pos: pos, Killer: pid}},
	}
	if !reflect.DeepEqual(f.rec.events, want) {
		t.Errorf("events:")
		for _, e := range f.rec.events {
			t.Logf("  got  %d %+v %+v %+v", e.WorldTick, e.Hit, e.Kill, e.Death)
		}
		for _, e := range want {
			t.Logf("  want %d %+v %+v %+v", e.WorldTick, e.Hit, e.Kill, e.Death)
		}
	}
	if p.XP != 3*f.d.Rules.XPPerDamage {
		t.Errorf("XP = %d, want %d", p.XP, 3*f.d.Rules.XPPerDamage)
	}
	if p.HP != p.MaxHP(f.d.Rules)-2 {
		t.Errorf("HP = %d, want %d", p.HP, p.MaxHP(f.d.Rules)-2)
	}
}

// TestLeftPlayerForgotten checks a player who leaves loses their target, so
// a newcomer with the same id doesn't inherit it.
func TestLeftPlayerForgotten(t *testing.T) {
	f := newFixture(t, alwaysMiss, false)
	p := f.player(t, 1, 5, f.field(0, 0), "")
	n := f.npc(t, "rabbit", f.field(1, 0))
	f.attack(p, n)
	f.combatTicks(1)
	f.w.RemovePlayer(p.ID)
	f.combatTicks(1)
	f.player(t, 1, 5, f.field(0, 0), "")
	if _, ok := f.sys.TargetOf(1); ok {
		t.Error("rejoined player inherited the old target")
	}
	if _, ok := f.sys.NPCTargetOf(n.ID); ok {
		t.Error("NPC still targets a player who left")
	}
}

// BenchmarkTick measures one combat tick with 200 players each fighting an
// NPC.
func BenchmarkTick(b *testing.B) {
	f := newFixture(b, rand.New(rand.NewPCG(1, 2)), false)
	for i := range 200 {
		x := float32(i%20) * 5
		y := float32(i/20) * 5
		p := f.player(b, world.PlayerID(i+1), 10, f.field(x, y), "wooden_stick")
		n := f.npc(b, "coyote_alpha", f.field(x+1, y))
		n.HP = 1 << 30
		p.HP = 1 << 30
		f.attack(p, n)
	}
	f.w.Tick()
	b.ResetTimer()
	for range b.N {
		f.sys.Tick(f.w, 600*time.Millisecond)
	}
}
