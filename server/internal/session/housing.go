package session

import (
	"context"
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

// SetHousing connects the housing system, so that CommitHousing can report
// results to it and every player gets the house list. homes is the plot store;
// plots are the plots the system was built from, which give each house its
// area. Call it before the world starts ticking.
func (s *Server) SetHousing(h *housing.System, homes store.Housing, plots []store.Plot) {
	s.housing, s.homes = h, homes
	s.auctions, _ = homes.(store.Auctions)
	s.areaOf = make(map[housing.HouseID]uint8, len(plots))
	for _, p := range plots {
		s.areaOf[housing.HouseID(p.ID)] = clampU8(int(p.AreaID))
	}
}

// ReserveOwner returns the session id for the stored player storeID, who owns
// a house at startup, so the housing system can name them as its owner before
// they connect. The id is theirs from then on: it is never given to another
// guest. It reports false when no id is left. Call it before the world starts
// ticking.
func (s *Server) ReserveOwner(storeID int64) (world.PlayerID, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.keep[storeID]; ok {
		return id, true
	}
	if g := s.guests[storeID]; g != nil {
		s.keep[storeID] = g.id
		return g.id, true
	}
	if int(s.nextID) >= s.cfg.MaxGuests {
		return 0, false
	}
	s.nextID++
	s.keep[storeID] = world.PlayerID(s.nextID)
	return world.PlayerID(s.nextID), true
}

// HouseResult implements housing.Sink. Called on the tick goroutine, before
// this Tick sends the changed PlayerStats and InventoryState.
func (s *Server) HouseResult(to world.PlayerID, _ housing.HouseID, o housing.Outcome) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeHousingResult(o))
	}
}

// AuctionResult implements housing.Sink. Called on the tick goroutine.
func (s *Server) AuctionResult(to world.PlayerID, _ housing.HouseID, o housing.AuctionOutcome) {
	if g := s.live[to]; g != nil && g.conn != nil {
		b := flatbuffers.NewBuilder(32)
		protocol.AuctionResultStart(b)
		protocol.AuctionResultAddCode(b, protocol.AuctionResultCode(o)) // the values match (housing.AuctionOutcome)
		g.conn.Send(finish(b, protocol.MessageAuctionResult, protocol.AuctionResultEnd(b)))
	}
}

// HouseList implements housing.Sink: it sends one player the houses, with
// mine set for their own. House owners' session ids are kept from then on.
// Called on the tick goroutine.
func (s *Server) HouseList(to world.PlayerID, houses []housing.Info) {
	mine := map[housing.HouseID]int{}
	for _, h := range houses {
		if h.MyBid > 0 {
			mine[h.ID] = h.MyBid
		}
	}
	s.setMyBids(to, mine)
	s.mu.Lock()
	for _, h := range houses {
		if g := s.live[h.Owner]; g != nil {
			s.keep[g.storeID] = g.id
		}
	}
	s.mu.Unlock()
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(s.housingState(g, houses))
	}
}

// HouseLapsedEvent implements housing.Sink. Lapses are never confirmed yet
// (CommitHousing refuses them), so there is nothing to tell anyone.
func (s *Server) HouseLapsedEvent(housing.HouseID, world.PlayerID) {}

// CommitHousing implements housing.Committer. It first queues the player's
// pending economy events, then the claim or rent payment as one store
// ClaimPlot or PayRent, which takes the gold and writes the audit entry in
// one transaction. The saver writes it after every earlier write of the
// player, and the result goes back to the housing system through Resolve.
// Lapses are not wired yet: they are logged once per house and answered as
// not saved (left unanswered while the owner is online), so the house stays
// owned. Called on the tick goroutine; never blocks.
func (s *Server) CommitHousing(req housing.CommitRequest) {
	resolve := func(ok bool) {
		if s.housing != nil {
			s.housing.Resolve(req.ID, ok)
		}
	}
	if req.Kind == housing.CommitBid || req.Kind == housing.CommitResolve {
		s.commitAuction(req)
		return
	}
	if req.Kind == housing.CommitLapse {
		if !s.lapseLog[req.House] {
			s.lapseLog[req.House] = true
			s.log.Error("commit housing: lapse is not wired, house stays owned", "house", req.House, "owner", req.Player)
		}
		if p := s.w.Player(req.Player); p != nil {
			// The system locked the online owner's inventory for the lapse
			// and, answered, would retry and lock it again every tick, so
			// the owner could never use it. A lapse moves no gold or items:
			// unlock it and leave this lapse unanswered, so the house stays
			// pending and owned and Resolve never clears a later lock.
			p.Inv.SetLocked(false)
			return
		}
		resolve(false)
		return
	}
	g, p := s.live[req.Player], s.w.Player(req.Player)
	if g == nil || p == nil || s.homes == nil {
		s.log.Error("commit housing: player not live or no plot store", "player", req.Player, "house", req.House)
		resolve(false)
		return
	}
	now := s.clock.Now()
	var write func(ctx context.Context) error
	switch req.Kind {
	case housing.CommitClaim:
		c := store.ClaimPlot{PlayerID: g.storeID, PlotID: int32(req.House), Cost: int64(req.Gold), Rent: req.PaidUntil.Sub(now), Now: now}
		write = func(ctx context.Context) error {
			if _, err := s.homes.ClaimPlot(ctx, c); err != nil {
				return err
			}
			// The claim is committed; a failure to read the new home back
			// only delays it until restart, so it does not fail the claim.
			if err := s.homeClaimed(ctx, g, c.PlotID); err != nil {
				s.log.Error("claimed home", "plot", c.PlotID, "err", err)
			}
			return nil
		}
	case housing.CommitRent:
		var before = req.PaidUntil
		for _, h := range s.housing.List() {
			if h.ID == req.House {
				before = h.PaidUntil
			}
		}
		r := store.PayRent{PlayerID: g.storeID, Cost: int64(req.Gold), Extend: req.PaidUntil.Sub(before)}
		write = func(ctx context.Context) error { _, err := s.homes.PayRent(ctx, r); return err }
	default:
		s.log.Error("commit housing: unknown kind", "kind", req.Kind)
		resolve(false)
		return
	}
	earlier := s.appendEconomy(nil, g, p)
	s.saver.submitEconomy(append(earlier, econReq{
		changes: []store.EconomyChange{{PlayerID: g.storeID}},
		write:   write,
		done:    resolve,
	})...)
}

func isAuction(reason string) bool {
	switch reason {
	case housing.ReasonAuctionHold, housing.ReasonAuctionRefund, housing.ReasonAuctionWin:
		return true
	}
	return false
}

// storeIDOf is the stored player id of the session player id, who may be
// offline: house owners and bidders keep their ids. Tick goroutine.
func (s *Server) storeIDOf(id world.PlayerID) (int64, bool) {
	if g := s.live[id]; g != nil {
		return g.storeID, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for sid, kept := range s.keep {
		if kept == id {
			return sid, true
		}
	}
	return 0, false
}

func (s *Server) setMyBids(id world.PlayerID, mine map[housing.HouseID]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.myBids == nil {
		s.myBids = map[world.PlayerID]map[housing.HouseID]int{}
	}
	s.myBids[id] = mine
}

// joinHouses is the house list for a player who just connected. The housing
// system lists a player's own bid only when it sends them a list, so it comes
// from the bids the session loaded or last sent; a bid that is no longer the
// top bid was refunded or settled, and is dropped.
func (s *Server) joinHouses(g *guest) []housing.Info {
	houses := s.housing.List()
	s.mu.Lock()
	mine := s.myBids[g.id]
	s.mu.Unlock()
	for i := range houses {
		if h := &houses[i]; h.State == housing.InAuction && mine[h.ID] == h.TopBid {
			h.MyBid = mine[h.ID]
		}
	}
	return houses
}

// AuctionOf is the housing system's auction for a stored one. Every bidder
// and the previous owner are given their session ids first, as for owners
// (ReserveOwner). Call it before the world starts ticking.
func (s *Server) AuctionOf(a store.Auction) (*housing.Auction, error) {
	out := &housing.Auction{EndsAt: a.EndsAt}
	reserve := func(storeID int64) (world.PlayerID, error) {
		id, ok := s.ReserveOwner(storeID)
		if !ok {
			return 0, fmt.Errorf("no session id left for a player in the auction of plot %d", a.PlotID)
		}
		return id, nil
	}
	var err error
	if a.PreviousOwner != 0 {
		if out.PreviousOwner, err = reserve(a.PreviousOwner); err != nil {
			return nil, err
		}
	}
	for _, b := range a.Bids {
		id, err := reserve(b.Player)
		if err != nil {
			return nil, err
		}
		out.Bids = append(out.Bids, housing.Bid{Player: id, Amount: int(b.Amount), Held: int(b.Held)})
		if b.Held > 0 {
			s.mu.Lock()
			if s.myBids == nil {
				s.myBids = map[world.PlayerID]map[housing.HouseID]int{}
			}
			if s.myBids[id] == nil {
				s.myBids[id] = map[housing.HouseID]int{}
			}
			s.myBids[id][housing.HouseID(a.PlotID)] = int(b.Held)
			s.mu.Unlock()
		}
	}
	return out, nil
}

// commitAuction saves a bid (store PlaceAuctionBid) or an ended auction
// (store FinishAuction) as one transaction, queued after the earlier economy
// writes of every player in it. Called on the tick goroutine; never blocks.
func (s *Server) commitAuction(req housing.CommitRequest) {
	resolve := func(ok bool) {
		if s.housing != nil {
			s.housing.Resolve(req.ID, ok)
		}
	}
	if s.auctions == nil {
		s.log.Error("commit housing: no auction store", "house", req.House)
		resolve(false)
		return
	}
	// The players in the commit: the bidder and the outbid for a bid, every
	// bidder for a resolution.
	var who []world.PlayerID
	if req.Kind == housing.CommitBid {
		who = append(who, req.Player)
		for _, r := range req.Refunds {
			who = append(who, r.Player)
		}
	} else {
		for _, b := range req.Bids {
			who = append(who, b.Player)
		}
	}
	stored := make(map[world.PlayerID]int64, len(who))
	for _, id := range who {
		sid, ok := s.storeIDOf(id)
		if !ok {
			s.log.Error("commit housing: unknown auction player", "player", id, "house", req.House)
			resolve(false)
			return
		}
		stored[id] = sid
	}
	var earlier []econReq
	var names []store.EconomyChange
	for _, id := range who {
		names = append(names, store.EconomyChange{PlayerID: stored[id]})
		if g, p := s.live[id], s.w.Player(id); g != nil && p != nil {
			earlier = s.appendEconomy(earlier, g, p)
		}
	}
	plot := int32(req.House)
	r := econReq{changes: names}
	if req.Kind == housing.CommitBid {
		g := s.live[req.Player]
		if g == nil {
			s.log.Error("commit housing: bidder not live", "player", req.Player)
			resolve(false)
			return
		}
		s.mu.Lock()
		s.keep[g.storeID] = g.id // the auction names them from now on
		s.mu.Unlock()
		b := store.PlaceAuctionBid{PlotID: plot, Player: g.storeID, Amount: int64(req.Amount)}
		r.write = func(ctx context.Context) error { _, err := s.auctions.PlaceAuctionBid(ctx, b); return err }
		r.done = resolve
	} else {
		now := s.clock.Now()
		f := store.FinishAuction{PlotID: plot, RentCost: int64(req.Rent), Rent: req.PaidUntil.Sub(now), Now: now}
		var res store.AuctionResult
		r.write = func(ctx context.Context) error {
			var err error
			if res, err = s.auctions.FinishAuction(ctx, f); err != nil {
				return err
			}
			if res.Winner != 0 {
				// Committed; a failure to read the new home back only delays it until restart.
				for id, sid := range stored {
					if sid == res.Winner {
						if err := s.homeCreated(ctx, sid, id, plot); err != nil {
							s.log.Error("won home", "plot", plot, "err", err)
						}
					}
				}
			}
			return nil
		}
		r.done = func(ok bool) {
			if !ok || s.housing == nil {
				resolve(false)
				return
			}
			var winner world.PlayerID
			for id, sid := range stored {
				if res.Winner != 0 && sid == res.Winner {
					winner = id
				}
			}
			if res.Winner != 0 && winner == 0 {
				resolve(false)
				return
			}
			s.housing.ResolveAuction(req.ID, winner)
		}
	}
	s.saver.submitEconomy(append(earlier, r)...)
}

// housingState encodes the house list for the client of g.
func (s *Server) housingState(g *guest, houses []housing.Info) []byte {
	return encodeHousingState(s.bld, houses, g.id, s.areaOf, s.data.Rules)
}

var housingCodes = map[housing.Outcome]protocol.HousingResultCode{
	housing.Ok:            protocol.HousingResultCodeOk,
	housing.TooFar:        protocol.HousingResultCodeTooFar,
	housing.AlreadyOwned:  protocol.HousingResultCodeAlreadyOwned,
	housing.OwnsAnother:   protocol.HousingResultCodeOwnsAnother,
	housing.NotEnoughGold: protocol.HousingResultCodeNotEnoughGold,
	housing.NotOwner:      protocol.HousingResultCodeNotOwner,
	housing.TooManyMonths: protocol.HousingResultCodeTooManyMonths,
	housing.SaveFailed:    protocol.HousingResultCodeSaveFailed,
	// The wire has no code for these; the nearest says why nothing happened.
	housing.BadMonths:   protocol.HousingResultCodeTooManyMonths, // months outside 1..max
	housing.HouseLapsed: protocol.HousingResultCodeNotOwner,      // a lapsed house is no longer theirs to pay
	housing.NoSuchHouse: protocol.HousingResultCodeTooFar,        // no door to reach
	housing.Busy:        protocol.HousingResultCodeSaveFailed,    // a save is pending; nothing changed
}

func encodeHousingResult(o housing.Outcome) []byte {
	code, ok := housingCodes[o]
	if !ok {
		code = protocol.HousingResultCodeSaveFailed
	}
	b := flatbuffers.NewBuilder(32)
	protocol.HousingResultStart(b)
	protocol.HousingResultAddCode(b, code)
	return finish(b, protocol.MessageHousingResult, protocol.HousingResultEnd(b))
}
