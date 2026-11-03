package housing

import (
	"errors"
	"fmt"
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

// Info is one house in the house list.
type Info struct {
	ID        HouseID
	Owner     world.PlayerID
	PaidUntil time.Time
	State     State
}

// Sink receives everything players must be told; the session implements it.
// It is called on the tick goroutine and must not block.
type Sink interface {
	// HouseResult is sent for every claim or rent request.
	HouseResult(to world.PlayerID, house HouseID, o Outcome)
	// HouseList is sent with every house after each confirmed change and
	// each confirmed lapse.
	HouseList(houses []Info)
	// HouseLapsedEvent is sent once a lapse is confirmed.
	HouseLapsedEvent(house HouseID, previousOwner world.PlayerID)
}

// CommitKind is what a commit does.
type CommitKind uint8

const (
	CommitClaim CommitKind = iota + 1
	CommitRent
	CommitLapse
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
}

// Committer saves requests off the tick, then calls System.Resolve with the
// ID. The player's inventory (if online) is locked when it is called. It is
// called on the tick goroutine and must not block.
type Committer interface {
	CommitHousing(req CommitRequest)
}

type house struct {
	Plot
	pending CommitID // 0 when none
}

type inflight struct {
	req    CommitRequest
	player *world.Player // nil when the previous owner was offline (lapse)
	inv    world.Inventory
}

type resolution struct {
	id CommitID
	ok bool
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
	s.resolved = append(s.resolved, resolution{id, ok})
	s.rmu.Unlock()
}

// List returns every house as of now.
func (s *System) List() []Info {
	now := s.clock.Now()
	out := make([]Info, len(s.houses))
	for i, h := range s.houses {
		out[i] = Info{ID: h.ID, Owner: h.Owner, PaidUntil: h.PaidUntil, State: s.rules.State(House{h.Owner, h.PaidUntil}, now)}
	}
	return out
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
			_, owns := s.owner[p.ID]
			return s.rules.Claim(House{h.Owner, h.PaidUntil}, p.ID, owns, p.Inv.Gold, now)
		})
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
func (s *System) start(h *house, req CommitRequest, p *world.Player, inv world.Inventory) {
	s.nextID++
	req.ID = s.nextID
	if p != nil {
		p.Inv.SetLocked(true)
		s.committing[req.Player] = req.ID
	}
	h.pending = req.ID
	s.commits[req.ID] = &inflight{req: req, player: p, inv: inv}
	out := req
	out.Changes = append([]world.Change(nil), req.Changes...)
	s.committer.CommitHousing(out)
}

// Tick applies the commit results reported since the last Tick, then asks
// the Committer to lapse every house now below one month prepaid that has no
// commit pending.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.rmu.Lock()
	resolved := s.resolved
	s.resolved = nil
	s.rmu.Unlock()
	for _, r := range resolved {
		s.finish(w, r.id, r.ok)
	}

	now := s.clock.Now()
	for _, h := range s.houses {
		if h.Owner == 0 || h.pending != 0 || s.rules.State(House{h.Owner, h.PaidUntil}, now) != Lapsed {
			continue
		}
		p := w.Player(h.Owner)
		if p != nil {
			if _, busy := s.committing[p.ID]; busy || p.Inv.Locked() {
				continue // try again on a later tick
			}
		}
		s.start(h, CommitRequest{Kind: CommitLapse, House: h.ID, Player: h.Owner}, p, world.Inventory{})
	}
}

func (s *System) finish(w *world.World, id CommitID, ok bool) {
	c := s.commits[id]
	if c == nil {
		return
	}
	delete(s.commits, id)
	h := s.byID[c.req.House]
	h.pending = 0
	p := c.player
	if p != nil {
		delete(s.committing, c.req.Player)
		p.Inv.SetLocked(false)
	}
	here := p != nil && w.Player(c.req.Player) == p // still in the world

	if !ok {
		if here && c.req.Kind != CommitLapse {
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
		h.Owner, h.PaidUntil = 0, time.Time{}
		delete(s.owner, prev)
		s.sink.HouseLapsedEvent(h.ID, prev)
		s.sink.HouseList(s.List())
		return
	}
	if here {
		inv := c.inv
		inv.SetLocked(false)
		p.Inv = inv
		p.RecordEconomy(c.req.Reason, c.req.Changes)
		s.sink.HouseResult(p.ID, h.ID, Ok)
	}
	s.sink.HouseList(s.List())
}
