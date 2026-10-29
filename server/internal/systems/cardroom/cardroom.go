// Package cardroom hosts a set of card tables in the world (docs/specs/
// card-tables.md). It applies sit and leave commands on the tick, runs every
// table's turn timer, and reports results and table changes to a Sink, so the
// session only decodes messages and sends what the Sink receives. It also
// applies Ready, Shed play and draw, and disconnect and reconnect commands.
//
// FromData builds the card room's tables from gamedata, each playing Shed;
// New hosts tables the caller builds. This package hard-codes no table
// numbers. It is pure game logic: no I/O and no wall-clock time.
//
// Stakes follow the trade commit pattern (see package trade). Sitting and
// pressing Ready need at least the table's stake in carried gold. Once every
// seated player at a staked table is ready, the next Tick unseats anyone
// without the stake (NotEnoughGold); otherwise it works out every seated
// player's inventory with the stake taken, locks those inventories and hands
// them to a Committer as one Request. The game starts only once Resolve
// reports the save succeeded. When a staked game ends, the whole pot
// (forfeited stakes included) is paid to the winner the same way, and their
// gold changes in memory only once that save succeeded.
package cardroom

import (
	"errors"
	"fmt"
	"sync"
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
	// NotEnoughGold: the player carries less gold than the table's stake, so
	// they can't sit or press Ready, or were unseated as a game was starting.
	NotEnoughGold Result = 13
	// StakeFailed: saving the stakes failed, so the game did not start and
	// nobody's gold changed. Every Ready flag at the table is cleared.
	StakeFailed Result = 14
)

var resultNames = [...]string{"ok", "unknown_table", "already_seated", "table_full", "in_progress", "not_seated",
	"no_game", "not_your_turn", "not_in_hand", "no_match", "bad_colour", "illegal_move", "start_failed",
	"not_enough_gold", "stake_failed"}

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
// starts on that tick, or, at a staked table, once the stakes are saved.
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
	// Stake is the gold each seated player puts into the pot when a game
	// starts; 0 for a table without stakes.
	Stake int
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
	// StakeResult tells a player at staked table id how a game start went:
	// OK when their stake is in the pot and the game started, StakeFailed
	// when saving the stakes failed, NotEnoughGold when they were unseated
	// for lacking the stake.
	StakeResult(to world.PlayerID, id TableID, r Result)
}

// Economy audit log reasons for card room gold movements.
const (
	ReasonStake  = "card_stake"
	ReasonPot    = "card_pot"
	ReasonRefund = "card_refund"
)

// Kind is what a Request saves.
type Kind uint8

const (
	// KindStake moves every seated player's stake into the pot.
	KindStake Kind = iota
	// KindPayout pays the whole pot to the winner.
	KindPayout
	// KindRefund returns a player's stake when a game could not start after
	// the stakes were saved.
	KindRefund
)

// CommitID identifies a Request in flight.
type CommitID uint64

// Side is one player's part of a Request.
type Side struct {
	Player world.PlayerID
	// Inv is the player's inventory once the gold has moved.
	Inv world.Inventory
	// Changes are the gold changes, for the economy audit log: a negative
	// ChangeGold for a stake, a positive one for a payout or refund.
	Changes []world.Change
}

// Request is a card room gold movement to save as one database transaction.
type Request struct {
	ID    CommitID
	Kind  Kind
	Table TableID
	// Pot is the gold the request moves into (KindStake) or out of the pot.
	Pot   int
	Sides []Side
}

// Committer saves a Request off the tick and then calls System.Resolve with
// req.ID. Every inventory in the request is locked when it is called. It is
// called on the tick goroutine and must not block.
type Committer interface {
	CommitCardRoom(req Request)
}

// credit is gold owed to a player out of a pot, waiting to be committed.
type credit struct {
	kind   Kind
	table  TableID
	player world.PlayerID
	gold   int
}

// inflight is a Request waiting for its result.
type inflight struct {
	req     Request
	players []*world.Player // as they were at commit time, as in req.Sides
	credit  credit          // what to retry if a payout or refund fails
}

type resolution struct {
	id CommitID
	ok bool
}

type hosted struct {
	id    TableID
	table *cardtable.Table
	stake int

	staking CommitID         // the stake request in flight, or 0
	leaving []world.PlayerID // leaves held back while staking
	pot     int              // gold in the pot of the game in progress
}

// System hosts card tables. Only the world tick may call its methods, except
// Resolve.
type System struct {
	tables    []*hosted // in the order supplied, for deterministic iteration
	byID      map[TableID]*hosted
	sink      Sink
	committer Committer
	now       uint64 // tick of the latest Tick call

	nextID  CommitID
	commits map[CommitID]*inflight
	credits []credit // owed, in the order owed

	rmu      sync.Mutex
	resolved []resolution // from Resolve, applied by Tick
}

// New builds a System hosting the given tables. Table ids must be unique and
// stakes not negative; c receives the stake and payout requests and may be
// nil only when no table has a stake.
func New(tables []Entry, sink Sink, c Committer) (*System, error) {
	if sink == nil {
		return nil, errors.New("cardroom: sink is nil")
	}
	s := &System{byID: make(map[TableID]*hosted, len(tables)), sink: sink, committer: c,
		commits: make(map[CommitID]*inflight)}
	for _, e := range tables {
		if e.Table == nil {
			return nil, fmt.Errorf("cardroom: table %d is nil", e.ID)
		}
		if _, dup := s.byID[e.ID]; dup {
			return nil, fmt.Errorf("cardroom: duplicate table id %d", e.ID)
		}
		if e.Stake < 0 {
			return nil, fmt.Errorf("cardroom: table %d: stake %d is negative", e.ID, e.Stake)
		}
		if e.Stake > 0 {
			if c == nil {
				return nil, fmt.Errorf("cardroom: table %d has a stake but the committer is nil", e.ID)
			}
			e.Table.SetManualStart(true)
		}
		h := &hosted{id: e.ID, table: e.Table, stake: e.Stake}
		s.byID[e.ID] = h
		s.tables = append(s.tables, h)
	}
	return s, nil
}

// FromData builds a System hosting one table per entry in data.CardTables,
// each playing Shed with the stake from data. tickInterval is the world tick
// interval; each table's turn timer is rounded up to whole ticks.
func FromData(data *gamedata.Data, tickInterval time.Duration, sink Sink, committer Committer) (*System, error) {
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
		entries = append(entries, Entry{ID: TableID(c.ID), Table: t, Stake: c.Stake})
	}
	return New(entries, sink, committer)
}

func (s *System) Name() string { return "cardroom" }

// Table returns the table with the given id.
func (s *System) Table(id TableID) (*cardtable.Table, bool) {
	h, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	return h.table, true
}

// Stake returns the stake of table id.
func (s *System) Stake(id TableID) (int, bool) {
	h, ok := s.byID[id]
	if !ok {
		return 0, false
	}
	return h.stake, true
}

// Staking reports whether table id's stakes are being saved. Sit and Ready
// are refused with InProgress meanwhile, and leaves wait for the result.
func (s *System) Staking(id TableID) bool {
	h, ok := s.byID[id]
	return ok && h.staking != 0
}

// Pot returns the gold in the pot of table id's game in progress.
func (s *System) Pot(id TableID) int {
	if h, ok := s.byID[id]; ok {
		return h.pot
	}
	return 0
}

// TableOf returns the table a player is seated at.
func (s *System) TableOf(p world.PlayerID) (TableID, bool) {
	if h := s.hostOf(p); h != nil {
		return h.id, true
	}
	return 0, false
}

func (s *System) hostOf(p world.PlayerID) *hosted {
	for _, h := range s.tables {
		if _, ok := h.table.SeatOf(p); ok {
			return h
		}
	}
	return nil
}

// Resolve reports the outcome of request id: ok if the database transaction
// committed. The next Tick applies it. Safe for concurrent use; unknown or
// repeated ids are ignored.
func (s *System) Resolve(id CommitID, ok bool) {
	s.rmu.Lock()
	s.resolved = append(s.resolved, resolution{id, ok})
	s.rmu.Unlock()
}

// Handle applies a card room command for p.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case SitCommand:
		s.sit(p, c.TableID)
	case LeaveCommand:
		s.leave(p.ID)
	case ReadyCommand:
		s.ready(p, c.Ready)
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

func (s *System) sit(pl *world.Player, id TableID) {
	p := pl.ID
	h, ok := s.byID[id]
	if !ok {
		s.sink.SitResult(p, id, 0, UnknownTable)
		return
	}
	t := h.table
	if _, seated := s.TableOf(p); seated {
		s.sink.SitResult(p, id, 0, AlreadySeated)
		return
	}
	if h.staking != 0 {
		s.sink.SitResult(p, id, 0, InProgress)
		return
	}
	if pl.Inv.Gold < h.stake && !t.InProgress() {
		s.sink.SitResult(p, id, 0, NotEnoughGold)
		return
	}
	seat, err := t.Sit(p)
	if err != nil {
		s.sink.SitResult(p, id, 0, resultOf(err))
		return
	}
	s.sink.SitResult(p, id, seat, OK)
	s.publish(h, 0, false)
}

func (s *System) leave(p world.PlayerID) {
	h := s.hostOf(p)
	if h == nil {
		s.sink.LeaveResult(p, 0, NotSeated)
		return
	}
	if h.staking != 0 {
		// Applied once the stakes are saved or not: mid-game it forfeits.
		h.leaving = append(h.leaving, p)
		return
	}
	if err := h.table.Leave(p, s.now); err != nil {
		s.sink.LeaveResult(p, h.id, resultOf(err))
		return
	}
	s.sink.LeaveResult(p, h.id, OK)
	s.ended(h)
	s.publish(h, p, true)
}

func (s *System) ready(pl *world.Player, ready bool) {
	p := pl.ID
	h := s.hostOf(p)
	if h == nil {
		s.sink.ReadyResult(p, 0, NotSeated)
		return
	}
	id, t := h.id, h.table
	if h.staking != 0 {
		s.sink.ReadyResult(p, id, InProgress)
		return
	}
	if ready && pl.Inv.Gold < h.stake && !t.InProgress() {
		s.sink.ReadyResult(p, id, NotEnoughGold)
		return
	}
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
			s.publish(h, 0, false)
		}
		return
	}
	s.sink.ReadyResult(p, id, OK)
	s.publish(h, 0, false)
}

func (s *System) move(p world.PlayerID, m any) {
	h := s.hostOf(p)
	if h == nil {
		s.sink.MoveResult(p, 0, NotSeated)
		return
	}
	if err := h.table.Move(p, m, s.now); err != nil {
		s.sink.MoveResult(p, h.id, moveResultOf(err))
		return
	}
	s.sink.MoveResult(p, h.id, OK)
	s.ended(h)
	s.publish(h, 0, false)
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
	h := s.hostOf(p)
	if h == nil {
		return
	}
	t := h.table
	seat, _ := t.SeatOf(p)
	if t.Disconnected(seat) == disconnected {
		return
	}
	if err := t.SetDisconnected(p, disconnected); err != nil {
		return
	}
	s.publish(h, 0, false)
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

// Tick applies the request results reported since the last Tick, runs every
// table's turn timer at w.TickCount() and reports each table whose turn
// passed or whose game ended, then starts the stake request of every staked
// table whose seated players are all ready, then commits the gold owed out of
// pots.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.now = w.TickCount()

	s.rmu.Lock()
	resolved := s.resolved
	s.resolved = nil
	s.rmu.Unlock()
	for _, r := range resolved {
		s.finish(w, r.id, r.ok)
	}

	for _, h := range s.tables {
		before := stateOf(h.table)
		h.table.Tick(s.now)
		if stateOf(h.table) != before {
			s.ended(h)
			s.publish(h, 0, false)
		}
	}
	for _, h := range s.tables {
		s.stake(w, h)
	}
	s.pay(w)
}

// ended owes table h's pot to the winner if its staked game just ended.
func (s *System) ended(h *hosted) {
	if h.pot == 0 || h.table.InProgress() {
		return
	}
	seat, _ := h.table.Winner()
	winner, _ := h.table.Occupant(seat)
	s.credits = append(s.credits, credit{kind: KindPayout, table: h.id, player: winner, gold: h.pot})
	h.pot = 0
}

// stake starts the stake request of staked table h once every seated player
// is ready. A player who is not in the world or carries less than the stake
// is unseated instead (NotEnoughGold), which clears every Ready flag, and
// nothing is committed. While any seated player's inventory is locked (for
// example by a trade being saved) it waits.
func (s *System) stake(w *world.World, h *hosted) {
	if h.stake == 0 || h.staking != 0 {
		return
	}
	seats, ok := h.table.StartSeats()
	if !ok {
		return
	}
	players := make([]*world.Player, 0, len(seats))
	var short []world.PlayerID
	for _, seat := range seats {
		id, _ := h.table.Occupant(seat)
		p := w.Player(id)
		switch {
		case p == nil, p.Inv.Gold < h.stake:
			short = append(short, id)
		case p.Inv.Locked():
			return
		}
		players = append(players, p)
	}
	for _, id := range short {
		if err := h.table.Leave(id, s.now); err != nil {
			continue
		}
		s.sink.StakeResult(id, h.id, NotEnoughGold)
		s.publish(h, id, true)
	}
	if len(short) > 0 {
		return
	}
	s.nextID++
	req := Request{ID: s.nextID, Kind: KindStake, Table: h.id, Pot: h.stake * len(players)}
	for _, p := range players {
		inv := p.Inv.Clone()
		ch, err := inv.SpendGold(h.stake)
		if err != nil {
			return // can't happen: gold and lock were checked above
		}
		req.Sides = append(req.Sides, Side{Player: p.ID, Inv: inv, Changes: ch})
	}
	h.staking = req.ID
	s.send(&inflight{req: req, players: players})
}

// pay commits each credit owed whose player is in the world with an unlocked
// inventory, as its own request. The rest wait.
func (s *System) pay(w *world.World) {
	kept := s.credits[:0]
	for _, c := range s.credits {
		p := w.Player(c.player)
		if p == nil || p.Inv.Locked() {
			kept = append(kept, c)
			continue
		}
		inv := p.Inv.Clone()
		ch, err := inv.AddGold(c.gold)
		if err != nil {
			kept = append(kept, c)
			continue
		}
		s.nextID++
		req := Request{ID: s.nextID, Kind: c.kind, Table: c.table, Pot: c.gold,
			Sides: []Side{{Player: p.ID, Inv: inv, Changes: ch}}}
		s.send(&inflight{req: req, players: []*world.Player{p}, credit: c})
	}
	clear(s.credits[len(kept):])
	s.credits = kept
}

// send locks every inventory in c and hands a copy of its request to the
// Committer, so nothing the Committer does reaches the inventories applied
// later.
func (s *System) send(c *inflight) {
	for _, p := range c.players {
		p.Inv.SetLocked(true)
	}
	s.commits[c.req.ID] = c
	req := c.req
	req.Sides = make([]Side, len(c.req.Sides))
	for i, side := range c.req.Sides {
		req.Sides[i] = Side{Player: side.Player, Inv: side.Inv.Clone(),
			Changes: append([]world.Change(nil), side.Changes...)}
	}
	s.committer.CommitCardRoom(req)
}

// finish applies the result of request id. Every inventory in it is unlocked;
// on success each player still in the world gets their resulting inventory
// and an economy outbox event. A stake request then starts the game, or on
// failure clears every Ready flag at the table; a failed payout or refund is
// owed again and retried.
func (s *System) finish(w *world.World, id CommitID, ok bool) {
	c := s.commits[id]
	if c == nil {
		return
	}
	delete(s.commits, id)
	reason := ReasonStake
	switch c.req.Kind {
	case KindPayout:
		reason = ReasonPot
	case KindRefund:
		reason = ReasonRefund
	}
	for i, side := range c.req.Sides {
		p := c.players[i]
		p.Inv.SetLocked(false)
		if !ok || w.Player(side.Player) != p {
			continue // failed, or left the world (and maybe came back as a new entity)
		}
		p.Inv = side.Inv
		p.Inv.SetLocked(false)
		p.RecordEconomy(reason, side.Changes)
	}
	if c.req.Kind != KindStake {
		if !ok {
			s.credits = append(s.credits, c.credit)
		}
		return
	}
	h := s.byID[c.req.Table]
	h.staking = 0
	switch {
	case !ok:
		s.unready(h)
		for _, side := range c.req.Sides {
			s.sink.StakeResult(side.Player, h.id, StakeFailed)
		}
	case h.table.Start(s.now) != nil:
		// The stakes are saved but the game can't start: give them back.
		s.unready(h)
		for _, side := range c.req.Sides {
			s.credits = append(s.credits, credit{kind: KindRefund, table: h.id, player: side.Player, gold: h.stake})
			s.sink.StakeResult(side.Player, h.id, StartFailed)
		}
	default:
		h.pot = c.req.Pot
		for _, side := range c.req.Sides {
			s.sink.StakeResult(side.Player, h.id, OK)
		}
	}
	s.publish(h, 0, false)
	leaving := h.leaving
	h.leaving = nil
	for _, p := range leaving {
		s.leave(p)
	}
}

// unready clears every Ready flag at table h.
func (s *System) unready(h *hosted) {
	for i := range h.table.Seats() {
		if p, ok := h.table.Occupant(cardtable.Seat(i)); ok {
			_ = h.table.SetReady(p, false, s.now)
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

// publish sends table h's snapshot and per-player views to the sink. When
// hasLeaver is set, leaver (who just left) gets a view too.
func (s *System) publish(h *hosted, leaver world.PlayerID, hasLeaver bool) {
	id, t := h.id, h.table
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
