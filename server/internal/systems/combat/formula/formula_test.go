package formula_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
)

func TestLevelForXP(t *testing.T) {
	tests := []struct {
		xp, want int
	}{
		{-5, 1},
		{0, 1},
		{82, 1},
		{83, 2},
		{173, 2},
		{174, 3},
		{1154, 10},
		{13362, 29},
		{13363, 30},
		{13364, 30},
		{1_000_000, 30},
	}
	for _, tt := range tests {
		if got := formula.LevelForXP(tt.xp); got != tt.want {
			t.Errorf("LevelForXP(%d) = %d, want %d", tt.xp, got, tt.want)
		}
	}
}

func TestXPForLevel(t *testing.T) {
	tests := []struct {
		level, want int
	}{
		{0, 0},
		{1, 0},
		{2, 83},
		{3, 174},
		{10, 1154},
		{30, 13363},
		{31, 13363},
	}
	for _, tt := range tests {
		if got := formula.XPForLevel(tt.level); got != tt.want {
			t.Errorf("XPForLevel(%d) = %d, want %d", tt.level, got, tt.want)
		}
	}
	for l := 1; l <= formula.MaxLevel; l++ {
		if got := formula.LevelForXP(formula.XPForLevel(l)); got != l {
			t.Errorf("LevelForXP(XPForLevel(%d)) = %d", l, got)
		}
	}
}

func TestMaxHP(t *testing.T) {
	tests := []struct{ level, want int }{{1, 10}, {15, 24}, {30, 39}}
	for _, tt := range tests {
		if got := formula.MaxHP(tt.level); got != tt.want {
			t.Errorf("MaxHP(%d) = %d, want %d", tt.level, got, tt.want)
		}
	}
}

func TestHitChance(t *testing.T) {
	tests := []struct {
		name                                  string
		attLevel, accuracy, defLevel, defence int
		want                                  float64
	}{
		// A = D = 9×64 = 576: A ≤ D branch, 576 / (2 × 577).
		{"level 1 zero bonus", 1, 0, 1, 0, 576.0 / 1154.0},
		// A = 9×67 = 603 > D = 576: 1 − 578 / (2 × 604).
		{"wooden stick vs rabbit", 1, 3, 1, 0, 1 - 578.0/1208.0},
		// A = 23×62 = 1426 ≤ D = 23×89 = 2047: 1426 / (2 × 2048).
		{"robot plating -2 vs robot", 15, -2, 15, 25, 1426.0 / 4096.0},
		// A = 28×92 = 2576 > D = 9×68 = 612: 1 − 614 / (2 × 2577).
		{"coyote alpha vs new player", 20, 28, 1, 4, 1 - 614.0/5154.0},
		// A = 38×60 = 2280 ≤ D = 38×78 = 2964: negative bonus, A ≤ D.
		{"level 30 negative accuracy", 30, -4, 30, 14, 2280.0 / 5930.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formula.HitChance(tt.attLevel, tt.accuracy, tt.defLevel, tt.defence)
			if math.Abs(got-tt.want) > 1e-12 {
				t.Errorf("HitChance = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaxHit(t *testing.T) {
	tests := []struct{ level, strength, want int }{
		{1, 0, 1},    // floor(0.5 + 576/640) = 1
		{1, 2, 1},    // floor(0.5 + 594/640) = 1
		{9, 10, 2},   // floor(0.5 + 1258/640) = 2
		{15, 18, 3},  // floor(0.5 + 1886/640) = 3
		{20, 26, 4},  // floor(0.5 + 2520/640) = 4
		{30, 16, 5},  // floor(0.5 + 3040/640) = 5
		{1, -64, 1},  // roll 0, clamped to at least 1
		{1, -100, 1}, // negative roll, clamped to at least 1
	}
	for _, tt := range tests {
		if got := formula.MaxHit(tt.level, tt.strength); got != tt.want {
			t.Errorf("MaxHit(%d, %d) = %d, want %d", tt.level, tt.strength, got, tt.want)
		}
	}
}

func TestMaxHitHalfRoundsUp(t *testing.T) {
	// Level 7, strength 0: 15×64 / 640 = 1.5 exactly, and floor(0.5 + 1.5) = 2.
	if got := formula.MaxHit(7, 0); got != 2 {
		t.Errorf("MaxHit(7, 0) = %d, want 2", got)
	}
}

func TestRollDamageDeterministic(t *testing.T) {
	a := formula.Attack{AttackerLevel: 20, Accuracy: 28, Strength: 26, DefenderLevel: 10, Defence: 10}
	roll := func() []int {
		r := rand.New(rand.NewPCG(42, 7))
		out := make([]int, 200)
		for i := range out {
			out[i] = formula.RollDamage(r, a)
		}
		return out
	}
	first, second := roll(), roll()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("roll %d differs with same seed: %d vs %d", i, first[i], second[i])
		}
	}
}

func TestRollDamageDistribution(t *testing.T) {
	tests := []struct {
		name string
		a    formula.Attack
	}{
		{"level 1 zero bonus", formula.Attack{AttackerLevel: 1, DefenderLevel: 1}},
		{"coyote alpha vs new player", formula.Attack{AttackerLevel: 20, Accuracy: 28, Strength: 26, DefenderLevel: 1, Defence: 4}},
		{"robot plating vs robot", formula.Attack{AttackerLevel: 15, Accuracy: -2, Strength: 16, DefenderLevel: 15, Defence: 25}},
		{"level 30 vs rabbit", formula.Attack{AttackerLevel: 30, Accuracy: 14, Strength: 16, DefenderLevel: 1}},
	}
	const n = 100_000
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewPCG(uint64(i), 99))
			maxHit := formula.MaxHit(tt.a.AttackerLevel, tt.a.Strength)
			want := formula.HitChance(tt.a.AttackerLevel, tt.a.Accuracy, tt.a.DefenderLevel, tt.a.Defence)
			hits := 0
			seen := make(map[int]bool)
			for range n {
				d := formula.RollDamage(r, tt.a)
				if d == 0 {
					continue
				}
				if d < 1 || d > maxHit {
					t.Fatalf("damage %d outside 1..%d", d, maxHit)
				}
				hits++
				seen[d] = true
			}
			rate := float64(hits) / n
			if math.Abs(rate-want) > 0.01 {
				t.Errorf("hit rate %.4f, want %.4f ± 0.01", rate, want)
			}
			for d := 1; d <= maxHit; d++ {
				if !seen[d] {
					t.Errorf("damage %d never rolled", d)
				}
			}
		})
	}
}

func TestXPForDamage(t *testing.T) {
	tests := []struct{ damage, want int }{{0, 0}, {1, 4}, {5, 20}, {39, 156}}
	for _, tt := range tests {
		if got := formula.XPForDamage(tt.damage); got != tt.want {
			t.Errorf("XPForDamage(%d) = %d, want %d", tt.damage, got, tt.want)
		}
	}
}

func BenchmarkRollDamage(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 2))
	a := formula.Attack{AttackerLevel: 20, Accuracy: 28, Strength: 26, DefenderLevel: 10, Defence: 10}
	for b.Loop() {
		formula.RollDamage(r, a)
	}
}

func BenchmarkLevelForXP(b *testing.B) {
	for b.Loop() {
		formula.LevelForXP(13363)
	}
}
