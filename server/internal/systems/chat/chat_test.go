package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

type rec struct{ got map[world.PlayerID][]Message }

func (r *rec) Deliver(to world.PlayerID, m Message) { r.got[to] = append(r.got[to], m) }

func setup(t *testing.T, xs ...float32) (*world.World, *world.FakeClock, *rec) {
	t.Helper()
	w := world.New(world.DefaultConfig())
	clk := world.NewFakeClock(time.Unix(1000, 0))
	r := &rec{got: map[world.PlayerID][]Message{}}
	if err := w.Register(New(DefaultConfig(), clk, r)); err != nil {
		t.Fatal(err)
	}
	for i, x := range xs {
		if err := w.AddPlayer(&world.Player{ID: world.PlayerID(i + 1), Pos: world.Vec2{X: x}}); err != nil {
			t.Fatal(err)
		}
	}
	return w, clk, r
}

func TestRadius(t *testing.T) {
	w, _, r := setup(t, 0, 24, 25, 26, 0)
	w.Enqueue(SendCommand{Player: 1, Text: "hi"})
	w.Tick()
	want := map[world.PlayerID]int{1: 1, 2: 1, 3: 1, 4: 0, 5: 1}
	for id, n := range want {
		if len(r.got[id]) != n {
			t.Errorf("player %d got %d messages, want %d", id, len(r.got[id]), n)
		}
	}
	if m := r.got[2][0]; m.From != 1 || m.Text != "hi" {
		t.Errorf("bad message %+v", m)
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name, in, want string
		ok             bool
	}{
		{"plain", "hello", "hello", true},
		{"exactly 120", strings.Repeat("a", 120), strings.Repeat("a", 120), true},
		{"121 rejected", strings.Repeat("a", 121), "", false},
		{"multibyte over", strings.Repeat("é", 61), "", false},
		{"controls stripped", "a\x00b\x07c\nd\te\x7ff\u0085g", "abcdefg", true},
		{"only controls", "\x01\x02\n", "", false},
		{"empty", "", "", false},
		{"invalid utf8", "a\xffb", "", false},
		{"stripped fits", "a" + strings.Repeat("\x01", 119), "a", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Sanitize(tt.in, 120)
			if ok != tt.ok || got != tt.want {
				t.Errorf("Sanitize = %q,%v want %q,%v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestRejectedNotDelivered(t *testing.T) {
	w, _, r := setup(t, 0, 1)
	w.Enqueue(SendCommand{Player: 1, Text: strings.Repeat("x", 121)})
	w.Tick()
	if len(r.got) != 0 {
		t.Fatalf("delivered %v", r.got)
	}
}

func TestRateLimit(t *testing.T) {
	w, clk, r := setup(t, 0, 1)
	send := func(txt string, p world.PlayerID) { w.Enqueue(SendCommand{Player: p, Text: txt}); w.Tick() }
	send("one", 1)
	clk.Advance(500 * time.Millisecond)
	send("dropped", 1)
	send("other player ok", 2)
	clk.Advance(499 * time.Millisecond)
	send("still dropped", 1)
	clk.Advance(time.Millisecond)
	send("two", 1)
	var texts []string
	for _, m := range r.got[2] {
		texts = append(texts, m.Text)
	}
	if got := strings.Join(texts, "|"); got != "one|two" {
		t.Errorf("player 2 heard %q, want one|two", got)
	}
	if n := len(r.got[1]); n != 3 { // one, other player ok, two
		t.Errorf("player 1 heard %d, want 3", n)
	}
}

func BenchmarkSend100Players(b *testing.B) {
	w := world.New(world.DefaultConfig())
	clk := world.NewFakeClock(time.Unix(0, 0))
	_ = w.Register(New(DefaultConfig(), clk, discard{}))
	for i := 1; i <= 100; i++ {
		_ = w.AddPlayer(&world.Player{ID: world.PlayerID(i), Pos: world.Vec2{X: float32(i % 30)}})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		clk.Advance(time.Second)
		w.Enqueue(SendCommand{Player: 1, Text: "hello"})
		w.Tick()
	}
}

type discard struct{}

func (discard) Deliver(world.PlayerID, Message) {}
