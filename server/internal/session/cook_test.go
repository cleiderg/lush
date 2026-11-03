package session_test

import (
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

// AC3: a burnt fish sends Burnt with the burnt item and a cooked fish sends
// Success with the cooked item; both reach the store with audit entries and
// only the cooked one gives Cooking XP.
func TestCookedAndBurntFishResultsAndSave(t *testing.T) {
	c := newCookRig(t)
	c.rnd.vals = []float64{0, 0.99} // first burns, second cooks
	c.give("raw_shrimp")
	c.give("raw_shrimp")
	audit0 := len(c.mem.AuditEntries())
	xp0 := c.p.SkillXP[world.Cooking]

	c.main.send(skillAction(protocol.SkillTargetGrill, 0))
	if r := c.result(c.main); r.code != protocol.SkillResultCodeStarted {
		t.Fatalf("start: %+v", r)
	}
	burnt := c.result(c.main)
	if burnt.code != protocol.SkillResultCodeBurnt || burnt.item != uint16(c.recipe("raw_shrimp").Burnt) {
		t.Fatalf("burnt: %+v", burnt)
	}
	if c.p.SkillXP[world.Cooking] != xp0 {
		t.Fatal("a burnt fish gave XP")
	}
	cooked := c.result(c.main)
	if cooked.code != protocol.SkillResultCodeSuccess || cooked.item != uint16(c.recipe("raw_shrimp").Cooked) {
		t.Fatalf("cooked: %+v", cooked)
	}
	if c.p.SkillXP[world.Cooking] <= xp0 {
		t.Fatal("a cooked fish gave no XP")
	}
	if r := c.result(c.main); r.code != protocol.SkillResultCodeStopped {
		t.Fatalf("out of fish: %+v", r)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(c.mem.AuditEntries()) < audit0+2 {
		if time.Now().After(deadline) {
			t.Fatalf("%d audit entries, want %d", len(c.mem.AuditEntries()), audit0+2)
		}
		time.Sleep(5 * time.Millisecond)
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
