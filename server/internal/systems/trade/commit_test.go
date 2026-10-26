package trade_test

import (
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/world"
)

// holdings is what an inventory holds, without its lock.
type holdings struct {
	Slots     []world.Stack
	Equipment world.Equipment
	Gold      int
}

func held(inv world.Inventory) holdings {
	return holdings{append([]world.Stack(nil), inv.Slots...), inv.Equipment, inv.Gold}
}

// setInv replaces id's inventory with one holding stacks from slot 0 on, no
// equipment and gold.
func (f *fixture) setInv(id world.PlayerID, gold int, stacks ...world.Stack) {
	f.t.Helper()
	inv := world.NewInventory(f.d)
	if len(stacks) > len(inv.Slots) {
		f.t.Fatalf("%d stacks for %d slots", len(stacks), len(inv.Slots))
	}
	copy(inv.Slots, stacks)
	inv.Gold = gold
	f.p[id].Inv = inv
}

// agree opens a trade between a and b, puts offA and offB on it (by slot),
// and accepts everything except a's accept of screen 2.
func (f *fixture) agree(offA, offB trade.Offer) {
	f.t.Helper()
	f.open()
	for _, s := range []struct {
		id world.PlayerID
		o  trade.Offer
	}{{a, offA}, {b, offB}} {
		for _, it := range s.o.Items {
			f.run(trade.OfferCommand{Player: s.id, Slot: it.Slot, Quantity: it.Qty})
		}
		if s.o.Gold > 0 {
			f.run(trade.SetGoldCommand{Player: s.id, Gold: s.o.Gold})
		}
	}
	f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer})
	f.run(trade.AcceptCommand{Player: b, Screen: trade.ScreenOffer})
	f.run(trade.AcceptCommand{Player: b, Screen: trade.ScreenConfirm})
}

// accept sends a's accept of screen 2 and returns the events sent.
func (f *fixture) accept() []event {
	return f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenConfirm})
}

func repeat(s world.Stack, n int) []world.Stack {
	out := make([]world.Stack, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// TestCommitRecheck checks that accepting screen 2 re-checks both
// inventories: an offer no longer held gives both players OfferNotHeld, no
// room for what a side receives (28 slots, only loot stacks) gives both
// InventoryFull, and in both cases nothing is committed or locked and the
// trade closes. Trades that do hold are committed.
func TestCommitRecheck(t *testing.T) {
	f0 := newFixture(t)
	boba, acorn, dagger := f0.itemID("boba"), f0.itemID("acorn"), f0.itemID("bronze_dagger")
	one := func(item gamedata.ItemID) world.Stack { return world.Stack{Item: item, Qty: 1} }

	tests := []struct {
		name         string
		goldA, goldB int
		invA, invB   []world.Stack
		offA, offB   trade.Offer
		act          func(f *fixture) // between the offers and a's last accept
		want         trade.Result     // ignored if commits
		commits      bool
	}{
		{
			name: "a no longer holds the item",
			invA: []world.Stack{one(boba)},
			offA: trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}}},
			act:  func(f *fixture) { f.p[a].Inv.Slots[0] = world.Stack{} },
			want: trade.OfferNotHeld,
		},
		{
			name: "a holds fewer than offered",
			invA: []world.Stack{{Item: acorn, Qty: 5}},
			offA: trade.Offer{Items: []trade.Item{{Slot: 0, Item: acorn, Qty: 5}}},
			act:  func(f *fixture) { f.p[a].Inv.Slots[0].Qty = 3 },
			want: trade.OfferNotHeld,
		},
		{
			name: "a offers two of one item, holds one",
			invA: []world.Stack{one(boba), one(boba)},
			offA: trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}, {Slot: 1, Item: boba, Qty: 1}}},
			act:  func(f *fixture) { f.p[a].Inv.Slots[1] = world.Stack{} },
			want: trade.OfferNotHeld,
		},
		{
			name:  "b no longer has the gold",
			goldB: 50,
			offB:  trade.Offer{Gold: 50},
			act:   func(f *fixture) { f.p[b].Inv.Gold = 49 },
			want:  trade.OfferNotHeld,
		},
		{
			name:  "not held beats full",
			invA:  repeat(one(dagger), 28),
			goldB: 10,
			invB:  []world.Stack{one(boba)},
			offB:  trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}}, Gold: 10},
			act:   func(f *fixture) { f.p[b].Inv.Gold = 0 },
			want:  trade.OfferNotHeld,
		},
		{
			name: "a is full and gets an unstackable item",
			invA: repeat(one(dagger), 28),
			invB: []world.Stack{one(boba)},
			offB: trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}}},
			want: trade.InventoryFull,
		},
		{
			name: "b is full and gets loot it has no stack of",
			invA: []world.Stack{{Item: acorn, Qty: 3}},
			offA: trade.Offer{Items: []trade.Item{{Slot: 0, Item: acorn, Qty: 3}}},
			invB: repeat(one(boba), 28),
			want: trade.InventoryFull,
		},
		{
			name: "a has one free slot and gets two unstackable items",
			invA: repeat(one(dagger), 27),
			invB: []world.Stack{one(boba), one(boba)},
			offB: trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}, {Slot: 1, Item: boba, Qty: 1}}},
			want: trade.InventoryFull,
		},
		{
			name: "a fills up after the offers",
			invB: []world.Stack{one(boba)},
			offB: trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}}},
			act: func(f *fixture) {
				for i := range f.p[a].Inv.Slots {
					f.p[a].Inv.Slots[i] = one(dagger)
				}
			},
			want: trade.InventoryFull,
		},
		{
			name:    "b is full but stacks the loot",
			invA:    []world.Stack{{Item: acorn, Qty: 3}},
			offA:    trade.Offer{Items: []trade.Item{{Slot: 0, Item: acorn, Qty: 3}}},
			invB:    append(repeat(one(boba), 27), world.Stack{Item: acorn, Qty: 1}),
			commits: true,
		},
		{
			name:    "a is full but frees a slot",
			invA:    repeat(one(dagger), 28),
			offA:    trade.Offer{Items: []trade.Item{{Slot: 4, Item: dagger, Qty: 1}}},
			invB:    []world.Stack{one(boba)},
			offB:    trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}}},
			commits: true,
		},
		{
			name:    "item moved to another slot is still held",
			invA:    []world.Stack{one(boba)},
			offA:    trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}}},
			act:     func(f *fixture) { f.p[a].Inv.Slots[0], f.p[a].Inv.Slots[9] = world.Stack{}, f.p[a].Inv.Slots[0] },
			commits: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.setInv(a, tt.goldA, tt.invA...)
			f.setInv(b, tt.goldB, tt.invB...)
			f.agree(tt.offA, tt.offB)
			f.sink.take()
			if tt.act != nil {
				tt.act(f)
			}
			beforeA, beforeB := held(f.p[a].Inv), held(f.p[b].Inv)
			got := f.accept()
			if f.sys.Trading(a) || f.sys.Trading(b) {
				t.Error("trade still open")
			}
			if tt.commits {
				check(t, got, nil)
				if len(f.commit.got) != 1 {
					t.Fatalf("commits = %d, want 1", len(f.commit.got))
				}
				return
			}
			check(t, got, results(a, b, tt.want))
			if len(f.commit.got) != 0 {
				t.Errorf("commits = %+v, want none", f.commit.got)
			}
			if f.sys.Committing(a) || f.sys.Committing(b) || f.p[a].Inv.Locked() || f.p[b].Inv.Locked() {
				t.Error("players left committing or locked")
			}
			if !reflect.DeepEqual(held(f.p[a].Inv), beforeA) || !reflect.DeepEqual(held(f.p[b].Inv), beforeB) {
				t.Error("inventories changed")
			}
			if len(f.p[a].PendingEconomy())+len(f.p[b].PendingEconomy()) != 0 {
				t.Error("economy events recorded")
			}
		})
	}
}

// commitFixture sets up the trade TestCommitRequest checks and returns it
// with the commit request sent, both inventories still unchanged.
func commitFixture(t *testing.T) (*fixture, trade.CommitRequest, holdings, holdings) {
	t.Helper()
	f := newFixture(t)
	boba, acorn, dagger, pelt := f.itemID("boba"), f.itemID("acorn"), f.itemID("bronze_dagger"), f.itemID("coyote_pelt")
	f.setInv(a, 100, world.Stack{Item: boba, Qty: 1}, world.Stack{Item: boba, Qty: 1}, world.Stack{Item: acorn, Qty: 10})
	f.setInv(b, 30, world.Stack{Item: dagger, Qty: 1}, world.Stack{Item: acorn, Qty: 4}, world.Stack{Item: pelt, Qty: 2})
	offA := trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}, {Slot: 1, Item: boba, Qty: 1}, {Slot: 2, Item: acorn, Qty: 6}}, Gold: 40}
	offB := trade.Offer{Items: []trade.Item{{Slot: 0, Item: dagger, Qty: 1}, {Slot: 1, Item: acorn, Qty: 4}}, Gold: 30}
	f.agree(offA, offB)
	f.sink.take()
	beforeA, beforeB := held(f.p[a].Inv), held(f.p[b].Inv)
	check(t, f.accept(), nil)
	if len(f.commit.got) != 1 {
		t.Fatalf("commits = %d, want 1", len(f.commit.got))
	}
	return f, f.commit.got[0], beforeA, beforeB
}

// TestCommitRequest checks that a valid trade hands the Committer both
// resulting inventories and each side's item and gold changes, and that
// both in-world inventories stay unchanged until the result arrives.
func TestCommitRequest(t *testing.T) {
	f, req, beforeA, beforeB := commitFixture(t)
	boba, acorn, dagger, pelt := f.itemID("boba"), f.itemID("acorn"), f.itemID("bronze_dagger"), f.itemID("coyote_pelt")

	slots := func(stacks ...world.Stack) []world.Stack {
		s := make([]world.Stack, 28)
		copy(s, stacks)
		return s
	}
	wantA := trade.Side{
		Player: a,
		Gave:   trade.Offer{Items: []trade.Item{{Slot: 0, Item: boba, Qty: 1}, {Slot: 1, Item: boba, Qty: 1}, {Slot: 2, Item: acorn, Qty: 6}}, Gold: 40},
		Changes: []world.Change{
			{Kind: world.ChangeRemove, Item: boba, Qty: 2},
			{Kind: world.ChangeRemove, Item: acorn, Qty: 6},
			{Kind: world.ChangeGold, Gold: -40},
			{Kind: world.ChangeAdd, Item: dagger, Qty: 1},
			{Kind: world.ChangeAdd, Item: acorn, Qty: 4},
			{Kind: world.ChangeGold, Gold: 30},
		},
	}
	wantInvA := holdings{Slots: slots(world.Stack{Item: dagger, Qty: 1}, world.Stack{}, world.Stack{Item: acorn, Qty: 8}), Gold: 90}
	wantB := trade.Side{
		Player: b,
		Gave:   trade.Offer{Items: []trade.Item{{Slot: 0, Item: dagger, Qty: 1}, {Slot: 1, Item: acorn, Qty: 4}}, Gold: 30},
		Changes: []world.Change{
			{Kind: world.ChangeRemove, Item: dagger, Qty: 1},
			{Kind: world.ChangeRemove, Item: acorn, Qty: 4},
			{Kind: world.ChangeGold, Gold: -30},
			{Kind: world.ChangeAdd, Item: boba, Qty: 2},
			{Kind: world.ChangeAdd, Item: acorn, Qty: 6},
			{Kind: world.ChangeGold, Gold: 40},
		},
	}
	wantInvB := holdings{Slots: slots(world.Stack{Item: boba, Qty: 1}, world.Stack{Item: boba, Qty: 1}, world.Stack{Item: pelt, Qty: 2}, world.Stack{Item: acorn, Qty: 6}), Gold: 40}

	if req.ID == 0 {
		t.Error("commit id is 0")
	}
	for _, s := range []struct {
		name    string
		got     trade.Side
		want    trade.Side
		wantInv holdings
	}{{"a", req.A, wantA, wantInvA}, {"b", req.B, wantB, wantInvB}} {
		if s.got.Player != s.want.Player || !reflect.DeepEqual(s.got.Gave, s.want.Gave) {
			t.Errorf("%s: player %d gave %+v, want %d gave %+v", s.name, s.got.Player, s.got.Gave, s.want.Player, s.want.Gave)
		}
		if !reflect.DeepEqual(s.got.Changes, s.want.Changes) {
			t.Errorf("%s: changes\n got %+v\nwant %+v", s.name, s.got.Changes, s.want.Changes)
		}
		if got := held(s.got.Inv); !reflect.DeepEqual(got, s.wantInv) {
			t.Errorf("%s: resulting inventory\n got %+v\nwant %+v", s.name, got, s.wantInv)
		}
		if s.got.Inv.Locked() {
			t.Errorf("%s: resulting inventory is locked", s.name)
		}
	}

	// Nothing changes in the world until the result arrives.
	for range 3 {
		check(t, f.run(), nil)
		if !reflect.DeepEqual(held(f.p[a].Inv), beforeA) || !reflect.DeepEqual(held(f.p[b].Inv), beforeB) {
			t.Fatal("in-world inventories changed before the result")
		}
		if len(f.p[a].PendingEconomy())+len(f.p[b].PendingEconomy()) != 0 {
			t.Fatal("economy events recorded before the result")
		}
	}
}

type nopShopSink struct{}

func (nopShopSink) ShopResult(world.PlayerID, shop.Result) {}

// TestLockedWhileCommitting checks that while a commit is in flight both
// inventories refuse every change, so the item and shop systems change
// nothing, and every trade command from either player is ignored.
func TestLockedWhileCommitting(t *testing.T) {
	f, _, beforeA, beforeB := commitFixture(t)
	acorn := f.itemID("acorn")
	if !f.sys.Committing(a) || !f.sys.Committing(b) {
		t.Fatal("players not committing")
	}

	ops := []struct {
		name string
		op   func(inv *world.Inventory) ([]world.Change, error)
	}{
		{"Add", func(inv *world.Inventory) ([]world.Change, error) { return inv.Add(f.d, acorn, 1) }},
		{"Remove", func(inv *world.Inventory) ([]world.Change, error) { return inv.Remove(acorn, 1) }},
		{"RemoveAt", func(inv *world.Inventory) ([]world.Change, error) { return inv.RemoveAt(2) }},
		{"RemoveFromSlot", func(inv *world.Inventory) ([]world.Change, error) { return inv.RemoveFromSlot(1, 1) }},
		{"AddGold", func(inv *world.Inventory) ([]world.Change, error) { return inv.AddGold(1) }},
		{"SpendGold", func(inv *world.Inventory) ([]world.Change, error) { return inv.SpendGold(1) }},
		{"Equip", func(inv *world.Inventory) ([]world.Change, error) { return inv.Equip(f.d, 0, 99) }},
		{"Unequip", func(inv *world.Inventory) ([]world.Change, error) { return inv.Unequip(gamedata.SlotWeapon) }},
	}
	for _, id := range []world.PlayerID{a, b} {
		p := f.p[id]
		if !p.Inv.Locked() {
			t.Errorf("player %d not locked", id)
		}
		for _, o := range ops {
			if changes, err := o.op(&p.Inv); !errors.Is(err, world.ErrInventoryLocked) || changes != nil {
				t.Errorf("player %d %s: changes %v, err %v; want ErrInventoryLocked", id, o.name, changes, err)
			}
		}
	}

	// The item and shop systems can't change them either.
	if err := f.w.Register(items.New(f.d)); err != nil {
		t.Fatal(err)
	}
	if err := f.w.Register(shop.New(f.d, f.l, tileSize, nopShopSink{})); err != nil {
		t.Fatal(err)
	}
	stall := f.l.Shop()
	for _, id := range []world.PlayerID{a, b} {
		f.p[id].Pos = stall
		f.p[id].HP = 1
		f.run(
			items.EquipCommand{Player: id, Slot: 0},
			items.UnequipCommand{Player: id, Slot: gamedata.SlotWeapon},
			items.EatCommand{Player: id, Slot: 0},
			shop.BuyCommand{Player: id, Item: f.itemID("boba"), Quantity: 1},
			shop.SellCommand{Player: id, Slot: 0, Quantity: 1},
			shop.SellCommand{Player: id, Slot: 2, Quantity: 1},
		)
	}
	if !reflect.DeepEqual(held(f.p[a].Inv), beforeA) || !reflect.DeepEqual(held(f.p[b].Inv), beforeB) {
		t.Error("item or shop system changed a locked inventory")
	}
	if len(f.p[a].PendingEconomy())+len(f.p[b].PendingEconomy()) != 0 {
		t.Error("economy events recorded")
	}

	// Trade commands from either player are ignored; requests to them are
	// refused as busy.
	stall2 := stall
	stall2.X++
	f.p[c].Pos = stall2
	for _, id := range []world.PlayerID{a, b} {
		got := f.run(
			trade.RequestCommand{Player: id, Partner: c},
			trade.OfferCommand{Player: id, Slot: 0, Quantity: 1},
			trade.RemoveCommand{Player: id, Index: 0},
			trade.SetGoldCommand{Player: id, Gold: 1},
			trade.AcceptCommand{Player: id, Screen: trade.ScreenOffer},
			trade.AcceptCommand{Player: id, Screen: trade.ScreenConfirm},
			trade.CancelCommand{Player: id},
		)
		check(t, got, nil)
		check(t, f.run(trade.RequestCommand{Player: c, Partner: id}), []event{{kind: "result", to: c, r: trade.PartnerBusy}})
	}
	if f.sys.Trading(a) || f.sys.Trading(b) || f.sys.Trading(c) {
		t.Error("a trade opened")
	}
	if !f.sys.Committing(a) || !f.sys.Committing(b) || len(f.commit.got) != 1 {
		t.Error("commit state changed")
	}
}

// TestResolve checks how a commit result is applied: success swaps both
// inventories, records an economy event for each player, unlocks both and
// sends Completed; failure only unlocks both and sends Cancelled; a player
// who left the world in the meantime is skipped.
func TestResolve(t *testing.T) {
	tests := []struct {
		name  string
		ok    bool
		leave world.PlayerID // 0: nobody leaves
		back  bool           // the leaver rejoins as a new entity
	}{
		{name: "committed", ok: true},
		{name: "failed", ok: false},
		{name: "committed, b left", ok: true, leave: b},
		{name: "committed, a left", ok: true, leave: a},
		{name: "failed, a left", ok: false, leave: a},
		{name: "committed, b left and came back", ok: true, leave: b, back: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, req, beforeA, beforeB := commitFixture(t)
			if tt.leave != 0 {
				f.w.RemovePlayer(tt.leave)
				if tt.back {
					inv, _, err := world.NewStartingInventory(f.d)
					if err != nil {
						t.Fatal(err)
					}
					if err := f.w.AddPlayer(&world.Player{ID: tt.leave, Pos: f.p[tt.leave].Pos, Inv: inv}); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.sys.Resolve(req.ID, tt.ok)
			got := f.run()

			r := trade.Cancelled
			if tt.ok {
				r = trade.Completed
			}
			var want []event
			for _, s := range []struct {
				side   trade.Side
				before holdings
			}{{req.A, beforeA}, {req.B, beforeB}} {
				id := s.side.Player
				p := f.p[id]
				if f.sys.Committing(id) || p.Inv.Locked() {
					t.Errorf("player %d still committing or locked", id)
				}
				if id == tt.leave {
					if q := f.w.Player(id); q != nil && (!reflect.DeepEqual(q.Inv.Slots, repeatStarting(t, f)) || len(q.PendingEconomy()) != 0) {
						t.Errorf("rejoined player %d got the trade", id)
					}
					if !reflect.DeepEqual(held(p.Inv), s.before) || len(p.PendingEconomy()) != 0 {
						t.Errorf("departed player %d changed", id)
					}
					continue
				}
				want = append(want, event{kind: "result", to: id, r: r})
				if !tt.ok {
					if !reflect.DeepEqual(held(p.Inv), s.before) || len(p.PendingEconomy()) != 0 {
						t.Errorf("player %d changed by a failed commit", id)
					}
					continue
				}
				if !reflect.DeepEqual(held(p.Inv), held(s.side.Inv)) {
					t.Errorf("player %d inventory\n got %+v\nwant %+v", id, held(p.Inv), held(s.side.Inv))
				}
				ev := p.DrainEconomy()
				if len(ev) != 1 || ev[0].Reason != trade.ReasonTrade || !reflect.DeepEqual(ev[0].Changes, s.side.Changes) || !reflect.DeepEqual(held(ev[0].Inv), held(s.side.Inv)) {
					t.Errorf("player %d economy events = %+v", id, ev)
				}
			}
			check(t, got, want)

			// A repeated or unknown result does nothing.
			f.sys.Resolve(req.ID, !tt.ok)
			f.sys.Resolve(req.ID+100, true)
			check(t, f.run(), nil)

			// Players who stayed can trade again.
			if tt.leave == 0 {
				f.open()
			}
		})
	}
}

func repeatStarting(t *testing.T, f *fixture) []world.Stack {
	t.Helper()
	inv, _, err := world.NewStartingInventory(f.d)
	if err != nil {
		t.Fatal(err)
	}
	return inv.Slots
}

// totals returns how many of each item (slots and equipment) and how much
// gold a and b hold together.
func totals(f *fixture) (map[gamedata.ItemID]int, int) {
	items := map[gamedata.ItemID]int{}
	gold := 0
	for _, id := range []world.PlayerID{a, b} {
		inv := &f.p[id].Inv
		for _, s := range inv.Slots {
			if !s.Empty() {
				items[s.Item] += s.Qty
			}
		}
		for _, e := range []gamedata.Slot{gamedata.SlotWeapon, gamedata.SlotHead, gamedata.SlotBody, gamedata.SlotShield} {
			if it := inv.Equipment.Get(e); it != 0 {
				items[it]++
			}
		}
		gold += inv.Gold
	}
	return items, gold
}

// move moves up to qty items from slot i of from's inventory to to's,
// keeping both players' totals, if to has room.
func move(f *fixture, from, to world.PlayerID, i, qty int) {
	src := &f.p[from].Inv
	s := src.Slots[i]
	if s.Empty() {
		return
	}
	qty = min(qty, s.Qty)
	dst := f.p[to].Inv.Clone()
	if _, err := dst.Add(f.d, s.Item, qty); err != nil {
		return
	}
	if _, err := src.RemoveFromSlot(i, qty); err != nil {
		f.t.Fatal(err)
	}
	f.p[to].Inv = dst
}

// TestRandomTradesKeepTotals runs many random trades between a and b,
// including failed commits, offers no longer held and full inventories, and
// checks after every tick that the total of every item and of gold across
// both players never changes.
func TestRandomTradesKeepTotals(t *testing.T) {
	f := newFixture(t)
	rng := rand.New(rand.NewPCG(184, 1))
	unstackable := []gamedata.ItemID{f.itemID("boba"), f.itemID("burrito"), f.itemID("bronze_dagger"), f.itemID("hoodie"), f.itemID("leather_cap")}
	loot := []gamedata.ItemID{f.itemID("acorn"), f.itemID("shiny_trinket"), f.itemID("coyote_pelt"), f.itemID("scrap_metal")}
	fill := func(n int) []world.Stack {
		var out []world.Stack
		for range n {
			out = append(out, world.Stack{Item: unstackable[rng.IntN(len(unstackable))], Qty: 1})
		}
		for _, l := range loot[:2] {
			out = append(out, world.Stack{Item: l, Qty: 1 + rng.IntN(50)})
		}
		return out
	}
	f.setInv(a, 1000, fill(24)...)
	f.setInv(b, 500, fill(20)...)
	f.p[b].Inv.Slots[27] = world.Stack{Item: loot[2], Qty: 7}
	f.p[a].Inv.Slots[27] = world.Stack{Item: loot[3], Qty: 3}
	wantItems, wantGold := totals(f)

	verify := func(when string) {
		t.Helper()
		items, gold := totals(f)
		if !reflect.DeepEqual(items, wantItems) || gold != wantGold {
			t.Fatalf("%s: totals %v / %d gold, want %v / %d gold", when, items, gold, wantItems, wantGold)
		}
	}

	count := map[trade.Result]int{}
	for n := range 2000 {
		f.open()
		for _, id := range []world.PlayerID{a, b} {
			inv := &f.p[id].Inv
			for range rng.IntN(5) {
				i := rng.IntN(len(inv.Slots))
				if s := inv.Slots[i]; !s.Empty() {
					f.run(trade.OfferCommand{Player: id, Slot: i, Quantity: 1 + rng.IntN(s.Qty)})
				}
			}
			if rng.IntN(2) == 0 && inv.Gold > 0 {
				f.run(trade.SetGoldCommand{Player: id, Gold: rng.IntN(inv.Gold + 1)})
			}
		}
		f.run(trade.AcceptCommand{Player: a, Screen: trade.ScreenOffer})
		f.run(trade.AcceptCommand{Player: b, Screen: trade.ScreenOffer})
		f.run(trade.AcceptCommand{Player: b, Screen: trade.ScreenConfirm})
		verify("before commit")
		// Sometimes move holdings between them after the offers, as other
		// systems could, so an offer may no longer be held.
		if rng.IntN(4) == 0 {
			from, to := a, b
			if rng.IntN(2) == 0 {
				from, to = b, a
			}
			for range 1 + rng.IntN(4) {
				move(f, from, to, rng.IntN(28), 1+rng.IntN(20))
			}
			if g := rng.IntN(f.p[from].Inv.Gold + 1); rng.IntN(2) == 0 && g > 0 {
				f.p[from].Inv.Gold -= g
				f.p[to].Inv.Gold += g
			}
		}
		f.sink.take()
		commits := len(f.commit.got)
		got := f.accept()
		verify("after accept")
		if len(f.commit.got) == commits {
			if len(got) != 2 {
				t.Fatalf("trade %d: no commit and events %+v", n, got)
			}
			count[got[0].r]++
			continue
		}
		req := f.commit.got[len(f.commit.got)-1]
		// Commands during the commit change nothing.
		for range rng.IntN(3) {
			f.run(trade.CancelCommand{Player: a}, trade.OfferCommand{Player: b, Slot: rng.IntN(28), Quantity: 1})
			verify("while committing")
		}
		ok := rng.IntN(3) != 0
		f.sys.Resolve(req.ID, ok)
		got = f.run()
		verify("after result")
		if len(got) != 2 {
			t.Fatalf("trade %d: result events %+v", n, got)
		}
		count[got[0].r]++
	}
	for _, r := range []trade.Result{trade.Completed, trade.Cancelled, trade.OfferNotHeld, trade.InventoryFull} {
		if count[r] == 0 {
			t.Errorf("no trade ended %v (counts %v)", r, count)
		}
	}
	t.Logf("results: %v", count)
}

// TestResolveConcurrent checks that Resolve may be called from other
// goroutines while the world ticks (run with -race).
func TestResolveConcurrent(t *testing.T) {
	f, req, _, _ := commitFixture(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 100 {
			f.sys.Resolve(req.ID+trade.CommitID(i), true)
		}
	}()
	for range 50 {
		f.run()
	}
	<-done
	f.run()
	if f.sys.Committing(a) || f.sys.Committing(b) {
		t.Error("result never applied")
	}
}
