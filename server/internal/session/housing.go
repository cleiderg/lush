package session

import (
	"context"

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

// HouseList implements housing.Sink: it sends every connected player the
// houses, with mine set for their own. House owners' session ids are kept
// from then on. Called on the tick goroutine.
func (s *Server) HouseList(houses []housing.Info) {
	s.mu.Lock()
	for _, h := range houses {
		if g := s.live[h.Owner]; g != nil {
			s.keep[g.storeID] = g.id
		}
	}
	s.mu.Unlock()
	for _, g := range s.live {
		if g.conn != nil {
			g.conn.Send(s.housingState(g, houses))
		}
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
// not saved, so the house stays owned. Called on the tick goroutine; never
// blocks.
func (s *Server) CommitHousing(req housing.CommitRequest) {
	resolve := func(ok bool) {
		if s.housing != nil {
			s.housing.Resolve(req.ID, ok)
		}
	}
	if req.Kind == housing.CommitLapse {
		if !s.lapseLog[req.House] {
			s.lapseLog[req.House] = true
			s.log.Error("commit housing: lapse is not wired, house stays owned", "house", req.House, "owner", req.Player)
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
		write = func(ctx context.Context) error { _, err := s.homes.ClaimPlot(ctx, c); return err }
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
