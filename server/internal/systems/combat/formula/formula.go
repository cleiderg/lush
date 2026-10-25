// Package formula is the Phase 1 combat maths from docs/specs/gameplay.md
// (Combat). Everything is pure and deterministic: callers pass levels and
// equipment bonuses, and randomness comes from an injected Rand. NPCs use the
// same functions with the numbers from the NPC table.
package formula

import "math"

// MaxLevel is the highest combat level in Phase 1 (D-015).
const MaxLevel = 30

// XPPerDamage is the XP gained per point of damage dealt.
const XPPerDamage = 4

// xpTable[l] is the XP needed for level l (OSRS experience table).
var xpTable = func() [MaxLevel + 1]int {
	var t [MaxLevel + 1]int
	points := 0
	for l := 1; l < MaxLevel; l++ {
		points += int(math.Floor(float64(l) + 300*math.Pow(2, float64(l)/7)))
		t[l+1] = points / 4
	}
	return t
}()

// XPForLevel returns the XP total at which level starts. Levels outside
// 1..MaxLevel are clamped.
func XPForLevel(level int) int {
	return xpTable[clampLevel(level)]
}

// LevelForXP returns the combat level for an XP total, from 1 to MaxLevel.
func LevelForXP(xp int) int {
	level := 1
	for l := 2; l <= MaxLevel && xp >= xpTable[l]; l++ {
		level = l
	}
	return level
}

// MaxHP is 9 + combat level.
func MaxHP(level int) int { return 9 + level }

// XPForDamage is the XP gained for dealing damage points of damage.
func XPForDamage(damage int) int { return XPPerDamage * damage }

// AttackRoll is (level + 8) × (accuracy + 64).
func AttackRoll(level, accuracy int) int { return (level + 8) * (accuracy + 64) }

// DefenceRoll is (level + 8) × (defence + 64).
func DefenceRoll(level, defence int) int { return (level + 8) * (defence + 64) }

// HitChance is the OSRS hit chance for an attacker at attLevel with accuracy
// bonus attAccuracy against a defender at defLevel with defence bonus
// defDefence.
func HitChance(attLevel, attAccuracy, defLevel, defDefence int) float64 {
	a := float64(AttackRoll(attLevel, attAccuracy))
	d := float64(DefenceRoll(defLevel, defDefence))
	if a > d {
		return 1 - (d+2)/(2*(a+1))
	}
	return a / (2 * (d + 1))
}

// MaxHit is floor(0.5 + (level + 8) × (strength + 64) / 640), at least 1.
func MaxHit(level, strength int) int {
	n := 320 + (level+8)*(strength+64) // 640 × (0.5 + roll/640), exact in integers
	if n < 640 {
		return 1
	}
	return n / 640
}

// Rand is the random source for rolls. *math/rand/v2.Rand satisfies it.
type Rand interface {
	Float64() float64 // uniform in [0, 1)
	IntN(n int) int   // uniform in [0, n)
}

// Attack is one attacker's swing at one defender.
type Attack struct {
	AttackerLevel int
	Accuracy      int // attacker's accuracy bonus
	Strength      int // attacker's strength bonus
	DefenderLevel int
	Defence       int // defender's defence bonus
}

// RollDamage rolls one swing: 0 on a miss, otherwise uniform in 1..MaxHit.
func RollDamage(r Rand, a Attack) int {
	if r.Float64() >= HitChance(a.AttackerLevel, a.Accuracy, a.DefenderLevel, a.Defence) {
		return 0
	}
	return 1 + r.IntN(MaxHit(a.AttackerLevel, a.Strength))
}

func clampLevel(level int) int {
	return min(max(level, 1), MaxLevel)
}
