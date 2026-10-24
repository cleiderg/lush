package emote_test

import (
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/emote"
	"github.com/cleiderg/lush/server/internal/systems/movement"
	"github.com/cleiderg/lush/server/internal/world"
)

func setup(t *testing.T) *world.World {
	t.Helper()
	w := world.New(world.DefaultConfig())
	mv, err := movement.New(movement.Config{MaxSpeed: 6})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Register(mv); err != nil {
		t.Fatal(err)
	}
	if err := w.Register(emote.New(emote.DefaultConfig())); err != nil {
		t.Fatal(err)
	}
	_ = w.AddPlayer(&world.Player{ID: 1})
	return w
}

func TestDefaultConfig(t *testing.T) {
	if c := emote.DefaultConfig(); c.WaveDurationTicks != 40 || c.WaveCooldownTicks != 40 {
		t.Fatalf("got %+v", c)
	}
	if world.AnimWave != 2 {
		t.Fatalf("AnimWave = %d, want 2", world.AnimWave)
	}
}

func TestWaveDuration(t *testing.T) {
	w := setup(t)
	p := w.Player(1)
	w.Enqueue(emote.WaveCommand{Player: 1})
	for i := 1; i <= 40; i++ {
		w.Tick()
		if p.Anim != world.AnimWave {
			t.Fatalf("tick %d: anim %v, want wave", i, p.Anim)
		}
	}
	w.Tick()
	if p.Anim != world.AnimIdle {
		t.Fatalf("after 40 ticks: anim %v, want idle", p.Anim)
	}
}

func TestWaveEndsOnWalk(t *testing.T) {
	w := setup(t)
	p := w.Player(1)
	w.Enqueue(emote.WaveCommand{Player: 1})
	for i := 0; i < 5; i++ {
		w.Tick()
	}
	// 0.6 units at 6 u/s is two ticks of walking.
	w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 0.6, Y: 0}})
	for i := 0; i < 2; i++ {
		w.Tick()
		if p.Anim != world.AnimWalk {
			t.Fatalf("anim %v, want walk", p.Anim)
		}
	}
	// The wave ended: it does not resume once the player stops.
	for i := 0; i < 40; i++ {
		w.Tick()
		if p.Anim != world.AnimIdle {
			t.Fatalf("tick %d after stopping: anim %v, want idle", i+1, p.Anim)
		}
	}
}

func TestWaveAcceptedAfterWalkEndsIt(t *testing.T) {
	w := setup(t)
	p := w.Player(1)
	w.Enqueue(emote.WaveCommand{Player: 1})
	for i := 0; i < 5; i++ {
		w.Tick()
	}
	w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 0.6, Y: 0}})
	for i := 0; i < 3; i++ { // two ticks walking, then idle
		w.Tick()
	}
	// No wave is showing any more, so a new one is not ignored (Q4).
	w.Enqueue(emote.WaveCommand{Player: 1})
	w.Tick()
	if p.Anim != world.AnimWave {
		t.Fatalf("anim %v, want wave", p.Anim)
	}
}

func TestWaveOnArrivalTick(t *testing.T) {
	w := setup(t)
	p := w.Player(1)
	w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 0.3, Y: 0}})
	w.Tick() // arrives this tick; Anim is still walk
	w.Enqueue(emote.WaveCommand{Player: 1})
	w.Tick()
	if p.Anim != world.AnimWave {
		t.Fatalf("anim %v, want wave", p.Anim)
	}
}

func TestWaveIgnoredWhileWalking(t *testing.T) {
	w := setup(t)
	p := w.Player(1)
	w.Enqueue(movement.MoveCommand{Player: 1, To: world.Vec2{X: 100, Y: 0}})
	w.Tick()
	w.Enqueue(emote.WaveCommand{Player: 1})
	w.Tick()
	if p.Anim != world.AnimWalk {
		t.Fatalf("anim %v, want walk", p.Anim)
	}
}

func TestWaveCooldown(t *testing.T) {
	cfg := emote.Config{WaveDurationTicks: 5, WaveCooldownTicks: 10}
	for _, tc := range []struct {
		name   string
		gap    int // ticks between first and second command
		accept bool
	}{
		{"inside cooldown", 9, false},
		{"after cooldown", 10, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := world.New(world.DefaultConfig())
			_ = w.Register(emote.New(cfg))
			_ = w.AddPlayer(&world.Player{ID: 1})
			p := w.Player(1)
			w.Enqueue(emote.WaveCommand{Player: 1})
			for i := 0; i < tc.gap; i++ {
				w.Tick()
			}
			p.Anim = world.AnimIdle
			w.Enqueue(emote.WaveCommand{Player: 1})
			w.Tick()
			if got := p.Anim == world.AnimWave; got != tc.accept {
				t.Fatalf("accepted=%v, want %v", got, tc.accept)
			}
		})
	}
}
