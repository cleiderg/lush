// Package trade is the player trade window (docs/specs/gameplay.md, Player
// trading): two players within Rules.TradeDistanceTiles of each other and
// outside the PvP zone request each other, offer items and gold, and accept
// two screens as in OSRS. Every change goes to both players through a Sink,
// and a trade both players accepted on screen 2 is committed.
//
// Committing re-checks both offers against the inventories, works out both
// resulting inventories, locks both players' inventories and hands the swap
// to a Committer, which saves it as one database transaction off the tick and
// reports back through Resolve. The swap is applied to the players only when
// the save succeeded, so items and gold can't be duplicated or lost.
package trade

import (
	"fmt"
	"sync"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// Result is why a trade closed or a request failed. The values match
// protocol TradeResultCode.
type Result uint8

const (
	Completed     Result = 0
	Cancelled     Result = 1
	TooFar        Result = 2
	InPvpZone     Result = 3
	PartnerBusy   Result = 4
	OfferNotHeld  Result = 5
	InventoryFull Result = 6
)

var resultNames = [...]string{"completed", "cancelled", "too_far", "in_pvp_zone", "partner_busy", "offer_not_held", "inventory_full"}

func (r Result) String() string {
	if int(r) < len(resultNames) {
		return resultNames[r]
	}
	return fmt.Sprintf("Result(%d)", uint8(r))
}

// Screens of the trade window.
const (
	ScreenOffer   = 1
	ScreenConfirm = 2
)

// RequestCommand asks Partner to trade, or accepts Partner's request.
type RequestCommand struct {
	Player  world.PlayerID
	Partner world.PlayerID
}

func (c RequestCommand) Target() world.PlayerID { return c.Player }

// OfferCommand adds Quantity items from inventory slot Slot to the offer.
type OfferCommand struct {
	Player   world.PlayerID
	Slot     int
	Quantity int
}

func (c OfferCommand) Target() world.PlayerID { return c.Player }

// RemoveCommand takes item Index (as in View.Mine.Items) off the offer.
type RemoveCommand struct {
	Player world.PlayerID
	Index  int
}

func (c RemoveCommand) Target() world.PlayerID { return c.Player }

// SetGoldCommand replaces the gold on the offer.
type SetGoldCommand struct {
	Player world.PlayerID
	Gold   int
}

func (c SetGoldCommand) Target() world.PlayerID { return c.Player }

// AcceptCommand accepts Screen. It is ignored unless Screen is the trade's
// current screen.
type AcceptCommand struct {
	Player world.PlayerID
	Screen int
}

func (c AcceptCommand) Target() world.PlayerID { return c.Player }

// CancelCommand closes the trade window, or withdraws a pending request.
type CancelCommand struct {
	Player world.PlayerID
}

func (c CancelCommand) Target() world.PlayerID { return c.Player }

// Item is one entry of an offer: Qty of Item from inventory slot Slot.
type Item struct {
	Slot int
	Item gamedata.ItemID
	Qty  int
}

// Offer is one side of a trade.
type Offer struct {
	Items []Item
	Gold  int
}

func (o Offer) clone() Offer {
	o.Items = append([]Item(nil), o.Items...)
	return o
}

// View is the trade window as one player sees it ("my" side is Mine).
type View struct {
	Partner         world.PlayerID
	Screen          int
	Mine            Offer
	Partners        Offer
	MyAccepted      bool
	PartnerAccepted bool
}

// Sink receives everything the players must be told; the session implements
// it to send TradeRequestNotice, TradeState and TradeResult. It is called on
// the tick goroutine and must not block.
type Sink interface {
	TradeRequestNotice(to, from world.PlayerID)
	TradeState(to world.PlayerID, v View)
	TradeResult(to world.PlayerID, r Result)
}

// ReasonTrade is the economy audit log reason for a completed trade.
const ReasonTrade = "trade"

// CommitID identifies a trade commit in flight.
type CommitID uint64

// Side is one player's half of a commit.
type Side struct {
	Player world.PlayerID
	// Gave is what the player offered.
	Gave Offer
	// Inv is the player's inventory once the swap is applied.
	Inv world.Inventory
	// Changes are the per-item and gold changes the swap makes to the
	// player's holdings, for the economy audit log: the items and gold given
	// (ChangeRemove, negative ChangeGold), then those received (ChangeAdd,
	// positive ChangeGold).
	Changes []world.Change
}

// CommitRequest is a trade to save as one database transaction.
type CommitRequest struct {
	ID   CommitID
	A, B Side
}

// Committer receives a trade both players accepted on screen 2, on the Tick
// after both accepted, once both players are still in the world, in reach and
// outside the PvP zone, and both offers were re-checked. The window is
// already closed and both inventories are locked when it is called. The
// Committer saves the swap off the tick and then calls System.Resolve with
// req.ID. It is called on the tick goroutine and must not block.
type Committer interface {
	CommitTrade(req CommitRequest)
}

// inflight is a commit waiting for its result.
type inflight struct {
	req     CommitRequest
	players [2]*world.Player // as they were at commit time
}

type resolution struct {
	id CommitID
	ok bool
}

// side is one player's half of an open trade.
type side struct {
	id       world.PlayerID
	offer    Offer
	accepted bool
}

type session struct {
	sides  [2]side
	screen int
}

// of returns p's side and the partner's side.
func (t *session) of(p world.PlayerID) (me, other *side) {
	if t.sides[0].id == p {
		return &t.sides[0], &t.sides[1]
	}
	return &t.sides[1], &t.sides[0]
}

// System runs trade windows. Only RequestCommand needs another player's
// position, so requests are queued by Handle and settled in Tick; every
// other command is settled when it is handled.
type System struct {
	data      *gamedata.Data
	areas     *areas.Lookup
	reach     float64 // metres
	sink      Sink
	committer Committer

	requests []RequestCommand                  // queued this tick
	pending  map[world.PlayerID]world.PlayerID // requester -> partner
	open     []*session                        // in opening order
	byPlayer map[world.PlayerID]*session

	nextID     CommitID
	commits    map[CommitID]*inflight
	committing map[world.PlayerID]CommitID

	rmu      sync.Mutex
	resolved []resolution // from Resolve, applied by Tick
}

// New creates the trade system. Rules.TradeDistanceTiles comes from d and the
// PvP zone from l; tileSize is the length of a tile in metres (the collision
// grid's cell size).
func New(d *gamedata.Data, l *areas.Lookup, tileSize float64, sink Sink, c Committer) *System {
	return &System{
		data:      d,
		areas:     l,
		reach:     float64(d.Rules.TradeDistanceTiles) * tileSize,
		sink:      sink,
		committer: c,
		pending:   make(map[world.PlayerID]world.PlayerID),
		byPlayer:  make(map[world.PlayerID]*session),

		commits:    make(map[CommitID]*inflight),
		committing: make(map[world.PlayerID]CommitID),
	}
}

func (s *System) Name() string { return "trade" }

// Trading reports whether p has a trade window open.
func (s *System) Trading(p world.PlayerID) bool { return s.byPlayer[p] != nil }

// Committing reports whether p's trade is being saved. Their inventory is
// locked and their trade commands are ignored until the result is applied.
func (s *System) Committing(p world.PlayerID) bool {
	_, ok := s.committing[p]
	return ok
}

// Resolve reports the outcome of commit id: ok if the database transaction
// committed. The next Tick applies it. Safe for concurrent use; unknown or
// repeated ids are ignored.
func (s *System) Resolve(id CommitID, ok bool) {
	s.rmu.Lock()
	s.resolved = append(s.resolved, resolution{id, ok})
	s.rmu.Unlock()
}

// Handle applies a trade command for p. Commands from a player whose trade
// is being saved are ignored.
func (s *System) Handle(cmd world.Command, p *world.Player) {
	if s.Committing(p.ID) {
		return
	}
	switch c := cmd.(type) {
	case RequestCommand:
		s.requests = append(s.requests, c)
	case OfferCommand:
		s.offer(p, c.Slot, c.Quantity)
	case RemoveCommand:
		s.remove(p, c.Index)
	case SetGoldCommand:
		s.setGold(p, c.Gold)
	case AcceptCommand:
		s.accept(p.ID, c.Screen)
	case CancelCommand:
		delete(s.pending, p.ID)
		if t := s.byPlayer[p.ID]; t != nil {
			s.close(t, Cancelled)
		}
	}
}

// Tick applies the commit results reported since the last Tick, settles
// this tick's requests, then closes every trade whose players left the world,
// entered the PvP zone or moved too far apart, then commits every trade both
// players accepted on screen 2. Committing here rather than in Handle means a
// player who left or moved since the last Tick can't complete a trade.
func (s *System) Tick(w *world.World, _ time.Duration) {
	s.rmu.Lock()
	resolved := s.resolved
	s.resolved = nil
	s.rmu.Unlock()
	for _, r := range resolved {
		s.finish(w, r.id, r.ok)
	}

	for _, r := range s.requests {
		s.request(w, r.Player, r.Partner)
	}
	clear(s.requests)
	s.requests = s.requests[:0]

	for from, to := range s.pending {
		if w.Player(from) == nil || w.Player(to) == nil {
			delete(s.pending, from)
		}
	}

	for i := 0; i < len(s.open); {
		t := s.open[i]
		a, b := w.Player(t.sides[0].id), w.Player(t.sides[1].id)
		r, bad := s.check(a, b)
		if a == nil || b == nil {
			r, bad = Cancelled, true
		}
		if bad {
			s.close(t, r) // removes s.open[i]
			continue
		}
		if t.screen == ScreenConfirm && t.sides[0].accepted && t.sides[1].accepted {
			s.forget(t) // removes s.open[i]
			s.commit(a, b, t.sides[0].offer.clone(), t.sides[1].offer.clone())
			continue
		}
		i++
	}
}

// InReach reports whether a and b are within the trade distance of each other
// (the boundary counts as inside).
func (s *System) InReach(a, b world.Vec2) bool {
	dx := float64(a.X) - float64(b.X)
	dy := float64(a.Y) - float64(b.Y)
	return dx*dx+dy*dy <= s.reach*s.reach
}

// check reports why a and b may not trade, if they may not. Nil players pass.
func (s *System) check(a, b *world.Player) (Result, bool) {
	if a == nil || b == nil {
		return 0, false
	}
	if s.areas.InPvP(a.Pos) || s.areas.InPvP(b.Pos) {
		return InPvpZone, true
	}
	if !s.InReach(a.Pos, b.Pos) {
		return TooFar, true
	}
	return 0, false
}

func (s *System) request(w *world.World, from, to world.PlayerID) {
	a, b := w.Player(from), w.Player(to)
	if a == nil || b == nil || from == to || s.Trading(from) {
		return
	}
	if r, bad := s.check(a, b); bad {
		s.sink.TradeResult(from, r)
		return
	}
	if s.Trading(to) || s.Committing(to) {
		s.sink.TradeResult(from, PartnerBusy)
		return
	}
	if s.pending[to] != from {
		s.pending[from] = to
		s.sink.TradeRequestNotice(to, from)
		return
	}
	delete(s.pending, from)
	delete(s.pending, to)
	t := &session{sides: [2]side{{id: to}, {id: from}}, screen: ScreenOffer}
	s.open = append(s.open, t)
	s.byPlayer[from], s.byPlayer[to] = t, t
	s.send(t)
}

func (s *System) offer(p *world.Player, index, qty int) {
	t := s.byPlayer[p.ID]
	if t == nil || index < 0 || index >= len(p.Inv.Slots) || qty <= 0 {
		return
	}
	slot := p.Inv.Slots[index]
	if slot.Empty() {
		return
	}
	me, _ := t.of(p.ID)
	at := -1
	for i, it := range me.offer.Items {
		if it.Slot == index && it.Item == slot.Item {
			at = i
			break
		}
	}
	offered := 0
	if at >= 0 {
		offered = me.offer.Items[at].Qty
	}
	if qty > slot.Qty-offered {
		return
	}
	if at >= 0 {
		me.offer.Items[at].Qty += qty
	} else {
		me.offer.Items = append(me.offer.Items, Item{Slot: index, Item: slot.Item, Qty: qty})
	}
	s.changed(t)
}

func (s *System) remove(p *world.Player, index int) {
	t := s.byPlayer[p.ID]
	if t == nil {
		return
	}
	me, _ := t.of(p.ID)
	if index < 0 || index >= len(me.offer.Items) {
		return
	}
	me.offer.Items = append(me.offer.Items[:index], me.offer.Items[index+1:]...)
	s.changed(t)
}

func (s *System) setGold(p *world.Player, gold int) {
	t := s.byPlayer[p.ID]
	if t == nil || gold < 0 || gold > p.Inv.Gold {
		return
	}
	me, _ := t.of(p.ID)
	me.offer.Gold = gold
	s.changed(t)
}

func (s *System) accept(p world.PlayerID, screen int) {
	t := s.byPlayer[p]
	if t == nil || screen != t.screen {
		return
	}
	me, other := t.of(p)
	if me.accepted {
		return
	}
	me.accepted = true
	if !other.accepted {
		s.send(t)
		return
	}
	if t.screen == ScreenOffer {
		t.screen = ScreenConfirm
		t.sides[0].accepted, t.sides[1].accepted = false, false
		s.send(t)
	}
	// Both accepted screen 2: Tick commits the trade.
}

// changed returns t to screen 1 with both accepts cleared and tells both
// players.
func (s *System) changed(t *session) {
	t.screen = ScreenOffer
	t.sides[0].accepted, t.sides[1].accepted = false, false
	s.send(t)
}

func (s *System) send(t *session) {
	for i := range t.sides {
		me, other := &t.sides[i], &t.sides[1-i]
		s.sink.TradeState(me.id, View{
			Partner:         other.id,
			Screen:          t.screen,
			Mine:            me.offer.clone(),
			Partners:        other.offer.clone(),
			MyAccepted:      me.accepted,
			PartnerAccepted: other.accepted,
		})
	}
}

// close ends t and sends r to both players.
func (s *System) close(t *session, r Result) {
	s.forget(t)
	s.sink.TradeResult(t.sides[0].id, r)
	s.sink.TradeResult(t.sides[1].id, r)
}

// forget forgets t without telling anyone.
func (s *System) forget(t *session) {
	delete(s.byPlayer, t.sides[0].id)
	delete(s.byPlayer, t.sides[1].id)
	for i, o := range s.open {
		if o == t {
			s.open = append(s.open[:i], s.open[i+1:]...)
			break
		}
	}
}

// commit re-checks both offers against x's and y's inventories and works out
// both resulting inventories. If either side no longer holds what it offered
// both players get OfferNotHeld; if either side has no room for what it would
// receive both get InventoryFull; in both cases nothing is committed. If the
// trade holds, both inventories are locked and the Committer gets it.
func (s *System) commit(x, y *world.Player, ox, oy Offer) {
	if !holds(&x.Inv, ox) || !holds(&y.Inv, oy) {
		s.sink.TradeResult(x.ID, OfferNotHeld)
		s.sink.TradeResult(y.ID, OfferNotHeld)
		return
	}
	invX, chX, errX := s.swap(&x.Inv, ox, oy)
	invY, chY, errY := s.swap(&y.Inv, oy, ox)
	if errX != nil || errY != nil {
		s.sink.TradeResult(x.ID, InventoryFull)
		s.sink.TradeResult(y.ID, InventoryFull)
		return
	}
	s.nextID++
	req := CommitRequest{
		ID: s.nextID,
		A:  Side{Player: x.ID, Gave: ox, Inv: invX, Changes: chX},
		B:  Side{Player: y.ID, Gave: oy, Inv: invY, Changes: chY},
	}
	x.Inv.SetLocked(true)
	y.Inv.SetLocked(true)
	s.commits[req.ID] = &inflight{req: req, players: [2]*world.Player{x, y}}
	s.committing[x.ID], s.committing[y.ID] = req.ID, req.ID
	// The Committer gets its own copies, so nothing it does reaches the
	// inventories applied later.
	reqA, reqB := req.A, req.B
	reqA.Inv, reqB.Inv = reqA.Inv.Clone(), reqB.Inv.Clone()
	reqA.Gave, reqB.Gave = reqA.Gave.clone(), reqB.Gave.clone()
	reqA.Changes = append([]world.Change(nil), reqA.Changes...)
	reqB.Changes = append([]world.Change(nil), reqB.Changes...)
	s.committer.CommitTrade(CommitRequest{ID: req.ID, A: reqA, B: reqB})
}

// finish applies the result of commit id: on success both resulting
// inventories, each with an economy outbox event, and Completed; on failure
// nothing but Cancelled. Both players are unlocked either way. A player who
// left the world since the commit is skipped.
func (s *System) finish(w *world.World, id CommitID, ok bool) {
	c := s.commits[id]
	if c == nil {
		return
	}
	delete(s.commits, id)
	r := Cancelled
	if ok {
		r = Completed
	}
	for i, side := range [2]Side{c.req.A, c.req.B} {
		p := c.players[i]
		delete(s.committing, side.Player)
		p.Inv.SetLocked(false)
		if w.Player(side.Player) != p {
			continue // left the world (and maybe came back as a new entity)
		}
		if ok {
			p.Inv = side.Inv
			p.Inv.SetLocked(false)
			p.RecordEconomy(ReasonTrade, side.Changes)
		}
		s.sink.TradeResult(side.Player, r)
	}
}

// itemTotals returns the quantity of each item in o, in first-offered order.
func itemTotals(o Offer) []Item {
	var out []Item
next:
	for _, it := range o.Items {
		for i := range out {
			if out[i].Item == it.Item {
				out[i].Qty += it.Qty
				continue next
			}
		}
		out = append(out, Item{Item: it.Item, Qty: it.Qty})
	}
	return out
}

// holds reports whether inv still holds everything o offers.
func holds(inv *world.Inventory, o Offer) bool {
	if o.Gold < 0 || o.Gold > inv.Gold {
		return false
	}
	for _, it := range itemTotals(o) {
		if it.Qty <= 0 || inv.Count(it.Item) < it.Qty {
			return false
		}
	}
	return true
}

// swap returns inv with give taken out and get put in, under the slot and
// stacking rules, and the changes that makes. inv itself is not changed.
func (s *System) swap(inv *world.Inventory, give, get Offer) (world.Inventory, []world.Change, error) {
	out := inv.Clone()
	var changes []world.Change
	do := func(ch []world.Change, err error) error {
		changes = append(changes, ch...)
		return err
	}
	for _, it := range itemTotals(give) {
		if err := do(out.Remove(it.Item, it.Qty)); err != nil {
			return world.Inventory{}, nil, err
		}
	}
	if give.Gold > 0 {
		if err := do(out.SpendGold(give.Gold)); err != nil {
			return world.Inventory{}, nil, err
		}
	}
	for _, it := range itemTotals(get) {
		if err := do(out.Add(s.data, it.Item, it.Qty)); err != nil {
			return world.Inventory{}, nil, err
		}
	}
	if get.Gold > 0 {
		if err := do(out.AddGold(get.Gold)); err != nil {
			return world.Inventory{}, nil, err
		}
	}
	return out, changes, nil
}
