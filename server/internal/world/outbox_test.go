package world_test

import (
	"reflect"
	"testing"

	"github.com/cleiderg/lush/server/internal/world"
)

func TestEconomyOutbox(t *testing.T) {
	d := gameData(t)
	boba, acorn := itemID(t, d, "boba"), itemID(t, d, "acorn")
	p := &world.Player{ID: 1, Inv: world.NewInventory(d)}

	if got := p.DrainEconomy(); len(got) != 0 {
		t.Fatalf("new player outbox = %+v, want empty", got)
	}

	type step struct {
		reason string
		apply  func() ([]world.Change, error)
	}
	steps := []step{
		{"first", func() ([]world.Change, error) { return p.Inv.Add(d, boba, 1) }},
		{"second", func() ([]world.Change, error) { return p.Inv.Add(d, acorn, 5) }},
		{"third", func() ([]world.Change, error) { return p.Inv.AddGold(7) }},
	}
	var wantChanges [][]world.Change
	var wantInv []world.Inventory
	for _, s := range steps {
		ch, err := s.apply()
		if err != nil {
			t.Fatalf("%s: %v", s.reason, err)
		}
		p.RecordEconomy(s.reason, ch)
		wantChanges = append(wantChanges, ch)
		wantInv = append(wantInv, clone(p.Inv))
	}

	// Later edits must not reach the recorded copies.
	p.Inv.Slots[0] = world.Stack{Item: acorn, Qty: 99}
	p.Inv.Slots[1].Qty = 1000
	p.Inv.Gold = 0
	p.Inv.Equipment.Weapon = boba

	if got := len(p.PendingEconomy()); got != len(steps) {
		t.Fatalf("pending = %d, want %d", got, len(steps))
	}
	got := p.DrainEconomy()
	if len(got) != len(steps) {
		t.Fatalf("drained %d events, want %d", len(got), len(steps))
	}
	for i, ev := range got {
		if ev.Reason != steps[i].reason {
			t.Errorf("event %d reason = %q, want %q", i, ev.Reason, steps[i].reason)
		}
		if !reflect.DeepEqual(ev.Changes, wantChanges[i]) {
			t.Errorf("event %d changes = %+v, want %+v", i, ev.Changes, wantChanges[i])
		}
		if !reflect.DeepEqual(ev.Inv, wantInv[i]) {
			t.Errorf("event %d inventory = %+v, want %+v", i, ev.Inv, wantInv[i])
		}
	}
	if got := p.DrainEconomy(); len(got) != 0 {
		t.Errorf("outbox after drain = %+v, want empty", got)
	}
	if got := p.PendingEconomy(); len(got) != 0 {
		t.Errorf("pending after drain = %+v, want empty", got)
	}

	// Drained events stay intact when new ones are recorded.
	p.RecordEconomy("fourth", nil)
	if got[0].Reason != "first" || !reflect.DeepEqual(got[0].Inv, wantInv[0]) {
		t.Errorf("drained event changed after a new record: %+v", got[0])
	}
	if got := p.DrainEconomy(); len(got) != 1 || got[0].Reason != "fourth" {
		t.Errorf("second drain = %+v, want one event \"fourth\"", got)
	}
}

func TestRemoveAt(t *testing.T) {
	d := gameData(t)
	boba, acorn := itemID(t, d, "boba"), itemID(t, d, "acorn")
	inv := world.NewInventory(d)
	inv.Slots[0] = world.Stack{Item: boba, Qty: 1}
	inv.Slots[1] = world.Stack{Item: boba, Qty: 1}
	inv.Slots[2] = world.Stack{Item: acorn, Qty: 3}

	ch, err := inv.RemoveAt(0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []world.Change{{Kind: world.ChangeRemove, Item: boba, Qty: 1}}; !reflect.DeepEqual(ch, want) {
		t.Errorf("changes = %+v, want %+v", ch, want)
	}
	if !inv.Slots[0].Empty() || inv.Slots[1].Item != boba {
		t.Errorf("slots = %+v, want slot 0 emptied and slot 1 kept", inv.Slots[:2])
	}
	if _, err := inv.RemoveAt(2); err != nil || inv.Slots[2].Qty != 2 {
		t.Errorf("remove from stack: err %v, slot %+v", err, inv.Slots[2])
	}
	for _, i := range []int{-1, len(inv.Slots), 0} {
		before := clone(inv)
		if _, err := inv.RemoveAt(i); err == nil {
			t.Errorf("RemoveAt(%d) succeeded", i)
		}
		if !reflect.DeepEqual(inv, before) {
			t.Errorf("RemoveAt(%d) changed the inventory", i)
		}
	}
}
