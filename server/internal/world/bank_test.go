package world_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/world"
)

func TestBank(t *testing.T) {
	const slots = 2
	full := world.Bank{Slots: []world.Stack{{Item: 1, Qty: 3}, {Item: 2, Qty: 1}}}
	tests := []struct {
		name    string
		start   world.Bank
		op      func(b *world.Bank) ([]world.Change, error)
		want    world.Bank
		wantCh  []world.Change
		wantErr error
	}{
		{"add new item", world.Bank{},
			func(b *world.Bank) ([]world.Change, error) { return b.Add(1, 5, slots) },
			world.Bank{Slots: []world.Stack{{Item: 1, Qty: 5}}},
			[]world.Change{{Kind: world.ChangeBankAdd, Item: 1, Qty: 5}}, nil},
		{"add stacks on its slot in a full bank", full,
			func(b *world.Bank) ([]world.Change, error) { return b.Add(1, 2, slots) },
			world.Bank{Slots: []world.Stack{{Item: 1, Qty: 5}, {Item: 2, Qty: 1}}},
			[]world.Change{{Kind: world.ChangeBankAdd, Item: 1, Qty: 2}}, nil},
		{"add new item to full bank", full,
			func(b *world.Bank) ([]world.Change, error) { return b.Add(3, 1, slots) },
			full, nil, world.ErrBankFull},
		{"add zero", world.Bank{},
			func(b *world.Bank) ([]world.Change, error) { return b.Add(1, 0, slots) },
			world.Bank{}, nil, world.ErrBadQuantity},
		{"add item 0", world.Bank{},
			func(b *world.Bank) ([]world.Change, error) { return b.Add(0, 1, slots) },
			world.Bank{}, nil, world.ErrUnknownItem},
		{"remove some", full,
			func(b *world.Bank) ([]world.Change, error) { return b.Remove(1, 2) },
			world.Bank{Slots: []world.Stack{{Item: 1, Qty: 1}, {Item: 2, Qty: 1}}},
			[]world.Change{{Kind: world.ChangeBankRemove, Item: 1, Qty: 2}}, nil},
		{"remove all frees the slot", full,
			func(b *world.Bank) ([]world.Change, error) { return b.Remove(1, 3) },
			world.Bank{Slots: []world.Stack{{Item: 2, Qty: 1}}},
			[]world.Change{{Kind: world.ChangeBankRemove, Item: 1, Qty: 3}}, nil},
		{"remove more than banked", full,
			func(b *world.Bank) ([]world.Change, error) { return b.Remove(1, 4) },
			full, nil, world.ErrNotEnoughItems},
		{"remove item not banked", full,
			func(b *world.Bank) ([]world.Change, error) { return b.Remove(9, 1) },
			full, nil, world.ErrNotEnoughItems},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.start.Clone()
			ch, err := tt.op(&b)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(ch, tt.wantCh) {
				t.Errorf("changes = %+v, want %+v", ch, tt.wantCh)
			}
			if len(b.Slots) != len(tt.want.Slots) || (len(b.Slots) > 0 && !reflect.DeepEqual(b.Slots, tt.want.Slots)) {
				t.Errorf("bank = %+v, want %+v", b.Slots, tt.want.Slots)
			}
		})
	}
	if full.Count(1) != 3 || full.Count(9) != 0 || full.Index(2) != 1 {
		t.Errorf("Count/Index wrong on %+v (cases must not change the shared start)", full)
	}
}
