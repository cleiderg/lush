package world_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/world"
)

type cmd struct {
	id  world.PlayerID
	seq int
}

func (c cmd) Target() world.PlayerID { return c.id }

type rec struct {
	name  string
	mu    sync.Mutex
	seqs  []int
	ticks int
}

func (r *rec) Name() string { return r.name }
func (r *rec) Handle(c world.Command, p *world.Player) {
	r.seqs = append(r.seqs, c.(cmd).seq)
}
func (r *rec) Tick(*world.World, time.Duration) { r.ticks++ }

func TestPlayerState(t *testing.T) {
	w := world.New(world.DefaultConfig())
	p := &world.Player{ID: 1, Pos: world.Vec2{X: 1, Y: 2}, Facing: 1.5, Anim: world.AnimWalk}
	if err := w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	if w.AddPlayer(p) == nil {
		t.Fatal("duplicate add should fail")
	}
	if got := w.Player(1); got != p || got.Pos.X != 1 || got.Facing != 1.5 || got.Anim != world.AnimWalk {
		t.Fatalf("bad player %+v", got)
	}
	w.RemovePlayer(1)
	if w.Player(1) != nil || len(w.Players()) != 0 {
		t.Fatal("not removed")
	}
}

func TestFixedTickRate(t *testing.T) {
	tests := []struct {
		name string
		rate int
		want uint64
	}{{"default 20Hz", 20, 20}, {"configured 30Hz", 30, 30}, {"zero falls back", 0, 20}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := world.New(world.Config{TickRate: tc.rate})
			clk := world.NewFakeClock(time.Unix(0, 0))
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { w.Run(ctx, clk); close(done) }()
			// Wait for the loop to create its ticker.
			for i := 0; i < 1000; i++ {
				if clk.TickerCount() > 0 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			clk.Advance(time.Second)
			cancel()
			<-done
			if got := w.TickCount(); got != tc.want {
				t.Fatalf("ticks = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCommandsApplyInOrderOnNextTick(t *testing.T) {
	w := world.New(world.DefaultConfig())
	r := &rec{name: "r"}
	_ = w.Register(r)
	_ = w.AddPlayer(&world.Player{ID: 1})
	for i := 0; i < 5; i++ {
		w.Enqueue(cmd{1, i})
	}
	if len(r.seqs) != 0 {
		t.Fatal("applied before tick")
	}
	w.Tick()
	if want := []int{0, 1, 2, 3, 4}; !reflect.DeepEqual(r.seqs, want) {
		t.Fatalf("got %v want %v", r.seqs, want)
	}
	w.Tick()
	if len(r.seqs) != 5 {
		t.Fatal("commands applied twice")
	}
}

func TestConcurrentEnqueue(t *testing.T) {
	w := world.New(world.DefaultConfig())
	r := &rec{name: "r"}
	_ = w.Register(r)
	_ = w.AddPlayer(&world.Player{ID: 1})
	const goroutines, per = 8, 200
	var wg sync.WaitGroup
	stop := make(chan struct{})
	ticked := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				close(ticked)
				return
			default:
				w.Tick()
			}
		}
	}()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				w.Enqueue(cmd{1, g*per + i})
			}
		}(g)
	}
	wg.Wait()
	close(stop)
	<-ticked
	w.Tick()
	if len(r.seqs) != goroutines*per {
		t.Fatalf("got %d commands, want %d", len(r.seqs), goroutines*per)
	}
	// Per-producer order must be preserved.
	last := map[int]int{}
	for _, s := range r.seqs {
		g := s / per
		if prev, ok := last[g]; ok && s < prev {
			t.Fatalf("producer %d out of order", g)
		}
		last[g] = s
	}
}

func TestSystemPeriod(t *testing.T) {
	w := world.New(world.DefaultConfig())
	every := &rec{name: "movement"}
	combat := &rec{name: "combat"}
	_ = w.Register(every)
	if err := w.RegisterEvery(combat, 12); err != nil {
		t.Fatal(err)
	}
	if w.Register(&rec{name: "combat"}) == nil {
		t.Fatal("duplicate name should fail")
	}
	if w.RegisterEvery(&rec{name: "x"}, 0) == nil {
		t.Fatal("period 0 should fail")
	}
	for i := 0; i < 36; i++ {
		w.Tick()
	}
	if every.ticks != 36 || combat.ticks != 3 {
		t.Fatalf("every=%d combat=%d, want 36 and 3", every.ticks, combat.ticks)
	}
}
