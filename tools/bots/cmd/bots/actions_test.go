package main

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/tools/bots/internal/protocol"
)

// fakeClock steps time forward in 100 ms ticks, like the bot's input loop.
type fakeClock struct{ now time.Time }

func (c *fakeClock) tick() time.Time {
	c.now = c.now.Add(inputEvery)
	return c.now
}

func TestSpanFlag(t *testing.T) {
	var s span
	if err := s.Set("20s-40s"); err != nil || s.min != 20*time.Second || s.max != 40*time.Second {
		t.Errorf("20s-40s: %+v, %v", s, err)
	}
	if err := s.Set("30s"); err != nil || s.min != 30*time.Second || s.max != 30*time.Second {
		t.Errorf("30s: %+v, %v", s, err)
	}
	for _, bad := range []string{"40s-20s", "0s", "x", "10s-y"} {
		if err := s.Set(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// TestWaveSchedule runs the default wave schedule for 10 minutes on a fake
// clock: the first wave within 15 s, then one every 20-40 s.
func TestWaveSchedule(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		clk := &fakeClock{now: time.Unix(1000, 0)}
		start := clk.now
		s := newSchedule(start, span{20 * time.Second, 40 * time.Second}, 15*time.Second, rand.New(rand.NewSource(seed)))
		var times []time.Time
		for clk.now.Sub(start) < 10*time.Minute {
			if now := clk.tick(); s.due(now) {
				s.done(now)
				times = append(times, now)
			}
		}
		if len(times) < 10 {
			t.Fatalf("seed %d: only %d waves in 10 min", seed, len(times))
		}
		if d := times[0].Sub(start); d > 15*time.Second+inputEvery {
			t.Errorf("seed %d: first wave after %v", seed, d)
		}
		for i := 1; i < len(times); i++ {
			if d := times[i].Sub(times[i-1]); d < 20*time.Second || d > 40*time.Second+inputEvery {
				t.Errorf("seed %d: wave gap %v", seed, d)
			}
		}
	}
}

// fakeServer applies ItemActions to a starting kit the way the server does,
// returning the new InventoryState only on a later tick (a round trip).
type fakeServer struct {
	inv     inventory
	pending []itemAction
}

func (f *fakeServer) apply() {
	for _, a := range f.pending {
		switch a.kind {
		case protocol.ItemActionKindUnequip:
			for i, id := range f.inv.slots {
				if id == 0 {
					f.inv.slots[i], f.inv.weapon = f.inv.weapon, 0
					break
				}
			}
		case protocol.ItemActionKindEquip:
			f.inv.slots[a.slot], f.inv.weapon = f.inv.weapon, f.inv.slots[a.slot]
		}
	}
	f.pending = nil
}

func (f *fakeServer) state() inventory {
	return inventory{known: true, weapon: f.inv.weapon, slots: append([]uint16(nil), f.inv.slots...)}
}

// TestItemSchedule runs the default item schedule for 10 minutes on a fake
// clock: the first unequip within 15 s, each followed by an equip of the same
// weapon from the slot the InventoryState shows, and unequips 30-60 s apart.
func TestItemSchedule(t *testing.T) {
	const stick = 1
	for seed := int64(1); seed <= 20; seed++ {
		clk := &fakeClock{now: time.Unix(1000, 0)}
		start := clk.now
		c := &itemCycle{sched: newSchedule(start, span{30 * time.Second, 60 * time.Second}, 15*time.Second,
			rand.New(rand.NewSource(seed)))}
		srv := &fakeServer{inv: inventory{known: true, weapon: stick, slots: []uint16{7, 0, 0, 0}}}

		// Nothing is sent before the first InventoryState arrives.
		if _, ok := c.step(clk.now.Add(time.Hour), inventory{}); ok {
			t.Fatal("acted without an inventory")
		}
		var unequips []time.Time
		var last protocol.ItemActionKind = protocol.ItemActionKindEquip
		seen := srv.state()
		for clk.now.Sub(start) < 10*time.Minute {
			now := clk.tick()
			srv.apply()
			if a, ok := c.step(now, seen); ok {
				if a.kind == last {
					t.Fatalf("seed %d: two %v in a row", seed, a.kind)
				}
				last = a.kind
				switch a.kind {
				case protocol.ItemActionKindUnequip:
					if a.equipSlot != protocol.EquipSlotWeapon {
						t.Fatalf("unequip slot %v", a.equipSlot)
					}
					unequips = append(unequips, now)
				case protocol.ItemActionKindEquip:
					if seen.slots[a.slot] != stick {
						t.Fatalf("seed %d: equip slot %d holds %d", seed, a.slot, seen.slots[a.slot])
					}
					if d := now.Sub(unequips[len(unequips)-1]); d > time.Second {
						t.Errorf("seed %d: re-equip %v after unequip", seed, d)
					}
				default:
					t.Fatalf("unexpected action %v", a.kind)
				}
				srv.pending = append(srv.pending, a)
			}
			seen = srv.state() // the reply reaches the bot before its next tick
		}
		if len(unequips) < 8 {
			t.Fatalf("seed %d: only %d unequips in 10 min", seed, len(unequips))
		}
		if d := unequips[0].Sub(start); d > 15*time.Second+inputEvery {
			t.Errorf("seed %d: first unequip after %v", seed, d)
		}
		for i := 1; i < len(unequips); i++ {
			if d := unequips[i].Sub(unequips[i-1]); d < 30*time.Second || d > 60*time.Second+inputEvery {
				t.Errorf("seed %d: unequip gap %v", seed, d)
			}
		}
		srv.apply()
		if last == protocol.ItemActionKindEquip && srv.inv.weapon != stick {
			t.Errorf("seed %d: weapon is %d at the end", seed, srv.inv.weapon)
		}
	}
}

// An unequip whose InventoryState never comes doesn't stall the cycle.
func TestItemCycleGivesUp(t *testing.T) {
	now := time.Unix(1000, 0)
	c := &itemCycle{sched: newSchedule(now, span{30 * time.Second, 30 * time.Second}, 0, rand.New(rand.NewSource(1)))}
	worn := inventory{known: true, weapon: 1, slots: []uint16{0}}
	if a, ok := c.step(now, worn); !ok || a.kind != protocol.ItemActionKindUnequip {
		t.Fatalf("want unequip, got %+v %v", a, ok)
	}
	if _, ok := c.step(now.Add(reequipGiveUp), worn); ok || c.held != 0 {
		t.Fatalf("should give up after %v", reequipGiveUp)
	}
	if a, ok := c.step(now.Add(30*time.Second), worn); !ok || a.kind != protocol.ItemActionKindUnequip {
		t.Fatalf("want the next unequip, got %+v %v", a, ok)
	}
}

func TestItemMessagesRoundTrip(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	env := protocol.GetRootAsEnvelope(encodeWave(b), 0)
	var tbl flatbuffers.Table
	if env.MsgType() != protocol.MessageEmoteSend || !env.Msg(&tbl) {
		t.Fatal("wave is not an EmoteSend")
	}
	var es protocol.EmoteSend
	es.Init(tbl.Bytes, tbl.Pos)
	if es.Emote() != protocol.EmoteWave {
		t.Errorf("emote %v", es.Emote())
	}

	env = protocol.GetRootAsEnvelope(encodeItemAction(b, itemAction{kind: protocol.ItemActionKindEquip, slot: 3}), 0)
	if env.MsgType() != protocol.MessageItemAction || !env.Msg(&tbl) {
		t.Fatal("not an ItemAction")
	}
	var ia protocol.ItemAction
	ia.Init(tbl.Bytes, tbl.Pos)
	if ia.Action() != protocol.ItemActionKindEquip || ia.Slot() != 3 {
		t.Errorf("item action %v slot %d", ia.Action(), ia.Slot())
	}

	b.Reset()
	protocol.InventoryStateStartSlotsVector(b, 2)
	protocol.CreateInventorySlot(b, 0, 0) // vectors are built back to front
	protocol.CreateInventorySlot(b, 5, 3)
	slots := b.EndVector(2)
	protocol.InventoryStateStart(b)
	protocol.InventoryStateAddSlots(b, slots)
	protocol.InventoryStateAddWeapon(b, 1)
	m, ok := decode(finish(b, protocol.MessageInventoryState, protocol.InventoryStateEnd(b)), 0)
	if !ok || !m.inv.known || m.inv.weapon != 1 || len(m.inv.slots) != 2 || m.inv.slots[0] != 5 || m.inv.slots[1] != 0 {
		t.Errorf("inventory %+v ok=%v", m.inv, ok)
	}
}

func TestSummaryActionCounts(t *testing.T) {
	data, err := json.Marshal(&summary{WavesSent: 4, ItemActionsSent: 6, InventoryStatesReceived: 9})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"waves_sent": "4", "item_actions_sent": "6", "inventory_states_received": "9"} {
		if string(got[k]) != want {
			t.Errorf("%s = %s, want %s", k, got[k], want)
		}
	}
}
