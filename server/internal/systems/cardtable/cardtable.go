// Package cardtable is the shared card table system: seats, turn order, the
// turn timer and per-seat hidden views. Each card game plugs in as a Rules
// module. It is pure game logic: no I/O and no wall-clock time; the caller
// passes the current world tick.
package cardtable

import (
	"errors"
	"fmt"

	"github.com/cleiderg/lush/server/internal/world"
)

// Seat is a seat index at a table, from 0 to Seats()-1.
type Seat int

// Errors returned by Table methods. A rejected call leaves the table unchanged.
var (
	ErrTableFull     = errors.New("cardtable: table is full")
	ErrAlreadySeated = errors.New("cardtable: player already seated")
	ErrNotSeated     = errors.New("cardtable: player not seated")
	ErrInProgress    = errors.New("cardtable: game in progress")
	ErrNoGame        = errors.New("cardtable: no game in progress")
	ErrNotYourTurn   = errors.New("cardtable: not this seat's turn")
)

// MissedTurnLimit is how many missed turns in a row (timeouts, including
// those run straight away for a disconnected seat) make a seat forfeit. It is
// the placeholder from docs/specs/card-tables.md, to be tuned after playtests.
const MissedTurnLimit = 3

// Outcome is what a rules module reports after a move or timeout, telling the
// table how to pass the turn.
type Outcome struct {
	// Reverse flips the turn direction before the turn passes.
	Reverse bool
	// Skip is how many seats are passed over in addition to the normal
	// advance of one seat.
	Skip int
}

// Rules is one card game, plugged into a Table.
type Rules interface {
	// MinPlayers is the fewest seated players needed to start a game.
	MinPlayers() int
	// Start begins a game for the given seats, in seat order. The first of
	// them takes the first turn.
	Start(seats []Seat) error
	// Move applies a move from the seat whose turn it is. On error the move
	// is rejected and the rules module must leave its state unchanged.
	Move(seat Seat, move any) (Outcome, error)
	// Timeout handles the current seat running out of time, or having its
	// turn while disconnected.
	Timeout(seat Seat) Outcome
	// Forfeit removes a seat from the game, after 3 missed turns in a row or
	// when its player leaves mid-game. The table skips the seat in turn order
	// from then on. If only one seat is left in the game and Over does not
	// report a winner, the table makes that seat the winner.
	Forfeit(seat Seat)
	// PublicView is the table state every viewer may see.
	PublicView() any
	// PrivateView is the data only the given seat may see (for example its
	// hand).
	PrivateView(seat Seat) any
	// Over reports whether the game has ended and, if so, which seat won.
	Over() (winner Seat, over bool)
}

// View is what one viewer may see of a table.
type View struct {
	// Seated is whether the viewer has a seat; Seat and Private are only set
	// when it is true.
	Seated  bool
	Seat    Seat
	Public  any
	Private any
}

type seatState struct {
	player       world.PlayerID
	occupied     bool
	ready        bool
	disconnected bool
	missed       int // missed turns in a row in the current game
}

// Table is one card table. Only the world tick may call its methods.
type Table struct {
	rules       Rules
	turnTimeout uint64
	seats       []seatState

	inProgress bool
	order      []Seat // seats in the current game, in seat order
	turn       int    // index into order of the current seat
	dir        int    // +1 or -1
	deadline   uint64 // tick at which the current seat times out

	hasResult bool
	winner    Seat
}

// New builds a table with the given seat count and turn timeout in world
// ticks. Both come from configuration (PRD 13 Q1); there are no defaults.
func New(seats int, turnTimeout uint64, rules Rules) (*Table, error) {
	if seats < 1 {
		return nil, fmt.Errorf("cardtable: seat count %d must be at least 1", seats)
	}
	if turnTimeout == 0 {
		return nil, errors.New("cardtable: turn timeout must be at least 1 tick")
	}
	if rules == nil {
		return nil, errors.New("cardtable: rules module is nil")
	}
	return &Table{rules: rules, turnTimeout: turnTimeout, seats: make([]seatState, seats)}, nil
}

// Seats is the table's seat count.
func (t *Table) Seats() int { return len(t.seats) }

// InProgress reports whether a game is being played.
func (t *Table) InProgress() bool { return t.inProgress }

// SeatOf returns the seat a player is sitting in.
func (t *Table) SeatOf(p world.PlayerID) (Seat, bool) {
	for i, s := range t.seats {
		if s.occupied && s.player == p {
			return Seat(i), true
		}
	}
	return 0, false
}

// Occupant returns the player in a seat.
func (t *Table) Occupant(s Seat) (world.PlayerID, bool) {
	if s < 0 || int(s) >= len(t.seats) || !t.seats[s].occupied {
		return 0, false
	}
	return t.seats[s].player, true
}

// Ready reports whether the player in a seat has pressed Ready.
func (t *Table) Ready(s Seat) bool {
	return s >= 0 && int(s) < len(t.seats) && t.seats[s].ready
}

// Missed returns how many turns in a row a seat has missed in the current
// game.
func (t *Table) Missed(s Seat) int {
	if s < 0 || int(s) >= len(t.seats) {
		return 0
	}
	return t.seats[s].missed
}

// Disconnected reports whether a seat is marked disconnected.
func (t *Table) Disconnected(s Seat) bool {
	return s >= 0 && int(s) < len(t.seats) && t.seats[s].disconnected
}

// Sit puts a player in the lowest free seat. It is rejected while a game is
// in progress.
func (t *Table) Sit(p world.PlayerID) (Seat, error) {
	if t.inProgress {
		return 0, ErrInProgress
	}
	if _, ok := t.SeatOf(p); ok {
		return 0, ErrAlreadySeated
	}
	for i := range t.seats {
		if !t.seats[i].occupied {
			t.seats[i] = seatState{player: p, occupied: true}
			return Seat(i), nil
		}
	}
	return 0, ErrTableFull
}

// Leave frees a player's seat at tick now. Mid-game, the seat forfeits.
// Between games, every seated player's Ready flag clears, since the group
// they agreed to play with has changed.
func (t *Table) Leave(p world.PlayerID, now uint64) error {
	s, ok := t.SeatOf(p)
	if !ok {
		return ErrNotSeated
	}
	if t.inGame(s) {
		t.forfeit(s, now)
		return nil
	}
	t.seats[s] = seatState{}
	if !t.inProgress {
		for i := range t.seats {
			t.seats[i].ready = false
		}
	}
	return nil
}

// SetReady sets or clears player p's Ready flag at tick now. Once at least
// MinPlayers are seated and every seated player is ready, the game starts
// straight away and every Ready flag clears. It is rejected while a game is
// in progress. A start the rules module rejects is returned as an error with
// the flag left set.
func (t *Table) SetReady(p world.PlayerID, ready bool, now uint64) error {
	if t.inProgress {
		return ErrInProgress
	}
	s, ok := t.SeatOf(p)
	if !ok {
		return ErrNotSeated
	}
	t.seats[s].ready = ready
	return t.tryStart(now)
}

// SetDisconnected marks or clears player p's seat as disconnected. While
// marked, the seat's turn runs the rules module's timeout action at the next
// Tick instead of waiting for the deadline.
func (t *Table) SetDisconnected(p world.PlayerID, disconnected bool) error {
	s, ok := t.SeatOf(p)
	if !ok {
		return ErrNotSeated
	}
	t.seats[s].disconnected = disconnected
	return nil
}

// tryStart begins a game at tick now if enough players are seated and all of
// them are ready.
func (t *Table) tryStart(now uint64) error {
	var order []Seat
	for i, s := range t.seats {
		if !s.occupied {
			continue
		}
		if !s.ready {
			return nil
		}
		order = append(order, Seat(i))
	}
	if len(order) < t.rules.MinPlayers() || len(order) < 2 {
		return nil
	}
	if err := t.rules.Start(append([]Seat(nil), order...)); err != nil {
		return fmt.Errorf("cardtable: start game: %w", err)
	}
	for _, s := range order {
		t.seats[s].ready = false
		t.seats[s].missed = 0
	}
	t.inProgress = true
	t.order = order
	t.turn = 0
	t.dir = 1
	t.deadline = now + t.turnTimeout
	t.hasResult = false
	t.winner = 0
	return nil
}

// inGame reports whether seat s is playing in the current game.
func (t *Table) inGame(s Seat) bool {
	for _, o := range t.order {
		if o == s {
			return true
		}
	}
	return false
}

// Turn returns the seat whose turn it is.
func (t *Table) Turn() (Seat, bool) {
	if !t.inProgress {
		return 0, false
	}
	return t.order[t.turn], true
}

// Reversed reports whether turns currently pass in descending seat order.
func (t *Table) Reversed() bool { return t.inProgress && t.dir < 0 }

// Deadline returns the tick at which the current seat times out.
func (t *Table) Deadline() (uint64, bool) {
	if !t.inProgress {
		return 0, false
	}
	return t.deadline, true
}

// Winner returns the winning seat of the last finished game, until the next
// game starts.
func (t *Table) Winner() (Seat, bool) { return t.winner, t.hasResult }

// Move applies a move from player p at tick now.
func (t *Table) Move(p world.PlayerID, move any, now uint64) error {
	if !t.inProgress {
		return ErrNoGame
	}
	s, ok := t.SeatOf(p)
	if !ok {
		return ErrNotSeated
	}
	if s != t.order[t.turn] {
		return ErrNotYourTurn
	}
	out, err := t.rules.Move(s, move)
	if err != nil {
		return fmt.Errorf("cardtable: move from seat %d: %w", s, err)
	}
	t.seats[s].missed = 0
	t.pass(out, now)
	return nil
}

// Tick runs the turn timer at tick now: once the current seat's deadline has
// passed, or straight away if the seat is disconnected, the rules module's
// timeout handler runs for it and the turn passes. That counts as a missed
// turn; at MissedTurnLimit in a row the seat forfeits.
func (t *Table) Tick(now uint64) {
	if !t.inProgress {
		return
	}
	s := t.order[t.turn]
	if now < t.deadline && !t.seats[s].disconnected {
		return
	}
	out := t.rules.Timeout(s)
	t.seats[s].missed++
	t.pass(out, now)
	if t.inProgress && t.seats[s].missed >= MissedTurnLimit {
		t.forfeit(s, now)
	}
}

// forfeit removes seat s from the current game at tick now and frees it. If
// s had the turn, the turn passes to the next seat in the current direction
// with a fresh deadline. The game ends if the rules module says so, or with
// the last seat left as the winner.
func (t *Table) forfeit(s Seat, now uint64) {
	t.rules.Forfeit(s)
	t.seats[s] = seatState{}
	i := 0
	for t.order[i] != s {
		i++
	}
	t.order = append(t.order[:i], t.order[i+1:]...)
	if w, over := t.rules.Over(); over {
		t.end(w)
		return
	}
	if len(t.order) == 1 {
		t.end(t.order[0])
		return
	}
	n := len(t.order)
	switch {
	case i < t.turn:
		t.turn--
	case i == t.turn:
		// The next seat forward has slid into index turn; backward it is
		// the one before.
		if t.dir < 0 {
			t.turn--
		}
		t.turn = (t.turn + n) % n
		t.deadline = now + t.turnTimeout
	}
}

// end finishes the current game with seat w as the winner.
func (t *Table) end(w Seat) {
	t.inProgress = false
	t.order = nil
	t.hasResult = true
	t.winner = w
}

// pass ends the game if the rules module says it is over, or else passes the
// turn according to out and restarts the deadline.
func (t *Table) pass(out Outcome, now uint64) {
	if w, over := t.rules.Over(); over {
		t.end(w)
		return
	}
	if out.Reverse {
		t.dir = -t.dir
	}
	n := len(t.order)
	step := (1 + max(out.Skip, 0)) % n
	t.turn = ((t.turn+t.dir*step)%n + n) % n
	t.deadline = now + t.turnTimeout
}

// View returns what player p may see: the public state, plus p's own private
// data if p is seated in the current game. Another seat's private data is
// never included.
func (t *Table) View(p world.PlayerID) View {
	v := View{Public: t.rules.PublicView()}
	s, ok := t.SeatOf(p)
	if !ok {
		return v
	}
	v.Seated, v.Seat = true, s
	if t.inProgress {
		v.Private = t.rules.PrivateView(s)
	}
	return v
}
