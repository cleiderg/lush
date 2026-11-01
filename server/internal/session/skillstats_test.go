package session_test

import (
	"context"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/world"
)

// msg is one received message.
type msg struct {
	typ protocol.Message
	tbl flatbuffers.Table
}

// readThrough reads c's messages up to and including the Snapshot for world
// tick tick (or a later one), ticking while waiting.
func (r *rig) readThrough(c *client, tick uint64) []msg {
	r.t.Helper()
	var got []msg
	for i := 0; i < 1000; i++ {
		typ, tbl := r.next(c)
		got = append(got, msg{typ, tbl})
		if typ == protocol.MessageSnapshot {
			var s protocol.Snapshot
			s.Init(tbl.Bytes, tbl.Pos)
			if uint64(s.Tick()) >= tick {
				return got
			}
		}
	}
	r.t.Fatalf("never saw the snapshot for tick %d", tick)
	return nil
}

func ofType(ms []msg, typ protocol.Message) []flatbuffers.Table {
	var out []flatbuffers.Table
	for _, m := range ms {
		if m.typ == typ {
			out = append(out, m.tbl)
		}
	}
	return out
}

type skillStats struct {
	wcLevel, fLevel, cLevel uint8
	wcXP, fXP, cXP          uint32
}

func decodeSkillStats(tbl flatbuffers.Table) skillStats {
	var m protocol.SkillStats
	m.Init(tbl.Bytes, tbl.Pos)
	return skillStats{
		wcLevel: m.WoodcuttingLevel(), wcXP: m.WoodcuttingXp(),
		fLevel: m.FishingLevel(), fXP: m.FishingXp(),
		cLevel: m.CookingLevel(), cXP: m.CookingXp(),
	}
}

func wantSkillStats(xp [world.NumSkills]int) skillStats {
	return skillStats{
		wcLevel: uint8(world.SkillLevelForXP(xp[world.Woodcutting])), wcXP: uint32(xp[world.Woodcutting]),
		fLevel: uint8(world.SkillLevelForXP(xp[world.Fishing])), fXP: uint32(xp[world.Fishing]),
		cLevel: uint8(world.SkillLevelForXP(xp[world.Cooking])), cXP: uint32(xp[world.Cooking]),
	}
}

type levelUpMsg struct {
	id    uint16
	skill protocol.Skill
	level uint8
}

func levelUps(ms []msg) []levelUpMsg {
	var out []levelUpMsg
	for _, tbl := range ofType(ms, protocol.MessageLevelUp) {
		var m protocol.LevelUp
		m.Init(tbl.Bytes, tbl.Pos)
		out = append(out, levelUpMsg{m.EntityId(), m.Skill(), m.Level()})
	}
	return out
}

func TestSkillConstantsMatchProtocol(t *testing.T) {
	if byte(world.Woodcutting) != byte(protocol.SkillWoodcutting) ||
		byte(world.Fishing) != byte(protocol.SkillFishing) ||
		byte(world.Cooking) != byte(protocol.SkillCooking) {
		t.Fatal("world.Skill constants differ from protocol.Skill")
	}
}

// AC1: a joining player gets one SkillStats with the levels and XP (in
// tenths) loaded from the store, and the loaded levels are not level-ups.
func TestJoinSendsStoredSkillStats(t *testing.T) {
	mem := store.NewMemory()
	const tok = "skill-stats-token"
	ctx := context.Background()
	sp, err := mem.CreateGuest(ctx, sha(tok), store.Holdings{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sp.Skills = store.SkillXP{Woodcutting: 8305, Fishing: 15, Cooking: 1234}
	if _, err := mem.Save(ctx, sp); err != nil {
		t.Fatal(err)
	}
	r := newRigStore(t, 64, cfg(), mem)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), tok))
	r.expectWelcome(c)
	r.w.Tick()
	ms := r.readThrough(c, r.w.TickCount()+3)

	got := ofType(ms, protocol.MessageSkillStats)
	if len(got) != 1 {
		t.Fatalf("got %d SkillStats, want 1", len(got))
	}
	var xp [world.NumSkills]int
	xp[world.Woodcutting], xp[world.Fishing], xp[world.Cooking] = 8305, 15, 1234
	want := wantSkillStats(xp)
	if want.wcLevel < 2 || want.cLevel < 2 {
		t.Fatalf("test XP too low to check levels: %+v", want)
	}
	if s := decodeSkillStats(got[0]); s != want {
		t.Fatalf("SkillStats = %+v, want %+v", s, want)
	}
	if u := levelUps(ms); len(u) != 0 {
		t.Fatalf("join sent LevelUp %+v", u)
	}
}

// AC2: skill XP added in the world goes out as a new SkillStats on the next
// tick; ticks with no change send none.
func TestSkillXPChangeSendsSkillStats(t *testing.T) {
	r := newRig(t, 64)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	wid, _ := r.expectWelcome(c)
	r.w.Tick()
	if got := ofType(r.readThrough(c, r.w.TickCount()), protocol.MessageSkillStats); len(got) != 1 {
		t.Fatalf("join sent %d SkillStats, want 1", len(got))
	}
	p := r.w.Player(world.PlayerID(wid))
	if p == nil {
		t.Fatal("player not in world")
	}

	// No change: no SkillStats.
	if got := ofType(r.readThrough(c, r.w.TickCount()+5), protocol.MessageSkillStats); len(got) != 0 {
		t.Fatalf("unchanged ticks sent %d SkillStats", len(got))
	}

	// A change: one SkillStats on the next tick, with no level gained.
	p.AddSkillXP(world.Fishing, 75)
	r.w.Tick()
	ms := r.readThrough(c, r.w.TickCount())
	got := ofType(ms, protocol.MessageSkillStats)
	if len(got) != 1 {
		t.Fatalf("tick after XP sent %d SkillStats, want 1", len(got))
	}
	if s, want := decodeSkillStats(got[0]), wantSkillStats(p.SkillXP); s != want || s.fXP != 75 {
		t.Fatalf("SkillStats = %+v, want %+v", s, want)
	}
	if u := levelUps(ms); len(u) != 0 {
		t.Fatalf("XP without a level sent LevelUp %+v", u)
	}

	// And nothing again after it.
	if got := ofType(r.readThrough(c, r.w.TickCount()+5), protocol.MessageSkillStats); len(got) != 0 {
		t.Fatalf("unchanged ticks sent %d SkillStats", len(got))
	}
}

// AC3: a level-up sends LevelUp to the player and every other player who has
// them in view, and not to players out of view.
func TestLevelUpSentToPlayersInView(t *testing.T) {
	r := newRig(t, 64)
	ca := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	aid, _ := r.expectWelcome(ca)
	cb := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	r.expectWelcome(cb)
	cc := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	cid, _ := r.expectWelcome(cc)
	r.w.Tick()
	pa, pc := r.w.Player(world.PlayerID(aid)), r.w.Player(world.PlayerID(cid))
	if pa == nil || pc == nil {
		t.Fatal("players not in world")
	}
	pc.Pos = world.Vec2{X: pa.Pos.X + 500, Y: pa.Pos.Y + 500} // far out of view
	r.w.Tick()
	sync := r.w.TickCount()
	for _, c := range []*client{ca, cb, cc} {
		r.readThrough(c, sync)
	}

	// 830 tenths (83 XP) is level 2.
	if leveled, level := pa.AddSkillXP(world.Woodcutting, 830); !leveled || level != 2 {
		t.Fatalf("AddSkillXP = %v, %d; want a level-up to 2", leveled, level)
	}
	r.w.Tick()
	tick := r.w.TickCount()
	want := []levelUpMsg{{id: aid, skill: protocol.SkillWoodcutting, level: 2}}
	for _, tc := range []struct {
		name string
		c    *client
		want []levelUpMsg
	}{
		{"leveling player", ca, want},
		{"player in view", cb, want},
		{"player out of view", cc, nil},
	} {
		ms := r.readThrough(tc.c, tick)
		if got := levelUps(ms); len(got) != len(tc.want) || len(got) == 1 && got[0] != tc.want[0] {
			t.Errorf("%s: LevelUp %+v, want %+v", tc.name, got, tc.want)
		}
	}

	// Only once: later ticks send no more LevelUp.
	for _, c := range []*client{ca, cb, cc} {
		if got := levelUps(r.readThrough(c, r.w.TickCount()+3)); len(got) != 0 {
			t.Fatalf("later ticks sent LevelUp %+v", got)
		}
	}
}
