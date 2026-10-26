package trade_test

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// tileSize is the collision grid's cell size in metres (shared/map/config.mjs
// CELL_SIZE).
const tileSize = 1.0

const (
	a world.PlayerID = 1
	b world.PlayerID = 2
	c world.PlayerID = 3
)

// pvpEdge is just inside the PvP zone's northern edge (z = -280 m);
// outsidePvP is 2 m away, just outside it.
var (
	pvpEdge    = world.Vec2{X: 300, Y: -281}
	outsidePvP = world.Vec2{X: 300, Y: -279}
)

type event struct {
	kind string // "notice", "state" or "result"
	to   world.PlayerID
	from world.PlayerID
	view trade.View
	r    trade.Result
}

type recSink struct{ got []event }

func (s *recSink) TradeRequestNotice(to, from world.PlayerID) {
	s.got = append(s.got, event{kind: "notice", to: to, from: from})
}
func (s *recSink) TradeState(to world.PlayerID, v trade.View) {
	s.got = append(s.got, event{kind: "state", to: to, view: v})
}
func (s *recSink) TradeResult(to world.PlayerID, r trade.Result) {
	s.got = append(s.got, event{kind: "result", to: to, r: r})
}

// take returns and forgets the events received so far.
func (s *recSink) take() []event {
	got := s.got
	s.got = nil
	return got
}

type recCommitter struct{ got []trade.CommitRequest }

func (c *recCommitter) CommitTrade(req trade.CommitRequest) {
	c.got = append(c.got, req)
}

type fixture struct {
	t      *testing.T
	d      *gamedata.Data
	l      *areas.Lookup
	w      *world.World
	sys    *trade.System
	sink   *recSink
	commit *recCommitter
	p      map[world.PlayerID]*world.Player
}

// newFixture returns a world with the trade system and players a, b and c
// standing at the spawn point, 1 m apart, with the starting kit (100 gold,
// Boba in slots 0-2).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	l, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, d: d, l: l, w: world.New(world.DefaultConfig()), sink: &recSink{}, commit: &recCommitter{}, p: map[world.PlayerID]*world.Player{}}
	f.sys = trade.New(d, l, tileSize, f.sink, f.commit)
	if err := f.w.Register(f.sys); err != nil {
		t.Fatal(err)
	}
	for i, id := range []world.PlayerID{a, b, c} {
		inv, _, err := world.NewStartingInventory(d)
		if err != nil {
			t.Fatal(err)
		}
		pos := l.Spawn()
		pos.X += float32(i)
		p := &world.Player{ID: id, Pos: pos, Inv: inv}
		f.p[id] = p
		if err := f.w.AddPlayer(p); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// run enqueues cmds, runs one tick and returns the events sent.
func (f *fixture) run(cmds ...world.Command) []event {
	f.t.Helper()
	for _, c := range cmds {
		f.w.Enqueue(c)
	}
	f.w.Tick()
	return f.sink.take()
}

// open opens a trade between a and b and drops the opening events.
func (f *fixture) open() {
	f.t.Helper()
	f.run(trade.RequestCommand{Player: a, Partner: b})
	f.run(trade.RequestCommand{Player: b, Partner: a})
	if !f.sys.Trading(a) || !f.sys.Trading(b) {
		f.t.Fatal("trade did not open")
	}
}

func (f *fixture) itemID(key string) gamedata.ItemID {
	f.t.Helper()
	it, ok := f.d.ItemByKey(key)
	if !ok {
		f.t.Fatalf("no item %q", key)
	}
	return it.ID
}

func results(to1, to2 world.PlayerID, r trade.Result) []event {
	return []event{{kind: "result", to: to1, r: r}, {kind: "result", to: to2, r: r}}
}

func states(screen int, offerA, offerB trade.Offer, accA, accB bool) []event {
	return []event{
		{kind: "state", to: a, view: trade.View{Partner: b, Screen: screen, Mine: offerA, Partners: offerB, MyAccepted: accA, PartnerAccepted: accB}},
		{kind: "state", to: b, view: trade.View{Partner: a, Screen: screen, Mine: offerB, Partners: offerA, MyAccepted: accB, PartnerAccepted: accA}},
	}
}

func check(t *testing.T, got, want []event) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events:\n got %+v\nwant %+v", got, want)
	}
}

func TestRequestOpensTrade(t *testing.T) {
	f := newFixture(t)
	check(t, f.run(trade.RequestCommand{Player: a, Partner: b}), []event{{kind: "notice", to: b, from: a}})
	if f.sys.Trading(a) || f.sys.Trading(b) {
		t.Fatal("trade opened before the partner accepted")
	}
	check(t, f.run(trade.RequestCommand{Player: b, Partner: a}), states(trade.ScreenOffer, trade.Offer{}, trade.Offer{}, false, false))
	if !f.sys.Trading(a) || !f.sys.Trading(b) {
		t.Fatal("trade not open")
	}
}

func TestRequestRefused(t *testing.T) {
	reach := float32(gameDataRules(t).TradeDistanceTiles) * tileSize
	tests := []struct {
		name  string
		setup func(f *fixture)
		want  []event
	}{
		{"too far", func(f *fixture) { f.p[b].Pos.X = f.p[a].Pos.X + reach + 0.5 }, []event{{kind: "result", to: a, r: trade.TooFar}}},
		{"at the limit", func(f *fixture) { f.p[b].Pos.X = f.p[a].Pos.X + reach }, []event{{kind: "notice", to: b, from: a}}},
		{"from pvp zone", func(f *fixture) { f.p[a].Pos, f.p[b].Pos = pvpEdge, outsidePvP }, []event{{kind: "result", to: a, r: trade.InPvpZone}}},
		{"to pvp zone", func(f *fixture) { f.p[a].Pos, f.p[b].Pos = outsidePvP, pvpEdge }, []event{{kind: "result", to: a, r: trade.InPvpZone}}},
		{"partner busy", func(f *fixture) {
			f.run(trade.RequestCommand{Player: b, Partner: c})
			f.run(trade.RequestCommand{Player: c, Partner: b})
		}, []event{{kind: "result", to: a, r: trade.PartnerBusy}}},
		{"self", func(f *fixture) {}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			partner := b
			if tt.name == "self" {
				partner = a
			}
			check(t, f.run(trade.RequestCommand{Player: a, Partner: partner}), tt.want)
			if f.sys.Trading(a) {
				t.Error("a is trading")
			}
		})
	}
}

func gameDataRules(t *testing.T) gamedata.Rules {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d.Rules
}

func TestOffers(t *testing.T) {
	boba := func(f *fixture) gamedata.ItemID { return f.itemID("boba") }
	tests := []struct {
		name  string
		setup []func(f *fixture) world.Command
		cmd   func(f *fixture) world.Command
		want  func(f *fixture) *trade.Offer // nil func: refused
	}{
		{"offer slot", nil,
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 1} },
			func(f *fixture) *trade.Offer {
				return &trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba(f), Qty: 1}}}
			}},
		{"offer more than slot holds", nil,
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 2} },
			nil},
		{"offer already offered", []func(*fixture) world.Command{
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 1} }},
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 1} },
			nil},
		{"offer zero", nil,
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 0} },
			nil},
		{"offer empty slot", nil,
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 5, Quantity: 1} },
			nil},
		{"offer negative slot", nil,
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: -1, Quantity: 1} },
			nil},
		{"offer slot out of range", nil,
			func(f *fixture) world.Command {
				return trade.OfferCommand{Player: a, Slot: len(f.p[a].Inv.Slots), Quantity: 1}
			},
			nil},
		{"offer second slot", []func(*fixture) world.Command{
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 1} }},
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 2, Quantity: 1} },
			func(f *fixture) *trade.Offer {
				return &trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba(f), Qty: 1}, {Slot: 2, Item: boba(f), Qty: 1}}}
			}},
		{"remove", []func(*fixture) world.Command{
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 1} },
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 1, Quantity: 1} }},
			func(*fixture) world.Command { return trade.RemoveCommand{Player: a, Index: 0} },
			func(f *fixture) *trade.Offer {
				return &trade.Offer{Items: []trade.Item{{Slot: 1, Item: boba(f), Qty: 1}}}
			}},
		{"remove out of range", []func(*fixture) world.Command{
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 1} }},
			func(*fixture) world.Command { return trade.RemoveCommand{Player: a, Index: 1} },
			nil},
		{"remove negative", []func(*fixture) world.Command{
			func(*fixture) world.Command { return trade.OfferCommand{Player: a, Slot: 0, Quantity: 1} }},
			func(*fixture) world.Command { return trade.RemoveCommand{Player: a, Index: -1} },
			nil},
		{"remove from empty offer", nil,
			func(*fixture) world.Command { return trade.RemoveCommand{Player: a, Index: 0} },
			nil},
		{"set gold", nil,
			func(f *fixture) world.Command { return trade.SetGoldCommand{Player: a, Gold: f.p[a].Inv.Gold} },
			func(f *fixture) *trade.Offer { return &trade.Offer{Gold: f.p[a].Inv.Gold} }},
		{"replace gold", []func(*fixture) world.Command{
			func(*fixture) world.Command { return trade.SetGoldCommand{Player: a, Gold: 40} }},
			func(*fixture) world.Command { return trade.SetGoldCommand{Player: a, Gold: 10} },
			func(*fixture) *trade.Offer { return &trade.Offer{Gold: 10} }},
		{"gold over balance", nil,
			func(f *fixture) world.Command { return trade.SetGoldCommand{Player: a, Gold: f.p[a].Inv.Gold + 1} },
			nil},
		{"negative gold", nil,
			func(*fixture) world.Command { return trade.SetGoldCommand{Player: a, Gold: -1} },
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.open()
			var offerA trade.Offer
			for _, s := range tt.setup {
				got := f.run(s(f))
				if len(got) != 2 {
					t.Fatalf("setup: %+v", got)
				}
				offerA = got[0].view.Mine
			}
			// Both accept screen 1 so a valid change can be seen to clear them.
			f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer})
			before := f.p[a].Inv.Clone()
			got := f.run(tt.cmd(f))
			if !reflect.DeepEqual(f.p[a].Inv, before) {
				t.Errorf("inventory changed: %+v, was %+v", f.p[a].Inv, before)
			}
			if tt.want == nil {
				check(t, got, nil)
				// Nothing changed: a's accept still counts.
				check(t, f.run(trade.AcceptCommand{Player: b, Screen: trade.ScreenOffer}), states(trade.ScreenConfirm, offerA, trade.Offer{}, false, false))
				return
			}
			check(t, got, states(trade.ScreenOffer, *tt.want(f), trade.Offer{}, false, false))
		})
	}
}

func TestChangeOnScreen2ReturnsToScreen1(t *testing.T) {
	f := newFixture(t)
	f.open()
	f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer})
	f.run(trade.AcceptCommand{Player: b, Screen: trade.ScreenOffer})
	f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenConfirm})
	check(t, f.run(trade.SetGoldCommand{Player: b, Gold: 5}), states(trade.ScreenOffer, trade.Offer{}, trade.Offer{Gold: 5}, false, false))
}

func TestAccept(t *testing.T) {
	f := newFixture(t)
	f.open()
	boba := f.itemID("boba")
	f.run(trade.OfferCommand{Player: a, Slot: 1, Quantity: 1})
	f.run(trade.SetGoldCommand{Player: b, Gold: 30})
	offerA := trade.Offer{Items: []trade.Item{{Slot: 1, Item: boba, Qty: 1}}}
	offerB := trade.Offer{Gold: 30}

	steps := []struct {
		name string
		cmd  world.Command
		want []event
	}{
		{"wrong screen", trade.AcceptCommand{Player: a, Screen: trade.ScreenConfirm}, nil},
		{"a accepts 1", trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer}, states(trade.ScreenOffer, offerA, offerB, true, false)},
		{"a accepts 1 again", trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer}, nil},
		{"b accepts 1", trade.AcceptCommand{Player: b, Screen: trade.ScreenOffer}, states(trade.ScreenConfirm, offerA, offerB, false, false)},
		{"stale accept of 1", trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer}, nil},
		{"b accepts 2", trade.AcceptCommand{Player: b, Screen: trade.ScreenConfirm}, states(trade.ScreenConfirm, offerA, offerB, false, true)},
		{"a accepts 2", trade.AcceptCommand{Player: a, Screen: trade.ScreenConfirm}, nil},
		{"a accepts 2 again", trade.AcceptCommand{Player: a, Screen: trade.ScreenConfirm}, nil},
		{"b accepts 2 again", trade.AcceptCommand{Player: b, Screen: trade.ScreenConfirm}, nil},
	}
	for _, s := range steps {
		check(t, f.run(s.cmd), s.want)
		if t.Failed() {
			t.Fatalf("step %q", s.name)
		}
	}
	if len(f.commit.got) != 1 {
		t.Fatalf("commits = %+v, want 1", f.commit.got)
	}
	got := f.commit.got[0]
	if got.A.Player != a || got.B.Player != b || !reflect.DeepEqual(got.A.Gave, offerA) || !reflect.DeepEqual(got.B.Gave, offerB) {
		t.Errorf("commit = %+v, want a gives %+v, b gives %+v", got, offerA, offerB)
	}
	if f.sys.Trading(a) || f.sys.Trading(b) {
		t.Error("trade still open after commit")
	}
}

// TestNoCommitAfterLeaving checks that a player who left the world, entered
// the PvP zone or moved away after accepting screen 2 (the session removes
// players after the trade system's Tick) can't have the trade completed by
// the partner's accept.
func TestNoCommitAfterLeaving(t *testing.T) {
	reach := float32(gameDataRules(t).TradeDistanceTiles) * tileSize
	tests := []struct {
		name string
		act  func(f *fixture)
		want []event
	}{
		{"b leaves", func(f *fixture) { f.w.RemovePlayer(b) }, results(a, b, trade.Cancelled)},
		{"b enters pvp", func(f *fixture) { f.p[a].Pos, f.p[b].Pos = outsidePvP, pvpEdge }, results(a, b, trade.InPvpZone)},
		{"b moves away", func(f *fixture) { f.p[b].Pos.X = f.p[a].Pos.X + reach + 0.1 }, results(a, b, trade.TooFar)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.open()
			f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer})
			f.run(trade.AcceptCommand{Player: b, Screen: trade.ScreenOffer})
			f.run(trade.AcceptCommand{Player: b, Screen: trade.ScreenConfirm})
			tt.act(f)
			check(t, f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenConfirm}), tt.want)
			if len(f.commit.got) != 0 {
				t.Errorf("commits = %+v", f.commit.got)
			}
		})
	}
}

func TestClose(t *testing.T) {
	reach := float32(gameDataRules(t).TradeDistanceTiles) * tileSize
	tests := []struct {
		name string
		act  func(f *fixture) []world.Command
		want []event
	}{
		{"a cancels", func(*fixture) []world.Command { return []world.Command{trade.CancelCommand{Player: a}} }, results(a, b, trade.Cancelled)},
		{"b cancels", func(*fixture) []world.Command { return []world.Command{trade.CancelCommand{Player: b}} }, results(a, b, trade.Cancelled)},
		{"move apart", func(f *fixture) []world.Command { f.p[b].Pos.X = f.p[a].Pos.X + reach + 0.1; return nil }, results(a, b, trade.TooFar)},
		{"a enters pvp", func(f *fixture) []world.Command { f.p[a].Pos, f.p[b].Pos = pvpEdge, outsidePvP; return nil }, results(a, b, trade.InPvpZone)},
		{"b enters pvp", func(f *fixture) []world.Command { f.p[a].Pos, f.p[b].Pos = outsidePvP, pvpEdge; return nil }, results(a, b, trade.InPvpZone)},
		{"a leaves", func(f *fixture) []world.Command { f.w.RemovePlayer(a); return nil }, results(a, b, trade.Cancelled)},
		{"b leaves", func(f *fixture) []world.Command { f.w.RemovePlayer(b); return nil }, results(a, b, trade.Cancelled)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.open()
			check(t, f.run(tt.act(f)...), tt.want)
			if f.sys.Trading(a) || f.sys.Trading(b) {
				t.Error("trade still open")
			}
			// Nothing more is sent, and no accept reaches the committer.
			check(t, f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer}), nil)
			if len(f.commit.got) != 0 {
				t.Errorf("commits = %+v", f.commit.got)
			}
		})
	}
}

func TestStillInReachStaysOpen(t *testing.T) {
	f := newFixture(t)
	f.open()
	reach := float32(f.d.Rules.TradeDistanceTiles) * tileSize
	f.p[b].Pos.X = f.p[a].Pos.X + reach
	check(t, f.run(), nil)
	if !f.sys.Trading(a) {
		t.Error("trade closed at the distance limit")
	}
}

// TestDistanceFromRules checks that the reach follows Rules.TradeDistanceTiles.
func TestDistanceFromRules(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	l, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tiles := range []int{1, 3, 7} {
		d.Rules.TradeDistanceTiles = tiles
		s := trade.New(d, l, 2, &recSink{}, &recCommitter{})
		at := world.Vec2{X: float32(tiles * 2)}
		if !s.InReach(world.Vec2{}, at) {
			t.Errorf("%d tiles: %v not in reach", tiles, at)
		}
		at.X += 0.01
		if s.InReach(world.Vec2{}, at) {
			t.Errorf("%d tiles: %v in reach", tiles, at)
		}
	}
}

// TestNoForbiddenDeps checks that the package depends, directly or
// transitively, on no transport, store, api, network or database code.
func TestNoForbiddenDeps(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, bad := range []string{"/internal/transport", "/internal/store", "/internal/api", "/internal/session", "pgx", "websocket", "flatbuffers"} {
			if strings.Contains(dep, bad) {
				t.Errorf("depends on %s", dep)
			}
		}
		if dep == "net" || strings.HasPrefix(dep, "net/") || strings.HasPrefix(dep, "database/") {
			t.Errorf("depends on %s", dep)
		}
	}
}

func BenchmarkTickOpenTrades(b *testing.B) {
	d, err := gamedata.Default()
	if err != nil {
		b.Fatal(err)
	}
	l, err := areas.Load()
	if err != nil {
		b.Fatal(err)
	}
	sys := trade.New(d, l, tileSize, &recSink{}, &recCommitter{})
	w := world.New(world.DefaultConfig())
	if err := w.Register(sys); err != nil {
		b.Fatal(err)
	}
	const players = 1000
	for i := range players {
		p := &world.Player{ID: world.PlayerID(i + 1), Pos: world.Vec2{X: float32(i / 2), Y: 0}}
		if err := w.AddPlayer(p); err != nil {
			b.Fatal(err)
		}
	}
	for i := 1; i <= players; i += 2 {
		w.Enqueue(trade.RequestCommand{Player: world.PlayerID(i), Partner: world.PlayerID(i + 1)})
	}
	w.Tick()
	for i := 1; i <= players; i += 2 {
		w.Enqueue(trade.RequestCommand{Player: world.PlayerID(i + 1), Partner: world.PlayerID(i)})
	}
	w.Tick()
	if !sys.Trading(1) {
		b.Fatal("trades not open")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Tick()
	}
}
