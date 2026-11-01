package shop_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// tileSize is the collision grid's cell size in metres (shared/map/config.mjs
// CELL_SIZE).
const tileSize = 1.0

func gameData(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func lookup(t testing.TB) *areas.Lookup {
	t.Helper()
	l, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func itemID(t testing.TB, d *gamedata.Data, key string) gamedata.ItemID {
	t.Helper()
	it, ok := d.ItemByKey(key)
	if !ok {
		t.Fatalf("no item %q", key)
	}
	return it.ID
}

type result struct {
	to world.PlayerID
	r  shop.Result
}

type recSink struct{ got []result }

func (s *recSink) ShopResult(to world.PlayerID, r shop.Result) { s.got = append(s.got, result{to, r}) }

// newPlayer returns a player standing on the stall with the starting kit:
// 100 gold, Boba in slots 0-2, stick and hoodie worn.
func newPlayer(t testing.TB, d *gamedata.Data, l *areas.Lookup) *world.Player {
	t.Helper()
	inv, _, err := world.NewStartingInventory(d)
	if err != nil {
		t.Fatal(err)
	}
	return &world.Player{ID: 7, Pos: l.Shop(), Inv: inv}
}

// handle runs cmd through a new shop system on a one-player world tick and
// returns the results the sink received.
func handle(t testing.TB, d *gamedata.Data, l *areas.Lookup, p *world.Player, cmd world.Command) []result {
	t.Helper()
	sink := &recSink{}
	w := world.New(world.DefaultConfig())
	if err := w.Register(shop.New(d, l, tileSize, sink)); err != nil {
		t.Fatal(err)
	}
	if err := w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	w.Enqueue(cmd)
	w.Tick()
	return sink.got
}

// checkUnchanged fails if p's inventory differs from before or an outbox
// event was recorded.
func checkUnchanged(t *testing.T, p *world.Player, before world.Inventory) {
	t.Helper()
	if !reflect.DeepEqual(p.Inv, before) {
		t.Errorf("inventory changed:\n got  %+v\n want %+v", p.Inv, before)
	}
	if ev := p.DrainEconomy(); len(ev) != 0 {
		t.Errorf("outbox has %d events, want 0: %+v", len(ev), ev)
	}
}

func checkResult(t *testing.T, got []result, want shop.Result) {
	t.Helper()
	if len(got) != 1 || got[0] != (result{7, want}) {
		t.Errorf("sink got %+v, want one %v for player 7", got, want)
	}
}

func TestReach(t *testing.T) {
	d, l := gameData(t), lookup(t)
	burrito := itemID(t, d, "burrito")
	reach := float32(float64(d.Rules.ShopDistanceTiles) * tileSize)
	stall := l.Shop()
	tests := []struct {
		name   string
		offset world.Vec2
		want   shop.Result
	}{
		{"on the stall", world.Vec2{}, shop.Ok},
		{"at the distance east", world.Vec2{X: reach}, shop.Ok},
		{"at the distance north", world.Vec2{Y: -reach}, shop.Ok},
		{"just beyond east", world.Vec2{X: reach + 0.01}, shop.TooFar},
		{"just beyond diagonally", world.Vec2{X: reach * 0.75, Y: reach * 0.75}, shop.TooFar},
		{"far away", world.Vec2{X: 500, Y: -300}, shop.TooFar},
	}
	for _, tt := range tests {
		for _, cmd := range []world.Command{
			shop.BuyCommand{Player: 7, Item: burrito, Quantity: 1},
			shop.SellCommand{Player: 7, Slot: 0, Quantity: 1},
		} {
			t.Run(tt.name+"/"+reflect.TypeOf(cmd).Name(), func(t *testing.T) {
				p := newPlayer(t, d, l)
				p.Pos = world.Vec2{X: stall.X + tt.offset.X, Y: stall.Y + tt.offset.Y}
				before := p.Inv.Clone()
				got := handle(t, d, l, p, cmd)
				checkResult(t, got, tt.want)
				if tt.want == shop.TooFar {
					checkUnchanged(t, p, before)
				} else if n := len(p.DrainEconomy()); n != 1 {
					t.Errorf("outbox has %d events, want 1", n)
				}
			})
		}
	}
}

func TestBuy(t *testing.T) {
	d, l := gameData(t), lookup(t)
	burrito, boba, dagger := itemID(t, d, "burrito"), itemID(t, d, "boba"), itemID(t, d, "bronze_dagger")
	helmet := itemID(t, d, "bike_helmet")
	price := func(id gamedata.ItemID) int {
		p, ok := d.ShopSellPrice(id)
		if !ok {
			t.Fatalf("shop doesn't sell %d", id)
		}
		return p
	}
	tests := []struct {
		name      string
		gold      int
		item      gamedata.ItemID
		qty       int
		wantSlots []int // slots that receive one item each
	}{
		{"one burrito", 100, burrito, 1, []int{3}},
		{"three burritos take a slot each", 100, burrito, 3, []int{3, 4, 5}},
		{"spend every coin", 2 * price(dagger), dagger, 2, []int{3, 4}},
		{"fill the inventory", 1000, boba, 25, seq(3, 28)},
		{"boba takes a free slot, not the Boba slots", 100, boba, 1, []int{3}},
		{"helmet", 80, helmet, 1, []int{3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayer(t, d, l)
			p.Inv.Gold = tt.gold
			want := p.Inv.Clone()
			for _, i := range tt.wantSlots {
				want.Slots[i] = world.Stack{Item: tt.item, Qty: 1}
			}
			cost := tt.qty * price(tt.item)
			want.Gold -= cost

			checkResult(t, handle(t, d, l, p, shop.BuyCommand{Player: 7, Item: tt.item, Quantity: tt.qty}), shop.Ok)
			if !reflect.DeepEqual(p.Inv, want) {
				t.Errorf("inventory:\n got  %+v\n want %+v", p.Inv, want)
			}
			checkEvent(t, p, shop.ReasonBuy, []world.Change{
				{Kind: world.ChangeAdd, Item: tt.item, Qty: tt.qty},
				{Kind: world.ChangeGold, Gold: -cost},
			})
		})
	}
}

func TestBuyRefused(t *testing.T) {
	d, l := gameData(t), lookup(t)
	burrito, boba, dagger := itemID(t, d, "burrito"), itemID(t, d, "boba"), itemID(t, d, "bronze_dagger")
	lock, acorn := itemID(t, d, "bike_lock"), itemID(t, d, "acorn")
	daggerPrice, _ := d.ShopSellPrice(dagger)
	tests := []struct {
		name string
		gold int
		item gamedata.ItemID
		qty  int
		want shop.Result
	}{
		{"one coin short", 2*daggerPrice - 1, dagger, 2, shop.NotEnoughGold},
		{"no gold", 0, boba, 1, shop.NotEnoughGold},
		{"quantity that would overflow the cost", 100, burrito, math.MaxInt, shop.NotEnoughGold},
		{"one item too many for the free slots", 1000, boba, 26, shop.InventoryFull},
		{"far too many for the free slots", 1 << 40, boba, 1000, shop.InventoryFull},
		{"item without a sell price", 1000, lock, 1, shop.ItemNotSold},
		{"loot the shop doesn't sell", 1000, acorn, 1, shop.ItemNotSold},
		{"unknown item id", 1000, 999, 1, shop.ItemNotSold},
		{"item id 0", 1000, 0, 1, shop.ItemNotSold},
		{"quantity 0", 1000, boba, 0, shop.ItemNotSold},
		{"negative quantity", 1000, boba, -1, shop.ItemNotSold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayer(t, d, l)
			p.Inv.Gold = tt.gold
			before := p.Inv.Clone()
			checkResult(t, handle(t, d, l, p, shop.BuyCommand{Player: 7, Item: tt.item, Quantity: tt.qty}), tt.want)
			checkUnchanged(t, p, before)
		})
	}
}

func TestBuyFullInventoryOfStackables(t *testing.T) {
	// Every free slot taken: even one item is refused.
	d, l := gameData(t), lookup(t)
	p := newPlayer(t, d, l)
	acorn := itemID(t, d, "acorn")
	for i := 3; i < len(p.Inv.Slots); i++ {
		p.Inv.Slots[i] = world.Stack{Item: acorn + gamedata.ItemID(i%4), Qty: 1}
	}
	before := p.Inv.Clone()
	checkResult(t, handle(t, d, l, p, shop.BuyCommand{Player: 7, Item: itemID(t, d, "hoodie"), Quantity: 1}), shop.InventoryFull)
	checkUnchanged(t, p, before)
}

func TestSell(t *testing.T) {
	d, l := gameData(t), lookup(t)
	acorn, trinket := itemID(t, d, "acorn"), itemID(t, d, "shiny_trinket")
	lock := itemID(t, d, "bike_lock")
	buys := func(id gamedata.ItemID) int {
		it, _ := d.Item(id)
		return it.ShopBuysFor
	}
	tests := []struct {
		name     string
		setup    world.Stack // put into slot 5 when set
		slot     int
		qty      int
		wantSold int
		wantLeft world.Stack
	}{
		{"one boba", world.Stack{}, 0, 1, 1, world.Stack{}},
		{"part of a stack", world.Stack{Item: acorn, Qty: 5}, 5, 3, 3, world.Stack{Item: acorn, Qty: 2}},
		{"whole stack", world.Stack{Item: trinket, Qty: 4}, 5, 4, 4, world.Stack{}},
		{"quantity capped at the stack", world.Stack{Item: acorn, Qty: 5}, 5, 10, 5, world.Stack{}},
		{"quantity capped at a single item", world.Stack{}, 1, math.MaxInt, 1, world.Stack{}},
		{"item the shop doesn't sell", world.Stack{Item: lock, Qty: 1}, 5, 1, 1, world.Stack{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayer(t, d, l)
			if !tt.setup.Empty() {
				p.Inv.Slots[5] = tt.setup
			}
			item := p.Inv.Slots[tt.slot].Item
			want := p.Inv.Clone()
			want.Slots[tt.slot] = tt.wantLeft
			paid := tt.wantSold * buys(item)
			want.Gold += paid

			checkResult(t, handle(t, d, l, p, shop.SellCommand{Player: 7, Slot: tt.slot, Quantity: tt.qty}), shop.Ok)
			if !reflect.DeepEqual(p.Inv, want) {
				t.Errorf("inventory:\n got  %+v\n want %+v", p.Inv, want)
			}
			checkEvent(t, p, shop.ReasonSell, []world.Change{
				{Kind: world.ChangeRemove, Item: item, Qty: tt.wantSold},
				{Kind: world.ChangeGold, Gold: paid},
			})
		})
	}
}

func TestSellRefused(t *testing.T) {
	d, l := gameData(t), lookup(t)
	n := d.Rules.InventorySlots
	tests := []struct {
		name  string
		setup func(p *world.Player)
		slot  int
		qty   int
	}{
		{"empty slot", nil, 3, 1},
		{"slot below range", nil, -1, 1},
		{"slot above range", nil, n, 1},
		{"quantity 0", nil, 0, 0},
		{"negative quantity", nil, 0, -1},
		{"unknown item in the slot", func(p *world.Player) { p.Inv.Slots[5] = world.Stack{Item: 999, Qty: 1} }, 5, 1},
		{"payment would overflow the balance", func(p *world.Player) { p.Inv.Gold = math.MaxInt }, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayer(t, d, l)
			if tt.setup != nil {
				tt.setup(p)
			}
			before := p.Inv.Clone()
			checkResult(t, handle(t, d, l, p, shop.SellCommand{Player: 7, Slot: tt.slot, Quantity: tt.qty}), shop.ItemNotSold)
			checkUnchanged(t, p, before)
		})
	}
}

// TestSellWithoutBuyPrice uses data where the shop pays nothing for an item.
func TestSellWithoutBuyPrice(t *testing.T) {
	d, l := gameData(t), lookup(t)
	acorn := itemID(t, d, "acorn")
	for i := range d.Items {
		if d.Items[i].ID == acorn {
			d.Items[i].ShopBuysFor = 0
		}
	}
	p := newPlayer(t, d, l)
	p.Inv.Slots[5] = world.Stack{Item: acorn, Qty: 3}
	before := p.Inv.Clone()
	checkResult(t, handle(t, d, l, p, shop.SellCommand{Player: 7, Slot: 5, Quantity: 1}), shop.ItemNotSold)
	checkUnchanged(t, p, before)
}

func TestOneEventPerRequest(t *testing.T) {
	d, l := gameData(t), lookup(t)
	burrito := itemID(t, d, "burrito")
	sink := &recSink{}
	w := world.New(world.DefaultConfig())
	if err := w.Register(shop.New(d, l, tileSize, sink)); err != nil {
		t.Fatal(err)
	}
	p := newPlayer(t, d, l)
	if err := w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	for _, c := range []world.Command{
		shop.BuyCommand{Player: 7, Item: burrito, Quantity: 2}, // ok
		shop.BuyCommand{Player: 7, Item: burrito, Quantity: 0}, // refused
		shop.SellCommand{Player: 7, Slot: 3, Quantity: 1},      // ok: a burrito
		shop.SellCommand{Player: 7, Slot: 20, Quantity: 1},     // refused
		items.EatCommand{Player: 7, Slot: 0},                   // not a shop command
	} {
		w.Enqueue(c)
	}
	w.Tick()

	want := []result{{7, shop.Ok}, {7, shop.ItemNotSold}, {7, shop.Ok}, {7, shop.ItemNotSold}}
	if !reflect.DeepEqual(sink.got, want) {
		t.Errorf("results = %+v, want %+v", sink.got, want)
	}
	ev := p.DrainEconomy()
	if len(ev) != 2 || ev[0].Reason != shop.ReasonBuy || ev[1].Reason != shop.ReasonSell {
		t.Errorf("outbox = %+v, want one shop_buy then one shop_sell", ev)
	}
}

func TestResultValues(t *testing.T) {
	// The values must match protocol ShopResultCode.
	tests := []struct {
		r    shop.Result
		val  uint8
		name string
	}{
		{shop.Ok, 0, "ok"},
		{shop.TooFar, 1, "too_far"},
		{shop.NotEnoughGold, 2, "not_enough_gold"},
		{shop.InventoryFull, 3, "inventory_full"},
		{shop.ItemNotSold, 4, "item_not_sold"},
		{shop.Result(9), 9, "Result(9)"},
	}
	for _, tt := range tests {
		if uint8(tt.r) != tt.val || tt.r.String() != tt.name {
			t.Errorf("Result %d = %q, want %d %q", uint8(tt.r), tt.r.String(), tt.val, tt.name)
		}
	}
}

func checkEvent(t *testing.T, p *world.Player, reason string, changes []world.Change) {
	t.Helper()
	ev := p.DrainEconomy()
	if len(ev) != 1 {
		t.Fatalf("outbox has %d events, want 1: %+v", len(ev), ev)
	}
	if ev[0].Reason != reason {
		t.Errorf("reason = %q, want %q", ev[0].Reason, reason)
	}
	if !reflect.DeepEqual(ev[0].Changes, changes) {
		t.Errorf("changes = %+v, want %+v", ev[0].Changes, changes)
	}
	if !reflect.DeepEqual(ev[0].Inv, p.Inv) {
		t.Errorf("event inventory = %+v, want the player's %+v", ev[0].Inv, p.Inv)
	}
}

func seq(from, to int) []int {
	var s []int
	for i := from; i < to; i++ {
		s = append(s, i)
	}
	return s
}

// TestSkillItemsAtDataPrices checks the shop buys the skill items at their
// data prices and pays out the data buy price (docs/specs/skills.md).
func TestSkillItemsAtDataPrices(t *testing.T) {
	d, l := gameData(t), lookup(t)
	tests := []struct {
		key        string
		price, buy int
	}{
		{"bronze_hatchet", 30, 10},
		{"steel_hatchet", 200, 70},
		{"small_net", 20, 5},
		{"fishing_rod", 40, 12},
		{"shrimp", 6, 2},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			id := itemID(t, d, tt.key)
			if got, ok := d.ShopSellPrice(id); !ok || got != tt.price {
				t.Fatalf("data price = %d (sold %v), want %d", got, ok, tt.price)
			}
			p := newPlayer(t, d, l)
			p.Inv.Gold = tt.price
			checkResult(t, handle(t, d, l, p, shop.BuyCommand{Player: 7, Item: id, Quantity: 1}), shop.Ok)
			if p.Inv.Gold != 0 || p.Inv.Slots[3].Item != id {
				t.Fatalf("after buy: gold %d, slot 3 %+v", p.Inv.Gold, p.Inv.Slots[3])
			}
			checkResult(t, handle(t, d, l, p, shop.SellCommand{Player: 7, Slot: 3, Quantity: 1}), shop.Ok)
			if p.Inv.Gold != tt.buy || !p.Inv.Slots[3].Empty() {
				t.Fatalf("after sell: gold %d, slot 3 %+v, want gold %d", p.Inv.Gold, p.Inv.Slots[3], tt.buy)
			}
		})
	}
}
