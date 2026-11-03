package main

import (
	"encoding/json"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/tools/bots/internal/protocol"
)

func testMap() *gatherMap {
	return &gatherMap{
		trees: []place{{"tree", 40, 0}, {"tree", 10, 0}, {"oak", -5, 0}},
		spots: []place{{"net", 0, 30}, {"rod", 0, 5}, {"net", 0, 20}},
		shop:  [2]float64{0, 0},
	}
}

// walk walks a gatherer at 4 m/s on a fake clock, buying from a fake shop,
// and returns the position and the actions it sent.
func walk(g *gatherer, res resources, secs int) (x, z float64, bought []uint16, acts []skillAction) {
	inv := inventory{known: true, weapon: 1, slots: []uint16{0, 0}}
	now := time.Unix(1000, 0)
	for i := 0; i < secs*10; i++ {
		now = now.Add(inputEvery)
		s := g.step(now, x, z, inv, res)
		x += s.moveX * 0.4
		z += s.moveZ * 0.4
		if s.buy != 0 {
			bought = append(bought, s.buy)
			inv.slots[0] = s.buy
		}
		if s.act != nil {
			acts = append(acts, *s.act)
		}
	}
	return
}

func TestGathererChops(t *testing.T) {
	res := resources{known: true, felled: map[uint16]bool{1: true}}
	// tree 1 is felled and 2 is an oak (level 15), so the one to chop is 0
	g := newGatherer(testMap(), false)
	x, z, bought, acts := walk(g, res, 0)
	_ = x
	_ = z
	if len(bought) != 0 || len(acts) != 0 {
		t.Fatal("nothing happens in zero time")
	}
	inv := inventory{known: true, slots: []uint16{0, 0}}
	// away from the shop: walks toward it, buys nothing yet
	if s := g.step(time.Unix(1000, 0), 30, 0, inv, res); s.buy != 0 || s.moveX >= 0 {
		t.Errorf("far from shop: %+v", s)
	}
	// at the shop: buys a bronze hatchet
	if s := g.step(time.Unix(1001, 0), 0.5, 0, inv, res); s.buy != bronzeHatchetID {
		t.Errorf("at shop: %+v", s)
	}
	g = newGatherer(testMap(), false)
	x, z, bought, acts = walk(g, res, 30)
	if len(bought) != 1 || bought[0] != bronzeHatchetID {
		t.Fatalf("bought %v", bought)
	}
	if len(acts) == 0 {
		t.Fatal("never chopped")
	}
	for _, a := range acts {
		if a.target != protocol.SkillTargetTree || a.id != 0 {
			t.Errorf("action %+v, want tree 0", a)
		}
	}
	if !inReach(x, z, place{"tree", 40, 0}) {
		t.Errorf("acted at %.2f, %.2f, not within 1 tile of the tree", x, z)
	}
}

func TestGathererFishes(t *testing.T) {
	// spot 0 is a rod spot (candidate 1), spot 1 a far net (candidate 0) and
	// spot 2 a near net (candidate 2): the near net is the one to pick.
	res := resources{known: true, spots: []spotState{{0, 1}, {1, 0}, {2, 2}}}
	g := newGatherer(testMap(), true)
	x, z, bought, acts := walk(g, res, 30)
	if len(bought) != 1 || bought[0] != smallNetID {
		t.Fatalf("bought %v", bought)
	}
	if len(acts) == 0 {
		t.Fatal("never fished")
	}
	for _, a := range acts {
		if a.target != protocol.SkillTargetFishingSpot || a.id != 2 {
			t.Errorf("action %+v, want fishing spot 2", a)
		}
	}
	if !inReach(x, z, place{"net", 0, 20}) {
		t.Errorf("acted at %.2f, %.2f, not within 1 tile of the spot", x, z)
	}
}

func TestGathererWaitsAndMoves(t *testing.T) {
	g := newGatherer(testMap(), false)
	inv := inventory{known: true, weapon: bronzeHatchetID}
	now := time.Unix(1000, 0)
	if s := g.step(now, 0, 0, inv, resources{}); s != (gatherStep{}) {
		t.Errorf("without ResourceState: %+v", s)
	}
	res := resources{known: true, felled: map[uint16]bool{}}
	if s := g.step(now, 5, 0, inv, res); s.act != nil || s.moveX <= 0 {
		t.Errorf("should head to the tree at x=10: %+v", s)
	}
	// the chosen tree is felled: moves on to the next plain one, past the
	// nearer oak at x=-5
	res.felled[1] = true
	if s := g.step(now.Add(time.Second), 5, 0, inv, res); s.act != nil || s.moveX <= 0 || g.target != 0 {
		t.Errorf("tree felled, next is at x=40: %+v", s)
	}
	// a full inventory ends it
	g.noteResult(protocol.SkillResultCodeInventoryFull)
	if s := g.step(now, 39.5, 0, inv, res); s != (gatherStep{}) {
		t.Errorf("inventory full: %+v", s)
	}
}

// TestGathererTileReach: the server's range is in tiles, so a spot off its
// tile centre can be 1.4 m away yet 2 tiles off. The bot keeps walking there,
// and an OutOfRange answer doesn't make it resend every input.
func TestGathererTileReach(t *testing.T) {
	m := testMap()
	m.spots = []place{{"net", -82.9, -17.7}}
	g := newGatherer(m, true)
	inv := inventory{known: true, slots: []uint16{smallNetID}}
	res := resources{known: true, spots: []spotState{{0, 0}}}
	now := time.Unix(1000, 0)
	if s := g.step(now, -84.3, -17.7, inv, res); s.act != nil || s.moveX <= 0 {
		t.Errorf("1.4 m but 2 tiles away: %+v", s)
	}
	s := g.step(now, -83.5, -17.7, inv, res)
	if s.act == nil || s.act.id != 0 {
		t.Fatalf("1 tile away: %+v", s)
	}
	g.noteResult(protocol.SkillResultCodeOutOfRange)
	if s := g.step(now.Add(inputEvery), -83.5, -17.7, inv, res); s.act != nil {
		t.Errorf("resent at once after OutOfRange: %+v", s)
	}
	now = now.Add(retryEvery)
	if s := g.step(now, -83.5, -17.7, inv, res); s.act == nil {
		t.Errorf("no retry after %v: %+v", retryEvery, s)
	}
	// once gathering, it stands still and sends nothing more until stopped
	g.noteResult(protocol.SkillResultCodeStarted)
	g.noteResult(protocol.SkillResultCodeSuccess)
	if s := g.step(now.Add(2*retryEvery), -83.5, -17.7, inv, res); s != (gatherStep{}) {
		t.Errorf("resent while gathering: %+v", s)
	}
	g.noteResult(protocol.SkillResultCodeStopped)
	if s := g.step(now.Add(2*retryEvery), -83.5, -17.7, inv, res); s.act == nil {
		t.Errorf("no SkillAction after Stopped: %+v", s)
	}
}

func TestIsGatherer(t *testing.T) {
	for _, c := range []struct {
		frac float64
		want int
	}{{0, 0}, {0.2, 20}, {0.5, 50}, {1, 100}} {
		n := 0
		for i := 0; i < 100; i++ {
			if isGatherer(i, c.frac) {
				n++
			}
		}
		if n != c.want {
			t.Errorf("frac %v: %d gatherers of 100, want %d", c.frac, n, c.want)
		}
	}
}

func TestGatherMessages(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	m, ok := decode(encodeSkillAction(b, skillAction{protocol.SkillTargetFishingSpot, 3}), 0)
	if !ok || m.typ != protocol.MessageSkillAction {
		t.Fatalf("SkillAction: %+v", m)
	}
	env := protocol.GetRootAsEnvelope(encodeShopBuy(b, smallNetID), 0)
	var tbl flatbuffers.Table
	var sb protocol.ShopBuy
	if env.MsgType() != protocol.MessageShopBuy || !env.Msg(&tbl) {
		t.Fatal("ShopBuy")
	}
	sb.Init(tbl.Bytes, tbl.Pos)
	if sb.ItemId() != smallNetID || sb.Quantity() != 1 {
		t.Errorf("ShopBuy %d x%d", sb.ItemId(), sb.Quantity())
	}

	b.Reset()
	protocol.ResourceStateStartFelledTreesVector(b, 2)
	b.PrependUint16(9)
	b.PrependUint16(4)
	felled := b.EndVector(2)
	protocol.ResourceStateStartFishingSpotsVector(b, 1)
	protocol.CreateFishingSpotState(b, 7, 2)
	spots := b.EndVector(1)
	protocol.ResourceStateStart(b)
	protocol.ResourceStateAddFelledTrees(b, felled)
	protocol.ResourceStateAddFishingSpots(b, spots)
	m, ok = decode(finish(b, protocol.MessageResourceState, protocol.ResourceStateEnd(b)), 0)
	if !ok || !m.res.known || !m.res.felled[4] || !m.res.felled[9] || len(m.res.spots) != 1 || m.res.spots[0] != (spotState{7, 2}) {
		t.Errorf("ResourceState %+v ok=%v", m.res, ok)
	}

	b.Reset()
	protocol.SkillResultStart(b)
	protocol.SkillResultAddCode(b, protocol.SkillResultCodeSuccess)
	m, ok = decode(finish(b, protocol.MessageSkillResult, protocol.SkillResultEnd(b)), 0)
	if !ok || m.skillCode != protocol.SkillResultCodeSuccess {
		t.Errorf("SkillResult %+v", m)
	}
}

func TestSkillSummary(t *testing.T) {
	st := &stats{}
	st.noteSkillResult(protocol.SkillResultCodeStarted)
	st.noteSkillResult(protocol.SkillResultCodeSuccess)
	st.noteSkillResult(protocol.SkillResultCodeSuccess)
	by, total := st.skillResults()
	if total != 3 || by["Success"] != 2 || by["Started"] != 1 {
		t.Errorf("%v %d", by, total)
	}
	data, _ := json.Marshal(&summary{SkillActionsSent: 5, SkillResultsReceived: 3, SkillResultsByCode: by})
	var got map[string]json.RawMessage
	_ = json.Unmarshal(data, &got)
	if string(got["skill_actions_sent"]) != "5" || string(got["skill_results_received"]) != "3" ||
		string(got["skill_results_by_code"]) != `{"Started":1,"Success":2}` {
		t.Errorf("%s", data)
	}
}

func TestLoadGatherMap(t *testing.T) {
	m, err := loadGatherMap("../../../../shared/map/skills.json", "../../../../shared/map/areas.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.trees) != 22 || m.trees[0] != (place{"tree", 32.5, 6.5}) || m.shop != [2]float64{-21.5, -95.5} {
		t.Errorf("%d trees, first %+v, shop %v", len(m.trees), m.trees[0], m.shop)
	}
}
