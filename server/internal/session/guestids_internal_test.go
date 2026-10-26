package session

import (
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/world"
)

// npcIDMin is the start of the world's NPC id range (#142).
const npcIDMin = world.PlayerID(world.DefaultNPCIDMin)

func newIDServer(t *testing.T, maxGuests int) *Server {
	t.Helper()
	w := world.New(world.DefaultConfig())
	return New(Config{TickRate: 20, MaxGuests: maxGuests}, w, world.NewFakeClock(time.Unix(0, 0)), store.NewMemory(), nil)
}

func assign(t *testing.T, s *Server, storeID int64) *guest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.guestLocked(store.Player{ID: storeID, Version: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGuestIDCapBelowNPCRange(t *testing.T) {
	if MaxGuestID != int(npcIDMin)-1 {
		t.Fatalf("MaxGuestID = %#x, want %#x", MaxGuestID, npcIDMin-1)
	}
	for _, mg := range []int{0, -1, 0xC000, 0xFFFF, 1 << 20} {
		if got := newIDServer(t, mg).cfg.MaxGuests; got != MaxGuestID {
			t.Errorf("MaxGuests %d became %d, want %d", mg, got, MaxGuestID)
		}
	}
	if got := newIDServer(t, 2).cfg.MaxGuests; got != 2 {
		t.Errorf("MaxGuests 2 became %d", got)
	}
}

func TestGuestIDsStayBelowNPCRange(t *testing.T) {
	s := newIDServer(t, 0) // default: the cap
	var all []*guest
	var highest world.PlayerID
	for i := int64(1); i <= int64(MaxGuestID); i++ {
		g := assign(t, s, i)
		if g == nil {
			t.Fatalf("guest %d refused before every id was in use", i)
		}
		if g.id == 0 || g.id >= npcIDMin {
			t.Fatalf("guest %d got id %#x, outside 1..%#x", i, g.id, MaxGuestID)
		}
		highest = max(highest, g.id)
		all = append(all, g)
	}
	if highest != 0xBFFF {
		t.Fatalf("highest id handed out %#x, want 0xBFFF", highest)
	}

	// Every id online: the next guest is refused cleanly.
	next := int64(MaxGuestID + 1)
	if g := assign(t, s, next); g != nil {
		t.Fatalf("guest past the cap got id %#x, want refused", g.id)
	}

	// Freed ids are reused, longest offline first, and never reach the NPC range.
	freed := []*guest{all[len(all)-1], all[0], all[1000]}
	for _, g := range freed {
		s.idleIfGone(g)
	}
	for _, old := range freed {
		g := assign(t, s, next)
		if g == nil {
			t.Fatal("guest refused while an id was free")
		}
		if g.id != old.id || g.id >= npcIDMin {
			t.Fatalf("new guest got id %#x, want reused %#x below %#x", g.id, old.id, npcIDMin)
		}
		next++
	}
	if g := assign(t, s, next); g != nil {
		t.Fatalf("guest got id %#x after reused ids ran out, want refused", g.id)
	}
}
