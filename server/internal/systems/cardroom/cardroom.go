// Package cardroom hosts a set of card tables in the world (docs/specs/
// card-tables.md). It applies sit and leave commands on the tick, runs every
// table's turn timer, and reports results and table changes to a Sink, so the
// session only decodes messages and sends what the Sink receives. It also
// applies Ready, Shed play and draw, and disconnect and reconnect commands.
//
// FromData builds the card room's tables from gamedata, each playing Shed;
// New hosts tables the caller builds. This package hard-codes no table
// numbers. It is pure game logic: no I/O and no wall-clock time.
package cardroom

import (
	"errors"
	"fmt"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/systems/cardtable/shed"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// TableID identifies a table within the System.
type TableID uint32

// Result is the outcome of a sit, leave, ready, play or draw command.
type Result uint8

const (
	OK            Result = 0
	UnknownTable  Result = 1
	AlreadySeated Result = 2
	TableFull     Result = 3
	InProgress    Result = 4
	NotSeated     Result = 5
	// NoGame: a play or draw with no game in progress at the table.
	NoGame Result = 6
	// NotYourTurn: a play or draw from a seat whose turn it isn't.
	NotYourTurn Result = 7
	// NotInHand: a play of a card the sender doesn't hold.
	NotInHand Result = 8
	// NoMatch: a play of a card that matches neither the colour to match
	// nor the top card's symbol.
	NoMatch Result = 9
	// BadColour: a Wild or Wild Draw Four played without a valid colour.
	BadColour Result = 10
	// IllegalMove: any other move the rules module rejects.
	IllegalMove Result = 11
	// StartFailed: every seated player is ready but the rules module could
	// not start the game. The sender's Ready flag stays set.
	StartFailed Result = 12
)

var resultNames = [...]string{"ok", "unknown_table", "already_seated", "table_full", "in_progress", "not_seated",
	"no_game", "not_your_turn", "not_in_hand", "no_match", "bad_colour", "illegal_move", "start_failed"}

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

// ReadyCommand sets or clears Player's Ready flag at the table they sit at.
// Once at least 2 players are seated and all of them are ready, the game
// starts on that tick.
type ReadyCommand struct {
	Player world.PlayerID
	Ready  bool
}

func (c ReadyCommand) Target() world.PlayerID { return c.Player }

// PlayCommand plays Card from Player's hand in their Shed game. Chosen is the
// colour to match next when Card is a Wild or Wild Draw Four.
type PlayCommand struct {
	Player world.PlayerID
	Card   shed.Card
	Chosen shed.Colour
}

func (c PlayCommand) Target() world.PlayerID { return c.Player }

// DrawCommand draws one card for Player in their Shed game; the turn passes.
type DrawCommand struct {
	Player world.PlayerID
}

func (c DrawCommand) Target() world.PlayerID { return c.Player }

// DisconnectCommand marks Player's seat as disconnected: their turns run the
// timeout action straight away until a ReconnectCommand clears it. It does
// nothing if Player isn't seated.
type DisconnectCommand struct {
	Player world.PlayerID
}

func (c DisconnectCommand) Target() world.PlayerID { return c.Player }

// ReconnectCommand clears Player's disconnected mark.
type ReconnectCommand struct {
	Player world.PlayerID
}

func (c ReconnectCommand) Target() world.PlayerID { return c.Player }

// Entry is one table the caller supplies to New.
type Entry struct {
	ID    TableID
	Table *cardtable.Table
}

// SeatInfo is who sits in one seat, and their flags.
type SeatInfo struct {
	Occupied     bool
	Player       world.PlayerID
	Ready        bool
	Disconnected bool
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
	// ReadyResult is the result of p's ReadyCommand at table id (0 when r
	// is NotSeated).
	ReadyResult(to world.PlayerID, id TableID, r Result)
	// MoveResult is the result of p's PlayCommand or DrawCommand at table
	// id (0 when r is NotSeated).
	MoveResult(to world.PlayerID, id TableID, r Result)
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

// FromData builds a System hosting one table per entry in data.CardTables,
// each playing Shed. tickInterval is the world tick interval; each table's
// turn timer is rounded up to whole ticks.
func FromData(data *gamedata.Data, tickInterval time.Duration, sink Sink) (*System, error) {
	if data == nil {
		return nil, errors.New("cardroom: game data is nil")
	}
	if tickInterval <= 0 {
		return nil, fmt.Errorf("cardroom: tick interval %v must be positive", tickInterval)
	}
	entries := make([]Entry, 0, len(data.CardTables))
	for _, c := range data.CardTables {
		ticks := uint64((c.TurnTimer() + tickInterval - 1) / tickInterval)
		t, err := cardtable.New(c.Seats, ticks, shed.New())
		if err != nil {
			return nil, fmt.Errorf("cardroom: table %d: %w", c.ID, err)
		}
		entries = append(entries, Entry{ID: TableID(c.ID), Table: t})
	}
	return New(entries, sink)
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

// Handle applies a card room command for p.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case SitCommand:
		s.sit(p.ID, c.TableID)
	case LeaveCommand:
		s.leave(p.ID)
	case ReadyCommand:
		s.ready(p.ID, c.Ready)
	case PlayCommand:
		s.move(p.ID, shed.Play{Card: c.Card, Chosen: c.Chosen})
	case DrawCommand:
		s.move(p.ID, shed.Draw{})
	case DisconnectCommand:
		s.setDisconnected(p.ID, true)
	case ReconnectCommand:
		s.setDisconnected(p.ID, false)
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

func (s *System) ready(p world.PlayerID, ready bool) {
	id, seated := s.TableOf(p)
	if !seated {
		s.sink.ReadyResult(p, 0, NotSeated)
		return
	}
	t := s.byID[id]
	seat, _ := t.SeatOf(p)
	before := t.Ready(seat)
	err := t.SetReady(p, ready, s.now)
	switch {
	case errors.Is(err, cardtable.ErrInProgress):
		s.sink.ReadyResult(p, id, InProgress)
		return
	case err != nil:
		// The rules module rejected the start; the flag is still set.
		s.sink.ReadyResult(p, id, StartFailed)
		if t.Ready(seat) != before {
			s.publish(id, t, 0, false)
		}
		return
	}
	s.sink.ReadyResult(p, id, OK)
	s.publish(id, t, 0, false)
}

func (s *System) move(p world.PlayerID, m any) {
	id, seated := s.TableOf(p)
	if !seated {
		s.sink.MoveResult(p, 0, NotSeated)
		return
	}
	t := s.byID[id]
	if err := t.Move(p, m, s.now); err != nil {
		s.sink.MoveResult(p, id, moveResultOf(err))
		return
	}
	s.sink.MoveResult(p, id, OK)
	s.publish(id, t, 0, false)
}

func moveResultOf(err error) Result {
	switch {
	case errors.Is(err, cardtable.ErrNoGame), errors.Is(err, shed.ErrNoGame):
		return NoGame
	case errors.Is(err, cardtable.ErrNotYourTurn):
		return NotYourTurn
	case errors.Is(err, shed.ErrNotInHand):
		return NotInHand
	case errors.Is(err, shed.ErrNoMatch):
		return NoMatch
	case errors.Is(err, shed.ErrBadColour):
		return BadColour
	case errors.Is(err, cardtable.ErrNotSeated):
		return NotSeated
	default:
		return IllegalMove
	}
}

func (s *System) setDisconnected(p world.PlayerID, disconnected bool) {
	id, seated := s.TableOf(p)
	if !seated {
		return
	}
	t := s.byID[id]
	seat, _ := t.SeatOf(p)
	if t.Disconnected(seat) == disconnected {
		return
	}
	if err := t.SetDisconnected(p, disconnected); err != nil {
		return
	}
	s.publish(id, t, 0, false)
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
		seat := cardtable.Seat(i)
		snap.Seats[i].Player, snap.Seats[i].Occupied = t.Occupant(seat)
		snap.Seats[i].Ready = t.Ready(seat)
		snap.Seats[i].Disconnected = t.Disconnected(seat)
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
