package shed

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/world"
)

type seat = cardtable.Seat

func c(col Colour, s Symbol) Card { return Card{col, s} }

var (
	wild = c(NoColour, Wild)
	wd4  = c(NoColour, WildDrawFour)
)

// setup builds a game in progress with the given hands (seats in the game
// are the keys, in seat order), draw pile (top first) and discard pile (top
// last). The colour to match is the top card's, or Colour1 for a wild top.
func setup(hands map[seat][]Card, draw, discard []Card) *Game {
	g := &Game{started: true, dir: 1, hands: map[seat][]Card{}}
	for s, h := range hands {
		g.order = append(g.order, s)
		g.hands[s] = slices.Clone(h)
	}
	slices.Sort(g.order)
	g.draw = slices.Clone(draw)
	g.discard = slices.Clone(discard)
	g.colour = g.Top().Colour
	if g.colour == NoColour {
		g.colour = Colour1
	}
	return g
}

// clone deep-copies a game so a test can check it is unchanged.
func clone(g *Game) *Game {
	cp := *g
	cp.order = slices.Clone(g.order)
	cp.draw = slices.Clone(g.draw)
	cp.discard = slices.Clone(g.discard)
	cp.hands = map[seat][]Card{}
	for s, h := range g.hands {
		cp.hands[s] = slices.Clone(h)
	}
	return &cp
}

func sorted(cards []Card) []Card {
	out := slices.Clone(cards)
	slices.SortFunc(out, func(a, b Card) int { return int(a.id()) - int(b.id()) })
	return out
}

// allCards is every card in the game: hands, draw and discard piles.
func allCards(g *Game) []Card {
	all := append(slices.Clone(g.draw), g.discard...)
	for _, h := range g.hands {
		all = append(all, h...)
	}
	return sorted(all)
}

func TestFullDeck(t *testing.T) {
	d := FullDeck()
	if len(d) != 108 {
		t.Fatalf("deck has %d cards, want 108", len(d))
	}
	count := map[Card]int{}
	for _, card := range d {
		count[card]++
	}
	want := map[Card]int{wild: 4, wd4: 4}
	for _, col := range Colours {
		want[c(col, 0)] = 1
		for s := Symbol(1); s <= 9; s++ {
			want[c(col, s)] = 2
		}
		want[c(col, Skip)] = 2
		want[c(col, Reverse)] = 2
		want[c(col, DrawTwo)] = 2
	}
	if !reflect.DeepEqual(count, want) {
		t.Errorf("deck counts = %v, want %v", count, want)
	}
}

func TestCardIDRoundTrip(t *testing.T) {
	for _, card := range FullDeck() {
		if got := fromID(card.id()); got != card {
			t.Errorf("fromID(id(%v)) = %v", card, got)
		}
	}
}

func TestStart(t *testing.T) {
	for _, n := range []int{2, 3, 4} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			var seats []seat
			for i := range n {
				seats = append(seats, seat(i))
			}
			g := New()
			if err := g.Start(seats); err != nil {
				t.Fatal(err)
			}
			for _, s := range seats {
				if len(g.hands[s]) != HandSize {
					t.Errorf("seat %d has %d cards, want %d", s, len(g.hands[s]), HandSize)
				}
			}
			if len(g.discard) != 1 || g.Top().IsWild() {
				t.Errorf("discard pile = %v, want one non-wild card", g.discard)
			}
			if g.colour != g.Top().Colour {
				t.Errorf("colour %d, want top card's %d", g.colour, g.Top().Colour)
			}
			if want := 108 - n*HandSize - 1; len(g.draw) != want {
				t.Errorf("draw pile has %d cards, want %d", len(g.draw), want)
			}
			if got := allCards(g); !slices.Equal(got, sorted(FullDeck())) {
				t.Errorf("cards in play are not the full deck")
			}
		})
	}
}

func TestStartRejectsOneSeat(t *testing.T) {
	if err := New().Start([]seat{0}); !errors.Is(err, ErrTooFewPlayers) {
		t.Errorf("Start with one seat: %v, want ErrTooFewPlayers", err)
	}
}

// fixedDeal is 14 number cards for two seats, then the given cards.
func fixedDeal(rest ...Card) []Card {
	var d []Card
	for i := range 14 {
		d = append(d, c(Colour1+Colour(i%4), Symbol(i%10)))
	}
	return append(d, rest...)
}

// TestStartingCard checks Q3: a wild turned up goes back into the draw pile
// and another card is turned; a starting action card has no effect.
func TestStartingCard(t *testing.T) {
	tests := []struct {
		name string
		rest []Card
	}{
		{"number", []Card{c(Colour2, 5), c(Colour3, 6)}},
		{"wild", []Card{wild, c(Colour2, 5)}},
		{"wild draw four", []Card{wd4, wild, c(Colour2, 5)}},
		{"skip", []Card{c(Colour2, Skip), c(Colour3, 6)}},
		{"reverse", []Card{c(Colour2, Reverse), c(Colour3, 6)}},
		{"draw two", []Card{c(Colour2, DrawTwo), c(Colour3, 6)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New()
			if err := g.start([]seat{0, 1}, fixedDeal(tt.rest...)); err != nil {
				t.Fatal(err)
			}
			if g.Top().IsWild() {
				t.Fatalf("top card %v is wild", g.Top())
			}
			if g.colour != g.Top().Colour {
				t.Errorf("colour %d, want %d", g.colour, g.Top().Colour)
			}
			for _, s := range []seat{0, 1} {
				if len(g.hands[s]) != HandSize {
					t.Errorf("seat %d has %d cards, want %d", s, len(g.hands[s]), HandSize)
				}
			}
			if g.dir != 1 {
				t.Errorf("direction %d, want 1", g.dir)
			}
			want := sorted(tt.rest)
			if got := sorted(append(slices.Clone(g.draw), g.Top())); !slices.Equal(got, want) {
				t.Errorf("draw pile plus top = %v, want %v", got, want)
			}
			for _, w := range tt.rest {
				if w.IsWild() && !slices.Contains(g.draw, w) {
					t.Errorf("turned-up %v not back in the draw pile %v", w, g.draw)
				}
			}
		})
	}
}

func TestStartWithOnlyWildsLeft(t *testing.T) {
	err := New().start([]seat{0, 1}, fixedDeal(wild, wd4))
	if !errors.Is(err, ErrNoStartCard) {
		t.Errorf("start: %v, want ErrNoStartCard", err)
	}
}

func TestPlay(t *testing.T) {
	hand := []Card{c(Colour1, 3), c(Colour2, 7), c(Colour3, 3), wild, wd4, c(Colour4, Skip), c(Colour2, 9)}
	top := c(Colour1, 7)
	tests := []struct {
		name       string
		move       any
		wantErr    error
		wantColour Colour
	}{
		{"same colour", Play{Card: c(Colour1, 3)}, nil, Colour1},
		{"same symbol", Play{Card: c(Colour2, 7)}, nil, Colour2},
		{"wild", Play{Card: wild, Chosen: Colour3}, nil, Colour3},
		{"wild draw four", Play{Card: wd4, Chosen: Colour4}, nil, Colour4},
		{"chosen colour ignored on a normal card", Play{Card: c(Colour1, 3), Chosen: Colour4}, nil, Colour1},
		{"no match", Play{Card: c(Colour3, 3)}, ErrNoMatch, 0},
		{"action no match", Play{Card: c(Colour4, Skip)}, ErrNoMatch, 0},
		{"not held", Play{Card: c(Colour1, 8)}, ErrNotInHand, 0},
		{"not held but matches symbol", Play{Card: c(Colour3, 7)}, ErrNotInHand, 0},
		{"wild without colour", Play{Card: wild}, ErrBadColour, 0},
		{"wild with bad colour", Play{Card: wd4, Chosen: 5}, ErrBadColour, 0},
		{"unknown move", "pass", ErrUnknownMove, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := setup(map[seat][]Card{0: hand, 1: {c(Colour1, 1)}}, []Card{c(Colour4, 4), c(Colour4, 5), c(Colour4, 6), c(Colour4, 8)}, []Card{top})
			before := clone(g)
			_, err := g.Move(0, tt.move)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Move: %v, want %v", err, tt.wantErr)
				}
				if !reflect.DeepEqual(g, before) {
					t.Errorf("state changed after a rejected move:\n got %+v\nwant %+v", g, before)
				}
				return
			}
			if err != nil {
				t.Fatalf("Move: %v", err)
			}
			p := tt.move.(Play)
			if g.Top() != p.Card {
				t.Errorf("top = %v, want %v", g.Top(), p.Card)
			}
			if g.colour != tt.wantColour {
				t.Errorf("colour = %d, want %d", g.colour, tt.wantColour)
			}
			if len(g.hands[0]) != len(hand)-1 || slices.Contains(g.hands[0], p.Card) {
				t.Errorf("hand after play = %v", g.hands[0])
			}
		})
	}
}

func TestPlayMatchesChosenColourAfterWild(t *testing.T) {
	g := setup(map[seat][]Card{0: {c(Colour3, 2), c(Colour3, 4)}, 1: {c(Colour1, 2), c(Colour2, 2)}}, nil, []Card{wild})
	g.colour = Colour2
	if _, err := g.Move(1, Play{Card: c(Colour1, 2)}); !errors.Is(err, ErrNoMatch) {
		t.Errorf("off-colour card on a wild: %v, want ErrNoMatch", err)
	}
	if _, err := g.Move(1, Play{Card: c(Colour2, 2)}); err != nil {
		t.Errorf("chosen-colour card on a wild: %v", err)
	}
}

func TestDrawMove(t *testing.T) {
	g := setup(map[seat][]Card{0: {c(Colour1, 1)}, 1: {c(Colour1, 2)}}, []Card{c(Colour2, 5), c(Colour2, 6)}, []Card{c(Colour3, 9)})
	out, err := g.Move(0, Draw{})
	if err != nil {
		t.Fatal(err)
	}
	if out != (cardtable.Outcome{}) {
		t.Errorf("outcome %+v, want the turn to pass normally", out)
	}
	if want := []Card{c(Colour1, 1), c(Colour2, 5)}; !slices.Equal(g.hands[0], want) {
		t.Errorf("hand %v, want %v", g.hands[0], want)
	}
	if want := []Card{c(Colour2, 6)}; !slices.Equal(g.draw, want) {
		t.Errorf("draw pile %v, want %v", g.draw, want)
	}
	if len(g.hands[1]) != 1 {
		t.Errorf("other seat's hand changed: %v", g.hands[1])
	}
}

func TestMoveFromSeatNotInGame(t *testing.T) {
	g := setup(map[seat][]Card{0: {c(Colour1, 1)}, 1: {c(Colour1, 2)}}, nil, []Card{c(Colour1, 9)})
	before := clone(g)
	if _, err := g.Move(2, Draw{}); !errors.Is(err, ErrNotInGame) {
		t.Errorf("Move from seat 2: %v, want ErrNotInGame", err)
	}
	if !reflect.DeepEqual(g, before) {
		t.Error("state changed")
	}
	if _, err := New().Move(0, Draw{}); !errors.Is(err, ErrNoGame) {
		t.Errorf("Move before Start: %v, want ErrNoGame", err)
	}
}

func TestActionCards(t *testing.T) {
	filler := []Card{c(Colour4, 1), c(Colour4, 2)}
	tests := []struct {
		name     string
		seats    int
		reversed bool
		card     Card
		want     cardtable.Outcome
		wantDir  int
		victim   seat // seat that must draw, or -1
		draws    int
	}{
		{"number", 3, false, c(Colour1, 5), cardtable.Outcome{}, 1, -1, 0},
		{"skip", 3, false, c(Colour1, Skip), cardtable.Outcome{Skip: 1}, 1, -1, 0},
		{"reverse 3 players", 3, false, c(Colour1, Reverse), cardtable.Outcome{Reverse: true}, -1, -1, 0},
		{"reverse back", 3, true, c(Colour1, Reverse), cardtable.Outcome{Reverse: true}, 1, -1, 0},
		{"reverse 2 players acts as skip", 2, false, c(Colour1, Reverse), cardtable.Outcome{Skip: 1}, 1, -1, 0},
		{"draw two", 3, false, c(Colour1, DrawTwo), cardtable.Outcome{Skip: 1}, 1, 1, 2},
		{"draw two reversed", 3, true, c(Colour1, DrawTwo), cardtable.Outcome{Skip: 1}, -1, 2, 2},
		{"draw two 2 players", 2, false, c(Colour1, DrawTwo), cardtable.Outcome{Skip: 1}, 1, 1, 2},
		{"wild draw four", 3, false, wd4, cardtable.Outcome{Skip: 1}, 1, 1, 4},
		{"wild draw four reversed", 3, true, wd4, cardtable.Outcome{Skip: 1}, -1, 2, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hands := map[seat][]Card{}
			for s := range tt.seats {
				hands[seat(s)] = []Card{c(Colour3, 1)}
			}
			hands[0] = []Card{tt.card, c(Colour3, 2)}
			draw := []Card{c(Colour2, 1), c(Colour2, 2), c(Colour2, 3), c(Colour2, 4), c(Colour2, 5)}
			g := setup(hands, draw, append(slices.Clone(filler), c(Colour1, 0)))
			if tt.reversed {
				g.dir = -1
			}
			out, err := g.Move(0, Play{Card: tt.card, Chosen: Colour2})
			if err != nil {
				t.Fatal(err)
			}
			if out != tt.want {
				t.Errorf("outcome %+v, want %+v", out, tt.want)
			}
			if g.dir != tt.wantDir {
				t.Errorf("direction %d, want %d", g.dir, tt.wantDir)
			}
			for s := range tt.seats {
				want := 1
				if seat(s) == tt.victim {
					want += tt.draws
				}
				if s == 0 {
					want = 1
				}
				if got := len(g.hands[seat(s)]); got != want {
					t.Errorf("seat %d has %d cards, want %d", s, got, want)
				}
			}
			if tt.victim >= 0 && !slices.Equal(g.hands[tt.victim][1:], draw[:tt.draws]) {
				t.Errorf("seat %d drew %v, want %v", tt.victim, g.hands[tt.victim][1:], draw[:tt.draws])
			}
		})
	}
}

// TestOnTable plays through a cardtable.Table to check the turn passes the
// way each Outcome says.
func TestOnTable(t *testing.T) {
	tests := []struct {
		name     string
		card     Card
		wantTurn seat
	}{
		{"number", c(Colour1, 5), 1},
		{"skip", c(Colour1, Skip), 2},
		{"reverse", c(Colour1, Reverse), 2},
		{"draw two", c(Colour1, DrawTwo), 2},
		{"wild draw four", wd4, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := New()
			tb, err := cardtable.New(3, 100, g)
			if err != nil {
				t.Fatal(err)
			}
			for p := world.PlayerID(1); p <= 3; p++ {
				if _, err := tb.Sit(p); err != nil {
					t.Fatal(err)
				}
			}
			for p := world.PlayerID(1); p <= 3; p++ {
				if err := tb.SetReady(p, true, 0); err != nil {
					t.Fatal(err)
				}
			}
			if !tb.InProgress() {
				t.Fatal("game did not start")
			}
			*g = *setup(map[seat][]Card{
				0: {tt.card, c(Colour3, 3)},
				1: {c(Colour3, 1)},
				2: {c(Colour3, 2)},
			}, []Card{c(Colour2, 1), c(Colour2, 2), c(Colour2, 3), c(Colour2, 4)}, []Card{c(Colour1, 0)})
			if err := tb.Move(1, Play{Card: tt.card, Chosen: Colour1}, 1); err != nil {
				t.Fatal(err)
			}
			if s, _ := tb.Turn(); s != tt.wantTurn {
				t.Errorf("turn passed to seat %d, want %d", s, tt.wantTurn)
			}
		})
	}
}

func TestReshuffle(t *testing.T) {
	discard := []Card{c(Colour1, 1), c(Colour1, 2), c(Colour1, 3), c(Colour2, 3)}
	g := setup(map[seat][]Card{0: {c(Colour4, 9)}, 1: {c(Colour4, 8)}}, nil, discard)
	if _, err := g.Move(0, Draw{}); err != nil {
		t.Fatal(err)
	}
	if want := []Card{c(Colour2, 3)}; !slices.Equal(g.discard, want) {
		t.Errorf("discard pile %v, want only the old top %v", g.discard, want)
	}
	if len(g.hands[0]) != 2 || len(g.draw) != 2 {
		t.Fatalf("hand %v, draw %v: want 2 cards each", g.hands[0], g.draw)
	}
	got := sorted(append(slices.Clone(g.draw), g.hands[0][1]))
	if want := sorted(discard[:3]); !slices.Equal(got, want) {
		t.Errorf("new draw pile plus drawn card = %v, want %v", got, want)
	}
}

// TestDrawTwoAcrossReshuffle checks a penalty draw reshuffles part-way.
func TestDrawTwoAcrossReshuffle(t *testing.T) {
	g := setup(map[seat][]Card{0: {c(Colour1, DrawTwo), c(Colour4, 9)}, 1: {}},
		[]Card{c(Colour3, 5)}, []Card{c(Colour2, 4), c(Colour1, 4)})
	if _, err := g.Move(0, Play{Card: c(Colour1, DrawTwo)}); err != nil {
		t.Fatal(err)
	}
	if len(g.hands[1]) != 2 || g.hands[1][0] != c(Colour3, 5) {
		t.Fatalf("seat 1 hand %v, want %v then a reshuffled card", g.hands[1], c(Colour3, 5))
	}
	if want := []Card{c(Colour1, DrawTwo)}; !slices.Equal(g.discard, want) {
		t.Errorf("discard %v, want %v", g.discard, want)
	}
	got := sorted(append(slices.Clone(g.draw), g.hands[1][1]))
	if want := sorted([]Card{c(Colour2, 4), c(Colour1, 4)}); !slices.Equal(got, want) {
		t.Errorf("reshuffled cards %v, want %v", got, want)
	}
}

// TestBothPilesEmpty checks Q3: with no card left even after reshuffling,
// the seat draws nothing (or only what is left) and the turn passes.
func TestBothPilesEmpty(t *testing.T) {
	tests := []struct {
		name      string
		draw      []Card
		discard   []Card
		play      Card
		wantDrawn int
	}{
		{"draw", nil, []Card{c(Colour1, 1)}, Card{}, 0},
		// The played card goes on top, so only the old top is reshuffled.
		{"draw two with one left", nil, []Card{c(Colour1, 1)}, c(Colour1, DrawTwo), 1},
		{"wild draw four with two left", []Card{c(Colour3, 3)}, []Card{c(Colour1, 1)}, wd4, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hands := map[seat][]Card{0: {c(Colour4, 9)}, 1: {c(Colour4, 8)}}
			move := any(Draw{})
			victim := seat(0)
			if tt.play != (Card{}) {
				hands[0] = append(hands[0], tt.play)
				move = Play{Card: tt.play, Chosen: Colour1}
				victim = 1
			}
			g := setup(hands, tt.draw, tt.discard)
			before := len(g.hands[victim])
			out, err := g.Move(0, move)
			if err != nil {
				t.Fatalf("Move: %v", err)
			}
			if got := len(g.hands[victim]) - before; got != tt.wantDrawn {
				t.Errorf("seat %d drew %d cards, want %d", victim, got, tt.wantDrawn)
			}
			if len(g.draw) != 0 || len(g.discard) != 1 {
				t.Errorf("draw %v, discard %v: want empty draw and one discard", g.draw, g.discard)
			}
			if tt.play == (Card{}) && out != (cardtable.Outcome{}) {
				t.Errorf("outcome %+v, want the turn to pass", out)
			}
		})
	}
	g := setup(map[seat][]Card{0: {c(Colour4, 9)}, 1: {c(Colour4, 8)}}, nil, []Card{c(Colour1, 1)})
	if out := g.Timeout(0); out != (cardtable.Outcome{}) || len(g.hands[0]) != 1 {
		t.Errorf("Timeout with no cards: outcome %+v, hand %v", out, g.hands[0])
	}
}

func TestTimeout(t *testing.T) {
	g := setup(map[seat][]Card{0: {c(Colour4, 9)}, 1: {c(Colour4, 8)}}, []Card{c(Colour1, 5), c(Colour1, 6)}, []Card{c(Colour2, 1)})
	out := g.Timeout(0)
	if out != (cardtable.Outcome{}) {
		t.Errorf("outcome %+v, want the turn to pass", out)
	}
	if want := []Card{c(Colour4, 9), c(Colour1, 5)}; !slices.Equal(g.hands[0], want) {
		t.Errorf("hand %v, want %v", g.hands[0], want)
	}
	if len(g.hands[1]) != 1 || len(g.draw) != 1 {
		t.Errorf("seat 1 hand %v, draw %v", g.hands[1], g.draw)
	}
}

// TestTimeoutOnTable checks the table's timer runs Shed's timeout.
func TestTimeoutOnTable(t *testing.T) {
	g := New()
	tb, err := cardtable.New(2, 10, g)
	if err != nil {
		t.Fatal(err)
	}
	for p := world.PlayerID(1); p <= 2; p++ {
		if _, err := tb.Sit(p); err != nil {
			t.Fatal(err)
		}
	}
	for p := world.PlayerID(1); p <= 2; p++ {
		if err := tb.SetReady(p, true, 0); err != nil {
			t.Fatal(err)
		}
	}
	drawBefore := len(g.draw)
	tb.Tick(10)
	if len(g.hands[0]) != HandSize+1 || len(g.draw) != drawBefore-1 {
		t.Errorf("after timeout: hand %d cards, draw %d; want %d and %d", len(g.hands[0]), len(g.draw), HandSize+1, drawBefore-1)
	}
	if s, _ := tb.Turn(); s != 1 {
		t.Errorf("turn is seat %d, want 1", s)
	}
}

func TestForfeit(t *testing.T) {
	forfeited := []Card{c(Colour3, 3), wild}
	g := setup(map[seat][]Card{0: {c(Colour1, DrawTwo), c(Colour1, 4)}, 1: forfeited, 2: {c(Colour4, 8)}},
		[]Card{c(Colour2, 5)}, []Card{c(Colour1, 1)})
	all := allCards(g)
	g.Forfeit(1)
	if _, ok := g.hands[1]; ok {
		t.Error("forfeited seat still has a hand")
	}
	if slices.Contains(g.order, 1) {
		t.Errorf("order %v still holds seat 1", g.order)
	}
	if got := allCards(g); !slices.Equal(got, all) {
		t.Errorf("cards in play %v, want %v", got, all)
	}
	if v := g.PublicView().(PublicView); len(v.HandCounts) != 2 {
		t.Errorf("hand counts %v, want seats 0 and 2", v.HandCounts)
	}
	if g.PrivateView(1) != nil {
		t.Error("forfeited seat still has a private view")
	}
	if _, err := g.Move(1, Draw{}); !errors.Is(err, ErrNotInGame) {
		t.Errorf("Move from forfeited seat: %v, want ErrNotInGame", err)
	}
	// Seat 2 is now next after seat 0, so it takes the Draw Two.
	if _, err := g.Move(0, Play{Card: c(Colour1, DrawTwo)}); err != nil {
		t.Fatal(err)
	}
	if len(g.hands[2]) != 3 {
		t.Errorf("seat 2 has %d cards, want 3", len(g.hands[2]))
	}
	g.Forfeit(1) // already gone: no effect
}

// TestForfeitOnTable checks that a seat leaving mid-game is removed from
// play and the last seat left wins.
func TestForfeitOnTable(t *testing.T) {
	g := New()
	tb, err := cardtable.New(3, 10, g)
	if err != nil {
		t.Fatal(err)
	}
	for p := world.PlayerID(1); p <= 3; p++ {
		if _, err := tb.Sit(p); err != nil {
			t.Fatal(err)
		}
	}
	for p := world.PlayerID(1); p <= 3; p++ {
		if err := tb.SetReady(p, true, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := tb.Leave(2, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.hands[1]; ok || len(g.order) != 2 {
		t.Errorf("seat 1 still in play: order %v", g.order)
	}
	if err := tb.Leave(1, 2); err != nil {
		t.Fatal(err)
	}
	if w, ok := tb.Winner(); !ok || w != 2 {
		t.Errorf("winner %d, %v; want seat 2", w, ok)
	}
}

func TestOver(t *testing.T) {
	g := setup(map[seat][]Card{0: {c(Colour1, 4)}, 1: {c(Colour1, 5), c(Colour2, 2)}, 2: {c(Colour1, Skip)}},
		[]Card{c(Colour3, 3)}, []Card{c(Colour1, 1)})
	if _, over := g.Over(); over {
		t.Fatal("over before anyone emptied their hand")
	}
	if _, err := g.Move(1, Play{Card: c(Colour1, 5)}); err != nil {
		t.Fatal(err)
	}
	if _, over := g.Over(); over {
		t.Fatal("over with cards still in every hand")
	}
	if _, err := g.Move(2, Play{Card: c(Colour1, Skip)}); err != nil {
		t.Fatal(err)
	}
	if w, over := g.Over(); !over || w != 2 {
		t.Errorf("Over() = %d, %v; want seat 2", w, over)
	}
	if _, err := g.Move(0, Play{Card: c(Colour1, 4)}); !errors.Is(err, ErrNoGame) {
		t.Errorf("move after game over: %v, want ErrNoGame", err)
	}
}

func TestOverOnTable(t *testing.T) {
	g := New()
	tb, err := cardtable.New(2, 10, g)
	if err != nil {
		t.Fatal(err)
	}
	for p := world.PlayerID(1); p <= 2; p++ {
		if _, err := tb.Sit(p); err != nil {
			t.Fatal(err)
		}
	}
	for p := world.PlayerID(1); p <= 2; p++ {
		if err := tb.SetReady(p, true, 0); err != nil {
			t.Fatal(err)
		}
	}
	*g = *setup(map[seat][]Card{0: {wd4}, 1: {c(Colour2, 2)}}, []Card{c(Colour3, 3)}, []Card{c(Colour1, 1)})
	if err := tb.Move(1, Play{Card: wd4, Chosen: Colour2}, 1); err != nil {
		t.Fatal(err)
	}
	if w, ok := tb.Winner(); !ok || w != 0 || tb.InProgress() {
		t.Errorf("winner %d, %v, in progress %v; want seat 0 and game over", w, ok, tb.InProgress())
	}
	if len(g.hands[1]) != 1 {
		t.Errorf("a winning Wild Draw Four still made seat 1 draw: %v", g.hands[1])
	}
}

func TestViews(t *testing.T) {
	h0 := []Card{c(Colour1, 4), wild}
	h1 := []Card{c(Colour2, 5), c(Colour3, 6), c(Colour4, 7)}
	g := setup(map[seat][]Card{0: h0, 1: h1}, []Card{c(Colour3, 3)}, []Card{c(Colour1, 1)})
	if got := g.PrivateView(0).([]Card); !slices.Equal(got, h0) {
		t.Errorf("seat 0 view %v, want %v", got, h0)
	}
	got := g.PrivateView(1).([]Card)
	if !slices.Equal(got, h1) {
		t.Errorf("seat 1 view %v, want %v", got, h1)
	}
	got[0] = wd4
	if g.hands[1][0] != h1[0] {
		t.Error("editing the private view changed the hand")
	}
	if g.PrivateView(2) != nil {
		t.Error("a seat not in the game got a private view")
	}
	pub := g.PublicView().(PublicView)
	want := PublicView{Top: c(Colour1, 1), Colour: Colour1, HandCounts: map[seat]int{0: 2, 1: 3}, DrawPile: 1}
	if !reflect.DeepEqual(pub, want) {
		t.Errorf("public view %+v, want %+v", pub, want)
	}
	// The public view has no field that could carry a hand.
	typ := reflect.TypeOf(pub)
	for i := range typ.NumField() {
		if k := typ.Field(i).Type.Kind(); k == reflect.Slice || k == reflect.Array {
			t.Errorf("PublicView.%s is a %s", typ.Field(i).Name, k)
		}
	}
	if New().PublicView() != nil {
		t.Error("public view before Start is not nil")
	}
}

// TestViewsOnTable checks each player gets only their own hand.
func TestViewsOnTable(t *testing.T) {
	g := New()
	tb, err := cardtable.New(3, 10, g)
	if err != nil {
		t.Fatal(err)
	}
	for p := world.PlayerID(1); p <= 2; p++ {
		if _, err := tb.Sit(p); err != nil {
			t.Fatal(err)
		}
	}
	for p := world.PlayerID(1); p <= 2; p++ {
		if err := tb.SetReady(p, true, 0); err != nil {
			t.Fatal(err)
		}
	}
	for p, s := range map[world.PlayerID]seat{1: 0, 2: 1} {
		v := tb.View(p)
		if !slices.Equal(v.Private.([]Card), g.hands[s]) {
			t.Errorf("player %d sees %v, want seat %d's hand", p, v.Private, s)
		}
	}
	if v := tb.View(99); v.Private != nil {
		t.Errorf("onlooker sees %v", v.Private)
	}
}

// TestStartShuffles checks two Starts deal different orders, so the deck
// really is shuffled (the chance of two equal 108-card orders is nil).
func TestStartShuffles(t *testing.T) {
	a, b := New(), New()
	if err := a.Start([]seat{0, 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.Start([]seat{0, 1}); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(a, b) {
		t.Error("two Starts dealt the same game")
	}
}

// TestShufflesOnlyWithDeck checks non-test code imports no random source or
// I/O, and that the only shuffle it calls is deck.Shuffled.
func TestShufflesOnlyWithDeck(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			switch {
			case p == "math/rand", p == "math/rand/v2", p == "crypto/rand", p == "time", p == "os", p == "net", strings.HasPrefix(p, "net/"):
				t.Errorf("%s imports %s", name, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "deck" && sel.Sel.Name == "Shuffled" {
				calls++
			}
			return true
		})
	}
	if calls != 1 {
		t.Errorf("found %d deck.Shuffled references, want 1 (in shuffled)", calls)
	}
}

func BenchmarkStart(b *testing.B) {
	seats := []seat{0, 1, 2, 3}
	for b.Loop() {
		_ = New().Start(seats)
	}
}

func BenchmarkPlayAndDraw(b *testing.B) {
	for b.Loop() {
		g := setup(map[seat][]Card{0: {c(Colour1, 2), c(Colour1, 3)}, 1: {c(Colour1, 4)}}, []Card{c(Colour2, 5)}, []Card{c(Colour1, 1)})
		_, _ = g.Move(0, Play{Card: c(Colour1, 2)})
		_, _ = g.Move(1, Draw{})
	}
}
