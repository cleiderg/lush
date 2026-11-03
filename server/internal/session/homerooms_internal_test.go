package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/homeroom"
	"github.com/cleiderg/lush/server/internal/systems/items"
	"github.com/cleiderg/lush/server/internal/world"
	roomgrid "github.com/cleiderg/lush/server/internal/world/housing"
)

// homeStore is a cardStore that logs home edits and lock changes, can fail
// the next edits, and checks after each edit that no item is both held and
// placed.
type homeStore struct {
	*cardStore
	hmu      sync.Mutex
	failEdit int
	chair    gamedata.ItemID
	bad      []string
	checked  int
}

func (h *homeStore) ApplyHomeEdit(ctx context.Context, e store.HomeEdit, entries []store.Entry) error {
	h.hmu.Lock()
	defer h.hmu.Unlock()
	if h.failEdit > 0 {
		h.failEdit--
		return errors.New("db down")
	}
	if err := h.Memory.ApplyHomeEdit(ctx, e, entries); err != nil {
		return err
	}
	if e.Holdings != nil {
		n := len(e.Items)
		for _, st := range e.Holdings.Inventory {
			if st.Item == int32(h.chair) {
				n += int(st.Qty)
			}
		}
		h.checked++
		if n != 1 {
			h.bad = append(h.bad, fmt.Sprintf("%d chairs after %d items placed", n, len(e.Items)))
		}
	}
	h.note("home")
	return nil
}

func (h *homeStore) SetHomeLocked(ctx context.Context, id int64, locked bool) error {
	if err := h.Memory.SetHomeLocked(ctx, id, locked); err != nil {
		return err
	}
	h.note("lock")
	return nil
}

type homeRig struct {
	*cardRig
	hs    *homeStore
	chair gamedata.ItemID
	sys   *homeroom.System
	p     *world.Player
	g     *guest
	d     *gamedata.Data
	door  world.Vec2
}

func newHomeRig(t *testing.T) *homeRig {
	t.Helper()
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	hs := &homeStore{cardStore: newCardStore()}
	logs := &logBuf{}
	w := world.New(world.DefaultConfig())
	clock := world.NewFakeClock(time.Unix(1000, 0))
	s := New(Config{Data: d}, w, clock, hs, slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(s.Close)
	r := &cardRig{econRig: &econRig{clock: clock, t: t, d: d, w: w, s: s, st: hs.econStore, logs: logs, hashes: map[*guest]string{}}, cs: hs.cardStore}
	h := &homeRig{cardRig: r, hs: hs, d: d}
	h.p, h.g = r.addPlayer("a")
	h.door = h.p.Pos
	ctx := context.Background()
	if err := hs.UpsertPlots(ctx, []store.Area{{ID: 1, Name: "Row"}}, []store.Plot{{ID: 1, AreaID: 1, X: h.door.X, Z: h.door.Y}}); err != nil {
		t.Fatal(err)
	}
	if _, err := hs.Memory.ClaimPlot(ctx, store.ClaimPlot{PlayerID: h.g.storeID, PlotID: 1, Rent: time.Hour, Now: clock.Now()}); err != nil {
		t.Fatal(err)
	}
	h.chair = h.item("wooden_chair")
	hs.chair = h.chair
	h.start(nil)
	return h
}

func (h *homeRig) item(key string) gamedata.ItemID {
	it, ok := h.d.ItemByKey(key)
	if !ok {
		h.t.Fatalf("no item %q", key)
	}
	return it.ID
}

// start builds the home room system from the store and registers it, as main
// does. A second call on a new Server is the restart of the test.
func (h *homeRig) start(reg func(*homeroom.System)) {
	h.t.Helper()
	ctx := context.Background()
	rooms, err := h.hs.LoadHomes(ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	cfg := homeroom.Config{Doors: map[homeroom.HomeID]world.Vec2{1: h.door}, Reach: 3, Clock: h.clock}
	h.sys = homeroom.New(h.d, cfg, h.s)
	for _, r := range rooms {
		if err := h.sys.AddHome(HomeSpec(r, h.g.id)); err != nil {
			h.t.Fatal(err)
		}
	}
	h.s.SetHomeRooms(h.sys, h.hs, rooms)
	for _, sys := range []world.System{items.New(h.d), h.sys, h.s} {
		if err := h.w.Register(sys); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *homeRig) give(key string) {
	h.t.Helper()
	ch, err := h.p.Inv.Add(h.d, h.item(key), 1)
	if err != nil {
		h.t.Fatal(err)
	}
	h.p.RecordEconomy("pickup", ch)
}

func (h *homeRig) slotOf(key string) int {
	for i, st := range h.p.Inv.Slots {
		if !st.Empty() && st.Item == h.item(key) {
			return i
		}
	}
	h.t.Fatalf("no %s in the inventory", key)
	return -1
}

func (h *homeRig) cmd(c world.Command) {
	h.clock.Advance(time.Second) // a new edit-rate window
	h.run(HomeCommand{c})
}

func (h *homeRig) enter() { h.cmd(homeroom.EnterCommand{Player: h.p.ID, Home: 1}) }

func (h *homeRig) place(key string, x, y int) {
	h.cmd(homeroom.PlaceCommand{Player: h.p.ID, Slot: h.slotOf(key), At: roomgrid.Placement{X: x, Y: y}})
}

func (h *homeRig) flush() {
	h.t.Helper()
	if err := h.saveAll(); err != nil {
		h.t.Fatal(err)
	}
}

// storedHome is the stored room and the stored count of the item.
func (h *homeRig) storedHome(item gamedata.ItemID) (store.HomeRoom, int) {
	h.t.Helper()
	rooms, err := h.hs.LoadHomes(context.Background())
	if err != nil || len(rooms) != 1 {
		h.t.Fatalf("homes %v, %v", rooms, err)
	}
	n := 0
	for _, st := range h.stored(h.g).Inventory {
		if st.Item == int32(item) {
			n += int(st.Qty)
		}
	}
	return rooms[0], n
}

func (h *homeRig) reasons(reason string) int {
	h.t.Helper()
	n := 0
	for _, e := range h.hs.Memory.AuditEntries() {
		if p, ok := e.Payload.(auditPayload); ok && p.Reason == reason {
			n++
		}
	}
	return n
}

// AC2 (#588): a shop buy followed by a place leaves the store with the
// place's holdings and the room, in that order, each written once; the place
// is audited with the homeroom reason.
func TestHomePlaceSavedAfterEarlierWritesWithHoldings(t *testing.T) {
	h := newHomeRig(t)
	h.enter()
	h.give("wooden_chair") // an earlier economy write
	h.place("wooden_chair", 2, 3)
	h.flush()
	room, n := h.storedHome(h.chair)
	if len(room.Items) != 1 || room.Items[0].X != 2 || room.Items[0].Y != 3 || n != 0 {
		t.Fatalf("stored room %+v, chairs held %d; want the chair placed and not held", room.Items, n)
	}
	if got := h.cs.log; len(got) != 2 || got[0] != "apply" || got[1] != "home" {
		t.Fatalf("writes %v, want the pick-up then the home edit", got)
	}
	if got := h.reasons(homeroom.ReasonPlace); got != 1 {
		t.Fatalf("%d %s audit entries, want 1", got, homeroom.ReasonPlace)
	}
}

// AC2 (#588): a failed home write is retried, not dropped and not doubled.
func TestHomeEditRetried(t *testing.T) {
	h := newHomeRig(t)
	h.enter()
	h.give("wooden_chair")
	h.hs.hmu.Lock()
	h.hs.failEdit = 1
	h.hs.hmu.Unlock()
	h.place("wooden_chair", 1, 1)
	h.waitSaverFailed("the home edit to fail", h.g.storeID)
	h.flush()
	room, n := h.storedHome(h.chair)
	if len(room.Items) != 1 || n != 0 {
		t.Fatalf("stored room %+v, held %d after the retry", room.Items, n)
	}
	if got := h.reasons(homeroom.ReasonPlace); got != 1 {
		t.Fatalf("%d place audit entries, want 1", got)
	}
}

// AC3 (#588): after several edits in one tick the store never holds a placed
// item in both the inventory and the room.
func TestHomeEditsInOneTickNeverDuplicate(t *testing.T) {
	h := newHomeRig(t)
	h.enter()
	h.give("wooden_chair")
	h.flush()
	slot := h.slotOf("wooden_chair")
	at := roomgrid.Placement{X: 1, Y: 1}
	h.run(HomeCommand{homeroom.PlaceCommand{Player: h.p.ID, Slot: slot, At: at}},
		HomeCommand{homeroom.RemoveCommand{Player: h.p.ID, Placed: 1}},
		HomeCommand{homeroom.PlaceCommand{Player: h.p.ID, Slot: slot, At: at}})
	h.flush()
	room, n := h.storedHome(h.chair)
	if len(room.Items) != 1 || n != 0 {
		t.Fatalf("stored room %+v, held %d; want one chair, placed", room.Items, n)
	}
	// Every write, not only the last, held the chair once: held + placed == 1.
	h.hs.hmu.Lock()
	defer h.hs.hmu.Unlock()
	if len(h.hs.bad) != 0 || h.hs.checked != 3 {
		t.Fatalf("%d writes checked, inconsistent: %v", h.hs.checked, h.hs.bad)
	}
}

// AC4 (#588): move, rotate, lock, wallpaper and floor are saved, and a
// restart on the same store loads the same room.
func TestHomeRoomSurvivesRestart(t *testing.T) {
	h := newHomeRig(t)
	h.enter()
	h.give("wooden_chair")
	h.give("white_wallpaper")
	h.give("dark_boards_floor")
	h.place("wooden_chair", 1, 1)
	h.cmd(homeroom.MoveCommand{Player: h.p.ID, Placed: 1, To: roomgrid.Placement{X: 4, Y: 5}})
	h.cmd(homeroom.RotateCommand{Player: h.p.ID, Placed: 1, Rot: 90})
	h.cmd(homeroom.SetWallpaperCommand{Player: h.p.ID, Slot: h.slotOf("white_wallpaper")})
	h.cmd(homeroom.SetFloorCommand{Player: h.p.ID, Slot: h.slotOf("dark_boards_floor")})
	h.cmd(homeroom.SetLockedCommand{Player: h.p.ID, Home: 1, Locked: true})
	h.flush()

	rooms, err := h.hs.LoadHomes(context.Background())
	if err != nil || len(rooms) != 1 {
		t.Fatal(rooms, err)
	}
	r := rooms[0]
	it := r.Items[0]
	if !r.Locked || r.WallpaperItem != int32(h.item("white_wallpaper")) || r.FlooringItem != int32(h.item("dark_boards_floor")) ||
		len(r.Items) != 1 || it.X != 4 || it.Y != 5 || it.Rotation != 1 {
		t.Fatalf("stored home %+v", r)
	}

	// A restart: a new home room system built from the store has the same state.
	sys := homeroom.New(h.d, homeroom.Config{Doors: map[homeroom.HomeID]world.Vec2{1: h.door}, Reach: 3, Clock: h.clock}, h.s)
	if err := sys.AddHome(HomeSpec(r, h.g.id)); err != nil {
		t.Fatal(err)
	}
	want, _ := h.sys.State(1)
	got, _ := sys.State(1)
	if len(got.Items) != 1 || got.Items[0] != want.Items[0] || got.Locked != want.Locked || got.Wallpaper != want.Wallpaper || got.Floor != want.Floor {
		t.Fatalf("restarted state %+v, want %+v", got, want)
	}
}
