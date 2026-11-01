package gamedata_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

// fixture returns the real data files with one replacement applied to file.
func fixture(t *testing.T, file, old, new string) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{}
	for _, name := range []string{gamedata.ItemsFile, gamedata.NPCsFile, gamedata.ShopFile, gamedata.RulesFile, gamedata.CardTablesFile, gamedata.SkillsFile} {
		b, err := os.ReadFile(filepath.Join("data", name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if name == file {
			if !strings.Contains(s, old) {
				t.Fatalf("%s does not contain %q", file, old)
			}
			s = strings.Replace(s, old, new, 1)
		}
		fsys[name] = &fstest.MapFile{Data: []byte(s)}
	}
	return fsys
}

func TestLoadRejectsBadData(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		old, new string
		wantErr  string
	}{
		{"drop of missing item", gamedata.NPCsFile,
			`"item": "acorn"`, `"item": "pinecone"`,
			`npcs.json: npc 2 ("squirrel") drop: unknown item "pinecone"`},
		{"shop entry for missing item", gamedata.ShopFile,
			`"item": "burrito"`, `"item": "taco"`,
			`shop.json: entry: unknown item "taco"`},
		{"starting kit equips missing item", gamedata.RulesFile,
			`"equipped": ["wooden_stick"`, `"equipped": ["laser_sword"`,
			`rules.json: starting kit equipped: unknown item "laser_sword"`},
		{"no bank slots", gamedata.RulesFile,
			`"bank_slots": 200`, `"bank_slots": 0`,
			`rules.json: bank_slots 0 must be at least 1`},
		{"negative bank distance", gamedata.RulesFile,
			`"bank_distance_tiles": 2`, `"bank_distance_tiles": -1`,
			`rules.json: bank_distance_tiles -1 must not be negative`},
		{"starting kit inventory has missing item", gamedata.RulesFile,
			`{"item": "boba"`, `{"item": "tea"`,
			`rules.json: starting kit inventory: unknown item "tea"`},
		{"equipment without slot", gamedata.ItemsFile,
			`"kind": "armor",  "slot": "body",   "accuracy": 0,  "strength": 0,  "defence": 4`,
			`"kind": "armor",  "accuracy": 0,  "strength": 0,  "defence": 4`,
			`items.json: item 4 ("hoodie"): equipment slot "" is not weapon, head, body or shield`},
		{"equipment with unknown slot", gamedata.ItemsFile,
			`"slot": "shield"`, `"slot": "feet"`,
			`items.json: item 5 ("cardboard_shield"): equipment slot "feet" is not weapon, head, body or shield`},
		{"duplicate item id", gamedata.ItemsFile,
			`"id": 2,`, `"id": 1,`,
			`items.json: item 1 ("bronze_dagger"): duplicate id 1 (also "wooden_stick")`},
		{"duplicate item key", gamedata.ItemsFile,
			`"key": "bronze_dagger"`, `"key": "wooden_stick"`,
			`items.json: item 2 ("wooden_stick"): duplicate key "wooden_stick"`},
		{"duplicate NPC id", gamedata.NPCsFile,
			`"id": 2,`, `"id": 1,`,
			`npcs.json: npc 1 ("squirrel"): duplicate id 1 (also "rabbit")`},
		{"duplicate NPC key", gamedata.NPCsFile,
			`"key": "squirrel"`, `"key": "rabbit"`,
			`npcs.json: npc 2 ("rabbit"): duplicate key "rabbit"`},
		{"id does not fit uint16", gamedata.ItemsFile,
			`"id": 14,`, `"id": 70000,`,
			`decode items.json`},
		{"id zero", gamedata.ItemsFile,
			`"id": 14,`, `"id": 0,`,
			`items.json: item 0 ("scrap_metal"): id must be 1 to 65535`},
		{"negative population", gamedata.NPCsFile,
			`"population": 12,`, `"population": -1,`,
			`npcs.json: npc 1 ("rabbit"): negative population -1`},
		{"population above max_alive", gamedata.NPCsFile,
			`"population": 1,`, `"population": 2,`,
			`npcs.json: npc 6 ("coyote_alpha"): population 2 exceeds max_alive 1`},
		{"unknown field", gamedata.RulesFile,
			`"starting_gold": 100`, `"starting_gems": 100`,
			`unknown field "starting_gems"`},
		{"card table with 1 seat", gamedata.CardTablesFile,
			`"id": 2, "seats": 4`, `"id": 2, "seats": 1`,
			`cardtables.json: table 2: seats 1 must be 2 to 4`},
		{"card table with 5 seats", gamedata.CardTablesFile,
			`"id": 3, "seats": 4`, `"id": 3, "seats": 5`,
			`cardtables.json: table 3: seats 5 must be 2 to 4`},
		{"card table with zero turn timer", gamedata.CardTablesFile,
			`"id": 4, "seats": 4, "turn_timer_s": 20`, `"id": 4, "seats": 4, "turn_timer_s": 0`,
			`cardtables.json: table 4: turn_timer_s 0 must be positive`},
		{"duplicate card table id", gamedata.CardTablesFile,
			`"id": 2,`, `"id": 1,`,
			`cardtables.json: table 1: duplicate id 1`},
		{"card table id zero", gamedata.CardTablesFile,
			`"id": 1,`, `"id": 0,`,
			`cardtables.json: table 0: id must be at least 1`},
		{"card table unknown field", gamedata.CardTablesFile,
			`"stake": 10}`, `"stake": 10, "rake": 1}`,
			`unknown field "rake"`},
		{"tool with unknown skill", gamedata.ItemsFile,
			`"tool_skill": "fishing", "tool_level": 1}`, `"tool_skill": "mining", "tool_level": 1}`,
			`items.json: item 17 ("small_net"): unknown tool_skill "mining"`},
		{"tool level above 30", gamedata.ItemsFile,
			`"tool_skill": "fishing", "tool_level": 5}`, `"tool_skill": "fishing", "tool_level": 31}`,
			`items.json: item 18 ("fishing_rod"): tool_level 31 must be 1 to 30`},
		{"tool level zero", gamedata.ItemsFile,
			`"tool_skill": "fishing", "tool_level": 5}`, `"tool_skill": "fishing", "tool_level": 0}`,
			`items.json: item 18 ("fishing_rod"): tool_level 0 must be 1 to 30`},
		{"tree drops unknown item", gamedata.SkillsFile,
			`"log": "oak_logs"`, `"log": "teak_logs"`,
			`skills.json: tree "oak" log: unknown item "teak_logs"`},
		{"fishing spot catches unknown item", gamedata.SkillsFile,
			`"item": "raw_trout"`, `"item": "raw_tuna"`,
			`skills.json: fishing spot "rod_spot" catch: unknown item "raw_tuna"`},
		{"fishing spot with unknown tool", gamedata.SkillsFile,
			`"tool": "small_net"`, `"tool": "harpoon"`,
			`skills.json: fishing spot "net_spot" tool: unknown item "harpoon"`},
		{"fishing spot with a non-fishing tool", gamedata.SkillsFile,
			`"tool": "small_net"`, `"tool": "steel_hatchet"`,
			`item "steel_hatchet" is not a fishing tool`},
		{"cooking of unknown item", gamedata.SkillsFile,
			`"cooked": "trout"`, `"cooked": "tuna"`,
			`skills.json: cooking "raw_trout" cooked: unknown item "tuna"`},
		{"tree level above 30", gamedata.SkillsFile,
			`"level": 25`, `"level": 31`,
			`skills.json: tree "eucalyptus": level must be 1 to 30`},
		{"card table with negative stake", gamedata.CardTablesFile,
			`"stake": 25}`, `"stake": -25}`,
			`cardtables.json: table 2: stake -25 must not be negative`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gamedata.Load(fixture(t, tt.file, tt.old, tt.new))
			if err == nil {
				t.Fatalf("Load succeeded, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestLoadShippedData checks the shipped files, population included, load.
func TestLoadShippedData(t *testing.T) {
	if _, err := gamedata.Load(fixture(t, "", "", "")); err != nil {
		t.Fatalf("Load shipped data: %v", err)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	fsys := fixture(t, "", "", "")
	delete(fsys, gamedata.ShopFile)
	if _, err := gamedata.Load(fsys); err == nil || !strings.Contains(err.Error(), "read shop.json") {
		t.Errorf("error = %v, want read shop.json", err)
	}
}
