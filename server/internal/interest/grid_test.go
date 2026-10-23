package interest

import (
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/cleiderg/lush/server/internal/world"
)

func ids(h []Hit) []world.PlayerID {
	r := make([]world.PlayerID, len(h))
	for i := range h {
		r[i] = h[i].ID
	}
	sort.Slice(r, func(i, j int) bool { return r[i] < r[j] })
	return r
}

func TestCellSizeCoversRadii(t *testing.T) {
	// AC1: a 3x3 scan (span 1) must cover both radii.
	if CellSize < ViewRadius || CellSize < ChatRadius {
		t.Fatalf("cell size %v too small", CellSize)
	}
	for _, r := range []float32{ViewRadius, ChatRadius} {
		if span := math.Ceil(float64(r / CellSize)); span != 1 {
			t.Errorf("radius %v needs span %v, want 1", r, span)
		}
	}
}

func TestNearest100Of150(t *testing.T) {
	g := NewGrid()
	// Entity i sits at distance 0.1*i (1..150 -> 0.1..15), all within view.
	for i := 1; i <= 150; i++ {
		g.Set(world.PlayerID(i), world.Vec2{X: 0.1 * float32(i)})
	}
	g.Set(1000, world.Vec2{}) // the player
	got := g.Query(world.Vec2{}, ViewRadius, MaxVisible, 1000, nil)
	if len(got) != 100 {
		t.Fatalf("got %d hits", len(got))
	}
	for i, id := range ids(got) {
		if id != world.PlayerID(i+1) {
			t.Fatalf("hit %d = %d, want %d", i, id, i+1)
		}
	}
}

func TestQueryMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	g := NewGrid()
	pos := map[world.PlayerID]world.Vec2{}
	for i := 1; i <= 400; i++ {
		p := world.Vec2{X: rng.Float32()*80 - 40, Y: rng.Float32()*80 - 40}
		pos[world.PlayerID(i)] = p
		g.Set(world.PlayerID(i), p)
	}
	for _, max := range []int{5, 100, 1000} {
		c := world.Vec2{X: 3, Y: -7}
		var all []Hit
		for id, p := range pos {
			dx, dy := p.X-c.X, p.Y-c.Y
			if d2 := dx*dx + dy*dy; d2 <= ViewRadius*ViewRadius {
				all = append(all, Hit{id, d2})
			}
		}
		sort.Slice(all, func(i, j int) bool { return less(all[i], all[j]) })
		if len(all) > max {
			all = all[:max]
		}
		got := g.Query(c, ViewRadius, max, 0, nil)
		if len(got) != len(all) {
			t.Fatalf("max %d: got %d want %d", max, len(got), len(all))
		}
		want := ids(all)
		for i, id := range ids(got) {
			if id != want[i] {
				t.Fatalf("max %d: mismatch at %d", max, i)
			}
		}
	}
}

func TestRadiusBoundary(t *testing.T) {
	tests := []struct {
		name string
		pos  world.Vec2
		want bool
	}{
		{"exactly on radius", world.Vec2{X: 20}, true},
		{"just inside", world.Vec2{X: 19.99}, true},
		{"just outside", world.Vec2{X: 20.01}, false},
		{"on radius negative y", world.Vec2{Y: -20}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid()
			g.Set(2, tc.pos)
			got := g.Query(world.Vec2{}, ViewRadius, MaxVisible, 1, nil)
			if (len(got) == 1) != tc.want {
				t.Fatalf("hits=%v want visible=%v", got, tc.want)
			}
		})
	}
}

func TestCellBorders(t *testing.T) {
	// Players straddling the x=25 and origin cell borders see each other.
	tests := []struct {
		name string
		a, b world.Vec2
		want bool
	}{
		{"either side of +x border", world.Vec2{X: 24.9}, world.Vec2{X: 25.1}, true},
		{"on the border", world.Vec2{X: 25}, world.Vec2{X: 25, Y: 10}, true},
		{"across negative border", world.Vec2{X: -0.1, Y: -0.1}, world.Vec2{X: 0.1, Y: 0.1}, true},
		{"diagonal cells", world.Vec2{X: 24.9, Y: 24.9}, world.Vec2{X: 25.1, Y: 25.1}, true},
		{"two cells apart, out of range", world.Vec2{X: 10}, world.Vec2{X: 60}, false},
		{"adjacent cells, out of range", world.Vec2{X: 1}, world.Vec2{X: 49}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGrid()
			g.Set(1, tc.a)
			g.Set(2, tc.b)
			got := g.Query(tc.a, ViewRadius, MaxVisible, 1, nil)
			if (len(got) == 1) != tc.want {
				t.Fatalf("hits=%v want visible=%v", got, tc.want)
			}
		})
	}
}

func TestChatRadiusWithinThreeByThree(t *testing.T) {
	g := NewGrid()
	g.Set(2, world.Vec2{X: 25, Y: 0}) // exactly on chat radius
	if got := g.Query(world.Vec2{}, ChatRadius, -1, 1, nil); len(got) != 1 {
		t.Fatalf("chat radius hit missing: %v", got)
	}
}

func TestMovingBetweenCells(t *testing.T) {
	g := NewGrid()
	g.Set(1, world.Vec2{X: 5.5})
	g.Set(2, world.Vec2{X: 10})
	g.Set(3, world.Vec2{X: 12}) // shares cell with 1 and 2
	// Walk 1 across several cell borders; it must never be duplicated or lost.
	for x := float32(5.5); x < 120; x += 3 {
		g.Set(1, world.Vec2{X: x})
		if g.Len() != 3 {
			t.Fatalf("len %d", g.Len())
		}
		got := g.Query(world.Vec2{X: x}, 0.01, -1, 99, nil)
		if len(got) != 1 || got[0].ID != 1 {
			t.Fatalf("x=%v: %v", x, got)
		}
	}
	if got := g.Query(world.Vec2{X: 10}, ViewRadius, -1, 2, nil); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("old cell residents wrong: %v", got)
	}
	g.Remove(1)
	g.Remove(1)
	if g.Len() != 2 {
		t.Fatalf("len after remove %d", g.Len())
	}
}

func benchGrid(b *testing.B, n int, radius float32) {
	rng := rand.New(rand.NewSource(7))
	g := NewGrid()
	pos := make([]world.Vec2, n)
	for i := range pos {
		r := radius * float32(math.Sqrt(rng.Float64()))
		a := rng.Float64() * 2 * math.Pi
		pos[i] = world.Vec2{X: r * float32(math.Cos(a)), Y: r * float32(math.Sin(a))}
		g.Set(world.PlayerID(i+1), pos[i])
	}
	buf := make([]Hit, 0, 4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range pos {
			buf = g.Query(pos[j], ViewRadius, MaxVisible, world.PlayerID(j+1), buf[:0])
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/player")
}

func BenchmarkQuerySpread250(b *testing.B) { benchGrid(b, 1000, 250) }
func BenchmarkQueryPlaza25(b *testing.B)   { benchGrid(b, 1000, 25) }
