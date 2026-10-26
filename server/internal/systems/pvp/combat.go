package pvp

import (
	"errors"
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// AttackCommand asks for Player to attack the player Victim. It replaces
// Player's previous PvP target.
type AttackCommand struct {
	Player world.PlayerID
	Victim world.PlayerID
}

func (c AttackCommand) Target() world.PlayerID { return c.Player }

// Hit is one PvP swing: Damage is 0 on a miss, and Died is set when it took
// the target to 0 HP.
type Hit struct {
	Attacker world.PlayerID
	Target   world.PlayerID
	Damage   int
	Died     bool
}

// HitSink receives every PvP swing on the tick that made it. It is called on
// the tick goroutine and must not block.
type HitSink interface {
	PvPHit(h Hit)
}

// Drop is what a player who died in the PvP zone left on the ground: the
// dropped stacks, reserved for Owner (the killer), at Pos (where they died).
type Drop struct {
	Owner  world.PlayerID
	Pos    world.Vec2
	Stacks []world.Stack
}

// DropSink receives the stacks a PvP death drops, on the tick of the death,
// for the ground-item spawner. It is called on the tick goroutine and must
// not block.
type DropSink interface {
	PvPDrop(d Drop)
}

// Zones is the map the combat system needs: which area a position is in, and
// where dead players respawn. *areas.Lookup implements it.
type Zones interface {
	AreaLookup
	Spawn() world.Vec2
}

// Combat is the PvP combat system (docs/specs/gameplay.md, Combat; Death in
// the PvP zone). Register it with RegisterEvery(s, 12) so it ticks on the
// combat tick. On each Tick, every player with a PvP target attacks it when
// CanAttack allows, the target is within Rules.AttackRangeTiles, and the
// attacker's weapon speed (counted in this system's Ticks) has passed since
// their last attack. A player who reaches 0 HP drops all but their kept
// items (ApplyDeath) and respawns at the spawn point with full HP.
//
// Targets and attack timing live here, not in world.Player. A target that
// has died or left the world is dropped; one that is out of range or refused
// by CanAttack is kept, so the attack resumes once it is allowed again.
type Combat struct {
	data     *gamedata.Data
	zones    Zones
	tileSize float64
	clock    world.Clock
	rand     formula.Rand
	hits     HitSink
	drops    DropSink

	ticks   uint64                            // combat ticks run so far
	targets map[world.PlayerID]world.PlayerID // attacker → victim
	next    map[world.PlayerID]uint64         // first combat tick the player may attack again
}

// NewCombat creates the PvP combat system. tileSize is the length of a tile
// in metres (the collision grid's cell size); rand drives hit and damage
// rolls; hits and drops receive each swing and each death's dropped stacks.
func NewCombat(d *gamedata.Data, z Zones, tileSize float64, clock world.Clock, r formula.Rand, hits HitSink, drops DropSink) (*Combat, error) {
	if d == nil || z == nil || clock == nil || r == nil || hits == nil || drops == nil {
		return nil, errors.New("pvp combat: nil dependency")
	}
	if tileSize <= 0 || math.IsNaN(tileSize) || math.IsInf(tileSize, 0) {
		return nil, errors.New("pvp combat: tile size must be positive")
	}
	return &Combat{
		data:     d,
		zones:    z,
		tileSize: tileSize,
		clock:    clock,
		rand:     r,
		hits:     hits,
		drops:    drops,
		targets:  make(map[world.PlayerID]world.PlayerID),
		next:     make(map[world.PlayerID]uint64),
	}, nil
}

func (s *Combat) Name() string { return "pvp_combat" }

// Handle sets the attacker's PvP target from an AttackCommand. Attacking
// oneself is ignored. Whether the attack is allowed is checked on each Tick.
func (s *Combat) Handle(cmd world.Command, p *world.Player) {
	c, ok := cmd.(AttackCommand)
	if !ok || c.Victim == p.ID {
		return
	}
	s.targets[p.ID] = c.Victim
}

// ClearTarget forgets player id's PvP target, so they make no further PvP
// attacks until they get a new one. The session calls it when the player
// targets an NPC or stops attacking. Tick-goroutine only.
func (s *Combat) ClearTarget(id world.PlayerID) { delete(s.targets, id) }

// TargetOf returns player id's PvP target, if they have one.
func (s *Combat) TargetOf(id world.PlayerID) (world.PlayerID, bool) {
	t, ok := s.targets[id]
	return t, ok
}

// Tick runs one combat tick: every attacker, in world order, swings at their
// target if the swing is due and allowed.
func (s *Combat) Tick(w *world.World, _ time.Duration) {
	s.ticks++
	for id := range s.next {
		if w.Player(id) == nil {
			delete(s.next, id)
		}
	}
	for id := range s.targets { // an attacker who left must not resume on rejoin (ids are reused)
		if w.Player(id) == nil {
			delete(s.targets, id)
		}
	}
	if len(s.targets) == 0 {
		return
	}
	now := s.clock.Now()
	for _, a := range w.Players() {
		vid, ok := s.targets[a.ID]
		if !ok {
			continue
		}
		v := w.Player(vid)
		if v == nil || v.HP <= 0 || a.HP <= 0 {
			delete(s.targets, a.ID)
			continue
		}
		if s.ticks < s.next[a.ID] || !s.inRange(a.Pos, v.Pos) {
			continue
		}
		r := s.data.Rules
		if CanAttack(s.data, s.zones, Fighter{a.CombatLevel(r), a.Pos}, Fighter{v.CombatLevel(r), v.Pos}) != Allowed {
			continue
		}
		if a.AttackDelay > 0 {
			a.AttackDelay--
			continue
		}
		s.attack(w, a, v, now)
	}
}

// attack makes one swing from a at v and applies its outcome.
func (s *Combat) attack(w *world.World, a, v *world.Player, now time.Time) {
	r := s.data.Rules
	att, speed := a.Inv.Equipment.Bonuses(s.data)
	def, _ := v.Inv.Equipment.Bonuses(s.data)
	s.next[a.ID] = s.ticks + uint64(max(speed, 1))

	dmg := formula.RollDamage(s.rand, formula.Attack{
		AttackerLevel: a.CombatLevel(r),
		Accuracy:      att.Accuracy,
		Strength:      att.Strength,
		DefenderLevel: v.CombatLevel(r),
		Defence:       def.Defence,
	})
	dmg = min(dmg, v.HP) // a hit never takes more than the HP left
	v.HP -= dmg
	a.AddXP(r, dmg*r.XPPerDamage)
	a.MarkCombat(now)
	v.MarkCombat(now)

	died := v.HP <= 0
	s.hits.PvPHit(Hit{Attacker: a.ID, Target: v.ID, Damage: dmg, Died: died})
	if died {
		s.die(w, v, a.ID)
	}
}

// die applies a PvP death to v, killed by killer: the dropped stacks go to
// the drop sink and the changes to v's economy outbox, then v respawns at
// the spawn point with full HP and nobody targets them any more.
//
// If v's inventory is locked (a trade is being saved), ApplyDeath refuses
// and v keeps everything: nothing may move while the trade's result is
// pending, or items could be duplicated or lost.
func (s *Combat) die(w *world.World, v *world.Player, killer world.PlayerID) {
	pos := v.Pos
	dropped, changes, err := ApplyDeath(s.data, &v.Inv)
	if err == nil {
		if len(changes) > 0 {
			v.RecordEconomy(ReasonPvPDeath, changes)
		}
		if len(dropped) > 0 {
			s.drops.PvPDrop(Drop{Owner: killer, Pos: pos, Stacks: dropped})
		}
	}

	spawn := s.zones.Spawn()
	v.Pos = spawn
	v.Desired = spawn
	v.HasDesired = false
	v.Anim = world.AnimIdle
	v.HP = v.MaxHP(s.data.Rules)

	delete(s.targets, v.ID)
	for id, t := range s.targets {
		if t == v.ID {
			delete(s.targets, id)
		}
	}
}

// inRange reports whether b's tile is within Rules.AttackRangeTiles of a's,
// counting diagonal steps as one when Rules.AttackRangeDiagonal is set.
func (s *Combat) inRange(a, b world.Vec2) bool {
	dx := abs(s.tile(a.X) - s.tile(b.X))
	dy := abs(s.tile(a.Y) - s.tile(b.Y))
	reach := s.data.Rules.AttackRangeTiles
	if s.data.Rules.AttackRangeDiagonal {
		return max(dx, dy) <= reach
	}
	return dx+dy <= reach
}

func (s *Combat) tile(v float32) int {
	return int(math.Floor(float64(v) / s.tileSize))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
