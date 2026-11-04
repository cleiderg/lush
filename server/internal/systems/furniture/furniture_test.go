package furniture_test

import (
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/systems/furniture"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

const reach = 3.0

var (
	bench = world.Vec2{X: 100, Y: 100}
	stall = world.Vec2{X: 200, Y: 50}
)

type result struct {
	to world.PlayerID
	r  furniture.Result
}
type recSink struct{ got []result }

func (s *recSink) FurnitureResult(to world.PlayerID, r furniture.Result) {
	s.got = append(s.got, result{to, r})
}

func gameData(t testing.TB) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func id(t testing.TB, d *gamedata.Data, key string) gamedata.ItemID {
	t.Helper()
	it, ok := d.ItemByKey(key)
	if !ok {
		t.Fatalf("no item %q", key)
	}
	return it.ID
}

func newPlayer(t testing.TB, d *gamedata.Data, pos world.Vec2) *world.Player {
	t.Helper()
	inv := world.NewInventory(d)
	return &world.Player{ID: 7, Pos: pos, Inv: inv}
}

func run(t testing.TB, d *gamedata.Data, p *world.Player, cmd world.Command) furniture.Result {
	t.Helper()
	sink := &recSink{}
	w := world.New(world.DefaultConfig())
	sys := furniture.New(d, []world.Vec2{{X: 5000, Y: 5000}, bench}, []world.Vec2{stall, {X: -5000, Y: 0}}, reach, sink)
	if err := w.Register(sys); err != nil {
		t.Fatal(err)
	}
	if err := w.AddPlayer(p); err != nil {
		t.Fatal(err)
	}
	w.Enqueue(cmd)
	w.Tick()
	if len(sink.got) != 1 || sink.got[0].to != 7 {
		t.Fatalf("sink got %+v, want one result for player 7", sink.got)
	}
	return sink.got[0].r
}

func fill(t testing.TB, d *gamedata.Data, p *world.Player, item gamedata.ItemID, qty int) {
	t.Helper()
	if _, err := p.Inv.Add(d, item, qty); err != nil {
		t.Fatal(err)
	}
}

func checkEvent(t *testing.T, p *world.Player, reason string, want []world.Change) {
	t.Helper()
	ev := p.DrainEconomy()
	if len(ev) != 1 || ev[0].Reason != reason || !reflect.DeepEqual(ev[0].Changes, want) {
		t.Errorf("outbox = %+v, want one %q with %+v", ev, reason, want)
	}
}

func checkUnchanged(t *testing.T, p *world.Player, before world.Inventory) {
	t.Helper()
	if !reflect.DeepEqual(p.Inv, before) {
		t.Errorf("inventory changed:\n got  %+v\n want %+v", p.Inv, before)
	}
	if ev := p.DrainEconomy(); len(ev) != 0 {
		t.Errorf("outbox has events: %+v", ev)
	}
}

// TestCraft covers AC1 and the craft part of AC4.
func TestCraft(t *testing.T) {
	d := gameData(t)
	logs, stool := id(t, d, "logs"), id(t, d, "stool")
	p := newPlayer(t, d, world.Vec2{X: bench.X + reach, Y: bench.Y}) // at the reach boundary
	fill(t, d, p, logs, 5)
	p.Inv.Gold = 12
	if got := run(t, d, p, furniture.CraftCommand{Player: 7, Output: stool}); got != furniture.Ok {
		t.Fatalf("result %v, want ok", got)
	}
	if p.Inv.Count(logs) != 3 || p.Inv.Count(stool) != 1 || p.Inv.Gold != 7 {
		t.Errorf("logs %d stool %d gold %d, want 3 1 7", p.Inv.Count(logs), p.Inv.Count(stool), p.Inv.Gold)
	}
	checkEvent(t, p, furniture.ReasonCraft, []world.Change{
		{Kind: world.ChangeRemove, Item: logs, Qty: 2},
		{Kind: world.ChangeGold, Gold: -5},
		{Kind: world.ChangeAdd, Item: stool, Qty: 1},
	})
}

// TestCraftRefused covers AC2.
func TestCraftRefused(t *testing.T) {
	d := gameData(t)
	logs, stool, coin := id(t, d, "logs"), id(t, d, "stool"), id(t, d, "boba")
	pelt := id(t, d, "mounted_coyote_pelt")
	tests := []struct {
		name  string
		pos   world.Vec2
		out   gamedata.ItemID
		logs  int
		gold  int
		fillN int // junk items to fill slots
		want  furniture.Result
	}{
		{"too far", world.Vec2{X: bench.X + reach + 0.01, Y: bench.Y}, stool, 2, 5, 0, furniture.TooFar},
		{"at the stall, not a bench", stall, stool, 2, 5, 0, furniture.TooFar},
		{"unknown recipe", bench, logs, 2, 5, 0, furniture.UnknownRecipe},
		{"missing inputs", bench, stool, 1, 5, 0, furniture.MissingInputs},
		{"not enough gold", bench, stool, 2, 4, 0, furniture.NotEnoughGold},
		{"inventory full", bench, pelt, 0, 50, -1, furniture.InventoryFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayer(t, d, tt.pos)
			if tt.logs > 0 {
				fill(t, d, p, logs, tt.logs)
			}
			if tt.fillN < 0 {
				// Pelts stack, so using 3 of 4 frees no slot for the output.
				fill(t, d, p, id(t, d, "coyote_pelt"), 4)
				for p.Inv.FreeSlots() > 0 {
					fill(t, d, p, coin, 1)
				}
			}
			p.Inv.Gold = tt.gold
			before := p.Inv.Clone()
			if got := run(t, d, p, furniture.CraftCommand{Player: 7, Output: tt.out}); got != tt.want {
				t.Errorf("result %v, want %v", got, tt.want)
			}
			checkUnchanged(t, p, before)
		})
	}
}

// TestThriftBuySell covers AC3 and the thrift part of AC4.
func TestThriftBuySell(t *testing.T) {
	d := gameData(t)
	stool := id(t, d, "stool")
	price, ok := d.ThriftSellPrice(stool)
	if !ok {
		t.Fatal("thrift doesn't sell stool")
	}
	p := newPlayer(t, d, stall)
	p.Inv.Gold = 3*price + 1
	if got := run(t, d, p, furniture.BuyCommand{Player: 7, Item: stool, Quantity: 3}); got != furniture.Ok {
		t.Fatalf("buy: %v", got)
	}
	if p.Inv.Count(stool) != 3 || p.Inv.Gold != 1 {
		t.Errorf("stools %d gold %d, want 3 1", p.Inv.Count(stool), p.Inv.Gold)
	}
	checkEvent(t, p, furniture.ReasonThriftBuy, []world.Change{
		{Kind: world.ChangeAdd, Item: stool, Qty: 3},
		{Kind: world.ChangeGold, Gold: -3 * price},
	})

	slot := -1
	for i, s := range p.Inv.Slots {
		if s.Item == stool {
			slot = i
			break
		}
	}
	back := price / 3
	if got := run(t, d, p, furniture.SellCommand{Player: 7, Slot: slot, Quantity: 1}); got != furniture.Ok {
		t.Fatalf("sell: %v", got)
	}
	if p.Inv.Count(stool) != 2 || p.Inv.Gold != 1+back {
		t.Errorf("stools %d gold %d, want 2 %d", p.Inv.Count(stool), p.Inv.Gold, 1+back)
	}
	checkEvent(t, p, furniture.ReasonThriftSell, []world.Change{
		{Kind: world.ChangeRemove, Item: stool, Qty: 1},
		{Kind: world.ChangeGold, Gold: back},
	})
}

func TestThriftRefused(t *testing.T) {
	d := gameData(t)
	stool, boba := id(t, d, "stool"), id(t, d, "boba")
	price, _ := d.ThriftSellPrice(stool)
	far := world.Vec2{X: stall.X + reach + 0.01, Y: stall.Y}
	tests := []struct {
		name string
		pos  world.Vec2
		cmd  world.Command
		want furniture.Result
	}{
		{"buy too far", far, furniture.BuyCommand{Player: 7, Item: stool, Quantity: 1}, furniture.TooFar},
		{"sell too far", far, furniture.SellCommand{Player: 7, Slot: 0, Quantity: 1}, furniture.TooFar},
		{"buy at the workbench", bench, furniture.BuyCommand{Player: 7, Item: stool, Quantity: 1}, furniture.TooFar},
		{"buy item not sold", stall, furniture.BuyCommand{Player: 7, Item: boba, Quantity: 1}, furniture.ItemNotSold},
		{"buy zero", stall, furniture.BuyCommand{Player: 7, Item: stool, Quantity: 0}, furniture.ItemNotSold},
		{"one coin short", stall, furniture.BuyCommand{Player: 7, Item: stool, Quantity: 2}, furniture.NotEnoughGold},
		{"sell item not bought", stall, furniture.SellCommand{Player: 7, Slot: 0, Quantity: 1}, furniture.ItemNotSold},
		{"sell empty slot", stall, furniture.SellCommand{Player: 7, Slot: 5, Quantity: 1}, furniture.ItemNotSold},
		{"sell bad slot", stall, furniture.SellCommand{Player: 7, Slot: -1, Quantity: 1}, furniture.ItemNotSold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPlayer(t, d, tt.pos)
			fill(t, d, p, boba, 1) // slot 0
			p.Inv.Gold = 2*price - 1
			before := p.Inv.Clone()
			if got := run(t, d, p, tt.cmd); got != tt.want {
				t.Errorf("result %v, want %v", got, tt.want)
			}
			checkUnchanged(t, p, before)
		})
	}
}

func TestBuyInventoryFull(t *testing.T) {
	d := gameData(t)
	stool, boba := id(t, d, "stool"), id(t, d, "boba")
	p := newPlayer(t, d, stall)
	for p.Inv.FreeSlots() > 0 {
		fill(t, d, p, boba, 1)
	}
	p.Inv.Gold = 1000
	before := p.Inv.Clone()
	if got := run(t, d, p, furniture.BuyCommand{Player: 7, Item: stool, Quantity: 1}); got != furniture.InventoryFull {
		t.Errorf("result %v, want inventory_full", got)
	}
	checkUnchanged(t, p, before)
}

func BenchmarkCraft(b *testing.B) {
	d := gameData(b)
	logs, stool := id(b, d, "logs"), id(b, d, "stool")
	sys := furniture.New(d, []world.Vec2{bench}, []world.Vec2{stall}, reach, &recSink{})
	p := newPlayer(b, d, bench)
	for b.Loop() {
		p.Inv = world.NewInventory(d)
		fill(b, d, p, logs, 2)
		p.Inv.Gold = 5
		sys.Handle(furniture.CraftCommand{Player: 7, Output: stool}, p)
		p.DrainEconomy()
	}
}
