// Package trade is the player trade window (docs/specs/gameplay.md, Player
// trading): two players within Rules.TradeDistanceTiles of each other and
// outside the PvP zone request each other, offer items and gold, and accept
// two screens as in OSRS. Every change goes to both players through a Sink,
// and a trade both players accepted on screen 2 is handed to a Committer.
//
// The system only reads inventories: it never moves items or gold. Moving
// them, and re-checking the offers at that moment, is the Committer's job.
package trade

import (
	"fmt"
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

// Committer receives a trade both players accepted on screen 2. The window
// is already closed when it is called; the Committer saves the swap and
// reports the outcome (Completed or a failure) to both players. It is called
// on the tick goroutine and must not block.
type Committer interface {
	CommitTrade(a, b world.PlayerID, offerA, offerB Offer)
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
	areas     *areas.Lookup
	reach     float64 // metres
	sink      Sink
	committer Committer

	requests []RequestCommand                  // queued this tick
	pending  map[world.PlayerID]world.PlayerID // requester -> partner
	open     []*session                        // in opening order
	byPlayer map[world.PlayerID]*session
}

// New creates the trade system. Rules.TradeDistanceTiles comes from d and the
// PvP zone from l; tileSize is the length of a tile in metres (the collision
// grid's cell size).
func New(d *gamedata.Data, l *areas.Lookup, tileSize float64, sink Sink, c Committer) *System {
	return &System{
		areas:     l,
		reach:     float64(d.Rules.TradeDistanceTiles) * tileSize,
		sink:      sink,
		committer: c,
		pending:   make(map[world.PlayerID]world.PlayerID),
		byPlayer:  make(map[world.PlayerID]*session),
	}
}

func (s *System) Name() string { return "trade" }

// Trading reports whether p has a trade window open.
func (s *System) Trading(p world.PlayerID) bool { return s.byPlayer[p] != nil }

// Handle applies a trade command for p.
func (s *System) Handle(cmd world.Command, p *world.Player) {
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

// Tick settles this tick's requests, then closes every trade whose players
// left the world, entered the PvP zone or moved too far apart.
func (s *System) Tick(w *world.World, _ time.Duration) {
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
	if s.Trading(to) {
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
		return
	}
	a, b := t.sides[0], t.sides[1]
	s.forget(t)
	s.committer.CommitTrade(a.id, b.id, a.offer.clone(), b.offer.clone())
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
