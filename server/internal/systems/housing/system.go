package housing

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// Outcome is the result of a claim or rent payment request.
type Outcome uint8

const (
	Ok            Outcome = 0
	TooFar        Outcome = 1 // beyond reach of the house's door
	AlreadyOwned  Outcome = 2
	OwnsAnother   Outcome = 3
	NotEnoughGold Outcome = 4
	NotOwner      Outcome = 5
	TooManyMonths Outcome = 6
	BadMonths     Outcome = 7
	HouseLapsed   Outcome = 8 // rent can't be paid on a lapsed house
	NoSuchHouse   Outcome = 9
	Busy          Outcome = 10 // a commit for the player or house is pending
	SaveFailed    Outcome = 11
)

var outcomeNames = [...]string{"ok", "too_far", "already_owned", "owns_another", "not_enough_gold",
	"not_owner", "too_many_months", "bad_months", "lapsed", "no_such_house", "busy", "save_failed"}

func (o Outcome) String() string {
	if int(o) < len(outcomeNames) {
		return outcomeNames[o]
	}
	return fmt.Sprintf("Outcome(%d)", uint8(o))
}

// HouseID identifies a house plot.
type HouseID uint32

// Plot is a house plot as the system is built: where its door is and who
// owns it now (Owner 0 when empty).
type Plot struct {
	ID        HouseID
	Door      world.Vec2
	Owner     world.PlayerID
	PaidUntil time.Time
	// Auction is the open auction when the house is up for auction (Owner 0).
	Auction *Auction
}

// Config is what the system needs beyond gamedata.
type Config struct {
	// TileSize is the length of a tile in metres.
	TileSize float64
	// DoorReachTiles is how close to a door a player must stand to claim or
	// pay rent. PLACEHOLDER: docs/specs/housing.md gives no distance; this
	// follows the other interaction distances (shop, bank).
	DoorReachTiles int
}

// DefaultConfig returns placeholder values (see Config).
func DefaultConfig() Config { return Config{TileSize: 1, DoorReachTiles: 3} }

// ClaimCommand claims the empty house House for its sender.
type ClaimCommand struct {
	Player world.PlayerID
	House  HouseID
}

func (c ClaimCommand) Target() world.PlayerID { return c.Player }

// PayRentCommand prepays Months months of rent on House.
type PayRentCommand struct {
	Player world.PlayerID
	House  HouseID
	Months int
}

func (c PayRentCommand) Target() world.PlayerID { return c.Player }

// BidCommand bids Amount gold on House, which is in auction.
type BidCommand struct {
	Player world.PlayerID
	House  HouseID
	Amount int
}

func (c BidCommand) Target() world.PlayerID { return c.Player }

// AuctionOutcome is the result of a bid; the values match the protocol's
// AuctionResultCode.
type AuctionOutcome uint8

const (
	AuctionOk            AuctionOutcome = 0
	AuctionNotAuction    AuctionOutcome = 1
	AuctionTooFar        AuctionOutcome = 2
	AuctionPreviousOwner AuctionOutcome = 3
	AuctionBidTooLow     AuctionOutcome = 4
	AuctionNotEnoughGold AuctionOutcome = 5
	AuctionOwnsAnother   AuctionOutcome = 6
	AuctionSaveFailed    AuctionOutcome = 7 // also sent when a commit for the player or house is pending
)

func (o AuctionOutcome) String() string {
	names := [...]string{"ok", "not_auction", "too_far", "previous_owner", "bid_too_low",
		"not_enough_gold", "owns_another", "save_failed"}
	if int(o) < len(names) {
		return names[o]
	}
	return fmt.Sprintf("AuctionOutcome(%d)", uint8(o))
}

// Info is one house in the house list. TopBid and AuctionEnd are set while
// the house is in auction (TopBid 0 with no bids). MyBid is the receiving
// player's own held bid; it is 0 in List and set per player in the lists
// sent to the Sink.
type Info struct {
	ID         HouseID
	Owner      world.PlayerID
	PaidUntil  time.Time
	State      State
	TopBid     int
	AuctionEnd time.Time
	MyBid      int
}

// Sink receives everything players must be told; the session implements it.
// It is called on the tick goroutine and must not block.
type Sink interface {
	// HouseResult is sent for every claim or rent request.
	HouseResult(to world.PlayerID, house HouseID, o Outcome)
	// AuctionResult is sent for every bid.
	AuctionResult(to world.PlayerID, house HouseID, o AuctionOutcome)
	// HouseList is sent to each online player with every house after each
	// confirmed change, lapse, bid and resolution. MyBid is that player's own.
	HouseList(to world.PlayerID, houses []Info)
	// HouseLapsedEvent is sent once a lapse is confirmed.
	HouseLapsedEvent(house HouseID, previousOwner world.PlayerID)
}

// CommitKind is what a commit does.
type CommitKind uint8

const (
	CommitClaim CommitKind = iota + 1
	CommitRent
	CommitLapse
	CommitBid     // hold a bid and refund the outbid player
	CommitResolve // settle an ended auction; the store picks the winner
)

// CommitID identifies a commit in flight.
type CommitID uint64

// CommitRequest is one claim, rent payment or lapse to save as one database
// transaction.
type CommitRequest struct {
	ID        CommitID
	Kind      CommitKind
	House     HouseID
	Player    world.PlayerID // the claimer or payer; the previous owner for a lapse
	Gold      int            // gold taken; 0 for a lapse
	PaidUntil time.Time      // the new paid-until; zero for a lapse
	Reason    string         // ReasonClaim or ReasonRent; empty for a lapse
	Changes   []world.Change // audit changes to the player's gold

	// CommitBid: Gold is the gold newly held (Reason ReasonAuctionHold), Amount
	// the bid, and Refunds the held gold returned to the outbid player.
	Amount  int
	Refunds []AuctionRefund
	// CommitResolve: Bids highest first. The store picks the first bidder who
	// can pay Amount plus Rent (from their gold and held gold) and owns no
	// house, makes them the owner paid until PaidUntil (ReasonAuctionWin),
	// refunds every other bidder's Held (ReasonAuctionRefund), and reports
	// the winner to System.ResolveAuction, or 0 when nobody can take it.
	Bids []AuctionBid
	Rent int
}

// AuctionRefund is held gold returned to a player (ReasonAuctionRefund).
type AuctionRefund struct {
	Player  world.PlayerID
	Gold    int
	Changes []world.Change
}

// AuctionBid is one bidder at resolution: their bid and the gold held for it.
type AuctionBid struct {
	Player world.PlayerID
	Amount int
	Held   int
}

// Committer saves requests off the tick, then calls System.Resolve with the
// ID. The player's inventory (if online) is locked when it is called. It is
// called on the tick goroutine and must not block.
type Committer interface {
	CommitHousing(req CommitRequest)
}

type house struct {
	Plot
	auction *Auction
	pending CommitID // 0 when none
}

type inflight struct {
	req     CommitRequest
	player  *world.Player // nil when the previous owner was offline (lapse)
	inv     world.Inventory
	locked  []*world.Player // every online player whose inventory is locked
	auction Auction         // CommitBid: the auction once confirmed
}

type resolution struct {
	id     CommitID
	ok     bool
	winner world.PlayerID
}

type queuedBid struct {
	cmd BidCommand
	p   *world.Player
}

// System runs the house plots.
type System struct {
	rules     Rules
	cfg       Config
	reach     float64
	clock     world.Clock
	sink      Sink
	committer Committer

	houses []*house // in id order given
	byID   map[HouseID]*house
	owner  map[world.PlayerID]HouseID

	nextID     CommitID
	commits    map[CommitID]*inflight
	committing map[world.PlayerID]CommitID

	bids []queuedBid // bids handled on the next Tick, which has the world

	rmu      sync.Mutex
	resolved []resolution
}

// NewSystem creates the housing system for plots. Rent settings come from d.
func NewSystem(d *gamedata.Data, cfg Config, plots []Plot, clock world.Clock, sink Sink, c Committer) (*System, error) {
	s := &System{
		rules:      New(d.Rules),
		cfg:        cfg,
		reach:      float64(cfg.DoorReachTiles) * cfg.TileSize,
		clock:      clock,
		sink:       sink,
		committer:  c,
		byID:       make(map[HouseID]*house),
		owner:      make(map[world.PlayerID]HouseID),
		commits:    make(map[CommitID]*inflight),
		committing: make(map[world.PlayerID]CommitID),
	}
	for _, p := range plots {
		if s.byID[p.ID] != nil {
			return nil, fmt.Errorf("housing: duplicate house %d", p.ID)
		}
		if p.Owner != 0 {
			if prev, dup := s.owner[p.Owner]; dup {
				return nil, fmt.Errorf("housing: player %d owns houses %d and %d", p.Owner, prev, p.ID)
			}
			s.owner[p.Owner] = p.ID
		}
		h := &house{Plot: p}
		if p.Auction != nil {
			if p.Owner != 0 {
				return nil, fmt.Errorf("housing: house %d is owned and in auction", p.ID)
			}
			a := *p.Auction
			a.Bids = append([]Bid(nil), a.Bids...)
			h.auction = &a
		}
		s.houses = append(s.houses, h)
		s.byID[p.ID] = h
	}
	return s, nil
}

func (s *System) Name() string { return "housing" }

// Resolve reports the outcome of commit id. The next Tick applies it. Safe
// for concurrent use; unknown or repeated ids are ignored.
func (s *System) Resolve(id CommitID, ok bool) {
	s.rmu.Lock()
	s.resolved = append(s.resolved, resolution{id: id, ok: ok})
	s.rmu.Unlock()
}

// ResolveAuction reports that the commit id of an ended auction was saved.
// winner is the bidder the store picked, or 0 when nobody could take the
// house. Report a failed commit with Resolve(id, false). Safe for concurrent
// use.
func (s *System) ResolveAuction(id CommitID, winner world.PlayerID) {
	s.rmu.Lock()
	s.resolved = append(s.resolved, resolution{id: id, ok: true, winner: winner})
	s.rmu.Unlock()
}

// List returns every house as of now, without anyone's own bid.
func (s *System) List() []Info { return s.listFor(0) }

// listFor is List with MyBid set for player.
func (s *System) listFor(player world.PlayerID) []Info {
	now := s.clock.Now()
	out := make([]Info, len(s.houses))
	for i, h := range s.houses {
		out[i] = Info{ID: h.ID, Owner: h.Owner, PaidUntil: h.PaidUntil, State: s.rules.State(House{h.Owner, h.PaidUntil}, now)}
		if a := h.auction; a != nil {
			out[i].State = InAuction
			out[i].AuctionEnd = a.EndsAt
			if top, ok := a.top(); ok {
				out[i].TopBid = top.Amount
			}
			for _, b := range a.Bids {
				if player != 0 && b.Player == player {
					out[i].MyBid = b.Held
				}
			}
		}
	}
	return out
}

// sendLists sends the house list to every online player.
func (s *System) sendLists(w *world.World) {
	for _, p := range w.Players() {
		s.sink.HouseList(p.ID, s.listFor(p.ID))
	}
}

// free reports whether p's inventory can be locked for a new commit.
func (s *System) free(p *world.Player) bool {
	_, busy := s.committing[p.ID]
	return !busy && !p.Inv.Locked()
}

func (s *System) inReach(p *world.Player, door world.Vec2) bool {
	dx := float64(p.Pos.X) - float64(door.X)
	dy := float64(p.Pos.Y) - float64(door.Y)
	return dx*dx+dy*dy <= s.reach*s.reach
}

func outcomeOf(err error) Outcome {
	switch {
	case errors.Is(err, ErrOwned):
		return AlreadyOwned
	case errors.Is(err, ErrAlreadyOwnsHouse):
		return OwnsAnother
	case errors.Is(err, ErrNotEnoughGold):
		return NotEnoughGold
	case errors.Is(err, ErrNotOwner):
		return NotOwner
	case errors.Is(err, ErrTooManyMonths):
		return TooManyMonths
	case errors.Is(err, ErrBadMonths):
		return BadMonths
	case errors.Is(err, ErrLapsed):
		return HouseLapsed
	}
	return Busy
}

// Handle applies a housing command for p.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	switch c := cmd.(type) {
	case ClaimCommand:
		s.request(p, c.House, CommitClaim, func(h *house, now time.Time) (House, int, error) {
			if h.auction != nil {
				return House{}, 0, ErrOwned
			}
			_, owns := s.owner[p.ID]
			return s.rules.Claim(House{h.Owner, h.PaidUntil}, p.ID, owns, p.Inv.Gold, now)
		})
	case BidCommand:
		s.bids = append(s.bids, queuedBid{c, p})
	case PayRentCommand:
		s.request(p, c.House, CommitRent, func(h *house, now time.Time) (House, int, error) {
			return s.rules.PayRent(House{h.Owner, h.PaidUntil}, p.ID, c.Months, p.Inv.Gold, now)
		})
	}
}

func (s *System) request(p *world.Player, id HouseID, kind CommitKind, apply func(*house, time.Time) (House, int, error)) {
	h := s.byID[id]
	switch {
	case h == nil:
		s.sink.HouseResult(p.ID, id, NoSuchHouse)
		return
	case !s.inReach(p, h.Door):
		s.sink.HouseResult(p.ID, id, TooFar)
		return
	}
	if _, busy := s.committing[p.ID]; busy || h.pending != 0 || p.Inv.Locked() {
		s.sink.HouseResult(p.ID, id, Busy)
		return
	}
	nh, cost, err := apply(h, s.clock.Now())
	if err != nil {
		s.sink.HouseResult(p.ID, id, outcomeOf(err))
		return
	}
	inv := p.Inv.Clone()
	changes, err := inv.SpendGold(cost)
	if err != nil {
		s.sink.HouseResult(p.ID, id, NotEnoughGold)
		return
	}
	reason := ReasonClaim
	if kind == CommitRent {
		reason = ReasonRent
	}
	req := CommitRequest{Kind: kind, House: id, Player: p.ID, Gold: cost, PaidUntil: nh.PaidUntil, Reason: reason, Changes: changes}
	s.start(h, req, p, inv)
}

// start registers req, locks the inventory of p (if online) and hands the
// request to the Committer.
// others are further online players to lock for the same commit.
func (s *System) start(h *house, req CommitRequest, p *world.Player, inv world.Inventory, others ...*world.Player) {
	s.nextID++
	req.ID = s.nextID
	c := &inflight{req: req, player: p, inv: inv}
	if p != nil {
		c.locked = append(c.locked, p)
	}
	c.locked = append(c.locked, others...)
	for _, lp := range c.locked {
		lp.Inv.SetLocked(true)
		s.committing[lp.ID] = req.ID
	}
	h.pending = req.ID
	s.commits[req.ID] = c
	out := req
	out.Changes = append([]world.Change(nil), req.Changes...)
	out.Refunds = append([]AuctionRefund(nil), req.Refunds...)
	out.Bids = append([]AuctionBid(nil), req.Bids...)
	s.committer.CommitHousing(out)
}

// Tick applies the commit results reported since the last Tick, handles the
// bids queued since then, asks the Committer to lapse every house now below
// one month prepaid, and to settle every auction that has ended, skipping
// houses with a commit pending.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.rmu.Lock()
	resolved := s.resolved
	s.resolved = nil
	s.rmu.Unlock()
	for _, r := range resolved {
		s.finish(w, r)
	}

	bids := s.bids
	s.bids = nil
	for _, b := range bids {
		if w.Player(b.p.ID) == b.p {
			s.bid(w, b.p, b.cmd)
		}
	}

	now := s.clock.Now()
	for _, h := range s.houses {
		if h.pending != 0 {
			continue
		}
		if a := h.auction; a != nil {
			if !now.Before(a.EndsAt) {
				s.startResolve(w, h, now)
			}
			continue
		}
		if h.Owner == 0 || s.rules.State(House{h.Owner, h.PaidUntil}, now) != Lapsed {
			continue
		}
		p := w.Player(h.Owner)
		if p != nil && !s.free(p) {
			continue // try again on a later tick
		}
		s.start(h, CommitRequest{Kind: CommitLapse, House: h.ID, Player: h.Owner}, p, world.Inventory{})
	}
}

func auctionOutcomeOf(err error) AuctionOutcome {
	switch {
	case errors.Is(err, ErrAuctionOver):
		return AuctionNotAuction
	case errors.Is(err, ErrPreviousOwner):
		return AuctionPreviousOwner
	case errors.Is(err, ErrAlreadyOwnsHouse):
		return AuctionOwnsAnother
	case errors.Is(err, ErrBidTooLow), errors.Is(err, ErrNotAboveTop):
		return AuctionBidTooLow
	case errors.Is(err, ErrNotEnoughGold):
		return AuctionNotEnoughGold
	}
	return AuctionSaveFailed
}

// bid validates a bid and hands it to the Committer, locking the bidder and
// the online outbid player. Nothing changes until the commit is confirmed.
func (s *System) bid(w *world.World, p *world.Player, c BidCommand) {
	h := s.byID[c.House]
	if h == nil || h.auction == nil {
		s.sink.AuctionResult(p.ID, c.House, AuctionNotAuction)
		return
	}
	if !s.inReach(p, h.Door) {
		s.sink.AuctionResult(p.ID, c.House, AuctionTooFar)
		return
	}
	if h.pending != 0 || !s.free(p) {
		s.sink.AuctionResult(p.ID, c.House, AuctionSaveFailed)
		return
	}
	_, owns := s.owner[p.ID]
	na, hold, refunds, err := s.rules.PlaceBid(*h.auction, p.ID, c.Amount, owns, p.Inv.Gold, s.clock.Now())
	if err != nil {
		s.sink.AuctionResult(p.ID, c.House, auctionOutcomeOf(err))
		return
	}
	inv := p.Inv.Clone()
	changes, err := inv.SpendGold(hold)
	if err != nil {
		s.sink.AuctionResult(p.ID, c.House, AuctionNotEnoughGold)
		return
	}
	req := CommitRequest{Kind: CommitBid, House: h.ID, Player: p.ID, Gold: hold, Amount: c.Amount,
		Reason: ReasonAuctionHold, Changes: changes}
	var others []*world.Player
	for _, r := range refunds {
		req.Refunds = append(req.Refunds, AuctionRefund{r.Player, r.Gold, []world.Change{{Kind: world.ChangeGold, Gold: r.Gold}}})
		if op := w.Player(r.Player); op != nil {
			if !s.free(op) {
				s.sink.AuctionResult(p.ID, c.House, AuctionSaveFailed)
				return
			}
			others = append(others, op)
		}
	}
	s.start(h, req, p, inv, others...)
	s.commits[s.nextID].auction = na
}

// startResolve asks the Committer to settle the ended auction of h, locking
// every online bidder. It does nothing (to be tried again) if one is busy.
func (s *System) startResolve(w *world.World, h *house, now time.Time) {
	bids := append([]Bid(nil), h.auction.Bids...)
	sort.SliceStable(bids, func(i, j int) bool { return bids[i].Amount > bids[j].Amount })
	req := CommitRequest{Kind: CommitResolve, House: h.ID, Player: h.auction.PreviousOwner,
		Rent: s.rules.ClaimCost(), PaidUntil: s.rules.ClaimPaidUntil(now), Reason: ReasonAuctionWin}
	var online []*world.Player
	for _, b := range bids {
		req.Bids = append(req.Bids, AuctionBid{b.Player, b.Amount, b.Held})
		if p := w.Player(b.Player); p != nil {
			if !s.free(p) {
				return
			}
			online = append(online, p)
		}
	}
	s.start(h, req, nil, world.Inventory{}, online...)
}

func (s *System) finish(w *world.World, r resolution) {
	c := s.commits[r.id]
	if c == nil {
		return
	}
	h := s.byID[c.req.House]
	if r.ok && c.req.Kind == CommitResolve && r.winner != 0 && !hasBid(h.auction, r.winner) {
		r.ok = false // the store named someone who never bid
	}
	delete(s.commits, r.id)
	h.pending = 0
	for _, lp := range c.locked {
		delete(s.committing, lp.ID)
		lp.Inv.SetLocked(false)
	}
	p := c.player
	here := p != nil && w.Player(c.req.Player) == p // still in the world

	if !r.ok {
		if here && c.req.Kind == CommitBid {
			s.sink.AuctionResult(p.ID, h.ID, AuctionSaveFailed)
		} else if here && c.req.Kind != CommitLapse {
			s.sink.HouseResult(p.ID, h.ID, SaveFailed)
		}
		return
	}
	switch c.req.Kind {
	case CommitClaim:
		h.Owner, h.PaidUntil = c.req.Player, c.req.PaidUntil
		s.owner[h.Owner] = h.ID
	case CommitRent:
		h.PaidUntil = c.req.PaidUntil
	case CommitLapse:
		prev := h.Owner
		now := s.clock.Now()
		a := s.rules.StartAuction(prev, now)
		h.Owner, h.PaidUntil, h.auction = 0, time.Time{}, &a
		delete(s.owner, prev)
		s.sink.HouseLapsedEvent(h.ID, prev)
		s.sendLists(w)
		return
	case CommitBid:
		a := c.auction
		h.auction = &a
		for _, rf := range c.req.Refunds {
			s.credit(w, c, rf.Player, ReasonAuctionRefund, rf.Gold)
		}
		if here {
			inv := c.inv
			inv.SetLocked(false)
			p.Inv = inv
			p.RecordEconomy(c.req.Reason, c.req.Changes)
			s.sink.AuctionResult(p.ID, h.ID, AuctionOk)
		}
		s.sendLists(w)
		return
	case CommitResolve:
		s.settle(w, h, c, r.winner)
		s.sendLists(w)
		return
	}
	if here {
		inv := c.inv
		inv.SetLocked(false)
		p.Inv = inv
		p.RecordEconomy(c.req.Reason, c.req.Changes)
		s.sink.HouseResult(p.ID, h.ID, Ok)
	}
	s.sendLists(w)
}

func hasBid(a *Auction, player world.PlayerID) bool {
	for _, b := range a.Bids {
		if b.Player == player {
			return true
		}
	}
	return false
}

// credit returns gold to player's inventory if they are online and were locked
// by c.
func (s *System) credit(w *world.World, c *inflight, player world.PlayerID, reason string, gold int) {
	p := w.Player(player)
	if p == nil || !containsPlayer(c.locked, p) || gold <= 0 {
		return
	}
	if changes, err := p.Inv.AddGold(gold); err == nil {
		p.RecordEconomy(reason, changes)
	}
}

func containsPlayer(ps []*world.Player, p *world.Player) bool {
	for _, x := range ps {
		if x == p {
			return true
		}
	}
	return false
}

// settle applies the store's result for an ended auction: the winner (0 for
// none) owns the house; online winners pay what wasn't held; every other
// online bidder gets their held bid back.
func (s *System) settle(w *world.World, h *house, c *inflight, winner world.PlayerID) {
	for _, b := range c.req.Bids {
		if b.Player == winner {
			if p := w.Player(b.Player); p != nil && containsPlayer(c.locked, p) {
				if changes, err := p.Inv.SpendGold(b.Amount + c.req.Rent - b.Held); err == nil {
					p.RecordEconomy(ReasonAuctionWin, changes)
				}
			}
			continue
		}
		s.credit(w, c, b.Player, ReasonAuctionRefund, b.Held)
	}
	h.auction = nil
	if winner != 0 {
		h.Owner, h.PaidUntil = winner, c.req.PaidUntil
		s.owner[winner] = h.ID
	}
}
