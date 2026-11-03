package housing

import (
	"reflect"
	"testing"
)

type def struct {
	w, d   int
	layers Layer
	slots  int
}

func (f def) Footprint() (int, int) { return f.w, f.d }
func (f def) Layers() Layer         { return f.layers }
func (f def) SurfaceSlots() int     { return f.slots }

var (
	chair  = def{1, 1, LayerFloor, 0}
	sofa   = def{2, 1, LayerFloor, 0}
	table  = def{2, 1, LayerFloor, 2}
	rug    = def{2, 2, LayerRug, 0}
	lamp   = def{1, 1, LayerSurface, 0}
	plant  = def{1, 1, LayerFloor | LayerSurface, 0}
	window = def{2, 1, LayerWall, 0}
	clock  = def{1, 1, LayerWall, 0}
)

func at(x, y int, rot Rotation) Placement { return Placement{X: x, Y: y, Rot: rot} }

func TestFloorPlacement(t *testing.T) {
	r := New(DefaultSize)
	if res := r.Place(1, sofa, at(0, 0, 0)); res != OK {
		t.Fatal(res)
	}
	tests := []struct {
		name string
		def  ItemDef
		p    Placement
		want Result
	}{
		{"adjacent", chair, at(2, 0, 0), OK},
		{"overlap", chair, at(1, 0, 0), Overlap},
		{"edge fits", sofa, at(6, 7, 0), OK},
		{"right out", sofa, at(7, 3, 0), OutOfBounds},
		{"negative", chair, at(-1, 0, 0), OutOfBounds},
		{"rot 90 swaps", sofa, at(0, 7, 90), OutOfBounds},
		{"rot 90 fits", sofa, at(5, 5, 90), OK},
		{"rot 270 out", sofa, at(7, 7, 270), OutOfBounds},
		{"rot 180 keeps", sofa, at(6, 4, 180), OK},
		{"bad rotation", chair, at(4, 4, 45), BadRotation},
		{"surface-only on floor", lamp, at(4, 4, 0), BadLayer},
	}
	id := ID(10)
	for _, tc := range tests {
		id++
		if got := r.Place(id, tc.def, tc.p); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	if res := r.Place(1, chair, at(3, 3, 0)); res != Exists {
		t.Errorf("duplicate id: %v", res)
	}
	if w, d := Size(sofa, 90); w != 1 || d != 2 {
		t.Errorf("90: %d×%d", w, d)
	}
	if w, d := Size(sofa, 270); w != 1 || d != 2 {
		t.Errorf("270: %d×%d", w, d)
	}
	if w, d := Size(sofa, 180); w != 2 || d != 1 {
		t.Errorf("180: %d×%d", w, d)
	}
}

func TestRugs(t *testing.T) {
	r := New(DefaultSize)
	for _, s := range []struct {
		id   ID
		def  ItemDef
		p    Placement
		want Result
	}{
		{1, rug, at(0, 0, 0), OK},
		{2, chair, at(0, 0, 0), OK},    // floor item on a rug
		{3, sofa, at(0, 1, 0), OK},     // another across rug tiles
		{4, rug, at(1, 1, 0), Overlap}, // rug on rug
		{5, rug, at(2, 0, 0), OK},
		{6, rug, at(7, 7, 0), OutOfBounds},
	} {
		if got := r.Place(s.id, s.def, s.p); got != s.want {
			t.Errorf("id %d: got %v want %v", s.id, got, s.want)
		}
	}
	// Moving a floor item over a rug works; a floor item still can't overlap floor items.
	if res := r.Move(2, at(1, 0, 0)); res != OK {
		t.Errorf("move on rug: %v", res)
	}
	if res := r.Move(2, at(0, 1, 0)); res != Overlap {
		t.Errorf("move onto sofa: %v", res)
	}
}

func TestSurface(t *testing.T) {
	r := New(DefaultSize)
	r.Place(1, table, at(0, 0, 0))
	r.Place(2, chair, at(5, 5, 0))
	on := func(parent ID, slot int) Placement { return Placement{Parent: parent, Slot: slot} }
	for _, s := range []struct {
		name string
		id   ID
		def  ItemDef
		p    Placement
		want Result
	}{
		{"first free", 10, lamp, on(1, -1), OK},
		{"taken slot", 11, lamp, on(1, 0), SurfaceFull},
		{"explicit slot", 12, plant, on(1, 1), OK},
		{"full", 13, lamp, on(1, -1), SurfaceFull},
		{"bad slot", 14, lamp, on(1, 2), OutOfBounds},
		{"not surface item", 15, chair, on(1, -1), NotSurfaceItem},
		{"parent has no slots", 16, lamp, on(2, -1), NoSurface},
		{"missing parent", 17, lamp, on(99, -1), NoSurface},
		{"onto small item", 18, lamp, on(10, -1), NoSurface},
	} {
		if got := r.Place(s.id, s.def, s.p); got != s.want {
			t.Errorf("%s: got %v want %v", s.name, got, s.want)
		}
	}
	if it, _ := r.Get(10); it.Slot != 0 {
		t.Errorf("slot = %d", it.Slot)
	}
	// Holding items blocks remove and move, rotate is fine.
	if res := r.Remove(1); res != HasItems {
		t.Errorf("remove: %v", res)
	}
	if res := r.Move(1, at(3, 3, 0)); res != HasItems {
		t.Errorf("move: %v", res)
	}
	if res := r.Rotate(1, 90); res != OK {
		t.Errorf("rotate: %v", res)
	}
	if res := r.Remove(10); res != OK {
		t.Fatal(res)
	}
	if res := r.Place(13, lamp, on(1, -1)); res != OK {
		t.Errorf("free slot reused: %v", res)
	}
	// A surface item can move to another surface or to the floor (plant).
	if res := r.Move(12, at(4, 4, 0)); res != OK {
		t.Errorf("plant to floor: %v", res)
	}
	r.Remove(13)
	if res := r.Remove(1); res != OK {
		t.Errorf("remove empty table: %v", res)
	}
}

func TestWall(t *testing.T) {
	r := New(DefaultSize)
	w := func(side Side, pos int) Placement { return Placement{Side: side, Pos: pos} }
	for _, s := range []struct {
		name string
		id   ID
		def  ItemDef
		p    Placement
		want Result
	}{
		{"window", 1, window, w(North, 0), OK},
		{"adjacent clock", 2, clock, w(North, 2), OK},
		{"overlap window", 3, clock, w(North, 1), Overlap},
		{"overlap clock", 4, window, w(North, 1), Overlap},
		{"other wall same pos", 5, window, w(West, 0), OK},
		{"end of wall", 6, window, w(South, 6), OK},
		{"past end", 7, window, w(South, 7), OutOfBounds},
		{"bad side", 8, clock, w(Side(9), 0), OutOfBounds},
	} {
		if got := r.Place(s.id, s.def, s.p); got != s.want {
			t.Errorf("%s: got %v want %v", s.name, got, s.want)
		}
	}
	if res := r.Rotate(1, 90); res != BadLayer {
		t.Errorf("rotate wall: %v", res)
	}
	if res := r.Move(1, w(North, 3)); res != OK {
		t.Errorf("slide window: %v", res)
	}
	if res := r.Move(1, w(North, 1)); res != Overlap {
		t.Errorf("move into clock: %v", res)
	}
	// Wall items don't block the floor.
	if res := r.Place(20, chair, at(0, 0, 0)); res != OK {
		t.Errorf("floor under wall item: %v", res)
	}
}

func TestFailedChangeLeavesRoomUnchanged(t *testing.T) {
	r := New(DefaultSize)
	r.Place(1, sofa, at(0, 0, 0))
	r.Place(2, chair, at(0, 1, 0))
	r.Place(3, table, at(4, 4, 0))
	r.Place(4, lamp, Placement{Parent: 3, Slot: -1})
	r.Place(5, window, Placement{Side: East, Pos: 2})
	before := r.Items()

	fails := []struct {
		name string
		res  Result
	}{
		{"move overlap", r.Move(1, at(0, 1, 0))},
		{"move out", r.Move(1, at(7, 7, 0))},
		{"rotate overlap", r.Rotate(1, 90)},
		{"rotate bad", r.Rotate(2, 33)},
		{"rotate out", r.Rotate(3, 360)},
		{"move held table", r.Move(3, at(0, 5, 0))},
		{"remove held table", r.Remove(3)},
		{"move lamp full", r.Move(4, Placement{Parent: 2, Slot: -1})},
		{"move wall out", r.Move(5, Placement{Side: East, Pos: 7})},
		{"remove missing", r.Remove(99)},
	}
	for _, f := range fails {
		if f.res == OK {
			t.Errorf("%s: unexpectedly OK", f.name)
		}
	}
	if after := r.Items(); !reflect.DeepEqual(before, after) {
		t.Errorf("room changed:\nbefore %+v\nafter  %+v", before, after)
	}
	if res := r.Rotate(3, 90); res != OK {
		t.Errorf("rotate table: %v", res)
	}
}

func BenchmarkPlace(b *testing.B) {
	r := New(DefaultSize)
	for i := 0; i < 20; i++ {
		r.Place(ID(i+1), chair, at(i%8, i/8*2, 0))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Move(1, at(i%2, 7, 0))
	}
}
