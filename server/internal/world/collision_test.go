package world_test

import (
	"encoding/binary"
	"testing"

	"github.com/cleiderg/lush/server/internal/world"
)

// MakeGrid builds collision.bin bytes from rows of '#' (blocked) and '.'.
func MakeGrid(rows []string, cellDm uint16, ox, oz int32) []byte {
	w, h := len(rows[0]), len(rows)
	b := make([]byte, 22, 22+(w*h+7)/8)
	copy(b, "LCOL")
	b[4] = 1
	binary.LittleEndian.PutUint16(b[6:], cellDm)
	binary.LittleEndian.PutUint16(b[8:], 64)
	binary.LittleEndian.PutUint16(b[10:], uint16(w))
	binary.LittleEndian.PutUint16(b[12:], uint16(h))
	binary.LittleEndian.PutUint32(b[14:], uint32(ox))
	binary.LittleEndian.PutUint32(b[18:], uint32(oz))
	b = append(b, make([]byte, (w*h+7)/8)...)
	for r, row := range rows {
		for c, ch := range row {
			if ch == '#' {
				i := r*w + c
				b[22+i>>3] |= 1 << (uint(i) & 7)
			}
		}
	}
	return b
}

func TestParseGrid(t *testing.T) {
	// 3x3 grid, 1 m cells, origin (-10 m, 20 m).
	data := MakeGrid([]string{"..#", ".#.", "#.."}, 10, -100, 200)
	g, err := world.ParseGrid(data)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := g.Size(); w != 3 || h != 3 {
		t.Fatalf("size %dx%d", w, h)
	}
	tests := []struct {
		name string
		p    world.Vec2
		want bool
	}{
		{"free NW cell", world.Vec2{X: -9.5, Y: 20.5}, false},
		{"blocked NE cell", world.Vec2{X: -7.5, Y: 20.5}, true},
		{"blocked centre", world.Vec2{X: -8.5, Y: 21.5}, true},
		{"blocked SW cell", world.Vec2{X: -9.5, Y: 22.5}, true},
		{"free SE cell", world.Vec2{X: -7.5, Y: 22.5}, false},
		{"west edge is inside", world.Vec2{X: -10, Y: 20}, false},
		{"outside west", world.Vec2{X: -10.01, Y: 20.5}, true},
		{"outside east", world.Vec2{X: -7, Y: 20.5}, true},
		{"outside north", world.Vec2{X: -9.5, Y: 19.9}, true},
		{"outside south", world.Vec2{X: -9.5, Y: 23}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.Blocked(tc.p); got != tc.want {
				t.Fatalf("Blocked(%+v) = %v, want %v", tc.p, got, tc.want)
			}
		})
	}
}

func TestParseGridErrors(t *testing.T) {
	good := MakeGrid([]string{"..", ".."}, 10, 0, 0)
	badMagic := append([]byte(nil), good...)
	badMagic[0] = 'X'
	badVer := append([]byte(nil), good...)
	badVer[4] = 2
	tests := []struct {
		name string
		data []byte
	}{
		{"short", good[:10]},
		{"bad magic", badMagic},
		{"bad version", badVer},
		{"truncated bits", good[:len(good)-1]},
		{"extra bits", append(append([]byte(nil), good...), 0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := world.ParseGrid(tc.data); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// AC3: the committed grid loads.
func TestLoadCommittedGrid(t *testing.T) {
	g, err := world.LoadGrid("../../../shared/map/collision.bin")
	if err != nil {
		t.Fatal(err)
	}
	if w, h := g.Size(); w < 64 || h < 64 {
		t.Fatalf("size %dx%d", w, h)
	}
	if !g.Blocked(world.Vec2{X: 1e9, Y: 0}) {
		t.Fatal("far outside should be blocked")
	}
}
