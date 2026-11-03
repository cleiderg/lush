package session_test

import (
	"slices"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/systems/homeroom"
	"github.com/cleiderg/lush/server/internal/world"
	roomgrid "github.com/cleiderg/lush/server/internal/world/housing"
)

func homeEnterMsg(id uint16) []byte {
	return envelope(protocol.MessageHomeEnter, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.HomeEnterStart(b)
		protocol.HomeEnterAddHouseId(b, id)
		return protocol.HomeEnterEnd(b)
	})
}

func homeLeaveMsg() []byte {
	return envelope(protocol.MessageHomeLeave, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.HomeLeaveStart(b)
		return protocol.HomeLeaveEnd(b)
	})
}

func homeSetOpenMsg(open bool) []byte {
	return envelope(protocol.MessageHomeSetOpen, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.HomeSetOpenStart(b)
		protocol.HomeSetOpenAddOpen(b, open)
		return protocol.HomeSetOpenEnd(b)
	})
}

// homeCode reads until the next HomeResult and returns its code. The types of
// the messages before it, a HomeState among them, are left in h.before.
func (h *housingRig) homeCode(c *client) protocol.HomeResultCode {
	h.t.Helper()
	h.before = nil
	for i := 0; i < 1000; i++ {
		typ, tbl := h.next(c)
		if typ == protocol.MessageHomeResult {
			var r protocol.HomeResult
			r.Init(tbl.Bytes, tbl.Pos)
			return r.Code()
		}
		h.before = append(h.before, typ)
	}
	h.t.Fatal("never saw a HomeResult")
	return 0
}

func hasMsg(got []protocol.Message, typ protocol.Message) bool { return slices.Contains(got, typ) }

// ask sends buf, ticks and returns the HomeResult code.
func (h *housingRig) ask(c *client, buf []byte) protocol.HomeResultCode {
	h.t.Helper()
	c.send(buf)
	return h.homeCode(c)
}

// AC1 (#588): HomeEnter, HomeLeave and HomeSetOpen reach the system and each
// refusal has its HomeResultCode; HomeState goes to the players inside the
// home and to no one outside it.
func TestHomeEnterLeaveOpenAndState(t *testing.T) {
	h := newHousingRig(t, map[int32]ownedPlot{1: {"a", 60 * 24 * time.Hour}}, map[string]int64{"a": 1000, "b": 1000, "c": 1000})
	ca, _, _ := h.join("a")
	cb, _, _ := h.join("b")
	cc, _, _ := h.join("c")

	if got := h.ask(ca, homeLeaveMsg()); got != protocol.HomeResultCodeNotInHome {
		t.Fatalf("leave outside: %v, want NotInHome", got)
	}
	if got := h.ask(ca, homeSetOpenMsg(false)); got != protocol.HomeResultCodeNotInHome {
		t.Fatalf("lock outside: %v, want NotInHome", got)
	}
	if got := h.ask(cb, homeEnterMsg(7)); got != protocol.HomeResultCodeTooFar {
		t.Fatalf("enter a missing home: %v, want TooFar", got)
	}
	if got := h.ask(ca, homeEnterMsg(1)); got != protocol.HomeResultCodeOk {
		t.Fatalf("owner enters: %v", got)
	}
	if !hasMsg(h.before, protocol.MessageHomeState) {
		t.Fatalf("owner entered without a HomeState: %v", h.before)
	}
	if got := h.ask(cb, homeEnterMsg(1)); got != protocol.HomeResultCodeOk {
		t.Fatalf("visitor enters an open home: %v", got)
	}
	if !hasMsg(h.before, protocol.MessageHomeState) {
		t.Fatalf("visitor entered without a HomeState: %v", h.before)
	}
	if got := h.ask(cb, homeSetOpenMsg(false)); got != protocol.HomeResultCodeNotOwner {
		t.Fatalf("visitor locks: %v, want NotOwner", got)
	}
	// Locking changes the room for those inside: a and b get a HomeState, c does not.
	if got := h.ask(ca, homeSetOpenMsg(false)); got != protocol.HomeResultCodeOk {
		t.Fatalf("owner locks: %v", got)
	}
	if !hasMsg(h.before, protocol.MessageHomeState) {
		t.Fatal("locking did not send a HomeState to the owner")
	}
	h.waitFor(cb, protocol.MessageHomeState)
	if got := h.ask(cc, homeEnterMsg(1)); got != protocol.HomeResultCodeLocked {
		t.Fatalf("enter a locked home: %v, want Locked", got)
	}
	if hasMsg(h.before, protocol.MessageHomeState) {
		t.Fatal("a refused visitor got a HomeState")
	}
	if got := h.ask(cb, homeLeaveMsg()); got != protocol.HomeResultCodeOk {
		t.Fatalf("leave: %v", got)
	}
	if got := h.ask(ca, homeSetOpenMsg(true)); got != protocol.HomeResultCodeOk {
		t.Fatalf("owner unlocks: %v", got)
	}
	// b left: the unlock sent them no HomeState, which would come before this.
	if got := h.ask(cb, homeLeaveMsg()); got != protocol.HomeResultCodeNotInHome || hasMsg(h.before, protocol.MessageHomeState) {
		t.Fatalf("after leaving: %v, before %v; want NotInHome and no HomeState", got, h.before)
	}
	// c never entered: no HomeState at all, however the room changed.
	if got := h.ask(cc, homeLeaveMsg()); got != protocol.HomeResultCodeNotInHome || hasMsg(h.before, protocol.MessageHomeState) {
		t.Fatalf("outside: %v, before %v; want NotInHome and no HomeState", got, h.before)
	}
	h.flush()
	rooms, _ := h.st.LoadHomes(t.Context())
	if len(rooms) != 1 || rooms[0].Locked {
		t.Fatalf("stored homes %+v, want one, open", rooms)
	}
}

// AC4 (#588): after a HouseClaim commits, the new owner's empty home is added
// to the home room system and they can enter it.
func TestClaimedHomeCanBeEntered(t *testing.T) {
	h := newHousingRig(t, nil, map[string]int64{"a": 1000})
	ca, _, _ := h.join("a")
	if got := h.ask(ca, homeEnterMsg(1)); got != protocol.HomeResultCodeTooFar {
		t.Fatalf("enter before claiming: %v, want TooFar", got)
	}
	ca.send(houseClaimMsg(1))
	if code := h.result(ca); code != protocol.HousingResultCodeOk {
		t.Fatalf("claim: %v", code)
	}
	for i := 0; ; i++ {
		if got := h.ask(ca, homeEnterMsg(1)); got == protocol.HomeResultCodeOk {
			break
		} else if i > 50 {
			t.Fatalf("enter after claiming: %v", got)
		}
	}
	if !hasMsg(h.before, protocol.MessageHomeState) {
		t.Fatalf("entered without a HomeState: %v", h.before)
	}
}

// AC1 (#588): every home message decodes to its command.
func TestDecodeHomeMessages(t *testing.T) {
	place := envelope(protocol.MessageHomePlace, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.HomePlaceStart(b)
		protocol.HomePlaceAddInventorySlot(b, 3)
		protocol.HomePlaceAddX(b, 2)
		protocol.HomePlaceAddY(b, 5)
		protocol.HomePlaceAddRotation(b, 1)
		return protocol.HomePlaceEnd(b)
	})
	d, err := session.DecodeForTest(place)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := d.Command(4).(session.HomeCommand).Command.(homeroom.PlaceCommand)
	if !ok || c.Slot != 3 || c.At.X != 2 || c.At.Y != 5 || c.At.Rot != roomgrid.Rotation(90) || c.Player != world.PlayerID(4) {
		t.Fatalf("decoded %+v", d.Command(4))
	}
}
