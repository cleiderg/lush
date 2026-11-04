package session

import (
	"context"
	"fmt"
	"math"
	"sync"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/systems/homeroom"
	"github.com/cleiderg/lush/server/internal/world"
	roomgrid "github.com/cleiderg/lush/server/internal/world/housing"
)

// HomeCommand is a client home message (HomeEnter..HomeSetOpen). Handle passes
// the command inside it to the home room system and saves what it changed; the
// system itself ignores it. A home's id is its plot's id.
type HomeCommand struct{ world.Command }

// newHome is a home created by a committed claim, waiting to be added to the
// home room system on the tick.
type newHome struct {
	plot  homeroom.HomeID
	store int64
	owner world.PlayerID
}

// homeRooms is the session's side of the home room system.
type homeRooms struct {
	sys   *homeroom.System
	homes store.Housing
	ids   map[homeroom.HomeID]int64 // the store's home id of each home; tick goroutine
	last  homeroom.Result           // result of the command being handled

	mu  sync.Mutex
	add []newHome // from the saver goroutine
}

// SetHomeRooms connects the home room system: HomeCommands go to it, every
// accepted edit is saved with the owner's holdings through ApplyHomeEdit, and
// a house claimed later gets its home added. The system must have been
// created with the Server as its Sink and registered on the world (for its
// Tick), and rooms are the homes it was given. Call it before the world
// starts ticking.
func (s *Server) SetHomeRooms(sys *homeroom.System, homes store.Housing, rooms []store.HomeRoom) {
	ids := make(map[homeroom.HomeID]int64, len(rooms))
	for _, r := range rooms {
		ids[homeroom.HomeID(r.PlotID)] = r.ID
	}
	s.rooms = &homeRooms{sys: sys, homes: homes, ids: ids}
}

// HomeSpec is the home room system's spec for a stored home owned by the
// session player owner.
func HomeSpec(r store.HomeRoom, owner world.PlayerID) homeroom.Spec {
	sp := homeroom.Spec{ID: homeroom.HomeID(r.PlotID), Owner: owner, Locked: r.Locked,
		Wallpaper: gamedata.ItemID(r.WallpaperItem), Floor: gamedata.ItemID(r.FlooringItem)}
	for _, it := range r.Items {
		pl := roomgrid.Placement{X: int(it.X), Y: int(it.Y), Rot: roomgrid.Rotation(int(it.Rotation) * 90),
			Parent: roomgrid.ID(it.ParentID), Slot: int(it.Slot), Side: roomgrid.Side(it.Side)}
		if roomgrid.Layer(it.Layer) == roomgrid.LayerWall {
			pl.Pos, pl.X, pl.Y = int(it.X), 0, 0
		}
		sp.Items = append(sp.Items, homeroom.PlacedItem{ID: roomgrid.ID(it.ID), Item: gamedata.ItemID(it.Item), Placement: pl, Layer: roomgrid.Layer(it.Layer)})
	}
	return sp
}

// storeItems is the room as the store keeps it.
func storeItems(st homeroom.State) []store.PlacedItem {
	out := make([]store.PlacedItem, len(st.Items))
	for i, it := range st.Items {
		x, y := it.X, it.Y
		if it.Layer == roomgrid.LayerWall {
			x, y = it.Pos, 0
		}
		out[i] = store.PlacedItem{ID: int64(it.ID), Item: int32(it.Item), X: int16(x), Y: int16(y),
			Rotation: uint8(int(it.Rot) / 90), Layer: uint8(it.Layer), ParentID: int64(it.Parent),
			Slot: uint8(max(it.Slot, 0)), Side: uint8(it.Side)}
	}
	return out
}

// handleHome runs one home command. The player's earlier economy events are
// queued first, then, if the command changed the room, one write saves the
// holdings and the room together (ApplyHomeEdit), so the queue keeps the
// order of the player's writes and a placed item is never in both the stored
// inventory and the stored room. Called on the tick goroutine.
func (s *Server) handleHome(cmd world.Command, p *world.Player) {
	r := s.rooms
	g := s.live[p.ID]
	if r == nil || g == nil {
		return
	}
	earlier := s.appendEconomy(nil, g, p)
	r.last = homeroom.NoHome
	r.sys.Handle(cmd, p)
	if r.last == homeroom.Ok {
		switch c := cmd.(type) {
		case homeroom.SetLockedCommand:
			if id, ok := r.ids[c.Home]; ok {
				locked, homes := c.Locked, r.homes
				earlier = append(earlier, econReq{changes: []store.EconomyChange{{PlayerID: g.storeID}},
					write: func(ctx context.Context) error { return homes.SetHomeLocked(ctx, id, locked) }})
			}
		case homeroom.PlaceCommand, homeroom.MoveCommand, homeroom.RotateCommand, homeroom.RemoveCommand,
			homeroom.SetWallpaperCommand, homeroom.SetFloorCommand:
			if req, ok := s.homeEdit(g, p); ok {
				earlier = append(earlier, req)
			}
		}
	}
	s.saver.submitEconomy(earlier...)
}

// homeEdit builds the write for the edit just applied: the room as it is now
// and, if the edit moved items or gold, the holdings it left and its audit
// entries. It reports false if nothing can be saved.
func (s *Server) homeEdit(g *guest, p *world.Player) (econReq, bool) {
	r := s.rooms
	hid, ok := r.sys.InHome(p.ID)
	if !ok {
		return econReq{}, false
	}
	st, _ := r.sys.State(hid)
	sid, ok := r.ids[hid]
	if !ok {
		s.log.Error("home edit: home has no store id", "home", hid, "player", g.id)
		return econReq{}, false
	}
	e := store.HomeEdit{HomeID: sid, OwnerID: g.storeID, Items: storeItems(st),
		WallpaperItem: int32(st.Wallpaper), FlooringItem: int32(st.Floor)}
	var entries []store.Entry
	for _, ev := range p.DrainEconomy() {
		h, err := toHoldings(ev.Inv)
		if err != nil {
			s.log.Error("home edit: economy event", "player", g.id, "reason", ev.Reason, "err", err)
			continue
		}
		e.Holdings = &h
		for _, en := range auditEntries(ev.Changes, ev.Reason) {
			en.Actor = g.storeID
			entries = append(entries, en)
		}
	}
	homes := r.homes
	return econReq{changes: []store.EconomyChange{{PlayerID: g.storeID}},
		write: func(ctx context.Context) error { return homes.ApplyHomeEdit(ctx, e, entries) }}, true
}

// homeClaimed records the home that a committed claim created, so that the
// next tick adds it to the home room system. Saver goroutine.
func (s *Server) homeClaimed(ctx context.Context, g *guest, plot int32) error {
	return s.homeCreated(ctx, g.storeID, g.id, plot)
}

// homeCreated is homeClaimed for the stored player storeID, whose session id
// is owner (a house winner may be offline). Saver goroutine.
func (s *Server) homeCreated(ctx context.Context, storeID int64, owner world.PlayerID, plot int32) error {
	if s.rooms == nil {
		return nil
	}
	h, err := s.rooms.homes.LoadHome(ctx, storeID)
	if err != nil {
		return fmt.Errorf("load new home: %w", err)
	}
	s.rooms.mu.Lock()
	s.rooms.add = append(s.rooms.add, newHome{plot: homeroom.HomeID(plot), store: h.ID, owner: owner})
	s.rooms.mu.Unlock()
	return nil
}

// addClaimedHomes adds the homes of committed claims. Tick goroutine.
func (s *Server) addClaimedHomes() {
	r := s.rooms
	if r == nil {
		return
	}
	r.mu.Lock()
	add := r.add
	r.add = nil
	r.mu.Unlock()
	for _, h := range add {
		if err := r.sys.AddHome(homeroom.Spec{ID: h.plot, Owner: h.owner}); err != nil {
			s.log.Error("add claimed home", "home", h.plot, "err", err)
			continue
		}
		r.ids[h.plot] = h.store
	}
}

var homeCodes = map[homeroom.Result]protocol.HomeResultCode{
	homeroom.Ok:             protocol.HomeResultCodeOk,
	homeroom.NoHome:         protocol.HomeResultCodeTooFar, // no door to reach
	homeroom.Locked:         protocol.HomeResultCodeLocked,
	homeroom.Full:           protocol.HomeResultCodeFull,
	homeroom.TooFar:         protocol.HomeResultCodeTooFar,
	homeroom.NotOwner:       protocol.HomeResultCodeNotOwner,
	homeroom.NotInside:      protocol.HomeResultCodeNotInHome,
	homeroom.AlreadyInside:  protocol.HomeResultCodeOk, // they are in a home; nothing is refused or changed
	homeroom.BadItem:        protocol.HomeResultCodeNotInInventory,
	homeroom.InventoryFull:  protocol.HomeResultCodeInventoryFull,
	homeroom.Busy:           protocol.HomeResultCodeSaveFailed, // the inventory is locked; nothing changed
	homeroom.OutOfBounds:    protocol.HomeResultCodeOutOfBounds,
	homeroom.Overlap:        protocol.HomeResultCodeOverlap,
	homeroom.BadRotation:    protocol.HomeResultCodeBadRotation,
	homeroom.BadLayer:       protocol.HomeResultCodeBadLayer,
	homeroom.NotSurfaceItem: protocol.HomeResultCodeNotSurfaceItem,
	homeroom.NoSurface:      protocol.HomeResultCodeNoSurface,
	homeroom.SurfaceFull:    protocol.HomeResultCodeSurfaceFull,
	homeroom.HasItems:       protocol.HomeResultCodeHasItems,
	homeroom.NotFound:       protocol.HomeResultCodeNotFound,
	homeroom.Exists:         protocol.HomeResultCodeOverlap, // the id is taken: the spot is
	homeroom.RateLimited:    protocol.HomeResultCodeRateLimited,
}

// HomeResult implements homeroom.Sink. Called on the tick goroutine.
func (s *Server) HomeResult(to world.PlayerID, r homeroom.Result) {
	if s.rooms != nil {
		s.rooms.last = r
	}
	g := s.live[to]
	if g == nil || g.conn == nil {
		return
	}
	code, ok := homeCodes[r]
	if !ok {
		code = protocol.HomeResultCodeSaveFailed
	}
	b := flatbuffers.NewBuilder(32)
	protocol.HomeResultStart(b)
	protocol.HomeResultAddCode(b, code)
	g.conn.Send(finish(b, protocol.MessageHomeResult, protocol.HomeResultEnd(b)))
}

// HomeState implements homeroom.Sink. Called on the tick goroutine.
func (s *Server) HomeState(to world.PlayerID, st homeroom.State) {
	if g := s.live[to]; g != nil && g.conn != nil {
		g.conn.Send(encodeHomeState(s.bld, st))
	}
}

// HomeLeft implements homeroom.Sink. The client leaves the room on the
// HomeResult of its HomeLeave, so there is nothing more to send.
func (s *Server) HomeLeft(world.PlayerID, homeroom.HomeID) {}

func encodeHomeState(b *flatbuffers.Builder, st homeroom.State) []byte {
	b.Reset()
	var name flatbuffers.UOffsetT
	if st.Owner != 0 {
		name = b.CreateString(fmt.Sprintf("Guest %d", st.Owner)) // PLACEHOLDER: players have no names yet
	}
	offs := make([]flatbuffers.UOffsetT, len(st.Items))
	for i, it := range st.Items {
		x, y := it.X, it.Y
		if it.Layer == roomgrid.LayerWall {
			x, y = it.Pos, 0
		}
		protocol.PlacedItemStart(b)
		protocol.PlacedItemAddPlacedId(b, clampU32(int(it.ID)))
		protocol.PlacedItemAddItemId(b, uint16(it.Item))
		protocol.PlacedItemAddX(b, clampU8(x))
		protocol.PlacedItemAddY(b, clampU8(y))
		protocol.PlacedItemAddRotation(b, clampU8(int(it.Rot)/90))
		protocol.PlacedItemAddLayer(b, uint8(it.Layer))
		protocol.PlacedItemAddParentPlacedId(b, clampU32(int(it.Parent)))
		protocol.PlacedItemAddSlot(b, clampU8(it.Slot))
		protocol.PlacedItemAddSide(b, uint8(it.Side))
		offs[i] = protocol.PlacedItemEnd(b)
	}
	protocol.HomeStateStartItemsVector(b, len(offs))
	for i := len(offs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(offs[i])
	}
	vec := b.EndVector(len(offs))
	protocol.HomeStateStart(b)
	protocol.HomeStateAddHouseId(b, uint16(min(uint32(st.Home), math.MaxUint16)))
	if st.Owner != 0 {
		protocol.HomeStateAddOwnerName(b, name)
	}
	protocol.HomeStateAddWallpaperItemId(b, uint16(st.Wallpaper))
	protocol.HomeStateAddFloorItemId(b, uint16(st.Floor))
	protocol.HomeStateAddItems(b, vec)
	return finish(b, protocol.MessageHomeState, protocol.HomeStateEnd(b))
}

// setOpenCommand is HomeSetOpen before the home is known.
type setOpenCommand struct {
	Player world.PlayerID
	Locked bool
}

func (c setOpenCommand) Target() world.PlayerID { return c.Player }

// homeCommand is the command for the home room system. HomeSetOpen names no
// home: it applies to the one the player is inside, or fails as not inside.
func (h HomeCommand) homeCommand(s *Server) world.Command {
	c, ok := h.Command.(setOpenCommand)
	if !ok {
		return h.Command
	}
	if s.rooms != nil {
		if id, in := s.rooms.sys.InHome(c.Player); in {
			return homeroom.SetLockedCommand{Player: c.Player, Home: id, Locked: c.Locked}
		}
	}
	return homeroom.LeaveCommand{Player: c.Player}
}
