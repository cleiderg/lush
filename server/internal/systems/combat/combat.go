// Package combat is the PvE combat system (docs/specs/gameplay.md, Combat;
// Death outside the PvP zone): players attack NPCs, NPCs fight back or pick
// fights when aggressive, and deaths are applied on the combat tick.
//
// It does no I/O. Every hit and death goes to an EventSink, which the server
// wires to the network and the drop spawner.
package combat

import (
	"errors"
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// AttackCommand asks for Player to attack the NPC NPC. It replaces Player's
// previous NPC target.
type AttackCommand struct {
	Player world.PlayerID
	NPC    world.NPCID
}

func (c AttackCommand) Target() world.PlayerID { return c.Player }

// Hit is one PvE swing between a player and an NPC. ByNPC says which side
// swung: false means Player hit NPC, true means NPC hit Player. Damage is 0
// on a miss, and Died is set when the swing took its target to 0 HP.
type Hit struct {
	Player world.PlayerID
	NPC    world.NPCID
	ByNPC  bool
	Damage int
	Died   bool
}

// Kill is an NPC killed by a player: its type and where it died (where its
// drops go), and the player who landed the killing hit.
type Kill struct {
	NPC    world.NPCID
	Type   world.NPCTypeID
	Pos    world.Vec2
	Killer world.PlayerID
}

// Death is a player killed by an NPC. Pos is where they died; they have
// already respawned when the event is emitted.
type Death struct {
	Player world.PlayerID
	Killer world.NPCID
	Pos    world.Vec2
}

// EventSink receives every PvE hit and death on the tick that made it. It
// is called on the tick goroutine and must not block.
type EventSink interface {
	Hit(h Hit)
	NPCKilled(k Kill)
	PlayerDied(d Death)
}

// Zones is the map the combat system needs: the safe circle, where no attack
// can start, and the spawn point where dead players respawn. *areas.Lookup
// implements it.
type Zones interface {
	InSafeCircle(p world.Vec2) bool
	Spawn() world.Vec2
}

// System is the PvE combat system. Register it with RegisterEvery(s, 12) so
// it ticks on the combat tick, after the NPC movement system. On each Tick:
//
//  1. Aggressive NPCs without a target pick the nearest living player within
//     Rules.AggressionRangeTiles who is outside the safe circle.
//  2. Every player with an NPC target swings at it when it is within
//     Rules.AttackRangeTiles and their weapon's attack speed (counted in
//     combat ticks) has passed. A player in the safe circle can't attack.
//     An NPC that is hit fights back if it has no target.
//  3. Every NPC with a target gives up and walks home once past
//     Rules.LeashTiles from home (or when its target is in the safe circle),
//     swings when in range and due, and otherwise walks toward its target
//     by setting its Goal for the NPC system's goal step.
//
// An NPC at 0 HP is removed from the world; a player at 0 HP respawns at the
// spawn point with full HP and keeps everything.
type System struct {
	data     *gamedata.Data
	zones    Zones
	tileSize float64
	leash    float64 // metres
	clock    world.Clock
	rand     formula.Rand
	events   EventSink

	ticks      uint64                         // combat ticks run so far
	targets    map[world.PlayerID]world.NPCID // player → NPC they attack
	npcTargets map[world.NPCID]world.PlayerID // NPC → player it attacks
	next       map[world.PlayerID]uint64      // first combat tick the player may attack again
	npcNext    map[world.NPCID]uint64         // first combat tick the NPC may attack again
}

// New creates the PvE combat system. tileSize is the length of a tile in
// metres (the collision grid's cell size); rand drives hit and damage rolls;
// events receives every hit and death.
func New(d *gamedata.Data, z Zones, tileSize float64, clock world.Clock, r formula.Rand, events EventSink) (*System, error) {
	if d == nil || z == nil || clock == nil || r == nil || events == nil {
		return nil, errors.New("pve combat: nil dependency")
	}
	if !(tileSize > 0) || math.IsInf(tileSize, 0) {
		return nil, errors.New("pve combat: tile size must be positive and finite")
	}
	return &System{
		data:       d,
		zones:      z,
		tileSize:   tileSize,
		leash:      float64(d.Rules.LeashTiles) * tileSize,
		clock:      clock,
		rand:       r,
		events:     events,
		targets:    make(map[world.PlayerID]world.NPCID),
		npcTargets: make(map[world.NPCID]world.PlayerID),
		next:       make(map[world.PlayerID]uint64),
		npcNext:    make(map[world.NPCID]uint64),
	}, nil
}

func (s *System) Name() string { return "pve_combat" }

// Handle sets the player's NPC target from an AttackCommand. Whether the
// attack is allowed is checked on each Tick.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	c, ok := cmd.(AttackCommand)
	if !ok {
		return
	}
	s.targets[p.ID] = c.NPC
}

// ClearTarget forgets player id's NPC target. The session calls it when the
// player targets someone else or stops attacking. Tick-goroutine only.
func (s *System) ClearTarget(id world.PlayerID) { delete(s.targets, id) }

// TargetOf returns player id's NPC target, if they have one.
func (s *System) TargetOf(id world.PlayerID) (world.NPCID, bool) {
	t, ok := s.targets[id]
	return t, ok
}

// NPCTargetOf returns the player NPC id is fighting, if any.
func (s *System) NPCTargetOf(id world.NPCID) (world.PlayerID, bool) {
	t, ok := s.npcTargets[id]
	return t, ok
}

// Tick runs one combat tick.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.ticks++
	s.forgetGone(w)
	now := s.clock.Now()
	s.aggress(w)
	for _, p := range w.Players() {
		s.playerTurn(w, p, now)
	}
	for _, n := range w.NPCs() {
		s.npcTurn(w, n, now)
	}
}

// forgetGone drops state for players and NPCs that left the world (ids are
// reused, so a newcomer must not inherit it).
func (s *System) forgetGone(w *world.World) {
	for id := range s.targets {
		if w.Player(id) == nil {
			delete(s.targets, id)
		}
	}
	for id := range s.next {
		if w.Player(id) == nil {
			delete(s.next, id)
		}
	}
	for id := range s.npcTargets {
		if w.NPC(id) == nil {
			delete(s.npcTargets, id)
		}
	}
	for id := range s.npcNext {
		if w.NPC(id) == nil {
			delete(s.npcNext, id)
		}
	}
}

// aggress gives every idle aggressive NPC the nearest player in range.
func (s *System) aggress(w *world.World) {
	reach := s.data.Rules.AggressionRangeTiles
	for _, n := range w.NPCs() {
		if _, busy := s.npcTargets[n.ID]; busy || n.Returning || n.HP <= 0 {
			continue
		}
		t, ok := s.data.NPC(gamedata.NPCTypeID(n.Type))
		if !ok || t.Behaviour != gamedata.Aggressive {
			continue
		}
		best, bestD := world.PlayerID(0), math.MaxInt
		found := false
		for _, p := range w.Players() {
			if p.HP <= 0 || s.zones.InSafeCircle(p.Pos) {
				continue
			}
			if d := s.tiles(n.Pos, p.Pos); d <= reach && d < bestD {
				best, bestD, found = p.ID, d, true
			}
		}
		if found {
			s.npcTargets[n.ID] = best
		}
	}
}

// playerTurn makes p swing at their NPC target if allowed and due.
func (s *System) playerTurn(w *world.World, p *world.Player, now time.Time) {
	nid, ok := s.targets[p.ID]
	if !ok {
		return
	}
	n := w.NPC(nid)
	if n == nil || n.HP <= 0 || p.HP <= 0 {
		delete(s.targets, p.ID)
		return
	}
	t, ok := s.data.NPC(gamedata.NPCTypeID(n.Type))
	if !ok {
		delete(s.targets, p.ID)
		return
	}
	if s.ticks < s.next[p.ID] || !s.inRange(p.Pos, n.Pos) || s.zones.InSafeCircle(p.Pos) || s.zones.InSafeCircle(n.Pos) {
		return
	}
	if p.AttackDelay > 0 {
		p.AttackDelay--
		return
	}

	r := s.data.Rules
	att, speed := p.Inv.Equipment.Bonuses(s.data)
	s.next[p.ID] = s.ticks + uint64(max(speed, 1))
	dmg := formula.RollDamage(s.rand, formula.Attack{
		AttackerLevel: p.CombatLevel(r),
		Accuracy:      att.Accuracy,
		Strength:      att.Strength,
		DefenderLevel: t.Level,
		Defence:       t.Defence,
	})
	dmg = min(dmg, n.HP) // a hit never takes more than the HP left
	n.HP -= dmg
	p.AddXP(r, dmg*r.XPPerDamage)
	p.MarkCombat(now)
	n.MarkCombat(now)

	died := n.HP <= 0
	s.events.Hit(Hit{Player: p.ID, NPC: n.ID, Damage: dmg, Died: died})
	if died {
		s.killNPC(w, n, p.ID)
		return
	}
	if _, busy := s.npcTargets[n.ID]; !busy && !n.Returning {
		s.npcTargets[n.ID] = p.ID // passive or not, an NPC fights back
	}
}

// npcTurn leashes, moves or swings an NPC that has a target.
func (s *System) npcTurn(w *world.World, n *world.NPC, now time.Time) {
	pid, ok := s.npcTargets[n.ID]
	if !ok {
		return
	}
	p := w.Player(pid)
	t, known := s.data.NPC(gamedata.NPCTypeID(n.Type))
	if p == nil || p.HP <= 0 || n.HP <= 0 || !known {
		s.giveUp(n, false)
		return
	}
	if n.Returning || dist(n.Pos, n.Home) > s.leash {
		s.giveUp(n, true)
		return
	}
	if s.zones.InSafeCircle(p.Pos) {
		s.giveUp(n, false)
		n.Goal, n.HasGoal = n.Home, true
		return
	}
	if !s.inRange(n.Pos, p.Pos) {
		n.Goal, n.HasGoal = p.Pos, true // the NPC system walks it there
		return
	}
	n.HasGoal = false
	if s.ticks < s.npcNext[n.ID] {
		return
	}

	r := s.data.Rules
	def, _ := p.Inv.Equipment.Bonuses(s.data)
	s.npcNext[n.ID] = s.ticks + uint64(max(t.AttackSpeed, 1))
	dmg := formula.RollDamage(s.rand, formula.Attack{
		AttackerLevel: t.Level,
		Accuracy:      t.Accuracy,
		Strength:      t.Strength,
		DefenderLevel: p.CombatLevel(r),
		Defence:       def.Defence,
	})
	dmg = min(dmg, p.HP)
	p.HP -= dmg
	n.MarkCombat(now)
	p.MarkCombat(now)

	died := p.HP <= 0
	s.events.Hit(Hit{Player: p.ID, NPC: n.ID, ByNPC: true, Damage: dmg, Died: died})
	if died {
		s.killPlayer(p, n.ID)
	}
}

// giveUp drops n's target. With leashed set, n walks home as the NPC
// system's leash does.
func (s *System) giveUp(n *world.NPC, leashed bool) {
	delete(s.npcTargets, n.ID)
	if leashed {
		n.Goal, n.HasGoal, n.Returning = n.Home, true, true
	} else {
		n.HasGoal = false
	}
}

// killNPC removes n, killed by killer, and emits the kill.
func (s *System) killNPC(w *world.World, n *world.NPC, killer world.PlayerID) {
	k := Kill{NPC: n.ID, Type: n.Type, Pos: n.Pos, Killer: killer}
	w.RemoveNPC(n.ID)
	delete(s.npcTargets, n.ID)
	delete(s.npcNext, n.ID)
	for pid, t := range s.targets {
		if t == n.ID {
			delete(s.targets, pid)
		}
	}
	s.events.NPCKilled(k)
}

// killPlayer respawns p, killed by killer, at the spawn point with full HP.
// A PvE death costs nothing: inventory, equipment and gold are untouched.
func (s *System) killPlayer(p *world.Player, killer world.NPCID) {
	d := Death{Player: p.ID, Killer: killer, Pos: p.Pos}
	spawn := s.zones.Spawn()
	p.Pos = spawn
	p.Desired = spawn
	p.HasDesired = false
	p.Anim = world.AnimIdle
	p.HP = p.MaxHP(s.data.Rules)

	delete(s.targets, p.ID)
	for nid, t := range s.npcTargets {
		if t == p.ID {
			delete(s.npcTargets, nid)
		}
	}
	s.events.PlayerDied(d)
}

// inRange reports whether b's tile is within Rules.AttackRangeTiles of a's,
// counting diagonal steps as one when Rules.AttackRangeDiagonal is set.
func (s *System) inRange(a, b world.Vec2) bool {
	reach := s.data.Rules.AttackRangeTiles
	if s.data.Rules.AttackRangeDiagonal {
		return s.tiles(a, b) <= reach
	}
	dx, dy := s.tileDelta(a, b)
	return dx+dy <= reach
}

// tiles is the distance between a's and b's tiles counting a diagonal step
// as one (Chebyshev distance).
func (s *System) tiles(a, b world.Vec2) int {
	dx, dy := s.tileDelta(a, b)
	return max(dx, dy)
}

func (s *System) tileDelta(a, b world.Vec2) (dx, dy int) {
	return abs(s.tile(a.X) - s.tile(b.X)), abs(s.tile(a.Y) - s.tile(b.Y))
}

func (s *System) tile(v float32) int {
	return int(math.Floor(float64(v) / s.tileSize))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func dist(a, b world.Vec2) float64 {
	return math.Hypot(float64(a.X)-float64(b.X), float64(a.Y)-float64(b.Y))
}
