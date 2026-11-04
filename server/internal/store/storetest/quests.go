package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/store"
)

// QuestStore is a store with quests.
type QuestStore interface {
	store.Players
	store.Quests
}

// RunQuests runs the Quests contract against fresh stores from factory, which
// also returns a function listing the store's audit entries in append order
// (Kind and Actor only).
func RunQuests(t *testing.T, factory func(t *testing.T) (QuestStore, func() []store.Entry)) {
	ctx := context.Background()
	newPlayer := func(t *testing.T, s QuestStore, h store.Holdings) store.Player {
		t.Helper()
		p, err := s.CreateGuest(ctx, "tok", h, nil)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("round trip", func(t *testing.T) {
		s, _ := factory(t)
		p := newPlayer(t, s, store.Holdings{})
		log, err := s.LoadQuests(ctx, p.ID)
		if err != nil || log.Points != 0 || len(log.Quests) != 0 {
			t.Fatalf("fresh log = %+v, err %v", log, err)
		}
		want := []store.QuestProgress{
			{Quest: "coyote", State: store.QuestInProgress, Step: 1, Count: 3},
			{Quest: "fish", State: store.QuestNotStarted},
			{Quest: "overdue", State: store.QuestInProgress, Step: 2},
		}
		if err := s.SaveQuests(ctx, p.ID, want); err != nil {
			t.Fatal(err)
		}
		// Change one and save again.
		want[0].Count = 4
		if err := s.SaveQuests(ctx, p.ID, want[:1]); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CompleteQuest(ctx, store.QuestCompletion{PlayerID: p.ID, Quest: "overdue", Points: 1}); err != nil {
			t.Fatal(err)
		}
		want[2] = store.QuestProgress{Quest: "overdue", State: store.QuestComplete}
		log, err = s.LoadQuests(ctx, p.ID)
		if err != nil || log.Points != 1 || !reflect.DeepEqual(log.Quests, want) {
			t.Fatalf("log = %+v, err %v; want points 1, quests %+v", log, err, want)
		}
		// A stale save can't undo a completion.
		if err := s.SaveQuests(ctx, p.ID, []store.QuestProgress{{Quest: "overdue", State: store.QuestInProgress, Step: 2}}); err != nil {
			t.Fatal(err)
		}
		log, _ = s.LoadQuests(ctx, p.ID)
		if log.Quests[2].State != store.QuestComplete {
			t.Fatalf("completed quest reverted: %+v", log.Quests[2])
		}
	})

	t.Run("save rejects", func(t *testing.T) {
		s, _ := factory(t)
		p := newPlayer(t, s, store.Holdings{})
		ok := store.QuestProgress{Quest: "a", State: store.QuestInProgress}
		for name, bad := range map[string]store.QuestProgress{
			"complete": {Quest: "b", State: store.QuestComplete},
			"no id":    {State: store.QuestInProgress},
			"negative": {Quest: "b", State: store.QuestInProgress, Step: -1},
		} {
			if err := s.SaveQuests(ctx, p.ID, []store.QuestProgress{ok, bad}); !errors.Is(err, store.ErrInvalid) {
				t.Errorf("%s: err = %v, want ErrInvalid", name, err)
			}
		}
		if log, _ := s.LoadQuests(ctx, p.ID); len(log.Quests) != 0 {
			t.Errorf("rejected saves left %+v", log.Quests)
		}
		if err := s.SaveQuests(ctx, p.ID+99, []store.QuestProgress{ok}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("unknown player: err = %v, want ErrNotFound", err)
		}
		if _, err := s.LoadQuests(ctx, p.ID+99); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("load unknown player: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("complete", func(t *testing.T) {
		s, audit := factory(t)
		var h store.Holdings
		h.Gold = 10
		h.Inventory[0] = store.Stack{Item: 50, Qty: 1} // the quest item
		h.Inventory[1] = store.Stack{Item: 9, Qty: 5}  // coins-like stack
		h.Equipment[store.EquipWeapon] = 7
		p := newPlayer(t, s, h)
		if err := s.SaveQuests(ctx, p.ID, []store.QuestProgress{{Quest: "q", State: store.QuestInProgress, Step: 2}}); err != nil {
			t.Fatal(err)
		}
		res, err := s.CompleteQuest(ctx, store.QuestCompletion{
			PlayerID: p.ID, Quest: "q", Points: 2, Gold: 150, XP: 5000,
			Skills: store.SkillXP{Cooking: 15000, Fishing: 10000},
			Items:  []store.QuestItem{{Item: 60, Qty: 1}, {Item: 9, Qty: 2, Stackable: true}},
			Remove: []store.QuestItem{{Item: 50, Qty: 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var want store.Holdings
		want.Gold = 160
		want.Inventory[0] = store.Stack{Item: 60, Qty: 1}
		want.Inventory[1] = store.Stack{Item: 9, Qty: 7}
		want.Equipment[store.EquipWeapon] = 7
		if res.Points != 2 || res.XP != 5000 || res.Skills != (store.SkillXP{Cooking: 15000, Fishing: 10000}) || res.Holdings != want {
			t.Fatalf("result = %+v", res)
		}
		got, err := s.LoadByTokenHash(ctx, "tok")
		if err != nil {
			t.Fatal(err)
		}
		if got.Holdings != want || got.XP != 5000 || got.Skills != res.Skills || got.Version != p.Version {
			t.Fatalf("stored player = %+v", got)
		}
		log, _ := s.LoadQuests(ctx, p.ID)
		if log.Points != 2 || !reflect.DeepEqual(log.Quests, []store.QuestProgress{{Quest: "q", State: store.QuestComplete}}) {
			t.Fatalf("log = %+v", log)
		}
		var kinds []string
		for _, e := range audit() {
			if e.Actor != p.ID {
				t.Errorf("audit actor = %d, want %d", e.Actor, p.ID)
			}
			kinds = append(kinds, e.Kind)
		}
		wantKinds := []string{store.QuestItemRemoveKind, store.QuestGoldKind, store.QuestItemKind, store.QuestItemKind}
		if !reflect.DeepEqual(kinds, wantKinds) {
			t.Fatalf("audit kinds = %v, want %v", kinds, wantKinds)
		}
	})

	t.Run("writes nothing", func(t *testing.T) {
		s, audit := factory(t)
		var full store.Holdings
		for i := range full.Inventory {
			full.Inventory[i] = store.Stack{Item: int32(100 + i), Qty: 1}
		}
		full.Gold = 3
		p := newPlayer(t, s, full)
		before := len(audit())
		check := func(name string, c store.QuestCompletion, want error) {
			t.Helper()
			c.PlayerID, c.Quest = p.ID, "q"
			c.Points, c.XP = 1, 100
			if c.Gold == 0 {
				c.Gold = 100
			}
			if _, err := s.CompleteQuest(ctx, c); !errors.Is(err, want) {
				t.Fatalf("%s: err = %v, want %v", name, err, want)
			}
			got, _ := s.LoadByTokenHash(ctx, "tok")
			log, _ := s.LoadQuests(ctx, p.ID)
			if got.Holdings != full || got.XP != 0 || log.Points != 0 || len(audit()) != before {
				t.Fatalf("%s wrote something: %+v %+v, %d audit entries", name, got, log, len(audit())-before)
			}
		}
		// Removing item 100 frees one slot, but two single items need two.
		check("no room", store.QuestCompletion{
			Items:  []store.QuestItem{{Item: 1, Qty: 1}, {Item: 2, Qty: 1}},
			Remove: []store.QuestItem{{Item: 100, Qty: 1}},
		}, store.ErrNoRoom)
		check("invalid", store.QuestCompletion{Gold: -1}, store.ErrInvalid)

	})

	t.Run("already complete writes nothing", func(t *testing.T) {
		s, audit := factory(t)
		p := newPlayer(t, s, store.Holdings{})
		c := store.QuestCompletion{PlayerID: p.ID, Quest: "q", Points: 1, Gold: 5, XP: 10,
			Items: []store.QuestItem{{Item: 3, Qty: 1}}}
		if _, err := s.CompleteQuest(ctx, c); err != nil {
			t.Fatal(err)
		}
		before, _ := s.LoadByTokenHash(ctx, "tok")
		entries := len(audit())
		if _, err := s.CompleteQuest(ctx, c); !errors.Is(err, store.ErrQuestComplete) {
			t.Fatalf("err = %v, want ErrQuestComplete", err)
		}
		after, _ := s.LoadByTokenHash(ctx, "tok")
		log, _ := s.LoadQuests(ctx, p.ID)
		if after != before || log.Points != 1 || len(audit()) != entries {
			t.Fatalf("second completion wrote something: %+v, points %d, %d audit entries", after, log.Points, len(audit())-entries)
		}
	})
}
