package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/furniture"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/world"
)

var (
	fixtureBench = world.Vec2{X: 100, Y: 100}
	fixtureStall = world.Vec2{X: 200, Y: 50}
)

const fixtureReach = 3.0

type furnRig struct {
	*rig
	d    *gamedata.Data
	mem  *store.Memory
	c    *client
	p    *world.Player
	shop world.Vec2
}

func newFurnRig(t *testing.T) *furnRig {
	t.Helper()
	f := &furnRig{d: testData(t), mem: store.NewMemory()}
	l := lookup(t)
	f.shop = l.Shop()
	cfg := session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: f.d, Spawn: fixtureBench}
	f.rig = newRigSystems(t, 1024, cfg, f.mem, nil, func(s *session.Server) []world.System {
		s.SetFurniture(furniture.New(f.d, []world.Vec2{fixtureBench}, []world.Vec2{fixtureStall}, fixtureReach, s.FurnitureSink()))
		return []world.System{items.New(f.d), shop.New(f.d, l, tileSize, s)}
	})
	f.c = f.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	id, _ := f.expectWelcome(f.c)
	f.w.Tick()
	f.p = f.w.Player(world.PlayerID(id))
	return f
}

func (f *furnRig) item(key string) gamedata.ItemID {
	it, ok := f.d.ItemByKey(key)
	if !ok {
		f.t.Fatalf("no item %q", key)
	}
	return it.ID
}

// ask sends buf, ticks and returns the next message of type typ.
func (f *furnRig) ask(buf []byte, typ protocol.Message) flatbuffers.Table {
	f.t.Helper()
	f.c.send(buf)
	time.Sleep(20 * time.Millisecond) // let the server goroutine enqueue it
	f.w.Tick()
	return f.waitFor(f.c, typ)
}

func (f *furnRig) craft(out gamedata.ItemID) protocol.CraftResultCode {
	f.t.Helper()
	tbl := f.ask(workbenchCraft(out), protocol.MessageCraftResult)
	var cr protocol.CraftResult
	cr.Init(tbl.Bytes, tbl.Pos)
	return cr.Code()
}

func (f *furnRig) shopCode(buf []byte) protocol.ShopResultCode {
	f.t.Helper()
	tbl := f.ask(buf, protocol.MessageShopResult)
	var sr protocol.ShopResult
	sr.Init(tbl.Bytes, tbl.Pos)
	return sr.Code()
}

func workbenchCraft(out gamedata.ItemID) []byte {
	return envelope(protocol.MessageWorkbenchCraft, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.WorkbenchCraftStart(b)
		protocol.WorkbenchCraftAddOutputItemId(b, uint16(out))
		return protocol.WorkbenchCraftEnd(b)
	})
}

func thriftBuy(item gamedata.ItemID, qty uint32) []byte {
	return envelope(protocol.MessageShopBuy, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShopBuyStart(b)
		protocol.ShopBuyAddItemId(b, uint16(item))
		protocol.ShopBuyAddQuantity(b, qty)
		protocol.ShopBuyAddShop(b, protocol.ShopKindThrift)
		return protocol.ShopBuyEnd(b)
	})
}

func thriftSell(slot byte, qty uint32) []byte {
	return envelope(protocol.MessageShopSell, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShopSellStart(b)
		protocol.ShopSellAddSlot(b, slot)
		protocol.ShopSellAddQuantity(b, qty)
		protocol.ShopSellAddShop(b, protocol.ShopKindThrift)
		return protocol.ShopSellEnd(b)
	})
}

func (f *furnRig) reasonAudit(reason string) map[string]int {
	f.t.Helper()
	if err := f.sess.SaveAll(context.Background()); err != nil {
		f.t.Fatal(err)
	}
	count := map[string]int{}
	for _, e := range f.mem.AuditEntries() {
		b, _ := json.Marshal(e.Payload)
		var p struct {
			Reason string
			Item   int
		}
		if err := json.Unmarshal(b, &p); err == nil && p.Reason == reason {
			count[fmt.Sprintf("%s %d", e.Kind, p.Item)]++
		}
	}
	return count
}

// AC1: the join sends the WorkbenchCatalog with every recipe, and the
// ItemCatalog carries the thrift prices.
func TestJoinSendsWorkbenchCatalogAndThriftPrices(t *testing.T) {
	f := newFurnRig(t)
	c := f.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	f.expectWelcome(c)
	var items protocol.ItemCatalog
	var bench protocol.WorkbenchCatalog
	var gotItems, gotBench bool
	for !gotItems || !gotBench {
		typ, tbl := f.next(c)
		switch typ {
		case protocol.MessageItemCatalog:
			items.Init(tbl.Bytes, tbl.Pos)
			gotItems = true
		case protocol.MessageWorkbenchCatalog:
			bench.Init(tbl.Bytes, tbl.Pos)
			gotBench = true
		case protocol.MessagePlayerStats:
			t.Fatal("join finished without both catalogs")
		}
	}

	if bench.RecipesLength() != len(f.d.Workbench) || len(f.d.Workbench) == 0 {
		t.Fatalf("catalog has %d recipes, data has %d", bench.RecipesLength(), len(f.d.Workbench))
	}
	var r protocol.WorkbenchRecipe
	for i, want := range f.d.Workbench {
		bench.Recipes(&r, i)
		if r.OutputItemId() != uint16(want.Output) || r.Gold() != uint32(want.Gold) || r.InputItemIdsLength() != len(want.Inputs) || r.InputQuantitiesLength() != len(want.Inputs) {
			t.Fatalf("recipe %d = output %d gold %d, want %+v", i, r.OutputItemId(), r.Gold(), want)
		}
		for j, in := range want.Inputs {
			if r.InputItemIds(j) != uint16(in.Item) || r.InputQuantities(j) != uint32(in.Quantity) {
				t.Fatalf("recipe %d input %d = %d×%d, want %+v", i, j, r.InputQuantities(j), r.InputItemIds(j), in)
			}
		}
	}

	var def protocol.ItemDef
	thrifty := 0
	for i := 0; i < items.ItemsLength(); i++ {
		items.Items(&def, i)
		sell, sells := f.d.ThriftSellPrice(gamedata.ItemID(def.Id()))
		buy, _ := f.d.ThriftBuyPrice(gamedata.ItemID(def.Id()))
		if def.ThriftSellsFor() != uint32(sell) || def.ThriftBuysFor() != uint32(buy) {
			t.Fatalf("item %d thrift prices %d/%d, want %d/%d", def.Id(), def.ThriftSellsFor(), def.ThriftBuysFor(), sell, buy)
		}
		if sells {
			thrifty++
		}
	}
	if thrifty == 0 {
		t.Fatal("no item has thrift prices")
	}
}

// AC2: WorkbenchCraft reaches the furniture system, each refusal has its
// code, and a success is saved with a furniture audit entry.
func TestWorkbenchCraft(t *testing.T) {
	f := newFurnRig(t)
	// The pelt recipe: its input stacks, so a spare one keeps its slot and the
	// inventory can be full after the craft.
	pelt := f.item("mounted_coyote_pelt")
	rec := f.d.Workbench[slices.IndexFunc(f.d.Workbench, func(r gamedata.WorkbenchRecipe) bool { return r.Output == pelt })]
	give := func() {
		for _, in := range rec.Inputs {
			if _, err := f.p.Inv.Add(f.d, in.Item, in.Quantity); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.p.Inv.Gold = 0

	if got := f.craft(rec.Output); got != protocol.CraftResultCodeMissingInputs {
		t.Fatalf("no inputs: %v", got)
	}
	give()
	if rec.Gold > 0 {
		if got := f.craft(rec.Output); got != protocol.CraftResultCodeNotEnoughGold {
			t.Fatalf("no gold: %v", got)
		}
	}
	if got := f.craft(f.item("logs")); got != protocol.CraftResultCodeUnknownRecipe {
		t.Fatalf("not a recipe: %v", got)
	}
	f.p.Inv.Gold = rec.Gold
	f.p.Pos = world.Vec2{X: fixtureBench.X + fixtureReach + 1, Y: fixtureBench.Y}
	if got := f.craft(rec.Output); got != protocol.CraftResultCodeTooFar {
		t.Fatalf("far: %v", got)
	}
	f.p.Pos = fixtureBench

	// One spare input keeps its slot after the craft; fill every other slot
	// with a distinct item so the output has nowhere to go.
	full := f.p.Inv.Clone()
	if _, err := f.p.Inv.Add(f.d, rec.Inputs[0].Item, 1); err != nil {
		t.Fatal(err)
	}
	next := 0
	for i := range f.p.Inv.Slots {
		for f.p.Inv.Slots[i].Empty() {
			it := f.d.Items[next%len(f.d.Items)]
			next++
			if it.ID != rec.Output && !slices.ContainsFunc(rec.Inputs, func(in gamedata.RecipeInput) bool { return in.Item == it.ID }) && f.p.Inv.Count(it.ID) == 0 {
				f.p.Inv.Slots[i] = world.Stack{Item: it.ID, Qty: 1}
			}
		}
	}
	if got := f.craft(rec.Output); got != protocol.CraftResultCodeInventoryFull {
		t.Fatalf("full inventory: %v", got)
	}
	f.p.Inv = full

	if got := f.craft(rec.Output); got != protocol.CraftResultCodeOk {
		t.Fatalf("craft: %v", got)
	}
	if f.p.Inv.Count(rec.Output) != 1 || f.p.Inv.Gold != 0 {
		t.Fatalf("after craft: %d outputs, %d gold", f.p.Inv.Count(rec.Output), f.p.Inv.Gold)
	}

	want := map[string]int{fmt.Sprintf("item_add %d", rec.Output): 1}
	for _, in := range rec.Inputs {
		want[fmt.Sprintf("item_remove %d", in.Item)]++
	}
	if rec.Gold > 0 {
		want["gold 0"]++
	}
	got := f.reasonAudit(furniture.ReasonCraft)
	for k := range want {
		if got[k] == 0 {
			t.Fatalf("craft audit %v lacks %q", got, k)
		}
	}
	if got[fmt.Sprintf("item_add %d", rec.Output)] != 1 {
		t.Fatalf("craft audit %v", got)
	}
}

// AC3: ShopBuy and ShopSell with the Thrift shop go to the furniture system
// and are saved and audited; the default shop still goes to the Campus Market.
func TestThriftShopRouting(t *testing.T) {
	f := newFurnRig(t)
	stool := f.item("stool")
	price, ok := f.d.ThriftSellPrice(stool)
	if !ok {
		t.Fatal("thrift doesn't sell stool")
	}
	f.p.Pos = fixtureStall
	f.p.Inv.Gold = 3 * price

	if got := f.shopCode(thriftBuy(stool, 3)); got != protocol.ShopResultCodeOk {
		t.Fatalf("thrift buy: %v", got)
	}
	if f.p.Inv.Count(stool) != 3 || f.p.Inv.Gold != 0 {
		t.Fatalf("after buy: %d stools, %d gold", f.p.Inv.Count(stool), f.p.Inv.Gold)
	}
	if got := f.shopCode(thriftBuy(stool, 1)); got != protocol.ShopResultCodeNotEnoughGold {
		t.Fatalf("broke: %v", got)
	}
	// Stools don't stack, so each sells from its own slot.
	back, _ := f.d.ThriftBuyPrice(stool)
	for i := 1; i <= 3; i++ {
		slot := slices.IndexFunc(f.p.Inv.Slots, func(s world.Stack) bool { return s.Item == stool })
		if got := f.shopCode(thriftSell(byte(slot), 1)); got != protocol.ShopResultCodeOk {
			t.Fatalf("thrift sell %d: %v", i, got)
		}
		if f.p.Inv.Count(stool) != 3-i || f.p.Inv.Gold != i*back {
			t.Fatalf("after sell %d: %d stools, %d gold, want %d and %d", i, f.p.Inv.Count(stool), f.p.Inv.Gold, 3-i, i*back)
		}
	}
	if got := f.shopCode(thriftSell(0, 0)); got != protocol.ShopResultCodeItemNotSold {
		t.Fatalf("sell empty: %v", got)
	}
	f.p.Pos = fixtureBench
	if got := f.shopCode(thriftBuy(stool, 1)); got != protocol.ShopResultCodeTooFar {
		t.Fatalf("far: %v", got)
	}

	if got := f.reasonAudit(furniture.ReasonThriftBuy); got[fmt.Sprintf("item_add %d", stool)] != 1 {
		t.Fatalf("buy audit %v", got)
	}
	if got := f.reasonAudit(furniture.ReasonThriftSell)[fmt.Sprintf("item_remove %d", stool)]; got != 3 {
		t.Fatalf("sell audit removed %d stool entries, want 3", got)
	}

	// The default shop field is still the Campus Market, which doesn't sell
	// stools and so refuses; at the thrift stall it is too far.
	f.p.Pos = f.shop
	if got := f.shopCode(shopBuy(stool, 1)); got != protocol.ShopResultCodeItemNotSold {
		t.Fatalf("market buy of a stool: %v", got)
	}
	food, mprice := cheapestSold(t, f.d)
	f.p.Inv.Gold = mprice
	if got := f.shopCode(shopBuy(food, 1)); got != protocol.ShopResultCodeOk || f.p.Inv.Gold != 0 {
		t.Fatalf("market buy: %v gold %d", got, f.p.Inv.Gold)
	}
}
