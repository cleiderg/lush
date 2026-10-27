// Package deck shuffles cards and deals them from a pile for the card table
// system. Shuffling uses crypto/rand (docs/specs/card-tables.md): the only
// exported way to build a shuffled pile, Shuffled, takes no random source, so
// game code can't swap in a predictable one.
package deck

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Card is a card id. What each id means is up to the game's rules module.
type Card int32

// Source is the random source for shuffling. IntN returns a uniform value in
// [0, n) for n > 0. *math/rand/v2.Rand satisfies it, but production code only
// ever shuffles with the crypto/rand source.
type Source interface {
	IntN(n int) int
}

// cryptoSource is a Source backed by crypto/rand.
type cryptoSource struct{}

// IntN returns a uniform value in [0, n), or 0 if n <= 0. It uses rejection
// sampling so no value is more likely than another. crypto/rand.Read never
// returns an error (it crashes the program if the OS can't supply randomness).
func (cryptoSource) IntN(n int) int {
	if n <= 1 {
		return 0
	}
	bound := uint64(n)
	// Largest multiple of bound that fits in a uint64; values at or above it
	// would bias the result, so they are drawn again.
	limit := math.MaxUint64 - math.MaxUint64%bound
	var b [8]byte
	for {
		_, _ = rand.Read(b[:])
		if v := binary.LittleEndian.Uint64(b[:]); v < limit {
			return int(v % bound)
		}
	}
}

// shuffle does an unbiased Fisher–Yates shuffle of cards in place.
func shuffle(cards []Card, src Source) {
	for i := len(cards) - 1; i > 0; i-- {
		j := src.IntN(i + 1)
		cards[i], cards[j] = cards[j], cards[i]
	}
}

// ErrNotEnoughCards is returned when a draw or deal asks for more cards than
// the pile holds.
var ErrNotEnoughCards = errors.New("not enough cards in pile")

// Pile is an ordered stack of cards. Index 0 is the top.
type Pile struct {
	cards []Card
}

// Shuffled returns a pile holding a copy of cards in a random order chosen
// with crypto/rand. The caller's slice is not modified.
func Shuffled(cards []Card) *Pile {
	return newShuffledPile(cards, cryptoSource{})
}

func newShuffledPile(cards []Card, src Source) *Pile {
	p := &Pile{cards: append([]Card(nil), cards...)}
	shuffle(p.cards, src)
	return p
}

// Len returns the number of cards left in the pile.
func (p *Pile) Len() int { return len(p.cards) }

// Draw removes the top n cards and returns them, top card first. If the pile
// holds fewer than n cards, or n is negative, it returns an error and the
// pile is unchanged.
func (p *Pile) Draw(n int) ([]Card, error) {
	if n < 0 {
		return nil, fmt.Errorf("draw %d cards: count is negative", n)
	}
	if n > len(p.cards) {
		return nil, fmt.Errorf("draw %d cards from %d: %w", n, len(p.cards), ErrNotEnoughCards)
	}
	drawn := append([]Card(nil), p.cards[:n]...)
	p.cards = append([]Card(nil), p.cards[n:]...)
	return drawn, nil
}

// Deal gives n cards to each of seats seats, one card at a time round the
// table, and returns the hands indexed by seat. If the pile holds fewer than
// seats*n cards, or either count is invalid, it returns an error and the pile
// is unchanged.
func (p *Pile) Deal(seats, n int) ([][]Card, error) {
	if seats <= 0 {
		return nil, fmt.Errorf("deal to %d seats: need at least one seat", seats)
	}
	if n < 0 {
		return nil, fmt.Errorf("deal %d cards each: count is negative", n)
	}
	if n > len(p.cards)/seats {
		return nil, fmt.Errorf("deal %d cards to %d seats from %d: %w", n, seats, len(p.cards), ErrNotEnoughCards)
	}
	hands := make([][]Card, seats)
	for s := range hands {
		hands[s] = make([]Card, 0, n)
	}
	for i := range seats * n {
		hands[i%seats] = append(hands[i%seats], p.cards[i])
	}
	p.cards = append([]Card(nil), p.cards[seats*n:]...)
	return hands, nil
}
