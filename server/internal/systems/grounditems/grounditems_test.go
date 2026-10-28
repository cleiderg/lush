package grounditems_test

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/grounditems"
	"github.com/cleiderg/lush/server/internal/world"
)

// tileSize is the collision grid's cell size in metres (shared/map/config.mjs
// CELL_SIZE).
const tileSize = 1.0

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func gameData(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func itemID(t testing.TB, d *gamedata.Data, key string) gamedata.ItemID {
	t.Helper()
	it, ok := d.ItemByKey(key)
	if !ok {
		t.Fatalf("no item %q", key)
	}
	return it.ID
}

func newPlayer(d *gamedata.Data, id world.PlayerID, pos world.Vec2) *world.Player {
	return &world.Player{ID: id, Pos: pos, Inv: world.NewInventory(d)}
}

func mustSpawn(t testing.TB, s *grounditems.System, item gamedata.ItemID, qty int, pos world.Vec2, owner world.PlayerID, at time.Time) grounditems.ID {
	t.Helper()
	id, err := s.Spawn(item, qty, pos, owner, at)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSpawnValidates(t *testing.T) {
	d := gameData(t)
	s := grounditems.New(d, world.NewFakeClock(t0), tileSize)
	acorn := itemID(t, d, "acorn")
	tests := []struct {
		name  string
		item  gamedata.ItemID
		qty   int
		owner world.PlayerID
		want  error
	}{
		{"unknown item", 9999, 1, 1, grounditems.ErrUnknownItem},
		{"zero quantity", acorn, 0, 1, grounditems.ErrBadQuantity},
		{"negative quantity", acorn, -3, 1, grounditems.ErrBadQuantity},
		{"no owner", acorn, 1, 0, grounditems.ErrUnknownOwner},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Spawn(tt.item, tt.qty, world.Vec2{}, tt.owner, t0); !errors.Is(err, tt.want) {
				t.Fatalf("Spawn err = %v, want %v", err, tt.want)
			}
			if s.Len() != 0 {
				t.Fatalf("Len = %d after failed spawn", s.Len())
			}
		})
	}
}

func TestSpawnStoresItem(t *testing.T) {
	d := gameData(t)
	s := grounditems.New(d, world.NewFakeClock(t0), tileSize)
	acorn := itemID(t, d, "acorn")
	id := mustSpawn(t, s, acorn, 4, world.Vec2{X: 3, Y: 5}, 7, t0)
	got, ok := s.Get(id)
	want := grounditems.Item{ID: id, Item: acorn, Qty: 4, Pos: world.Vec2{X: 3, Y: 5}, Owner: 7, Spawned: t0}
	if !ok || got != want {
		t.Fatalf("Get(%d) = %+v, %v; want %+v", id, got, ok, want)
	}
}

func TestIDAllocation(t *testing.T) {
	d := gameData(t)
	acorn := itemID(t, d, "acorn")

	t.Run("sequential and not reused right away", func(t *testing.T) {
		clock := world.NewFakeClock(t0)
		s := grounditems.New(d, clock, tileSize)
		var ids []grounditems.ID
		for range 3 {
			ids = append(ids, mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, t0))
		}
		if want := []grounditems.ID{1, 2, 3}; !reflect.DeepEqual(ids, want) {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
		p := newPlayer(d, 1, world.Vec2{})
		if err := s.PickUp(p, 2); err != nil {
			t.Fatal(err)
		}
		if id := mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, t0); id != 4 {
			t.Fatalf("next id = %d, want 4", id)
		}
	})

	t.Run("wraps around skipping ids in use", func(t *testing.T) {
		clock := world.NewFakeClock(t0)
		s := grounditems.New(d, clock, tileSize)
		late := t0.Add(time.Minute)
		// Ids 1 and 2 outlive the rest.
		mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, late)
		mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, late)
		for i := 3; i <= math.MaxUint16; i++ {
			if id := mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, t0); int(id) != i {
				t.Fatalf("id = %d, want %d", id, i)
			}
		}
		if _, err := s.Spawn(acorn, 1, world.Vec2{}, 1, t0); !errors.Is(err, grounditems.ErrNoFreeID) {
			t.Fatalf("Spawn with every id in use: err = %v, want ErrNoFreeID", err)
		}
		if s.Len() != math.MaxUint16 {
			t.Fatalf("Len = %d, want %d", s.Len(), math.MaxUint16)
		}

		// The early items despawn; 1 and 2 still exist.
		clock.Advance(d.Rules.DropDespawn())
		s.Tick(nil, 0)
		if s.Len() != 2 {
			t.Fatalf("Len after despawn = %d, want 2", s.Len())
		}
		for _, want := range []grounditems.ID{3, 4} {
			if id := mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, clock.Now()); id != want {
				t.Fatalf("id after wrap = %d, want %d", id, want)
			}
		}
		for _, id := range []grounditems.ID{1, 2} {
			if it, ok := s.Get(id); !ok || !it.Spawned.Equal(late) {
				t.Fatalf("item %d = %+v, %v; want the original late item", id, it, ok)
			}
		}
	})
}

func TestNear(t *testing.T) {
	d := gameData(t)
	s := grounditems.New(d, world.NewFakeClock(t0), tileSize)
	acorn := itemID(t, d, "acorn")
	a := mustSpawn(t, s, acorn, 1, world.Vec2{X: 0, Y: 0}, 1, t0)
	b := mustSpawn(t, s, acorn, 1, world.Vec2{X: 3, Y: 4}, 1, t0) // 5 m from the origin
	c := mustSpawn(t, s, acorn, 1, world.Vec2{X: -10, Y: 0}, 1, t0)
	tests := []struct {
		name   string
		pos    world.Vec2
		radius float64
		want   []grounditems.ID
	}{
		{"zero radius on an item", world.Vec2{}, 0, []grounditems.ID{a}},
		{"boundary counts", world.Vec2{}, 5, []grounditems.ID{a, b}},
		{"just short of the boundary", world.Vec2{}, 4.99, []grounditems.ID{a}},
		{"everything", world.Vec2{}, 100, []grounditems.ID{a, b, c}},
		{"other side", world.Vec2{X: -9, Y: 1}, 2, []grounditems.ID{c}},
		{"nothing", world.Vec2{X: 50, Y: 50}, 5, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []grounditems.ID
			for _, it := range s.Near(nil, tt.pos, tt.radius) {
				got = append(got, it.ID)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Near = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestReservationAndDespawn: only the owner before Rules.DropReservation,
// anyone after it, nobody once Rules.DropDespawn is reached.
func TestReservationAndDespawn(t *testing.T) {
	d := gameData(t)
	acorn := itemID(t, d, "acorn")
	reserve, despawn := d.Rules.DropReservation(), d.Rules.DropDespawn()
	const owner, other world.PlayerID = 1, 2
	tests := []struct {
		name    string
		age     time.Duration
		picker  world.PlayerID
		wantErr error
	}{
		{"owner at once", 0, owner, nil},
		{"other at once", 0, other, grounditems.ErrReserved},
		{"owner just before reservation ends", reserve - time.Millisecond, owner, nil},
		{"other just before reservation ends", reserve - time.Millisecond, other, grounditems.ErrReserved},
		{"other when reservation ends", reserve, other, nil},
		{"owner after reservation", reserve + time.Second, owner, nil},
		{"other just before despawn", despawn - time.Millisecond, other, nil},
		{"owner at despawn", despawn, owner, grounditems.ErrNoSuchItem},
		{"other at despawn", despawn, other, grounditems.ErrNoSuchItem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := world.NewFakeClock(t0)
			s := grounditems.New(d, clock, tileSize)
			id := mustSpawn(t, s, acorn, 2, world.Vec2{}, owner, t0)
			clock.Advance(tt.age)
			s.Tick(nil, 0)
			p := newPlayer(d, tt.picker, world.Vec2{})
			err := s.PickUp(p, id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("PickUp err = %v, want %v", err, tt.wantErr)
			}
			_, exists := s.Get(id)
			if tt.wantErr == nil {
				if exists || p.Inv.Count(acorn) != 2 || len(p.PendingEconomy()) != 1 {
					t.Fatalf("after pick-up: exists=%v count=%d events=%d", exists, p.Inv.Count(acorn), len(p.PendingEconomy()))
				}
				return
			}
			if p.Inv.Count(acorn) != 0 || len(p.PendingEconomy()) != 0 {
				t.Fatalf("failed pick-up changed the player: count=%d events=%d", p.Inv.Count(acorn), len(p.PendingEconomy()))
			}
			if wantExists := tt.age < despawn; exists != wantExists {
				t.Fatalf("item exists = %v, want %v", exists, wantExists)
			}
		})
	}
}

func TestTickDespawnsFromRules(t *testing.T) {
	d := gameData(t)
	// Durations come from the rules data: change them and the system follows.
	d.Rules.DropReservationSeconds = 5
	d.Rules.DropDespawnSeconds = 20
	acorn := itemID(t, d, "acorn")
	clock := world.NewFakeClock(t0)
	s := grounditems.New(d, clock, tileSize)
	first := mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, t0)
	second := mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, t0.Add(10*time.Second))

	if err := s.PickUp(newPlayer(d, 2, world.Vec2{}), first); !errors.Is(err, grounditems.ErrReserved) {
		t.Fatalf("other at 0 s: err = %v, want ErrReserved", err)
	}
	clock.Advance(5 * time.Second)
	if err := s.PickUp(newPlayer(d, 2, world.Vec2{}), first); err != nil {
		t.Fatalf("other at 5 s: %v", err)
	}
	first = mustSpawn(t, s, acorn, 1, world.Vec2{}, 1, t0)

	steps := []struct {
		at         time.Duration
		wantFirst  bool
		wantSecond bool
	}{
		{19 * time.Second, true, true},
		{20 * time.Second, false, true},
		{29 * time.Second, false, true},
		{30 * time.Second, false, false},
	}
	for _, st := range steps {
		clock.Advance(t0.Add(st.at).Sub(clock.Now()))
		s.Tick(nil, 0)
		_, f := s.Get(first)
		_, sec := s.Get(second)
		if f != st.wantFirst || sec != st.wantSecond {
			t.Fatalf("at %v: first exists %v, second %v; want %v, %v", st.at, f, sec, st.wantFirst, st.wantSecond)
		}
	}
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
}

func TestPickUpRange(t *testing.T) {
	d := gameData(t)
	if d.Rules.PickupRangeTiles != 1 || !d.Rules.PickupRangeDiagonal {
		t.Fatalf("test assumes a 1 tile pick-up range with diagonals, rules say %d, %v", d.Rules.PickupRangeTiles, d.Rules.PickupRangeDiagonal)
	}
	acorn := itemID(t, d, "acorn")
	item := world.Vec2{X: 10.5, Y: 10.5} // tile (10, 10)
	tests := []struct {
		name string
		pos  world.Vec2
		ok   bool
	}{
		{"same tile", world.Vec2{X: 10.1, Y: 10.9}, true},
		{"adjacent east", world.Vec2{X: 11.9, Y: 10.5}, true},
		{"adjacent west", world.Vec2{X: 9.0, Y: 10.5}, true},
		{"diagonal", world.Vec2{X: 11.5, Y: 9.5}, true},
		{"two tiles east", world.Vec2{X: 12.0, Y: 10.5}, false},
		{"two tiles north", world.Vec2{X: 10.5, Y: 8.99}, false},
		{"knight move", world.Vec2{X: 11.5, Y: 12.5}, false},
		{"far away", world.Vec2{X: 100, Y: 100}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := grounditems.New(d, world.NewFakeClock(t0), tileSize)
			id := mustSpawn(t, s, acorn, 3, item, 1, t0)
			p := newPlayer(d, 1, tt.pos)
			err := s.PickUp(p, id)
			if tt.ok != (err == nil) {
				t.Fatalf("PickUp err = %v, want ok=%v", err, tt.ok)
			}
			if !tt.ok && !errors.Is(err, grounditems.ErrTooFar) {
				t.Fatalf("PickUp err = %v, want ErrTooFar", err)
			}
		})
	}
}

func TestPickUpMovesStack(t *testing.T) {
	d := gameData(t)
	acorn := itemID(t, d, "acorn")
	s := grounditems.New(d, world.NewFakeClock(t0), tileSize)
	id := mustSpawn(t, s, acorn, 5, world.Vec2{X: 1, Y: 1}, 1, t0)
	p := newPlayer(d, 1, world.Vec2{X: 1.5, Y: 1.5})
	if _, err := p.Inv.Add(d, acorn, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.PickUp(p, id); err != nil {
		t.Fatal(err)
	}
	if got := p.Inv.Count(acorn); got != 7 {
		t.Fatalf("acorns = %d, want 7", got)
	}
	if _, ok := s.Get(id); ok || s.Len() != 0 {
		t.Fatal("ground item still exists after pick-up")
	}
	events := p.DrainEconomy()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Reason != "pickup" {
		t.Fatalf("reason = %q, want pickup", ev.Reason)
	}
	if want := []world.Change{{Kind: world.ChangeAdd, Item: acorn, Qty: 5}}; !reflect.DeepEqual(ev.Changes, want) {
		t.Fatalf("changes = %+v, want %+v", ev.Changes, want)
	}
	if ev.Inv.Count(acorn) != 7 {
		t.Fatalf("event inventory has %d acorns, want 7", ev.Inv.Count(acorn))
	}
}

// TestPickUpFailuresChangeNothing: every refused pick-up leaves the player,
// their outbox and the ground untouched.
func TestPickUpFailuresChangeNothing(t *testing.T) {
	d := gameData(t)
	acorn := itemID(t, d, "acorn")
	boba := itemID(t, d, "boba")
	const owner, other world.PlayerID = 1, 2
	full := func(p *world.Player) {
		if _, err := p.Inv.Add(d, boba, d.Rules.InventorySlots); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name   string
		picker world.PlayerID
		pos    world.Vec2
		setup  func(p *world.Player)
		badID  bool
		want   error
	}{
		{"out of range", owner, world.Vec2{X: 5, Y: 0}, nil, false, grounditems.ErrTooFar},
		{"full inventory", owner, world.Vec2{}, full, false, grounditems.ErrNoInvSpace},
		{"locked inventory", owner, world.Vec2{}, func(p *world.Player) { p.Inv.SetLocked(true) }, false, grounditems.ErrNoInvSpace},
		{"reserved for someone else", other, world.Vec2{}, nil, false, grounditems.ErrReserved},
		{"unknown id", owner, world.Vec2{}, nil, true, grounditems.ErrNoSuchItem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := grounditems.New(d, world.NewFakeClock(t0), tileSize)
			id := mustSpawn(t, s, acorn, 3, world.Vec2{}, owner, t0)
			p := newPlayer(d, tt.picker, tt.pos)
			if tt.setup != nil {
				tt.setup(p)
			}
			before := p.Inv.Clone()
			target := id
			if tt.badID {
				target = id + 100
			}
			if err := s.PickUp(p, target); !errors.Is(err, tt.want) {
				t.Fatalf("PickUp err = %v, want %v", err, tt.want)
			}
			if !reflect.DeepEqual(p.Inv, before) {
				t.Fatalf("inventory changed: %+v, was %+v", p.Inv, before)
			}
			if n := len(p.PendingEconomy()); n != 0 {
				t.Fatalf("outbox events = %d, want 0", n)
			}
			if it, ok := s.Get(id); !ok || it.Qty != 3 || s.Len() != 1 {
				t.Fatalf("ground item = %+v, %v (len %d); want it untouched", it, ok, s.Len())
			}
		})
	}
}

// TestSameTickPickUpNoDuplication: two players pick up the same item in one
// tick through the world's command queue; exactly one gets it.
func TestSameTickPickUpNoDuplication(t *testing.T) {
	d := gameData(t)
	acorn := itemID(t, d, "acorn")
	for _, first := range []world.PlayerID{1, 2} {
		clock := world.NewFakeClock(t0)
		s := grounditems.New(d, clock, tileSize)
		w := world.New(world.DefaultConfig())
		if err := w.Register(s); err != nil {
			t.Fatal(err)
		}
		a, b := newPlayer(d, 1, world.Vec2{X: 0.5}), newPlayer(d, 2, world.Vec2{X: 1.5})
		for _, p := range []*world.Player{a, b} {
			if err := w.AddPlayer(p); err != nil {
				t.Fatal(err)
			}
		}
		id := mustSpawn(t, s, acorn, 4, world.Vec2{X: 1}, 1, t0)
		clock.Advance(d.Rules.DropReservation()) // either may take it
		second := 3 - first
		w.Enqueue(grounditems.PickUpCommand{Player: first, ID: id})
		w.Enqueue(grounditems.PickUpCommand{Player: second, ID: id})
		w.Tick()

		winner := w.Player(first)
		loser := w.Player(second)
		if winner.Inv.Count(acorn) != 4 || len(winner.PendingEconomy()) != 1 {
			t.Fatalf("first picker %d: %d acorns, %d events; want 4, 1", first, winner.Inv.Count(acorn), len(winner.PendingEconomy()))
		}
		if loser.Inv.Count(acorn) != 0 || len(loser.PendingEconomy()) != 0 {
			t.Fatalf("second picker %d: %d acorns, %d events; want 0, 0", second, loser.Inv.Count(acorn), len(loser.PendingEconomy()))
		}
		if s.Len() != 0 {
			t.Fatalf("ground items = %d, want 0", s.Len())
		}
	}
}

func BenchmarkNear(b *testing.B) {
	d := gameData(b)
	acorn := itemID(b, d, "acorn")
	s := grounditems.New(d, world.NewFakeClock(t0), tileSize)
	for i := range 1000 {
		mustSpawn(b, s, acorn, 1, world.Vec2{X: float32(i % 100), Y: float32(i / 100)}, 1, t0)
	}
	var buf []grounditems.Item
	b.ReportAllocs()
	for b.Loop() {
		buf = s.Near(buf[:0], world.Vec2{X: 50, Y: 5}, 16)
	}
}

func BenchmarkTick(b *testing.B) {
	d := gameData(b)
	acorn := itemID(b, d, "acorn")
	s := grounditems.New(d, world.NewFakeClock(t0), tileSize)
	for i := range 1000 {
		mustSpawn(b, s, acorn, 1, world.Vec2{X: float32(i)}, 1, t0)
	}
	b.ReportAllocs()
	for b.Loop() {
		s.Tick(nil, 0)
	}
}
