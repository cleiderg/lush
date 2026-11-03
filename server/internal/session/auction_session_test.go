package session_test

import (
	"context"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

const auctionLen = 5 * 24 * time.Hour // house_auction_days in rules.json

func auctionBidMsg(house uint16, amount uint32) []byte {
	return envelope(protocol.MessageAuctionBid, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.AuctionBidStart(b)
		protocol.AuctionBidAddHouseId(b, house)
		protocol.AuctionBidAddAmount(b, amount)
		return protocol.AuctionBidEnd(b)
	})
}

// openAuction1 opens an auction on plot 1 ending auctionLen after the start;
// prev, if set, is its previous owner's token.
func openAuction1(prev string) func(*housingStore, map[string]int64) {
	return func(st *housingStore, ids map[string]int64) {
		if err := st.OpenAuction(context.Background(), 1, ids[prev], housingStart.Add(auctionLen)); err != nil {
			panic(err)
		}
	}
}

func (h *housingRig) bidResult(c *client) protocol.AuctionResultCode {
	h.t.Helper()
	var r protocol.AuctionResult
	tbl := h.waitFor(c, protocol.MessageAuctionResult)
	r.Init(tbl.Bytes, tbl.Pos)
	return r.Code()
}

type auctionView struct {
	state        protocol.HouseState
	mine         bool
	top, my      uint32
	auctionEnd   int64
	hasOwnerName bool
}

func auctionViewOf(tbl flatbuffers.Table, id uint16) auctionView {
	var hs protocol.HousingState
	hs.Init(tbl.Bytes, tbl.Pos)
	var v auctionView
	for i := 0; i < hs.HousesLength(); i++ {
		var hi protocol.HouseInfo
		hs.Houses(&hi, i)
		if hi.Id() == id {
			v = auctionView{hi.State(), hi.Mine(), hi.TopBid(), hi.MyBid(), hi.AuctionEnd(), len(hi.OwnerName()) > 0}
		}
	}
	return v
}

// joinView connects token and returns the client, its player id and the view
// of house id in the HousingState sent on join.
func (h *housingRig) joinView(tok string, id uint16) (*client, world.PlayerID, auctionView) {
	h.t.Helper()
	c := h.dial(hello(uint16(protocol.ProtocolVersionCurrent), tok))
	pid, _ := h.expectWelcome(c)
	return c, world.PlayerID(pid), auctionViewOf(h.waitFor(c, protocol.MessageHousingState), id)
}

// stateOf reads HousingStates from c until house id satisfies ok.
func (h *housingRig) stateOf(c *client, id uint16, ok func(auctionView) bool) auctionView {
	h.t.Helper()
	var last auctionView
	for range 50 {
		last = auctionViewOf(h.waitFor(c, protocol.MessageHousingState), id)
		if ok(last) {
			return last
		}
	}
	h.t.Fatalf("house %d never reached the wanted state; last %+v", id, last)
	return last
}

func (h *housingRig) gold(tok string) int64 {
	h.t.Helper()
	p, err := h.st.LoadByTokenHash(context.Background(), tokenHash(tok))
	if err != nil {
		h.t.Fatal(err)
	}
	return p.Gold
}

// AC1 (#601): AuctionBid reaches the housing system and each refusal has its
// AuctionResultCode.
func TestAuctionBidRefusals(t *testing.T) {
	h := newHousingRigOn(t, nil, map[int32]ownedPlot{2: {"o", 90 * 24 * time.Hour}},
		map[string]int64{"prev": 1000, "o": 1000, "poor": 100, "a": 1000}, openAuction1("prev"))
	cases := []struct {
		tok    string
		house  uint16
		amount uint32
		want   protocol.AuctionResultCode
	}{
		{"a", 2, 300, protocol.AuctionResultCodeNotAuction},
		{"a", 9, 300, protocol.AuctionResultCodeNotAuction},
		{"prev", 1, 300, protocol.AuctionResultCodePreviousOwner},
		{"a", 1, 100, protocol.AuctionResultCodeBidTooLow},
		{"poor", 1, 200, protocol.AuctionResultCodeNotEnoughGold},
		{"o", 1, 300, protocol.AuctionResultCodeOwnsAnother},
		{"a", 1, 300, protocol.AuctionResultCodeOk},
		{"a", 1, 300, protocol.AuctionResultCodeBidTooLow}, // not above the top bid
	}
	clients := map[string]*client{}
	for _, tok := range []string{"prev", "o", "poor", "a"} {
		clients[tok], _, _ = h.join(tok)
	}
	for _, c := range cases {
		clients[c.tok].send(auctionBidMsg(c.house, c.amount))
		if got := h.bidResult(clients[c.tok]); got != c.want {
			t.Fatalf("%s bids %d on %d: %v, want %v", c.tok, c.amount, c.house, got, c.want)
		}
	}
}

// AC1 (#601): the housing outcomes and the wire codes share their values.
func TestAuctionOutcomeCodes(t *testing.T) {
	for o, code := range map[housing.AuctionOutcome]protocol.AuctionResultCode{
		housing.AuctionOk:            protocol.AuctionResultCodeOk,
		housing.AuctionNotAuction:    protocol.AuctionResultCodeNotAuction,
		housing.AuctionTooFar:        protocol.AuctionResultCodeTooFar,
		housing.AuctionPreviousOwner: protocol.AuctionResultCodePreviousOwner,
		housing.AuctionBidTooLow:     protocol.AuctionResultCodeBidTooLow,
		housing.AuctionNotEnoughGold: protocol.AuctionResultCodeNotEnoughGold,
		housing.AuctionOwnsAnother:   protocol.AuctionResultCodeOwnsAnother,
		housing.AuctionSaveFailed:    protocol.AuctionResultCodeSaveFailed,
	} {
		if uint8(o) != uint8(code) {
			t.Errorf("%v = %d, wire %v = %d", o, o, code, code)
		}
	}
}

// AC2 (#601): an accepted bid commits through PlaceAuctionBid, replies Ok,
// updates the gold of the bidder and of the online outbid player, and sends
// HousingState with my_bid only for each player's own bid. A store failure
// replies SaveFailed and changes nothing.
func TestAuctionBidCommitsAndBroadcasts(t *testing.T) {
	h := newHousingRigOn(t, nil, nil, map[string]int64{"a": 1000, "b": 1000}, openAuction1(""))
	ca, ida, _ := h.join("a")
	cb, idb, _ := h.join("b")

	ca.send(auctionBidMsg(1, 300))
	if got := h.bidResult(ca); got != protocol.AuctionResultCodeOk {
		t.Fatalf("a bids: %v", got)
	}
	if g := h.w.Player(ida).Inv.Gold; g != 700 {
		t.Fatalf("a gold %d, want 700", g)
	}
	va := h.stateOf(ca, 1, func(v auctionView) bool { return v.my == 300 })
	vb := h.stateOf(cb, 1, func(v auctionView) bool { return v.top == 300 })
	if va.state != protocol.HouseStateAuction || va.top != 300 || vb.my != 0 || va.auctionEnd != housingStart.Add(auctionLen).Unix() {
		t.Fatalf("views a %+v b %+v", va, vb)
	}

	cb.send(auctionBidMsg(1, 400))
	if got := h.bidResult(cb); got != protocol.AuctionResultCodeOk {
		t.Fatalf("b bids: %v", got)
	}
	va = h.stateOf(ca, 1, func(v auctionView) bool { return v.top == 400 })
	vb = h.stateOf(cb, 1, func(v auctionView) bool { return v.top == 400 })
	if va.my != 0 || vb.my != 400 {
		t.Fatalf("my_bid a %d b %d, want 0 and 400", va.my, vb.my)
	}
	if ga, gb := h.w.Player(ida).Inv.Gold, h.w.Player(idb).Inv.Gold; ga != 1000 || gb != 600 {
		t.Fatalf("gold a %d b %d, want 1000 and 600", ga, gb)
	}
	h.flush()
	if ga, gb := h.gold("a"), h.gold("b"); ga != 1000 || gb != 600 {
		t.Fatalf("stored gold a %d b %d, want 1000 and 600", ga, gb)
	}

	h.st.setFail(true)
	ca.send(auctionBidMsg(1, 500))
	if got := h.bidResult(ca); got != protocol.AuctionResultCodeSaveFailed {
		t.Fatalf("failed bid: %v, want SaveFailed", got)
	}
	h.st.setFail(false)
	h.flush()
	if g := h.w.Player(ida).Inv.Gold; g != 1000 {
		t.Fatalf("a gold %d after failed bid, want 1000", g)
	}
	au, err := h.st.LoadAuctions(context.Background())
	if err != nil || len(au) != 1 || au[0].Bids[0].Amount != 400 || h.gold("a") != 1000 {
		t.Fatalf("store changed by failed bid: %+v %v", au, err)
	}
}

// AC3 (#601): an ended auction commits through FinishAuction. The winner owns
// the house and has a home they can enter, losers get their held gold back.
func TestAuctionEndWinnerAndRefund(t *testing.T) {
	h := newHousingRigOn(t, nil, nil, map[string]int64{"a": 1000, "b": 1000}, openAuction1(""))
	ca, ida, _ := h.join("a")
	cb, idb, _ := h.join("b")
	ca.send(auctionBidMsg(1, 300))
	h.bidResult(ca)
	cb.send(auctionBidMsg(1, 500))
	h.bidResult(cb)
	h.stateOf(ca, 1, func(v auctionView) bool { return v.top == 500 })

	h.clock.Advance(auctionLen + time.Second)
	vb := h.stateOf(cb, 1, func(v auctionView) bool { return v.state == protocol.HouseStateOwned })
	if !vb.mine {
		t.Fatalf("winner's view %+v, want mine", vb)
	}
	va := h.stateOf(ca, 1, func(v auctionView) bool { return v.state == protocol.HouseStateOwned })
	if va.mine {
		t.Fatalf("loser's view %+v, want not mine", va)
	}
	h.flush()
	if ga, gb := h.w.Player(ida).Inv.Gold, h.w.Player(idb).Inv.Gold; ga != 1000 || gb != 100 {
		t.Fatalf("gold a %d b %d, want 1000 and 100 (bid 500 + 2 months' rent 400)", ga, gb)
	}
	if ga, gb := h.gold("a"), h.gold("b"); ga != 1000 || gb != 100 {
		t.Fatalf("stored gold a %d b %d", ga, gb)
	}
	if p := h.storedPlot(1); p.OwnerID != h.storeID("b") {
		t.Fatalf("stored owner %d, want b", p.OwnerID)
	}
	for i := 0; ; i++ {
		if got := h.ask(cb, homeEnterMsg(1)); got == protocol.HomeResultCodeOk {
			break
		} else if i > 50 {
			t.Fatalf("winner enters their home: %v", got)
		}
	}
}

// AC3 (#601): an auction with no taker leaves the house Empty and claimable.
func TestAuctionEndWithoutBidsLeavesHouseClaimable(t *testing.T) {
	h := newHousingRigOn(t, nil, nil, map[string]int64{"a": 1000}, openAuction1(""))
	ca, _, _ := h.join("a")
	h.clock.Advance(auctionLen + time.Second)
	h.stateOf(ca, 1, func(v auctionView) bool { return v.state == protocol.HouseStateEmpty })
	ca.send(houseClaimMsg(1))
	if code := h.result(ca); code != protocol.HousingResultCodeOk {
		t.Fatalf("claim: %v, want Ok", code)
	}
}

// AC4 (#601): a server restarted on the same store loads open auctions and
// held bids.
func TestAuctionsLoadedAtStart(t *testing.T) {
	h := newHousingRigOn(t, nil, nil, map[string]int64{"a": 1000, "b": 1000}, openAuction1(""))
	ca, _, _ := h.join("a")
	ca.send(auctionBidMsg(1, 300))
	h.bidResult(ca)
	h.flush()

	h2 := newHousingRigOn(t, h.st, nil, nil, nil)
	var info housing.Info
	for _, i := range h2.sys.List() {
		if i.ID == 1 {
			info = i
		}
	}
	if info.State != housing.InAuction || info.TopBid != 300 || !info.AuctionEnd.Equal(housingStart.Add(auctionLen)) {
		t.Fatalf("house 1 after restart: %+v", info)
	}
	_, ida2, va := h2.joinView("a", 1)
	if va.state != protocol.HouseStateAuction || va.my != 300 || va.top != 300 {
		t.Fatalf("a's view after restart %+v, want my_bid 300", va)
	}
	if g := h2.w.Player(ida2).Inv.Gold; g != 700 {
		t.Fatalf("a gold %d after restart, want 700", g)
	}
	if _, _, vb := h2.joinView("b", 1); vb.my != 0 || vb.top != 300 {
		t.Fatalf("b's view %+v", vb)
	}
}
