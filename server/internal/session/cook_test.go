package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/cook"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/gather"
	"github.com/cleiderg/lush/server/internal/world"
)

// scriptRand returns the scripted values in order, then 0.99 (never burns).
type scriptRand struct{ vals []float64 }

func (r *scriptRand) Float64() float64 {
	if len(r.vals) == 0 {
		return 0.99
	}
	v := r.vals[0]
	r.vals = r.vals[1:]
	return v
}
func (r *scriptRand) IntN(n int) int { return n - 1 }

type cookRig struct {
	*gatherRig
	cook *cook.System
	rnd  *scriptRand
}

func newCookRig(t *testing.T) *cookRig {
	t.Helper()
	c := &cookRig{rnd: &scriptRand{}}
	c.gatherRig = &gatherRig{d: testData(t), rnd: &gatherRand{}, mem: store.NewMemory()}
	var err error
	if c.l, err = gather.DefaultLayout(); err != nil {
		t.Fatal(err)
	}
	cfg := session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: c.d}
	c.rig = newRigSystems(t, 1024, cfg, c.mem, nil, func(s *session.Server) []world.System {
		var err error
		if c.sys, err = gather.New(c.d, c.l, c.clockRef(), c.gatherRig.rnd, s, gather.DefaultConfig()); err != nil {
			t.Fatal(err)
		}
		s.SetGather(c.sys, c.l)
		if c.cook, err = cook.New(c.d, c.l, c.clockRef(), c.rnd, s.CookSink(), cook.DefaultConfig()); err != nil {
			t.Fatal(err)
		}
		c.cook.LinkGather(c.sys)
		s.SetCook(c.cook)
		return []world.System{c.sys, c.cook}
	})
	c.join()
	c.p.Pos = c.l.Grill
	return c
}

// AC1, AC2: SkillAction Grill starts cooking and every start result and stop
// is a Cooking SkillResult with the matching code.
func TestSkillActionGrillStartAndStopResults(t *testing.T) {
	c := newCookRig(t)
	cooking := protocol.Skill(world.Cooking)
	expect := func(what string, code protocol.SkillResultCode, level uint8) {
		t.Helper()
		if r := c.result(c.main); r.skill != cooking || r.code != code || r.level != level {
			t.Fatalf("%s: %+v, want code %v level %d", what, r, code, level)
		}
	}

	c.main.send(skillAction(protocol.SkillTargetGrill, 9)) // target_id ignored
	expect("nothing to cook", protocol.SkillResultCodeNothingToCook, 0)

	c.give("raw_trout")
	c.main.send(skillAction(protocol.SkillTargetGrill, 0))
	expect("level too low", protocol.SkillResultCodeNeedLevel, uint8(c.recipe("raw_trout").Level))

	c.give("raw_shrimp")
	c.p.Pos = world.Vec2{X: c.l.Grill.X + 10, Y: c.l.Grill.Y}
	c.main.send(skillAction(protocol.SkillTargetGrill, 0))
	expect("out of range", protocol.SkillResultCodeOutOfRange, 0)

	c.p.Pos = c.l.Grill
	c.main.send(skillAction(protocol.SkillTargetGrill, 0))
	expect("started", protocol.SkillResultCodeStarted, 0)
	if !c.cook.Cooking(c.p.ID) {
		t.Fatal("not cooking after Started")
	}
	c.main.send(skillAction(protocol.SkillTargetGrill, 0)) // restart stops, then starts
	c.w.Enqueue(cook.StopCommand{Player: c.p.ID})
	for i := 0; i < 3; i++ {
		c.result(c.main)
	}
	if c.cook.Cooking(c.p.ID) {
		t.Fatal("still cooking after stop")
	}
}

// AC2: starting in combat, moving and combat starting are each a Cooking
// SkillResult Stopped (running out of raw fish is in the AC3 test).
func TestSkillActionGrillCombatAndMoveStop(t *testing.T) {
	cooking := protocol.Skill(world.Cooking)
	// stopped reads results until the first that is not a cooked or burnt fish.
	stopped := func(t *testing.T, c *cookRig) {
		t.Helper()
		for {
			r := c.result(c.main)
			if r.code == protocol.SkillResultCodeSuccess || r.code == protocol.SkillResultCodeBurnt {
				continue
			}
			if r.skill != cooking || r.code != protocol.SkillResultCodeStopped {
				t.Fatalf("result %+v, want Cooking Stopped", r)
			}
			if c.cook.Cooking(c.p.ID) {
				t.Fatal("still cooking after Stopped")
			}
			return
		}
	}
	start := func(t *testing.T) *cookRig {
		t.Helper()
		c := newCookRig(t)
		for i := 0; i < 5; i++ {
			c.give("raw_shrimp")
		}
		c.main.send(skillAction(protocol.SkillTargetGrill, 0))
		if r := c.result(c.main); r.skill != cooking || r.code != protocol.SkillResultCodeStarted {
			t.Fatalf("start: %+v", r)
		}
		return c
	}
	t.Run("start in combat", func(t *testing.T) {
		c := newCookRig(t)
		c.give("raw_shrimp")
		c.p.LastCombat = c.clock.Now()
		c.main.send(skillAction(protocol.SkillTargetGrill, 0))
		stopped(t, c)
	})
	t.Run("moved", func(t *testing.T) {
		c := start(t)
		c.main.send(input(1, 1, 0))
		stopped(t, c)
	})
	t.Run("combat started", func(t *testing.T) {
		c := start(t)
		c.p.LastCombat = c.clock.Now().Add(time.Second)
		stopped(t, c)
	})
}

// AC3: a burnt fish sends Burnt with the burnt item and a cooked fish sends
// Success with the cooked item; both reach the store through the economy
// outbox with audit entries, and only the cooked one raises Cooking XP in
// SkillStats.
func TestCookedAndBurntFishResultsAndSave(t *testing.T) {
	c := newCookRig(t)
	c.rnd.vals = []float64{0, 0.99} // first burns, second cooks
	c.give("raw_shrimp")
	c.give("raw_shrimp")
	shrimp := c.recipe("raw_shrimp")

	// Read SkillResults and SkillStats in order; xpAt[i] is the Cooking XP the
	// client was last told when the i-th SkillResult arrived.
	var results []skillResult
	var xpAt []uint32
	var xp uint32
	c.main.send(skillAction(protocol.SkillTargetGrill, 0))
	for i := 0; len(results) < 4; i++ {
		if i > 5000 {
			t.Fatalf("results %+v", results)
		}
		typ, tbl := c.next(c.main)
		switch typ {
		case protocol.MessageSkillStats:
			xp = decodeSkillStats(tbl).cXP
		case protocol.MessageSkillResult:
			var r protocol.SkillResult
			r.Init(tbl.Bytes, tbl.Pos)
			results = append(results, skillResult{r.Skill(), r.Code(), r.ItemId(), r.Level()})
			xpAt = append(xpAt, xp)
		}
	}
	// SkillStats for a tick follows that tick's results: read on to the next one.
	xp = decodeSkillStats(c.waitFor(c.main, protocol.MessageSkillStats)).cXP

	cooking := protocol.Skill(world.Cooking)
	want := []skillResult{
		{skill: cooking, code: protocol.SkillResultCodeStarted},
		{skill: cooking, code: protocol.SkillResultCodeBurnt, item: uint16(shrimp.Burnt)},
		{skill: cooking, code: protocol.SkillResultCodeSuccess, item: uint16(shrimp.Cooked)},
		{skill: cooking, code: protocol.SkillResultCodeStopped},
	}
	for i := range want {
		if results[i] != want[i] {
			t.Fatalf("result %d = %+v, want %+v", i, results[i], want[i])
		}
	}
	if xpAt[2] != 0 {
		t.Fatalf("SkillStats Cooking XP %d before the cooked fish: the burnt one gave XP", xpAt[2])
	}
	if wantXP := uint32(math.Round(shrimp.XP * world.SkillXPScale)); xp != wantXP {
		t.Fatalf("SkillStats Cooking XP %d after the cooked fish, want %d", xp, wantXP)
	}

	if err := c.sess.SaveAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, e := range c.mem.AuditEntries() {
		b, _ := json.Marshal(e.Payload)
		var p struct {
			Reason string
			Item   int
		}
		if err := json.Unmarshal(b, &p); err == nil && p.Reason == cook.ReasonCook {
			count[fmt.Sprintf("%s %d", e.Kind, p.Item)]++
		}
	}
	wantAudit := map[string]int{
		fmt.Sprintf("item_remove %d", shrimp.Raw): 2,
		fmt.Sprintf("item_add %d", shrimp.Burnt):  1,
		fmt.Sprintf("item_add %d", shrimp.Cooked): 1,
	}
	if !maps.Equal(count, wantAudit) {
		t.Fatalf("cook audit entries %v, want %v", count, wantAudit)
	}
}

func (c *cookRig) recipe(raw string) gamedata.Recipe {
	for _, r := range c.d.Skills.Cooking {
		if r.Raw == c.item(raw) {
			return r
		}
	}
	c.t.Fatalf("no recipe for %s", raw)
	return gamedata.Recipe{}
}
