package session_test

import (
	"context"
	"testing"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/world"
)

// savedSkills reports whether a successful save of store id sid carried want.
func savedSkills(r *saveRig, sid int64, want store.SkillXP) func() bool {
	return func() bool {
		for _, s := range r.st.snapshot() {
			if s.p.ID == sid && s.err == nil && s.p.Skills == want {
				return true
			}
		}
		return false
	}
}

// AC3: skill XP added in the world is written by the periodic, logout and
// shutdown saves, a rejoin with the same token gets it back, and a fresh join
// after a restart loads it from the store.
func TestSkillXPSavedAndLoadedOnJoin(t *testing.T) {
	r := newSaveRig(t)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	wid, tok := r.expectWelcome(c)
	id := world.PlayerID(wid)
	sp, err := r.st.Players.LoadByTokenHash(context.Background(), sha(tok))
	if err != nil {
		t.Fatal(err)
	}
	sid := sp.ID
	r.w.Tick() // arms the interval
	p := r.w.Player(id)
	if p == nil {
		t.Fatal("player not in world")
	}
	if p.SkillXP != ([world.NumSkills]int{}) {
		t.Fatalf("new guest skill xp = %v, want zero", p.SkillXP)
	}

	// Periodic save.
	p.AddSkillXP(world.Woodcutting, 250)
	p.AddSkillXP(world.Fishing, 15)
	r.intervalPasses()
	r.st.waitFor(t, "periodic save with skill XP", savedSkills(r, sid, store.SkillXP{Woodcutting: 250, Fishing: 15}))

	// Shutdown save.
	p.AddSkillXP(world.Cooking, 300)
	if err := r.sess.SaveAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !savedSkills(r, sid, store.SkillXP{Woodcutting: 250, Fishing: 15, Cooking: 300})() {
		t.Fatalf("shutdown save missing skill XP; saves: %+v", r.st.snapshot())
	}

	// Logout save.
	p.AddSkillXP(world.Fishing, 5)
	want := store.SkillXP{Woodcutting: 250, Fishing: 20, Cooking: 300}
	c.c.CloseNow()
	r.tickUntilGone(id)
	r.st.waitFor(t, "logout save with skill XP", savedSkills(r, sid, want))

	// A rejoin with the same token gets it back.
	c2 := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), tok))
	wid2, _ := r.expectWelcome(c2)
	r.w.Tick()
	p2 := r.w.Player(world.PlayerID(wid2))
	if p2 == nil {
		t.Fatal("rejoined player not in world")
	}
	var wantWorld [world.NumSkills]int
	wantWorld[world.Woodcutting], wantWorld[world.Fishing], wantWorld[world.Cooking] = 250, 20, 300
	if p2.SkillXP != wantWorld {
		t.Fatalf("rejoined skill xp = %v, want %v", p2.SkillXP, wantWorld)
	}
	if p2.SkillLevel(world.Woodcutting) != world.SkillLevelForXP(250) {
		t.Fatalf("rejoined woodcutting level = %d", p2.SkillLevel(world.Woodcutting))
	}

	// "Restart": a new world and session over the same store, so the join
	// loads skill XP from the stored record.
	r2 := newRigStore(t, 16, cfg(), r.st.Players)
	c3 := r2.dial(hello(uint16(protocol.ProtocolVersionCurrent), tok))
	wid3, _ := r2.expectWelcome(c3)
	r2.w.Tick()
	p3 := r2.w.Player(world.PlayerID(wid3))
	if p3 == nil {
		t.Fatal("player not in world after restart")
	}
	if p3.SkillXP != wantWorld {
		t.Fatalf("skill xp after restart = %v, want %v", p3.SkillXP, wantWorld)
	}
}
