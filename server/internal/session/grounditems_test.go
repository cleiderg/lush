package session_test

import (
	"context"
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/grounditems"
	"github.com/cleiderg/lush/server/internal/world"
)

func pickUp(id uint16) []byte {
	return envelope(protocol.MessagePickUp, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.PickUpStart(b)
		protocol.PickUpAddGroundItemId(b, id)
		return protocol.PickUpEnd(b)
	})
}

// groundEntry is one GroundItem as a client got it.
type groundEntry struct {
	ID   uint16
	Item uint16
	Qty  uint32
	X, Z int16
}

// frames is what a client got up to the Snapshot of one tick.
type frames struct {
	grounds [][]groundEntry // one per GroundItems, in order
	invs    []protocol.InventoryState
}

// collect reads c's frames, without ticking, up to the Snapshot of the
// current world tick.
func (r *pvpRig) collect(c *client) frames {
	r.t.Helper()
	want := uint32(r.w.TickCount())
	var f frames
	for i := 0; i < 10000; i++ {
		typ, tbl := r.readNoTick(c)
		switch typ {
		case protocol.MessageGroundItems:
			var gi protocol.GroundItems
			gi.Init(tbl.Bytes, tbl.Pos)
			list := []groundEntry{}
			var it protocol.GroundItem
			for j := 0; j < gi.ItemsLength(); j++ {
				gi.Items(&it, j)
				list = append(list, groundEntry{it.Id(), it.ItemId(), it.Quantity(), it.X(), it.Z()})
			}
			f.grounds = append(f.grounds, list)
		case protocol.MessageInventoryState:
			var inv protocol.InventoryState
			inv.Init(tbl.Bytes, tbl.Pos)
			f.invs = append(f.invs, inv)
		case protocol.MessageSnapshot:
			var s protocol.Snapshot
			if s.Init(tbl.Bytes, tbl.Pos); s.Tick() == want {
				return f
			}
		}
	}
	r.t.Fatalf("never saw the snapshot of tick %d", want)
	return f
}

// stepGround ticks once and returns the GroundItems c got up to that tick's
// Snapshot.
func (r *pvpRig) stepGround(c *client) [][]groundEntry {
	r.t.Helper()
	r.w.Tick()
	return r.collect(c).grounds
}

// expectGround fails unless got is exactly one GroundItems listing want.
func expectGround(t *testing.T, what string, got [][]groundEntry, want ...groundEntry) {
	t.Helper()
	if want == nil {
		want = []groundEntry{}
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("%s: GroundItems = %+v, want exactly one listing %+v", what, got, want)
	}
}

func expectNoGround(t *testing.T, what string, got [][]groundEntry) {
	t.Helper()
	if len(got) != 0 {
		t.Fatalf("%s: got GroundItems %+v, want none", what, got)
	}
}

func (r *pvpRig) spawnGround(key string, qty int, pos world.Vec2, owner world.PlayerID) groundEntry {
	r.t.Helper()
	it, ok := r.data.ItemByKey(key)
	if !ok {
		r.t.Fatalf("no item %q", key)
	}
	id, err := r.ground.Spawn(it.ID, qty, pos, owner, r.clock.Now())
	if err != nil {
		r.t.Fatal(err)
	}
	return groundEntry{uint16(id), uint16(it.ID), uint32(qty), int16(pos.X * 10), int16(pos.Y * 10)}
}

func countItem(inv protocol.InventoryState, item gamedata.ItemID) int {
	var s protocol.InventorySlot
	n := 0
	for i := 0; i < inv.SlotsLength(); i++ {
		if inv.Slots(&s, i); s.ItemId() == uint16(item) {
			n += int(s.Quantity())
		}
	}
	return n
}

// AC1, AC4: a client gets GroundItems with the ground items in view it may
// see on join, on reconnect, and whenever that list changes (spawned, picked
// up, despawned, reservation ended), and on no other tick. A PickUp picks the
// item up, the InventoryState shows it, and the store gets a pickup audit
// entry through the economy writer.
func TestGroundItemsInViewSentOnChange(t *testing.T) {
	r := newPvPRig(t, pvpRand{f: 0.999999})
	v := uint16(protocol.ProtocolVersionCurrent)
	ca := r.dial(hello(v, ""))
	a, tokA, _, _, _ := joinFully(t, r.rig, ca)
	r.w.Player(a).HasDesired = false

	expectGround(t, "join", r.stepGround(ca))
	expectNoGround(t, "quiet tick", r.stepGround(ca))

	acorn := r.spawnGround("acorn", 3, world.Vec2{X: 1, Y: 0}, a)
	r.spawnGround("acorn", 1, world.Vec2{X: 30, Y: 0}, a) // out of view
	expectGround(t, "spawned", r.stepGround(ca), acorn)
	for range 3 {
		expectNoGround(t, "unchanged", r.stepGround(ca))
	}
	pelt := r.spawnGround("coyote_pelt", 1, world.Vec2{X: -2.5, Y: 1}, a)
	expectGround(t, "second spawned", r.stepGround(ca), acorn, pelt)

	// Pick up the acorns.
	ca.send(pickUp(acorn.ID))
	var got [][]groundEntry
	var invs []protocol.InventoryState
	for i := 0; i < 200 && len(got) == 0; i++ {
		r.w.Tick()
		f := r.collect(ca)
		got, invs = f.grounds, append(invs, f.invs...)
	}
	expectGround(t, "picked up", got, pelt)
	if len(invs) != 1 || countItem(invs[0], gamedata.ItemID(acorn.Item)) != 3 {
		t.Fatalf("InventoryState after the pick-up = %d frames, want one holding 3 acorns", len(invs))
	}
	waitFor(t, "pickup saved", func() bool { return len(r.store.callsWithReason(t, grounditems.ReasonPickup)) > 0 })
	calls := r.store.callsWithReason(t, grounditems.ReasonPickup)
	if len(calls) != 1 {
		t.Fatalf("%d transactions with pickup entries, want 1", len(calls))
	}
	for _, e := range calls[0] {
		if got := entryReason(t, e); got != grounditems.ReasonPickup {
			t.Errorf("entry %+v has reason %q, want %q", e, got, grounditems.ReasonPickup)
		}
	}
	sp, err := r.store.LoadByTokenHash(context.Background(), hashTok(tokA))
	if err != nil {
		t.Fatal(err)
	}
	if n := holdingsCount(sp.Holdings)[int32(acorn.Item)]; n != 3 {
		t.Fatalf("stored acorns = %d, want 3", n)
	}

	// Despawned.
	r.clock.Advance(r.data.Rules.DropDespawn())
	expectGround(t, "despawned", r.stepGround(ca))

	// Someone else's drop shows once its reservation ends.
	other := r.spawnGround("shiny_trinket", 2, world.Vec2{X: 0, Y: 3}, a+50)
	expectNoGround(t, "reserved for another player", r.stepGround(ca))
	r.clock.Advance(r.data.Rules.DropReservation())
	expectGround(t, "reservation ended", r.stepGround(ca), other)
	expectNoGround(t, "unchanged", r.stepGround(ca))

	// Reconnect with the same token: the new connection gets the list again.
	cb := r.dial(hello(v, tokA))
	if b, _, _, _, _ := joinFully(t, r.rig, cb); b != a {
		t.Fatalf("reconnected as %d, want %d", b, a)
	}
	expectGround(t, "reconnect", r.stepGround(cb), other)
	expectNoGround(t, "unchanged after reconnect", r.stepGround(cb))
}

// AC2: a drop reserved for another player is left out of a client's
// GroundItems until the reservation ends (Q2); its owner sees it at once.
func TestReservedDropHiddenFromOthers(t *testing.T) {
	r := newPvPRig(t, pvpRand{f: 0.999999})
	v := uint16(protocol.ProtocolVersionCurrent)
	ca, cb := r.dial(hello(v, "")), r.dial(hello(v, ""))
	a, _, _, _, _ := joinFully(t, r.rig, ca)
	b, _, _, _, _ := joinFully(t, r.rig, cb)
	for _, id := range []world.PlayerID{a, b} {
		r.w.Player(id).HasDesired = false
	}
	r.w.Tick()
	expectGround(t, "owner join", r.collect(ca).grounds)
	expectGround(t, "other join", r.collect(cb).grounds)

	drop := r.spawnGround("coyote_pelt", 1, world.Vec2{X: 1, Y: 1}, a)
	r.w.Tick()
	expectGround(t, "owner", r.collect(ca).grounds, drop)
	expectNoGround(t, "other player while reserved", r.collect(cb).grounds)

	// Just before the reservation ends it is still hidden.
	r.clock.Advance(r.data.Rules.DropReservation() - 1)
	r.w.Tick()
	expectNoGround(t, "owner, unchanged", r.collect(ca).grounds)
	expectNoGround(t, "other player just before the end", r.collect(cb).grounds)

	r.clock.Advance(1)
	r.w.Tick()
	expectNoGround(t, "owner, unchanged", r.collect(ca).grounds)
	expectGround(t, "other player after the reservation", r.collect(cb).grounds, drop)
}

// AC3: a PickUp with no message or naming ground item 0 is dropped without
// closing the connection.
func TestMalformedPickUpKeepsConnection(t *testing.T) {
	r := newPvPRig(t, pvpRand{f: 0.999999})
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	a, _, _, _, _ := joinFully(t, r.rig, c)
	r.w.Player(a).HasDesired = false
	drop := r.spawnGround("acorn", 1, world.Vec2{X: 0, Y: 0}, a)

	noMsg := func() []byte {
		b := flatbuffers.NewBuilder(32)
		protocol.EnvelopeStart(b)
		protocol.EnvelopeAddMsgType(b, protocol.MessagePickUp)
		b.Finish(protocol.EnvelopeEnd(b))
		return b.FinishedBytes()
	}()
	c.send(noMsg)
	c.send(pickUp(0))
	c.send(ping(9))
	r.waitFor(c, protocol.MessagePong) // still connected
	r.w.Tick()
	if _, ok := r.ground.Get(grounditems.ID(drop.ID)); !ok {
		t.Fatal("a malformed PickUp picked the item up")
	}

	c.send(pickUp(drop.ID))
	waitFor(t, "pick-up", func() bool {
		r.w.Tick()
		_, ok := r.ground.Get(grounditems.ID(drop.ID))
		return !ok
	})
}
