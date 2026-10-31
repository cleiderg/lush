package storetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/store"
)

// BankStore is a store with banks.
type BankStore interface {
	store.Players
	store.Bank
}

// RunBank runs the Bank contract against fresh stores from factory, which
// also returns a function listing the store's audit entries in append order
// (Kind, Actor and Counterparty only).
func RunBank(t *testing.T, factory func(t *testing.T) (BankStore, func() []store.Entry)) {
	ctx := context.Background()

	// start is the holdings every test player begins with: 10 of item 1 in
	// slot 0, 3 of item 2 in slot 1, 1 of item 3 in slot 2, a weapon (item 7)
	// and a helmet (item 8).
	start := func() store.Holdings {
		var h store.Holdings
		h.Gold = 50
		h.Inventory[0] = store.Stack{Item: 1, Qty: 10}
		h.Inventory[1] = store.Stack{Item: 2, Qty: 3}
		h.Inventory[2] = store.Stack{Item: 3, Qty: 1}
		h.Equipment[store.EquipWeapon] = 7
		h.Equipment[store.EquipHead] = 8
		return h
	}
	newPlayer := func(t *testing.T) (BankStore, func() []store.Entry, store.Player) {
		t.Helper()
		s, audit := factory(t)
		p, err := s.CreateGuest(ctx, "b1", start(), nil)
		if err != nil {
			t.Fatal(err)
		}
		return s, audit, p
	}
	dep := func(slot int, item int32, qty int64) store.BankMove {
		return store.BankMove{Deposit: true, Slot: slot, Item: item, Qty: qty}
	}
	wd := func(slot int, item int32, qty int64) store.BankMove {
		return store.BankMove{Slot: slot, Item: item, Qty: qty}
	}
	// state renders the player's stored holdings, bank and the audit log.
	state := func(t *testing.T, s BankStore, audit func() []store.Entry, p store.Player) string {
		t.Helper()
		l, err := s.LoadByTokenHash(ctx, p.TokenHash)
		if err != nil {
			t.Fatal(err)
		}
		bank, err := s.LoadBank(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("holdings %+v version %d\nbank %v\naudit %v", l.Holdings, l.Version, bank, audit())
	}

	t.Run("new player has an empty bank", func(t *testing.T) {
		s, _, p := newPlayer(t)
		bank, err := s.LoadBank(ctx, p.ID)
		if err != nil || len(bank) != 0 {
			t.Fatalf("bank = %v, %v; want empty", bank, err)
		}
		if _, err := s.LoadBank(ctx, p.ID+1000); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown player: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("deposit and withdraw", func(t *testing.T) {
		s, audit, p := newPlayer(t)
		h, bank, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 200, Moves: []store.BankMove{
			dep(0, 1, 4),
			dep(1, 2, 3),
			{Deposit: true, Equipment: true, Slot: int(store.EquipWeapon), Item: 7, Qty: 1},
		}})
		if err != nil {
			t.Fatal(err)
		}
		want := start()
		want.Inventory[0].Qty = 6
		want.Inventory[1] = store.Stack{}
		want.Equipment[store.EquipWeapon] = 0
		wantBank := []store.BankItem{{Item: 1, Qty: 4}, {Item: 2, Qty: 3}, {Item: 7, Qty: 1}}
		if h != want || !reflect.DeepEqual(bank, wantBank) {
			t.Fatalf("after deposit: holdings %+v bank %v; want %+v %v", h, bank, want, wantBank)
		}

		// Deposit more of a banked item: it stacks in its slot.
		if _, bank, err = s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 3, Moves: []store.BankMove{dep(0, 1, 6)}}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(bank, []store.BankItem{{Item: 1, Qty: 10}, {Item: 2, Qty: 3}, {Item: 7, Qty: 1}}) {
			t.Fatalf("bank after stacking = %v", bank)
		}

		// Withdraw into an empty slot, onto the same item, and into an
		// empty equipment slot.
		if _, _, err = s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 200, Moves: []store.BankMove{
			wd(5, 1, 7),
			wd(5, 1, 3),
		}}); err != nil {
			t.Fatal(err)
		}
		h, bank, err = s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 200, Moves: []store.BankMove{
			wd(1, 2, 1),
			{Equipment: true, Slot: int(store.EquipWeapon), Item: 7, Qty: 1},
		}})
		if err != nil {
			t.Fatal(err)
		}
		want = start()
		want.Inventory[0] = store.Stack{}
		want.Inventory[1].Qty = 1
		want.Inventory[5] = store.Stack{Item: 1, Qty: 10}
		wantBank = []store.BankItem{{Item: 2, Qty: 2}}
		if h != want || !reflect.DeepEqual(bank, wantBank) {
			t.Fatalf("after withdraw: holdings %+v bank %v; want %+v %v", h, bank, want, wantBank)
		}

		l, err := s.LoadByTokenHash(ctx, p.TokenHash)
		if err != nil || l.Holdings != want || l.Version != p.Version {
			t.Fatalf("stored = %+v, %v; want holdings %+v at version %d", l, err, want, p.Version)
		}
		if got, _ := s.LoadBank(ctx, p.ID); !reflect.DeepEqual(got, wantBank) {
			t.Fatalf("stored bank = %v, want %v", got, wantBank)
		}

		// One entry per move, in order, against the player.
		kinds := []string{store.BankDepositKind, store.BankDepositKind, store.BankDepositKind, store.BankDepositKind,
			store.BankWithdrawKind, store.BankWithdrawKind, store.BankWithdrawKind, store.BankWithdrawKind}
		got := audit()
		if len(got) != len(kinds) {
			t.Fatalf("audit = %v, want %d entries", got, len(kinds))
		}
		for i, e := range got {
			if e.Kind != kinds[i] || e.Actor != p.ID || e.Counterparty != 0 {
				t.Errorf("audit[%d] = %+v, want %s by %d", i, e, kinds[i], p.ID)
			}
		}
	})

	t.Run("slot limit is a parameter", func(t *testing.T) {
		s, _, p := newPlayer(t)
		moves := []store.BankMove{dep(0, 1, 1), dep(1, 2, 1)}
		before := state(t, s, func() []store.Entry { return nil }, p)
		if _, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 1, Moves: moves}); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("limit 1: err = %v, want ErrInvalid", err)
		}
		if after := state(t, s, func() []store.Entry { return nil }, p); after != before {
			t.Fatalf("failed transfer changed state:\nbefore %s\nafter  %s", before, after)
		}
		if _, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 2, Moves: moves}); err != nil {
			t.Fatalf("limit 2: %v", err)
		}
		// A full bank still takes more of an item it holds, and gives items back.
		if _, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 2, Moves: []store.BankMove{dep(0, 1, 2)}}); err != nil {
			t.Fatalf("stack into a full bank: %v", err)
		}
		if _, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 2, Moves: []store.BankMove{dep(2, 3, 1)}}); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("new item into a full bank: err = %v, want ErrInvalid", err)
		}
		if _, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 1, Moves: []store.BankMove{wd(9, 1, 1)}}); err != nil {
			t.Fatalf("withdraw from a bank over the limit: %v", err)
		}
	})

	t.Run("bad transfer changes nothing", func(t *testing.T) {
		weapon := int(store.EquipWeapon)
		for _, tc := range []struct {
			name    string
			moves   []store.BankMove
			limit   int
			wantErr error
		}{
			{"exceeds slot limit", []store.BankMove{dep(0, 1, 1), dep(1, 2, 1), dep(2, 3, 1)}, 2, store.ErrInvalid},
			{"deposit more than held", []store.BankMove{dep(0, 1, 11)}, 200, store.ErrInvalid},
			{"deposit more than held over two moves", []store.BankMove{dep(0, 1, 6), dep(0, 1, 5)}, 200, store.ErrInvalid},
			{"deposit wrong item", []store.BankMove{dep(0, 2, 1)}, 200, store.ErrInvalid},
			{"deposit empty slot", []store.BankMove{dep(9, 1, 1)}, 200, store.ErrInvalid},
			{"withdraw more than banked", []store.BankMove{dep(0, 1, 4), wd(9, 1, 5)}, 200, store.ErrInvalid},
			{"withdraw unbanked item", []store.BankMove{wd(9, 5, 1)}, 200, store.ErrInvalid},
			{"withdraw onto another item", []store.BankMove{dep(1, 2, 3), wd(0, 2, 1)}, 200, store.ErrInvalid},
			{"withdraw into occupied equipment", []store.BankMove{dep(0, 1, 1), {Equipment: true, Slot: weapon, Item: 1, Qty: 1}}, 200, store.ErrInvalid},
			{"deposit wrong equipment", []store.BankMove{{Deposit: true, Equipment: true, Slot: weapon, Item: 8, Qty: 1}}, 200, store.ErrInvalid},
			{"equipment qty 2", []store.BankMove{{Deposit: true, Equipment: true, Slot: weapon, Item: 7, Qty: 2}}, 200, store.ErrInvalid},
			{"equipment slot out of range", []store.BankMove{{Deposit: true, Equipment: true, Slot: int(store.EquipSlots), Item: 7, Qty: 1}}, 200, store.ErrInvalid},
			{"inventory slot out of range", []store.BankMove{dep(0, 1, 1), wd(store.InventorySlots, 1, 1)}, 200, store.ErrInvalid},
			{"zero qty", []store.BankMove{dep(0, 1, 0)}, 200, store.ErrInvalid},
			{"no moves", nil, 200, store.ErrInvalid},
			{"negative limit", []store.BankMove{dep(0, 1, 1)}, -1, store.ErrInvalid},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s, audit, p := newPlayer(t)
				// Bank one helmet first, so the bank has a row to keep.
				if _, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: 200, Moves: []store.BankMove{
					{Deposit: true, Equipment: true, Slot: int(store.EquipHead), Item: 8, Qty: 1}}}); err != nil {
					t.Fatal(err)
				}
				limit := tc.limit
				if limit > 0 {
					limit++ // count the helmet
				}
				before := state(t, s, audit, p)
				_, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID, SlotLimit: limit, Moves: tc.moves})
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if after := state(t, s, audit, p); after != before {
					t.Fatalf("failed transfer changed state:\nbefore %s\nafter  %s", before, after)
				}
			})
		}
	})

	t.Run("unknown player", func(t *testing.T) {
		s, audit, p := newPlayer(t)
		before := state(t, s, audit, p)
		_, _, err := s.TransferBank(ctx, store.BankTransfer{PlayerID: p.ID + 1000, SlotLimit: 200, Moves: []store.BankMove{wd(0, 1, 1)}})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if after := state(t, s, audit, p); after != before {
			t.Fatalf("state changed:\nbefore %s\nafter  %s", before, after)
		}
	})
}
