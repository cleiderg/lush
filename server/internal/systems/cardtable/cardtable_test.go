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
	forfeits []Seat
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
func (f *fakeRules) Forfeit(s Seat)                 { f.forfeits = append(f.forfeits, s) }
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
	readyAll(t, tb, 0)
	if !tb.InProgress() {
		t.Fatal("game did not start once everyone was ready")
	}
	return tb, r
}

// readyAll sets Ready for every seated player at tick now.
func readyAll(t *testing.T, tb *Table, now uint64) {
	t.Helper()
	for i := 0; i < tb.Seats(); i++ {
		if p, ok := tb.Occupant(Seat(i)); ok {
			if err := tb.SetReady(p, true, now); err != nil {
				t.Fatal(err)
			}
		}
	}
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
	if err := tb.Leave(7, 0); err != nil {
		t.Fatalf("Leave(7) = %v", err)
	}
	if _, ok := tb.Occupant(0); ok {
		t.Error("seat 0 still occupied after Leave")
	}
	if err := tb.Leave(7, 0); !errors.Is(err, ErrNotSeated) {
		t.Errorf("Leave(7) again err = %v, want ErrNotSeated", err)
	}
	if s, err := tb.Sit(9); err != nil || s != 0 {
		t.Errorf("Sit(9) after Leave = %d, %v; want 0, nil", s, err)
	}
	if p, ok := tb.Occupant(1); !ok || p != 8 {
		t.Errorf("Occupant(1) = %d, %v; want 8, true", p, ok)
	}
}

// #301 AC1
func TestStartNeedsEveryoneReady(t *testing.T) {
	tb, r := newTable(t, 4, 10, 3)
	ready := func(p world.PlayerID, on bool) {
		t.Helper()
		if err := tb.SetReady(p, on, 0); err != nil {
			t.Fatalf("SetReady(%d, %v) = %v", p, on, err)
		}
	}
	if err := tb.SetReady(1, true, 0); !errors.Is(err, ErrNotSeated) {
		t.Errorf("SetReady unseated err = %v, want ErrNotSeated", err)
	}
	// Two seated and both ready is below MinPlayers 3: no game.
	tb.Sit(1)
	tb.Sit(2)
	ready(1, true)
	ready(2, true)
	if tb.InProgress() || r.started != nil {
		t.Fatal("game started below MinPlayers")
	}
	// A third player sits but is not ready: no game.
	tb.Sit(3)
	if tb.InProgress() {
		t.Fatal("game started with an unready seated player")
	}
	// One player clears Ready as the last one sets it: still no game.
	ready(1, false)
	ready(3, true)
	if tb.InProgress() {
		t.Fatal("game started after a player cleared Ready")
	}
	if tb.Ready(0) || !tb.Ready(1) || !tb.Ready(2) {
		t.Errorf("ready flags = %v %v %v, want false true true", tb.Ready(0), tb.Ready(1), tb.Ready(2))
	}
	ready(1, true)
	if !tb.InProgress() || !reflect.DeepEqual(r.started, []Seat{0, 1, 2}) {
		t.Fatalf("after all ready: in progress %v, rules started %v", tb.InProgress(), r.started)
	}
	for s := Seat(0); s < 3; s++ {
		if tb.Ready(s) {
			t.Errorf("seat %d still ready after the game started", s)
		}
	}
	if _, err := tb.Sit(4); !errors.Is(err, ErrInProgress) {
		t.Errorf("Sit during game err = %v, want ErrInProgress", err)
	}
	if err := tb.SetReady(1, true, 1); !errors.Is(err, ErrInProgress) {
		t.Errorf("SetReady during game err = %v, want ErrInProgress", err)
	}
}

// #301 AC1
func TestLeaveClearsReady(t *testing.T) {
	tb, r := newTable(t, 4, 10, 2)
	for _, p := range []world.PlayerID{1, 2, 3} {
		tb.Sit(p)
	}
	tb.SetReady(1, true, 0)
	tb.SetReady(2, true, 0)
	// The unready player leaves: the others must press Ready again rather
	// than being thrown into a game.
	if err := tb.Leave(3, 0); err != nil {
		t.Fatal(err)
	}
	if tb.InProgress() {
		t.Fatal("game started when the unready player left")
	}
	if tb.Ready(0) || tb.Ready(1) || tb.Ready(2) {
		t.Errorf("ready flags after leave = %v %v %v, want all false", tb.Ready(0), tb.Ready(1), tb.Ready(2))
	}
	// The leaver's own flag does not follow them back into the seat.
	tb.SetReady(1, true, 0)
	tb.Leave(1, 0)
	tb.Sit(1)
	if tb.Ready(0) {
		t.Error("re-seated player is still ready")
	}
	tb.SetReady(1, true, 0)
	tb.SetReady(2, true, 0)
	if !tb.InProgress() || !reflect.DeepEqual(r.started, []Seat{0, 1}) {
		t.Errorf("in progress %v, started %v; want game for seats [0 1]", tb.InProgress(), r.started)
	}
}

func TestStartUsesOnlySeatedSeats(t *testing.T) {
	tb, r := newTable(t, 4, 10, 2)
	for _, p := range []world.PlayerID{1, 2, 3} {
		tb.Sit(p)
	}
	tb.Leave(2, 0) // seat 1 empty
	readyAll(t, tb, 0)
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
				readyAll(t, tb, 0)
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
	readyAll(t, tb, 100)
	if !tb.InProgress() {
		t.Fatal("new game did not start")
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
		tb.SetReady(p, true, 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Tick(uint64(i))
	}
}

// timeoutAt runs Tick at the current deadline.
func timeoutAt(t *testing.T, tb *Table) uint64 {
	t.Helper()
	d, ok := tb.Deadline()
	if !ok {
		t.Fatal("no deadline: game not in progress")
	}
	tb.Tick(d)
	return d
}

// #301 AC2
func TestMissedTurnsForfeit(t *testing.T) {
	const timeout = 10
	tb, r := startGame(t, 3, 3, timeout)
	// Every seat times out round after round. Seat 0 reaches the limit
	// first, on its third turn.
	for i := 0; i < 3*(MissedTurnLimit-1)+1; i++ {
		timeoutAt(t, tb)
	}
	if !reflect.DeepEqual(r.forfeits, []Seat{0}) {
		t.Fatalf("forfeits = %v, want [0]", r.forfeits)
	}
	if _, ok := tb.Occupant(0); ok {
		t.Error("forfeited seat 0 still occupied")
	}
	if _, ok := tb.SeatOf(1); ok {
		t.Error("player 1 still seated after forfeit")
	}
	if !tb.InProgress() {
		t.Fatal("game ended with two seats left")
	}
	now := uint64(3*(MissedTurnLimit-1)+1) * timeout
	if s, _ := tb.Turn(); s != 1 {
		t.Errorf("turn after forfeit = %d, want 1", s)
	}
	if d, _ := tb.Deadline(); d != now+timeout {
		t.Errorf("deadline after forfeit = %d, want %d", d, now+timeout)
	}
	// Seat 1 has missed 2 so far; it moves, so the count resets and the
	// next timeout does not forfeit it.
	if got := tb.Missed(1); got != MissedTurnLimit-1 {
		t.Fatalf("seat 1 missed = %d, want %d", got, MissedTurnLimit-1)
	}
	if err := tb.Move(2, "ok", now); err != nil {
		t.Fatal(err)
	}
	if got := tb.Missed(1); got != 0 {
		t.Errorf("seat 1 missed after move = %d, want 0", got)
	}
	// Seat 2 is on its last chance and times out: forfeits, leaving seat 1
	// the winner.
	timeoutAt(t, tb)
	if !reflect.DeepEqual(r.forfeits, []Seat{0, 2}) {
		t.Fatalf("forfeits = %v, want [0 2]", r.forfeits)
	}
	if w, ok := tb.Winner(); !ok || w != 1 || tb.InProgress() {
		t.Errorf("Winner = %d, %v (in progress %v); want 1, true, game over", w, ok, tb.InProgress())
	}
}

// #301 AC2
func TestMoveResetsMissedCount(t *testing.T) {
	tb, r := startGame(t, 2, 2, 10)
	// Seat 0 misses one less than the limit, moves, then misses one less
	// again: it never forfeits. Seat 1 always moves.
	for round := 0; round < 2; round++ {
		for i := 0; i < MissedTurnLimit-1; i++ {
			now := timeoutAt(t, tb) // seat 0 misses
			if err := tb.Move(2, "ok", now); err != nil {
				t.Fatal(err)
			}
		}
		if err := tb.Move(1, "ok", 0); err != nil {
			t.Fatal(err)
		}
		if err := tb.Move(2, "ok", 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.forfeits) != 0 || !tb.InProgress() {
		t.Fatalf("forfeits = %v, in progress %v; want none and still playing", r.forfeits, tb.InProgress())
	}
	if got := len(r.timeouts); got != 2*(MissedTurnLimit-1) {
		t.Errorf("timeouts = %d, want %d", got, 2*(MissedTurnLimit-1))
	}
}

// #301 AC3
func TestLeaveMidGameForfeits(t *testing.T) {
	tb, r := startGame(t, 4, 3, 10)
	// Player 2 (seat 1) leaves while seat 0 has the turn.
	if err := tb.Leave(2, 3); err != nil {
		t.Fatalf("Leave mid-game = %v, want nil", err)
	}
	if !reflect.DeepEqual(r.forfeits, []Seat{1}) {
		t.Fatalf("forfeits = %v, want [1]", r.forfeits)
	}
	if _, ok := tb.Occupant(1); ok {
		t.Error("leaver's seat still occupied")
	}
	if s, _ := tb.Turn(); s != 0 || !tb.InProgress() {
		t.Fatalf("turn = %d, in progress %v; want seat 0 still to play", s, tb.InProgress())
	}
	if d, _ := tb.Deadline(); d != 10 {
		t.Errorf("deadline = %d, want 10 (unchanged by another seat leaving)", d)
	}
	if err := tb.Move(1, "ok", 4); err != nil {
		t.Fatal(err)
	}
	if s, _ := tb.Turn(); s != 2 {
		t.Errorf("turn = %d, want 2 (seat 1 skipped)", s)
	}
	// Leaving does not let anyone sit mid-game.
	if _, err := tb.Sit(9); !errors.Is(err, ErrInProgress) {
		t.Errorf("Sit after a mid-game leave err = %v, want ErrInProgress", err)
	}
	// Player 3 (seat 2) leaves on their own turn: player 1 is the last one
	// left and wins.
	if err := tb.Leave(3, 5); err != nil {
		t.Fatal(err)
	}
	if w, ok := tb.Winner(); !ok || w != 0 {
		t.Errorf("Winner = %d, %v; want 0, true", w, ok)
	}
	if tb.InProgress() {
		t.Error("game still in progress with one player left")
	}
	if p, ok := tb.Occupant(0); !ok || p != 1 {
		t.Errorf("winner's seat = %d, %v; want player 1 still seated", p, ok)
	}
}

// #301 AC3
func TestForfeitRulesWinnerTakesPrecedence(t *testing.T) {
	tb, r := startGame(t, 3, 3, 10)
	// The rules module ends the game itself when seat 1 forfeits.
	r.over, r.winner = true, 2
	if err := tb.Leave(2, 1); err != nil {
		t.Fatal(err)
	}
	if w, ok := tb.Winner(); !ok || w != 2 || tb.InProgress() {
		t.Errorf("Winner = %d, %v; want rules module's winner 2", w, ok)
	}
}

// #301 AC4
func TestForfeitSkipsSeat(t *testing.T) {
	// Four players at seats 0..3. Before the forfeit, the moves listed in
	// pre are played (as outcomes); then the player at seat leaver leaves.
	// want is the turn sequence afterwards.
	tests := []struct {
		name   string
		pre    []Outcome
		leaver Seat
		want   []Seat
	}{
		{"current seat forward", nil, 0, []Seat{1, 2, 3, 1}},
		{"current seat last wraps", []Outcome{{}, {}, {}}, 3, []Seat{0, 1, 2, 0}},
		{"current seat reversed", []Outcome{{}, {Reverse: true}}, 0, []Seat{3, 2, 1, 3}},
		{"seat before current", []Outcome{{}, {}}, 0, []Seat{2, 3, 1, 2}},
		{"seat after current", nil, 2, []Seat{0, 1, 3, 0}},
		{"seat after current reversed", []Outcome{{Reverse: true}}, 2, []Seat{3, 1, 0, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb, r := startGame(t, 4, 4, 100)
			for i, out := range tt.pre {
				p, _ := tb.Occupant(turn(t, tb))
				r.next = out
				if err := tb.Move(p, "ok", uint64(i)); err != nil {
					t.Fatal(err)
				}
			}
			p, _ := tb.Occupant(tt.leaver)
			if err := tb.Leave(p, 50); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r.forfeits, []Seat{tt.leaver}) {
				t.Fatalf("forfeit hook calls = %v, want [%d]", r.forfeits, tt.leaver)
			}
			var got []Seat
			for i := range tt.want {
				cur := turn(t, tb)
				got = append(got, cur)
				if i == len(tt.want)-1 {
					break
				}
				p, _ := tb.Occupant(cur)
				if err := tb.Move(p, "ok", 60); err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("turns = %v, want %v", got, tt.want)
			}
		})
	}
}

// #301 AC4
func TestForfeitedSeatNeverTimesOut(t *testing.T) {
	tb, r := startGame(t, 3, 3, 10)
	tb.Leave(1, 0) // seat 0, whose turn it is
	// Seats 1 and 2 each miss one turn less than the limit.
	for i := 0; i < 2*(MissedTurnLimit-1); i++ {
		timeoutAt(t, tb)
	}
	for _, s := range r.timeouts {
		if s == 0 {
			t.Fatalf("forfeited seat 0 timed out: %v", r.timeouts)
		}
	}
}

// #301 AC5
func TestDisconnectedSeatTimesOutAtOnce(t *testing.T) {
	const timeout = 10
	tb, r := startGame(t, 3, 3, timeout)
	if err := tb.SetDisconnected(99, true); !errors.Is(err, ErrNotSeated) {
		t.Errorf("SetDisconnected unseated err = %v, want ErrNotSeated", err)
	}
	// Seat 1 drops while seat 0 has the turn; seat 0 still gets its full
	// timer.
	if err := tb.SetDisconnected(2, true); err != nil {
		t.Fatal(err)
	}
	if !tb.Disconnected(1) {
		t.Fatal("seat 1 not marked disconnected")
	}
	tb.Tick(1)
	if len(r.timeouts) != 0 {
		t.Fatalf("timeouts = %v before seat 0's deadline", r.timeouts)
	}
	if err := tb.Move(1, "ok", 2); err != nil {
		t.Fatal(err)
	}
	// Seat 1's turn: the next Tick runs its timeout without waiting.
	tb.Tick(3)
	if !reflect.DeepEqual(r.timeouts, []Seat{1}) {
		t.Fatalf("timeouts = %v, want [1] at the first tick of seat 1's turn", r.timeouts)
	}
	if got := tb.Missed(1); got != 1 {
		t.Errorf("seat 1 missed = %d, want 1", got)
	}
	if s := turn(t, tb); s != 2 {
		t.Errorf("turn = %d, want 2", s)
	}
	// Seat 1 reconnects: on its next turn it waits for the normal deadline.
	if err := tb.SetDisconnected(2, false); err != nil {
		t.Fatal(err)
	}
	if err := tb.Move(3, "ok", 4); err != nil {
		t.Fatal(err)
	}
	if err := tb.Move(1, "ok", 5); err != nil {
		t.Fatal(err)
	}
	for now := uint64(6); now < 5+timeout; now++ {
		tb.Tick(now)
	}
	if len(r.timeouts) != 1 {
		t.Fatalf("reconnected seat timed out before its deadline: %v", r.timeouts)
	}
	tb.Tick(5 + timeout)
	if !reflect.DeepEqual(r.timeouts, []Seat{1, 1}) {
		t.Errorf("timeouts = %v, want [1 1] at the normal deadline", r.timeouts)
	}
	if got := tb.Missed(1); got != 2 {
		t.Errorf("seat 1 missed = %d, want 2", got)
	}
}

// #301 AC5
func TestDisconnectedSeatForfeitsAfterLimit(t *testing.T) {
	tb, r := startGame(t, 3, 3, 1000)
	tb.SetDisconnected(1, true) // seat 0, whose turn it is
	now := uint64(1)
	for len(r.forfeits) == 0 && now < 100 {
		tb.Tick(now) // seat 0 times out at once
		now++
		if s := turn(t, tb); s != 0 {
			p, _ := tb.Occupant(s)
			if err := tb.Move(p, "ok", now); err != nil {
				t.Fatal(err)
			}
		}
		if s := turn(t, tb); s != 0 {
			p, _ := tb.Occupant(s)
			if err := tb.Move(p, "ok", now); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(r.forfeits, []Seat{0}) || len(r.timeouts) != MissedTurnLimit {
		t.Errorf("forfeits = %v after %d timeouts; want [0] after %d", r.forfeits, len(r.timeouts), MissedTurnLimit)
	}
}
