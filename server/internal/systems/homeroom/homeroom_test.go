package homeroom_test

import (
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/homeroom"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/housing"
)

const home1 homeroom.HomeID = 1

var door = world.Vec2{X: 100, Y: 100}

type res struct {
	to world.PlayerID
	r  homeroom.Result
}

type rec struct {
	results []res
	states  map[world.PlayerID][]homeroom.State
	left    []world.PlayerID
}

func (s *rec) HomeResult(to world.PlayerID, r homeroom.Result) { s.results = append(s.results, res{to, r}) }
func (s *rec) HomeState(to world.PlayerID, st homeroom.State)  { s.states[to] = append(s.states[to], st) }
func (s *rec) HomeLeft(to world.PlayerID, _ homeroom.HomeID)   { s.left = append(s.left, to) }
func (s *rec) last() homeroom.Result                           { return s.results[len(s.results)-1].r }

type fixture struct {
	t    *testing.T
	d    *gamedata.Data
	w    *world.World
	sys  *homeroom.System
	sink *rec
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, d: d, sink: &rec{states: map[world.PlayerID][]homeroom.State{}}}
	f.w = world.New(world.DefaultConfig())
	f.sys = homeroom.New(d, homeroom.Config{Doors: map[homeroom.HomeID]world.Vec2{home1: door}, Reach: 3}, f.sink)
	if err := f.w.Register(f.sys); err != nil {
		t.Fatal(err)
	}
	if err := f.sys.AddHome(homeroom.Spec{ID: home1, Owner: 1}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) item(key string) gamedata.ItemID {
	it, ok := f.d.ItemByKey(key)
	if !ok {
		f.t.Fatalf("no item %q", key)
	}
	return it.ID
}

// player adds a player at the door with an empty inventory plus items.
func (f *fixture) player(id world.PlayerID, keys ...string) *world.Player {
	inv := world.NewInventory(f.d)
	for _, k := range keys {
		if _, err := inv.Add(f.d, f.item(k), 1); err != nil {
			f.t.Fatal(err)
		}
	}
	p := &world.Player{ID: id, Pos: door, Inv: inv}
	if err := f.w.AddPlayer(p); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) do(c world.Command) homeroom.Result {
	f.t.Helper()
	f.sink.results = nil
	f.w.Enqueue(c)
	f.w.Tick()
	if len(f.sink.results) != 1 {
		f.t.Fatalf("got %d results, want 1", len(f.sink.results))
	}
	return f.sink.last()
}

func (f *fixture) want(c world.Command, want homeroom.Result) {
	f.t.Helper()
	if got := f.do(c); got != want {
		f.t.Fatalf("%T = %v, want %v", c, got, want)
	}
}

func count(p *world.Player, id gamedata.ItemID) int { return p.Inv.Count(id) }

// TestEnterLeave covers AC1.
func TestEnterLeave(t *testing.T) {
	f := newFixture(t)
	owner := f.player(1)
	f.want(homeroom.EnterCommand{Player: 1, Home: 99}, homeroom.NoHome)

	far := f.player(2)
	far.Pos = world.Vec2{X: 110, Y: 100}
	f.want(homeroom.EnterCommand{Player: 2, Home: home1}, homeroom.TooFar)

	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.Ok)
	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.AlreadyInside)

	// Only the owner switches open/locked.
	f.player(3)
	f.want(homeroom.SetLockedCommand{Player: 3, Home: home1, Locked: true}, homeroom.NotOwner)
	f.want(homeroom.SetLockedCommand{Player: 1, Home: home1, Locked: true}, homeroom.Ok)
	f.want(homeroom.EnterCommand{Player: 3, Home: home1}, homeroom.Locked)
	f.want(homeroom.SetLockedCommand{Player: 1, Home: home1, Locked: false}, homeroom.Ok)
	f.want(homeroom.EnterCommand{Player: 3, Home: home1}, homeroom.Ok)

	// Leaving returns the player to the door.
	owner.Pos = world.Vec2{X: 5, Y: 5}
	f.want(homeroom.LeaveCommand{Player: 1}, homeroom.Ok)
	if owner.Pos != door {
		t.Errorf("pos after leave = %v, want %v", owner.Pos, door)
	}
	f.want(homeroom.LeaveCommand{Player: 1}, homeroom.NotInside)
}

func TestEnterLockedOwnerAndFull(t *testing.T) {
	f := newFixture(t)
	f.player(1)
	f.want(homeroom.SetLockedCommand{Player: 1, Home: home1, Locked: true}, homeroom.Ok)
	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.Ok) // owner may enter a locked home
	f.want(homeroom.SetLockedCommand{Player: 1, Home: home1, Locked: false}, homeroom.Ok)
	for id := world.PlayerID(2); id <= homeroom.MaxVisitors; id++ {
		f.player(id)
		f.want(homeroom.EnterCommand{Player: id, Home: home1}, homeroom.Ok)
	}
	f.player(50)
	f.want(homeroom.EnterCommand{Player: 50, Home: home1}, homeroom.Full)
	f.want(homeroom.LeaveCommand{Player: 4}, homeroom.Ok)
	f.want(homeroom.EnterCommand{Player: 50, Home: home1}, homeroom.Ok)
}

func TestLeftWorldFreesSpot(t *testing.T) {
	f := newFixture(t)
	f.player(1)
	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.Ok)
	f.w.RemovePlayer(1)
	f.w.Tick()
	if got := f.sys.Inside(home1); len(got) != 0 {
		t.Errorf("inside = %v", got)
	}
}

// TestVisitorsCannotEdit covers AC2's NotOwner half.
func TestVisitorsCannotEdit(t *testing.T) {
	f := newFixture(t)
	f.player(1)
	v := f.player(2, "stool", "white_wallpaper", "dark_boards_floor")
	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.Ok)
	f.want(homeroom.EnterCommand{Player: 2, Home: home1}, homeroom.Ok)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 0}, homeroom.BadItem) // owner has nothing
	cmds := []world.Command{
		homeroom.PlaceCommand{Player: 2, Slot: 0},
		homeroom.MoveCommand{Player: 2, Placed: 1},
		homeroom.RotateCommand{Player: 2, Placed: 1, Rot: 90},
		homeroom.RemoveCommand{Player: 2, Placed: 1},
		homeroom.SetWallpaperCommand{Player: 2, Slot: 1},
		homeroom.SetFloorCommand{Player: 2, Slot: 2},
	}
	for _, c := range cmds {
		f.want(c, homeroom.NotOwner)
	}
	if count(v, f.item("stool")) != 1 || count(v, f.item("white_wallpaper")) != 1 {
		t.Error("refused edit changed the visitor's inventory")
	}
	// Not inside at all.
	f.player(3, "stool")
	f.want(homeroom.PlaceCommand{Player: 3, Slot: 0}, homeroom.NotInside)
}

// TestGridResultsMapped covers AC2's room-grid half: each housing.Result has
// its own distinct code, and real refusals come through.
func TestGridResultsMapped(t *testing.T) {
	seen := map[homeroom.Result]housing.Result{}
	for r := housing.OK; r <= housing.Exists; r++ {
		got := homeroom.FromGrid(r)
		if prev, dup := seen[got]; dup {
			t.Errorf("%v and %v both map to %v", prev, r, got)
		}
		seen[got] = r
		if (r == housing.OK) != (got == homeroom.Ok) {
			t.Errorf("%v -> %v", r, got)
		}
	}

	f := newFixture(t)
	o := f.player(1, "stool", "stool", "wall_clock", "cactus", "wooden_table", "rug_small")
	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.Ok)
	at := func(x, y int) housing.Placement { return housing.Placement{X: x, Y: y} }
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 0, At: at(0, 0)}, homeroom.Ok)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 1, At: at(0, 0)}, homeroom.Overlap)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 1, At: at(8, 0)}, homeroom.OutOfBounds)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 1, At: housing.Placement{X: 1, Rot: 45}}, homeroom.BadRotation)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 3, At: housing.Placement{Parent: 1, Slot: -1}}, homeroom.NoSurface)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 1, At: housing.Placement{Parent: 1}}, homeroom.NotSurfaceItem)
	f.want(homeroom.RemoveCommand{Player: 1, Placed: 77}, homeroom.NotFound)
	f.want(homeroom.MoveCommand{Player: 1, Placed: 77}, homeroom.NotFound)
	f.want(homeroom.RotateCommand{Player: 1, Placed: 77}, homeroom.NotFound)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 4, At: at(3, 3)}, homeroom.Ok) // table = id 2
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 3, At: housing.Placement{Parent: 2, Slot: -1}}, homeroom.Ok)
	f.want(homeroom.RemoveCommand{Player: 1, Placed: 2}, homeroom.HasItems)
	f.want(homeroom.MoveCommand{Player: 1, Placed: 2, To: at(5, 5)}, homeroom.HasItems)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 2, At: housing.Placement{Side: housing.North}}, homeroom.Ok)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 5, At: at(7, 7)}, homeroom.OutOfBounds)
	if o.Inv.Count(f.item("stool")) != 1 {
		t.Error("refused place changed the inventory")
	}
}

func totals(f *fixture, p *world.Player) map[gamedata.ItemID]int {
	m := map[gamedata.ItemID]int{}
	for _, s := range p.Inv.Slots {
		if !s.Empty() {
			m[s.Item] += s.Qty
		}
	}
	st, _ := f.sys.State(home1)
	for _, it := range st.Items {
		m[it.Item]++
	}
	for _, c := range []gamedata.ItemID{st.Wallpaper, st.Floor} {
		if c != 0 {
			m[c]++
		}
	}
	return m
}

func sameTotals(t *testing.T, a, b map[gamedata.ItemID]int) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("totals %v != %v", a, b)
	}
	for k, v := range a {
		if b[k] != v {
			t.Fatalf("totals %v != %v", a, b)
		}
	}
}

// TestInventoryMoves covers AC3 and AC4.
func TestInventoryMoves(t *testing.T) {
	f := newFixture(t)
	o := f.player(1, "stool", "white_wallpaper", "cream_wallpaper", "dark_boards_floor")
	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.Ok)
	before := totals(f, o)
	stool, w1, w2, fl := f.item("stool"), f.item("white_wallpaper"), f.item("cream_wallpaper"), f.item("dark_boards_floor")

	f.want(homeroom.PlaceCommand{Player: 1, Slot: 0, At: housing.Placement{X: 2, Y: 2}}, homeroom.Ok)
	if !o.Inv.Slots[0].Empty() || count(o, stool) != 0 {
		t.Error("place left the stool in the inventory")
	}
	f.want(homeroom.SetWallpaperCommand{Player: 1, Slot: 1}, homeroom.Ok)
	f.want(homeroom.SetFloorCommand{Player: 1, Slot: 3}, homeroom.Ok)
	// Swapping returns the old covering.
	f.want(homeroom.SetWallpaperCommand{Player: 1, Slot: 2}, homeroom.Ok)
	if count(o, w1) != 1 || count(o, w2) != 0 {
		t.Errorf("after swap: white %d cream %d", count(o, w1), count(o, w2))
	}
	// A floor item is not a wallpaper.
	f.want(homeroom.SetWallpaperCommand{Player: 1, Slot: 3}, homeroom.BadItem)
	sameTotals(t, before, totals(f, o))

	// Fill the inventory, then remove must fail and change nothing.
	for o.Inv.FreeSlots() > 0 {
		if _, err := o.Inv.Add(f.d, f.item("coffee_mug"), 1); err != nil {
			t.Fatal(err)
		}
	}
	events := len(o.PendingEconomy())
	full := totals(f, o)
	f.want(homeroom.RemoveCommand{Player: 1, Placed: 1}, homeroom.InventoryFull)
	sameTotals(t, full, totals(f, o))
	if len(o.PendingEconomy()) != events {
		t.Error("refused remove wrote to the outbox")
	}
	// A swap still works with a full inventory.
	f.want(homeroom.SetFloorCommand{Player: 1, Slot: 3}, homeroom.BadItem) // slot 3 holds a mug now? floor was taken
	o.Inv.Slots[5] = world.Stack{}
	f.want(homeroom.RemoveCommand{Player: 1, Placed: 1}, homeroom.Ok)
	if count(o, stool) != 1 {
		t.Error("remove did not return the stool")
	}

	var reasons []string
	for _, e := range o.PendingEconomy() {
		reasons = append(reasons, e.Reason)
	}
	want := []string{homeroom.ReasonPlace, homeroom.ReasonWallpaper, homeroom.ReasonFloor, homeroom.ReasonWallpaper, homeroom.ReasonRemove}
	if len(reasons) != len(want) {
		t.Fatalf("outbox reasons = %v, want %v", reasons, want)
	}
	for i := range want {
		if reasons[i] != want[i] {
			t.Fatalf("outbox reasons = %v, want %v", reasons, want)
		}
	}
	_ = fl
}

// TestBroadcast covers AC5.
func TestBroadcast(t *testing.T) {
	f := newFixture(t)
	f.player(1, "stool")
	f.player(2)
	f.player(3)
	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.Ok)
	f.want(homeroom.EnterCommand{Player: 2, Home: home1}, homeroom.Ok)
	n1, n2, n3 := len(f.sink.states[1]), len(f.sink.states[2]), len(f.sink.states[3])
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 0, At: housing.Placement{X: 1, Y: 1}}, homeroom.Ok)
	if len(f.sink.states[1]) != n1+1 || len(f.sink.states[2]) != n2+1 || len(f.sink.states[3]) != n3 {
		t.Fatalf("states sent: %d %d %d", len(f.sink.states[1])-n1, len(f.sink.states[2])-n2, len(f.sink.states[3])-n3)
	}
	st := f.sink.states[2][len(f.sink.states[2])-1]
	if len(st.Items) != 1 || st.Items[0].X != 1 || st.Items[0].Item != f.item("stool") {
		t.Errorf("state = %+v", st)
	}
	// A refused edit sends nothing.
	f.want(homeroom.PlaceCommand{Player: 2, Slot: 0}, homeroom.NotOwner)
	if len(f.sink.states[2]) != n2+1 {
		t.Error("refused edit sent state")
	}
	// After leaving, no more updates.
	f.want(homeroom.LeaveCommand{Player: 2}, homeroom.Ok)
	f.want(homeroom.RotateCommand{Player: 1, Placed: 1, Rot: 90}, homeroom.Ok)
	if len(f.sink.states[2]) != n2+1 {
		t.Error("player outside got state")
	}
}

// TestRelease covers AC6.
func TestRelease(t *testing.T) {
	f := newFixture(t)
	o := f.player(1, "wooden_table", "cactus", "white_wallpaper", "dark_boards_floor")
	v := f.player(2)
	f.want(homeroom.EnterCommand{Player: 1, Home: home1}, homeroom.Ok)
	f.want(homeroom.EnterCommand{Player: 2, Home: home1}, homeroom.Ok)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 0, At: housing.Placement{X: 1, Y: 1}}, homeroom.Ok)
	f.want(homeroom.PlaceCommand{Player: 1, Slot: 1, At: housing.Placement{Parent: 1, Slot: -1}}, homeroom.Ok)
	f.want(homeroom.SetWallpaperCommand{Player: 1, Slot: 2}, homeroom.Ok)
	f.want(homeroom.SetFloorCommand{Player: 1, Slot: 3}, homeroom.Ok)
	o.Pos, v.Pos = world.Vec2{X: 1, Y: 1}, world.Vec2{X: 2, Y: 2}

	got, err := f.sys.Release(f.w, home1)
	if err != nil {
		t.Fatal(err)
	}
	want := map[gamedata.ItemID]int{f.item("wooden_table"): 1, f.item("cactus"): 1, f.item("white_wallpaper"): 1, f.item("dark_boards_floor"): 1}
	m := map[gamedata.ItemID]int{}
	for _, r := range got {
		m[r.Item] += r.Qty
	}
	sameTotals(t, want, m)
	if o.Pos != door || v.Pos != door {
		t.Errorf("positions %v %v, want door", o.Pos, v.Pos)
	}
	if len(f.sys.Inside(home1)) != 0 {
		t.Error("players still inside")
	}
	if len(f.sink.left) != 2 {
		t.Errorf("left notices = %v", f.sink.left)
	}
	st, _ := f.sys.State(home1)
	if len(st.Items) != 0 || st.Wallpaper != 0 || st.Floor != 0 {
		t.Errorf("room not empty: %+v", st)
	}
	f.want(homeroom.EnterCommand{Player: 2, Home: home1}, homeroom.NoHome)
	if _, err := f.sys.Release(f.w, home1); err == nil {
		t.Error("second release should fail")
	}
	if err := f.sys.AddHome(homeroom.Spec{ID: home1, Owner: 2}); err != nil {
		t.Errorf("re-adding released home: %v", err)
	}
}

func BenchmarkTick(b *testing.B) {
	f := newFixture(&testing.T{})
	for id := world.PlayerID(1); id <= homeroom.MaxVisitors; id++ {
		f.player(id)
		f.w.Enqueue(homeroom.EnterCommand{Player: id, Home: home1})
	}
	f.w.Tick()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.sys.Tick(f.w, 0)
	}
}
