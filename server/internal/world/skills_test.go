package world_test

import (
	"math"
	"testing"

	"github.com/cleiderg/lush/server/internal/world"
)

func TestSkillXPSeparateFromCombat(t *testing.T) {
	r := rules(t)
	tests := []struct {
		name   string
		apply  func(p *world.Player)
		combat int
		skills [world.NumSkills]int
	}{
		{"skill XP leaves combat level", func(p *world.Player) {
			for s := range world.NumSkills {
				p.AddSkillXP(s, 13363*world.SkillXPScale)
			}
		}, 1, [world.NumSkills]int{30, 30, 30}},
		{"combat XP leaves skill levels", func(p *world.Player) { p.AddXP(r, 13363) }, 30, [world.NumSkills]int{1, 1, 1}},
		{"one skill leaves the others", func(p *world.Player) { p.AddSkillXP(world.Fishing, 83*world.SkillXPScale) }, 1, [world.NumSkills]int{1, 2, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &world.Player{}
			tt.apply(p)
			if got := p.CombatLevel(r); got != tt.combat {
				t.Errorf("CombatLevel = %d, want %d", got, tt.combat)
			}
			for s := range world.NumSkills {
				if got := p.SkillLevel(s); got != tt.skills[s] {
					t.Errorf("%s level = %d, want %d", s, got, tt.skills[s])
				}
			}
		})
	}
}

func TestAddSkillXP(t *testing.T) {
	tests := []struct {
		name        string
		start, add  int // tenths
		wantXP      int
		wantLeveled bool
		wantLevel   int
	}{
		{"level 1 at 0 XP", 0, 0, 0, false, 1},
		{"negative ignored", 0, -50, 0, false, 1},
		{"just below level 2", 0, 829, 829, false, 1},
		{"first level-up at 83 XP", 820, 10, 830, true, 2},
		{"37.5 XP is exact", 0, 375, 375, false, 1},
		{"two 37.5 awards make 75", 375, 375, 750, false, 1},
		{"67.5 XP crosses 83 from 16", 160, 675, 835, true, 2},
		{"fractional below a level stays", 829, 0, 829, false, 1},
		{"reaches 30", 13362 * 10, 10, 13363 * 10, true, 30},
		{"capped at 30", 13363 * 10, 1_000_000, 13363*10 + 1_000_000, false, 30},
		{"overflow saturates", math.MaxInt - 5, 100, math.MaxInt, false, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &world.Player{}
			p.SkillXP[world.Woodcutting] = tt.start
			leveled, level := p.AddSkillXP(world.Woodcutting, tt.add)
			if leveled != tt.wantLeveled || level != tt.wantLevel {
				t.Errorf("AddSkillXP = (%v, %d), want (%v, %d)", leveled, level, tt.wantLeveled, tt.wantLevel)
			}
			if got := p.SkillXP[world.Woodcutting]; got != tt.wantXP {
				t.Errorf("SkillXP = %d, want %d", got, tt.wantXP)
			}
			if got := p.SkillLevel(world.Woodcutting); got != tt.wantLevel {
				t.Errorf("SkillLevel = %d, want %d", got, tt.wantLevel)
			}
		})
	}
}

func TestTotalLevel(t *testing.T) {
	r := rules(t)
	maxed := &world.Player{XP: 13363}
	for s := range world.NumSkills {
		maxed.SkillXP[s] = 13363 * world.SkillXPScale
	}
	mixed := &world.Player{XP: 83}
	mixed.SkillXP[world.Cooking] = 83 * world.SkillXPScale
	tests := []struct {
		name string
		p    *world.Player
		want int
	}{
		{"new player", &world.Player{}, 4},
		{"mixed", mixed, 6},
		{"maxed player", maxed, 120},
	}
	for _, tt := range tests {
		if got := tt.p.TotalLevel(r); got != tt.want {
			t.Errorf("%s: TotalLevel = %d, want %d", tt.name, got, tt.want)
		}
	}
}

const steelHatchet = 0.10

func TestGatherChance(t *testing.T) {
	tests := []struct {
		name            string
		low, high, tool float64
		level           int
		want            float64
	}{
		// Trees (docs/specs/skills.md).
		{"tree at 1", 0.25, 0.75, 0, 1, 0.25},
		{"tree at 15", 0.25, 0.75, 0, 15, 0.25 + 0.50*14/29},
		{"tree at 30", 0.25, 0.75, 0, 30, 0.75},
		{"tree at 1, steel", 0.25, 0.75, steelHatchet, 1, 0.35},
		{"tree at 30, steel", 0.25, 0.75, steelHatchet, 30, 0.85},
		{"oak at 15", 0.12, 0.50, 0, 15, 0.12 + 0.38*14/29},
		{"oak at 15, steel", 0.12, 0.50, steelHatchet, 15, 0.22 + 0.38*14/29},
		{"oak at 30", 0.12, 0.50, 0, 30, 0.50},
		{"oak at 30, steel", 0.12, 0.50, steelHatchet, 30, 0.60},
		{"eucalyptus at 25", 0.06, 0.32, 0, 25, 0.06 + 0.26*24/29},
		{"eucalyptus at 25, steel", 0.06, 0.32, steelHatchet, 25, 0.16 + 0.26*24/29},
		{"eucalyptus at 30", 0.06, 0.32, 0, 30, 0.32},
		{"eucalyptus at 30, steel", 0.06, 0.32, steelHatchet, 30, 0.42},
		// Fishing spots.
		{"net spot at 1", 0.25, 0.75, 0, 1, 0.25},
		{"net spot at 30", 0.25, 0.75, 0, 30, 0.75},
		{"rod spot at 5", 0.20, 0.60, 0, 5, 0.20 + 0.40*4/29},
		{"rod spot at 20", 0.20, 0.60, 0, 20, 0.20 + 0.40*19/29},
		{"rod spot at 30", 0.20, 0.60, 0, 30, 0.60},
		// Clamping.
		{"clamped to 1", 0.25, 0.75, 0.5, 30, 1},
		{"clamped to 0", 0.05, 0.10, -0.2, 1, 0},
		{"level above 30 clamped", 0.25, 0.75, 0, 99, 0.75},
		{"level below 1 clamped", 0.25, 0.75, 0, 0, 0.25},
	}
	for _, tt := range tests {
		if got := world.GatherChance(tt.low, tt.high, tt.tool, tt.level); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("%s: GatherChance = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBurnChance(t *testing.T) {
	tests := []struct {
		name                   string
		at                     float64
		required, stops, level int
		want                   float64
	}{
		{"shrimp at 1", 0.5, 1, 15, 1, 0.5},
		{"shrimp at 8", 0.5, 1, 15, 8, 0.25},
		{"shrimp at 14", 0.5, 1, 15, 14, 0.5 / 14},
		{"shrimp at 15", 0.5, 1, 15, 15, 0},
		{"shrimp at 30", 0.5, 1, 15, 30, 0},
		{"sardine at 5", 0.5, 5, 20, 5, 0.5},
		{"sardine at 10", 0.5, 5, 20, 10, 0.5 * 10 / 15},
		{"sardine at 20", 0.5, 5, 20, 20, 0},
		{"trout at 15", 0.5, 15, 30, 15, 0.5},
		{"trout at 27", 0.5, 15, 30, 27, 0.1},
		{"trout at 30", 0.5, 15, 30, 30, 0},
		{"below required uses required value", 0.5, 15, 30, 3, 0.5},
	}
	for _, tt := range tests {
		if got := world.BurnChance(tt.at, tt.required, tt.stops, tt.level); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("%s: BurnChance = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func BenchmarkGatherChance(b *testing.B) {
	for i := range b.N {
		world.GatherChance(0.12, 0.50, steelHatchet, 1+i%30)
	}
}

func BenchmarkAddSkillXP(b *testing.B) {
	p := &world.Player{}
	for range b.N {
		p.AddSkillXP(world.Woodcutting, 375)
	}
}
