package session

import (
	"context"
	"fmt"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/cardroom"
	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/systems/cardtable/shed"
	"github.com/cleiderg/lush/server/internal/world"
)

// potRef is the stored pot a card room game's stakes went into, and how many
// of its stakes have not been paid out or refunded.
type potRef struct {
	id   int64
	left int
}

// SetCardRoom connects the card room, so that CommitCardRoom can report
// results to it, a disconnected player is not removed while a stake, payout
// or refund of theirs is being saved, a seated player's seat is marked
// disconnected while they have no connection, and a seated player removed
// after the logout time leaves their table (a forfeit mid-game; card-tables
// spec, Table rules). The store must implement store.CardPots. Call it before
// the world starts ticking.
func (s *Server) SetCardRoom(c *cardroom.System) { s.cards = c }

// cardResultCodes maps card room results to the wire's CardTableResultCode.
// The rules module's reasons for rejecting a card all become IllegalMove, as
// does a game that could not start. StakeFailed has no code: the
// CardTableState that follows shows every Ready flag cleared.
var cardResultCodes = map[cardroom.Result]protocol.CardTableResultCode{
	cardroom.OK:            protocol.CardTableResultCodeOk,
	cardroom.UnknownTable:  protocol.CardTableResultCodeUnknownTable,
	cardroom.AlreadySeated: protocol.CardTableResultCodeAlreadySeated,
	cardroom.TableFull:     protocol.CardTableResultCodeTableFull,
	cardroom.InProgress:    protocol.CardTableResultCodeInProgress,
	cardroom.NotSeated:     protocol.CardTableResultCodeNotSeated,
	cardroom.NoGame:        protocol.CardTableResultCodeNoGame,
	cardroom.NotYourTurn:   protocol.CardTableResultCodeNotYourTurn,
	cardroom.NotInHand:     protocol.CardTableResultCodeIllegalMove,
	cardroom.NoMatch:       protocol.CardTableResultCodeIllegalMove,
	cardroom.BadColour:     protocol.CardTableResultCodeIllegalMove,
	cardroom.IllegalMove:   protocol.CardTableResultCodeIllegalMove,
	cardroom.StartFailed:   protocol.CardTableResultCodeIllegalMove,
	cardroom.TooFar:        protocol.CardTableResultCodeTooFar,
	cardroom.NotEnoughGold: protocol.CardTableResultCodeNotEnoughGold,
}

// sendCardResult sends r to player to as a CardTableResult, if they are
// connected and r has a wire code.
func (s *Server) sendCardResult(to world.PlayerID, r cardroom.Result) {
	code, ok := cardResultCodes[r]
	if !ok {
		return
	}
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeCardTableResult(code))
	}
}

// SitResult implements cardroom.Sink.
func (s *Server) SitResult(to world.PlayerID, _ cardroom.TableID, _ cardtable.Seat, r cardroom.Result) {
	s.sendCardResult(to, r)
}

// LeaveResult implements cardroom.Sink.
func (s *Server) LeaveResult(to world.PlayerID, _ cardroom.TableID, r cardroom.Result) {
	s.sendCardResult(to, r)
}

// ReadyResult implements cardroom.Sink.
func (s *Server) ReadyResult(to world.PlayerID, _ cardroom.TableID, r cardroom.Result) {
	s.sendCardResult(to, r)
}

// MoveResult implements cardroom.Sink: the result of a ShedPlay or ShedDraw.
func (s *Server) MoveResult(to world.PlayerID, _ cardroom.TableID, r cardroom.Result) {
	s.sendCardResult(to, r)
}

// StakeResult implements cardroom.Sink. Only NotEnoughGold, which unseats
// the player, is sent as a CardTableResult; whether the game started (OK) or
// the stakes failed to save shows in the CardTableState sent with it.
func (s *Server) StakeResult(to world.PlayerID, _ cardroom.TableID, r cardroom.Result) {
	if r == cardroom.NotEnoughGold {
		s.sendCardResult(to, r)
	}
}

// TableChanged implements cardroom.Sink. Every connected player with a view
// gets the table's CardTableState. Once the table has had a Shed game, each
// seated one also gets its ShedState and a ShedHand with only their own
// cards (empty when no game is in progress); a player who just left gets
// neither. Called on the tick goroutine.
func (s *Server) TableChanged(id cardroom.TableID, snap cardroom.Snapshot, views []cardroom.PlayerView) {
	var state, public []byte
	for _, v := range views {
		g := s.live[v.Player]
		if g == nil || g.conn == nil {
			continue
		}
		if state == nil {
			state = encodeCardTableState(s.bld, id, snap)
		}
		g.conn.Send(state)
		pub, ok := v.View.Public.(shed.PublicView)
		if !v.View.Seated || !ok {
			continue
		}
		if public == nil {
			public = encodeShedState(s.bld, id, len(snap.Seats), pub)
		}
		g.conn.Send(public)
		hand, _ := v.View.Private.([]shed.Card)
		g.conn.Send(encodeShedHand(s.bld, id, hand))
	}
}

// isCardRoom reports whether an economy event was recorded by the card room
// for a gold movement CommitCardRoom already saved.
func isCardRoom(reason string) bool {
	switch reason {
	case cardroom.ReasonStake, cardroom.ReasonPot, cardroom.ReasonRefund:
		return true
	}
	return false
}

// CommitCardRoom implements cardroom.Committer. It first queues every side's
// pending economy events, then the request as one store.CardPots
// transaction: a stake request opens a pot with every side's holdings, one
// audit entry per change and each player's stake; a payout closes the game's
// pot with the winner's holdings and audit entries; a refund returns one
// stake from it. The saver writes it after every earlier write of each
// player it names, and the result goes back to the card room through
// Resolve. Called on the tick goroutine; never blocks.
func (s *Server) CommitCardRoom(req cardroom.Request) {
	resolve := func(ok bool) {
		if s.cards != nil {
			s.cards.Resolve(req.ID, ok)
		}
	}
	pots, ok := s.store.(store.CardPots)
	if !ok {
		s.log.Error("commit card room: store has no card pots", "table", req.Table, "kind", req.Kind)
		resolve(false)
		return
	}
	if len(req.Sides) == 0 || req.Kind != cardroom.KindStake && len(req.Sides) != 1 {
		s.log.Error("commit card room: bad request", "table", req.Table, "kind", req.Kind, "sides", len(req.Sides))
		resolve(false)
		return
	}
	reason := cardroom.ReasonStake
	switch req.Kind {
	case cardroom.KindPayout:
		reason = cardroom.ReasonPot
	case cardroom.KindRefund:
		reason = cardroom.ReasonRefund
	}
	// Check every side before draining any outbox, so a refused request
	// leaves every pending event to the next Tick.
	gs := make([]*guest, len(req.Sides))
	ps := make([]*world.Player, len(req.Sides))
	changes := make([]store.EconomyChange, len(req.Sides))
	stakes := make([]store.CardStake, len(req.Sides))
	var entries []store.Entry
	for i, side := range req.Sides {
		g, p := s.live[side.Player], s.w.Player(side.Player)
		if g == nil || p == nil {
			s.log.Error("commit card room: player not live", "table", req.Table, "kind", req.Kind, "player", side.Player)
			resolve(false)
			return
		}
		h, err := toHoldings(side.Inv)
		if err != nil {
			s.log.Error("commit card room", "table", req.Table, "kind", req.Kind, "store_id", g.storeID, "err", err)
			resolve(false)
			return
		}
		gs[i], ps[i] = g, p
		changes[i] = store.EconomyChange{PlayerID: g.storeID, Holdings: h}
		stakes[i].PlayerID = g.storeID
		for _, c := range side.Changes {
			stakes[i].Gold -= int64(c.Gold)
			entries = append(entries, cardEntry(reason, g.storeID, int64(req.Table), 0, c))
		}
	}
	var earlier []econReq
	for i := range gs {
		earlier = s.appendEconomy(earlier, gs[i], ps[i])
	}

	write := func(ctx context.Context) error { return s.writeCardRoom(ctx, pots, req, stakes, changes, entries) }
	s.saver.submitEconomy(append(earlier, econReq{changes: changes, entries: entries, write: write, done: resolve})...)
}

// writeCardRoom writes req. Called on the saver goroutine, in order with
// every other economy write, so a payout or refund runs after the stake
// request that opened its pot.
func (s *Server) writeCardRoom(ctx context.Context, pots store.CardPots, req cardroom.Request,
	stakes []store.CardStake, changes []store.EconomyChange, entries []store.Entry) error {
	if req.Kind == cardroom.KindStake {
		id, err := pots.OpenCardPot(ctx, int32(req.Table), stakes, changes, entries)
		if err != nil {
			return err
		}
		s.potMu.Lock()
		s.pots[req.Game] = &potRef{id: id, left: len(stakes)}
		s.potMu.Unlock()
		return nil
	}
	s.potMu.Lock()
	ref := s.pots[req.Game]
	s.potMu.Unlock()
	if ref == nil {
		return fmt.Errorf("card room game %d at table %d: no open pot", req.Game, req.Table)
	}
	for i := range entries {
		setPot(&entries[i], ref.id)
	}
	var err error
	if req.Kind == cardroom.KindPayout {
		err = pots.PayCardPot(ctx, ref.id, changes, entries)
	} else {
		err = pots.RefundCardStake(ctx, ref.id, changes[0].PlayerID, changes, entries)
	}
	if err != nil {
		return err
	}
	s.potMu.Lock()
	ref.left--
	if req.Kind == cardroom.KindPayout || ref.left <= 0 {
		delete(s.pots, req.Game)
	}
	s.potMu.Unlock()
	return nil
}

// cardEntry is the audit entry for one card room gold change by actor.
func cardEntry(reason string, actor, table, pot int64, c world.Change) store.Entry {
	return store.Entry{Kind: c.Kind.String(), Actor: actor,
		Payload: auditPayload{Reason: reason, Item: int(c.Item), Qty: c.Qty, Gold: c.Gold, Table: table, Pot: pot}}
}

func setPot(e *store.Entry, pot int64) {
	if p, ok := e.Payload.(auditPayload); ok {
		p.Pot = pot
		e.Payload = p
	}
}

// RefundCardPots returns the stakes of every card pot still open, from a
// server that stopped with a staked game in progress, to the players' stored
// gold, each with a card_refund audit entry, and closes the pots. Call it at
// startup before accepting connections, so no player is loaded with the
// gold from before the refund. It does nothing if the store has no card
// pots.
func (s *Server) RefundCardPots(ctx context.Context) error {
	pots, ok := s.store.(store.CardPots)
	if !ok {
		return nil
	}
	done, err := pots.RefundOpenCardPots(ctx, func(p store.CardPot, st store.CardStake) store.Entry {
		return store.Entry{Kind: world.ChangeGold.String(), Actor: st.PlayerID,
			Payload: auditPayload{Reason: cardroom.ReasonRefund, Gold: int(st.Gold), Table: int64(p.Table), Pot: p.ID}}
	})
	for _, p := range done {
		s.log.Info("refunded open card pot", "pot", p.ID, "table", p.Table, "stakes", len(p.Stakes))
	}
	if err != nil {
		return fmt.Errorf("refund card pots: %w", err)
	}
	return nil
}
