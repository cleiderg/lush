package gamedata_test

import (
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

func itemKey(t *testing.T, d *gamedata.Data, id gamedata.ItemID) string {
	t.Helper()
	it, ok := d.Item(id)
	if !ok {
		t.Fatalf("no item %d", id)
	}
	return it.Key
}

// TestSkillsMatchSpec checks skills.json against the tables in docs/specs/skills.md.
func TestSkillsMatchSpec(t *testing.T) {
	d := load(t)
	s := d.Skills
	if s.GatherIntervalTicks != 4 || s.CookIntervalTicks != 4 || s.TreeFallOneIn != 8 ||
		s.SpotMoveMinSecs != 180 || s.SpotMoveMaxSecs != 300 || s.RangeTiles != 1 || !s.RangeDiagonal {
		t.Errorf("constants = %+v", s)
	}

	trees := []struct {
		key, log      string
		level         int
		xp, low, high float64
		respawn       int
	}{
		{"tree", "logs", 1, 25, 0.25, 0.75, 15},
		{"oak", "oak_logs", 15, 37.5, 0.12, 0.50, 30},
		{"eucalyptus", "eucalyptus_logs", 25, 67.5, 0.06, 0.32, 60},
	}
	if len(s.Trees) != len(trees) {
		t.Fatalf("%d trees, want %d", len(s.Trees), len(trees))
	}
	for i, w := range trees {
		g := s.Trees[i]
		if g.Key != w.key || itemKey(t, d, g.Log) != w.log || g.Level != w.level || g.XP != w.xp ||
			g.ChanceLow != w.low || g.ChanceHigh != w.high || g.RespawnSecs != w.respawn {
			t.Errorf("tree %d = %+v, want %+v", i, g, w)
		}
	}

	type catch struct {
		item  string
		level int
		xp    float64
	}
	spots := []struct {
		key, tool string
		level     int
		low, high float64
		catches   []catch
	}{
		{"net_spot", "small_net", 1, 0.25, 0.75, []catch{{"raw_shrimp", 1, 10}}},
		{"rod_spot", "fishing_rod", 5, 0.20, 0.60, []catch{{"raw_sardine", 5, 20}, {"raw_trout", 20, 50}}},
	}
	if len(s.FishingSpots) != len(spots) {
		t.Fatalf("%d fishing spots, want %d", len(s.FishingSpots), len(spots))
	}
	for i, w := range spots {
		g := s.FishingSpots[i]
		if g.Key != w.key || itemKey(t, d, g.Tool) != w.tool || g.Level != w.level || g.ChanceLow != w.low || g.ChanceHigh != w.high || len(g.Catches) != len(w.catches) {
			t.Errorf("spot %d = %+v, want %+v", i, g, w)
			continue
		}
		for k, c := range w.catches {
			if got := g.Catches[k]; itemKey(t, d, got.Item) != c.item || got.Level != c.level || got.XP != c.xp {
				t.Errorf("spot %s catch %d = %+v, want %+v", w.key, k, got, c)
			}
		}
	}

	recipes := []struct {
		raw, cooked string
		level       int
		xp, burn    float64
		stops       int
	}{
		{"raw_shrimp", "shrimp", 1, 30, 0.5, 15},
		{"raw_sardine", "sardine", 5, 40, 0.5, 20},
		{"raw_trout", "trout", 15, 70, 0.5, 30},
	}
	if len(s.Cooking) != len(recipes) {
		t.Fatalf("%d recipes, want %d", len(s.Cooking), len(recipes))
	}
	for i, w := range recipes {
		g := s.Cooking[i]
		if itemKey(t, d, g.Raw) != w.raw || itemKey(t, d, g.Cooked) != w.cooked || itemKey(t, d, g.Burnt) != "burnt_fish" ||
			g.Level != w.level || g.XP != w.xp || g.BurnChance != w.burn || g.StopsBurningLevel != w.stops {
			t.Errorf("recipe %d = %+v, want %+v", i, g, w)
		}
	}
}

// TestToolsMatchSpec checks the tool fields of the hatchets, net and rod.
func TestToolsMatchSpec(t *testing.T) {
	d := load(t)
	tests := []struct {
		key   string
		skill gamedata.Skill
		level int
		bonus float64
		kind  gamedata.ItemKind
	}{
		{"bronze_hatchet", gamedata.SkillWoodcutting, 1, 0, gamedata.KindWeapon},
		{"steel_hatchet", gamedata.SkillWoodcutting, 15, 0.10, gamedata.KindWeapon},
		{"small_net", gamedata.SkillFishing, 1, 0, gamedata.KindLoot},
		{"fishing_rod", gamedata.SkillFishing, 5, 0, gamedata.KindLoot},
	}
	for _, tt := range tests {
		it, ok := d.ItemByKey(tt.key)
		if !ok {
			t.Fatalf("no item %q", tt.key)
		}
		if !it.IsTool() || it.ToolSkill != tt.skill || it.ToolLevel != tt.level || it.ToolBonus != tt.bonus || it.Kind != tt.kind {
			t.Errorf("%s = %+v", tt.key, it)
		}
		if it.Kind.IsEquipment() == (tt.kind == gamedata.KindLoot) {
			t.Errorf("%s: equippable = %v", tt.key, it.Kind.IsEquipment())
		}
	}
}
