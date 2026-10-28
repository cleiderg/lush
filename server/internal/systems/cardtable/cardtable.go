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
	ErrTooFewPlayers = errors.New("cardtable: too few players to start")
	ErrNotYourTurn   = errors.New("cardtable: not this seat's turn")
)

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
	// Timeout handles the current seat running out of time.
	Timeout(seat Seat) Outcome
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
	player   world.PlayerID
	occupied bool
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

// Leave frees a player's seat. It is rejected while a game is in progress
// (mid-game leave behaviour waits on PRD 13 Q1).
func (t *Table) Leave(p world.PlayerID) error {
	if t.inProgress {
		return ErrInProgress
	}
	s, ok := t.SeatOf(p)
	if !ok {
		return ErrNotSeated
	}
	t.seats[s] = seatState{}
	return nil
}

// Start begins a game for every seated player at tick now.
func (t *Table) Start(now uint64) error {
	if t.inProgress {
		return ErrInProgress
	}
	var order []Seat
	for i, s := range t.seats {
		if s.occupied {
			order = append(order, Seat(i))
		}
	}
	if len(order) < t.rules.MinPlayers() || len(order) == 0 {
		return ErrTooFewPlayers
	}
	if err := t.rules.Start(append([]Seat(nil), order...)); err != nil {
		return fmt.Errorf("cardtable: start game: %w", err)
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
	t.pass(out, now)
	return nil
}

// Tick runs the turn timer at tick now: once the current seat's deadline has
// passed, the rules module's timeout handler runs for it and the turn passes.
func (t *Table) Tick(now uint64) {
	if !t.inProgress || now < t.deadline {
		return
	}
	out := t.rules.Timeout(t.order[t.turn])
	t.pass(out, now)
}

// pass ends the game if the rules module says it is over, or else passes the
// turn according to out and restarts the deadline.
func (t *Table) pass(out Outcome, now uint64) {
	if w, over := t.rules.Over(); over {
		t.inProgress = false
		t.order = nil
		t.hasResult = true
		t.winner = w
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
