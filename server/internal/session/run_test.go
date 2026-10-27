package session_test

import (
	"math"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/ops"
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/world"
)

// inputRun is input with the Run action flag set or clear.
func inputRun(seq uint32, x, z float32, run bool) []byte {
	return envelope(protocol.MessageInput, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.InputStart(b)
		protocol.InputAddSeq(b, seq)
		protocol.InputAddMove(b, protocol.CreateVec2(b, x, z))
		if run {
			protocol.InputAddActions(b, byte(protocol.ActionFlagsRun))
		}
		return protocol.InputEnd(b)
	})
}

// waitInput reads until a snapshot acknowledges input seq.
func (r *rig) waitInput(c *client, seq uint32) {
	r.t.Helper()
	for i := 0; i < 1000; i++ {
		tbl := r.waitFor(c, protocol.MessageSnapshot)
		var s protocol.Snapshot
		s.Init(tbl.Bytes, tbl.Pos)
		if s.LastInputSeq() >= seq {
			return
		}
	}
	r.t.Fatalf("input %d never acknowledged", seq)
}

func speedConfig(walk, run float32) session.Config {
	return session.Config{TickRate: 20, MoveSpeed: walk, RunSpeed: run, HelloTimeout: time.Second}
}

// TestInputRunFlag (#237 AC3): an Input with the Run flag marks the player
// Running and puts its desired position RunSpeed*tick away; one without it
// clears Running and uses the walk speed.
func TestInputRunFlag(t *testing.T) {
	const walk, run = 4, 7
	r := newRigCfg(t, 16, speedConfig(walk, run))
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	id, _ := r.expectWelcome(c)
	tick := r.w.TickInterval().Seconds()

	for i, step := range []struct {
		run   bool
		speed float64
	}{{true, run}, {false, walk}, {true, run}, {false, walk}} {
		seq := uint32(i + 1)
		before := r.w.Player(world.PlayerID(id)).Pos
		// A stick pushed past full is clamped to full: the reach never
		// exceeds speed*tick.
		c.send(inputRun(seq, 3, 0, step.run))
		r.waitInput(c, seq)
		p := r.w.Player(world.PlayerID(id))
		if p.Running != step.run {
			t.Fatalf("input %d (run %v): Running = %v", seq, step.run, p.Running)
		}
		got := math.Hypot(float64(p.Desired.X-before.X), float64(p.Desired.Y-before.Y))
		if want := step.speed * tick; math.Abs(got-want) > 1e-4 {
			t.Fatalf("input %d (run %v): desired %.4f m away, want %.4f", seq, step.run, got, want)
		}
		if p.Pos != p.Desired {
			t.Fatalf("input %d (run %v): player at %+v, never reached desired %+v", seq, step.run, p.Pos, p.Desired)
		}
	}
}

// TestWelcomeCarriesSpeeds (#237 AC4): the Welcome a client receives carries
// the configured walk and run speeds, on a first join and on a reconnect.
func TestWelcomeCarriesSpeeds(t *testing.T) {
	def, err := ops.LoadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if def.MoveSpeed != 4 || def.RunSpeed != 7 {
		t.Fatalf("default speeds walk %v run %v, want 4 7 (D-044)", def.MoveSpeed, def.RunSpeed)
	}
	for _, tc := range []struct {
		name              string
		walk, run         float32
		wantWalk, wantRun float32
	}{
		{"defaults", def.MoveSpeed, def.RunSpeed, 4, 7},
		{"configured", 3, 9.5, 3, 9.5},
		{"no run speed runs at walk speed", 5, 0, 5, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigCfg(t, 16, speedConfig(tc.walk, tc.run))
			v := uint16(protocol.ProtocolVersionCurrent)
			check := func(what string, c *client) string {
				t.Helper()
				typ, tbl := r.next(c)
				if typ != protocol.MessageWelcome {
					t.Fatalf("%s: got message %v, want Welcome", what, typ)
				}
				var w protocol.Welcome
				w.Init(tbl.Bytes, tbl.Pos)
				if w.WalkSpeed() != tc.wantWalk || w.RunSpeed() != tc.wantRun {
					t.Fatalf("%s: Welcome speeds walk %v run %v, want %v %v", what, w.WalkSpeed(), w.RunSpeed(), tc.wantWalk, tc.wantRun)
				}
				return string(w.GuestToken())
			}
			c1 := r.dial(hello(v, ""))
			tok := check("first join", c1)
			c2 := r.dial(hello(v, tok)) // takes over the player still in the world
			check("reconnect", c2)
		})
	}
}
