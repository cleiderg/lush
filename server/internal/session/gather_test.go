package session_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/combat/formula"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/gather"
	"github.com/cleiderg/lush/server/internal/world"
)

// gatherRand always succeeds at an attempt; fall decides whether a tree falls.
type gatherRand struct{ fall bool }

func (r *gatherRand) Float64() float64 { return 0 }
func (r *gatherRand) IntN(n int) int {
	if r.fall {
		return 0
	}
	return n - 1
}

type gatherRig struct {
	*rig
	d    *gamedata.Data
	sys  *gather.System
	rnd  *gatherRand
	l    gather.Layout
	mem  *store.Memory
	main *client
	p    *world.Player
}

func newGatherRig(t *testing.T) *gatherRig {
	t.Helper()
	g := &gatherRig{d: testData(t), rnd: &gatherRand{}, mem: store.NewMemory()}
	var err error
	if g.l, err = gather.DefaultLayout(); err != nil {
		t.Fatal(err)
	}
	cfg := session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: g.d}
	g.rig = newRigSystems(t, 1024, cfg, g.mem, nil, func(s *session.Server) []world.System {
		var err error
		if g.sys, err = gather.New(g.d, g.l, g.clockRef(), g.rnd, s, gather.DefaultConfig()); err != nil {
			t.Fatal(err)
		}
		s.SetGather(g.sys, g.l)
		return []world.System{g.sys}
	})
	return g
}

// clockRef reads the rig's fake clock, which exists once the rig does.
func (g *gatherRig) clockRef() world.Clock { return gatherClock{g} }

// gatherClock is the rig's clock, or the rig's start time while it is built.
type gatherClock struct{ g *gatherRig }

func (c gatherClock) Now() time.Time {
	if c.g.rig == nil {
		return time.Unix(0, 0)
	}
	return c.g.clock.Now()
}
func (c gatherClock) NewTicker(time.Duration) world.Ticker { panic("no tickers") }

func (g *gatherRig) join() *client {
	c := g.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	id, _ := g.expectWelcome(c)
	g.w.Tick()
	g.readThrough(c, g.w.TickCount())
	if g.main == nil {
		g.main, g.p = c, g.w.Player(world.PlayerID(id))
	}
	return c
}

func (g *gatherRig) item(key string) gamedata.ItemID {
	it, ok := g.d.ItemByKey(key)
	if !ok {
		g.t.Fatalf("no item %q", key)
	}
	return it.ID
}

func (g *gatherRig) give(key string) {
	if _, err := g.p.Inv.Add(g.d, g.item(key), 1); err != nil {
		g.t.Fatal(err)
	}
}

func (g *gatherRig) at(id string) {
	for _, p := range append(append([]gather.Place{}, g.l.Trees...), g.l.Spots...) {
		if p.ID == id {
			g.p.Pos = world.Vec2{X: float32(int(p.Pos.X)) + 0.5, Y: float32(int(p.Pos.Y)) + 0.5}
			return
		}
	}
	g.t.Fatalf("no place %q", id)
}

func (g *gatherRig) level(s world.Skill, lvl int) {
	g.p.SkillXP[s] = formula.XPForLevel(lvl) * world.SkillXPScale
}

func skillAction(kind protocol.SkillTarget, id uint16) []byte {
	return envelope(protocol.MessageSkillAction, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.SkillActionStart(b)
		protocol.SkillActionAddTarget(b, kind)
		protocol.SkillActionAddTargetId(b, id)
		return protocol.SkillActionEnd(b)
	})
}

type skillResult struct {
	skill protocol.Skill
	code  protocol.SkillResultCode
	item  uint16
	level uint8
}

func (g *gatherRig) result(c *client) skillResult {
	g.t.Helper()
	var r protocol.SkillResult
	tbl := g.waitFor(c, protocol.MessageSkillResult)
	r.Init(tbl.Bytes, tbl.Pos)
	return skillResult{r.Skill(), r.Code(), r.ItemId(), r.Level()}
}

// act sends a SkillAction and returns the SkillResult that answers it.
func (g *gatherRig) act(kind protocol.SkillTarget, id uint16) skillResult {
	g.main.send(skillAction(kind, id))
	return g.result(g.main)
}

func TestDecodeSkillAction(t *testing.T) {
	m, err := session.DecodeForTest(skillAction(protocol.SkillTargetFishingSpot, 3))
	if err != nil {
		t.Fatal(err)
	}
	want := session.SkillActionCommand{Player: 7, Kind: protocol.SkillTargetFishingSpot, ID: 3}
	if got := m.Command(7); got != want {
		t.Fatalf("command %#v, want %#v", got, want)
	}
}

// AC1: SkillAction becomes a StartCommand for the map id P8's lookup returns;
// a number outside the lists is ResourceGone.
func TestSkillActionStartsGatherAtNumberedPlace(t *testing.T) {
	g := newGatherRig(t)
	g.join()
	g.give("bronze_hatchet")
	g.give("small_net")
	for _, tt := range []struct {
		kind protocol.SkillTarget
		n    int
		sk   protocol.Skill
		id   func(int) (string, bool)
	}{
		{protocol.SkillTargetTree, 0, protocol.Skill(world.Woodcutting), g.sys.TreePlace},
		{protocol.SkillTargetTree, 3, protocol.Skill(world.Woodcutting), g.sys.TreePlace},
		{protocol.SkillTargetFishingSpot, 0, protocol.Skill(world.Fishing), g.sys.SpotPlace},
		{protocol.SkillTargetFishingSpot, 1, protocol.Skill(world.Fishing), g.sys.SpotPlace},
	} {
		id, ok := tt.id(tt.n)
		if !ok {
			t.Fatalf("no place %d", tt.n)
		}
		if _, was := g.sys.Gathering(g.p.ID); was {
			g.w.Enqueue(gather.StopCommand{Player: g.p.ID})
			if r := g.result(g.main); r.code != protocol.SkillResultCodeStopped {
				t.Fatalf("stop: %+v", r)
			}
		}
		g.at(id)
		got := g.act(tt.kind, uint16(tt.n))
		if got.code != protocol.SkillResultCodeStarted || got.skill != tt.sk {
			t.Fatalf("%s: result %+v", id, got)
		}
		if at, ok := g.sys.Gathering(g.p.ID); !ok || at != id {
			t.Fatalf("gathering at %q (%v), want %q", at, ok, id)
		}
	}
	for _, tt := range []struct {
		kind protocol.SkillTarget
		n    uint16
		sk   world.Skill
	}{
		{protocol.SkillTargetTree, uint16(len(g.l.Trees)), world.Woodcutting},
		{protocol.SkillTargetTree, 65535, world.Woodcutting},
		{protocol.SkillTargetFishingSpot, 4, world.Fishing},
	} {
		got := g.act(tt.kind, tt.n)
		if got.code != protocol.SkillResultCodeResourceGone || got.skill != protocol.Skill(tt.sk) {
			t.Fatalf("target %d/%d: result %+v", tt.kind, tt.n, got)
		}
	}
}

func catalogOf(t *testing.T, tbl flatbuffers.Table) (trees map[string]uint8, spots map[string][2]int) {
	var c protocol.SkillCatalog
	c.Init(tbl.Bytes, tbl.Pos)
	trees, spots = map[string]uint8{}, map[string][2]int{}
	for i := 0; i < c.TreesLength(); i++ {
		var d protocol.TreeKindDef
		c.Trees(&d, i)
		trees[string(d.Kind())] = d.Level()
	}
	for i := 0; i < c.FishingSpotsLength(); i++ {
		var d protocol.FishingSpotKindDef
		c.FishingSpots(&d, i)
		spots[string(d.Kind())] = [2]int{int(d.Level()), int(d.ToolItemId())}
	}
	return
}

func resourceOf(tbl flatbuffers.Table) (felled []uint16, spots [][2]int) {
	var r protocol.ResourceState
	r.Init(tbl.Bytes, tbl.Pos)
	for i := 0; i < r.FelledTreesLength(); i++ {
		felled = append(felled, r.FelledTrees(i))
	}
	for i := 0; i < r.FishingSpotsLength(); i++ {
		var s protocol.FishingSpotState
		r.FishingSpots(&s, i)
		spots = append(spots, [2]int{int(s.Id()), int(s.Candidate())})
	}
	return
}

// AC2: a joining player gets SkillCatalog and ResourceState; ResourceState is
// sent again to everyone when a tree falls or regrows.
func TestJoinSendsSkillCatalogAndResourceState(t *testing.T) {
	g := newGatherRig(t)
	c := g.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	g.expectWelcome(c)
	g.w.Tick()
	ms := g.readThrough(c, g.w.TickCount())
	cat := ofType(ms, protocol.MessageSkillCatalog)
	res := ofType(ms, protocol.MessageResourceState)
	if len(cat) != 1 || len(res) != 1 {
		t.Fatalf("join sent %d SkillCatalog and %d ResourceState", len(cat), len(res))
	}
	trees, spots := catalogOf(t, cat[0])
	for _, tr := range g.d.Skills.Trees {
		if trees[tr.Key] != uint8(tr.Level) {
			t.Errorf("tree %s level %d, want %d", tr.Key, trees[tr.Key], tr.Level)
		}
	}
	if len(trees) != len(g.d.Skills.Trees) || len(spots) != len(g.d.Skills.FishingSpots) {
		t.Errorf("catalog has %d trees, %d spots", len(trees), len(spots))
	}
	for _, f := range g.d.Skills.FishingSpots {
		kind := f.Key[:len(f.Key)-len("_spot")]
		if want := [2]int{f.Level, int(f.Tool)}; spots[kind] != want || f.Tool == 0 {
			t.Errorf("spot %s = %v, want %v", kind, spots[kind], want)
		}
	}
	felled, fs := resourceOf(res[0])
	state := g.sys.Resources()
	if len(felled) != 0 || len(fs) != len(state.Spots) || len(fs) == 0 {
		t.Fatalf("ResourceState felled %v spots %v", felled, fs)
	}
	for i, s := range state.Spots {
		if fs[i] != [2]int{s.Slot, s.Candidate} {
			t.Errorf("spot %d = %v, want %+v", i, fs[i], s)
		}
	}
}

func TestResourceStateResentWhenTreeFallsAndRegrows(t *testing.T) {
	g := newGatherRig(t)
	g.join()
	other := g.join()
	g.give("bronze_hatchet")
	g.at("tree-1")
	g.rnd.fall = true
	if r := g.act(protocol.SkillTargetTree, 0); r.code != protocol.SkillResultCodeStarted {
		t.Fatalf("start: %+v", r)
	}
	// The attempt fells the tree: Success, then ResourceGone, and both
	// players get ResourceState with tree 0 felled.
	var got []skillResult
	for len(got) < 2 {
		got = append(got, g.result(g.main))
	}
	if got[0].code != protocol.SkillResultCodeSuccess || got[1].code != protocol.SkillResultCodeResourceGone {
		t.Fatalf("results %+v", got)
	}
	for _, c := range []*client{g.main, other} {
		tbl := g.waitFor(c, protocol.MessageResourceState)
		if felled, _ := resourceOf(tbl); len(felled) != 1 || felled[0] != 0 {
			t.Fatalf("felled %v, want [0]", felled)
		}
	}
	g.clock.Advance(time.Minute)
	for _, c := range []*client{g.main, other} {
		tbl := g.waitFor(c, protocol.MessageResourceState)
		if felled, _ := resourceOf(tbl); len(felled) != 0 {
			t.Fatalf("felled %v after regrow", felled)
		}
	}
}

// AC3: start results.
func TestSkillResultCodesForStart(t *testing.T) {
	d := testData(t)
	hatchet := uint16(0)
	for _, it := range d.Items {
		if it.ToolSkill == gamedata.SkillWoodcutting && (hatchet == 0 || it.ToolLevel < 1) {
			hatchet = uint16(it.ID)
		}
	}
	oak := 0
	for _, tr := range d.Skills.Trees {
		if tr.Key == "oak" {
			oak = tr.Level
		}
	}
	tests := []struct {
		name  string
		setup func(g *gatherRig)
		kind  protocol.SkillTarget
		n     uint16
		want  skillResult
	}{
		{"started", func(g *gatherRig) { g.give("bronze_hatchet"); g.at("tree-1") }, protocol.SkillTargetTree, 0,
			skillResult{skill: protocol.Skill(world.Woodcutting), code: protocol.SkillResultCodeStarted}},
		{"no hatchet", func(g *gatherRig) { g.at("tree-1") }, protocol.SkillTargetTree, 0,
			skillResult{skill: protocol.Skill(world.Woodcutting), code: protocol.SkillResultCodeNeedTool, item: hatchet}},
		{"level", func(g *gatherRig) { g.give("bronze_hatchet"); g.at("oak-1") }, protocol.SkillTargetTree, 12,
			skillResult{skill: protocol.Skill(world.Woodcutting), code: protocol.SkillResultCodeNeedLevel, level: uint8(oak)}},
		{"too far", func(g *gatherRig) { g.give("bronze_hatchet"); g.p.Pos = world.Vec2{X: 500, Y: 500} }, protocol.SkillTargetTree, 0,
			skillResult{skill: protocol.Skill(world.Woodcutting), code: protocol.SkillResultCodeOutOfRange}},
		{"inventory full", func(g *gatherRig) {
			g.give("bronze_hatchet")
			g.at("tree-1")
			for i := range g.p.Inv.Slots {
				if g.p.Inv.Slots[i].Empty() {
					g.p.Inv.Slots[i] = world.Stack{Item: g.item("bronze_hatchet"), Qty: 1}
				}
			}
		}, protocol.SkillTargetTree, 0, skillResult{skill: protocol.Skill(world.Woodcutting), code: protocol.SkillResultCodeInventoryFull}},
		{"in combat", func(g *gatherRig) { g.give("bronze_hatchet"); g.at("tree-1"); g.p.LastCombat = g.clock.Now() },
			protocol.SkillTargetTree, 0, skillResult{skill: protocol.Skill(world.Woodcutting), code: protocol.SkillResultCodeStopped}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGatherRig(t)
			g.join()
			tt.setup(g)
			if got := g.act(tt.kind, tt.n); got != tt.want {
				t.Fatalf("result %+v, want %+v", got, tt.want)
			}
		})
	}
}

// AC3: needing a fishing tool names the spot kind's tool.
func TestSkillResultNeedToolForFishing(t *testing.T) {
	g := newGatherRig(t)
	g.join()
	g.level(world.Fishing, 10)
	var num uint16
	var kind string
	for n := 0; ; n++ {
		id, ok := g.sys.SpotPlace(n)
		if !ok {
			t.Fatal("no rod spot")
		}
		if id[:3] == "rod" {
			num, kind = uint16(n), "rod_spot"
			g.at(id)
			break
		}
	}
	var tool gamedata.ItemID
	for _, f := range g.d.Skills.FishingSpots {
		if f.Key == kind {
			tool = f.Tool
		}
	}
	g.give("small_net") // the wrong tool
	want := skillResult{skill: protocol.Skill(world.Fishing), code: protocol.SkillResultCodeNeedTool, item: uint16(tool)}
	if got := g.act(protocol.SkillTargetFishingSpot, num); got != want {
		t.Fatalf("result %+v, want %+v", got, want)
	}
}

// AC3: stop reasons.
func TestSkillResultCodesForStops(t *testing.T) {
	t.Run("moved", func(t *testing.T) {
		g := newGatherRig(t)
		g.join()
		g.give("bronze_hatchet")
		g.at("tree-1")
		if r := g.act(protocol.SkillTargetTree, 0); r.code != protocol.SkillResultCodeStarted {
			t.Fatal(r)
		}
		g.main.send(input(1, 1, 0))
		for {
			r := g.result(g.main)
			if r.code == protocol.SkillResultCodeSuccess {
				continue
			}
			if r.code != protocol.SkillResultCodeStopped || r.skill != protocol.Skill(world.Woodcutting) {
				t.Fatalf("result %+v", r)
			}
			return
		}
	})
	t.Run("full", func(t *testing.T) {
		g := newGatherRig(t)
		g.join()
		g.give("bronze_hatchet")
		g.at("tree-1")
		for i := range g.p.Inv.Slots[1:] {
			if i < len(g.p.Inv.Slots)-3 && g.p.Inv.Slots[i+1].Empty() {
				g.p.Inv.Slots[i+1] = world.Stack{Item: g.item("bronze_hatchet"), Qty: 1}
			}
		}
		if r := g.act(protocol.SkillTargetTree, 0); r.code != protocol.SkillResultCodeStarted {
			t.Fatal(r)
		}
		var codes []protocol.SkillResultCode
		for len(codes) == 0 || codes[len(codes)-1] != protocol.SkillResultCodeInventoryFull {
			codes = append(codes, g.result(g.main).code)
			if len(codes) > 10 {
				t.Fatalf("codes %v", codes)
			}
		}
	})
	t.Run("hatchet gone", func(t *testing.T) {
		g := newGatherRig(t)
		g.join()
		g.give("bronze_hatchet")
		g.at("tree-1")
		if r := g.act(protocol.SkillTargetTree, 0); r.code != protocol.SkillResultCodeStarted {
			t.Fatal(r)
		}
		for i := range g.p.Inv.Slots {
			if g.p.Inv.Slots[i].Item == g.item("bronze_hatchet") {
				g.p.Inv.Slots[i] = world.Stack{}
			}
		}
		for {
			r := g.result(g.main)
			if r.code == protocol.SkillResultCodeSuccess {
				continue
			}
			if r.code != protocol.SkillResultCodeNeedTool || r.item == 0 {
				t.Fatalf("result %+v", r)
			}
			return
		}
	})
	t.Run("player stop", func(t *testing.T) {
		g := newGatherRig(t)
		g.join()
		g.give("bronze_hatchet")
		g.at("tree-1")
		g.act(protocol.SkillTargetTree, 0)
		g.w.Enqueue(gather.StopCommand{Player: g.p.ID})
		for {
			r := g.result(g.main)
			if r.code != protocol.SkillResultCodeSuccess {
				if r.code != protocol.SkillResultCodeStopped {
					t.Fatalf("result %+v", r)
				}
				return
			}
		}
	})
}

// AC3: every gather result and stop reason has a code.
func TestSkillCodeTables(t *testing.T) {
	session.CheckSkillCodes(t)
}

// AC4: a success sends SkillResult Success with the item, saves it through
// the economy outbox with an audit entry, and the XP reaches SkillStats.
func TestGatherSuccessIsSavedAndAudited(t *testing.T) {
	g := newGatherRig(t)
	g.join()
	g.give("bronze_hatchet")
	g.at("tree-1")
	if r := g.act(protocol.SkillTargetTree, 0); r.code != protocol.SkillResultCodeStarted {
		t.Fatal(r)
	}
	r := g.result(g.main)
	logs := g.item("logs")
	if r.code != protocol.SkillResultCodeSuccess || r.item != uint16(logs) || r.skill != protocol.Skill(world.Woodcutting) {
		t.Fatalf("result %+v", r)
	}
	stats := decodeSkillStats(g.waitFor(g.main, protocol.MessageSkillStats))
	if stats.wcXP == 0 {
		t.Fatalf("SkillStats %+v has no woodcutting XP", stats)
	}
	if err := g.sess.SaveAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range g.mem.AuditEntries() {
		b, _ := json.Marshal(e.Payload)
		var p struct {
			Reason string
			Item   int
		}
		if err := json.Unmarshal(b, &p); err == nil && p.Reason == gather.ReasonGather && p.Item == int(logs) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no audit entry for the logs: %+v", g.mem.AuditEntries())
	}
}
