package cardroom

import (
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/world"
)

// stubRules is a minimal rules module: timeouts pass the turn by one seat and
// the game ends when over is set.
type stubRules struct {
	timeouts []cardtable.Seat
	over     bool
	winner   cardtable.Seat
}

func (r *stubRules) MinPlayers() int                  { return 2 }
func (r *stubRules) Start([]cardtable.Seat) error     { r.over = false; return nil }
func (r *stubRules) PublicView() any                  { return "public" }
func (r *stubRules) PrivateView(s cardtable.Seat) any { return fmt.Sprintf("hand-%d", s) }
func (r *stubRules) Over() (cardtable.Seat, bool)     { return r.winner, r.over }
func (r *stubRules) Move(cardtable.Seat, any) (cardtable.Outcome, error) {
	return cardtable.Outcome{}, nil
}
func (r *stubRules) Forfeit(cardtable.Seat) {}
func (r *stubRules) Timeout(s cardtable.Seat) cardtable.Outcome {
	r.timeouts = append(r.timeouts, s)
	return cardtable.Outcome{}
}

type sitResult struct {
	to   world.PlayerID
	id   TableID
	seat cardtable.Seat
	r    Result
}

type leaveResult struct {
	to world.PlayerID
	id TableID
	r  Result
}

type change struct {
	id    TableID
	snap  Snapshot
	views []PlayerView
}

type fakeSink struct {
	sits    []sitResult
	leaves  []leaveResult
	changes []change
	readies []leaveResult
	moves   []leaveResult
	stakes  []leaveResult
}

func (f *fakeSink) SitResult(to world.PlayerID, id TableID, seat cardtable.Seat, r Result) {
	f.sits = append(f.sits, sitResult{to, id, seat, r})
}
func (f *fakeSink) LeaveResult(to world.PlayerID, id TableID, r Result) {
	f.leaves = append(f.leaves, leaveResult{to, id, r})
}
func (f *fakeSink) TableChanged(id TableID, snap Snapshot, views []PlayerView) {
	f.changes = append(f.changes, change{id, snap, views})
}

func (f *fakeSink) ReadyResult(to world.PlayerID, id TableID, r Result) {
	f.readies = append(f.readies, leaveResult{to, id, r})
}
func (f *fakeSink) StakeResult(to world.PlayerID, id TableID, r Result) {
	f.stakes = append(f.stakes, leaveResult{to, id, r})
}

func (f *fakeSink) MoveResult(to world.PlayerID, id TableID, r Result) {
	f.moves = append(f.moves, leaveResult{to, id, r})
}

func (f *fakeSink) reset() {
	f.sits, f.leaves, f.changes, f.readies, f.moves, f.stakes = nil, nil, nil, nil, nil, nil
}

// fakeCommitter records every request. With auto set it reports each one
// saved straight away, so it is applied on the next Tick.
type fakeCommitter struct {
	sys  *System
	auto bool
	reqs []Request
}

func (c *fakeCommitter) CommitCardRoom(req Request) {
	c.reqs = append(c.reqs, req)
	if c.auto {
		c.sys.Resolve(req.ID, true)
	}
}

type spec struct {
	id      TableID
	seats   int
	timeout uint64
}

type fixture struct {
	sys    *System
	sink   *fakeSink
	world  *world.World
	tables map[TableID]*cardtable.Table
	rules  map[TableID]*stubRules
}

func newFixture(t *testing.T, specs ...spec) *fixture {
	t.Helper()
	f := &fixture{sink: &fakeSink{}, tables: map[TableID]*cardtable.Table{}, rules: map[TableID]*stubRules{}}
	var entries []Entry
	for _, sp := range specs {
		r := &stubRules{}
		tb, err := cardtable.New(sp.seats, sp.timeout, r)
		if err != nil {
			t.Fatal(err)
		}
		f.tables[sp.id], f.rules[sp.id] = tb, r
		entries = append(entries, Entry{ID: sp.id, Table: tb})
	}
	sys, err := New(entries, f.sink, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.sys = sys
	f.world = world.New(world.DefaultConfig())
	if err := f.world.Register(sys); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) do(cmd world.Command) {
	f.sys.Handle(cmd, &world.Player{ID: cmd.Target()})
}

func (f *fixture) sit(t *testing.T, p world.PlayerID, id TableID) {
	t.Helper()
	f.do(SitCommand{Player: p, TableID: id})
	if got := f.sink.sits[len(f.sink.sits)-1]; got.r != OK {
		t.Fatalf("player %d sit at %d: %v", p, id, got.r)
	}
}

// occupants returns every table's occupants, in table then seat order.
func (f *fixture) occupants() map[TableID][]SeatInfo {
	out := map[TableID][]SeatInfo{}
	for id, tb := range f.tables {
		for i := 0; i < tb.Seats(); i++ {
			p, ok := tb.Occupant(cardtable.Seat(i))
			out[id] = append(out[id], SeatInfo{Occupied: ok, Player: p})
		}
	}
	return out
}

func TestNewTwoTablesDifferentSeatCounts(t *testing.T) {
	f := newFixture(t, spec{id: 1, seats: 2, timeout: 5}, spec{id: 7, seats: 4, timeout: 9})
	for _, c := range []struct {
		id    TableID
		seats int
	}{{1, 2}, {7, 4}} {
		tb, ok := f.sys.Table(c.id)
		if !ok || tb.Seats() != c.seats {
			t.Errorf("table %d: ok=%v seats=%d, want %d", c.id, ok, tb.Seats(), c.seats)
		}
	}
	// Fill both tables to capacity: seat count is the table's, not the system's.
	for p := world.PlayerID(1); p <= 2; p++ {
		f.sit(t, p, 1)
	}
	for p := world.PlayerID(3); p <= 6; p++ {
		f.sit(t, p, 7)
	}
	f.do(SitCommand{Player: 9, TableID: 1})
	f.do(SitCommand{Player: 10, TableID: 7})
	for _, r := range f.sink.sits[len(f.sink.sits)-2:] {
		if r.r != TableFull {
			t.Errorf("player %d: got %v, want TableFull", r.to, r.r)
		}
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	tb, err := cardtable.New(2, 1, &stubRules{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		entries   []Entry
		sink      Sink
		committer Committer
	}{
		{"nil sink", []Entry{{1, tb, 0}}, nil, nil},
		{"nil table", []Entry{{1, nil, 0}}, &fakeSink{}, nil},
		{"duplicate id", []Entry{{1, tb, 0}, {1, tb, 0}}, &fakeSink{}, nil},
		{"negative stake", []Entry{{1, tb, -1}}, &fakeSink{}, &fakeCommitter{}},
		{"stake without committer", []Entry{{1, tb, 10}}, &fakeSink{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.entries, tt.sink, tt.committer); err == nil {
				t.Fatal("New succeeded, want error")
			}
		})
	}
}

func TestOneTableAtATime(t *testing.T) {
	f := newFixture(t, spec{id: 1, seats: 2, timeout: 5}, spec{id: 2, seats: 3, timeout: 5})
	f.sit(t, 1, 1)
	f.do(SitCommand{Player: 1, TableID: 2})
	if got, _ := f.sys.TableOf(1); got != 1 {
		t.Fatalf("player 1 at table %d, want 1", got)
	}
	if _, ok := f.tables[2].SeatOf(1); ok {
		t.Fatal("player 1 seated at a second table")
	}
	// After leaving, sitting at the other table works.
	f.do(LeaveCommand{Player: 1})
	f.sit(t, 1, 2)
	if got, _ := f.sys.TableOf(1); got != 2 {
		t.Fatalf("player 1 at table %d, want 2", got)
	}
}

func TestRejectedCommands(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *fixture)
		cmd   world.Command
		want  Result
	}{
		{
			name: "unknown table",
			cmd:  SitCommand{Player: 1, TableID: 99},
			want: UnknownTable,
		},
		{
			name:  "already seated at this table",
			setup: func(t *testing.T, f *fixture) { f.sit(t, 1, 1) },
			cmd:   SitCommand{Player: 1, TableID: 1},
			want:  AlreadySeated,
		},
		{
			name:  "already seated at another table",
			setup: func(t *testing.T, f *fixture) { f.sit(t, 1, 2) },
			cmd:   SitCommand{Player: 1, TableID: 1},
			want:  AlreadySeated,
		},
		{
			name: "table full",
			setup: func(t *testing.T, f *fixture) {
				f.sit(t, 1, 1)
				f.sit(t, 2, 1)
			},
			cmd:  SitCommand{Player: 3, TableID: 1},
			want: TableFull,
		},
		{
			name:  "sit at table in progress",
			setup: startOnTable1,
			cmd:   SitCommand{Player: 3, TableID: 1},
			want:  InProgress,
		},
		{
			name:  "leave while not seated",
			setup: func(t *testing.T, f *fixture) { f.sit(t, 1, 2) },
			cmd:   LeaveCommand{Player: 5},
			want:  NotSeated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, spec{id: 1, seats: 2, timeout: 5}, spec{id: 2, seats: 3, timeout: 5})
			if tt.setup != nil {
				tt.setup(t, f)
			}
			before := f.occupants()
			f.sink.reset()
			f.do(tt.cmd)

			var got Result
			switch tt.cmd.(type) {
			case SitCommand:
				if len(f.sink.sits) != 1 || len(f.sink.leaves) != 0 {
					t.Fatalf("sits=%v leaves=%v, want one sit result", f.sink.sits, f.sink.leaves)
				}
				got = f.sink.sits[0].r
			case LeaveCommand:
				if len(f.sink.leaves) != 1 || len(f.sink.sits) != 0 {
					t.Fatalf("sits=%v leaves=%v, want one leave result", f.sink.sits, f.sink.leaves)
				}
				got = f.sink.leaves[0].r
			}
			if got != tt.want {
				t.Errorf("result %v, want %v", got, tt.want)
			}
			if tt.cmd.Target() != f.sink.lastTo() {
				t.Errorf("result sent to %d, want %d", f.sink.lastTo(), tt.cmd.Target())
			}
			if after := f.occupants(); !reflect.DeepEqual(before, after) {
				t.Errorf("occupants changed: %v -> %v", before, after)
			}
			if len(f.sink.changes) != 0 {
				t.Errorf("rejected command published %d table changes", len(f.sink.changes))
			}
		})
	}
}

func (f *fakeSink) lastTo() world.PlayerID {
	if len(f.sits) > 0 {
		return f.sits[len(f.sits)-1].to
	}
	return f.leaves[len(f.leaves)-1].to
}

// startOnTable1 seats players 1 and 2 at table 1 and starts a game directly
// on the table, by setting both ready, at the world's current tick.
func startOnTable1(t *testing.T, f *fixture) {
	t.Helper()
	f.sit(t, 1, 1)
	f.sit(t, 2, 1)
	for _, p := range []world.PlayerID{1, 2} {
		if err := f.tables[1].SetReady(p, true, f.world.TickCount()); err != nil {
			t.Fatal(err)
		}
	}
	if !f.tables[1].InProgress() {
		t.Fatal("game did not start")
	}
}

func TestLeaveMidGameForfeits(t *testing.T) {
	f := newFixture(t, spec{id: 1, seats: 2, timeout: 5})
	startOnTable1(t, f)
	f.sink.reset()
	f.do(LeaveCommand{Player: 1})
	if want := []leaveResult{{1, 1, OK}}; !reflect.DeepEqual(f.sink.leaves, want) {
		t.Fatalf("leave results %v, want %v", f.sink.leaves, want)
	}
	if len(f.sink.changes) != 1 {
		t.Fatalf("got %d table changes, want 1", len(f.sink.changes))
	}
	snap := f.sink.changes[0].snap
	if snap.InProgress || !snap.HasWinner || snap.Winner != 1 {
		t.Errorf("snapshot %+v, want game over with winner seat 1", snap)
	}
	if !reflect.DeepEqual(snap.Seats, []SeatInfo{{}, {Occupied: true, Player: 2}}) {
		t.Errorf("seats %v, want only player 2 seated", snap.Seats)
	}
}

func TestSitAndLeaveSucceed(t *testing.T) {
	f := newFixture(t, spec{id: 4, seats: 3, timeout: 5})
	f.do(SitCommand{Player: 8, TableID: 4})
	if want := []sitResult{{8, 4, 0, OK}}; !reflect.DeepEqual(f.sink.sits, want) {
		t.Fatalf("sit results %v, want %v", f.sink.sits, want)
	}
	f.do(LeaveCommand{Player: 8})
	if want := []leaveResult{{8, 4, OK}}; !reflect.DeepEqual(f.sink.leaves, want) {
		t.Fatalf("leave results %v, want %v", f.sink.leaves, want)
	}
	if len(f.sink.changes) != 2 {
		t.Fatalf("got %d table changes, want 2", len(f.sink.changes))
	}
	if got := f.sink.changes[0].snap.Seats; !reflect.DeepEqual(got, []SeatInfo{{Occupied: true, Player: 8}, {}, {}}) {
		t.Errorf("after sit seats = %v", got)
	}
	if got := f.sink.changes[1].snap.Seats; !reflect.DeepEqual(got, []SeatInfo{{}, {}, {}}) {
		t.Errorf("after leave seats = %v", got)
	}
}

func TestTurnPassesAfterTimeout(t *testing.T) {
	const timeout = 4
	// Table 2 has a different timeout and no game, to show tables are
	// independent.
	f := newFixture(t, spec{id: 1, seats: 3, timeout: timeout}, spec{id: 2, seats: 2, timeout: 1})
	f.world.Tick() // tick 1
	startOnTable1(t, f)
	f.sink.reset()
	start := f.world.TickCount()

	for f.world.TickCount() < start+timeout-1 {
		f.world.Tick()
		if s, _ := f.tables[1].Turn(); s != 0 {
			t.Fatalf("tick %d: turn passed to seat %d before the timeout", f.world.TickCount(), s)
		}
	}
	if len(f.sink.changes) != 0 {
		t.Fatalf("published %d changes before the timeout", len(f.sink.changes))
	}
	f.world.Tick() // tick start+timeout: deadline reached
	if s, _ := f.tables[1].Turn(); s != 1 {
		t.Fatalf("tick %d: turn at seat %d, want 1", f.world.TickCount(), s)
	}
	if got := f.rules[1].timeouts; !reflect.DeepEqual(got, []cardtable.Seat{0}) {
		t.Errorf("timeouts %v, want [0]", got)
	}
	if len(f.sink.changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(f.sink.changes))
	}
	c := f.sink.changes[0]
	want := Snapshot{
		Seats:      []SeatInfo{{Occupied: true, Player: 1}, {Occupied: true, Player: 2}, {}},
		InProgress: true,
		Turn:       1,
		TicksLeft:  timeout,
	}
	if c.id != 1 || !reflect.DeepEqual(c.snap, want) {
		t.Errorf("change = table %d %+v, want table 1 %+v", c.id, c.snap, want)
	}
}

func TestGameEndPublished(t *testing.T) {
	f := newFixture(t, spec{id: 1, seats: 2, timeout: 2})
	startOnTable1(t, f)
	f.sink.reset()
	f.rules[1].over, f.rules[1].winner = true, 1
	f.world.Tick()
	f.world.Tick()
	if len(f.sink.changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(f.sink.changes))
	}
	snap := f.sink.changes[0].snap
	if snap.InProgress || !snap.HasWinner || snap.Winner != 1 || snap.TicksLeft != 0 {
		t.Errorf("snapshot %+v, want game over with winner seat 1", snap)
	}
}

func TestSeatedViewsArePrivate(t *testing.T) {
	f := newFixture(t, spec{id: 1, seats: 3, timeout: 1})
	startOnTable1(t, f)
	f.sink.reset()
	f.world.Tick() // timeout passes the turn, publishing a change
	if len(f.sink.changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(f.sink.changes))
	}
	views := f.sink.changes[0].views
	want := []PlayerView{
		{1, cardtable.View{Seated: true, Seat: 0, Public: "public", Private: "hand-0"}},
		{2, cardtable.View{Seated: true, Seat: 1, Public: "public", Private: "hand-1"}},
	}
	if !reflect.DeepEqual(views, want) {
		t.Fatalf("views %+v, want %+v", views, want)
	}
	for _, v := range views {
		for s := 0; s < 3; s++ {
			if cardtable.Seat(s) != v.View.Seat && v.View.Private == fmt.Sprintf("hand-%d", s) {
				t.Errorf("player %d sees seat %d's private data", v.Player, s)
			}
		}
	}
}

func TestLeaverGetsUnseatedView(t *testing.T) {
	f := newFixture(t, spec{id: 1, seats: 3, timeout: 1})
	f.sit(t, 1, 1)
	f.sit(t, 2, 1)
	f.sink.reset()
	f.do(LeaveCommand{Player: 1})
	if len(f.sink.changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(f.sink.changes))
	}
	views := f.sink.changes[0].views
	want := []PlayerView{
		{2, cardtable.View{Seated: true, Seat: 1, Public: "public"}},
		{1, cardtable.View{Public: "public"}},
	}
	if !reflect.DeepEqual(views, want) {
		t.Fatalf("views %+v, want %+v", views, want)
	}
}

// TestNoForbiddenImports checks that cardroom does no I/O: it imports no os
// package directly, and depends, directly or transitively, on no network,
// database, transport, session, store, api or protocol package.
func TestNoForbiddenImports(t *testing.T) {
	direct, err := exec.Command("go", "list", "-f", "{{join .Imports \" \"}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, imp := range strings.Fields(string(direct)) {
		if imp == "os" || strings.HasPrefix(imp, "os/") || imp == "io" || strings.HasPrefix(imp, "io/") {
			t.Errorf("cardroom imports %s", imp)
		}
	}
	deps, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(deps)) {
		if forbiddenImport(dep) {
			t.Errorf("cardroom depends on forbidden package %s", dep)
		}
	}
}

func forbiddenImport(path string) bool {
	for _, p := range []string{"net", "database", "crypto/tls"} {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	for _, p := range []string{"transport", "store", "api", "protocol", "session"} {
		if strings.Contains(path, "/internal/"+p) {
			return true
		}
	}
	return false
}

func BenchmarkTick(b *testing.B) {
	var entries []Entry
	for i := 0; i < 50; i++ {
		tb, err := cardtable.New(4, 1<<40, &stubRules{})
		if err != nil {
			b.Fatal(err)
		}
		for p := 0; p < 4; p++ {
			if _, err := tb.Sit(world.PlayerID(i*4 + p + 1)); err != nil {
				b.Fatal(err)
			}
		}
		for p := 0; p < 4; p++ {
			if err := tb.SetReady(world.PlayerID(i*4+p+1), true, 0); err != nil {
				b.Fatal(err)
			}
		}
		entries = append(entries, Entry{ID: TableID(i), Table: tb})
	}
	sys, err := New(entries, &fakeSink{}, nil)
	if err != nil {
		b.Fatal(err)
	}
	w := world.New(world.DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sys.Tick(w, 0)
	}
}
