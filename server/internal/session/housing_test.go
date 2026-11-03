package session_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/homeroom"
	"github.com/cleiderg/lush/server/internal/systems/housing"
	"github.com/cleiderg/lush/server/internal/world"
)

var housingStart = time.Unix(1_700_000_000, 0)

// housingStore is the in-memory store whose ClaimPlot and PayRent can fail.
type housingStore struct {
	*store.Memory
	mu   sync.Mutex
	fail bool
}

func (s *housingStore) setFail(v bool) { s.mu.Lock(); s.fail = v; s.mu.Unlock() }
func (s *housingStore) failing() bool  { s.mu.Lock(); defer s.mu.Unlock(); return s.fail }

func (s *housingStore) ClaimPlot(ctx context.Context, c store.ClaimPlot) (store.Plot, error) {
	if s.failing() {
		return store.Plot{}, errors.New("db down")
	}
	return s.Memory.ClaimPlot(ctx, c)
}

func (s *housingStore) PayRent(ctx context.Context, r store.PayRent) (store.Plot, error) {
	if s.failing() {
		return store.Plot{}, errors.New("db down")
	}
	return s.Memory.PayRent(ctx, r)
}

type housingRig struct {
	*rig
	d      *gamedata.Data
	st     *housingStore
	sys    *housing.System
	logs   *syncBuf
	before []protocol.Message // see homeCode
}

// month is the length of a rent month.
func (h *housingRig) month() time.Duration {
	return time.Duration(h.d.Rules.HouseDaysPerMonth) * 24 * time.Hour
}

// newHousingRig is a rig with two plots (1 and 2) at the spawn; owned maps a
// plot to the guest token that owns it, with rent paid for the given time
// from housingStart.
type ownedPlot struct {
	token string
	rent  time.Duration
}

func newHousingRig(t *testing.T, owned map[int32]ownedPlot, golds map[string]int64) *housingRig {
	t.Helper()
	d := testData(t)
	l := lookup(t)
	h := &housingRig{d: d, st: &housingStore{Memory: store.NewMemory()}, logs: &syncBuf{}}
	ctx := context.Background()
	spawn := l.Spawn()
	areas := []store.Area{{ID: 1, Name: "Row", X: spawn.X, Z: spawn.Y}}
	plots := []store.Plot{{ID: 1, AreaID: 1, X: spawn.X, Z: spawn.Y}, {ID: 2, AreaID: 1, X: spawn.X, Z: spawn.Y}}
	if err := h.st.UpsertPlots(ctx, areas, plots); err != nil {
		t.Fatal(err)
	}
	kit, _ := kitHoldings(t, d)
	ids := map[string]int64{}
	for tok, gold := range golds {
		k := kit
		k.Gold = gold
		sp, err := h.st.CreateGuest(ctx, tokenHash(tok), k, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids[tok] = sp.ID
	}
	for plot, o := range owned {
		_, err := h.st.Memory.ClaimPlot(ctx, store.ClaimPlot{PlayerID: ids[o.token], PlotID: plot, Rent: o.rent, Now: housingStart})
		if err != nil {
			t.Fatal(err)
		}
	}
	stored, err := h.st.LoadPlots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg := session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d, Spawn: spawn}
	log := slog.New(slog.NewTextHandler(h.logs, nil))
	var rg *rig
	rg = newRigAt(t, 1024, cfg, h.st, log, func(s *session.Server) []world.System {
		var hp []housing.Plot
		for _, sp := range stored {
			p := housing.Plot{ID: housing.HouseID(sp.ID), Door: world.Vec2{X: sp.X, Y: sp.Z}, PaidUntil: sp.RentPaidUntil}
			if sp.OwnerID != 0 {
				id, ok := s.ReserveOwner(sp.OwnerID)
				if !ok {
					t.Fatal("no id")
				}
				p.Owner = id
			}
			hp = append(hp, p)
		}
		sys, err := housing.NewSystem(d, housing.DefaultConfig(), hp, rigClock{&rg}, s, s)
		if err != nil {
			t.Fatal(err)
		}
		s.SetHousing(sys, h.st, stored)
		h.sys = sys
		doors := map[homeroom.HomeID]world.Vec2{}
		for _, sp := range stored {
			doors[homeroom.HomeID(sp.ID)] = world.Vec2{X: sp.X, Y: sp.Z}
		}
		rooms := homeroom.New(d, homeroom.Config{Doors: doors, Reach: 3, Clock: rigClock{&rg}}, s)
		stored, err := h.st.LoadHomes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, hr := range stored {
			owner, _ := s.ReserveOwner(hr.OwnerID)
			if err := rooms.AddHome(session.HomeSpec(hr, owner)); err != nil {
				t.Fatal(err)
			}
		}
		s.SetHomeRooms(rooms, h.st, stored)
		return []world.System{sys, rooms}
	}, housingStart)
	h.rig = rg
	return h
}

func (h *housingRig) storeID(tok string) int64 {
	h.t.Helper()
	p, err := h.st.LoadByTokenHash(context.Background(), tokenHash(tok))
	if err != nil {
		h.t.Fatal(err)
	}
	return p.ID
}

type houseView struct {
	id    uint16
	mine  bool
	state protocol.HouseState
	owner string
	paid  int64
}

type housingView struct {
	houses              []houseView
	rent                uint32
	claimMonths, maxPre byte
}

func readHousingState(tbl flatbuffers.Table) housingView {
	var hs protocol.HousingState
	hs.Init(tbl.Bytes, tbl.Pos)
	v := housingView{rent: hs.RentPerMonth(), claimMonths: hs.ClaimMonths(), maxPre: hs.MaxPrepaidMonths()}
	for i := 0; i < hs.HousesLength(); i++ {
		var hi protocol.HouseInfo
		hs.Houses(&hi, i)
		v.houses = append(v.houses, houseView{hi.Id(), hi.Mine(), hi.State(), string(hi.OwnerName()), hi.PaidUntil()})
	}
	return v
}

// join connects token and returns the client, its player id and the HousingState sent on join.
func (h *housingRig) join(tok string) (*client, world.PlayerID, housingView) {
	h.t.Helper()
	c := h.dial(hello(uint16(protocol.ProtocolVersionCurrent), tok))
	id, _ := h.expectWelcome(c)
	return c, world.PlayerID(id), readHousingState(h.waitFor(c, protocol.MessageHousingState))
}

func houseClaimMsg(id uint16) []byte {
	return envelope(protocol.MessageHouseClaim, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.HouseClaimStart(b)
		protocol.HouseClaimAddHouseId(b, id)
		return protocol.HouseClaimEnd(b)
	})
}

func rentPayMsg(id uint16, months byte) []byte {
	return envelope(protocol.MessageRentPay, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.RentPayStart(b)
		protocol.RentPayAddHouseId(b, id)
		protocol.RentPayAddMonths(b, months)
		return protocol.RentPayEnd(b)
	})
}

// result reads until the next HousingResult and returns its code.
func (h *housingRig) result(c *client) protocol.HousingResultCode {
	h.t.Helper()
	var r protocol.HousingResult
	tbl := h.waitFor(c, protocol.MessageHousingResult)
	r.Init(tbl.Bytes, tbl.Pos)
	return r.Code()
}

func (h *housingRig) flush() {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.sess.SaveAll(ctx); err != nil {
		h.t.Fatal(err)
	}
}

func (h *housingRig) storedPlot(id int32) store.Plot {
	h.t.Helper()
	ps, err := h.st.LoadPlots(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	for _, p := range ps {
		if p.ID == id {
			return p
		}
	}
	h.t.Fatalf("no plot %d", id)
	return store.Plot{}
}

// AC1 (#587): a HouseClaim at the door commits through ClaimPlot, replies Ok,
// takes the gold, and every connected player gets a HousingState with mine
// set only for the claimer.
func TestHouseClaimCommitsAndBroadcasts(t *testing.T) {
	h := newHousingRig(t, nil, map[string]int64{"a": 1000, "b": 1000})
	ca, ida, _ := h.join("a")
	cb, idb, _ := h.join("b")
	ca.send(houseClaimMsg(1))
	if code := h.result(ca); code != protocol.HousingResultCodeOk {
		t.Fatalf("claim: %v, want Ok", code)
	}
	if g := h.w.Player(ida).Inv.Gold; g != 600 {
		t.Fatalf("gold %d, want 600", g)
	}
	// Both players get the new list (A's may have come before the result).
	var views []housingView
	for _, c := range []*client{ca, cb} {
		var v housingView
		for i := 0; i < 20; i++ {
			v = readHousingState(h.waitFor(c, protocol.MessageHousingState))
			if v.houses[0].state != protocol.HouseStateEmpty {
				break
			}
		}
		views = append(views, v)
	}
	if !views[0].houses[0].mine || views[1].houses[0].mine || views[0].houses[1].mine || views[1].houses[1].mine {
		t.Fatalf("mine flags wrong: %+v", views)
	}
	_ = idb
	h.flush()
	if p := h.storedPlot(1); p.OwnerID != h.storeID("a") {
		t.Fatalf("stored owner %d, want %d", p.OwnerID, h.storeID("a"))
	}
	pl, _ := h.st.LoadByTokenHash(context.Background(), tokenHash("a"))
	if pl.Gold != 600 {
		t.Fatalf("stored gold %d, want 600", pl.Gold)
	}
	kinds := 0
	for _, e := range h.st.AuditEntries() {
		if e.Kind == store.HousingClaimKind {
			kinds++
		}
	}
	if kinds != 1 {
		t.Fatalf("%d claim audit entries, want 1", kinds)
	}
}

// AC2 (#587): a RentPay commits through PayRent; a store failure replies
// SaveFailed and leaves gold and paid-until unchanged in world and store.
func TestRentPayCommitAndFailure(t *testing.T) {
	h := newHousingRig(t, map[int32]ownedPlot{1: {"a", 3 * 30 * 24 * time.Hour}}, map[string]int64{"a": 1000})
	c, id, _ := h.join("a")
	before := h.storedPlot(1).RentPaidUntil

	h.st.setFail(true)
	c.send(rentPayMsg(1, 1))
	if code := h.result(c); code != protocol.HousingResultCodeSaveFailed {
		t.Fatalf("failed pay: %v, want SaveFailed", code)
	}
	h.flush()
	if g := h.w.Player(id).Inv.Gold; g != 1000 {
		t.Fatalf("world gold %d, want 1000", g)
	}
	pl, _ := h.st.LoadByTokenHash(context.Background(), tokenHash("a"))
	if pl.Gold != 1000 || !h.storedPlot(1).RentPaidUntil.Equal(before) {
		t.Fatalf("store changed: gold %d paid-until %v (was %v)", pl.Gold, h.storedPlot(1).RentPaidUntil, before)
	}
	for _, hi := range h.sys.List() {
		if hi.ID == 1 && !hi.PaidUntil.Equal(before) {
			t.Fatalf("world paid-until %v, want %v", hi.PaidUntil, before)
		}
	}

	h.st.setFail(false)
	c.send(rentPayMsg(1, 1))
	if code := h.result(c); code != protocol.HousingResultCodeOk {
		t.Fatalf("pay: %v, want Ok", code)
	}
	h.flush()
	pl, _ = h.st.LoadByTokenHash(context.Background(), tokenHash("a"))
	if pl.Gold != 800 || !h.storedPlot(1).RentPaidUntil.Equal(before.Add(h.month())) {
		t.Fatalf("after pay: gold %d paid-until %v", pl.Gold, h.storedPlot(1).RentPaidUntil)
	}
}

// AC3 (#587): the HousingState on join carries the rent terms from
// rules.json and marks the player's own house Warning inside the 7-day window.
func TestJoinHousingStateTermsAndWarning(t *testing.T) {
	h := newHousingRig(t, map[int32]ownedPlot{1: {"a", 30*24*time.Hour + 3*24*time.Hour}}, map[string]int64{"a": 1000, "b": 0})
	_, _, v := h.join("a")
	r := h.d.Rules
	if v.rent != uint32(r.HouseRentPerMonth) || int(v.claimMonths) != r.HouseClaimMonths || int(v.maxPre) != r.HouseMaxPrepaidMonths {
		t.Fatalf("terms %d/%d/%d, want rules.json", v.rent, v.claimMonths, v.maxPre)
	}
	if len(v.houses) != 2 || !v.houses[0].mine || v.houses[0].state != protocol.HouseStateWarning || v.houses[1].state != protocol.HouseStateEmpty {
		t.Fatalf("houses %+v, want own house 1 Warning and 2 Empty", v.houses)
	}
	_, _, vb := h.join("b")
	if vb.houses[0].mine || vb.houses[0].state != protocol.HouseStateWarning {
		t.Fatalf("other player's view %+v", vb.houses[0])
	}
}

// AC4 (#587): a lapse commit is answered as not saved and logged once, so the
// house stays owned in world and store.
func TestLapseCommitNotSaved(t *testing.T) {
	h := newHousingRig(t, map[int32]ownedPlot{1: {"a", 10 * 24 * time.Hour}}, map[string]int64{"a": 1000})
	for range 30 {
		h.w.Tick()
	}
	if n := strings.Count(h.logs.String(), "lapse is not wired"); n != 1 {
		t.Fatalf("logged %d times, want once:\n%s", n, h.logs.String())
	}
	if h.storedPlot(1).OwnerID != h.storeID("a") {
		t.Fatal("store lost the owner")
	}
	if hi := h.sys.List()[0]; hi.Owner == 0 {
		t.Fatal("world lost the owner")
	}
}

// A lapse while the owner is online must not leave their inventory locked:
// the unanswered lapse is not retried, so the owner can still use it, and the
// house stays owned in world and store.
func TestLapseLeavesOnlineOwnerInventoryUnlocked(t *testing.T) {
	h := newHousingRig(t, map[int32]ownedPlot{1: {"a", 10 * 24 * time.Hour}}, map[string]int64{"a": 1000})
	_, id, _ := h.join("a")
	for range 30 {
		h.w.Tick()
		if h.w.Player(id).Inv.Locked() {
			t.Fatal("owner's inventory locked by the unsaved lapse")
		}
	}
	if n := strings.Count(h.logs.String(), "lapse is not wired"); n != 1 {
		t.Fatalf("logged %d times, want once:\n%s", n, h.logs.String())
	}
	if h.storedPlot(1).OwnerID != h.storeID("a") {
		t.Fatal("store lost the owner")
	}
	if hi := h.sys.List()[0]; hi.Owner != id {
		t.Fatal("world lost the owner")
	}
}
