package bank_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/systems/bank"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// tileSize is the collision grid's cell size in metres (shared/map/config.mjs
// CELL_SIZE).
const tileSize = 1.0

const pid world.PlayerID = 1

var (
	// booth is a booth position in the safe circle, near the shop stall
	// (docs/specs/map.md).
	booth = world.Vec2{X: -12, Y: -96}
	// pvpBooth is a position in the PvP zone, for a booth the player can
	// reach there.
	pvpBooth = world.Vec2{X: 250, Y: -350}
	start    = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
)

type recSink struct {
	results []bank.Result
	states  []bank.View
}

func (s *recSink) BankState(to world.PlayerID, v bank.View)    { s.states = append(s.states, v) }
func (s *recSink) BankResult(to world.PlayerID, r bank.Result) { s.results = append(s.results, r) }

type recCommitter struct{ reqs []bank.CommitRequest }

func (c *recCommitter) CommitBank(req bank.CommitRequest) { c.reqs = append(c.reqs, req) }

type fixture struct {
	t      *testing.T
	d      *gamedata.Data
	clock  *world.FakeClock
	w      *world.World
	sys    *bank.System
	sink   *recSink
	commit *recCommitter
	p      *world.Player
}

// newFixture returns a world with the bank system for a booth at b and one
// player standing on it with an empty inventory and bank.
func newFixture(t *testing.T, b world.Vec2) *fixture {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	l, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, d: d, clock: world.NewFakeClock(start), w: world.New(world.DefaultConfig()),
		sink: &recSink{}, commit: &recCommitter{}}
	f.sys = bank.New(d, l, b, tileSize, f.clock, f.sink, f.commit)
	if err := f.w.Register(f.sys); err != nil {
		t.Fatal(err)
	}
	f.p = &world.Player{ID: pid, Pos: b, Inv: world.NewInventory(d)}
	if err := f.w.AddPlayer(f.p); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) item(key string) gamedata.ItemID {
	f.t.Helper()
	it, ok := f.d.ItemByKey(key)
	if !ok {
		f.t.Fatalf("no item %q", key)
	}
	return it.ID
}

// run enqueues cmds, runs one tick and returns the results sent, clearing
// the sink.
func (f *fixture) run(cmds ...world.Command) []bank.Result {
	f.t.Helper()
	for _, c := range cmds {
		f.w.Enqueue(c)
	}
	f.w.Tick()
	got := f.sink.results
	f.sink.results = nil
	return got
}

// open opens the bank and fails unless it opened.
func (f *fixture) open() {
	f.t.Helper()
	if got := f.run(bank.OpenCommand{Player: pid}); !reflect.DeepEqual(got, []bank.Result{bank.Ok}) {
		f.t.Fatalf("open: results %v, want [ok]", got)
	}
}

// do runs cmd on an open bank, resolves its commit, if any, with ok, and
// returns every result sent.
func (f *fixture) do(cmd world.Command, ok bool) []bank.Result {
	f.t.Helper()
	n := len(f.commit.reqs)
	got := f.run(cmd)
	if len(f.commit.reqs) > n {
		f.sys.Resolve(f.commit.reqs[n].ID, ok)
		got = append(got, f.run()...)
	}
	return got
}

// setInv fills the inventory from slot 0 with stacks.
func (f *fixture) setInv(stacks ...world.Stack) {
	f.t.Helper()
	inv := world.NewInventory(f.d)
	copy(inv.Slots, stacks)
	f.p.Inv = inv
}

func repeat(s world.Stack, n int) []world.Stack {
	out := make([]world.Stack, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// TestOpen checks the bank opens within the bank distance of the booth and
// is refused, each with its own result, too far away, in combat and in the
// PvP zone.
func TestOpen(t *testing.T) {
	reach := float32(2 * tileSize) // Rules.BankDistanceTiles
	tests := []struct {
		name       string
		booth      world.Vec2
		offset     world.Vec2
		lastCombat time.Duration // before now; 0 means never
		want       bank.Result
	}{
		{"on the booth", booth, world.Vec2{}, 0, bank.Ok},
		{"at the bank distance", booth, world.Vec2{X: reach}, 0, bank.Ok},
		{"diagonally inside", booth, world.Vec2{X: 1.4, Y: 1.4}, 0, bank.Ok},
		{"just past the bank distance", booth, world.Vec2{X: reach + 0.1}, 0, bank.TooFar},
		{"diagonally too far", booth, world.Vec2{X: 1.5, Y: 1.5}, 0, bank.TooFar},
		{"in combat", booth, world.Vec2{}, time.Second, bank.InCombat},
		{"combat lock just running", booth, world.Vec2{}, 10*time.Second - time.Millisecond, bank.InCombat},
		{"combat lock over", booth, world.Vec2{}, 10 * time.Second, bank.Ok},
		{"in the PvP zone", pvpBooth, world.Vec2{}, 0, bank.InPvpZone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.booth)
			f.p.Pos = world.Vec2{X: tt.booth.X + tt.offset.X, Y: tt.booth.Y + tt.offset.Y}
			if tt.lastCombat > 0 {
				f.p.LastCombat = start.Add(-tt.lastCombat)
			}
			got := f.run(bank.OpenCommand{Player: pid})
			if !reflect.DeepEqual(got, []bank.Result{tt.want}) {
				t.Fatalf("results %v, want [%v]", got, tt.want)
			}
			if f.sys.IsOpen(pid) != (tt.want == bank.Ok) {
				t.Errorf("IsOpen = %v", f.sys.IsOpen(pid))
			}
			if wantStates := map[bool]int{true: 1, false: 0}[tt.want == bank.Ok]; len(f.sink.states) != wantStates {
				t.Errorf("%d bank states sent, want %d", len(f.sink.states), wantStates)
			}
		})
	}
	refusals := []bank.Result{bank.TooFar, bank.InCombat, bank.InPvpZone}
	for i, a := range refusals {
		if a == bank.Ok {
			t.Errorf("refusal %v equals Ok", a)
		}
		for _, b := range refusals[i+1:] {
			if a == b {
				t.Errorf("refusals %v and %v share a code", a, b)
			}
		}
	}
}

// TestCloses checks an open bank closes when its player walks out of reach
// or enters combat, and that requests then get NotOpen.
func TestCloses(t *testing.T) {
	tests := []struct {
		name  string
		apply func(f *fixture)
		want  bank.Result
	}{
		{"walked away", func(f *fixture) { f.p.Pos.X += 3 }, bank.TooFar},
		{"attacked", func(f *fixture) { f.p.LastCombat = start }, bank.InCombat},
		{"closed", func(f *fixture) { f.w.Enqueue(bank.CloseCommand{Player: pid}) }, bank.Closed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, booth)
			f.setInv(world.Stack{Item: f.item("boba"), Qty: 1})
			f.open()
			tt.apply(f)
			if got := f.run(); !reflect.DeepEqual(got, []bank.Result{tt.want}) {
				t.Fatalf("results %v, want [%v]", got, tt.want)
			}
			if f.sys.IsOpen(pid) {
				t.Error("bank still open")
			}
			if got := f.run(bank.DepositCommand{Player: pid, Slot: 0, Quantity: bank.All}); !reflect.DeepEqual(got, []bank.Result{bank.NotOpen}) {
				t.Errorf("deposit after close: %v, want [not_open]", got)
			}
			if len(f.commit.reqs) != 0 {
				t.Errorf("%d commits, want 0", len(f.commit.reqs))
			}
		})
	}
}

// TestDepositWithdraw checks deposits and withdrawals of one, five, all and
// a given amount, of food, loot and equipment, including several of an item
// spread over several inventory slots: the items move, every item stacks in
// one bank slot, and the commit and outbox carry the matching changes.
func TestDepositWithdraw(t *testing.T) {
	f0 := newFixture(t, booth)
	boba, acorn, dagger, hoodie := f0.item("boba"), f0.item("acorn"), f0.item("bronze_dagger"), f0.item("hoodie")
	type count struct {
		item     gamedata.ItemID
		inv, bnk int
	}
	tests := []struct {
		name     string
		inv      []world.Stack
		bnk      []world.Stack
		cmd      func(slot int) world.Command
		slot     int
		reason   string
		moved    int // quantity of the item moved
		want     count
		invSlots int // inventory slots holding the item afterwards
	}{
		{"deposit one food from three slots", repeat(world.Stack{Item: boba, Qty: 1}, 3), nil,
			dep(bank.One), 0, bank.ReasonDeposit, 1, count{boba, 2, 1}, 2},
		{"deposit five food of seven", repeat(world.Stack{Item: boba, Qty: 1}, 7), nil,
			dep(bank.Five), 3, bank.ReasonDeposit, 5, count{boba, 2, 5}, 2},
		{"deposit five food of three", repeat(world.Stack{Item: boba, Qty: 1}, 3), nil,
			dep(bank.Five), 0, bank.ReasonDeposit, 3, count{boba, 0, 3}, 0},
		{"deposit all food spread over slots", repeat(world.Stack{Item: boba, Qty: 1}, 6), nil,
			dep(bank.All), 5, bank.ReasonDeposit, 6, count{boba, 0, 6}, 0},
		{"deposit all onto the banked stack", repeat(world.Stack{Item: boba, Qty: 1}, 4), []world.Stack{{Item: acorn, Qty: 2}, {Item: boba, Qty: 10}},
			dep(bank.All), 0, bank.ReasonDeposit, 4, count{boba, 0, 14}, 0},
		{"deposit a given amount of loot", []world.Stack{{Item: acorn, Qty: 40}}, nil,
			dep(17), 0, bank.ReasonDeposit, 17, count{acorn, 23, 17}, 1},
		{"deposit all equipment items", repeat(world.Stack{Item: dagger, Qty: 1}, 3), nil,
			dep(bank.All), 1, bank.ReasonDeposit, 3, count{dagger, 0, 3}, 0},
		{"withdraw one food", nil, []world.Stack{{Item: boba, Qty: 10}},
			wd(bank.One), 0, bank.ReasonWithdraw, 1, count{boba, 1, 9}, 1},
		{"withdraw five food", nil, []world.Stack{{Item: boba, Qty: 10}},
			wd(bank.Five), 0, bank.ReasonWithdraw, 5, count{boba, 5, 5}, 5},
		{"withdraw five of three", nil, []world.Stack{{Item: boba, Qty: 3}},
			wd(bank.Five), 0, bank.ReasonWithdraw, 3, count{boba, 3, 0}, 3},
		{"withdraw all food", nil, []world.Stack{{Item: acorn, Qty: 1}, {Item: boba, Qty: 10}},
			wd(bank.All), 1, bank.ReasonWithdraw, 10, count{boba, 10, 0}, 10},
		{"withdraw a given amount", nil, []world.Stack{{Item: boba, Qty: 10}},
			wd(7), 0, bank.ReasonWithdraw, 7, count{boba, 7, 3}, 7},
		{"withdraw all only what fits", repeat(world.Stack{Item: acorn, Qty: 1}, 1), []world.Stack{{Item: boba, Qty: 40}},
			wd(bank.All), 0, bank.ReasonWithdraw, 27, count{boba, 27, 13}, 27},
		{"withdraw stackable loot onto its stack", []world.Stack{{Item: acorn, Qty: 5}}, []world.Stack{{Item: acorn, Qty: 100}},
			wd(bank.All), 0, bank.ReasonWithdraw, 100, count{acorn, 105, 0}, 1},
		{"withdraw equipment", nil, []world.Stack{{Item: hoodie, Qty: 4}},
			wd(bank.All), 0, bank.ReasonWithdraw, 4, count{hoodie, 4, 0}, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, booth)
			f.setInv(tt.inv...)
			f.p.Bank = world.Bank{Slots: append([]world.Stack(nil), tt.bnk...)}
			f.open()
			if got := f.do(tt.cmd(tt.slot), true); !reflect.DeepEqual(got, []bank.Result{bank.Ok}) {
				t.Fatalf("results %v, want [ok]", got)
			}
			if got := f.p.Inv.Count(tt.want.item); got != tt.want.inv {
				t.Errorf("inventory holds %d, want %d", got, tt.want.inv)
			}
			if got := f.p.Bank.Count(tt.want.item); got != tt.want.bnk {
				t.Errorf("bank holds %d, want %d", got, tt.want.bnk)
			}
			slots := 0
			for _, s := range f.p.Inv.Slots {
				if s.Item == tt.want.item {
					slots++
				}
			}
			if slots != tt.invSlots {
				t.Errorf("item in %d inventory slots, want %d", slots, tt.invSlots)
			}
			seen := map[gamedata.ItemID]bool{}
			for _, s := range f.p.Bank.Slots {
				if seen[s.Item] || s.Qty <= 0 {
					t.Errorf("bank slots %+v: item in two slots or empty slot", f.p.Bank.Slots)
				}
				seen[s.Item] = true
			}
			var wantCh []world.Change
			if tt.reason == bank.ReasonDeposit {
				wantCh = []world.Change{{Kind: world.ChangeRemove, Item: tt.want.item, Qty: tt.moved}, {Kind: world.ChangeBankAdd, Item: tt.want.item, Qty: tt.moved}}
			} else {
				wantCh = []world.Change{{Kind: world.ChangeBankRemove, Item: tt.want.item, Qty: tt.moved}, {Kind: world.ChangeAdd, Item: tt.want.item, Qty: tt.moved}}
			}
			req := f.commit.reqs[0]
			if req.Reason != tt.reason || !reflect.DeepEqual(req.Changes, wantCh) {
				t.Errorf("commit %s %+v, want %s %+v", req.Reason, req.Changes, tt.reason, wantCh)
			}
			ev := f.p.DrainEconomy()
			if len(ev) != 1 || ev[0].Reason != tt.reason || !reflect.DeepEqual(ev[0].Changes, wantCh) {
				t.Errorf("outbox %+v, want one %s event", ev, tt.reason)
			}
		})
	}
}

func dep(qty int) func(int) world.Command {
	return func(slot int) world.Command { return bank.DepositCommand{Player: pid, Slot: slot, Quantity: qty} }
}

func wd(qty int) func(int) world.Command {
	return func(slot int) world.Command { return bank.WithdrawCommand{Player: pid, Slot: slot, Quantity: qty} }
}

// TestDepositAll checks Deposit inventory and Deposit equipment move
// everything they can, and stop at a full bank.
func TestDepositAll(t *testing.T) {
	f0 := newFixture(t, booth)
	boba, acorn, stick, hoodie, dagger := f0.item("boba"), f0.item("acorn"), f0.item("wooden_stick"), f0.item("hoodie"), f0.item("bronze_dagger")
	tests := []struct {
		name     string
		inv      []world.Stack
		equip    world.Equipment
		bnk      []world.Stack
		cmd      world.Command
		wantInv  []world.Stack // first slots; the rest empty
		wantEq   world.Equipment
		wantBank []world.Stack
	}{
		{"deposit inventory",
			[]world.Stack{{Item: boba, Qty: 1}, {Item: acorn, Qty: 9}, {Item: boba, Qty: 1}, {Item: dagger, Qty: 1}},
			world.Equipment{Weapon: stick}, []world.Stack{{Item: acorn, Qty: 1}},
			bank.DepositInventoryCommand{Player: pid},
			nil, world.Equipment{Weapon: stick},
			[]world.Stack{{Item: acorn, Qty: 10}, {Item: boba, Qty: 2}, {Item: dagger, Qty: 1}}},
		{"deposit inventory into a nearly full bank",
			[]world.Stack{{Item: boba, Qty: 1}, {Item: dagger, Qty: 1}, {Item: boba, Qty: 1}, {Item: acorn, Qty: 3}},
			world.Equipment{}, fill(199, acorn),
			bank.DepositInventoryCommand{Player: pid},
			[]world.Stack{{}, {Item: dagger, Qty: 1}, {}, {}}, world.Equipment{},
			append(withQty(fill(199, acorn), 0, 4), world.Stack{Item: boba, Qty: 2})},
		{"deposit equipment",
			[]world.Stack{{Item: boba, Qty: 1}}, world.Equipment{Weapon: stick, Body: hoodie}, []world.Stack{{Item: hoodie, Qty: 2}},
			bank.DepositEquipmentCommand{Player: pid},
			[]world.Stack{{Item: boba, Qty: 1}}, world.Equipment{},
			[]world.Stack{{Item: hoodie, Qty: 3}, {Item: stick, Qty: 1}}},
		{"deposit equipment into a full bank holding one of them",
			nil, world.Equipment{Weapon: stick, Body: hoodie}, append(fill(199, 0), world.Stack{Item: hoodie, Qty: 1}),
			bank.DepositEquipmentCommand{Player: pid},
			nil, world.Equipment{Weapon: stick},
			append(fill(199, 0), world.Stack{Item: hoodie, Qty: 2})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, booth)
			f.setInv(tt.inv...)
			f.p.Inv.Equipment = tt.equip
			f.p.Bank = world.Bank{Slots: tt.bnk}
			f.open()
			if got := f.do(tt.cmd, true); !reflect.DeepEqual(got, []bank.Result{bank.Ok}) {
				t.Fatalf("results %v, want [ok]", got)
			}
			wantInv := make([]world.Stack, len(f.p.Inv.Slots))
			copy(wantInv, tt.wantInv)
			if !reflect.DeepEqual(f.p.Inv.Slots, wantInv) {
				t.Errorf("inventory %+v, want %+v", f.p.Inv.Slots, wantInv)
			}
			if f.p.Inv.Equipment != tt.wantEq {
				t.Errorf("equipment %+v, want %+v", f.p.Inv.Equipment, tt.wantEq)
			}
			if !reflect.DeepEqual(f.p.Bank.Slots, tt.wantBank) {
				t.Errorf("bank %+v, want %+v", f.p.Bank.Slots, tt.wantBank)
			}
		})
	}
}

// fill returns n bank stacks of made-up distinct items (ids from 1000), each
// of quantity 1. A non-zero first replaces the first item.
func fill(n int, first gamedata.ItemID) []world.Stack {
	out := make([]world.Stack, n)
	for i := range out {
		out[i] = world.Stack{Item: gamedata.ItemID(1000 + i), Qty: 1}
	}
	if n > 0 && first != 0 {
		out[0].Item = first
	}
	return out
}

// withQty sets the quantity of stacks[i] and returns stacks.
func withQty(stacks []world.Stack, i, qty int) []world.Stack {
	stacks[i].Qty = qty
	return stacks
}

// TestRefusals checks the bank refuses a 201st distinct item and a
// withdrawal into a full inventory, among others, and changes nothing and
// commits nothing when it refuses.
func TestRefusals(t *testing.T) {
	f0 := newFixture(t, booth)
	boba, acorn, stick, hoodie := f0.item("boba"), f0.item("acorn"), f0.item("wooden_stick"), f0.item("hoodie")
	tests := []struct {
		name  string
		inv   []world.Stack
		equip world.Equipment
		bnk   []world.Stack
		cmd   world.Command
		want  bank.Result
	}{
		{"201st distinct item", []world.Stack{{Item: boba, Qty: 1}}, world.Equipment{}, fill(200, 0),
			bank.DepositCommand{Player: pid, Slot: 0, Quantity: bank.All}, bank.BankFull},
		{"deposit inventory into a full bank", []world.Stack{{Item: boba, Qty: 1}, {Item: acorn, Qty: 2}}, world.Equipment{}, fill(200, 0),
			bank.DepositInventoryCommand{Player: pid}, bank.BankFull},
		{"deposit equipment into a full bank", nil, world.Equipment{Weapon: stick, Body: hoodie}, fill(200, 0),
			bank.DepositEquipmentCommand{Player: pid}, bank.BankFull},
		{"withdraw into a full inventory", repeat(world.Stack{Item: hoodie, Qty: 1}, 28), world.Equipment{}, []world.Stack{{Item: boba, Qty: 5}},
			bank.WithdrawCommand{Player: pid, Slot: 0, Quantity: bank.One}, bank.InventoryFull},
		{"withdraw loot into a full inventory", repeat(world.Stack{Item: hoodie, Qty: 1}, 28), world.Equipment{}, []world.Stack{{Item: acorn, Qty: 5}},
			bank.WithdrawCommand{Player: pid, Slot: 0, Quantity: bank.All}, bank.InventoryFull},
		{"deposit from an empty slot", []world.Stack{{}, {Item: boba, Qty: 1}}, world.Equipment{}, nil,
			bank.DepositCommand{Player: pid, Slot: 0, Quantity: bank.All}, bank.NotHeld},
		{"deposit from no such slot", []world.Stack{{Item: boba, Qty: 1}}, world.Equipment{}, nil,
			bank.DepositCommand{Player: pid, Slot: 28, Quantity: bank.All}, bank.NotHeld},
		{"deposit zero", []world.Stack{{Item: boba, Qty: 1}}, world.Equipment{}, nil,
			bank.DepositCommand{Player: pid, Slot: 0, Quantity: 0}, bank.NotHeld},
		{"deposit a bad amount", []world.Stack{{Item: boba, Qty: 1}}, world.Equipment{}, nil,
			bank.DepositCommand{Player: pid, Slot: 0, Quantity: -2}, bank.NotHeld},
		{"withdraw from no such bank slot", nil, world.Equipment{}, []world.Stack{{Item: boba, Qty: 5}},
			bank.WithdrawCommand{Player: pid, Slot: 1, Quantity: bank.All}, bank.NotHeld},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, booth)
			f.setInv(tt.inv...)
			f.p.Inv.Equipment = tt.equip
			f.p.Bank = world.Bank{Slots: tt.bnk}
			beforeInv, beforeBank := f.p.Inv.Clone(), f.p.Bank.Clone()
			f.open()
			if got := f.run(tt.cmd); !reflect.DeepEqual(got, []bank.Result{tt.want}) {
				t.Fatalf("results %v, want [%v]", got, tt.want)
			}
			if !reflect.DeepEqual(f.p.Inv, beforeInv) || !reflect.DeepEqual(f.p.Bank, beforeBank) {
				t.Errorf("changed:\n inv  %+v\n bank %+v", f.p.Inv, f.p.Bank.Slots)
			}
			if len(f.commit.reqs) != 0 || len(f.p.PendingEconomy()) != 0 {
				t.Errorf("%d commits, %d outbox events, want none", len(f.commit.reqs), len(f.p.PendingEconomy()))
			}
		})
	}
}

// TestCommit checks each accepted request goes to the Committer as one
// request with both sides and the audit changes, that a second request while
// it is in flight is refused, that a successful commit leaves the world
// matching the request, and that a failed one leaves the inventory,
// equipment and bank as they were.
func TestCommit(t *testing.T) {
	f0 := newFixture(t, booth)
	boba, stick, hoodie := f0.item("boba"), f0.item("wooden_stick"), f0.item("hoodie")
	setup := func(t *testing.T) *fixture {
		f := newFixture(t, booth)
		f.setInv(world.Stack{Item: boba, Qty: 1}, world.Stack{Item: boba, Qty: 1})
		f.p.Inv.Equipment = world.Equipment{Weapon: stick, Body: hoodie}
		f.p.Bank = world.Bank{Slots: []world.Stack{{Item: boba, Qty: 4}}}
		f.open()
		return f
	}
	cmds := []world.Command{
		bank.DepositCommand{Player: pid, Slot: 0, Quantity: bank.All},
		bank.WithdrawCommand{Player: pid, Slot: 0, Quantity: bank.Five},
		bank.DepositInventoryCommand{Player: pid},
		bank.DepositEquipmentCommand{Player: pid},
	}
	for _, cmd := range cmds {
		for _, ok := range []bool{true, false} {
			t.Run(reflect.TypeOf(cmd).Name()+map[bool]string{true: "/saved", false: "/failed"}[ok], func(t *testing.T) {
				f := setup(t)
				beforeInv, beforeBank := f.p.Inv.Clone(), f.p.Bank.Clone()
				if got := f.run(cmd); len(got) != 0 {
					t.Fatalf("results before the commit resolved: %v", got)
				}
				if len(f.commit.reqs) != 1 {
					t.Fatalf("%d commits, want 1", len(f.commit.reqs))
				}
				req := f.commit.reqs[0]
				if req.Player != pid || len(req.Changes) == 0 {
					t.Errorf("request %+v", req)
				}
				if !f.p.Inv.Locked() || !f.sys.Committing(pid) {
					t.Error("inventory not locked while the commit is in flight")
				}
				// Nothing changes before the commit resolves, and every
				// further request is refused.
				for _, again := range cmds {
					if got := f.run(again); !reflect.DeepEqual(got, []bank.Result{bank.Busy}) {
						t.Errorf("second request %T: %v, want [busy]", again, got)
					}
				}
				if len(f.commit.reqs) != 1 {
					t.Errorf("%d commits after refused requests, want 1", len(f.commit.reqs))
				}
				if !reflect.DeepEqual(f.p.Bank, beforeBank) || !reflect.DeepEqual(f.p.Inv.Slots, beforeInv.Slots) || f.p.Inv.Equipment != beforeInv.Equipment {
					t.Error("player changed before the commit resolved")
				}
				// The committer's copy is its own.
				if len(req.Inv.Slots) > 0 {
					f.commit.reqs[0].Inv.Slots[0] = world.Stack{Item: 99, Qty: 99}
				}

				f.sys.Resolve(req.ID, ok)
				got := f.run()
				if f.p.Inv.Locked() || f.sys.Committing(pid) {
					t.Error("still locked after the commit resolved")
				}
				if !ok {
					if !reflect.DeepEqual(got, []bank.Result{bank.SaveFailed}) {
						t.Errorf("results %v, want [save_failed]", got)
					}
					if !reflect.DeepEqual(f.p.Inv, beforeInv) || !reflect.DeepEqual(f.p.Bank, beforeBank) {
						t.Errorf("failed commit changed the player:\n inv  %+v\n bank %+v", f.p.Inv, f.p.Bank.Slots)
					}
					if n := len(f.p.PendingEconomy()); n != 0 {
						t.Errorf("%d outbox events, want 0", n)
					}
					return
				}
				if !reflect.DeepEqual(got, []bank.Result{bank.Ok}) {
					t.Errorf("results %v, want [ok]", got)
				}
				if !slices.Equal(f.p.Bank.Slots, req.Bank.Slots) {
					t.Errorf("bank %+v, want the request's %+v", f.p.Bank.Slots, req.Bank.Slots)
				}
				if !reflect.DeepEqual(f.p.Inv.Equipment, req.Inv.Equipment) {
					t.Errorf("equipment %+v, want the request's %+v", f.p.Inv.Equipment, req.Inv.Equipment)
				}
				ev := f.p.DrainEconomy()
				if len(ev) != 1 || ev[0].Reason != req.Reason || !reflect.DeepEqual(ev[0].Changes, req.Changes) {
					t.Errorf("outbox %+v, want one event with the request's changes", ev)
				}
				// Every item moved is accounted for: the totals across
				// inventory, equipment and bank are unchanged.
				if got, want := total(f.p), total(&world.Player{Inv: beforeInv, Bank: beforeBank}); !reflect.DeepEqual(got, want) {
					t.Errorf("item totals changed: %v, want %v", got, want)
				}
				if n := len(f.sink.states); n != 2 {
					t.Errorf("%d bank states sent, want 2 (open and after the commit)", n)
				}
			})
		}
	}
}

// TestCommitMatchesRequest checks the inventory applied after a successful
// commit is exactly the one the Committer was given.
func TestCommitMatchesRequest(t *testing.T) {
	f := newFixture(t, booth)
	boba := f.item("boba")
	f.setInv(world.Stack{Item: boba, Qty: 1}, world.Stack{Item: boba, Qty: 1}, world.Stack{Item: boba, Qty: 1})
	f.open()
	f.run(bank.DepositCommand{Player: pid, Slot: 2, Quantity: 2})
	req := f.commit.reqs[0]
	want := req.Inv.Clone()
	f.sys.Resolve(req.ID, true)
	f.run()
	if !reflect.DeepEqual(f.p.Inv, want) {
		t.Errorf("inventory %+v, want the request's %+v", f.p.Inv, want)
	}
}

// TestCommitPlayerLeft checks a commit resolving after its player left the
// world is dropped, and that unknown and repeated ids are ignored.
func TestCommitPlayerLeft(t *testing.T) {
	f := newFixture(t, booth)
	f.setInv(world.Stack{Item: f.item("boba"), Qty: 1})
	f.open()
	f.run(bank.DepositCommand{Player: pid, Slot: 0, Quantity: bank.All})
	id := f.commit.reqs[0].ID
	f.w.RemovePlayer(pid)
	f.sys.Resolve(id, true)
	f.sys.Resolve(id, true)
	f.sys.Resolve(id+7, true)
	if got := f.run(); len(got) != 0 {
		t.Errorf("results %v, want none", got)
	}
	if f.sys.Committing(pid) || f.sys.IsOpen(pid) {
		t.Error("player still committing or open after leaving")
	}
}

// total counts every item a player holds across inventory, equipment and
// bank.
func total(p *world.Player) map[gamedata.ItemID]int {
	out := map[gamedata.ItemID]int{}
	for _, s := range p.Inv.Slots {
		if !s.Empty() {
			out[s.Item] += s.Qty
		}
	}
	for _, it := range []gamedata.ItemID{p.Inv.Equipment.Weapon, p.Inv.Equipment.Head, p.Inv.Equipment.Body, p.Inv.Equipment.Shield} {
		if it != 0 {
			out[it]++
		}
	}
	for _, s := range p.Bank.Slots {
		out[s.Item] += s.Qty
	}
	return out
}

// BenchmarkTick runs every tick, checking each open bank's player.
func BenchmarkTick(b *testing.B) {
	d, err := gamedata.Default()
	if err != nil {
		b.Fatal(err)
	}
	l, err := areas.Load()
	if err != nil {
		b.Fatal(err)
	}
	sink := &recSink{}
	sys := bank.New(d, l, booth, tileSize, world.NewFakeClock(start), sink, &recCommitter{})
	w := world.New(world.DefaultConfig())
	if err := w.Register(sys); err != nil {
		b.Fatal(err)
	}
	for i := 1; i <= 200; i++ {
		if err := w.AddPlayer(&world.Player{ID: world.PlayerID(i), Pos: booth, Inv: world.NewInventory(d)}); err != nil {
			b.Fatal(err)
		}
		w.Enqueue(bank.OpenCommand{Player: world.PlayerID(i)})
	}
	w.Tick()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sys.Tick(w, 0)
	}
}
