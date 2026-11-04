package gamedata_test

import (
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

func TestGroupsPlaceholders(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	g := d.Groups
	ints := []struct {
		name      string
		got, want int
	}{
		{"found cost", g.Group.FoundCost, 500}, {"name min", g.Group.NameMin, 3}, {"name max", g.Group.NameMax, 20},
		{"banners", len(g.Group.Banners), 12}, {"max members", g.Group.MaxMembers, 30},
		{"leave cooldown h", g.Group.LeaveCooldownH, 24}, {"leader inactive days", g.Group.LeaderInactiveDays, 14},
		{"claim", g.Hall.ClaimCost, 2000}, {"upkeep", g.Hall.Upkeep, 500}, {"upkeep hour", g.Hall.UpkeepHour, 0},
		{"door hp", g.Door.HP, 300}, {"door defence", g.Door.Defence, 40}, {"door self repair", g.Door.SelfRepairMin, 10},
		{"door repair cost", g.Door.RepairCost, 50}, {"door log days", g.Door.LogDays, 7},
		{"heist channel", g.Heist.ChannelS, 60}, {"heist lost per hit", g.Heist.LostPerHitS, 10},
		{"heist distance", g.Heist.MaxDistanceTiles, 1}, {"heist gold pct", g.Heist.GoldPercent, 10},
		{"heist gold cap", g.Heist.GoldCap, 2000}, {"heist sabotage", g.Heist.SabotagePercent, 25},
		{"heist backdoor h", g.Heist.BackdoorH, 24}, {"heist cooldown h", g.Heist.CooldownH, 24},
		{"siege start", g.Siege.StartHour, 19}, {"siege end", g.Siege.EndHour, 20},
		{"declare cost", g.Siege.DeclareCost, 1000}, {"capture", g.Siege.CaptureS, 30}, {"gate hp", g.Siege.GateHP, 2000},
		{"gate loss", g.Siege.GateHPLossPercentPerWeek, 20}, {"gate floor", g.Siege.GateHPFloorPercent, 20},
		{"damage bonus", g.Siege.DamageBonusPercentPerWeek, 5}, {"damage cap", g.Siege.DamageBonusCapPercent, 25},
		{"shop tax max", g.Tax.ShopMaxPercent, 20}, {"over taxing", g.Tax.ShopOverTaxingAbovePercent, 10},
		{"group tax max", g.Tax.GroupMaxPercent, 10},
	}
	for _, c := range ints {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	strs := []struct{ name, got, want string }{
		{"upkeep weekday", g.Hall.UpkeepWeekday, "Monday"}, {"hall tz", g.Hall.Timezone, "America/Los_Angeles"},
		{"siege weekday", g.Siege.Weekday, "Sunday"}, {"siege tz", g.Siege.Timezone, "America/Los_Angeles"},
		{"declare by", g.Siege.DeclareByWeekday, "Saturday"},
	}
	for _, c := range strs {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestGroupsPermissionTable(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	// Spec table, per permission: leader, officer, member, recruit
	// (Y yes, . no, T set by leader).
	want := map[string]string{
		gamedata.PermInvite: "YY..", gamedata.PermPromoteDemote: "YY..", gamedata.PermKick: "YY..",
		gamedata.PermKeys: "YYT.", gamedata.PermVaultRoom: "YYT.", gamedata.PermWithdraw: "YT..",
		gamedata.PermDoorLog: "YY..", gamedata.PermHallAndSiege: "Y...",
	}
	if len(d.Groups.Permissions) != len(want) {
		t.Fatalf("%d permissions, want %d", len(d.Groups.Permissions), len(want))
	}
	for _, p := range d.Groups.Permissions {
		var got strings.Builder
		for _, r := range gamedata.Ranks {
			c := p.Cell(r)
			switch c.Mode {
			case gamedata.CellYes:
				got.WriteByte('Y')
			case gamedata.CellNo:
				got.WriteByte('.')
			case gamedata.CellSetByLeader:
				got.WriteByte('T')
				if c.On {
					t.Errorf("%s/%s toggle is on by default", p.Key, r)
				}
			}
		}
		if got.String() != want[p.Key] {
			t.Errorf("%s = %s, want %s", p.Key, got.String(), want[p.Key])
		}
	}
}

func TestValidGroupName(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"valid", "Anteater Guild 7", true},
		{"minimum length", "abc", true},
		{"maximum length", strings.Repeat("a", 20), true},
		{"blocked word lower", "the shit club", false},
		{"blocked word upper", "NAZI Club", false},
		{"blocked word mixed", "My HiTlEr Fans", false},
		{"too short", "ab", false},
		{"empty", "", false},
		{"too long", strings.Repeat("a", 21), false},
		{"punctuation", "Cool-Group", false},
		{"underscore", "Cool_Group", false},
		{"emoji", "Cool 😀 Group", false},
		{"accented letter", "Café Club", false},
	}
	for _, tt := range tests {
		if got := d.Groups.ValidGroupName(tt.in); got != tt.want {
			t.Errorf("%s: ValidGroupName(%q) = %v, want %v", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestLoadRejectsBadGroups(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		wantErr  string
	}{
		{"negative cost", `"found_cost": 500`, `"found_cost": -5`, `group.found_cost -5 must not be negative`},
		{"negative claim cost", `"claim_cost": 2000`, `"claim_cost": -1`, `hall.claim_cost -1 must not be negative`},
		{"eleven banners", `      {"id": 12, "colour": "#2b2b33"}`, `      {"id": 12, "colour": "#2b2b33"}, {"id": 13, "colour": "#010203"}`, `13 banner colours, want 12`},
		{"eleven banners short", `,
      {"id": 12, "colour": "#2b2b33"}`, ``, `11 banner colours, want 12`},
		{"unknown permission", `"key": "invite"`, `"key": "teleport"`, `unknown permission "teleport"`},
		{"unknown mode", `"officer": {"mode": "yes"}, "member": {"mode": "no"}, "recruit": {"mode": "no"}},
    {"key": "promote_demote"`, `"officer": {"mode": "maybe"}, "member": {"mode": "no"}, "recruit": {"mode": "no"}},
    {"key": "promote_demote"`, `unknown mode "maybe"`},
		{"toggle on unmarked cell", `{"key": "invite", "leader": {"mode": "yes"}, "officer": {"mode": "yes"}, "member": {"mode": "no"}`,
			`{"key": "invite", "leader": {"mode": "yes"}, "officer": {"mode": "yes"}, "member": {"mode": "set_by_leader"}`,
			`permission "invite" rank "member": the spec does not mark this cell "set by leader"`},
		{"toggle on by default", `"member": {"mode": "set_by_leader", "on": false}, "recruit": {"mode": "no"}},
    {"key": "vault_room"`, `"member": {"mode": "set_by_leader", "on": true}, "recruit": {"mode": "no"}},
    {"key": "vault_room"`, `a toggle must be off by default`},
		{"marked cell not toggleable", `"officer": {"mode": "set_by_leader", "on": false}`, `"officer": {"mode": "yes"}`, `must be set_by_leader`},
		{"unknown rank field", `"recruit": {"mode": "no"}},
    {"key": "promote_demote"`, `"cadet": {"mode": "no"}},
    {"key": "promote_demote"`, `unknown field "cadet"`},
		{"over taxing above max", `"shop_over_taxing_above_percent": 10`, `"shop_over_taxing_above_percent": 30`, `above shop_max_percent`},
		{"bad timezone", `"timezone": "America/Los_Angeles",
    "declare_by`, `"timezone": "Mars/Base",
    "declare_by`, `unknown timezone "Mars/Base"`},
		{"upper case blocked word", `"fuck"`, `"Fuck"`, `must be non-empty lower case`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gamedata.Load(fixture(t, gamedata.GroupsFile, tt.old, tt.new))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
