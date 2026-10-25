package world_test

import (
	"math"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

func rules(t testing.TB) gamedata.Rules {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d.Rules
}

func TestCombatLevelAndMaxHP(t *testing.T) {
	r := rules(t)
	tests := []struct {
		xp, level, maxHP int
	}{
		{0, 1, r.BaseHP + 1},
		{82, 1, r.BaseHP + 1},
		{83, 2, r.BaseHP + 2},
		{13362, 29, r.BaseHP + 29},
		{13363, 30, r.BaseHP + 30},
		{1_000_000, 30, r.BaseHP + 30},
	}
	for _, tt := range tests {
		p := &world.Player{XP: tt.xp}
		if got := p.CombatLevel(r); got != tt.level {
			t.Errorf("CombatLevel at %d XP = %d, want %d", tt.xp, got, tt.level)
		}
		if got := p.MaxHP(r); got != tt.maxHP {
			t.Errorf("MaxHP at %d XP = %d, want %d", tt.xp, got, tt.maxHP)
		}
	}
	// The spec numbers: 10 HP at level 1, 39 at level 30.
	if got := (&world.Player{}).MaxHP(r); got != 10 {
		t.Errorf("level 1 max HP = %d, want 10", got)
	}
	if got := (&world.Player{XP: 13363}).MaxHP(r); got != 39 {
		t.Errorf("level 30 max HP = %d, want 39", got)
	}
}

func TestCombatLevelClampedToRules(t *testing.T) {
	r := rules(t)
	r.MinCombatLevel, r.MaxCombatLevel = 3, 10
	tests := []struct{ xp, want int }{
		{0, 3},
		{174, 3},
		{1154, 10},
		{13363, 10},
	}
	for _, tt := range tests {
		if got := world.CombatLevelForXP(r, tt.xp); got != tt.want {
			t.Errorf("CombatLevelForXP(%d) = %d, want %d", tt.xp, got, tt.want)
		}
	}
	r.BaseHP = 20
	if got := world.MaxHPForLevel(r, 5); got != 25 {
		t.Errorf("MaxHPForLevel with base 20 = %d, want 25", got)
	}
}

func TestAddXP(t *testing.T) {
	r := rules(t)
	tests := []struct {
		name               string
		start, add         int
		wantXP, wantGained int
		wantLevel, wantMax int
	}{
		{"below boundary", 0, 82, 82, 0, 1, 10},
		{"crosses to 2", 80, 4, 84, 1, 2, 11},
		{"exactly at boundary", 0, 83, 83, 1, 2, 11},
		{"several levels", 0, 1154, 1154, 9, 10, 19},
		{"to max", 13000, 400, 13400, 1, 30, 39},
		{"past max", 13363, 1000, 14363, 0, 30, 39},
		{"zero ignored", 50, 0, 50, 0, 1, 10},
		{"negative ignored", 100, -50, 100, 0, 2, 11},
		{"saturates", math.MaxInt - 1, 10, math.MaxInt, 0, 30, 39},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &world.Player{XP: tt.start, HP: 7}
			if got := p.AddXP(r, tt.add); got != tt.wantGained {
				t.Errorf("gained %d levels, want %d", got, tt.wantGained)
			}
			if p.XP != tt.wantXP {
				t.Errorf("XP = %d, want %d", p.XP, tt.wantXP)
			}
			if got := p.CombatLevel(r); got != tt.wantLevel {
				t.Errorf("level = %d, want %d", got, tt.wantLevel)
			}
			if got := p.MaxHP(r); got != tt.wantMax {
				t.Errorf("max HP = %d, want %d", got, tt.wantMax)
			}
			if p.HP != 7 {
				t.Errorf("HP = %d, want unchanged 7", p.HP)
			}
		})
	}
}

func TestHeal(t *testing.T) {
	r := rules(t)
	tests := []struct {
		hp, n, wantHP, wantHealed int
	}{
		{5, 1, 6, 1},
		{5, 3, 8, 3},
		{9, 5, 10, 1},
		{10, 1, 10, 0},
		{12, 1, 12, 0}, // above max is left alone
		{5, 0, 5, 0},
		{5, -2, 5, 0},
	}
	for _, tt := range tests {
		p := &world.Player{HP: tt.hp}
		if got := p.Heal(r, tt.n); got != tt.wantHealed || p.HP != tt.wantHP {
			t.Errorf("Heal(%d) from %d = %d healed, HP %d; want %d, %d", tt.n, tt.hp, got, p.HP, tt.wantHealed, tt.wantHP)
		}
	}
}

func TestMarkCombat(t *testing.T) {
	at := time.Unix(1000, 0)
	p := &world.Player{}
	p.MarkCombat(at)
	if !p.LastCombat.Equal(at) {
		t.Errorf("LastCombat = %v, want %v", p.LastCombat, at)
	}
}
