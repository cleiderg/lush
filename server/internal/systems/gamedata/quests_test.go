package gamedata_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

func loadDefault(t *testing.T) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestStarterQuests checks each quest against docs/specs/quests.md (AC1).
func TestStarterQuests(t *testing.T) {
	d := loadDefault(t)
	tests := []struct {
		key     string
		name    string
		reqs    gamedata.QuestRequirements
		kinds   []gamedata.StepKind
		details func(t *testing.T, q gamedata.Quest)
		rewards gamedata.QuestRewards
	}{
		{"overdue", "Overdue", gamedata.QuestRequirements{},
			[]gamedata.StepKind{gamedata.StepTalk, gamedata.StepQuestItem, gamedata.StepBringItems},
			func(t *testing.T, q gamedata.Quest) {
				s := q.Steps[1]
				if s.ItemKey != "overdue_book" || s.SourceNPCKey != "crow" || s.Chance != 4 {
					t.Errorf("step 2 = %+v, want overdue_book from crow 1 in 4", s)
				}
				if q.Steps[0].NPCKey != "librarian" || q.Steps[2].NPCKey != "librarian" || q.Steps[2].Items[0].ItemKey != "overdue_book" {
					t.Errorf("talk and bring steps should be with the librarian: %+v", q.Steps)
				}
			},
			gamedata.QuestRewards{QuestPoints: 1, Gold: 150, XP: []gamedata.SkillXP{{Skill: "combat", XP: 500}},
				Items: []gamedata.ItemAmount{{ItemKey: "library_card", Quantity: 1}}}},
		{"coyote_trouble", "Coyote Trouble",
			gamedata.QuestRequirements{Levels: []gamedata.LevelReq{{Skill: "combat", Level: 15}}},
			[]gamedata.StepKind{gamedata.StepTalk, gamedata.StepKill, gamedata.StepQuestItem, gamedata.StepBringItems},
			func(t *testing.T, q gamedata.Quest) {
				if k := q.Steps[1]; k.NPCKey != "coyote" || k.Count != 5 {
					t.Errorf("step 2 = %+v, want 5 coyotes", k)
				}
				if s := q.Steps[2]; s.ItemKey != "alphas_fang" || s.SourceNPCKey != "coyote_alpha" || s.Chance != 1 {
					t.Errorf("step 3 = %+v, want alphas_fang always from coyote_alpha", s)
				}
				if q.Steps[0].NPCKey != "park_ranger" || q.Steps[3].NPCKey != "park_ranger" || q.Steps[3].Items[0].ItemKey != "alphas_fang" {
					t.Errorf("talk and bring steps should be with the park ranger: %+v", q.Steps)
				}
			},
			gamedata.QuestRewards{QuestPoints: 2, Gold: 400, XP: []gamedata.SkillXP{{Skill: "combat", XP: 2000}},
				Items: []gamedata.ItemAmount{{ItemKey: "rangers_machete", Quantity: 1}}}},
		{"fish_fry", "Fish Fry",
			gamedata.QuestRequirements{Levels: []gamedata.LevelReq{{Skill: "fishing", Level: 15}, {Skill: "cooking", Level: 15}}},
			[]gamedata.StepKind{gamedata.StepTalk, gamedata.StepBringItems},
			func(t *testing.T, q gamedata.Quest) {
				b := q.Steps[1]
				if b.NPCKey != "food_truck_cook" || len(b.Items) != 1 || b.Items[0].ItemKey != "trout" || b.Items[0].Quantity != 5 {
					t.Errorf("step 2 = %+v, want 5 trout to the cook", b)
				}
			},
			gamedata.QuestRewards{QuestPoints: 1, Gold: 250,
				XP:    []gamedata.SkillXP{{Skill: "cooking", XP: 1500}, {Skill: "fishing", XP: 1000}},
				Items: []gamedata.ItemAmount{{ItemKey: "chefs_hat", Quantity: 1}}}},
	}
	if len(d.Quests) != len(tests) {
		t.Fatalf("got %d quests, want %d", len(d.Quests), len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			q, ok := d.QuestByKey(tt.key)
			if !ok {
				t.Fatal("missing")
			}
			if q.Name != tt.name || q.ID == 0 || q.Description == "" {
				t.Errorf("name/id/description = %q %d %q", q.Name, q.ID, q.Description)
			}
			if !reflect.DeepEqual(q.Requirements.Levels, tt.reqs.Levels) || len(q.Requirements.Quests) != 0 {
				t.Errorf("requirements = %+v, want %+v", q.Requirements, tt.reqs)
			}
			var kinds []gamedata.StepKind
			for _, s := range q.Steps {
				kinds = append(kinds, s.Kind)
			}
			if !reflect.DeepEqual(kinds, tt.kinds) {
				t.Fatalf("step kinds = %v, want %v", kinds, tt.kinds)
			}
			tt.details(t, q)
			got := q.Rewards
			for i := range got.Items {
				got.Items[i].Item = 0 // resolved ids aren't part of the spec
			}
			if !reflect.DeepEqual(got, tt.rewards) {
				t.Errorf("rewards = %+v, want %+v", got, tt.rewards)
			}
			if byID, _ := d.Quest(q.ID); byID.Key != q.Key {
				t.Errorf("Quest(%d) = %q", q.ID, byID.Key)
			}
		})
	}
}

// TestTalkingNPCs checks the four talkers load as non-combat types (AC2).
func TestTalkingNPCs(t *testing.T) {
	d := loadDefault(t)
	want := []struct {
		id        gamedata.NPCTypeID
		key, name string
	}{{10, "librarian", "Librarian"}, {11, "park_ranger", "Park ranger"}, {12, "food_truck_cook", "Food truck cook"}, {13, "guide", "Guide"}}
	if len(d.Talkers) != len(want) {
		t.Fatalf("got %d talkers, want %d", len(d.Talkers), len(want))
	}
	for _, w := range want {
		n, ok := d.NPC(w.id)
		if !ok || n.Key != w.key || n.Name != w.name || !n.Talkable || n.Spot != w.key {
			t.Errorf("npc %d = %+v, want talkable %q with spot %q", w.id, n, w.key, w.key)
		}
		if byKey, _ := d.NPCByKey(w.key); byKey.ID != w.id {
			t.Errorf("NPCByKey(%q) = %d", w.key, byKey.ID)
		}
	}
	for _, n := range d.NPCs {
		if n.Talkable {
			t.Errorf("%q is talkable but listed with combat NPCs", n.Key)
		}
	}
}

const talker = `{"id": 10, "key": "librarian", "name": "Librarian", "talkable": true, "spot": "librarian"}`

// TestLoadRejectsBadTalkers covers AC2's rejections.
func TestLoadRejectsBadTalkers(t *testing.T) {
	tests := []struct {
		name, new, wantErr string
	}{
		{"hp", `"hp": 5, "talkable": true, "spot": "librarian"`, "no combat stats, population or drops"},
		{"level", `"level": 3, "talkable": true, "spot": "librarian"`, "no combat stats, population or drops"},
		{"accuracy", `"accuracy": 4, "talkable": true, "spot": "librarian"`, "no combat stats, population or drops"},
		{"population", `"population": 2, "talkable": true, "spot": "librarian"`, "no combat stats, population or drops"},
		{"drops", `"drops": [{"item": "acorn", "chance": 2, "quantity": 1}], "talkable": true, "spot": "librarian"`, "no combat stats, population or drops"},
		{"gold", `"gold_max": 3, "talkable": true, "spot": "librarian"`, "no combat stats, population or drops"},
		{"no spot", `"talkable": true, "spot": ""`, "needs a spot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gamedata.Load(fixture(t, gamedata.NPCsFile, `"talkable": true, "spot": "librarian"`, tt.new))
			wantErr(t, err, tt.wantErr)
		})
	}
	t.Run("spot without talkable", func(t *testing.T) {
		_, err := gamedata.Load(fixture(t, gamedata.NPCsFile, `"id": 1, "key": "rabbit"`, `"spot": "x", "id": 1, "key": "rabbit"`))
		wantErr(t, err, "spot on a non-talkable npc")
	})
	t.Run("duplicate spot", func(t *testing.T) {
		_, err := gamedata.Load(fixture(t, gamedata.NPCsFile, `"spot": "guide"`, `"spot": "librarian"`))
		wantErr(t, err, `spot "librarian" also used by`)
	})
	t.Run("id clash with combat npc", func(t *testing.T) {
		_, err := gamedata.Load(fixture(t, gamedata.NPCsFile, `"id": 10, "key": "librarian"`, `"id": 1, "key": "librarian"`))
		wantErr(t, err, "id 1 used by")
	})
}

// TestLoadRejectsBadQuests covers AC4's quest rejections.
func TestLoadRejectsBadQuests(t *testing.T) {
	const fishSteps = `{"kind": "talk", "npc": "food_truck_cook"},`
	tests := []struct {
		name     string
		old, new string
		wantErr  string
	}{
		{"unknown item in step", `"item": "trout"`, `"item": "salmon"`, `unknown item "salmon"`},
		{"unknown item in reward", `"item": "chefs_hat"`, `"item": "top_hat"`, `unknown item "top_hat"`},
		{"unknown npc in step", `"npc": "food_truck_cook", "items"`, `"npc": "chef", "items"`, `unknown talking npc "chef"`},
		{"unknown kill npc", `"npc": "coyote", "count"`, `"npc": "wolf", "count"`, `unknown npc "wolf"`},
		{"unknown source npc", `"source_npc": "crow"`, `"source_npc": "raven"`, `unknown source npc "raven"`},
		{"unknown skill in requirement", `"skill": "fishing", "level": 15`, `"skill": "mining", "level": 15`, `unknown skill "mining"`},
		{"unknown skill in reward", `"skill": "cooking", "xp"`, `"skill": "mining", "xp"`, `unknown skill "mining"`},
		{"unknown required quest", `"requirements": {"levels": [{"skill": "fishing"`, `"requirements": {"quests": ["nope"], "levels": [{"skill": "fishing"`, `unknown quest "nope"`},
		{"quest requires itself", `"requirements": {"levels": [{"skill": "fishing"`, `"requirements": {"quests": ["fish_fry"], "levels": [{"skill": "fishing"`, "cycle"},
		{"five steps", fishSteps, strings.Repeat(fishSteps, 4), "5 steps"},
		{"not a quest item", `"item": "overdue_book", "source_npc"`, `"item": "feather", "source_npc"`, "not a quest item"},
		{"unknown step kind", `"kind": "bring_items", "npc": "food_truck_cook"`, `"kind": "dance", "npc": "food_truck_cook"`, `unknown step kind "dance"`},
		{"level too high", `"skill": "cooking", "level": 15`, `"skill": "cooking", "level": 31`, "level 31 must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gamedata.Load(fixture(t, gamedata.QuestsFile, tt.old, tt.new))
			wantErr(t, err, tt.wantErr)
		})
	}
	t.Run("two quests require each other", func(t *testing.T) {
		fsys := fixture(t, gamedata.QuestsFile, `"requirements": {}`, `"requirements": {"quests": ["fish_fry"]}`)
		s := string(fsys[gamedata.QuestsFile].Data)
		s = strings.Replace(s, `"requirements": {"levels": [{"skill": "fishing"`, `"requirements": {"quests": ["overdue"], "levels": [{"skill": "fishing"`, 1)
		fsys[gamedata.QuestsFile] = &fstest.MapFile{Data: []byte(s)}
		_, err := gamedata.Load(fsys)
		wantErr(t, err, "cycle")
	})
}

// TestLoadRejectsBadDialogue covers AC4's reply limit and the dangling and
// unreachable checks of AC3.
func TestLoadRejectsBadDialogue(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		wantErr  string
	}{
		{"five replies", `{"text": "Bye"}]},
    {"id": "bank"`, `{"text": "Bye"}, {"text": "a"}, {"text": "b"}, {"text": "c"}]},
    {"id": "bank"`, "5 replies"},
		{"dangling next", `"next": "controls"`, `"next": "nowhere"`, `unknown node "nowhere"`},
		{"unreachable node", `{"id": "controls"`, `{"id": "orphan", "text": ["x"], "replies": [{"text": "y"}]},
    {"id": "controls"`, `node "orphan" is unreachable`},
		{"unknown quest in condition", `"quest": "overdue", "state": "complete"`, `"quest": "nope", "state": "complete"`, `unknown quest "nope"`},
		{"unknown quest to start", `"start_quest": "overdue"`, `"start_quest": "nope"`, `starts unknown quest "nope"`},
		{"missing step", `"state": "step", "step": 2`, `"state": "step", "step": 9`, "has no step 9"},
		{"unknown talker", `"npc": "guide"`, `"npc": "janitor"`, `unknown talking npc`},
		{"missing start", `"start": "menu"`, `"start": "gone"`, `start node "gone" does not exist`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := gamedata.Load(fixture(t, gamedata.DialogueFile, tt.old, tt.new))
			wantErr(t, err, tt.wantErr)
		})
	}
}

// TestDialogueWalk walks every node from each NPC's entry points (AC3).
func TestDialogueWalk(t *testing.T) {
	d := loadDefault(t)
	for _, n := range d.Talkers {
		t.Run(n.Key, func(t *testing.T) {
			dl, ok := d.DialogueFor(n.ID)
			if !ok {
				t.Fatal("no dialogue")
			}
			seen := map[string]bool{}
			var walk func(id string)
			walk = func(id string) {
				if seen[id] {
					return
				}
				node, ok := dl.Node(id)
				if !ok {
					t.Fatalf("dangling node %q", id)
				}
				seen[id] = true
				if len(node.Replies) < 1 || len(node.Replies) > gamedata.MaxDialogueReplies {
					t.Errorf("node %q has %d replies", id, len(node.Replies))
				}
				for _, r := range node.Replies {
					if r.Next != "" {
						walk(r.Next)
					}
				}
			}
			walk(dl.Start)
			for _, node := range dl.Nodes {
				if node.When != nil {
					walk(node.ID)
				}
			}
			for _, node := range dl.Nodes {
				if !seen[node.ID] {
					t.Errorf("node %q unreachable", node.ID)
				}
			}
		})
	}
}

// TestQuestNPCsHaveStateNodes checks each quest giver has a node for every
// state of its quest, and the Guide covers the four topics.
func TestQuestNPCsHaveStateNodes(t *testing.T) {
	d := loadDefault(t)
	giver := map[string]string{"overdue": "librarian", "coyote_trouble": "park_ranger", "fish_fry": "food_truck_cook"}
	for qk, nk := range giver {
		q, _ := d.QuestByKey(qk)
		n, _ := d.NPCByKey(nk)
		dl, _ := d.DialogueFor(n.ID)
		has := func(state gamedata.QuestState, step int) bool {
			for _, node := range dl.Nodes {
				if c := node.When; c != nil && c.Quest == qk && c.State == state && c.Step == step {
					return true
				}
			}
			return false
		}
		if !has(gamedata.QuestNotStarted, 0) || !has(gamedata.QuestComplete, 0) {
			t.Errorf("%s: missing not_started or complete node", qk)
		}
		for i, s := range q.Steps {
			if (s.Kind == gamedata.StepTalk || s.Kind == gamedata.StepBringItems) && !has(gamedata.QuestOnStep, i) {
				t.Errorf("%s: no node for step %d", qk, i)
			}
		}
	}
	g, _ := d.NPCByKey("guide")
	dl, _ := d.DialogueFor(g.ID)
	for _, topic := range []string{"controls", "shop", "bank", "pvp"} {
		if _, ok := dl.Node(topic); !ok {
			t.Errorf("guide has no %q node", topic)
		}
	}
}

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want containing %q", err, want)
	}
}
