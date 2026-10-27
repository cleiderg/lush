package session_test

import (
	"slices"
	"testing"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/pvp"
	"github.com/cleiderg/lush/server/internal/world"
)

// rabbit is a passive NPC type from the default NPC table: it fights back
// only when hit.
const rabbit world.NPCTypeID = 1

// AC3 (#241): AttackTarget naming an NPC id goes to the PvE combat system,
// and one naming a player still goes to PvP. A player has one target, so
// each clears the other system's target.
func TestAttackTargetNPCReachesPvECombat(t *testing.T) {
	r := newPvPRig(t, pvpRand{f: 0.999999})
	ca := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	a, _, _, _, _ := joinFully(t, r.rig, ca)
	cb := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	b, _, _, _, _ := joinFully(t, r.rig, cb)
	n := addNPC(t, r.w, rabbit, world.Vec2{X: 40, Y: 40})

	// An NPC id with no NPC in the world is ignored.
	ca.send(attackTarget(uint16(n.ID) + 1))
	r.settle(ca)
	if v, ok := r.pve.TargetOf(a); ok {
		t.Fatalf("after AttackTarget of a missing NPC, player %d targets NPC %d, want none", a, v)
	}

	ca.send(attackTarget(uint16(n.ID)))
	r.settle(ca)
	if v, ok := r.pve.TargetOf(a); !ok || v != n.ID {
		t.Fatalf("PvE TargetOf(%d) = %d, %v; want %d, true", a, v, ok, n.ID)
	}
	if v, ok := r.combat.TargetOf(a); ok {
		t.Fatalf("PvP TargetOf(%d) = %d after targeting an NPC, want none", a, v)
	}

	ca.send(attackTarget(uint16(b)))
	r.settle(ca)
	if v, ok := r.combat.TargetOf(a); !ok || v != b {
		t.Fatalf("PvP TargetOf(%d) = %d, %v; want %d, true", a, v, ok, b)
	}
	if v, ok := r.pve.TargetOf(a); ok {
		t.Fatalf("PvE TargetOf(%d) = %d after targeting a player, want none", a, v)
	}

	ca.send(attackTarget(uint16(n.ID)))
	r.settle(ca)
	if v, ok := r.pve.TargetOf(a); !ok || v != n.ID {
		t.Fatalf("PvE TargetOf(%d) = %d, %v; want %d, true", a, v, ok, n.ID)
	}
	if v, ok := r.combat.TargetOf(a); ok {
		t.Fatalf("PvP TargetOf(%d) = %d after targeting an NPC again, want none", a, v)
	}
}

// AC4 (#241): on a combat tick, PvE hits (player on NPC and NPC on player)
// arrive in the same CombatHit as that tick's PvP hits, at every client that
// sees the attacker or the target, and not at a client that sees neither.
func TestPvEHitsShareCombatHitWithPvP(t *testing.T) {
	r := newPvPRig(t, pvpRand{f: 0.999999}) // every swing misses: nobody dies
	v := uint16(protocol.ProtocolVersionCurrent)
	ca, cb, cc, cn, cf := r.dial(hello(v, "")), r.dial(hello(v, "")), r.dial(hello(v, "")), r.dial(hello(v, "")), r.dial(hello(v, ""))
	a, _, _, _, _ := joinFully(t, r.rig, ca)
	b, _, _, _, _ := joinFully(t, r.rig, cb)
	c, _, _, _, _ := joinFully(t, r.rig, cc)
	joinFully(t, r.rig, cn)
	far, _, _, _, _ := joinFully(t, r.rig, cf)
	r.w.Player(far).Pos = world.Vec2{X: 500, Y: 500} // out of everyone's view
	r.w.Player(far).HasDesired = false
	n := addNPC(t, r.w, rabbit, r.w.Player(c).Pos)

	ca.send(attackTarget(uint16(b)))
	cc.send(attackTarget(uint16(n.ID)))
	r.settle(ca)
	r.settle(cc)
	npcID := world.PlayerID(n.ID)
	want := []pvp.Hit{{Attacker: a, Target: b}, {Attacker: c, Target: npcID}, {Attacker: npcID, Target: c}}
	for _, cl := range []*client{ca, cb, cc, cn} {
		// The NPC fights back on the tick it is first hit, so within two
		// combat ticks one CombatHit carries all three swings.
		var got []pvp.Hit
		for i := 0; i < 3; i++ {
			_, hits := combatHit(r.waitFor(cl, protocol.MessageCombatHit))
			if got = hits; containsAll(hits, want) {
				break
			}
		}
		if !containsAll(got, want) {
			t.Fatalf("CombatHit hits = %+v, want all of %+v", got, want)
		}
	}
	end := uint32(r.w.TickCount()) + 36
	for {
		typ, tbl := r.next(cf)
		if typ == protocol.MessageCombatHit {
			t.Fatal("player out of view got a CombatHit")
		}
		if typ != protocol.MessageSnapshot {
			continue
		}
		var s protocol.Snapshot
		if s.Init(tbl.Bytes, tbl.Pos); s.Tick() >= end {
			break
		}
	}
}

func containsAll(hits, want []pvp.Hit) bool {
	for _, w := range want {
		if !slices.Contains(hits, w) {
			return false
		}
	}
	return true
}
