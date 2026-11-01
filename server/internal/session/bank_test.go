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
	"github.com/cleiderg/lush/server/internal/systems/bank"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// bankStore is the in-memory store whose TransferBank can fail or wait for a
// gate, and signals each call.
type bankStore struct {
	*store.Memory
	fail    bool
	gate    chan struct{} // if set, TransferBank waits for it to close
	entered chan struct{}
	mu      sync.Mutex
}

func newBankStore() *bankStore {
	return &bankStore{Memory: store.NewMemory(), entered: make(chan struct{}, 16)}
}

func (s *bankStore) TransferBank(ctx context.Context, t store.BankTransfer) (store.Holdings, []store.BankItem, error) {
	s.entered <- struct{}{}
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return store.Holdings{}, nil, ctx.Err()
		}
	}
	s.mu.Lock()
	fail := s.fail
	s.mu.Unlock()
	if fail {
		return store.Holdings{}, nil, errors.New("db down")
	}
	return s.Memory.TransferBank(ctx, t)
}

func (s *bankStore) setFail(v bool) {
	s.mu.Lock()
	s.fail = v
	s.mu.Unlock()
}

// rigClock reads the rig's clock once the rig exists.
type rigClock struct{ r **rig }

func (c rigClock) Now() time.Time                       { return (*c.r).clock.Now() }
func (c rigClock) NewTicker(time.Duration) world.Ticker { panic("no tickers") }

type bankRig struct {
	*rig
	d    *gamedata.Data
	st   *bankStore
	bank *bank.System
}

// pvpPos is a position in the PvP zone.
var pvpPos = world.Vec2{X: 250, Y: -350}

// newBankRig is a rig with the bank system whose booth is the spawn, with
// bankSlots distinct items allowed in a bank.
func newBankRig(t *testing.T, st *bankStore, bankSlots int) *bankRig {
	t.Helper()
	d := testData(t)
	d.Rules.BankSlots = bankSlots
	l := lookup(t)
	cfg := session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d, Spawn: l.Spawn()}
	var rg *rig
	br := &bankRig{d: d, st: st}
	rg = newRigSystems(t, 1024, cfg, st, nil, func(s *session.Server) []world.System {
		br.bank = bank.New(d, l, l.Spawn(), tileSize, rigClock{&rg}, s, s)
		s.SetBank(br.bank)
		return []world.System{br.bank}
	})
	br.rig = rg
	return br
}

func bankFrame(typ protocol.Message, start func(*flatbuffers.Builder), end func(*flatbuffers.Builder) flatbuffers.UOffsetT) []byte {
	return envelope(typ, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		start(b)
		return end(b)
	})
}

func bankOpen() []byte {
	return bankFrame(protocol.MessageBankOpen, protocol.BankOpenStart, protocol.BankOpenEnd)
}
func bankClose() []byte {
	return bankFrame(protocol.MessageBankClose, protocol.BankCloseStart, protocol.BankCloseEnd)
}
func bankDepositInventory() []byte {
	return bankFrame(protocol.MessageBankDepositInventory, protocol.BankDepositInventoryStart, protocol.BankDepositInventoryEnd)
}
func bankDepositEquipment() []byte {
	return bankFrame(protocol.MessageBankDepositEquipment, protocol.BankDepositEquipmentStart, protocol.BankDepositEquipmentEnd)
}

func bankDeposit(item uint16, amount uint32) []byte {
	return envelope(protocol.MessageBankDeposit, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.BankDepositStart(b)
		protocol.BankDepositAddItemId(b, item)
		protocol.BankDepositAddAmount(b, amount)
		return protocol.BankDepositEnd(b)
	})
}

func bankWithdraw(item uint16, amount uint32) []byte {
	return envelope(protocol.MessageBankWithdraw, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.BankWithdrawStart(b)
		protocol.BankWithdrawAddItemId(b, item)
		protocol.BankWithdrawAddAmount(b, amount)
		return protocol.BankWithdrawEnd(b)
	})
}

// bankReply reads until the next BankResult and returns its code and the
// slots of the BankState that came before it, if any.
func (r *rig) bankReply(c *client) (protocol.BankResultCode, []store.BankItem) {
	r.t.Helper()
	var slots []store.BankItem
	for i := 0; i < 1000; i++ {
		typ, tbl := r.next(c)
		switch typ {
		case protocol.MessageBankState:
			var bs protocol.BankState
			bs.Init(tbl.Bytes, tbl.Pos)
			slots = nil
			for j := 0; j < bs.SlotsLength(); j++ {
				var sl protocol.BankSlot
				bs.Slots(&sl, j)
				slots = append(slots, store.BankItem{Item: int32(sl.ItemId()), Qty: int64(sl.Quantity())})
			}
		case protocol.MessageBankResult:
			var res protocol.BankResult
			res.Init(tbl.Bytes, tbl.Pos)
			return res.Code(), slots
		}
	}
	r.t.Fatal("no BankResult")
	return 0, nil
}

func tokenHash(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// seeded creates the stored guest for token with the starting kit and banks
// one of its first non-stackable item, so the bank holds that item and the
// inventory no longer does. It returns the player's store id and the item.
func (b *bankRig) seeded(t *testing.T, token string) (int64, gamedata.ItemID) {
	t.Helper()
	kit, _ := kitHoldings(t, b.d)
	sp, err := b.st.CreateGuest(context.Background(), tokenHash(token), kit, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, st := range kit.Inventory {
		if it, ok := b.d.Item(gamedata.ItemID(st.Item)); ok && st.Item != 0 && !it.Stackable() {
			_, _, err := b.st.Memory.TransferBank(context.Background(), store.BankTransfer{PlayerID: sp.ID, SlotLimit: 200,
				Moves: []store.BankMove{{Deposit: true, Slot: i, Item: st.Item, Qty: 1}}})
			if err != nil {
				t.Fatal(err)
			}
			return sp.ID, gamedata.ItemID(st.Item)
		}
	}
	t.Fatal("no non-stackable item in the kit")
	return 0, 0
}

func (b *bankRig) join(token string) (*client, world.PlayerID) {
	b.t.Helper()
	c := b.dial(hello(uint16(protocol.ProtocolVersionCurrent), token))
	id, _, _, _, _ := joinFully(b.t, b.rig, c)
	return c, id
}

// AC2 (#471): a player who opens the bank at the booth gets BankResult Ok
// and a BankState with their saved slots.
func TestBankOpenSendsSavedSlots(t *testing.T) {
	b := newBankRig(t, newBankStore(), 200)
	_, item := b.seeded(t, "tok")
	c, _ := b.join("tok")
	c.send(bankOpen())
	code, slots := b.bankReply(c)
	if code != protocol.BankResultCodeOk || !slices.Equal(slots, []store.BankItem{{Item: int32(item), Qty: 1}}) {
		t.Fatalf("open: code %v slots %v, want Ok and item %d x1", code, slots, item)
	}
}

// AC1, AC3 (#471): a deposit and a withdrawal by item id move the item
// between the first slot holding it and the bank and are saved by one
// TransferBank each, whose audit entries are not written again.
func TestBankDepositWithdrawCommitOnce(t *testing.T) {
	b := newBankRig(t, newBankStore(), 200)
	sid, item := b.seeded(t, "tok")
	c, id := b.join("tok")
	auditBefore := len(b.st.AuditEntries())
	c.send(bankOpen())
	if code, _ := b.bankReply(c); code != protocol.BankResultCodeOk {
		t.Fatalf("open: %v", code)
	}

	// Withdraw by item id: the bank slot holding it, BankAmount.All.
	c.send(bankWithdraw(uint16(item), uint32(protocol.BankAmountAll)))
	code, slots := b.bankReply(c)
	if code != protocol.BankResultCodeOk || len(slots) != 0 {
		t.Fatalf("withdraw: code %v slots %v, want Ok and an empty bank", code, slots)
	}
	p := b.w.Player(id)
	held := p.Inv.Count(item) // the kit holds more than the one banked
	if held < 1 || p.Bank.Count(item) != 0 {
		t.Fatalf("inventory holds %d, bank %d of item %d, want some and 0", held, p.Bank.Count(item), item)
	}

	// Deposit it again: the first inventory slot holding it.
	c.send(bankDeposit(uint16(item), 1))
	code, slots = b.bankReply(c)
	if code != protocol.BankResultCodeOk || !slices.Equal(slots, []store.BankItem{{Item: int32(item), Qty: 1}}) {
		t.Fatalf("deposit: code %v slots %v", code, slots)
	}
	if p.Inv.Count(item) != held-1 || p.Bank.Count(item) != 1 {
		t.Fatalf("after the deposit the inventory holds %d and the bank %d of item %d, want %d and 1", p.Inv.Count(item), p.Bank.Count(item), item, held-1)
	}

	// Let the saver and the tick settle, then check what was written.
	if err := b.sessFlush(); err != nil {
		t.Fatal(err)
	}
	entries := b.st.AuditEntries()[auditBefore:]
	if len(entries) != 2 || entries[0].Kind != store.BankWithdrawKind || entries[1].Kind != store.BankDepositKind {
		t.Fatalf("audit entries %+v, want one bank_withdraw then one bank_deposit", entries)
	}
	stored, err := b.st.LoadBank(context.Background(), sid)
	if err != nil || !slices.Equal(stored, []store.BankItem{{Item: int32(item), Qty: 1}}) {
		t.Fatalf("stored bank %v, %v", stored, err)
	}
}

// sessFlush waits for every pending save.
func (b *bankRig) sessFlush() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The world is not ticking concurrently: next() ticks only while reading.
	return b.sess.SaveAll(ctx)
}

// AC1 (#471): depositing an item the player doesn't hold, or withdrawing one
// the bank doesn't hold, sends NotEnoughItems.
func TestBankItemNotHeld(t *testing.T) {
	b := newBankRig(t, newBankStore(), 200)
	_, item := b.seeded(t, "tok")
	c, _ := b.join("tok")
	c.send(bankOpen())
	b.bankReply(c)
	for name, msg := range map[string][]byte{
		"deposit":  bankDeposit(60000, 1),
		"withdraw": bankWithdraw(60000, 1),
		"zero":     bankWithdraw(uint16(item), 0),
	} {
		c.send(msg)
		if code, _ := b.bankReply(c); code != protocol.BankResultCodeNotEnoughItems {
			t.Errorf("%s: %v, want NotEnoughItems", name, code)
		}
	}
}

// AC1 (#471): BankDepositInventory and BankDepositEquipment reach the bank
// system; AC3: a failed save leaves everything as it was and sends SaveFailed.
func TestBankDepositAllAndSaveFailure(t *testing.T) {
	st := newBankStore()
	b := newBankRig(t, st, 200)
	b.seeded(t, "tok")
	c, id := b.join("tok")
	c.send(bankOpen())
	b.bankReply(c)
	p := b.w.Player(id)
	inv, bk := cloneInv(p.Inv), p.Bank.Clone()

	st.setFail(true)
	c.send(bankDepositInventory())
	if code, _ := b.bankReply(c); code != protocol.BankResultCodeSaveFailed {
		t.Fatalf("failed save: %v, want SaveFailed", code)
	}
	if !slices.Equal(p.Inv.Slots, inv.Slots) || p.Inv.Equipment != inv.Equipment || !slices.Equal(p.Bank.Slots, bk.Slots) || p.Inv.Locked() {
		t.Fatal("inventory, equipment or bank changed or stayed locked after a failed save")
	}

	st.setFail(false)
	c.send(bankDepositInventory())
	if code, _ := b.bankReply(c); code != protocol.BankResultCodeOk {
		t.Fatalf("deposit inventory: %v", code)
	}
	for _, s := range p.Inv.Slots {
		if !s.Empty() {
			t.Fatalf("inventory still holds %v", s)
		}
	}
	c.send(bankDepositEquipment())
	if code, _ := b.bankReply(c); code != protocol.BankResultCodeOk {
		t.Fatalf("deposit equipment: %v", code)
	}
}

func cloneInv(i world.Inventory) world.Inventory {
	i.Slots = slices.Clone(i.Slots)
	return i
}

// AC4 (#471): a player who disconnects while their transfer is being saved
// stays in the world until the result arrives; the stored holdings and bank
// then match it.
func TestBankTransferOutlivesDisconnect(t *testing.T) {
	st := newBankStore()
	st.gate = make(chan struct{})
	b := newBankRig(t, st, 200)
	sid, item := b.seeded(t, "tok")
	c, id := b.join("tok")
	c.send(bankOpen())
	b.bankReply(c)
	before := b.w.Player(id).Inv.Count(item)
	c.send(bankWithdraw(uint16(item), 1))
	tickUntil(t, b.rig, "the transfer to start", func() bool { return len(st.entered) > 0 })
	c.c.CloseNow()
	tickUntil(t, b.rig, "the disconnect", func() bool { return session.Disconnected(b.sess, id) })
	b.clock.Advance(2 * b.d.Rules.LogoutCombatLock())
	for i := 0; i < 10; i++ {
		b.w.Tick()
		time.Sleep(time.Millisecond)
	}
	p := b.w.Player(id)
	if p == nil {
		t.Fatal("player removed while their bank transfer was being saved")
	}

	close(st.gate)
	tickUntil(t, b.rig, "the player to be removed", func() bool { return b.w.Player(id) == nil })
	if p.Inv.Count(item) != before+1 || p.Bank.Count(item) != 0 || p.Inv.Locked() {
		t.Fatalf("result not applied: inventory %d, bank %d, locked %v", p.Inv.Count(item), p.Bank.Count(item), p.Inv.Locked())
	}
	if err := b.sessFlush(); err != nil {
		t.Fatal(err)
	}
	sp, err := b.st.LoadByTokenHash(context.Background(), tokenHash("tok"))
	if err != nil {
		t.Fatal(err)
	}
	held := 0
	for _, s := range sp.Holdings.Inventory {
		if s.Item == int32(item) {
			held += int(s.Qty)
		}
	}
	stored, _ := b.st.LoadBank(context.Background(), sid)
	if held != before+1 || len(stored) != 0 {
		t.Fatalf("stored inventory holds %d of item %d, bank %v; want %d and an empty bank", held, item, stored, before+1)
	}
}

// AC2 (#471): a player removed from the world after a withdrawal keeps the
// bank as it was then when they resume, not the bank loaded at first join.
func TestBankKeptAcrossResume(t *testing.T) {
	b := newBankRig(t, newBankStore(), 200)
	_, item := b.seeded(t, "tok")
	c, id := b.join("tok")
	c.send(bankOpen())
	b.bankReply(c)
	c.send(bankWithdraw(uint16(item), 1))
	if code, slots := b.bankReply(c); code != protocol.BankResultCodeOk || len(slots) != 0 {
		t.Fatalf("withdraw: code %v slots %v, want Ok and an empty bank", code, slots)
	}
	held := b.w.Player(id).Inv.Count(item)

	c.c.CloseNow()
	tickUntil(t, b.rig, "the disconnect", func() bool { return session.Disconnected(b.sess, id) })
	b.clock.Advance(2 * b.d.Rules.LogoutCombatLock())
	tickUntil(t, b.rig, "the player to be removed", func() bool { return b.w.Player(id) == nil })

	c2, id2 := b.join("tok")
	p := b.w.Player(id2)
	if p.Bank.Count(item) != 0 || p.Inv.Count(item) != held {
		t.Fatalf("resumed with bank %d and inventory %d of item %d, want 0 and %d", p.Bank.Count(item), p.Inv.Count(item), item, held)
	}
	c2.send(bankOpen())
	if code, slots := b.bankReply(c2); code != protocol.BankResultCodeOk || len(slots) != 0 {
		t.Fatalf("open after resume: code %v slots %v, want Ok and an empty bank", code, slots)
	}
}

func tickUntil(t *testing.T, r *rig, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		r.w.Tick()
	}
}

// AC5 (#471): each refused request sends the matching code, and a BankClose
// sends no reply.
func TestBankRefusals(t *testing.T) {
	t.Run("not open, and close sends nothing", func(t *testing.T) {
		b := newBankRig(t, newBankStore(), 200)
		_, item := b.seeded(t, "tok")
		c, _ := b.join("tok")
		c.send(bankWithdraw(uint16(item), 1))
		if code, _ := b.bankReply(c); code != protocol.BankResultCodeNotOpen {
			t.Fatalf("not open: %v", code)
		}
		c.send(bankOpen())
		b.bankReply(c)
		c.send(bankClose())
		c.send(bankWithdraw(uint16(item), 1))
		// A reply to BankClose would come first.
		if code, _ := b.bankReply(c); code != protocol.BankResultCodeNotOpen {
			t.Fatalf("after close: %v, want NotOpen as the only reply", code)
		}
	})
	for _, tt := range []struct {
		name string
		prep func(b *bankRig, p *world.Player)
		want protocol.BankResultCode
	}{
		{"too far", func(b *bankRig, p *world.Player) { p.Pos.X += 30 }, protocol.BankResultCodeTooFar},
		{"in combat", func(b *bankRig, p *world.Player) { p.LastCombat = b.clock.Now() }, protocol.BankResultCodeInCombat},
		{"in the PvP zone", func(b *bankRig, p *world.Player) { p.Pos = pvpPos }, protocol.BankResultCodeInPvpZone},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := newBankRig(t, newBankStore(), 200)
			b.seeded(t, "tok")
			c, id := b.join("tok")
			tt.prep(b, b.w.Player(id))
			c.send(bankOpen())
			if code, _ := b.bankReply(c); code != tt.want {
				t.Fatalf("%v, want %v", code, tt.want)
			}
		})
	}
	t.Run("bank full", func(t *testing.T) {
		b := newBankRig(t, newBankStore(), 1) // the seeded item fills it
		_, item := b.seeded(t, "tok")
		c, id := b.join("tok")
		c.send(bankOpen())
		b.bankReply(c)
		other := gamedata.ItemID(1)
		if other == item {
			other = 2
		}
		p := b.w.Player(id)
		p.Inv.Slots[len(p.Inv.Slots)-1] = world.Stack{Item: other, Qty: 1}
		c.send(bankDeposit(uint16(other), 1))
		if code, _ := b.bankReply(c); code != protocol.BankResultCodeBankFull {
			t.Fatalf("%v, want BankFull", code)
		}
	})
	t.Run("inventory full", func(t *testing.T) {
		b := newBankRig(t, newBankStore(), 200)
		_, item := b.seeded(t, "tok")
		c, id := b.join("tok")
		c.send(bankOpen())
		b.bankReply(c)
		p := b.w.Player(id)
		for i := range p.Inv.Slots {
			p.Inv.Slots[i] = world.Stack{Item: item, Qty: 1}
		}
		c.send(bankWithdraw(uint16(item), 1))
		if code, _ := b.bankReply(c); code != protocol.BankResultCodeInventoryFull {
			t.Fatalf("%v, want InventoryFull", code)
		}
	})
	t.Run("busy", func(t *testing.T) {
		st := newBankStore()
		st.gate = make(chan struct{})
		b := newBankRig(t, st, 200)
		_, item := b.seeded(t, "tok")
		c, _ := b.join("tok")
		c.send(bankOpen())
		b.bankReply(c)
		c.send(bankWithdraw(uint16(item), 1))
		tickUntil(t, b.rig, "the transfer to start", func() bool { return len(st.entered) > 0 })
		c.send(bankDepositInventory())
		if code, _ := b.bankReply(c); code != protocol.BankResultCodeBusy {
			t.Fatalf("%v, want Busy", code)
		}
		close(st.gate)
		if code, _ := b.bankReply(c); code != protocol.BankResultCodeOk {
			t.Fatalf("withdraw result %v, want Ok", code)
		}
	})
}
