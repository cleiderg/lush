package session

import (
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/world"
)

// BenchmarkCollectSaves measures the per-tick snapshot step with 1,000 players.
func BenchmarkCollectSaves(b *testing.B) {
	w := world.New(world.DefaultConfig())
	s := New(Config{}, w, world.NewFakeClock(time.Unix(0, 0)), store.NewMemory(), nil)
	defer s.Close()
	for i := 1; i <= 1000; i++ {
		id := world.PlayerID(i)
		if err := w.AddPlayer(&world.Player{ID: id, Pos: world.Vec2{X: float32(i), Y: 1}}); err != nil {
			b.Fatal(err)
		}
		s.live[id] = &guest{id: id, storeID: int64(i), ver: 1}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := len(s.collectSaves()); got != 1000 {
			b.Fatal(got)
		}
	}
}
