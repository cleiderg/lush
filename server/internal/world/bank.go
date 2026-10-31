package world

import (
	"errors"
	"fmt"
	"math"

	"github.com/cleiderg/lush/server/internal/systems/gamedata"
)

// ErrBankFull is returned when an item not yet in the bank would need a slot
// and every slot is taken.
var ErrBankFull = errors.New("bank full")

// Bank is a player's banked items (docs/specs/skills.md, Bank). Every item
// stacks: each slot holds any quantity of one item, so the bank holds at most
// Rules.BankSlots distinct items. Slots are kept in the order the items were
// first deposited; a slot that is emptied is removed. Banked items are never
// lost on death. Only the tick may mutate it.
type Bank struct {
	Slots []Stack
}

// Clone returns a copy of b that shares no memory with it.
func (b *Bank) Clone() Bank {
	return Bank{Slots: append([]Stack(nil), b.Slots...)}
}

// Index returns the slot holding item, or -1.
func (b *Bank) Index(item gamedata.ItemID) int {
	for i, s := range b.Slots {
		if s.Item == item {
			return i
		}
	}
	return -1
}

// Count returns how many of item the bank holds.
func (b *Bank) Count(item gamedata.ItemID) int {
	if i := b.Index(item); i >= 0 {
		return b.Slots[i].Qty
	}
	return 0
}

// Add puts qty of item into the bank, onto its slot or into a new slot if the
// bank holds fewer than slots distinct items. If it fails nothing changes.
func (b *Bank) Add(item gamedata.ItemID, qty, slots int) ([]Change, error) {
	if item == 0 {
		return nil, fmt.Errorf("bank add item 0: %w", ErrUnknownItem)
	}
	if qty <= 0 {
		return nil, fmt.Errorf("bank add %d × item %d: %w", qty, item, ErrBadQuantity)
	}
	if i := b.Index(item); i >= 0 {
		if b.Slots[i].Qty > math.MaxInt-qty {
			return nil, fmt.Errorf("bank add %d × item %d: %w", qty, item, ErrStackOverflow)
		}
		b.Slots[i].Qty += qty
	} else {
		if len(b.Slots) >= slots {
			return nil, fmt.Errorf("bank add %d × item %d: %w", qty, item, ErrBankFull)
		}
		b.Slots = append(b.Slots, Stack{Item: item, Qty: qty})
	}
	return []Change{{Kind: ChangeBankAdd, Item: item, Qty: qty}}, nil
}

// Remove takes qty of item out of the bank, removing its slot when it empties.
// If fewer than qty are banked nothing changes.
func (b *Bank) Remove(item gamedata.ItemID, qty int) ([]Change, error) {
	if qty <= 0 {
		return nil, fmt.Errorf("bank remove %d × item %d: %w", qty, item, ErrBadQuantity)
	}
	i := b.Index(item)
	if i < 0 || item == 0 || b.Slots[i].Qty < qty {
		return nil, fmt.Errorf("bank remove %d × item %d (have %d): %w", qty, item, b.Count(item), ErrNotEnoughItems)
	}
	b.Slots[i].Qty -= qty
	if b.Slots[i].Qty == 0 {
		b.Slots = append(b.Slots[:i], b.Slots[i+1:]...)
	}
	return []Change{{Kind: ChangeBankRemove, Item: item, Qty: qty}}, nil
}
