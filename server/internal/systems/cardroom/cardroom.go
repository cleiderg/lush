// Package cardroom hosts a set of card tables in the world (docs/specs/
// card-tables.md). It applies sit and leave commands on the tick, runs every
// table's turn timer, and reports results and table changes to a Sink, so the
// session only decodes messages and sends what the Sink receives.
//
// The tables themselves (seat count, turn timeout, rules module) are built by
// the caller; this package hard-codes none of them. It is pure game logic: no
// I/O and no wall-clock time.
package cardroom

import (
	"errors"
	"fmt"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/world"
)

// TableID identifies a table within the System.
type TableID uint32

// Result is the outcome of a sit or leave command.
type Result uint8

const (
	OK            Result = 0
	UnknownTable  Result = 1
	AlreadySeated Result = 2
	TableFull     Result = 3
	InProgress    Result = 4
	NotSeated     Result = 5
)

var resultNames = [...]string{"ok", "unknown_table", "already_seated", "table_full", "in_progress", "not_seated"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return fmt.Sprintf("Result(%d)", uint8(r))
}

// SitCommand asks to sit Player at table TableID.
type SitCommand struct {
	Player  world.PlayerID
	TableID TableID
}

func (c SitCommand) Target() world.PlayerID { return c.Player }

// LeaveCommand asks to free Player's seat, at whichever table it is.
type LeaveCommand struct {
	Player world.PlayerID
}

func (c LeaveCommand) Target() world.PlayerID { return c.Player }

// Entry is one table the caller supplies to New.
type Entry struct {
	ID    TableID
	Table *cardtable.Table
}

// SeatInfo is who sits in one seat.
type SeatInfo struct {
	Occupied bool
	Player   world.PlayerID
}

// Snapshot is a table's public state.
type Snapshot struct {
	// Seats has one entry per seat, in seat order.
	Seats      []SeatInfo
	InProgress bool
	// Turn is the seat whose turn it is; only meaningful while InProgress.
	Turn     cardtable.Seat
	Reversed bool
	// TicksLeft is the number of ticks until the current seat times out;
	// zero when no game is in progress.
	TicksLeft uint64
	// Winner is the winning seat of the last finished game, when HasWinner.
	HasWinner bool
	Winner    cardtable.Seat
}

// PlayerView is the view one player is sent.
type PlayerView struct {
	Player world.PlayerID
	View   cardtable.View
}

// Sink receives everything players must be told. It is called on the tick
// goroutine and must not block.
type Sink interface {
	// SitResult is the result of p's SitCommand for table id. seat is only
	// meaningful when r is OK.
	SitResult(to world.PlayerID, id TableID, seat cardtable.Seat, r Result)
	// LeaveResult is the result of p's LeaveCommand. id is the table left,
	// only meaningful when r is OK. Leaving mid-game forfeits the seat.
	LeaveResult(to world.PlayerID, id TableID, r Result)
	// TableChanged reports a change to table id: its public snapshot and a
	// view for each seated player, in seat order, followed by the player who
	// just left, if any.
	TableChanged(id TableID, snap Snapshot, views []PlayerView)
}

type hosted struct {
	id    TableID
	table *cardtable.Table
}

// System hosts card tables. Only the world tick may call its methods.
type System struct {
	tables []hosted // in the order supplied, for deterministic iteration
	byID   map[TableID]*cardtable.Table
	sink   Sink
	now    uint64 // tick of the latest Tick call
}

// New builds a System hosting the given tables. Table ids must be unique.
func New(tables []Entry, sink Sink) (*System, error) {
	if sink == nil {
		return nil, errors.New("cardroom: sink is nil")
	}
	s := &System{byID: make(map[TableID]*cardtable.Table, len(tables)), sink: sink}
	for _, e := range tables {
		if e.Table == nil {
			return nil, fmt.Errorf("cardroom: table %d is nil", e.ID)
		}
		if _, dup := s.byID[e.ID]; dup {
			return nil, fmt.Errorf("cardroom: duplicate table id %d", e.ID)
		}
		s.byID[e.ID] = e.Table
		s.tables = append(s.tables, hosted{id: e.ID, table: e.Table})
	}
	return s, nil
}

func (s *System) Name() string { return "cardroom" }

// Table returns the table with the given id.
func (s *System) Table(id TableID) (*cardtable.Table, bool) {
	t, ok := s.byID[id]
	return t, ok
}

// TableOf returns the table a player is seated at.
func (s *System) TableOf(p world.PlayerID) (TableID, bool) {
	for _, h := range s.tables {
		if _, ok := h.table.SeatOf(p); ok {
			return h.id, true
		}
	}
	return 0, false
}

// Handle applies a sit or leave command for p.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case SitCommand:
		s.sit(p.ID, c.TableID)
	case LeaveCommand:
		s.leave(p.ID)
	}
}

func (s *System) sit(p world.PlayerID, id TableID) {
	t, ok := s.byID[id]
	if !ok {
		s.sink.SitResult(p, id, 0, UnknownTable)
		return
	}
	if _, seated := s.TableOf(p); seated {
		s.sink.SitResult(p, id, 0, AlreadySeated)
		return
	}
	seat, err := t.Sit(p)
	if err != nil {
		s.sink.SitResult(p, id, 0, resultOf(err))
		return
	}
	s.sink.SitResult(p, id, seat, OK)
	s.publish(id, t, 0, false)
}

func (s *System) leave(p world.PlayerID) {
	id, seated := s.TableOf(p)
	if !seated {
		s.sink.LeaveResult(p, 0, NotSeated)
		return
	}
	t := s.byID[id]
	if err := t.Leave(p, s.now); err != nil {
		s.sink.LeaveResult(p, id, resultOf(err))
		return
	}
	s.sink.LeaveResult(p, id, OK)
	s.publish(id, t, p, true)
}

func resultOf(err error) Result {
	switch {
	case errors.Is(err, cardtable.ErrTableFull):
		return TableFull
	case errors.Is(err, cardtable.ErrAlreadySeated):
		return AlreadySeated
	case errors.Is(err, cardtable.ErrInProgress):
		return InProgress
	default:
		return NotSeated
	}
}

// timerState is what Table.Tick can change.
type timerState struct {
	inProgress bool
	turn       cardtable.Seat
	reversed   bool
	deadline   uint64
	hasWinner  bool
	winner     cardtable.Seat
}

func stateOf(t *cardtable.Table) timerState {
	var st timerState
	st.inProgress = t.InProgress()
	st.turn, _ = t.Turn()
	st.reversed = t.Reversed()
	st.deadline, _ = t.Deadline()
	st.winner, st.hasWinner = t.Winner()
	return st
}

// Tick runs every table's turn timer at w.TickCount() and reports each table
// whose turn passed or whose game ended.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.now = w.TickCount()
	for _, h := range s.tables {
		before := stateOf(h.table)
		h.table.Tick(s.now)
		if stateOf(h.table) != before {
			s.publish(h.id, h.table, 0, false)
		}
	}
}

// snapshot returns table t's public state at the latest tick.
func (s *System) snapshot(t *cardtable.Table) Snapshot {
	snap := Snapshot{
		Seats:      make([]SeatInfo, t.Seats()),
		InProgress: t.InProgress(),
		Reversed:   t.Reversed(),
	}
	for i := range snap.Seats {
		snap.Seats[i].Player, snap.Seats[i].Occupied = t.Occupant(cardtable.Seat(i))
	}
	snap.Turn, _ = t.Turn()
	if d, ok := t.Deadline(); ok && d > s.now {
		snap.TicksLeft = d - s.now
	}
	snap.Winner, snap.HasWinner = t.Winner()
	return snap
}

// publish sends table id's snapshot and per-player views to the sink. When
// hasLeaver is set, leaver (who just left) gets a view too.
func (s *System) publish(id TableID, t *cardtable.Table, leaver world.PlayerID, hasLeaver bool) {
	snap := s.snapshot(t)
	views := make([]PlayerView, 0, len(snap.Seats)+1)
	for _, seat := range snap.Seats {
		if seat.Occupied {
			views = append(views, PlayerView{Player: seat.Player, View: t.View(seat.Player)})
		}
	}
	if hasLeaver {
		views = append(views, PlayerView{Player: leaver, View: t.View(leaver)})
	}
	s.sink.TableChanged(id, snap, views)
}
