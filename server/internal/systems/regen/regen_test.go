package regen_test

import (
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/regen"
	"github.com/cleiderg/lush/server/internal/world"
)

var t0 = time.Unix(1_000_000, 0)

func rules(t testing.TB) gamedata.Rules {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d.Rules
}

type harness struct {
	w     *world.World
	clock *world.FakeClock
}

func newHarness(t testing.TB, r gamedata.Rules, players ...*world.Player) *harness {
	t.Helper()
	clock := world.NewFakeClock(t0)
	sys, err := regen.New(r, clock)
	if err != nil {
		t.Fatal(err)
	}
	w := world.New(world.DefaultConfig())
	if err := w.Register(sys); err != nil {
		t.Fatal(err)
	}
	for _, p := range players {
		if err := w.AddPlayer(p); err != nil {
			t.Fatal(err)
		}
	}
	h := &harness{w: w, clock: clock}
	h.w.Tick() // first sight at t0
	return h
}

// runTo ticks the world every tick interval until the clock reaches at.
func (h *harness) runTo(at time.Duration) {
	for h.clock.Now().Before(t0.Add(at)) {
		h.clock.Advance(h.w.TickInterval())
		h.w.Tick()
	}
}

func TestRegenSchedule(t *testing.T) {
	r := rules(t)
	if r.RegenDelaySeconds != 10 || r.RegenIntervalSeconds != 6 || r.RegenHP != 1 {
		t.Fatalf("test assumes the spec rules (10 s delay, 6 s interval, 1 HP), got %+v", r)
	}
	p := &world.Player{ID: 1, HP: 5, LastCombat: t0} // max HP 10 at level 1
	h := newHarness(t, r, p)

	steps := []struct {
		at   time.Duration
		want int
	}{
		{9950 * time.Millisecond, 5}, // still inside the 10 s delay
		{10 * time.Second, 6},        // first heal at the delay
		{15950 * time.Millisecond, 6},
		{16 * time.Second, 7}, // then every 6 s
		{22 * time.Second, 8},
		{28 * time.Second, 9},
		{34 * time.Second, 10},
		{40 * time.Second, 10}, // capped at max HP
		{2 * time.Minute, 10},
	}
	for _, s := range steps {
		h.runTo(s.at)
		if p.HP != s.want {
			t.Fatalf("HP at %v = %d, want %d", s.at, p.HP, s.want)
		}
	}
}

func TestRegenRestartsAfterCombat(t *testing.T) {
	r := rules(t)
	p := &world.Player{ID: 1, HP: 3, LastCombat: t0}
	h := newHarness(t, r, p)

	h.runTo(10 * time.Second)
	if p.HP != 4 {
		t.Fatalf("HP at 10 s = %d, want 4", p.HP)
	}
	// Damage at 12 s pushes the next heal to 22 s.
	h.runTo(12 * time.Second)
	p.HP--
	p.MarkCombat(h.clock.Now())
	h.runTo(21950 * time.Millisecond)
	if p.HP != 3 {
		t.Fatalf("HP at 21.95 s = %d, want 3 (delay restarted)", p.HP)
	}
	h.runTo(22 * time.Second)
	if p.HP != 4 {
		t.Fatalf("HP at 22 s = %d, want 4", p.HP)
	}
	h.runTo(28 * time.Second)
	if p.HP != 5 {
		t.Fatalf("HP at 28 s = %d, want 5", p.HP)
	}
}

func TestRegenUsesRules(t *testing.T) {
	r := rules(t)
	r.RegenHP, r.RegenIntervalSeconds, r.RegenDelaySeconds = 3, 2, 1
	r.BaseHP = 20 // max HP 21
	p := &world.Player{ID: 1, HP: 10, LastCombat: t0}
	h := newHarness(t, r, p)
	for _, s := range []struct {
		at   time.Duration
		want int
	}{
		{900 * time.Millisecond, 10},
		{1 * time.Second, 13},
		{3 * time.Second, 16},
		{5 * time.Second, 19},
		{7 * time.Second, 21}, // 22 capped to 21
		{20 * time.Second, 21},
	} {
		h.runTo(s.at)
		if p.HP != s.want {
			t.Fatalf("HP at %v = %d, want %d", s.at, p.HP, s.want)
		}
	}
}

func TestRegenWithoutCombat(t *testing.T) {
	r := rules(t)
	p := &world.Player{ID: 1, HP: 8} // never in combat
	h := newHarness(t, r, p)
	h.runTo(5950 * time.Millisecond)
	if p.HP != 8 {
		t.Fatalf("HP at 5.95 s = %d, want 8 (no catch-up on first sight)", p.HP)
	}
	h.runTo(6 * time.Second)
	if p.HP != 9 {
		t.Fatalf("HP at 6 s = %d, want 9", p.HP)
	}
}

func TestRegenIgnoresStepSize(t *testing.T) {
	r := rules(t)
	p := &world.Player{ID: 1, HP: 1, LastCombat: t0}
	h := newHarness(t, r, p)
	h.clock.Advance(23 * time.Second) // heals due at 10, 16 and 22 s
	h.w.Tick()
	if p.HP != 4 {
		t.Fatalf("HP after one 23 s step = %d, want 4", p.HP)
	}
}

func TestRegenPlayerRemovedAndReadded(t *testing.T) {
	r := rules(t)
	p := &world.Player{ID: 1, HP: 5, LastCombat: t0}
	h := newHarness(t, r, p)
	h.w.RemovePlayer(1)
	h.runTo(30 * time.Second)
	if err := h.w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	h.runTo(31 * time.Second)
	if p.HP != 5 {
		t.Fatalf("HP right after re-adding = %d, want 5 (no catch-up)", p.HP)
	}
	h.runTo(34 * time.Second)
	if p.HP != 6 {
		t.Fatalf("HP at 34 s = %d, want 6", p.HP)
	}
}

func TestNewRejectsBadRules(t *testing.T) {
	tests := []struct {
		name                string
		hp, interval, delay int
		ok                  bool
	}{
		{"spec", 1, 6, 10, true},
		{"no delay", 1, 6, 0, true},
		{"zero hp", 0, 6, 10, false},
		{"zero interval", 1, 0, 10, false},
		{"negative delay", 1, 6, -1, false},
	}
	for _, tt := range tests {
		r := rules(t)
		r.RegenHP, r.RegenIntervalSeconds, r.RegenDelaySeconds = tt.hp, tt.interval, tt.delay
		_, err := regen.New(r, world.NewFakeClock(t0))
		if (err == nil) != tt.ok {
			t.Errorf("%s: err = %v, want ok %v", tt.name, err, tt.ok)
		}
	}
}

func BenchmarkRegenTick(b *testing.B) {
	r := rules(b)
	clock := world.NewFakeClock(t0)
	sys, err := regen.New(r, clock)
	if err != nil {
		b.Fatal(err)
	}
	w := world.New(world.DefaultConfig())
	for i := range 1000 {
		if err := w.AddPlayer(&world.Player{ID: world.PlayerID(i + 1), HP: 1, LastCombat: t0}); err != nil {
			b.Fatal(err)
		}
	}
	step := w.TickInterval()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		clock.Advance(step)
		sys.Tick(w, step)
	}
}
