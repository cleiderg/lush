package deck_test

import (
	"slices"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/cardtable/deck"
)

func TestShuffledHoldsInputCards(t *testing.T) {
	tests := []struct {
		name  string
		cards []deck.Card
	}{
		{"empty", nil},
		{"one", []deck.Card{7}},
		{"duplicates", []deck.Card{3, 3, 1, 2, 2, 2}},
		{"108 cards", func() []deck.Card {
			c := make([]deck.Card, 108)
			for i := range c {
				c[i] = deck.Card(i)
			}
			return c
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := slices.Clone(tt.cards)
			p := deck.Shuffled(input)
			if !slices.Equal(input, tt.cards) {
				t.Errorf("Shuffled modified its input: %v", input)
			}
			if p.Len() != len(tt.cards) {
				t.Fatalf("pile has %d cards, want %d", p.Len(), len(tt.cards))
			}
			got, err := p.Draw(p.Len())
			if err != nil {
				t.Fatal(err)
			}
			want := slices.Clone(tt.cards)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("pile holds %v, want %v", got, want)
			}
			if p.Len() != 0 {
				t.Errorf("pile has %d cards after drawing all", p.Len())
			}
		})
	}
}
