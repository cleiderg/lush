package world

import (
	"math"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

// CombatLevelForXP is the combat level for an XP total: the formula level
// clamped to r.MinCombatLevel..r.MaxCombatLevel.
func CombatLevelForXP(r gamedata.Rules, xp int) int {
	return min(max(formula.LevelForXP(xp), r.MinCombatLevel), r.MaxCombatLevel)
}

// MaxHPForLevel is r.BaseHP + level.
func MaxHPForLevel(r gamedata.Rules, level int) int { return r.BaseHP + level }

// CombatLevel is the player's combat level from their XP.
func (p *Player) CombatLevel(r gamedata.Rules) int { return CombatLevelForXP(r, p.XP) }

// MaxHP is the player's maximum hitpoints at their combat level.
func (p *Player) MaxHP(r gamedata.Rules) int { return MaxHPForLevel(r, p.CombatLevel(r)) }

// AddXP adds xp (ignored if not positive) and returns how many combat levels
// the player gained. Max HP follows the level; current HP is unchanged.
func (p *Player) AddXP(r gamedata.Rules, xp int) int {
	if xp <= 0 {
		return 0
	}
	before := p.CombatLevel(r)
	if p.XP > math.MaxInt-xp {
		p.XP = math.MaxInt
	} else {
		p.XP += xp
	}
	return p.CombatLevel(r) - before
}

// Heal adds up to n HP, never above MaxHP, and returns the HP restored.
func (p *Player) Heal(r gamedata.Rules, n int) int {
	room := p.MaxHP(r) - p.HP
	if n <= 0 || room <= 0 {
		return 0
	}
	n = min(n, room)
	p.HP += n
	return n
}

// MarkCombat records that the player dealt or took damage at now.
func (p *Player) MarkCombat(now time.Time) { p.LastCombat = now }
