package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/interest"
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/transport"
	"github.com/cleiderg/lush/server/internal/world"
)

// readNoTick reads one envelope without ticking the world.
func (r *rig) readNoTick(c *client) (protocol.Message, flatbuffers.Table) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, d, err := c.c.Read(ctx)
	if err != nil {
		r.t.Fatalf("read: %v", err)
	}
	env := protocol.GetRootAsEnvelope(d, 0)
	var tbl flatbuffers.Table
	env.Msg(&tbl)
	return env.MsgType(), tbl
}

// step ticks the world once and returns that tick's Snapshot and every
// EntityInfo read before it. After one step has caught up with the client's
// frames, that is exactly the EntityInfo sent on the tick.
func (r *rig) step(c *client) (protocol.Snapshot, []protocol.EntityInfo) {
	r.t.Helper()
	r.w.Tick()
	want := uint32(r.w.TickCount())
	var infos []protocol.EntityInfo
	for i := 0; i < 10000; i++ {
		typ, tbl := r.readNoTick(c)
		switch typ {
		case protocol.MessageEntityInfo:
			var e protocol.EntityInfo
			e.Init(tbl.Bytes, tbl.Pos)
			infos = append(infos, e)
		case protocol.MessageSnapshot:
			var s protocol.Snapshot
			s.Init(tbl.Bytes, tbl.Pos)
			if s.Tick() == want {
				return s, infos
			}
		}
	}
	r.t.Fatalf("never saw the snapshot of tick %d", want)
	return protocol.Snapshot{}, nil
}

func snapshotIDs(s protocol.Snapshot) map[uint16]bool {
	ids := map[uint16]bool{}
	var e protocol.Entity
	for j := 0; j < s.EntitiesLength(); j++ {
		s.Entities(&e, j)
		ids[e.Id()] = true
	}
	return ids
}

func addNPC(t *testing.T, w *world.World, typ world.NPCTypeID, pos world.Vec2) *world.NPC {
	t.Helper()
	n := &world.NPC{Type: typ, Pos: pos, Home: pos, HP: 1}
	if _, err := w.AddNPC(n); err != nil {
		t.Fatal(err)
	}
	if int(n.ID) <= session.MaxGuestID || n.ID > 0xFFFF {
		t.Fatalf("NPC id %d outside the 16-bit range above guest ids", n.ID)
	}
	return n
}

// AC1: NPCs within view radius are in the snapshot, players and NPCs together
// capped at the nearest interest.MaxVisible.
func TestSnapshotIncludesNearestPlayersAndNPCs(t *testing.T) {
	r := newRig(t, 1024)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	self, _ := r.expectWelcome(c)

	// 120 others at increasing distance, alternating player and NPC, all in
	// view; only the nearest MaxVisible may be sent.
	const others = interest.MaxVisible + 20
	want, all := map[uint16]bool{self: true}, map[uint16]bool{}
	for k := 0; k < others; k++ {
		pos := world.Vec2{X: 0.15 * float32(k+1)}
		var id uint16
		if k%2 == 0 {
			id = uint16(1000 + k)
			if err := r.w.AddPlayer(&world.Player{ID: world.PlayerID(id), Pos: pos}); err != nil {
				t.Fatal(err)
			}
		} else {
			id = uint16(addNPC(t, r.w, 1, pos).ID)
		}
		all[id] = true
		if k < interest.MaxVisible {
			want[id] = true
		}
	}
	far := addNPC(t, r.w, 1, world.Vec2{X: interest.ViewRadius + 1})

	s, _ := r.step(c)
	got := snapshotIDs(s)
	var e protocol.Entity
	if s.Entities(&e, 0); e.Id() != self {
		t.Fatalf("first entity %d, want self %d", e.Id(), self)
	}
	if got[uint16(far.ID)] {
		t.Fatalf("NPC %d outside view radius is in the snapshot", far.ID)
	}
	if len(got) != interest.MaxVisible+1 || s.EntitiesLength() != len(got) {
		t.Fatalf("snapshot has %d entities (%d distinct), want self + %d", s.EntitiesLength(), len(got), interest.MaxVisible)
	}
	npcs := 0
	for id := range got {
		if !want[id] {
			t.Fatalf("entity %d in snapshot is not among the nearest %d", id, interest.MaxVisible)
		}
		if int(id) > session.MaxGuestID {
			npcs++
		}
	}
	if npcs != interest.MaxVisible/2 {
		t.Fatalf("%d NPCs in snapshot, want %d", npcs, interest.MaxVisible/2)
	}

	// A removed NPC leaves the snapshot and frees its place for the next nearest.
	var nearNPC world.NPCID
	for _, n := range r.w.NPCs() {
		if want[uint16(n.ID)] {
			nearNPC = n.ID
			break
		}
	}
	r.w.RemoveNPC(nearNPC)
	s, _ = r.step(c)
	got = snapshotIDs(s)
	if got[uint16(nearNPC)] || len(got) != interest.MaxVisible+1 {
		t.Fatalf("after removing NPC %d: in snapshot %v, %d entities", nearNPC, got[uint16(nearNPC)], len(got))
	}
	for id := range got {
		if !all[id] && id != self {
			t.Fatalf("unknown entity %d", id)
		}
	}
}

// AC2: every client gets the NpcCatalog from the NPC table right after the
// ItemCatalog, on join and on reconnect.
func TestNpcCatalogOnJoinAndReconnect(t *testing.T) {
	d := testData(t)
	if len(d.NPCs) == 0 {
		t.Fatal("NPC table is empty")
	}
	r := newRig(t, 64)
	v := uint16(protocol.ProtocolVersionCurrent)

	check := func(c *client) {
		t.Helper()
		if typ, _ := r.next(c); typ != protocol.MessageItemCatalog {
			t.Fatalf("after Welcome got %v, want ItemCatalog", typ)
		}
		typ, tbl := r.next(c)
		if typ != protocol.MessageNpcCatalog {
			t.Fatalf("after ItemCatalog got %v, want NpcCatalog", typ)
		}
		var cat protocol.NpcCatalog
		cat.Init(tbl.Bytes, tbl.Pos)
		if cat.NpcsLength() != len(d.NPCs) {
			t.Fatalf("NpcCatalog has %d NPCs, want %d", cat.NpcsLength(), len(d.NPCs))
		}
		var def protocol.NpcDef
		for i, n := range d.NPCs {
			cat.Npcs(&def, i)
			if def.Id() != uint16(n.ID) || string(def.Name()) != n.Name || int(def.CombatLevel()) != n.Level {
				t.Fatalf("NpcDef %d = {%d %q %d}, want {%d %q %d}", i, def.Id(), def.Name(), def.CombatLevel(), n.ID, n.Name, n.Level)
			}
		}
	}

	c1 := r.dial(hello(v, ""))
	id, tok := r.expectWelcome(c1)
	check(c1)

	c1.c.Close(websocket.StatusNormalClosure, "")
	c2 := r.dial(hello(v, tok))
	if id2, _ := r.expectWelcome(c2); id2 != id {
		t.Fatalf("reconnect got id %d, want %d", id2, id)
	}
	check(c2)
}

// itemIn returns an item id worn in slot.
func itemIn(t *testing.T, d *gamedata.Data, slot gamedata.Slot) gamedata.ItemID {
	t.Helper()
	for _, it := range d.Items {
		if it.Slot == slot {
			return it.ID
		}
	}
	t.Fatalf("no item for slot %v", slot)
	return 0
}

// AC3: EntityInfo is sent for each entity when it enters view, again when a
// visible player's combat level or equipment changes, and not otherwise.
func TestEntityInfoOnEnterAndChange(t *testing.T) {
	d := testData(t)
	r := newRig(t, 1024)
	v := uint16(protocol.ProtocolVersionCurrent)
	c := r.dial(hello(v, ""))
	self, tok := r.expectWelcome(c)

	other := &world.Player{ID: 3000, Pos: world.Vec2{X: 2}, XP: 0}
	other.Inv.Equipment.Weapon = itemIn(t, d, gamedata.SlotWeapon)
	if err := r.w.AddPlayer(other); err != nil {
		t.Fatal(err)
	}
	npcType := d.NPCs[len(d.NPCs)-1]
	near := addNPC(t, r.w, world.NPCTypeID(npcType.ID), world.Vec2{Y: 3})
	far := addNPC(t, r.w, world.NPCTypeID(npcType.ID), world.Vec2{X: 500})

	byID := func(infos []protocol.EntityInfo) map[uint16]protocol.EntityInfo {
		t.Helper()
		m := map[uint16]protocol.EntityInfo{}
		for _, e := range infos {
			if _, dup := m[e.Id()]; dup {
				t.Fatalf("two EntityInfo for %d", e.Id())
			}
			m[e.Id()] = e
		}
		return m
	}
	checkPlayer := func(e protocol.EntityInfo, p *world.Player) {
		t.Helper()
		eq := p.Inv.Equipment
		if int(e.TotalLevel()) != p.TotalLevel(d.Rules) {
			t.Fatalf("EntityInfo %d total level %d, want %d", e.Id(), e.TotalLevel(), p.TotalLevel(d.Rules))
		}
		if e.Kind() != protocol.EntityKindPlayer || e.NpcType() != 0 || int(e.CombatLevel()) != p.CombatLevel(d.Rules) ||
			e.Weapon() != uint16(eq.Weapon) || e.Head() != uint16(eq.Head) || e.Body() != uint16(eq.Body) || e.Shield() != uint16(eq.Shield) {
			t.Fatalf("EntityInfo %d = kind %v type %d level %d equip %d/%d/%d/%d, want player level %d equip %+v",
				e.Id(), e.Kind(), e.NpcType(), e.CombatLevel(), e.Weapon(), e.Head(), e.Body(), e.Shield(), p.CombatLevel(d.Rules), eq)
		}
	}
	checkNPC := func(e protocol.EntityInfo) {
		t.Helper()
		if e.TotalLevel() != 0 {
			t.Fatalf("NPC EntityInfo %d total level %d, want 0", e.Id(), e.TotalLevel())
		}
		if e.Kind() != protocol.EntityKindNpc || e.NpcType() != uint16(npcType.ID) || int(e.CombatLevel()) != npcType.Level {
			t.Fatalf("EntityInfo %d = kind %v type %d level %d, want NPC type %d level %d",
				e.Id(), e.Kind(), e.NpcType(), e.CombatLevel(), npcType.ID, npcType.Level)
		}
	}
	// firstSight steps until other and near are in view and checks that the
	// client got exactly one EntityInfo per entity by then (its own may come
	// on an earlier tick).
	firstSight := func(c *client) {
		t.Helper()
		var all []protocol.EntityInfo
		for i := 0; ; i++ {
			s, infos := r.step(c)
			all = append(all, infos...)
			ids := snapshotIDs(s)
			if !ids[uint16(other.ID)] || !ids[uint16(near.ID)] {
				if i == 100 {
					t.Fatal("others never in view")
				}
				continue
			}
			m := byID(all)
			if len(m) != 3 {
				t.Fatalf("got EntityInfo for %d entities %v, want self, player and NPC", len(m), m)
			}
			checkPlayer(m[self], r.w.Player(world.PlayerID(self)))
			checkPlayer(m[uint16(other.ID)], other)
			checkNPC(m[uint16(near.ID)])
			if _, ok := m[uint16(far.ID)]; ok {
				t.Fatal("EntityInfo for an NPC out of view")
			}
			return
		}
	}
	quiet := func(c *client, what string) {
		t.Helper()
		for i := 0; i < 5; i++ {
			if _, infos := r.step(c); len(infos) != 0 {
				t.Fatalf("%s: %d EntityInfo on a tick where nothing changed", what, len(infos))
			}
		}
	}
	only := func(c *client, id uint16, what string) protocol.EntityInfo {
		t.Helper()
		_, infos := r.step(c)
		if len(infos) != 1 || infos[0].Id() != id {
			t.Fatalf("%s: got %d EntityInfo, want one for %d", what, len(infos), id)
		}
		return infos[0]
	}

	firstSight(c)
	quiet(c, "after first sight")

	other.Inv.Equipment.Head = itemIn(t, d, gamedata.SlotHead)
	checkPlayer(only(c, uint16(other.ID), "equipment change"), other)
	quiet(c, "after equipment change")

	lvl := other.CombatLevel(d.Rules)
	for other.CombatLevel(d.Rules) == lvl {
		other.XP += 10
	}
	checkPlayer(only(c, uint16(other.ID), "level change"), other)
	quiet(c, "after level change")

	// A skill level-up changes the total level too.
	before := other.TotalLevel(d.Rules)
	for other.TotalLevel(d.Rules) == before {
		other.AddSkillXP(world.Skill(0), 100)
	}
	checkPlayer(only(c, uint16(other.ID), "skill level-up"), other)
	quiet(c, "after skill level-up")

	// Leaving view and coming back counts as entering view again.
	near.Pos = world.Vec2{X: -500}
	if s, infos := r.step(c); snapshotIDs(s)[uint16(near.ID)] || len(infos) != 0 {
		t.Fatalf("NPC left view: still in snapshot or %d EntityInfo", len(infos))
	}
	near.Pos = world.Vec2{Y: 3}
	checkNPC(only(c, uint16(near.ID), "NPC re-entered view"))
	quiet(c, "after NPC re-entered")

	// A reconnect starts with no EntityInfo, so everything in view is sent again.
	c.c.Close(websocket.StatusNormalClosure, "")
	c2 := r.dial(hello(v, tok))
	r.expectWelcome(c2)
	firstSight(c2)
	quiet(c2, "after reconnect")
}

// A crowd entering view at once, with the production send queue, still gets
// every tick's Snapshot and, within a few ticks, exactly one EntityInfo per
// entity: none are lost to a full queue.
func TestEntityInfoForCrowdFitsSendQueue(t *testing.T) {
	r := newRig(t, transport.DefaultSendQueue)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	r.expectWelcome(c)
	r.step(c)
	for k := 0; k < interest.MaxVisible; k++ {
		addNPC(t, r.w, 1, world.Vec2{X: 0.15 * float32(k+1)})
	}
	seen := map[uint16]int{}
	var s protocol.Snapshot
	for i := 0; i < 10; i++ {
		var infos []protocol.EntityInfo
		s, infos = r.step(c) // fails if this tick's Snapshot was dropped
		for _, e := range infos {
			seen[e.Id()]++
		}
	}
	ids := snapshotIDs(s)
	if len(ids) != interest.MaxVisible+1 {
		t.Fatalf("snapshot has %d entities, want %d", len(ids), interest.MaxVisible+1)
	}
	for id := range ids {
		if n := seen[id]; n != 1 && !(n == 0 && int(id) <= session.MaxGuestID) { // self's came on the first step
			t.Fatalf("entity %d got %d EntityInfo, want 1", id, n)
		}
	}
}
