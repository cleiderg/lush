package cardroom

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/systems/cardtable/shed"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// room is the card room built from the shipped game data, registered in a
// world whose players 1 to 9 exist, each carrying roomGold, so commands can go
// through the queue. Its committer saves every request straight away unless
// commit.auto is cleared.
type room struct {
	sys    *System
	sink   *fakeSink
	commit *fakeCommitter
	world  *world.World
	// lastTurn is the player whose turn it was before the latest command
	// under test.
	lastTurn world.PlayerID
}

func newRoom(t *testing.T) *room {
	t.Helper()
	data, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	r := &room{sink: &fakeSink{}, commit: &fakeCommitter{auto: true}, world: world.New(world.DefaultConfig())}
	r.sys, err = FromData(data, r.world.TickInterval(), r.sink, r.commit)
	if err != nil {
		t.Fatalf("FromData: %v", err)
	}
	r.commit.sys = r.sys
	if err := r.world.Register(r.sys); err != nil {
		t.Fatal(err)
	}
	for p := world.PlayerID(1); p <= 9; p++ {
		inv := world.NewInventory(data)
		inv.Gold = roomGold
		if err := r.world.AddPlayer(&world.Player{ID: p, Inv: inv}); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// roomGold is the gold each player in a room starts with.
const roomGold = 1000

// run enqueues cmds and runs one world tick. A player who sits is first
// moved to the table's centre, so the sit is in reach.
func (r *room) run(cmds ...world.Command) {
	for _, c := range cmds {
		if sit, ok := c.(SitCommand); ok {
			if pos, ok := r.sys.Position(sit.TableID); ok {
				r.world.Player(sit.Player).Pos = pos
			}
		}
		r.world.Enqueue(c)
	}
	r.world.Tick()
}

func (r *room) table(t *testing.T, id TableID) *cardtable.Table {
	t.Helper()
	tb, ok := r.sys.Table(id)
	if !ok {
		t.Fatalf("no table %d", id)
	}
	return tb
}

func (r *room) lastChange(t *testing.T) change {
	t.Helper()
	if len(r.sink.changes) == 0 {
		t.Fatal("no table change published")
	}
	return r.sink.changes[len(r.sink.changes)-1]
}

// start seats players at table 1, readies them all on one tick and runs
// one more tick, in which the saved stakes start the game.
func (r *room) start(t *testing.T, players ...world.PlayerID) {
	t.Helper()
	var cmds []world.Command
	for _, p := range players {
		cmds = append(cmds, SitCommand{Player: p, TableID: 1})
	}
	r.run(cmds...)
	cmds = nil
	for _, p := range players {
		cmds = append(cmds, ReadyCommand{Player: p, Ready: true})
	}
	r.run(cmds...)
	r.run()
	if !r.table(t, 1).InProgress() {
		t.Fatal("game did not start")
	}
	r.sink.reset()
}

func hand(t *testing.T, tb *cardtable.Table, p world.PlayerID) []shed.Card {
	t.Helper()
	h, ok := tb.View(p).Private.([]shed.Card)
	if !ok {
		t.Fatalf("player %d has no hand", p)
	}
	return h
}

func public(t *testing.T, v cardtable.View) shed.PublicView {
	t.Helper()
	pub, ok := v.Public.(shed.PublicView)
	if !ok {
		t.Fatalf("public view %T, want shed.PublicView", v.Public)
	}
	return pub
}

// current returns the player whose turn it is at table 1.
func current(t *testing.T, tb *cardtable.Table) world.PlayerID {
	t.Helper()
	s, ok := tb.Turn()
	if !ok {
		t.Fatal("no game in progress")
	}
	p, _ := tb.Occupant(s)
	return p
}

// drawUntil has each player in turn draw until the player whose turn it is
// holds a card for which ok returns true, and returns that player and card.
func (r *room) drawUntil(t *testing.T, ok func(pub shed.PublicView, c shed.Card) bool) (world.PlayerID, shed.Card) {
	t.Helper()
	tb := r.table(t, 1)
	for range 200 {
		p := current(t, tb)
		pub := public(t, tb.View(p))
		for _, c := range hand(t, tb, p) {
			if ok(pub, c) {
				r.sink.reset()
				return p, c
			}
		}
		r.run(DrawCommand{Player: p})
	}
	t.Fatal("no suitable card after 200 draws")
	return 0, shed.Card{}
}

func playable(pub shed.PublicView, c shed.Card) bool {
	return c.IsWild() || c.Colour == pub.Colour || c.Symbol == pub.Top.Symbol
}

func TestFromDataBuildsShedTables(t *testing.T) {
	r := newRoom(t)
	for id := TableID(1); id <= 4; id++ {
		tb := r.table(t, id)
		if tb.Seats() != 4 {
			t.Errorf("table %d has %d seats, want 4", id, tb.Seats())
		}
	}
	if _, ok := r.sys.Table(5); ok {
		t.Error("table 5 exists, want only 1 to 4")
	}
	r.start(t, 1, 2)
	tb := r.table(t, 1)
	public(t, tb.View(1)) // Shed is the rules module
	// 20 s at 20 Hz is 400 ticks.
	d, _ := tb.Deadline()
	if want := r.world.TickCount() + 400; d != want {
		t.Errorf("deadline %d, want %d (a 400-tick turn timer)", d, want)
	}
}

func TestFromDataRejectsBadInput(t *testing.T) {
	good, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		data     *gamedata.Data
		interval time.Duration
		sink     Sink
		wantErr  string
	}{
		{"nil committer", good, time.Millisecond, &fakeSink{}, "committer is nil"},
		{"nil data", nil, time.Millisecond, &fakeSink{}, "game data is nil"},
		{"zero interval", good, 0, &fakeSink{}, "tick interval"},
		{"nil sink", good, time.Millisecond, nil, "sink is nil"},
		{"no seats", &gamedata.Data{CardTables: []gamedata.CardTable{{ID: 1, Seats: 0, TurnTimerSeconds: 1}}},
			time.Millisecond, &fakeSink{}, "table 1"},
		{"table without position", &gamedata.Data{CardTables: []gamedata.CardTable{{ID: 9, Seats: 2, TurnTimerSeconds: 1}}},
			time.Millisecond, &fakeSink{}, "table 9 has no position"},
		{"position without table", &gamedata.Data{CardTables: []gamedata.CardTable{{ID: 1, Seats: 2, TurnTimerSeconds: 1}}},
			time.Millisecond, &fakeSink{}, "4 table positions for 1 card tables"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Committer(&fakeCommitter{})
			if tt.wantErr == "committer is nil" {
				c = nil
			}
			_, err := FromData(tt.data, tt.interval, tt.sink, c)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestReadyStartsGame(t *testing.T) {
	tests := []struct {
		name      string
		seated    []world.PlayerID
		ready     []world.PlayerID
		wantStart bool
		wantReady []bool // per seat, in the last snapshot
	}{
		{"one seated and ready", []world.PlayerID{1}, []world.PlayerID{1}, false, []bool{true, false, false, false}},
		{"two seated, one ready", []world.PlayerID{1, 2}, []world.PlayerID{2}, false, []bool{false, true, false, false}},
		{"three seated, two ready", []world.PlayerID{1, 2, 3}, []world.PlayerID{1, 3}, false, []bool{true, false, true, false}},
		{"two seated, both ready", []world.PlayerID{1, 2}, []world.PlayerID{1, 2}, true, []bool{false, false, false, false}},
		{"four seated, all ready", []world.PlayerID{1, 2, 3, 4}, []world.PlayerID{4, 3, 2, 1}, true, []bool{false, false, false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoom(t)
			for _, p := range tt.seated {
				r.run(SitCommand{Player: p, TableID: 1})
			}
			r.sink.reset()
			var cmds []world.Command
			for _, p := range tt.ready {
				cmds = append(cmds, ReadyCommand{Player: p, Ready: true})
			}
			r.run(cmds...)
			r.run() // applies the saved stakes
			tick := r.world.TickCount()
			for _, res := range r.sink.readies {
				if res.r != OK || res.id != 1 {
					t.Errorf("ready result %+v, want OK at table 1", res)
				}
			}
			snap := r.lastChange(t).snap
			if snap.InProgress != tt.wantStart || r.table(t, 1).InProgress() != tt.wantStart {
				t.Fatalf("in progress = %v, want %v", snap.InProgress, tt.wantStart)
			}
			var ready []bool
			for _, s := range snap.Seats {
				ready = append(ready, s.Ready)
			}
			if !reflect.DeepEqual(ready, tt.wantReady) {
				t.Errorf("snapshot ready flags %v, want %v", ready, tt.wantReady)
			}
			if tt.wantStart {
				// Started on the tick the stakes were saved: the full turn
				// timer is left.
				if d, _ := r.table(t, 1).Deadline(); d != tick+400 {
					t.Errorf("deadline %d, want game started on tick %d", d, tick)
				}
			}
		})
	}
}

func TestReadyRejected(t *testing.T) {
	r := newRoom(t)
	r.start(t, 1, 2)
	r.run(ReadyCommand{Player: 9, Ready: true}, ReadyCommand{Player: 1, Ready: true})
	want := []leaveResult{{9, 0, NotSeated}, {1, 1, InProgress}}
	if !reflect.DeepEqual(r.sink.readies, want) || len(r.sink.changes) != 0 {
		t.Errorf("results %v, changes %d; want %v and no changes", r.sink.readies, len(r.sink.changes), want)
	}
}

func TestUnreadyClearsFlag(t *testing.T) {
	r := newRoom(t)
	r.run(SitCommand{Player: 1, TableID: 1}, SitCommand{Player: 2, TableID: 1})
	r.run(ReadyCommand{Player: 1, Ready: true})
	r.run(ReadyCommand{Player: 1, Ready: false}, ReadyCommand{Player: 2, Ready: true})
	snap := r.lastChange(t).snap
	if snap.InProgress || snap.Seats[0].Ready || !snap.Seats[1].Ready {
		t.Errorf("snapshot %+v, want no game, only seat 1 ready", snap)
	}
}

func TestAcceptedDrawPublishesState(t *testing.T) {
	r := newRoom(t)
	r.start(t, 1, 2)
	tb := r.table(t, 1)
	r.run(DrawCommand{Player: 1})
	if want := []leaveResult{{1, 1, OK}}; !reflect.DeepEqual(r.sink.moves, want) {
		t.Fatalf("move results %v, want %v", r.sink.moves, want)
	}
	c := r.lastChange(t)
	if c.id != 1 || c.snap.Turn != 1 || !c.snap.InProgress {
		t.Errorf("change table %d %+v, want table 1 with turn at seat 1", c.id, c.snap)
	}
	pub := public(t, c.views[0].View)
	if pub.HandCounts[0] != shed.HandSize+1 || pub.HandCounts[1] != shed.HandSize {
		t.Errorf("hand counts %v, want seat 0 to have drawn one", pub.HandCounts)
	}
	if got := len(hand(t, tb, 1)); got != shed.HandSize+1 {
		t.Errorf("player 1 holds %d cards, want %d", got, shed.HandSize+1)
	}
}

func TestAcceptedPlayPublishesState(t *testing.T) {
	r := newRoom(t)
	r.start(t, 1, 2)
	p, card := r.drawUntil(t, func(pub shed.PublicView, c shed.Card) bool { return playable(pub, c) })
	before := len(hand(t, r.table(t, 1), p))
	r.run(PlayCommand{Player: p, Card: card, Chosen: shed.Colour2})
	if want := []leaveResult{{p, 1, OK}}; !reflect.DeepEqual(r.sink.moves, want) {
		t.Fatalf("move results %v, want %v", r.sink.moves, want)
	}
	c := r.lastChange(t)
	pub := public(t, c.views[0].View)
	if pub.Top != card {
		t.Errorf("top card %v, want the played %v", pub.Top, card)
	}
	if card.IsWild() && pub.Colour != shed.Colour2 {
		t.Errorf("colour %d after wild, want %d", pub.Colour, shed.Colour2)
	}
	seat, _ := r.table(t, 1).SeatOf(p)
	if pub.HandCounts[seat] != before-1 {
		t.Errorf("hand count %d, want %d", pub.HandCounts[seat], before-1)
	}
}

func TestRejectedMoves(t *testing.T) {
	notInHand := func(t *testing.T, r *room, p world.PlayerID) shed.Card {
		h := hand(t, r.table(t, 1), p)
		for _, c := range shed.FullDeck() {
			if !slices.Contains(h, c) {
				return c
			}
		}
		t.Fatal("hand holds every card")
		return shed.Card{}
	}
	tests := []struct {
		name string
		// cmd builds the command; r has a game between 1 and 2 at table 1,
		// and player 3 alone at table 2.
		cmd  func(t *testing.T, r *room) world.Command
		want func(r *room) leaveResult
	}{
		{"not seated", func(*testing.T, *room) world.Command { return DrawCommand{Player: 9} },
			func(*room) leaveResult { return leaveResult{9, 0, NotSeated} }},
		{"no game in progress", func(*testing.T, *room) world.Command { return DrawCommand{Player: 3} },
			func(*room) leaveResult { return leaveResult{3, 2, NoGame} }},
		{"play with no game in progress", func(*testing.T, *room) world.Command {
			return PlayCommand{Player: 3, Card: shed.Card{Colour: shed.Colour1, Symbol: 5}}
		}, func(*room) leaveResult { return leaveResult{3, 2, NoGame} }},
		{"draw out of turn", func(*testing.T, *room) world.Command { return DrawCommand{Player: 2} },
			func(*room) leaveResult { return leaveResult{2, 1, NotYourTurn} }},
		{"play out of turn", func(t *testing.T, r *room) world.Command {
			return PlayCommand{Player: 2, Card: hand(t, r.table(t, 1), 2)[0]}
		}, func(*room) leaveResult { return leaveResult{2, 1, NotYourTurn} }},
		{"card not in hand", func(t *testing.T, r *room) world.Command {
			return PlayCommand{Player: 1, Card: notInHand(t, r, 1), Chosen: shed.Colour1}
		}, func(*room) leaveResult { return leaveResult{1, 1, NotInHand} }},
		{"card does not match", func(t *testing.T, r *room) world.Command {
			p, c := r.drawUntil(t, func(pub shed.PublicView, c shed.Card) bool { return !playable(pub, c) })
			return PlayCommand{Player: p, Card: c}
		}, func(r *room) leaveResult { return leaveResult{r.lastTurn, 1, NoMatch} }},
		{"wild without a colour", func(t *testing.T, r *room) world.Command {
			p, c := r.drawUntil(t, func(_ shed.PublicView, c shed.Card) bool { return c.IsWild() })
			return PlayCommand{Player: p, Card: c, Chosen: shed.NoColour}
		}, func(r *room) leaveResult { return leaveResult{r.lastTurn, 1, BadColour} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRoom(t)
			r.start(t, 1, 2)
			r.run(SitCommand{Player: 3, TableID: 2})
			cmd := tt.cmd(t, r)
			r.lastTurn = current(t, r.table(t, 1))
			tb := r.table(t, 1)
			turn, _ := tb.Turn()
			h1, h2 := hand(t, tb, 1), hand(t, tb, 2)
			r.sink.reset()
			r.run(cmd)
			if want := []leaveResult{tt.want(r)}; !reflect.DeepEqual(r.sink.moves, want) {
				t.Errorf("move results %v, want %v", r.sink.moves, want)
			}
			if len(r.sink.changes) != 0 {
				t.Errorf("rejected move published %d changes", len(r.sink.changes))
			}
			if now, _ := tb.Turn(); now != turn || !reflect.DeepEqual(hand(t, tb, 1), h1) || !reflect.DeepEqual(hand(t, tb, 2), h2) {
				t.Error("rejected move changed the game")
			}
		})
	}
}

func TestDisconnectMarksSeat(t *testing.T) {
	r := newRoom(t)
	r.start(t, 1, 2)
	tb := r.table(t, 1)
	r.run(DisconnectCommand{Player: 2})
	if !tb.Disconnected(1) {
		t.Fatal("seat 1 not marked disconnected at the table")
	}
	snap := r.lastChange(t).snap
	if !snap.Seats[1].Disconnected || snap.Seats[0].Disconnected {
		t.Errorf("snapshot seats %+v, want only seat 1 disconnected", snap.Seats)
	}

	// Player 1 draws; player 2's turn times out on the next tick.
	r.sink.reset()
	r.run(DrawCommand{Player: 1})
	r.run()
	if turn, _ := tb.Turn(); turn != 0 || tb.Missed(1) != 1 {
		t.Errorf("turn %d, seat 1 missed %d; want the disconnected seat's turn to time out", turn, tb.Missed(1))
	}
	if got := len(hand(t, tb, 2)); got != shed.HandSize+1 {
		t.Errorf("disconnected player holds %d cards, want %d after the timeout draw", got, shed.HandSize+1)
	}

	r.sink.reset()
	r.run(ReconnectCommand{Player: 2})
	if tb.Disconnected(1) || r.lastChange(t).snap.Seats[1].Disconnected {
		t.Error("reconnect did not clear the mark")
	}

	// Unseated players and repeated marks publish nothing.
	r.sink.reset()
	r.run(DisconnectCommand{Player: 9}, ReconnectCommand{Player: 9}, ReconnectCommand{Player: 2})
	if len(r.sink.changes) != 0 {
		t.Errorf("no-op marks published %d changes", len(r.sink.changes))
	}
}

func TestDisconnectedLeaveMidGameForfeits(t *testing.T) {
	r := newRoom(t)
	r.start(t, 1, 2, 3)
	r.run(DisconnectCommand{Player: 2})
	r.sink.reset()
	r.run(LeaveCommand{Player: 2})
	if want := []leaveResult{{2, 1, OK}}; !reflect.DeepEqual(r.sink.leaves, want) {
		t.Fatalf("leave results %v, want %v", r.sink.leaves, want)
	}
	c := r.lastChange(t)
	if !c.snap.InProgress || c.snap.Seats[1].Occupied {
		t.Errorf("snapshot %+v, want game going on with seat 1 freed", c.snap)
	}
	pub := public(t, c.views[0].View)
	if _, ok := pub.HandCounts[1]; ok {
		t.Errorf("hand counts %v still include the forfeited seat", pub.HandCounts)
	}
	if last := c.views[len(c.views)-1]; last.Player != 2 || last.View.Seated || last.View.Private != nil {
		t.Errorf("leaver's view %+v, want unseated with no hand", last)
	}
}

func TestViewsCarryShedStateAndOwnHand(t *testing.T) {
	r := newRoom(t)
	r.start(t, 1, 2, 3)
	r.run(DrawCommand{Player: 1})
	c := r.lastChange(t)
	if len(c.views) != 3 {
		t.Fatalf("got %d views, want 3", len(c.views))
	}
	tb := r.table(t, 1)
	for _, v := range c.views {
		pub := public(t, v.View)
		if !reflect.DeepEqual(pub, public(t, c.views[0].View)) {
			t.Errorf("player %d sees a different public state", v.Player)
		}
		seat, _ := tb.SeatOf(v.Player)
		if !v.View.Seated || v.View.Seat != seat {
			t.Errorf("player %d view seat %d/%v, want %d", v.Player, v.View.Seat, v.View.Seated, seat)
		}
		h, ok := v.View.Private.([]shed.Card)
		if !ok || len(h) != pub.HandCounts[seat] {
			t.Fatalf("player %d private %v, want a hand of %d", v.Player, v.View.Private, pub.HandCounts[seat])
		}
		if !reflect.DeepEqual(h, hand(t, tb, v.Player)) {
			t.Errorf("player %d is sent a hand that is not their own", v.Player)
		}
	}
	if pub := public(t, c.views[0].View); pub.HandCounts[0] != shed.HandSize+1 || len(pub.HandCounts) != 3 {
		t.Errorf("hand counts %v, want 3 seats with seat 0 at %d", pub.HandCounts, shed.HandSize+1)
	}
}

func BenchmarkTickShed(b *testing.B) {
	data, err := gamedata.Default()
	if err != nil {
		b.Fatal(err)
	}
	sys, err := FromData(data, 50*time.Millisecond, &fakeSink{}, &fakeCommitter{})
	if err != nil {
		b.Fatal(err)
	}
	for id := TableID(1); id <= 4; id++ {
		tb, _ := sys.Table(id)
		for p := range 4 {
			if _, err := tb.Sit(world.PlayerID(int(id)*4 + p)); err != nil {
				b.Fatal(err)
			}
		}
		for p := range 4 {
			if err := tb.SetReady(world.PlayerID(int(id)*4+p), true, 0); err != nil {
				b.Fatal(err)
			}
		}
		if err := tb.Start(0); err != nil {
			b.Fatal(err)
		}
	}
	w := world.New(world.DefaultConfig())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sys.Tick(w, 0)
	}
}
