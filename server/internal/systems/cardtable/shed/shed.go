// Package shed is Shed, the first card game (docs/specs/card-tables.md), as a
// cardtable rules module. It is pure game logic: no I/O, no wall-clock time
// and no random source of its own. Every shuffled pile is built with
// deck.Shuffled, which uses crypto/rand.
package shed

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/systems/cardtable/deck"
)

// Colour is a card's colour. The values match protocol ShedColour.
type Colour uint8

const (
	NoColour Colour = 0 // Wild and Wild Draw Four cards
	Colour1  Colour = 1
	Colour2  Colour = 2
	Colour3  Colour = 3
	Colour4  Colour = 4
)

// Colours is every playable colour, in order.
var Colours = [...]Colour{Colour1, Colour2, Colour3, Colour4}

// Symbol is a card's symbol. The values match protocol ShedSymbol; 0 to 9
// are the number cards.
type Symbol uint8

const (
	Skip         Symbol = 10
	Reverse      Symbol = 11
	DrawTwo      Symbol = 12
	Wild         Symbol = 13
	WildDrawFour Symbol = 14
)

// Card is one Shed card.
type Card struct {
	Colour Colour
	Symbol Symbol
}

// IsWild reports whether c is a Wild or Wild Draw Four.
func (c Card) IsWild() bool { return c.Symbol == Wild || c.Symbol == WildDrawFour }

func (c Card) String() string { return fmt.Sprintf("%d/%d", c.Colour, c.Symbol) }

// id encodes c as a deck card so it can be shuffled.
func (c Card) id() deck.Card { return deck.Card(c.Colour)<<8 | deck.Card(c.Symbol) }

func fromID(id deck.Card) Card { return Card{Colour: Colour(id >> 8), Symbol: Symbol(id & 0xff)} }

// HandSize is how many cards each seat is dealt.
const HandSize = 7

// FullDeck returns the 108-card Shed deck in a fixed order: per colour one 0,
// two each of 1 to 9, Skip, Reverse and Draw Two; then 4 Wild and 4 Wild
// Draw Four.
func FullDeck() []Card {
	cards := make([]Card, 0, 108)
	for _, c := range Colours {
		cards = append(cards, Card{c, 0})
		for s := Symbol(1); s <= DrawTwo; s++ {
			cards = append(cards, Card{c, s}, Card{c, s})
		}
	}
	for range 4 {
		cards = append(cards, Card{NoColour, Wild}, Card{NoColour, WildDrawFour})
	}
	return cards
}

// Play plays Card from the seat's hand. Chosen is the colour to match next
// when Card is a Wild or Wild Draw Four, and is ignored otherwise.
type Play struct {
	Card   Card
	Chosen Colour
}

// Draw draws one card; the turn passes.
type Draw struct{}

// Errors returned by Move. A rejected move leaves the game unchanged.
var (
	ErrNoGame        = errors.New("shed: no game in progress")
	ErrNotInGame     = errors.New("shed: seat is not in the game")
	ErrNotInHand     = errors.New("shed: card is not in the seat's hand")
	ErrNoMatch       = errors.New("shed: card does not match the top card")
	ErrBadColour     = errors.New("shed: a wild needs a chosen colour")
	ErrUnknownMove   = errors.New("shed: unknown move")
	ErrTooFewPlayers = errors.New("shed: need at least 2 seats")
	ErrNoStartCard   = errors.New("shed: no card can start the discard pile")
)

// PublicView is the table state every viewer may see. It holds no cards
// from any hand, only how many each seat has.
type PublicView struct {
	Top        Card
	Colour     Colour // the colour to match (the chosen colour after a wild)
	HandCounts map[cardtable.Seat]int
	DrawPile   int
}

// Game is one Shed game. Only the table, on the world tick, calls its
// methods. The zero value is ready for Start.
type Game struct {
	started bool
	order   []cardtable.Seat // seats still in the game, in seat order
	dir     int              // +1 or -1, mirroring the table
	hands   map[cardtable.Seat][]Card
	draw    []Card // index 0 is the top
	discard []Card // the last card is the face-up top card
	colour  Colour // colour to match

	over   bool
	winner cardtable.Seat
}

var _ cardtable.Rules = (*Game)(nil)

// New returns a Shed rules module ready for a table.
func New() *Game { return &Game{} }

// MinPlayers is 2.
func (g *Game) MinPlayers() int { return 2 }

// shuffled returns cards in a random order. It is the only way this package
// orders a pile.
func shuffled(cards []Card) []Card {
	ids := make([]deck.Card, len(cards))
	for i, c := range cards {
		ids[i] = c.id()
	}
	p := deck.Shuffled(ids)
	// Drawing every card a pile holds can't fail.
	ids, _ = p.Draw(p.Len())
	out := make([]Card, len(ids))
	for i, id := range ids {
		out[i] = fromID(id)
	}
	return out
}

// Start shuffles a full deck, deals HandSize cards to each seat and turns
// the top card face up. A Wild or Wild Draw Four turned up goes back into
// the draw pile, which is shuffled again, and another card is turned; a
// starting Skip, Reverse or Draw Two has no effect.
func (g *Game) Start(seats []cardtable.Seat) error {
	if len(seats) < 2 {
		return ErrTooFewPlayers
	}
	return g.start(seats, shuffled(FullDeck()))
}

// start deals from draw, top first, as Start does.
func (g *Game) start(seats []cardtable.Seat, draw []Card) error {
	if len(draw) < len(seats)*HandSize+1 {
		return fmt.Errorf("shed: deal %d seats from %d cards: %w", len(seats), len(draw), deck.ErrNotEnoughCards)
	}
	hands := make(map[cardtable.Seat][]Card, len(seats))
	for i := range len(seats) * HandSize {
		s := seats[i%len(seats)]
		hands[s] = append(hands[s], draw[i])
	}
	draw = draw[len(seats)*HandSize:]
	if !slices.ContainsFunc(draw, func(c Card) bool { return !c.IsWild() }) {
		return ErrNoStartCard
	}
	for draw[0].IsWild() {
		draw = shuffled(draw)
	}
	*g = Game{
		started: true,
		order:   slices.Clone(seats),
		dir:     1,
		hands:   hands,
		draw:    slices.Clone(draw[1:]),
		discard: []Card{draw[0]},
		colour:  draw[0].Colour,
	}
	return nil
}

// Top is the face-up top card of the discard pile.
func (g *Game) Top() Card { return g.discard[len(g.discard)-1] }

// Playable reports whether c may be played on the current top card.
func (g *Game) Playable(c Card) bool {
	return c.IsWild() || c.Colour == g.colour || c.Symbol == g.Top().Symbol
}

// Move applies a Play or Draw from seat, whose turn it is.
func (g *Game) Move(seat cardtable.Seat, move any) (cardtable.Outcome, error) {
	if !g.started || g.over {
		return cardtable.Outcome{}, ErrNoGame
	}
	hand, ok := g.hands[seat]
	if !ok {
		return cardtable.Outcome{}, ErrNotInGame
	}
	switch m := move.(type) {
	case Draw:
		g.drawInto(seat, 1)
		return cardtable.Outcome{}, nil
	case Play:
		i := slices.Index(hand, m.Card)
		if i < 0 {
			return cardtable.Outcome{}, fmt.Errorf("play %v: %w", m.Card, ErrNotInHand)
		}
		if !g.Playable(m.Card) {
			return cardtable.Outcome{}, fmt.Errorf("play %v on %v: %w", m.Card, g.Top(), ErrNoMatch)
		}
		if m.Card.IsWild() && (m.Chosen < Colour1 || m.Chosen > Colour4) {
			return cardtable.Outcome{}, fmt.Errorf("play %v choosing %d: %w", m.Card, m.Chosen, ErrBadColour)
		}
		g.hands[seat] = slices.Delete(hand, i, i+1)
		g.discard = append(g.discard, m.Card)
		g.colour = m.Card.Colour
		if m.Card.IsWild() {
			g.colour = m.Chosen
		}
		if len(g.hands[seat]) == 0 {
			g.over, g.winner = true, seat
			return cardtable.Outcome{}, nil
		}
		return g.effect(seat, m.Card.Symbol), nil
	default:
		return cardtable.Outcome{}, fmt.Errorf("%w: %T", ErrUnknownMove, move)
	}
}

// effect applies the action of a card seat just played and returns how the
// turn passes.
func (g *Game) effect(seat cardtable.Seat, s Symbol) cardtable.Outcome {
	switch s {
	case Skip:
		return cardtable.Outcome{Skip: 1}
	case Reverse:
		if len(g.order) == 2 {
			return cardtable.Outcome{Skip: 1}
		}
		g.dir = -g.dir
		return cardtable.Outcome{Reverse: true}
	case DrawTwo:
		g.drawInto(g.next(seat), 2)
		return cardtable.Outcome{Skip: 1}
	case WildDrawFour:
		g.drawInto(g.next(seat), 4)
		return cardtable.Outcome{Skip: 1}
	}
	return cardtable.Outcome{}
}

// next is the seat after seat in the current direction.
func (g *Game) next(seat cardtable.Seat) cardtable.Seat {
	n := len(g.order)
	i := slices.Index(g.order, seat)
	return g.order[((i+g.dir)%n+n)%n]
}

// drawInto moves up to n cards from the draw pile into seat's hand, one at a
// time. When the draw pile runs out, the discard pile except its top card is
// shuffled into a new draw pile. If no card is left even then, the seat
// draws nothing more.
func (g *Game) drawInto(seat cardtable.Seat, n int) {
	for range n {
		if len(g.draw) == 0 {
			g.reshuffle()
		}
		if len(g.draw) == 0 {
			return
		}
		g.hands[seat] = append(g.hands[seat], g.draw[0])
		g.draw = g.draw[1:]
	}
}

// reshuffle shuffles the discard pile except its top card into a new draw
// pile, under any cards still in the draw pile.
func (g *Game) reshuffle() {
	top := len(g.discard) - 1
	if top == 0 {
		return
	}
	g.draw = append(g.draw, shuffled(g.discard[:top])...)
	g.discard = []Card{g.discard[top]}
}

// Timeout draws one card for seat; the turn passes.
func (g *Game) Timeout(seat cardtable.Seat) cardtable.Outcome {
	if g.started && !g.over {
		if _, ok := g.hands[seat]; ok {
			g.drawInto(seat, 1)
		}
	}
	return cardtable.Outcome{}
}

// Forfeit removes seat from play. Its hand is shuffled into the draw pile so
// no card leaves the game.
func (g *Game) Forfeit(seat cardtable.Seat) {
	hand, ok := g.hands[seat]
	if !ok {
		return
	}
	delete(g.hands, seat)
	g.order = slices.DeleteFunc(g.order, func(s cardtable.Seat) bool { return s == seat })
	if len(hand) > 0 {
		g.draw = shuffled(append(slices.Clone(g.draw), hand...))
	}
}

// PublicView returns a PublicView, or nil before the first Start.
func (g *Game) PublicView() any {
	if !g.started {
		return nil
	}
	counts := make(map[cardtable.Seat]int, len(g.hands))
	for s, h := range g.hands {
		counts[s] = len(h)
	}
	return PublicView{Top: g.Top(), Colour: g.colour, HandCounts: counts, DrawPile: len(g.draw)}
}

// PrivateView returns a copy of seat's hand as []Card, or nil if seat is not
// in the game.
func (g *Game) PrivateView(seat cardtable.Seat) any {
	h, ok := g.hands[seat]
	if !ok {
		return nil
	}
	return slices.Clone(h)
}

// Over reports the first seat to empty its hand.
func (g *Game) Over() (cardtable.Seat, bool) { return g.winner, g.over }
