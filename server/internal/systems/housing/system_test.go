package housing_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

var sysStart = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

type recSink struct {
	results []housing.Outcome
	lists   [][]housing.Info
	lapsed  [][2]uint64
}

func (s *recSink) HouseResult(_ world.PlayerID, _ housing.HouseID, o housing.Outcome) {
	s.results = append(s.results, o)
}
func (s *recSink) HouseList(h []housing.Info) { s.lists = append(s.lists, h) }
func (s *recSink) HouseLapsedEvent(h housing.HouseID, prev world.PlayerID) {
	s.lapsed = append(s.lapsed, [2]uint64{uint64(h), uint64(prev)})
}

type recCommitter struct {
	reqs   []housing.CommitRequest
	locked func() bool // inventory lock state at commit time
	atCall []bool
}

func (c *recCommitter) CommitHousing(r housing.CommitRequest) {
	c.reqs = append(c.reqs, r)
	if c.locked != nil {
		c.atCall = append(c.atCall, c.locked())
	}
}

type sysFixture struct {
	t     *testing.T
	d     *gamedata.Data
	clock *world.FakeClock
	w     *world.World
	sys   *housing.System
	sink  *recSink
	com   *recCommitter
	p     *world.Player
}

var door = world.Vec2{X: 10, Y: 10}

func newSysFixture(t *testing.T, plots []housing.Plot, gold int) *sysFixture {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	f := &sysFixture{t: t, d: d, clock: world.NewFakeClock(sysStart), w: world.New(world.DefaultConfig()),
		sink: &recSink{}, com: &recCommitter{}}
	f.sys, err = housing.NewSystem(d, housing.DefaultConfig(), plots, f.clock, f.sink, f.com)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.w.Register(f.sys); err != nil {
		t.Fatal(err)
	}
	f.p = &world.Player{ID: 1, Pos: door, Inv: world.NewInventory(d)}
	f.p.Inv.Gold = gold
	if err := f.w.AddPlayer(f.p); err != nil {
		t.Fatal(err)
	}
	f.com.locked = f.p.Inv.Locked
	return f
}

func (f *sysFixture) run(cmds ...world.Command) []housing.Outcome {
	f.t.Helper()
	for _, c := range cmds {
		f.w.Enqueue(c)
	}
	f.w.Tick()
	got := f.sink.results
	f.sink.results = nil
	return got
}

func (f *sysFixture) info(id housing.HouseID) housing.Info {
	for _, i := range f.sys.List() {
		if i.ID == id {
			return i
		}
	}
	f.t.Fatalf("no house %d", id)
	return housing.Info{}
}

func TestSystemClaimRefusals(t *testing.T) {
	other := sysStart.Add(90 * 24 * time.Hour)
	plots := []housing.Plot{
		{ID: 1, Door: door},
		{ID: 2, Door: door, Owner: 9, PaidUntil: other},
		{ID: 3, Door: world.Vec2{X: 500, Y: 500}},
		{ID: 4, Door: door, Owner: 1, PaidUntil: other},
	}
	tests := []struct {
		name  string
		house housing.HouseID
		gold  int
		owns  bool
		want  housing.Outcome
	}{
		{"too far", 3, 1000, false, housing.TooFar},
		{"already owned", 2, 1000, false, housing.AlreadyOwned},
		{"not enough gold", 1, 399, false, housing.NotEnoughGold},
		{"no such house", 77, 1000, false, housing.NoSuchHouse},
		{"owns another", 1, 1000, true, housing.OwnsAnother},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := plots
			if !tc.owns {
				ps = plots[:3]
			}
			f := newSysFixture(t, ps, tc.gold)
			got := f.run(housing.ClaimCommand{Player: 1, House: tc.house})
			if !reflect.DeepEqual(got, []housing.Outcome{tc.want}) {
				t.Fatalf("results %v, want [%v]", got, tc.want)
			}
			if len(f.com.reqs) != 0 || f.p.Inv.Gold != tc.gold || f.p.Inv.Locked() || len(f.sink.lists) != 0 {
				t.Fatal("refusal changed something")
			}
		})
	}
}

func TestSystemClaimCommit(t *testing.T) {
	f := newSysFixture(t, []housing.Plot{{ID: 1, Door: door}, {ID: 2, Door: door}}, 1000)
	if got := f.run(housing.ClaimCommand{Player: 1, House: 1}); len(got) != 0 {
		t.Fatalf("results before commit: %v", got)
	}
	if len(f.com.reqs) != 1 || !f.com.atCall[0] || !f.p.Inv.Locked() {
		t.Fatalf("want one commit with inventory locked, got %v", f.com.reqs)
	}
	r := f.com.reqs[0]
	if r.House != 1 || r.Player != 1 || r.Gold != 400 || !r.PaidUntil.Equal(sysStart.Add(60*24*time.Hour)) {
		t.Fatalf("bad request %+v", r)
	}
	if f.p.Inv.Gold != 1000 || f.info(1).Owner != 0 {
		t.Fatal("changed before confirmation")
	}
	// A second request while pending is refused.
	if got := f.run(housing.ClaimCommand{Player: 1, House: 2}); !reflect.DeepEqual(got, []housing.Outcome{housing.Busy}) {
		t.Fatalf("second request: %v", got)
	}
	f.sys.Resolve(r.ID, true)
	if got := f.run(); !reflect.DeepEqual(got, []housing.Outcome{housing.Ok}) {
		t.Fatalf("results %v", got)
	}
	if f.p.Inv.Gold != 600 || f.p.Inv.Locked() || f.info(1).Owner != 1 || len(f.p.PendingEconomy()) != 1 {
		t.Fatal("not applied after confirmation")
	}
	if len(f.sink.lists) != 1 || len(f.sink.lists[0]) != 2 || f.sink.lists[0][0].State != housing.Owned {
		t.Fatalf("house list %+v", f.sink.lists)
	}
	// One house at most.
	if got := f.run(housing.ClaimCommand{Player: 1, House: 2}); !reflect.DeepEqual(got, []housing.Outcome{housing.OwnsAnother}) {
		t.Fatalf("got %v", got)
	}
}

func TestSystemFailedCommit(t *testing.T) {
	f := newSysFixture(t, []housing.Plot{{ID: 1, Door: door}}, 1000)
	f.run(housing.ClaimCommand{Player: 1, House: 1})
	f.sys.Resolve(f.com.reqs[0].ID, false)
	if got := f.run(); !reflect.DeepEqual(got, []housing.Outcome{housing.SaveFailed}) {
		t.Fatalf("results %v", got)
	}
	if f.p.Inv.Gold != 1000 || f.p.Inv.Locked() || f.info(1).Owner != 0 || len(f.sink.lists) != 0 {
		t.Fatal("failed commit changed something")
	}
	// Can retry.
	f.run(housing.ClaimCommand{Player: 1, House: 1})
	if len(f.com.reqs) != 2 {
		t.Fatal("retry not committed")
	}
}

func TestSystemPayRent(t *testing.T) {
	paid := sysStart.Add(60 * 24 * time.Hour)
	plots := []housing.Plot{{ID: 1, Door: door, Owner: 1, PaidUntil: paid}, {ID: 2, Door: door, Owner: 9, PaidUntil: paid}}
	f := newSysFixture(t, plots, 1000)
	tests := []struct {
		house  housing.HouseID
		months int
		want   housing.Outcome
	}{
		{2, 1, housing.NotOwner},
		{1, 5, housing.TooManyMonths}, // 2+5 > 6
		{1, 0, housing.BadMonths},
	}
	for _, tc := range tests {
		got := f.run(housing.PayRentCommand{Player: 1, House: tc.house, Months: tc.months})
		if !reflect.DeepEqual(got, []housing.Outcome{tc.want}) {
			t.Fatalf("house %d months %d: %v, want %v", tc.house, tc.months, got, tc.want)
		}
	}
	if len(f.com.reqs) != 0 {
		t.Fatal("refusals committed")
	}
	f.run(housing.PayRentCommand{Player: 1, House: 1, Months: 2})
	r := f.com.reqs[0]
	if r.Kind != housing.CommitRent || r.Gold != 400 || !r.PaidUntil.Equal(paid.Add(60*24*time.Hour)) || !f.p.Inv.Locked() {
		t.Fatalf("bad request %+v", r)
	}
	f.sys.Resolve(r.ID, true)
	f.run()
	if f.p.Inv.Gold != 600 || !f.info(1).PaidUntil.Equal(r.PaidUntil) || f.p.Inv.Locked() {
		t.Fatal("rent not applied")
	}
}

func TestSystemLapse(t *testing.T) {
	// 31 days left: one month plus 1 day, inside the warning window.
	plots := []housing.Plot{{ID: 1, Door: door, Owner: 1, PaidUntil: sysStart.Add(31 * 24 * time.Hour)}}
	f := newSysFixture(t, plots, 0)
	f.run()
	if st := f.info(1).State; st != housing.Warning {
		t.Fatalf("state %v, want Warning", st)
	}
	if len(f.com.reqs) != 0 {
		t.Fatal("lapse asked early")
	}
	f.clock.Advance(2 * 24 * time.Hour)
	f.run()
	if len(f.com.reqs) != 1 || f.com.reqs[0].Kind != housing.CommitLapse || f.com.reqs[0].Player != 1 || !f.com.atCall[0] || !f.p.Inv.Locked() {
		t.Fatalf("want one locked lapse commit, got %+v", f.com.reqs)
	}
	f.run() // no second commit while pending
	if len(f.com.reqs) != 1 || f.info(1).Owner != 1 || len(f.sink.lapsed) != 0 {
		t.Fatal("changed before confirmation")
	}
	// Failure: still owned, unlocked, retried on a later tick.
	f.sys.Resolve(f.com.reqs[0].ID, false)
	f.run()
	if len(f.com.reqs) != 2 || f.info(1).Owner != 1 || len(f.sink.lapsed) != 0 {
		t.Fatalf("after failure: %d commits", len(f.com.reqs))
	}
	f.sys.Resolve(f.com.reqs[1].ID, true)
	f.run()
	if f.info(1).Owner != 0 || f.p.Inv.Locked() || !reflect.DeepEqual(f.sink.lapsed, [][2]uint64{{1, 1}}) {
		t.Fatalf("lapse not applied: %+v", f.sink.lapsed)
	}
	if len(f.sink.lists) != 1 || f.sink.lists[0][0].Owner != 0 || f.sink.lists[0][0].State != housing.Empty {
		t.Fatalf("list %+v", f.sink.lists)
	}
	f.run()
	if len(f.com.reqs) != 2 {
		t.Fatal("lapsed again")
	}
}

func TestSystemLapseOfflineOwner(t *testing.T) {
	plots := []housing.Plot{{ID: 1, Door: door, Owner: 7, PaidUntil: sysStart}}
	f := newSysFixture(t, plots, 0)
	f.run()
	if len(f.com.reqs) != 1 || f.com.reqs[0].Player != 7 || f.p.Inv.Locked() {
		t.Fatalf("got %+v", f.com.reqs)
	}
	f.sys.Resolve(f.com.reqs[0].ID, true)
	f.run()
	if !reflect.DeepEqual(f.sink.lapsed, [][2]uint64{{1, 7}}) {
		t.Fatalf("lapsed %v", f.sink.lapsed)
	}
}
