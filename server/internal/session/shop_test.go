package session_test

import (
	"slices"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/systems/shop"
	"github.com/cleiderg/lush/server/internal/world"
	"github.com/cleiderg/lush/server/internal/world/areas"
)

// tileSize is the shop distance unit in these tests, in metres.
const tileSize = 1.0

// newShopRig is a rig with the items and shop systems, whose new guests
// spawn at spawn.
func newShopRig(t *testing.T, d *gamedata.Data, l *areas.Lookup, spawn world.Vec2) *rig {
	t.Helper()
	cfg := session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d, Spawn: spawn}
	return newRigSystems(t, 256, cfg, store.NewMemory(), nil, func(s *session.Server) []world.System {
		return []world.System{items.New(d), shop.New(d, l, tileSize, s)}
	})
}

func lookup(t *testing.T) *areas.Lookup {
	t.Helper()
	l, err := areas.Load()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func itemAction(kind protocol.ItemActionKind, slot byte, eq protocol.EquipSlot) []byte {
	return envelope(protocol.MessageItemAction, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ItemActionStart(b)
		protocol.ItemActionAddAction(b, kind)
		protocol.ItemActionAddSlot(b, slot)
		protocol.ItemActionAddEquipSlot(b, eq)
		return protocol.ItemActionEnd(b)
	})
}

func shopBuy(item gamedata.ItemID, qty uint32) []byte {
	return envelope(protocol.MessageShopBuy, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShopBuyStart(b)
		protocol.ShopBuyAddItemId(b, uint16(item))
		protocol.ShopBuyAddQuantity(b, qty)
		return protocol.ShopBuyEnd(b)
	})
}

// nextNonSnapshot reads the next message that is not a Snapshot or EntityInfo
// (both sent by the tick as entities move and change).
func (r *rig) nextNonSnapshot(c *client) (protocol.Message, flatbuffers.Table) {
	r.t.Helper()
	for i := 0; i < 1000; i++ {
		if typ, tbl := r.next(c); typ != protocol.MessageSnapshot && typ != protocol.MessageEntityInfo {
			return typ, tbl
		}
	}
	r.t.Fatal("only snapshots")
	return 0, flatbuffers.Table{}
}

func shopResult(t *testing.T, r *rig, c *client) protocol.ShopResultCode {
	t.Helper()
	typ, tbl := r.nextNonSnapshot(c)
	if typ != protocol.MessageShopResult {
		t.Fatalf("got %v, want ShopResult", typ)
	}
	var sr protocol.ShopResult
	sr.Init(tbl.Bytes, tbl.Pos)
	return sr.Code()
}

func inventoryState(t *testing.T, r *rig, c *client) protocol.InventoryState {
	t.Helper()
	var inv protocol.InventoryState
	tbl := r.waitFor(c, protocol.MessageInventoryState)
	inv.Init(tbl.Bytes, tbl.Pos)
	return inv
}

func slotItems(inv protocol.InventoryState) []uint16 {
	out := make([]uint16, inv.SlotsLength())
	var s protocol.InventorySlot
	for i := range out {
		inv.Slots(&s, i)
		out[i] = s.ItemId()
	}
	return out
}

// cheapestSold returns the item the shop sells for the least gold.
func cheapestSold(t *testing.T, d *gamedata.Data) (gamedata.ItemID, int) {
	t.Helper()
	var id gamedata.ItemID
	best := 0
	for _, it := range d.Items {
		if p, ok := d.ShopSellPrice(it.ID); ok && (best == 0 || p < best) {
			id, best = it.ID, p
		}
	}
	if best == 0 {
		t.Fatal("the shop sells nothing")
	}
	return id, best
}

// AC3: ItemAction Unequip then Equip each bring an updated InventoryState.
func TestItemActionEquipSendsInventoryState(t *testing.T) {
	d := testData(t)
	r := newShopRig(t, d, lookup(t), world.Vec2{})
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	_, _, _, _, inv := joinFully(t, r, c)
	weapon := inv.Weapon()
	if weapon == 0 {
		t.Fatal("starting kit wears no weapon")
	}

	c.send(itemAction(protocol.ItemActionKindUnequip, 0, protocol.EquipSlotWeapon))
	inv = inventoryState(t, r, c)
	if inv.Weapon() != 0 {
		t.Fatalf("after Unequip weapon = %d, want none", inv.Weapon())
	}
	slot := slices.Index(slotItems(inv), weapon)
	if slot < 0 {
		t.Fatalf("unequipped weapon %d not in inventory %v", weapon, slotItems(inv))
	}

	c.send(itemAction(protocol.ItemActionKindEquip, byte(slot), protocol.EquipSlotNone))
	inv = inventoryState(t, r, c)
	if inv.Weapon() != weapon {
		t.Fatalf("after Equip weapon = %d, want %d", inv.Weapon(), weapon)
	}
	if got := slotItems(inv)[slot]; got != 0 {
		t.Fatalf("slot %d still holds %d after Equip", slot, got)
	}
}

// AC3: ShopBuy at the stall brings ShopResult Ok, then PlayerStats with the
// new gold, then InventoryState with the item.
func TestShopBuyNearStall(t *testing.T) {
	d := testData(t)
	l := lookup(t)
	r := newShopRig(t, d, l, l.Shop())
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	_, _, _, st, inv := joinFully(t, r, c)
	item, price := cheapestSold(t, d)
	if int(st.Gold()) < price {
		t.Fatalf("starting gold %d can't buy item %d at %d", st.Gold(), item, price)
	}
	before := qtyOf(inv, uint16(item))

	c.send(shopBuy(item, 1))
	if code := shopResult(t, r, c); code != protocol.ShopResultCodeOk {
		t.Fatalf("ShopResult = %v, want Ok", code)
	}
	typ, tbl := r.nextNonSnapshot(c)
	if typ != protocol.MessagePlayerStats {
		t.Fatalf("after ShopResult got %v, want PlayerStats", typ)
	}
	var ps protocol.PlayerStats
	ps.Init(tbl.Bytes, tbl.Pos)
	if want := st.Gold() - uint32(price); ps.Gold() != want {
		t.Fatalf("gold = %d, want %d", ps.Gold(), want)
	}
	typ, tbl = r.nextNonSnapshot(c)
	if typ != protocol.MessageInventoryState {
		t.Fatalf("after PlayerStats got %v, want InventoryState", typ)
	}
	var after protocol.InventoryState
	after.Init(tbl.Bytes, tbl.Pos)
	if got := qtyOf(after, uint16(item)); got != before+1 {
		t.Fatalf("holds %d of item %d, want %d", got, item, before+1)
	}
}

// AC4: ShopBuy far from the stall brings ShopResult TooFar and changes
// nothing.
func TestShopBuyFarFromStall(t *testing.T) {
	d := testData(t)
	l := lookup(t)
	far := world.Vec2{X: l.Shop().X + 50, Y: l.Shop().Y}
	r := newShopRig(t, d, l, far)
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	id, _, _, st, _ := joinFully(t, r, c)
	item, _ := cheapestSold(t, d)
	p := r.w.Player(id)
	before := p.Inv.Clone()

	c.send(shopBuy(item, 1))
	if code := shopResult(t, r, c); code != protocol.ShopResultCodeTooFar {
		t.Fatalf("ShopResult = %v, want TooFar", code)
	}
	if s, i, _ := countUpdates(t, r, c, 10); s != 0 || i != 0 {
		t.Fatalf("refused buy sent %d PlayerStats and %d InventoryState", s, i)
	}
	if p.Inv.Gold != int(st.Gold()) || !slices.Equal(p.Inv.Slots, before.Slots) || p.Inv.Equipment != before.Equipment {
		t.Fatalf("inventory changed: %+v, want %+v", p.Inv, before)
	}
}

// qtyOf is how many of item the inventory holds.
func qtyOf(inv protocol.InventoryState, item uint16) uint32 {
	var n uint32
	var s protocol.InventorySlot
	for i := 0; i < inv.SlotsLength(); i++ {
		if inv.Slots(&s, i); s.ItemId() == item {
			n += s.Quantity()
		}
	}
	return n
}
