package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/trade"
	"github.com/cleiderg/lush/server/internal/world"
)

// economyCall is one ApplyEconomy call that reached the store.
type economyCall struct {
	changes []store.EconomyChange
	entries []store.Entry
}

// tradeStore is the in-memory store recording every ApplyEconomy call; with
// fail set every call fails and changes nothing.
type tradeStore struct {
	*store.Memory
	fail bool

	mu    sync.Mutex
	calls []economyCall
}

func (s *tradeStore) ApplyEconomy(ctx context.Context, changes []store.EconomyChange, entries []store.Entry) error {
	s.mu.Lock()
	s.calls = append(s.calls, economyCall{slices.Clone(changes), slices.Clone(entries)})
	s.mu.Unlock()
	if s.fail {
		return errors.New("db down")
	}
	return s.Memory.ApplyEconomy(ctx, changes, entries)
}

func (s *tradeStore) recorded() []economyCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

func (s *tradeStore) load(t *testing.T, token string) store.Player {
	t.Helper()
	h := sha256.Sum256([]byte(token))
	sp, err := s.LoadByTokenHash(context.Background(), hex.EncodeToString(h[:]))
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func newTradeRig(t *testing.T, st *tradeStore) *rig {
	t.Helper()
	d := testData(t)
	l := lookup(t)
	cfg := session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d, Spawn: l.Spawn()}
	return newRigSystems(t, 1024, cfg, st, nil, func(s *session.Server) []world.System {
		tr := trade.New(d, l, tileSize, s, s)
		s.SetTrades(tr)
		return []world.System{tr}
	})
}

func tradeRequest(target world.PlayerID) []byte {
	return envelope(protocol.MessageTradeRequest, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeRequestStart(b)
		protocol.TradeRequestAddTargetId(b, uint16(target))
		return protocol.TradeRequestEnd(b)
	})
}

func tradeOffer(slot int, qty uint32) []byte {
	return envelope(protocol.MessageTradeOffer, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeOfferStart(b)
		protocol.TradeOfferAddSlot(b, byte(slot))
		protocol.TradeOfferAddQuantity(b, qty)
		return protocol.TradeOfferEnd(b)
	})
}

func tradeSetGold(gold uint32) []byte {
	return envelope(protocol.MessageTradeSetGold, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeSetGoldStart(b)
		protocol.TradeSetGoldAddGold(b, gold)
		return protocol.TradeSetGoldEnd(b)
	})
}

func tradeAccept(screen byte) []byte {
	return envelope(protocol.MessageTradeAccept, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TradeAcceptStart(b)
		protocol.TradeAcceptAddScreen(b, screen)
		return protocol.TradeAcceptEnd(b)
	})
}

// tradeState reads until a TradeState for which ok holds, failing on a
// TradeResult first.
func tradeState(t *testing.T, r *rig, c *client, ok func(protocol.TradeState) bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		typ, tbl := r.nextNonSnapshot(c)
		switch typ {
		case protocol.MessageTradeResult:
			var res protocol.TradeResult
			res.Init(tbl.Bytes, tbl.Pos)
			t.Fatalf("TradeResult %v while waiting for a TradeState", res.Code())
		case protocol.MessageTradeState:
			var ts protocol.TradeState
			ts.Init(tbl.Bytes, tbl.Pos)
			if ok(ts) {
				return
			}
		}
	}
	t.Fatal("never got the expected TradeState")
}

func tradeResult(t *testing.T, r *rig, c *client) protocol.TradeResultCode {
	t.Helper()
	tbl := r.waitFor(c, protocol.MessageTradeResult)
	var res protocol.TradeResult
	res.Init(tbl.Bytes, tbl.Pos)
	return res.Code()
}

type trader struct {
	c     *client
	id    world.PlayerID
	token string
	st    protocol.PlayerStats
	inv   protocol.InventoryState
}

// tradeKit is what the trade moves: a gives one of item (from slot) and b
// gives gold.
type tradeKit struct {
	slot int
	item uint16
	gold uint32
}

// runTrade has a and b trade through their clients up to and including both
// accepts of screen 2.
func runTrade(t *testing.T, r *rig, a, b *trader, k tradeKit) {
	t.Helper()
	a.c.send(tradeRequest(b.id))
	tbl := r.waitFor(b.c, protocol.MessageTradeRequestNotice)
	var n protocol.TradeRequestNotice
	n.Init(tbl.Bytes, tbl.Pos)
	if world.PlayerID(n.FromId()) != a.id {
		t.Fatalf("TradeRequestNotice from %d, want %d", n.FromId(), a.id)
	}
	b.c.send(tradeRequest(a.id))
	for _, p := range []struct{ me, other *trader }{{a, b}, {b, a}} {
		tradeState(t, r, p.me.c, func(ts protocol.TradeState) bool {
			return world.PlayerID(ts.PartnerId()) == p.other.id && ts.Screen() == trade.ScreenOffer
		})
	}

	a.c.send(tradeOffer(k.slot, 1))
	tradeState(t, r, a.c, func(ts protocol.TradeState) bool { return ts.MyItemsLength() == 1 })
	tradeState(t, r, b.c, func(ts protocol.TradeState) bool { return ts.PartnerItemsLength() == 1 })
	b.c.send(tradeSetGold(k.gold))
	tradeState(t, r, b.c, func(ts protocol.TradeState) bool { return ts.MyGold() == k.gold })
	tradeState(t, r, a.c, func(ts protocol.TradeState) bool { return ts.PartnerGold() == k.gold })

	a.c.send(tradeAccept(trade.ScreenOffer))
	tradeState(t, r, b.c, func(ts protocol.TradeState) bool { return ts.PartnerAccepted() })
	b.c.send(tradeAccept(trade.ScreenOffer))
	for _, p := range []*trader{a, b} {
		tradeState(t, r, p.c, func(ts protocol.TradeState) bool { return ts.Screen() == trade.ScreenConfirm })
	}
	a.c.send(tradeAccept(trade.ScreenConfirm))
	tradeState(t, r, b.c, func(ts protocol.TradeState) bool { return ts.Screen() == trade.ScreenConfirm && ts.PartnerAccepted() })
	b.c.send(tradeAccept(trade.ScreenConfirm))
}

// joinTraders connects two guests and picks what a gives: a non-stacking
// inventory item, so one of it leaves one slot.
func joinTraders(t *testing.T, r *rig, d *gamedata.Data) (a, b *trader, k tradeKit) {
	t.Helper()
	a, b = &trader{c: r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))}, &trader{}
	a.id, a.token, _, a.st, a.inv = joinFully(t, r, a.c)
	b.c = r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	b.id, b.token, _, b.st, b.inv = joinFully(t, r, b.c)
	k.slot = -1
	for i, item := range slotItems(a.inv) {
		if it, ok := d.Item(gamedata.ItemID(item)); ok && item != 0 && !it.Stackable() {
			k.slot, k.item = i, item
			break
		}
	}
	if k.slot < 0 {
		t.Fatalf("starting kit has no non-stacking item: %v", slotItems(a.inv))
	}
	k.gold = 7
	if b.st.Gold() < k.gold {
		t.Fatalf("starting gold %d < %d", b.st.Gold(), k.gold)
	}
	return a, b, k
}

// AC1, AC2: a full trade over the wire: both clients see every TradeState
// and TradeResult Completed, then their new InventoryState and PlayerStats;
// the swap is saved as one ApplyEconomy holding both players' holdings, with
// "trade" audit entries naming both players.
func TestTradeOverTheWire(t *testing.T) {
	d := testData(t)
	st := &tradeStore{Memory: store.NewMemory()}
	r := newTradeRig(t, st)
	a, b, k := joinTraders(t, r, d)
	sa, sb := st.load(t, a.token), st.load(t, b.token)
	before := len(st.recorded())

	runTrade(t, r, a, b, k)
	for _, p := range []*trader{a, b} {
		if code := tradeResult(t, r, p.c); code != protocol.TradeResultCodeCompleted {
			t.Fatalf("player %d: TradeResult %v, want Completed", p.id, code)
		}
		var gotInv, gotStats bool
		for !gotInv || !gotStats {
			typ, tbl := r.nextNonSnapshot(p.c)
			switch typ {
			case protocol.MessageInventoryState:
				var inv protocol.InventoryState
				inv.Init(tbl.Bytes, tbl.Pos)
				want := qtyOf(p.inv, k.item) + 1
				if p == a {
					want -= 2
				}
				if got := qtyOf(inv, k.item); got != want {
					t.Fatalf("player %d holds %d of item %d, want %d", p.id, got, k.item, want)
				}
				gotInv = true
			case protocol.MessagePlayerStats:
				var ps protocol.PlayerStats
				ps.Init(tbl.Bytes, tbl.Pos)
				want := p.st.Gold() - k.gold
				if p == a {
					want = p.st.Gold() + k.gold
				}
				if ps.Gold() != want {
					t.Fatalf("player %d gold %d, want %d", p.id, ps.Gold(), want)
				}
				gotStats = true
			default:
				t.Fatalf("player %d: got %v after Completed, want InventoryState and PlayerStats", p.id, typ)
			}
		}
	}

	calls := st.recorded()[before:]
	if len(calls) != 1 {
		t.Fatalf("%d ApplyEconomy calls for the trade, want 1: %+v", len(calls), calls)
	}
	call := calls[0]
	if len(call.changes) != 2 || call.changes[0].PlayerID != sa.ID || call.changes[1].PlayerID != sb.ID {
		t.Fatalf("trade wrote holdings for %+v, want players %d and %d", call.changes, sa.ID, sb.ID)
	}
	for i, tok := range []string{a.token, b.token} {
		if got := st.load(t, tok).Holdings; got != call.changes[i].Holdings {
			t.Fatalf("stored holdings %+v, want the trade's %+v", got, call.changes[i].Holdings)
		}
	}
	if g := st.load(t, a.token).Gold; g != sa.Gold+int64(k.gold) {
		t.Fatalf("a's stored gold %d, want %d", g, sa.Gold+int64(k.gold))
	}
	if g := st.load(t, b.token).Gold; g != sb.Gold-int64(k.gold) {
		t.Fatalf("b's stored gold %d, want %d", g, sb.Gold-int64(k.gold))
	}
	if len(call.entries) == 0 {
		t.Fatal("trade wrote no audit entries")
	}
	actors := map[int64]int{}
	for _, e := range call.entries {
		if e.Kind != "trade" {
			t.Fatalf("audit entry kind %q, want trade", e.Kind)
		}
		if !(e.Actor == sa.ID && e.Counterparty == sb.ID) && !(e.Actor == sb.ID && e.Counterparty == sa.ID) {
			t.Fatalf("audit entry actor %d counterparty %d, want players %d and %d", e.Actor, e.Counterparty, sa.ID, sb.ID)
		}
		actors[e.Actor]++
	}
	if actors[sa.ID] == 0 || actors[sb.ID] == 0 {
		t.Fatalf("audit entries by actor %v, want both players", actors)
	}
	trades := 0
	for _, e := range st.AuditEntries() {
		if e.Kind == "trade" {
			trades++
		}
	}
	if trades != len(call.entries) {
		t.Fatalf("%d trade audit entries stored, want %d (logged once)", trades, len(call.entries))
	}
}

// AC4: when the trade's ApplyEconomy fails both clients get Cancelled, and
// both players' world inventories and stored holdings stay as they were.
func TestTradeSaveFailureCancels(t *testing.T) {
	d := testData(t)
	st := &tradeStore{Memory: store.NewMemory(), fail: true}
	r := newTradeRig(t, st)
	a, b, k := joinTraders(t, r, d)
	sa, sb := st.load(t, a.token), st.load(t, b.token)
	invA, invB := r.w.Player(a.id).Inv.Clone(), r.w.Player(b.id).Inv.Clone()

	runTrade(t, r, a, b, k)
	for _, p := range []*trader{a, b} {
		if code := tradeResult(t, r, p.c); code != protocol.TradeResultCodeCancelled {
			t.Fatalf("player %d: TradeResult %v, want Cancelled", p.id, code)
		}
	}
	if n := len(st.recorded()); n != 1 {
		t.Fatalf("%d ApplyEconomy calls, want the 1 failed trade", n)
	}
	for _, x := range []struct {
		id  world.PlayerID
		inv world.Inventory
	}{{a.id, invA}, {b.id, invB}} {
		p := r.w.Player(x.id)
		if !slices.Equal(p.Inv.Slots, x.inv.Slots) || p.Inv.Equipment != x.inv.Equipment || p.Inv.Gold != x.inv.Gold || p.Inv.Locked() {
			t.Fatalf("player %d inventory %+v, want unchanged and unlocked %+v", x.id, p.Inv, x.inv)
		}
	}
	if got := st.load(t, a.token).Holdings; got != sa.Holdings {
		t.Fatalf("a's stored holdings changed: %+v, want %+v", got, sa.Holdings)
	}
	if got := st.load(t, b.token).Holdings; got != sb.Holdings {
		t.Fatalf("b's stored holdings changed: %+v, want %+v", got, sb.Holdings)
	}
}
