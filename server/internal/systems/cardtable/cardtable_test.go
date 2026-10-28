package cardtable

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/world"
)

var errBadMove = errors.New("bad move")

// fakeRules is a scriptable rules module that records what the table asks.
type fakeRules struct {
	min      int
	started  []Seat
	moves    []Seat
	timeouts []Seat
	next     Outcome // returned by the next Move or Timeout
	over     bool
	winner   Seat
}

func (f *fakeRules) MinPlayers() int          { return f.min }
func (f *fakeRules) Start(seats []Seat) error { f.started = seats; f.over = false; return nil }
func (f *fakeRules) Move(s Seat, m any) (Outcome, error) {
	if m == "bad" {
		return Outcome{}, errBadMove
	}
	f.moves = append(f.moves, s)
	out := f.next
	f.next = Outcome{}
	return out, nil
}
func (f *fakeRules) Timeout(s Seat) Outcome {
	f.timeouts = append(f.timeouts, s)
	out := f.next
	f.next = Outcome{}
	return out
}
func (f *fakeRules) PublicView() any                { return "public" }
func (f *fakeRules) PrivateView(s Seat) any         { return fmt.Sprintf("hand-%d", s) }
func (f *fakeRules) Over() (winner Seat, over bool) { return f.winner, f.over }

func newTable(t *testing.T, seats int, timeout uint64, min int) (*Table, *fakeRules) {
	t.Helper()
	r := &fakeRules{min: min}
	tb, err := New(seats, timeout, r)
	if err != nil {
		t.Fatal(err)
	}
	return tb, r
}

// startGame seats players 1..n and starts at tick 0.
func startGame(t *testing.T, seats, n int, timeout uint64) (*Table, *fakeRules) {
	t.Helper()
	tb, r := newTable(t, seats, timeout, 2)
	for i := 1; i <= n; i++ {
		if _, err := tb.Sit(world.PlayerID(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tb.Start(0); err != nil {
		t.Fatal(err)
	}
	return tb, r
}

func turn(t *testing.T, tb *Table) Seat {
	t.Helper()
	s, ok := tb.Turn()
	if !ok {
		t.Fatal("no turn: game not in progress")
	}
	return s
}

func TestNewRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name    string
		seats   int
		timeout uint64
		rules   Rules
	}{
		{"zero seats", 0, 10, &fakeRules{}},
		{"zero timeout", 4, 0, &fakeRules{}},
		{"nil rules", 4, 10, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.seats, tt.timeout, tt.rules); err == nil {
				t.Error("New succeeded, want error")
			}
		})
	}
}

// AC1
func TestSitAndLeave(t *testing.T) {
	tb, _ := newTable(t, 2, 10, 2)
	if s, err := tb.Sit(7); err != nil || s != 0 {
		t.Fatalf("Sit(7) = %d, %v; want 0, nil", s, err)
	}
	if _, err := tb.Sit(7); !errors.Is(err, ErrAlreadySeated) {
		t.Errorf("second Sit(7) err = %v, want ErrAlreadySeated", err)
	}
	if s, err := tb.Sit(8); err != nil || s != 1 {
		t.Fatalf("Sit(8) = %d, %v; want 1, nil", s, err)
	}
	if _, err := tb.Sit(9); !errors.Is(err, ErrTableFull) {
		t.Errorf("Sit(9) on full table err = %v, want ErrTableFull", err)
	}
	if err := tb.Leave(7); err != nil {
		t.Fatalf("Leave(7) = %v", err)
	}
	if _, ok := tb.Occupant(0); ok {
		t.Error("seat 0 still occupied after Leave")
	}
	if err := tb.Leave(7); !errors.Is(err, ErrNotSeated) {
		t.Errorf("Leave(7) again err = %v, want ErrNotSeated", err)
	}
	if s, err := tb.Sit(9); err != nil || s != 0 {
		t.Errorf("Sit(9) after Leave = %d, %v; want 0, nil", s, err)
	}
	if p, ok := tb.Occupant(1); !ok || p != 8 {
		t.Errorf("Occupant(1) = %d, %v; want 8, true", p, ok)
	}
}

// AC2
func TestStartNeedsMinPlayers(t *testing.T) {
	tb, r := newTable(t, 4, 10, 3)
	for i, p := range []world.PlayerID{1, 2} {
		if _, err := tb.Sit(p); err != nil {
			t.Fatal(err)
		}
		if err := tb.Start(0); !errors.Is(err, ErrTooFewPlayers) {
			t.Errorf("Start with %d seated err = %v, want ErrTooFewPlayers", i+1, err)
		}
	}
	if tb.InProgress() || r.started != nil {
		t.Fatal("rejected Start began a game")
	}
	if _, err := tb.Sit(3); err != nil {
		t.Fatal(err)
	}
	if err := tb.Start(0); err != nil {
		t.Fatalf("Start with 3 seated = %v", err)
	}
	if !tb.InProgress() || !reflect.DeepEqual(r.started, []Seat{0, 1, 2}) {
		t.Errorf("after Start: in progress %v, rules started %v", tb.InProgress(), r.started)
	}
	if _, err := tb.Sit(4); !errors.Is(err, ErrInProgress) {
		t.Errorf("Sit during game err = %v, want ErrInProgress", err)
	}
	if err := tb.Leave(1); !errors.Is(err, ErrInProgress) {
		t.Errorf("Leave during game err = %v, want ErrInProgress", err)
	}
	if s, ok := tb.SeatOf(1); !ok || s != 0 {
		t.Error("rejected Leave freed the seat")
	}
	if err := tb.Start(0); !errors.Is(err, ErrInProgress) {
		t.Errorf("Start during game err = %v, want ErrInProgress", err)
	}
}

func TestStartUsesOnlySeatedSeats(t *testing.T) {
	tb, r := newTable(t, 4, 10, 2)
	for _, p := range []world.PlayerID{1, 2, 3} {
		tb.Sit(p)
	}
	tb.Leave(2) // seat 1 empty
	if err := tb.Start(0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.started, []Seat{0, 2}) {
		t.Errorf("rules started %v, want [0 2]", r.started)
	}
	if err := tb.Move(1, "ok", 1); err != nil {
		t.Fatal(err)
	}
	if s := turn(t, tb); s != 2 {
		t.Errorf("turn = %d, want 2 (empty seat 1 not in game)", s)
	}
}

// AC3
func TestTurnOrder(t *testing.T) {
	// Seats 0..3 hold players 1..4. Each step: the outcome the rules module
	// returns for the current seat's move, and the seat whose turn is next.
	type step struct {
		out  Outcome
		want Seat
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"forward wraps", []step{{Outcome{}, 1}, {Outcome{}, 2}, {Outcome{}, 3}, {Outcome{}, 0}}},
		{"reverse", []step{{Outcome{}, 1}, {Outcome{Reverse: true}, 0}, {Outcome{}, 3}, {Outcome{}, 2}}},
		{"reverse from first wraps", []step{{Outcome{Reverse: true}, 3}, {Outcome{}, 2}}},
		{"skip", []step{{Outcome{Skip: 1}, 2}, {Outcome{}, 3}, {Outcome{Skip: 1}, 1}}},
		{"reverse and skip", []step{{Outcome{Reverse: true, Skip: 1}, 2}, {Outcome{}, 1}}},
		{"skip around table", []step{{Outcome{Skip: 3}, 0}, {Outcome{Skip: 5}, 2}}},
		{"reverse twice", []step{{Outcome{Reverse: true}, 3}, {Outcome{Reverse: true}, 0}, {Outcome{}, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb, r := startGame(t, 4, 4, 100)
			for i, st := range tt.steps {
				cur := turn(t, tb)
				p, _ := tb.Occupant(cur)
				r.next = st.out
				if err := tb.Move(p, "ok", uint64(i)); err != nil {
					t.Fatalf("step %d: Move = %v", i, err)
				}
				if got := turn(t, tb); got != st.want {
					t.Fatalf("step %d: turn = %d, want %d", i, got, st.want)
				}
			}
		})
	}
}

// AC4
func TestTurnTimer(t *testing.T) {
	const timeout = 10
	tb, r := startGame(t, 3, 3, timeout)
	if d, _ := tb.Deadline(); d != timeout {
		t.Fatalf("deadline after Start = %d, want %d", d, timeout)
	}
	for now := uint64(0); now < timeout; now++ {
		tb.Tick(now)
	}
	if len(r.timeouts) != 0 {
		t.Fatalf("timeouts before deadline: %v", r.timeouts)
	}
	tb.Tick(timeout)
	if !reflect.DeepEqual(r.timeouts, []Seat{0}) {
		t.Fatalf("timeouts at deadline = %v, want [0]", r.timeouts)
	}
	if s := turn(t, tb); s != 1 {
		t.Errorf("turn after timeout = %d, want 1", s)
	}
	if d, _ := tb.Deadline(); d != 2*timeout {
		t.Errorf("deadline after timeout = %d, want %d", d, 2*timeout)
	}
	// Exactly once: further ticks before the new deadline do nothing.
	for now := uint64(timeout + 1); now < 2*timeout; now++ {
		tb.Tick(now)
	}
	if len(r.timeouts) != 1 {
		t.Errorf("timeouts = %v, want exactly one", r.timeouts)
	}

	// The timeout outcome passes the turn like a move does.
	r.next = Outcome{Reverse: true}
	tb.Tick(2 * timeout)
	if !reflect.DeepEqual(r.timeouts, []Seat{0, 1}) {
		t.Fatalf("timeouts = %v, want [0 1]", r.timeouts)
	}
	if s := turn(t, tb); s != 0 {
		t.Errorf("turn after reversing timeout = %d, want 0", s)
	}

	// An accepted move restarts the deadline.
	const moveAt = 2*timeout + 3
	p, _ := tb.Occupant(0)
	if err := tb.Move(p, "ok", moveAt); err != nil {
		t.Fatal(err)
	}
	if d, _ := tb.Deadline(); d != moveAt+timeout {
		t.Errorf("deadline after move = %d, want %d", d, moveAt+timeout)
	}
	tb.Tick(3 * timeout) // old deadline would have been here
	if len(r.timeouts) != 2 {
		t.Errorf("timeout fired at old deadline: %v", r.timeouts)
	}
	tb.Tick(moveAt + timeout)
	if !reflect.DeepEqual(r.timeouts, []Seat{0, 1, 2}) {
		t.Errorf("timeouts = %v, want [0 1 2]", r.timeouts)
	}
}

type snapshot struct {
	inProgress bool
	turn       Seat
	reversed   bool
	deadline   uint64
	moves      int
}

func snap(tb *Table, r *fakeRules) snapshot {
	s, _ := tb.Turn()
	d, _ := tb.Deadline()
	return snapshot{tb.InProgress(), s, tb.Reversed(), d, len(r.moves)}
}

// AC5
func TestRejectedMoves(t *testing.T) {
	tests := []struct {
		name    string
		start   bool
		player  world.PlayerID
		move    any
		wantErr error
	}{
		{"not your turn", true, 2, "ok", ErrNotYourTurn},
		{"not seated", true, 99, "ok", ErrNotSeated},
		{"no game", false, 1, "ok", ErrNoGame},
		{"rules reject", true, 1, "bad", errBadMove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb, r := newTable(t, 3, 10, 2)
			tb.Sit(1)
			tb.Sit(2)
			if tt.start {
				if err := tb.Start(0); err != nil {
					t.Fatal(err)
				}
			}
			before := snap(tb, r)
			if err := tb.Move(tt.player, tt.move, 5); !errors.Is(err, tt.wantErr) {
				t.Errorf("Move err = %v, want %v", err, tt.wantErr)
			}
			if after := snap(tb, r); after != before {
				t.Errorf("state changed: %+v -> %+v", before, after)
			}
		})
	}
}

// AC6
func TestGameOver(t *testing.T) {
	const timeout = 10
	tb, r := startGame(t, 3, 3, timeout)
	if _, ok := tb.Winner(); ok {
		t.Fatal("winner before game over")
	}
	r.over, r.winner = true, 0
	if err := tb.Move(1, "ok", 1); err != nil {
		t.Fatal(err)
	}
	if w, ok := tb.Winner(); !ok || w != 0 {
		t.Errorf("Winner = %d, %v; want 0, true", w, ok)
	}
	if tb.InProgress() {
		t.Error("game still in progress")
	}
	for _, p := range []world.PlayerID{1, 2} {
		if err := tb.Move(p, "ok", 2); !errors.Is(err, ErrNoGame) {
			t.Errorf("Move(%d) after game over err = %v, want ErrNoGame", p, err)
		}
	}
	for now := uint64(0); now < 10*timeout; now++ {
		tb.Tick(now)
	}
	if len(r.timeouts) != 0 {
		t.Errorf("timeouts after game over: %v", r.timeouts)
	}
	if err := tb.Start(100); err != nil {
		t.Fatalf("new Start = %v", err)
	}
	if _, ok := tb.Winner(); ok {
		t.Error("old winner still reported after new Start")
	}
	if s := turn(t, tb); s != 0 {
		t.Errorf("first turn of new game = %d, want 0", s)
	}
}

func TestGameOverOnTimeout(t *testing.T) {
	tb, r := startGame(t, 2, 2, 5)
	r.over, r.winner = true, 1
	tb.Tick(5)
	if w, ok := tb.Winner(); !ok || w != 1 {
		t.Errorf("Winner = %d, %v; want 1, true", w, ok)
	}
	tb.Tick(10)
	if len(r.timeouts) != 1 {
		t.Errorf("timeouts = %v, want one", r.timeouts)
	}
}

// AC7
func TestView(t *testing.T) {
	tb, _ := startGame(t, 4, 3, 10)
	for p := world.PlayerID(1); p <= 3; p++ {
		v := tb.View(p)
		seat := Seat(p - 1)
		want := View{Seated: true, Seat: seat, Public: "public", Private: fmt.Sprintf("hand-%d", seat)}
		if v != want {
			t.Errorf("View(%d) = %+v, want %+v", p, v, want)
		}
	}
	if v := tb.View(99); v != (View{Public: "public"}) {
		t.Errorf("View(unseated) = %+v, want public only", v)
	}
}

// AC8
func TestNoForbiddenImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if forbiddenImport(p) {
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files checked")
	}
}

func forbiddenImport(path string) bool {
	for _, p := range []string{"time", "os", "net"} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	for _, p := range []string{"transport", "session", "store", "api"} {
		pkg := "github.com/cleiderg/lush/server/internal/" + p
		if path == pkg || strings.HasPrefix(path, pkg+"/") {
			return true
		}
	}
	return false
}

func TestForbiddenImport(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"time", true},
		{"os", true},
		{"os/exec", true},
		{"net", true},
		{"net/http", true},
		{"github.com/cleiderg/lush/server/internal/transport", true},
		{"github.com/cleiderg/lush/server/internal/session", true},
		{"github.com/cleiderg/lush/server/internal/store", true},
		{"github.com/cleiderg/lush/server/internal/api", true},
		{"github.com/cleiderg/lush/server/internal/world", false},
		{"errors", false},
		{"fmt", false},
		{"network", false},
	}
	for _, tt := range tests {
		if got := forbiddenImport(tt.path); got != tt.want {
			t.Errorf("forbiddenImport(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func BenchmarkTick(b *testing.B) {
	r := &fakeRules{min: 2}
	tb, _ := New(4, 1<<62, r)
	for p := world.PlayerID(1); p <= 4; p++ {
		tb.Sit(p)
	}
	tb.Start(0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Tick(uint64(i))
	}
}
