package world_test

import (
	"testing"

	"github.com/cleiderg/lush/server/internal/world"
)

func TestNPCState(t *testing.T) {
	w := world.New(world.DefaultConfig())
	n := &world.NPC{Type: 3, Pos: world.Vec2{X: 1, Y: 2}, Home: world.Vec2{X: 4, Y: 5}, HP: 7, Facing: 1.5, Anim: world.AnimWalk}
	id, err := w.AddNPC(n)
	if err != nil {
		t.Fatal(err)
	}
	if id != world.DefaultNPCIDMin || n.ID != id {
		t.Fatalf("id %d (n.ID %d), want %d", id, n.ID, world.DefaultNPCIDMin)
	}
	got := w.NPC(id)
	if got != n || got.Type != 3 || got.Pos != (world.Vec2{X: 1, Y: 2}) || got.Home != (world.Vec2{X: 4, Y: 5}) ||
		got.HP != 7 || got.Facing != 1.5 || got.Anim != world.AnimWalk {
		t.Fatalf("bad npc %+v", got)
	}
	w.RemoveNPC(id)
	if w.NPC(id) != nil || len(w.NPCs()) != 0 {
		t.Fatal("not removed")
	}
	w.RemoveNPC(id) // removing twice is a no-op
}

func TestNPCsDeterministicOrder(t *testing.T) {
	w := world.New(world.Config{TickRate: 20, NPCIDMin: 100, NPCIDMax: 199})
	var ids []world.NPCID
	for i := 0; i < 5; i++ {
		id, err := w.AddNPC(&world.NPC{HP: i})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	w.RemoveNPC(ids[1])
	id, err := w.AddNPC(&world.NPC{HP: 5})
	if err != nil {
		t.Fatal(err)
	}
	if id != 105 {
		t.Fatalf("new id %d, want 105 (ids rotate, not reused at once)", id)
	}
	wantHP := []int{0, 2, 3, 4, 5}
	for run := 0; run < 3; run++ {
		got := w.NPCs()
		if len(got) != len(wantHP) {
			t.Fatalf("%d npcs, want %d", len(got), len(wantHP))
		}
		for i, n := range got {
			if n.HP != wantHP[i] {
				t.Fatalf("run %d: order %v, want HP order %v", run, got, wantHP)
			}
		}
	}
}

func TestAddNPCRangeFull(t *testing.T) {
	tests := []struct {
		name     string
		min, max world.NPCID
		fits     int
	}{
		{"three ids", 10, 12, 3},
		{"single id", 7, 7, 1},
		{"top of id space", 0xFFFE, 0xFFFF, 2},
		{"empty range", 9, 8, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := world.New(world.Config{TickRate: 20, NPCIDMin: tc.min, NPCIDMax: tc.max})
			seen := map[world.NPCID]bool{}
			for i := 0; i < tc.fits; i++ {
				id, err := w.AddNPC(&world.NPC{})
				if err != nil {
					t.Fatalf("add %d: %v", i, err)
				}
				if id < tc.min || id > tc.max || seen[id] {
					t.Fatalf("add %d: id %d out of range or duplicate", i, id)
				}
				seen[id] = true
			}
			if _, err := w.AddNPC(&world.NPC{}); err == nil {
				t.Fatal("AddNPC succeeded on a full range")
			}
			if tc.fits == 0 {
				return
			}
			// Freeing one id makes room again, and that id is reused.
			freed := w.NPCs()[0].ID
			w.RemoveNPC(freed)
			id, err := w.AddNPC(&world.NPC{})
			if err != nil || id != freed {
				t.Fatalf("after remove: id %d err %v, want %d", id, err, freed)
			}
		})
	}
}

func TestAddPlayerRejectsNPCRange(t *testing.T) {
	tests := []struct {
		name string
		cfg  world.Config
		id   world.PlayerID
		ok   bool
	}{
		{"default below range", world.DefaultConfig(), world.PlayerID(world.DefaultNPCIDMin) - 1, true},
		{"default range start", world.DefaultConfig(), world.PlayerID(world.DefaultNPCIDMin), false},
		{"default range end", world.DefaultConfig(), world.PlayerID(world.DefaultNPCIDMax), false},
		{"default above range", world.DefaultConfig(), world.PlayerID(world.DefaultNPCIDMax) + 1, true},
		{"unset range uses default", world.Config{TickRate: 20}, world.PlayerID(world.DefaultNPCIDMin), false},
		{"custom range inside", world.Config{NPCIDMin: 10, NPCIDMax: 20}, 15, false},
		{"custom range outside", world.Config{NPCIDMin: 10, NPCIDMax: 20}, 21, true},
		{"empty range", world.Config{NPCIDMin: 20, NPCIDMax: 10}, 15, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := world.New(tc.cfg)
			err := w.AddPlayer(&world.Player{ID: tc.id})
			if (err == nil) != tc.ok {
				t.Fatalf("AddPlayer(%d) err %v, want ok=%v", tc.id, err, tc.ok)
			}
		})
	}
}
