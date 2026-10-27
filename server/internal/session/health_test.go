package session_test

import (
	"testing"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/world"
)

// stepHealth ticks the world once and returns that tick's Snapshot and every
// EntityHealth read before it, as one map per message from entity id to ratio.
func (r *rig) stepHealth(c *client) (protocol.Snapshot, []map[uint16]uint8) {
	r.t.Helper()
	r.w.Tick()
	want := uint32(r.w.TickCount())
	var msgs []map[uint16]uint8
	for i := 0; i < 10000; i++ {
		typ, tbl := r.readNoTick(c)
		switch typ {
		case protocol.MessageEntityHealth:
			var e protocol.EntityHealth
			e.Init(tbl.Bytes, tbl.Pos)
			m := map[uint16]uint8{}
			var h protocol.HealthRatio
			for j := 0; j < e.EntriesLength(); j++ {
				e.Entries(&h, j)
				if _, dup := m[h.Id()]; dup {
					r.t.Fatalf("entity %d listed twice in one EntityHealth", h.Id())
				}
				m[h.Id()] = h.Ratio()
			}
			msgs = append(msgs, m)
		case protocol.MessageSnapshot:
			var s protocol.Snapshot
			s.Init(tbl.Bytes, tbl.Pos)
			if s.Tick() == want {
				return s, msgs
			}
		}
	}
	r.t.Fatalf("never saw the snapshot of tick %d", want)
	return protocol.Snapshot{}, nil
}

// healthRig is a client with another player and an NPC in view, both at
// full health, after the tick they entered view.
type healthRig struct {
	*rig
	c      *client
	self   *world.Player
	other  *world.Player
	npc    *world.NPC
	npcMax int
}

func newHealthRig(t *testing.T) healthRig {
	t.Helper()
	d := testData(t)
	r := newRig(t, 1024)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	self, _ := r.expectWelcome(c)

	other := &world.Player{ID: 3000, Pos: world.Vec2{X: 2}}
	other.HP = other.MaxHP(d.Rules)
	if err := r.w.AddPlayer(other); err != nil {
		t.Fatal(err)
	}
	typ := d.NPCs[0]
	n := addNPC(t, r.w, world.NPCTypeID(typ.ID), world.Vec2{Y: 3})
	n.HP = typ.HP

	for i := 0; ; i++ {
		s, hs := r.stepHealth(c)
		ids := snapshotIDs(s)
		if ids[uint16(other.ID)] && ids[uint16(n.ID)] {
			if len(hs) != 0 {
				t.Fatalf("EntityHealth %v for entities entering view at full health", hs)
			}
			break
		}
		if i == 100 {
			t.Fatal("others never in view")
		}
	}
	return healthRig{rig: r, c: c, self: r.w.Player(world.PlayerID(self)), other: other, npc: n, npcMax: typ.HP}
}

// one steps once and fails unless exactly one EntityHealth listing exactly
// want arrived.
func (h healthRig) one(want map[uint16]uint8, what string) {
	h.t.Helper()
	_, hs := h.stepHealth(h.c)
	if len(hs) != 1 {
		h.t.Fatalf("%s: got %d EntityHealth %v, want one", what, len(hs), hs)
	}
	if len(hs[0]) != len(want) {
		h.t.Fatalf("%s: EntityHealth lists %v, want %v", what, hs[0], want)
	}
	for id, r := range want {
		if got, ok := hs[0][id]; !ok || got != r {
			h.t.Fatalf("%s: EntityHealth lists %v, want %v", what, hs[0], want)
		}
	}
}

// quiet steps a few ticks and fails if any EntityHealth arrives.
func (h healthRig) quiet(what string) {
	h.t.Helper()
	for i := 0; i < 5; i++ {
		if _, hs := h.stepHealth(h.c); len(hs) != 0 {
			h.t.Fatalf("%s: EntityHealth %v on a tick where no health changed", what, hs)
		}
	}
}

// ratio is hp / max scaled to 0..255, as HealthRatio defines it.
func ratio(hp, max int) uint8 { return uint8(hp * 255 / max) }

// AC1: when a visible player's or NPC's HP changes, the client gets one
// EntityHealth that tick listing it with hp / max_hp scaled to 0..255.
func TestEntityHealthOnChange(t *testing.T) {
	h := newHealthRig(t)
	d := testData(t)
	otherMax := h.other.MaxHP(d.Rules)
	selfMax := h.self.MaxHP(d.Rules)
	oid, nid, sid := uint16(h.other.ID), uint16(h.npc.ID), uint16(h.self.ID)

	h.other.HP = otherMax / 2
	h.one(map[uint16]uint8{oid: ratio(otherMax/2, otherMax)}, "player hit")

	h.npc.HP = 1
	h.one(map[uint16]uint8{nid: ratio(1, h.npcMax)}, "NPC hit")

	h.other.HP, h.npc.HP = 1, 0
	h.one(map[uint16]uint8{oid: ratio(1, otherMax), nid: 0}, "both hit")

	h.self.HP = selfMax - 1
	h.one(map[uint16]uint8{sid: ratio(selfMax-1, selfMax)}, "own player hit")

	h.other.HP, h.self.HP = otherMax, selfMax
	h.one(map[uint16]uint8{oid: 255, sid: 255}, "healed to full")

	// Exact scaling: half health is 127, full is 255, dead is 0.
	if ratio(5, 10) != 127 || ratio(10, 10) != 255 || ratio(0, 10) != 0 {
		t.Fatal("ratio helper is off")
	}
}

// AC2: entities whose health did not change are not listed, and no
// EntityHealth is sent on a tick where nothing changed.
func TestEntityHealthOnlyChanges(t *testing.T) {
	h := newHealthRig(t)
	h.quiet("after entering view")

	h.npc.HP--
	h.one(map[uint16]uint8{uint16(h.npc.ID): ratio(h.npc.HP, h.npcMax)}, "NPC hit")
	h.quiet("after NPC hit")

	h.other.HP-- // only the player is listed; the NPC did not change again
	h.one(map[uint16]uint8{uint16(h.other.ID): ratio(h.other.HP, h.other.MaxHP(testData(t).Rules))}, "player hit")
	h.quiet("after player hit")
}

// AC3: an entity entering view below full health is listed on the tick it
// enters, and again after it leaves view and comes back.
func TestEntityHealthOnEnterView(t *testing.T) {
	h := newHealthRig(t)
	d := testData(t)
	typ := d.NPCs[0]
	far := addNPC(t, h.w, world.NPCTypeID(typ.ID), world.Vec2{X: 500})
	far.HP = typ.HP / 2
	if far.HP == 0 {
		far.HP = 1
	}
	fid := uint16(far.ID)
	want := map[uint16]uint8{fid: ratio(far.HP, typ.HP)}
	h.quiet("hurt NPC out of view")

	enter := func(what string) {
		t.Helper()
		far.Pos = world.Vec2{X: -3}
		s, hs := h.stepHealth(h.c)
		if !snapshotIDs(s)[fid] {
			t.Fatalf("%s: NPC not in the snapshot", what)
		}
		if len(hs) != 1 || len(hs[0]) != 1 || hs[0][fid] != want[fid] {
			t.Fatalf("%s: got EntityHealth %v, want one listing %v", what, hs, want)
		}
		h.quiet("after " + what)
	}
	enter("NPC entered view")

	far.Pos = world.Vec2{X: 500}
	if s, hs := h.stepHealth(h.c); snapshotIDs(s)[fid] || len(hs) != 0 {
		t.Fatalf("NPC left view: still in snapshot or EntityHealth %v", hs)
	}
	enter("NPC came back")

	// The same holds for a player.
	h.other.HP = 1
	h.other.Pos = world.Vec2{X: 500}
	if s, hs := h.stepHealth(h.c); snapshotIDs(s)[uint16(h.other.ID)] || len(hs) != 0 {
		t.Fatalf("player left view: still in snapshot or EntityHealth %v", hs)
	}
	h.other.Pos = world.Vec2{X: 2}
	h.one(map[uint16]uint8{uint16(h.other.ID): ratio(1, h.other.MaxHP(d.Rules))}, "hurt player came back")
}
