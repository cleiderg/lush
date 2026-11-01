package gamedata

import (
	"fmt"
	"time"
)

// Skill is a gathering or cooking skill. SkillNone means "not a tool".
type Skill string

const (
	SkillNone        Skill = ""
	SkillWoodcutting Skill = "woodcutting"
	SkillFishing     Skill = "fishing"
	SkillCooking     Skill = "cooking"
)

// Skill levels run from MinSkillLevel to MaxSkillLevel (docs/specs/skills.md).
const (
	MinSkillLevel = 1
	MaxSkillLevel = 30
)

// toolSkill reports whether s is a skill a tool can be for.
func (s Skill) toolSkill() bool { return s == SkillWoodcutting || s == SkillFishing }

func (it Item) validateTool(where string) error {
	if it.ToolSkill == SkillNone {
		if it.ToolLevel != 0 || it.ToolBonus != 0 {
			return fmt.Errorf("gamedata: %s: tool_level and tool_bonus need a tool_skill", where)
		}
		return nil
	}
	if !it.ToolSkill.toolSkill() {
		return fmt.Errorf("gamedata: %s: unknown tool_skill %q", where, it.ToolSkill)
	}
	if it.ToolLevel < MinSkillLevel || it.ToolLevel > MaxSkillLevel {
		return fmt.Errorf("gamedata: %s: tool_level %d must be %d to %d", where, it.ToolLevel, MinSkillLevel, MaxSkillLevel)
	}
	if it.ToolBonus < 0 || it.ToolBonus > 1 {
		return fmt.Errorf("gamedata: %s: tool_bonus %v must be 0 to 1", where, it.ToolBonus)
	}
	return nil
}

// Tree is one row of the tree table.
type Tree struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Level       int     `json:"level"`
	XP          float64 `json:"xp"`
	ChanceLow   float64 `json:"chance_low"`  // success chance at the required level
	ChanceHigh  float64 `json:"chance_high"` // success chance at level 30
	RespawnSecs int     `json:"respawn_s"`
	LogKey      string  `json:"log"`
	Log         ItemID  `json:"-"` // resolved from LogKey by Load
}

// Respawn is how long a felled tree stays a stump.
func (t Tree) Respawn() time.Duration { return seconds(t.RespawnSecs) }

// Catch is one fish a fishing spot can give.
type Catch struct {
	ItemKey string  `json:"item"`
	Level   int     `json:"level"`
	XP      float64 `json:"xp"`
	Item    ItemID  `json:"-"`
}

// FishingSpot is one row of the fishing spot table. Catches are in
// ascending level order; the server tries the best one the level allows first.
type FishingSpot struct {
	Key        string  `json:"key"`
	Name       string  `json:"name"`
	Level      int     `json:"level"`
	ToolKey    string  `json:"tool"`
	ChanceLow  float64 `json:"chance_low"`
	ChanceHigh float64 `json:"chance_high"`
	Catches    []Catch `json:"catches"`
	Tool       ItemID  `json:"-"`
}

// Recipe is one row of the cooking table.
type Recipe struct {
	RawKey            string  `json:"raw"`
	CookedKey         string  `json:"cooked"`
	BurntKey          string  `json:"burnt"`
	Level             int     `json:"level"`
	XP                float64 `json:"xp"`
	BurnChance        float64 `json:"burn_chance"`         // at Level; falls in a line to 0
	StopsBurningLevel int     `json:"stops_burning_level"` // burn chance is 0 from here
	Raw               ItemID  `json:"-"`
	Cooked            ItemID  `json:"-"`
	Burnt             ItemID  `json:"-"`
}

// Skills holds the gathering and cooking tables and constants.
type Skills struct {
	GatherIntervalTicks int           `json:"gather_interval_combat_ticks"`
	CookIntervalTicks   int           `json:"cook_interval_combat_ticks"`
	TreeFallOneIn       int           `json:"tree_fall_one_in"`
	SpotMoveMinSecs     int           `json:"fishing_spot_move_min_s"`
	SpotMoveMaxSecs     int           `json:"fishing_spot_move_max_s"`
	RangeTiles          int           `json:"range_tiles"`
	RangeDiagonal       bool          `json:"range_diagonal"`
	Trees               []Tree        `json:"trees"`
	FishingSpots        []FishingSpot `json:"fishing_spots"`
	Cooking             []Recipe      `json:"cooking"`
}

// SpotMoveMin and SpotMoveMax bound how long a fishing spot stays put.
func (s Skills) SpotMoveMin() time.Duration { return seconds(s.SpotMoveMinSecs) }
func (s Skills) SpotMoveMax() time.Duration { return seconds(s.SpotMoveMaxSecs) }

func levelOK(l int) bool { return l >= MinSkillLevel && l <= MaxSkillLevel }

func chanceOK(c float64) bool { return c >= 0 && c <= 1 }

// validateSkills checks the skill tables and resolves their item keys.
func (d *Data) validateSkills() error {
	s := &d.Skills
	w := SkillsFile
	if s.GatherIntervalTicks < 1 || s.CookIntervalTicks < 1 {
		return fmt.Errorf("gamedata: %s: intervals must be at least 1 combat tick", w)
	}
	if s.TreeFallOneIn < 1 {
		return fmt.Errorf("gamedata: %s: tree_fall_one_in %d must be at least 1", w, s.TreeFallOneIn)
	}
	if s.SpotMoveMinSecs < 1 || s.SpotMoveMaxSecs < s.SpotMoveMinSecs {
		return fmt.Errorf("gamedata: %s: bad fishing spot move time %d to %d s", w, s.SpotMoveMinSecs, s.SpotMoveMaxSecs)
	}
	if s.RangeTiles < 1 {
		return fmt.Errorf("gamedata: %s: range_tiles %d must be at least 1", w, s.RangeTiles)
	}
	seen := map[string]bool{}
	for i := range s.Trees {
		t := &s.Trees[i]
		where := fmt.Sprintf("%s: tree %q", w, t.Key)
		if t.Key == "" || seen["tree/"+t.Key] {
			return fmt.Errorf("gamedata: %s: empty or duplicate key", where)
		}
		seen["tree/"+t.Key] = true
		if !levelOK(t.Level) || t.XP <= 0 || !chanceOK(t.ChanceLow) || !chanceOK(t.ChanceHigh) || t.RespawnSecs < 1 {
			return fmt.Errorf("gamedata: %s: level must be %d to %d, xp and respawn_s positive, chances 0 to 1", where, MinSkillLevel, MaxSkillLevel)
		}
		id, err := d.resolve(t.LogKey, where+" log")
		if err != nil {
			return err
		}
		t.Log = id
	}
	for i := range s.FishingSpots {
		f := &s.FishingSpots[i]
		where := fmt.Sprintf("%s: fishing spot %q", w, f.Key)
		if f.Key == "" || seen["spot/"+f.Key] {
			return fmt.Errorf("gamedata: %s: empty or duplicate key", where)
		}
		seen["spot/"+f.Key] = true
		if !levelOK(f.Level) || !chanceOK(f.ChanceLow) || !chanceOK(f.ChanceHigh) || len(f.Catches) == 0 {
			return fmt.Errorf("gamedata: %s: level must be %d to %d, chances 0 to 1, and it needs a catch", where, MinSkillLevel, MaxSkillLevel)
		}
		id, err := d.resolve(f.ToolKey, where+" tool")
		if err != nil {
			return err
		}
		if tool, _ := d.Item(id); tool.ToolSkill != SkillFishing {
			return fmt.Errorf("gamedata: %s: item %q is not a fishing tool", where, f.ToolKey)
		}
		f.Tool = id
		for k := range f.Catches {
			c := &f.Catches[k]
			if !levelOK(c.Level) || c.XP <= 0 {
				return fmt.Errorf("gamedata: %s catch %q: level must be %d to %d and xp positive", where, c.ItemKey, MinSkillLevel, MaxSkillLevel)
			}
			if c.Item, err = d.resolve(c.ItemKey, where+" catch"); err != nil {
				return err
			}
		}
	}
	for i := range s.Cooking {
		r := &s.Cooking[i]
		where := fmt.Sprintf("%s: cooking %q", w, r.RawKey)
		if !levelOK(r.Level) || !levelOK(r.StopsBurningLevel) || r.StopsBurningLevel <= r.Level || r.XP <= 0 || !chanceOK(r.BurnChance) {
			return fmt.Errorf("gamedata: %s: levels must be %d to %d with stops_burning_level above level, xp positive, burn_chance 0 to 1", where, MinSkillLevel, MaxSkillLevel)
		}
		var err error
		if r.Raw, err = d.resolve(r.RawKey, where+" raw"); err != nil {
			return err
		}
		if r.Cooked, err = d.resolve(r.CookedKey, where+" cooked"); err != nil {
			return err
		}
		if r.Burnt, err = d.resolve(r.BurntKey, where+" burnt"); err != nil {
			return err
		}
		if cooked, _ := d.Item(r.Cooked); cooked.Kind != KindFood {
			return fmt.Errorf("gamedata: %s: cooked item %q is not food", where, r.CookedKey)
		}
	}
	return nil
}
