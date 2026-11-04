package gamedata

import (
	"fmt"
	"strings"
	"time"
)

// GroupsFile holds the milestone 12 numbers and lists (docs/specs/groups.md).
// Every number in it is a placeholder to tune after playtests.
const GroupsFile = "groups.json"

// BannerCount is the number of banner colour presets a group can pick from.
const BannerCount = 12

// Rank is a group rank.
type Rank string

const (
	RankLeader  Rank = "leader"
	RankOfficer Rank = "officer"
	RankMember  Rank = "member"
	RankRecruit Rank = "recruit"
)

// Ranks lists every rank, highest first.
var Ranks = []Rank{RankLeader, RankOfficer, RankMember, RankRecruit}

// Permission keys in the rank table.
const (
	PermInvite        = "invite"
	PermPromoteDemote = "promote_demote"
	PermKick          = "kick"
	PermKeys          = "keys"
	PermVaultRoom     = "vault_room"
	PermWithdraw      = "withdraw"
	PermDoorLog       = "door_log"
	PermHallAndSiege  = "hall_and_siege"
)

// Cell modes in the permission table.
const (
	CellYes         = "yes"
	CellNo          = "no"
	CellSetByLeader = "set_by_leader" // the leader can toggle it; On is the default
)

// setByLeaderCells are the only cells the spec marks "set by leader".
var setByLeaderCells = map[string]bool{
	PermKeys + "/" + string(RankMember):      true,
	PermVaultRoom + "/" + string(RankMember): true,
	PermWithdraw + "/" + string(RankOfficer): true,
}

// Banner is one colour preset.
type Banner struct {
	ID     int    `json:"id"`
	Colour string `json:"colour"`
}

// GroupRules are the group numbers.
type GroupRules struct {
	FoundCost          int      `json:"found_cost"`
	NameMin            int      `json:"name_min"`
	NameMax            int      `json:"name_max"`
	MaxMembers         int      `json:"max_members"`
	LeaveCooldownH     int      `json:"leave_cooldown_h"`
	LeaderInactiveDays int      `json:"leader_inactive_days"`
	Banners            []Banner `json:"banners"`
	// BlockedWords may not appear in a group name, in any letter case.
	BlockedWords []string `json:"blocked_words"`
}

// PermCell is one cell of the permission table.
type PermCell struct {
	Mode string `json:"mode"`
	// On is the default for a set_by_leader cell. It is off for all of them.
	On bool `json:"on"`
}

// Permission is one row of the permission table.
type Permission struct {
	Key     string   `json:"key"`
	Leader  PermCell `json:"leader"`
	Officer PermCell `json:"officer"`
	Member  PermCell `json:"member"`
	Recruit PermCell `json:"recruit"`
}

// Cell returns the row's cell for a rank.
func (p Permission) Cell(r Rank) PermCell {
	switch r {
	case RankLeader:
		return p.Leader
	case RankOfficer:
		return p.Officer
	case RankMember:
		return p.Member
	}
	return p.Recruit
}

// HallRules are the hall numbers. Upkeep is due on UpkeepWeekday at UpkeepHour.
type HallRules struct {
	ClaimCost     int    `json:"claim_cost"`
	Upkeep        int    `json:"upkeep"`
	UpkeepWeekday string `json:"upkeep_weekday"`
	UpkeepHour    int    `json:"upkeep_hour"`
	Timezone      string `json:"timezone"`
}

// DoorRules are the door numbers.
type DoorRules struct {
	HP            int `json:"hp"`
	Defence       int `json:"defence"`
	SelfRepairMin int `json:"self_repair_min"`
	RepairCost    int `json:"repair_cost"`
	LogDays       int `json:"log_days"`
}

// HeistRules are the heist numbers.
type HeistRules struct {
	ChannelS         int `json:"channel_s"`
	LostPerHitS      int `json:"lost_per_hit_s"`
	MaxDistanceTiles int `json:"max_distance_tiles"`
	GoldPercent      int `json:"gold_percent"`
	GoldCap          int `json:"gold_cap"`
	SabotagePercent  int `json:"sabotage_percent"`
	BackdoorH        int `json:"backdoor_h"`
	CooldownH        int `json:"cooldown_h"`
}

// SiegeRules are the siege numbers. The siege runs on Weekday from StartHour
// to EndHour; groups declare by the end of DeclareByWeekday.
type SiegeRules struct {
	Weekday                   string `json:"weekday"`
	StartHour                 int    `json:"start_hour"`
	EndHour                   int    `json:"end_hour"`
	Timezone                  string `json:"timezone"`
	DeclareByWeekday          string `json:"declare_by_weekday"`
	DeclareCost               int    `json:"declare_cost"`
	CaptureS                  int    `json:"capture_s"`
	GateHP                    int    `json:"gate_hp"`
	GateHPLossPercentPerWeek  int    `json:"gate_hp_loss_percent_per_week"`
	GateHPFloorPercent        int    `json:"gate_hp_floor_percent"`
	DamageBonusPercentPerWeek int    `json:"damage_bonus_percent_per_week"`
	DamageBonusCapPercent     int    `json:"damage_bonus_cap_percent"`
}

// TaxRules are the tax limits the ruler can choose within.
type TaxRules struct {
	ShopMaxPercent             int `json:"shop_max_percent"`
	ShopOverTaxingAbovePercent int `json:"shop_over_taxing_above_percent"`
	GroupMaxPercent            int `json:"group_max_percent"`
}

// Groups is the parsed groups.json.
type Groups struct {
	Group       GroupRules   `json:"group"`
	Permissions []Permission `json:"permissions"`
	Hall        HallRules    `json:"hall"`
	Door        DoorRules    `json:"door"`
	Heist       HeistRules   `json:"heist"`
	Siege       SiegeRules   `json:"siege"`
	Tax         TaxRules     `json:"tax"`
}

// ValidGroupName reports whether name is 3 to 20 letters, numbers and spaces
// and contains no blocked word in any letter case. Length counts characters,
// and only ASCII letters and digits are allowed.
func (g *Groups) ValidGroupName(name string) bool {
	n := len([]rune(name))
	if n < g.Group.NameMin || n > g.Group.NameMax {
		return false
	}
	for _, r := range name {
		ok := r == ' ' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	lower := strings.ToLower(name)
	for _, w := range g.Group.BlockedWords {
		if strings.Contains(lower, w) {
			return false
		}
	}
	return true
}

func (d *Data) validateGroups() error {
	g := &d.Groups
	bad := func(format string, a ...any) error {
		return fmt.Errorf("gamedata: %s: %s", GroupsFile, fmt.Sprintf(format, a...))
	}
	nonNeg := []struct {
		name string
		v    int
	}{
		{"group.found_cost", g.Group.FoundCost}, {"hall.claim_cost", g.Hall.ClaimCost}, {"hall.upkeep", g.Hall.Upkeep},
		{"door.repair_cost", g.Door.RepairCost}, {"heist.gold_cap", g.Heist.GoldCap}, {"siege.declare_cost", g.Siege.DeclareCost},
		{"heist.lost_per_hit_s", g.Heist.LostPerHitS}, {"heist.max_distance_tiles", g.Heist.MaxDistanceTiles},
		{"heist.gold_percent", g.Heist.GoldPercent}, {"heist.sabotage_percent", g.Heist.SabotagePercent},
		{"siege.gate_hp_loss_percent_per_week", g.Siege.GateHPLossPercentPerWeek}, {"siege.gate_hp_floor_percent", g.Siege.GateHPFloorPercent},
		{"siege.damage_bonus_percent_per_week", g.Siege.DamageBonusPercentPerWeek}, {"siege.damage_bonus_cap_percent", g.Siege.DamageBonusCapPercent},
		{"tax.shop_max_percent", g.Tax.ShopMaxPercent}, {"tax.shop_over_taxing_above_percent", g.Tax.ShopOverTaxingAbovePercent},
		{"tax.group_max_percent", g.Tax.GroupMaxPercent}, {"hall.upkeep_hour", g.Hall.UpkeepHour},
	}
	for _, c := range nonNeg {
		if c.v < 0 {
			return bad("%s %d must not be negative", c.name, c.v)
		}
	}
	positive := []struct {
		name string
		v    int
	}{
		{"group.name_min", g.Group.NameMin}, {"group.max_members", g.Group.MaxMembers},
		{"group.leave_cooldown_h", g.Group.LeaveCooldownH}, {"group.leader_inactive_days", g.Group.LeaderInactiveDays},
		{"door.hp", g.Door.HP}, {"door.defence", g.Door.Defence}, {"door.self_repair_min", g.Door.SelfRepairMin}, {"door.log_days", g.Door.LogDays},
		{"heist.channel_s", g.Heist.ChannelS}, {"heist.backdoor_h", g.Heist.BackdoorH}, {"heist.cooldown_h", g.Heist.CooldownH},
		{"siege.capture_s", g.Siege.CaptureS}, {"siege.gate_hp", g.Siege.GateHP},
	}
	for _, c := range positive {
		if c.v < 1 {
			return bad("%s %d must be at least 1", c.name, c.v)
		}
	}
	if g.Group.NameMax < g.Group.NameMin {
		return bad("name_max %d is below name_min %d", g.Group.NameMax, g.Group.NameMin)
	}
	for _, p := range []struct {
		name string
		v    int
	}{
		{"heist.gold_percent", g.Heist.GoldPercent}, {"heist.sabotage_percent", g.Heist.SabotagePercent},
		{"siege.gate_hp_floor_percent", g.Siege.GateHPFloorPercent}, {"tax.shop_max_percent", g.Tax.ShopMaxPercent},
		{"tax.group_max_percent", g.Tax.GroupMaxPercent},
	} {
		if p.v > 100 {
			return bad("%s %d is over 100", p.name, p.v)
		}
	}
	if g.Tax.ShopOverTaxingAbovePercent > g.Tax.ShopMaxPercent {
		return bad("shop_over_taxing_above_percent %d is above shop_max_percent %d", g.Tax.ShopOverTaxingAbovePercent, g.Tax.ShopMaxPercent)
	}
	for _, h := range []struct {
		name string
		v    int
	}{{"hall.upkeep_hour", g.Hall.UpkeepHour}, {"siege.start_hour", g.Siege.StartHour}, {"siege.end_hour", g.Siege.EndHour}} {
		if h.v > 24 || (h.name == "hall.upkeep_hour" && h.v > 23) {
			return bad("%s %d is not an hour of the day", h.name, h.v)
		}
	}
	if g.Siege.EndHour <= g.Siege.StartHour {
		return bad("siege end_hour %d must be after start_hour %d", g.Siege.EndHour, g.Siege.StartHour)
	}
	for _, w := range []struct {
		name string
		v    string
	}{{"hall.upkeep_weekday", g.Hall.UpkeepWeekday}, {"siege.weekday", g.Siege.Weekday}, {"siege.declare_by_weekday", g.Siege.DeclareByWeekday}} {
		if !isWeekday(w.v) {
			return bad("%s %q is not a weekday name", w.name, w.v)
		}
	}
	for _, tz := range []string{g.Hall.Timezone, g.Siege.Timezone} {
		if _, err := time.LoadLocation(tz); err != nil || tz == "" {
			return bad("unknown timezone %q", tz)
		}
	}

	if len(g.Group.Banners) != BannerCount {
		return bad("%d banner colours, want %d", len(g.Group.Banners), BannerCount)
	}
	ids := map[int]bool{}
	colours := map[string]bool{}
	for _, b := range g.Group.Banners {
		if b.ID < 1 || ids[b.ID] {
			return bad("banner id %d is not unique and at least 1", b.ID)
		}
		ids[b.ID] = true
		if !validColour(b.Colour) {
			return bad("banner %d colour %q is not #rrggbb", b.ID, b.Colour)
		}
		if colours[strings.ToLower(b.Colour)] {
			return bad("banner %d repeats colour %q", b.ID, b.Colour)
		}
		colours[strings.ToLower(b.Colour)] = true
	}
	if len(g.Group.BlockedWords) == 0 {
		return bad("blocked_words is empty")
	}
	for i, w := range g.Group.BlockedWords {
		if w == "" || w != strings.ToLower(w) || strings.TrimSpace(w) != w {
			return bad("blocked word %q must be non-empty lower case without outer spaces", w)
		}
		g.Group.BlockedWords[i] = w
	}

	wantKeys := []string{PermInvite, PermPromoteDemote, PermKick, PermKeys, PermVaultRoom, PermWithdraw, PermDoorLog, PermHallAndSiege}
	known := map[string]bool{}
	for _, k := range wantKeys {
		known[k] = true
	}
	seen := map[string]bool{}
	for _, p := range g.Permissions {
		if !known[p.Key] {
			return bad("unknown permission %q", p.Key)
		}
		if seen[p.Key] {
			return bad("duplicate permission %q", p.Key)
		}
		seen[p.Key] = true
		for _, r := range Ranks {
			c := p.Cell(r)
			switch c.Mode {
			case CellYes, CellNo:
				if c.On {
					return bad("permission %q rank %q: only a set_by_leader cell has a default", p.Key, r)
				}
			case CellSetByLeader:
				if !setByLeaderCells[p.Key+"/"+string(r)] {
					return bad("permission %q rank %q: the spec does not mark this cell \"set by leader\"", p.Key, r)
				}
				if c.On {
					return bad("permission %q rank %q: a toggle must be off by default", p.Key, r)
				}
			default:
				return bad("permission %q rank %q: unknown mode %q", p.Key, r, c.Mode)
			}
			if setByLeaderCells[p.Key+"/"+string(r)] && c.Mode != CellSetByLeader {
				return bad("permission %q rank %q must be set_by_leader", p.Key, r)
			}
		}
		if p.Leader.Mode != CellYes {
			return bad("permission %q: the leader always has every permission", p.Key)
		}
	}
	for _, k := range wantKeys {
		if !seen[k] {
			return bad("missing permission %q", k)
		}
	}
	return nil
}

func isWeekday(s string) bool {
	for d := time.Sunday; d <= time.Saturday; d++ {
		if d.String() == s {
			return true
		}
	}
	return false
}

func validColour(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
