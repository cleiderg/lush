package world

import (
	"math"

	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

// Skill is one of the non-combat skills (docs/specs/skills.md, D-051).
type Skill uint8

// The skills, in the order Player.SkillXP stores them.
const (
	Woodcutting Skill = iota
	Fishing
	Cooking
	NumSkills
)

// String is the skill's name.
func (s Skill) String() string {
	switch s {
	case Woodcutting:
		return "woodcutting"
	case Fishing:
		return "fishing"
	case Cooking:
		return "cooking"
	}
	return "unknown"
}

// SkillXPScale is how many stored XP units make one XP point: skill XP is kept
// in tenths so awards like 37.5 and 67.5 are exact.
const SkillXPScale = 10

// SkillMinLevel and SkillMaxLevel bound every skill level.
const (
	SkillMinLevel = 1
	SkillMaxLevel = 30
)

// SkillLevelForXP is the level for a skill XP total in tenths, from the same
// OSRS table as combat, clamped to SkillMinLevel..SkillMaxLevel.
func SkillLevelForXP(tenths int) int {
	return min(max(formula.LevelForXP(tenths/SkillXPScale), SkillMinLevel), SkillMaxLevel)
}

// SkillLevel is the player's level in skill s.
func (p *Player) SkillLevel(s Skill) int { return SkillLevelForXP(p.SkillXP[s]) }

// AddSkillXP adds tenths (XP in tenths of a point; ignored if not positive) to
// skill s. It reports whether the player gained a level and their level after.
// Combat XP is untouched.
func (p *Player) AddSkillXP(s Skill, tenths int) (leveled bool, level int) {
	before := p.SkillLevel(s)
	if tenths <= 0 {
		return false, before
	}
	if p.SkillXP[s] > math.MaxInt-tenths {
		p.SkillXP[s] = math.MaxInt
	} else {
		p.SkillXP[s] += tenths
	}
	level = p.SkillLevel(s)
	return level > before, level
}

// TotalLevel is the player's combat level plus their level in every skill
// (4 to 120 with three skills).
func (p *Player) TotalLevel(r gamedata.Rules) int {
	total := p.CombatLevel(r)
	for s := range NumSkills {
		total += p.SkillLevel(s)
	}
	return total
}

// GatherChance is the chance one gathering attempt succeeds at level:
// low + (high − low) × (level − 1) / 29, plus the tool's bonus, clamped to
// 0..1. level is clamped to SkillMinLevel..SkillMaxLevel.
func GatherChance(low, high, toolBonus float64, level int) float64 {
	level = min(max(level, SkillMinLevel), SkillMaxLevel)
	c := low + (high-low)*float64(level-SkillMinLevel)/float64(SkillMaxLevel-SkillMinLevel) + toolBonus
	return min(max(c, 0), 1)
}

// BurnChance is the chance cooking burns the food at level. It is atRequired
// at the required level (and below), falls in a straight line to 0 at
// stopLevel, and is 0 from there on. The result is clamped to 0..1.
func BurnChance(atRequired float64, required, stopLevel, level int) float64 {
	if level >= stopLevel {
		return 0
	}
	c := atRequired
	if level > required {
		c = atRequired * float64(stopLevel-level) / float64(stopLevel-required)
	}
	return min(max(c, 0), 1)
}
