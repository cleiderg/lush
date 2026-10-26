package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/world"
)

// seenBy reports whether c's first snapshot from the current tick on lists id.
func seenBy(r *rig, c *client, id world.PlayerID) bool {
	r.t.Helper()
	from := uint32(r.w.TickCount())
	for {
		tbl := r.waitFor(c, protocol.MessageSnapshot)
		var s protocol.Snapshot
		s.Init(tbl.Bytes, tbl.Pos)
		if s.Tick() < from {
			continue
		}
		var e protocol.Entity
		for j := 0; j < s.EntitiesLength(); j++ {
			if s.Entities(&e, j); world.PlayerID(e.Id()) == id {
				return true
			}
		}
		return false
	}
}

func savedXP(r *saveRig, sid int64, xp int64) func() bool {
	return func() bool {
		for _, s := range r.st.snapshot() {
			if s.p.ID == sid && s.err == nil && s.p.XP == xp {
				return true
			}
		}
		return false
	}
}

// AC1: a disconnected player stays in the world, seen by others, for
// logout_combat_lock_s from the rules data, then is removed and saved.
func TestDisconnectedPlayerStaysForLogoutLock(t *testing.T) {
	for _, secs := range []int{10, 3} {
		t.Run(time.Duration(secs*int(time.Second)).String(), func(t *testing.T) {
			c := cfg()
			c.Data = testData(t)
			c.Data.Rules.LogoutCombatLockSeconds = secs
			lock := c.Data.Rules.LogoutCombatLock()
			r := newSaveRigCfg(t, c)
			a, sid, ca := r.join()
			_, _, cb := r.join()
			r.w.Tick()

			ca.c.CloseNow()
			r.tickUntilDisconnected(a)
			r.clock.Advance(lock - time.Second)
			r.w.Tick()
			if r.w.Player(a) == nil {
				t.Fatalf("player removed %v after disconnect, lock is %v", lock-time.Second, lock)
			}
			if !seenBy(r.rig, cb, a) {
				t.Fatal("disconnected player missing from another player's snapshot")
			}

			r.w.Player(a).XP = 4242
			r.clock.Advance(time.Second)
			r.w.Tick()
			if r.w.Player(a) != nil {
				t.Fatalf("player still in the world %v after disconnect", lock)
			}
			if seenBy(r.rig, cb, a) {
				t.Fatal("removed player still in another player's snapshot")
			}
			r.st.waitFor(t, "save on removal", savedXP(r, sid, 4242))
		})
	}
}

// AC2: a player in combat when the timer runs out stays until LastCombat + lock.
func TestDisconnectedPlayerInCombatStaysUntilLastCombatPlusLock(t *testing.T) {
	r := newSaveRig(t)
	a, _, ca := r.join()
	r.w.Tick()
	ca.c.CloseNow()
	r.tickUntilDisconnected(a)
	gone := r.clock.Now()

	r.clock.Advance(6 * time.Second)
	lastCombat := r.clock.Now() // hit while disconnected
	r.w.Player(a).MarkCombat(lastCombat)
	r.w.Tick()

	r.clock.Advance(gone.Add(lock).Sub(r.clock.Now()))
	r.w.Tick()
	if r.w.Player(a) == nil {
		t.Fatal("player in combat removed when the disconnect timer ran out")
	}
	r.clock.Advance(lastCombat.Add(lock - time.Millisecond).Sub(r.clock.Now()))
	r.w.Tick()
	if r.w.Player(a) == nil {
		t.Fatal("player removed before LastCombat + lock")
	}
	r.clock.Advance(time.Millisecond)
	r.w.Tick()
	if r.w.Player(a) != nil {
		t.Fatal("player still in the world at LastCombat + lock")
	}
}

// AC3: reconnecting during the window takes over the same world player.
func TestReconnectDuringLogoutLockResumesSamePlayer(t *testing.T) {
	r := newSaveRig(t)
	v := uint16(protocol.ProtocolVersionCurrent)
	c := r.dial(hello(v, ""))
	pid, tok := r.expectWelcome(c)
	id := world.PlayerID(pid)
	r.w.Tick()

	p := r.w.Player(id)
	p.Pos = world.Vec2{X: 12, Y: -4}
	p.HP = p.MaxHP(testData(t).Rules) - 3
	p.Inv.Gold += 17
	wantPos, wantHP, wantGold, wantSlots := p.Pos, p.HP, p.Inv.Gold, append([]world.Stack(nil), p.Inv.Slots...)

	c.c.CloseNow()
	r.tickUntilDisconnected(id)
	r.clock.Advance(lock / 2)
	r.w.Tick()

	c2 := r.dial(hello(v, tok))
	if got, _ := r.expectWelcome(c2); world.PlayerID(got) != id {
		t.Fatalf("reconnect got id %d, want %d", got, id)
	}
	if n := len(r.w.Players()); n != 1 {
		t.Fatalf("%d players in the world after reconnect, want 1", n)
	}
	q := r.w.Player(id)
	if q != p {
		t.Fatal("reconnect created a new world player")
	}
	if q.Pos != wantPos || q.HP != wantHP || q.Inv.Gold != wantGold || len(q.Inv.Slots) != len(wantSlots) {
		t.Fatalf("resumed pos %v hp %d gold %d slots %d, want %v %d %d %d",
			q.Pos, q.HP, q.Inv.Gold, len(q.Inv.Slots), wantPos, wantHP, wantGold, len(wantSlots))
	}
	for i := range wantSlots {
		if q.Inv.Slots[i] != wantSlots[i] {
			t.Fatalf("slot %d is %+v, want %+v", i, q.Inv.Slots[i], wantSlots[i])
		}
	}

	// The old window no longer applies to the live connection.
	r.clock.Advance(2 * lock)
	r.w.Tick()
	if r.w.Player(id) == nil || session.Disconnected(r.sess, id) {
		t.Fatal("reconnected player removed when the old logout lock ran out")
	}
}

// AC4: SaveAll at shutdown saves players waiting out the window.
func TestSaveAllSavesDisconnectedPlayers(t *testing.T) {
	r := newSaveRig(t)
	a, sid, ca := r.join()
	r.w.Tick()
	ca.c.CloseNow()
	r.tickUntilDisconnected(a)
	r.w.Player(a).XP = 555

	if err := r.sess.SaveAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !savedXP(r, sid, 555)() {
		t.Fatalf("shutdown did not save the disconnected player: %+v", r.st.snapshot())
	}
}
