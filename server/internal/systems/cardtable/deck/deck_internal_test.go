package deck

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func cardsUpTo(n int) []Card {
	cards := make([]Card, n)
	for i := range cards {
		cards[i] = Card(i)
	}
	return cards
}

// TestShuffleUniform checks that every order of 3 cards appears within one
// percentage point of 1/6 over 60,000 seeded shuffles.
func TestShuffleUniform(t *testing.T) {
	const runs = 60000
	src := rand.New(rand.NewPCG(278, 1))
	counts := map[string]int{}
	for range runs {
		cards := []Card{0, 1, 2}
		shuffle(cards, src)
		counts[fmt.Sprint(cards)]++
	}
	if len(counts) != 6 {
		t.Fatalf("saw %d distinct orders, want 6: %v", len(counts), counts)
	}
	for order, c := range counts {
		pct := 100 * float64(c) / runs
		if pct < 15.67 || pct > 17.67 {
			t.Errorf("order %s: %.2f%% of shuffles, want 15.67%%..17.67%%", order, pct)
		}
	}
}

func TestShuffleKeepsCards(t *testing.T) {
	src := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{0, 1, 2, 52, 108} {
		cards := cardsUpTo(n)
		shuffle(cards, src)
		slices.Sort(cards)
		if !slices.Equal(cards, cardsUpTo(n)) {
			t.Errorf("shuffle of %d cards changed the card set: %v", n, cards)
		}
	}
}

func TestCryptoSourceRange(t *testing.T) {
	var src Source = cryptoSource{}
	for _, n := range []int{1, 2, 7, 108} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			seen := make([]bool, n)
			for range 20 * n {
				v := src.IntN(n)
				if v < 0 || v >= n {
					t.Fatalf("IntN(%d) = %d, want [0, %d)", n, v, n)
				}
				seen[v] = true
			}
			if n <= 7 && slices.Contains(seen, false) {
				t.Errorf("IntN(%d) never returned some values: seen %v", n, seen)
			}
		})
	}
}

func TestDraw(t *testing.T) {
	tests := []struct {
		name    string
		size, n int
		wantErr bool
	}{
		{"none", 5, 0, false},
		{"one", 5, 1, false},
		{"all", 5, 5, false},
		{"one too many", 5, 6, true},
		{"from empty", 0, 1, true},
		{"negative", 5, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pile{cards: cardsUpTo(tt.size)}
			got, err := p.Draw(tt.n)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Draw(%d) from %d: got %v, want error", tt.n, tt.size, got)
				}
				if tt.n > tt.size && !errors.Is(err, ErrNotEnoughCards) {
					t.Errorf("error %v is not ErrNotEnoughCards", err)
				}
				if !slices.Equal(p.cards, cardsUpTo(tt.size)) {
					t.Errorf("pile changed after failed draw: %v", p.cards)
				}
				return
			}
			if err != nil {
				t.Fatalf("Draw(%d): %v", tt.n, err)
			}
			if !slices.Equal(got, cardsUpTo(tt.n)) {
				t.Errorf("drew %v, want top %d cards", got, tt.n)
			}
			if p.Len() != tt.size-tt.n {
				t.Errorf("pile has %d cards, want %d", p.Len(), tt.size-tt.n)
			}
			if all := append(got, p.cards...); !slices.Equal(all, cardsUpTo(tt.size)) {
				t.Errorf("drawn plus pile = %v, want original cards", all)
			}
		})
	}
}

func TestDeal(t *testing.T) {
	tests := []struct {
		name           string
		size, seats, n int
		wantErr        bool
	}{
		{"seven each to four", 108, 4, 7, false},
		{"exact fit", 12, 3, 4, false},
		{"zero each", 5, 2, 0, false},
		{"one seat", 5, 1, 5, false},
		{"one card short", 11, 3, 4, true},
		{"empty pile", 0, 2, 1, true},
		{"no seats", 5, 0, 1, true},
		{"negative count", 5, 2, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := rand.New(rand.NewPCG(uint64(tt.size), uint64(tt.seats)))
			p := newShuffledPile(cardsUpTo(tt.size), src)
			before := slices.Clone(p.cards)
			hands, err := p.Deal(tt.seats, tt.n)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Deal(%d, %d) from %d: got %v, want error", tt.seats, tt.n, tt.size, hands)
				}
				if tt.seats > 0 && tt.n > 0 && !errors.Is(err, ErrNotEnoughCards) {
					t.Errorf("error %v is not ErrNotEnoughCards", err)
				}
				if !slices.Equal(p.cards, before) {
					t.Errorf("pile changed after failed deal: %v, was %v", p.cards, before)
				}
				return
			}
			if err != nil {
				t.Fatalf("Deal(%d, %d): %v", tt.seats, tt.n, err)
			}
			if len(hands) != tt.seats {
				t.Fatalf("got %d hands, want %d", len(hands), tt.seats)
			}
			var all []Card
			for s, h := range hands {
				if len(h) != tt.n {
					t.Errorf("seat %d got %d cards, want %d", s, len(h), tt.n)
				}
				// Cards go round the table one at a time from the top.
				for i, c := range h {
					if want := before[i*tt.seats+s]; c != want {
						t.Errorf("seat %d card %d = %d, want %d", s, i, c, want)
					}
				}
				all = append(all, h...)
			}
			if !slices.Equal(p.cards, before[tt.seats*tt.n:]) {
				t.Errorf("remaining pile %v, want %v", p.cards, before[tt.seats*tt.n:])
			}
			all = append(all, p.cards...)
			slices.Sort(all)
			if !slices.Equal(all, cardsUpTo(tt.size)) {
				t.Errorf("hands plus pile = %v, want each original card once", all)
			}
		})
	}
}

// TestDrawDoesNotAlias checks that drawn cards and the pile don't share
// memory, so a rules module editing a hand can't change the pile.
func TestDrawDoesNotAlias(t *testing.T) {
	p := &Pile{cards: cardsUpTo(4)}
	hand, err := p.Draw(2)
	if err != nil {
		t.Fatal(err)
	}
	hand = append(hand, 99)
	hand[0] = 99
	if !slices.Equal(p.cards, []Card{2, 3}) {
		t.Errorf("pile = %v after editing drawn hand, want [2 3]", p.cards)
	}
}

// TestNoForbiddenImports checks that no non-test file imports a weak random
// source, the wall clock, the OS or the network.
func TestNoForbiddenImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); forbiddenImport(p) {
				t.Errorf("%s imports %s", name, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files found")
	}
}

func forbiddenImport(path string) bool {
	switch path {
	case "math/rand", "math/rand/v2", "time", "os", "net":
		return true
	}
	return strings.HasPrefix(path, "net/")
}

func TestForbiddenImport(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"math/rand", true},
		{"math/rand/v2", true},
		{"time", true},
		{"os", true},
		{"net", true},
		{"net/http", true},
		{"net/netip", true},
		{"crypto/rand", false},
		{"math", false},
		{"os/exec", false},
		{"network", false},
	}
	for _, tt := range tests {
		if got := forbiddenImport(tt.path); got != tt.want {
			t.Errorf("forbiddenImport(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func BenchmarkShuffled108(b *testing.B) {
	cards := cardsUpTo(108)
	for b.Loop() {
		Shuffled(cards)
	}
}
