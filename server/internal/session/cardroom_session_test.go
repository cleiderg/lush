package session_test

import (
	"reflect"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/cardroom"
	"github.com/cleiderg/lush/server/internal/systems/cardtable/shed"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

// cardWire is a websocket rig with the card room built from game data, as
// the server binary does, with the session as its sink and committer.
// Players spawn at table 1; turn timers are an hour so no turn times out.
type cardWire struct {
	*rig
	cards *cardroom.System
	data  *gamedata.Data
}

const wireTable = cardroom.TableID(1)

func newCardWire(t *testing.T) *cardWire {
	t.Helper()
	d := testData(t)
	for i := range d.CardTables {
		d.CardTables[i].TurnTimerSeconds = 3600
	}
	pos, err := cardroom.Positions()
	if err != nil {
		t.Fatal(err)
	}
	c := cfg()
	c.Data, c.Spawn = d, pos[wireTable]
	cw := &cardWire{data: d}
	cw.rig = newRigSystems(t, 1024, c, store.NewMemory(), nil, func(s *session.Server) []world.System {
		cards, err := cardroom.FromData(d, world.New(world.DefaultConfig()).TickInterval(), s, s)
		if err != nil {
			t.Fatal(err)
		}
		s.SetCardRoom(cards)
		cw.cards = cards
		return []world.System{cards}
	})
	return cw
}

func cardSit(table uint16) []byte {
	return envelope(protocol.MessageCardTableSit, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.CardTableSitStart(b)
		protocol.CardTableSitAddTableId(b, table)
		return protocol.CardTableSitEnd(b)
	})
}

func cardLeave() []byte {
	return envelope(protocol.MessageCardTableLeave, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.CardTableLeaveStart(b)
		return protocol.CardTableLeaveEnd(b)
	})
}

func cardReady() []byte {
	return envelope(protocol.MessageCardTableReady, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.CardTableReadyStart(b)
		return protocol.CardTableReadyEnd(b)
	})
}

func shedDraw() []byte {
	return envelope(protocol.MessageShedDraw, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShedDrawStart(b)
		return protocol.ShedDrawEnd(b)
	})
}

func shedPlay(c shed.Card, chosen shed.Colour) []byte {
	return envelope(protocol.MessageShedPlay, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShedPlayStart(b)
		protocol.ShedPlayAddCard(b, protocol.CreateShedCard(b, protocol.ShedColour(c.Colour), protocol.ShedSymbol(c.Symbol)))
		protocol.ShedPlayAddChosenColour(b, protocol.ShedColour(chosen))
		return protocol.ShedPlayEnd(b)
	})
}

// join connects a new guest and returns its id and token.
func (r *cardWire) join() (world.PlayerID, string, *client) {
	r.t.Helper()
	c := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
	id, tok := r.expectWelcome(c)
	return world.PlayerID(id), tok, c
}

// result reads until a CardTableResult arrives and returns its code.
func (r *cardWire) result(c *client) protocol.CardTableResultCode {
	r.t.Helper()
	tbl := r.waitFor(c, protocol.MessageCardTableResult)
	var res protocol.CardTableResult
	res.Init(tbl.Bytes, tbl.Pos)
	return res.Code()
}

func (r *cardWire) expectResult(c *client, what string, want protocol.CardTableResultCode) {
	r.t.Helper()
	if got := r.result(c); got != want {
		r.t.Fatalf("%s: result %v, want %v", what, protocol.EnumNamesCardTableResultCode[got], protocol.EnumNamesCardTableResultCode[want])
	}
}

type wireState struct {
	table      uint16
	seats      []uint16
	ready      []bool
	inProgress bool
	hasWinner  bool
	winner     byte
}

// state reads until a CardTableState arrives.
func (r *cardWire) state(c *client) wireState {
	r.t.Helper()
	tbl := r.waitFor(c, protocol.MessageCardTableState)
	var st protocol.CardTableState
	st.Init(tbl.Bytes, tbl.Pos)
	ws := wireState{table: st.TableId(), inProgress: st.InProgress(), hasWinner: st.HasWinner(), winner: st.WinnerSeat()}
	for i := 0; i < st.SeatsLength(); i++ {
		ws.seats = append(ws.seats, st.Seats(i))
	}
	for i := 0; i < st.ReadyLength(); i++ {
		ws.ready = append(ws.ready, st.Ready(i))
	}
	return ws
}

// stateWhere reads CardTableStates until one satisfies ok.
func (r *cardWire) stateWhere(c *client, what string, ok func(wireState) bool) wireState {
	r.t.Helper()
	for i := 0; i < 50; i++ {
		if st := r.state(c); ok(st) {
			return st
		}
	}
	r.t.Fatalf("never got a CardTableState with %s", what)
	return wireState{}
}

// hand reads until a ShedHand arrives.
func (r *cardWire) hand(c *client) (uint16, []shed.Card) {
	r.t.Helper()
	tbl := r.waitFor(c, protocol.MessageShedHand)
	var h protocol.ShedHand
	h.Init(tbl.Bytes, tbl.Pos)
	var out []shed.Card
	var sc protocol.ShedCard
	for i := 0; i < h.CardsLength(); i++ {
		h.Cards(&sc, i)
		out = append(out, shed.Card{Colour: shed.Colour(sc.Colour()), Symbol: shed.Symbol(sc.Symbol())})
	}
	return h.TableId(), out
}

// serverHand is p's hand as the card room holds it.
func (r *cardWire) serverHand(p world.PlayerID) []shed.Card {
	tb, _ := r.cards.Table(wireTable)
	h, _ := tb.View(p).Private.([]shed.Card)
	return h
}

// typesUntilSnapshot reads c's messages up to the first Snapshot of a tick
// after the current one and returns their types.
func (r *cardWire) typesUntilSnapshot(c *client) []protocol.Message {
	r.t.Helper()
	from := uint32(r.w.TickCount()) + 1
	var types []protocol.Message
	for i := 0; i < 1000; i++ {
		typ, tbl := r.next(c)
		if typ == protocol.MessageSnapshot {
			var s protocol.Snapshot
			s.Init(tbl.Bytes, tbl.Pos)
			if s.Tick() >= from {
				return types
			}
			continue
		}
		types = append(types, typ)
	}
	r.t.Fatal("no snapshot")
	return nil
}

// startGame seats a and b at the table, readies both and waits until the
// stakes are saved and the game is in progress.
func (r *cardWire) startGame(ca, cb *client) {
	r.t.Helper()
	ca.send(cardSit(uint16(wireTable)))
	r.expectResult(ca, "a sits", protocol.CardTableResultCodeOk)
	cb.send(cardSit(uint16(wireTable)))
	r.expectResult(cb, "b sits", protocol.CardTableResultCodeOk)
	ca.send(cardReady())
	r.expectResult(ca, "a ready", protocol.CardTableResultCodeOk)
	cb.send(cardReady())
	r.expectResult(cb, "b ready", protocol.CardTableResultCodeOk)
	for _, c := range []*client{ca, cb} {
		r.stateWhere(c, "the game in progress", func(s wireState) bool { return s.inProgress })
	}
}

// AC1: a joining player gets a CardTableCatalog with each table's id, seat
// count and stake from the game data.
func TestJoinSendsCardTableCatalog(t *testing.T) {
	r := newCardWire(t)
	_, _, c := r.join()
	tbl := r.waitFor(c, protocol.MessageCardTableCatalog)
	var cat protocol.CardTableCatalog
	cat.Init(tbl.Bytes, tbl.Pos)
	type info struct{ id, seats, stake int }
	var got, want []info
	var ti protocol.CardTableInfo
	for i := 0; i < cat.TablesLength(); i++ {
		cat.Tables(&ti, i)
		got = append(got, info{int(ti.Id()), int(ti.Seats()), int(ti.Stake())})
	}
	for _, ct := range r.data.CardTables {
		want = append(want, info{int(ct.ID), ct.Seats, ct.Stake})
	}
	if len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog %+v, want %+v", got, want)
	}
}

// AC2: sit, leave, Ready, ShedPlay and ShedDraw reach the card room; each
// sender gets its CardTableResult (including the TooFar, NotEnoughGold and
// NoGame codes), and CardTableState with ready flags goes to the players
// seated at the table and not to anyone else.
func TestCardTableMessagesOverTheWire(t *testing.T) {
	r := newCardWire(t)
	a, _, ca := r.join()
	b, _, cb := r.join()
	_, _, cc := r.join()

	ca.send(cardSit(99))
	r.expectResult(ca, "sit at an unknown table", protocol.CardTableResultCodeUnknownTable)
	ca.send(shedDraw())
	r.expectResult(ca, "draw while not seated", protocol.CardTableResultCodeNotSeated)

	ca.send(cardSit(uint16(wireTable)))
	r.expectResult(ca, "a sits", protocol.CardTableResultCodeOk)
	if st := r.state(ca); st.table != uint16(wireTable) || !reflect.DeepEqual(st.seats, []uint16{uint16(a), 0, 0, 0}) {
		t.Fatalf("a's state after sitting = %+v", st)
	}
	ca.send(cardSit(uint16(wireTable)))
	r.expectResult(ca, "a sits again", protocol.CardTableResultCodeAlreadySeated)

	// b sits from too far, then without the stake's gold, then properly.
	r.w.Player(b).Pos.X += 50
	cb.send(cardSit(uint16(wireTable)))
	r.expectResult(cb, "b sits from afar", protocol.CardTableResultCodeTooFar)
	r.w.Player(b).Pos = r.w.Player(a).Pos
	gold := r.w.Player(b).Inv.Gold
	r.w.Player(b).Inv.Gold = 0
	cb.send(cardSit(uint16(wireTable)))
	r.expectResult(cb, "b sits without gold", protocol.CardTableResultCodeNotEnoughGold)
	r.w.Player(b).Inv.Gold = gold
	cb.send(cardSit(uint16(wireTable)))
	r.expectResult(cb, "b sits", protocol.CardTableResultCodeOk)
	both := []uint16{uint16(a), uint16(b), 0, 0}
	for name, c := range map[string]*client{"a": ca, "b": cb} {
		r.stateWhere(c, name+" seeing both seated", func(s wireState) bool { return reflect.DeepEqual(s.seats, both) })
	}

	ca.send(cardReady())
	r.expectResult(ca, "a ready", protocol.CardTableResultCodeOk)
	for name, c := range map[string]*client{"a": ca, "b": cb} {
		st := r.stateWhere(c, name+" seeing a ready", func(s wireState) bool { return len(s.ready) == 4 && s.ready[0] })
		if !reflect.DeepEqual(st.ready, []bool{true, false, false, false}) || st.inProgress {
			t.Fatalf("%s: state %+v, want only a ready and no game", name, st)
		}
	}
	cb.send(shedDraw())
	r.expectResult(cb, "draw with no game", protocol.CardTableResultCodeNoGame)
	cb.send(shedPlay(shed.Card{Colour: shed.Colour1, Symbol: 1}, 0))
	r.expectResult(cb, "play with no game", protocol.CardTableResultCodeNoGame)

	for _, typ := range r.typesUntilSnapshot(cc) {
		if typ == protocol.MessageCardTableState || typ == protocol.MessageCardTableResult {
			t.Fatalf("unseated player got %v", typ)
		}
	}

	cb.send(cardLeave())
	r.expectResult(cb, "b leaves", protocol.CardTableResultCodeOk)
	if st := r.state(cb); !reflect.DeepEqual(st.seats, []uint16{uint16(a), 0, 0, 0}) {
		t.Fatalf("b's state after leaving = %+v, want only a seated", st)
	}
	r.stateWhere(ca, "b gone", func(s wireState) bool { return reflect.DeepEqual(s.seats, []uint16{uint16(a), 0, 0, 0}) })
	cb.send(cardLeave())
	r.expectResult(cb, "b leaves again", protocol.CardTableResultCodeNotSeated)
}

// AC3: once two seated players start a game, each gets ShedState and a
// ShedHand holding exactly their own cards, never the other player's, also
// after a move.
func TestShedHandsArePrivate(t *testing.T) {
	r := newCardWire(t)
	a, _, ca := r.join()
	b, _, cb := r.join()
	r.startGame(ca, cb)

	check := func(name string, c *client, me, other world.PlayerID) {
		t.Helper()
		tbl := r.waitFor(c, protocol.MessageShedState)
		var st protocol.ShedState
		st.Init(tbl.Bytes, tbl.Pos)
		if st.TableId() != uint16(wireTable) || st.HandCountsLength() != 4 {
			t.Fatalf("%s: ShedState table %d with %d hand counts", name, st.TableId(), st.HandCountsLength())
		}
		id, h := r.hand(c)
		mine, theirs := r.serverHand(me), r.serverHand(other)
		if id != uint16(wireTable) || len(mine) == 0 || !reflect.DeepEqual(h, mine) {
			t.Fatalf("%s: ShedHand table %d %v, want own hand %v", name, id, h, mine)
		}
		if reflect.DeepEqual(h, theirs) {
			t.Fatalf("%s: got the other player's hand %v", name, theirs)
		}
	}
	check("a", ca, a, b)
	check("b", cb, b, a)

	// Whoever's turn it is draws; both get the new state and their own hand.
	tb, _ := r.cards.Table(wireTable)
	seat, _ := tb.Turn()
	mover, _ := tb.Occupant(seat)
	cm := ca
	if mover == b {
		cm = cb
	}
	cm.send(shedDraw())
	r.expectResult(cm, "draw on your turn", protocol.CardTableResultCodeOk)
	for name, c := range map[string]*client{"a": ca, "b": cb} {
		me, other := a, b
		if c == cb {
			me, other = b, a
		}
		for {
			_, h := r.hand(c)
			if len(h) == len(r.serverHand(me)) {
				if !reflect.DeepEqual(h, r.serverHand(me)) || reflect.DeepEqual(h, r.serverHand(other)) {
					t.Fatalf("%s: hand after the draw %v, want own hand %v", name, h, r.serverHand(me))
				}
				break
			}
		}
	}
}

// AC4: a seated player who disconnects is marked disconnected at their
// table; reconnecting within the logout time clears the mark and sends them
// the table and their hand again.
func TestSeatedPlayerDisconnectAndReconnect(t *testing.T) {
	r := newCardWire(t)
	a, tok, ca := r.join()
	_, _, cb := r.join()
	r.startGame(ca, cb)
	tb, _ := r.cards.Table(wireTable)
	seat, _ := tb.SeatOf(a)

	ca.c.CloseNow()
	for i := 0; i < 1000 && !session.Disconnected(r.sess, a); i++ {
		time.Sleep(time.Millisecond)
		r.w.Tick()
	}
	if !tb.Disconnected(seat) {
		t.Fatal("disconnected player's seat not marked disconnected")
	}
	r.clock.Advance(r.data.Rules.LogoutCombatLock() / 2)
	r.w.Tick()
	if r.w.Player(a) == nil || !tb.Disconnected(seat) {
		t.Fatal("player removed or mark cleared before the logout time")
	}

	c2 := r.dial(hello(uint16(protocol.ProtocolVersionCurrent), tok))
	if id, _ := r.expectWelcome(c2); world.PlayerID(id) != a {
		t.Fatalf("reconnect got id %d, want %d", id, a)
	}
	r.state(c2)
	if tb.Disconnected(seat) {
		t.Fatal("reconnecting did not clear the disconnected mark")
	}
	if _, h := r.hand(c2); !reflect.DeepEqual(h, r.serverHand(a)) {
		t.Fatalf("reconnected hand %v, want %v", h, r.serverHand(a))
	}
	if occ, _ := tb.Occupant(seat); occ != a || !tb.InProgress() {
		t.Fatal("reconnected player lost their seat")
	}
}

// AC5 (Q4): a seated player removed from the world after the logout time
// leaves their table; mid-game this is a forfeit, so the other player wins.
func TestSeatedPlayerRemovedAfterLogoutForfeits(t *testing.T) {
	r := newCardWire(t)
	a, _, ca := r.join()
	b, _, cb := r.join()
	r.startGame(ca, cb)
	tb, _ := r.cards.Table(wireTable)
	bSeat, _ := tb.SeatOf(b)

	ca.c.CloseNow()
	for i := 0; i < 1000 && !session.Disconnected(r.sess, a); i++ {
		time.Sleep(time.Millisecond)
		r.w.Tick()
	}
	r.clock.Advance(r.data.Rules.LogoutCombatLock())
	r.w.Tick()
	if r.w.Player(a) != nil {
		t.Fatal("player still in the world after the logout time")
	}
	if _, seated := r.cards.TableOf(a); seated {
		t.Fatal("removed player still seated")
	}
	st := r.stateWhere(cb, "the game over", func(s wireState) bool { return !s.inProgress })
	seats := make([]uint16, 4)
	seats[bSeat] = uint16(b)
	if !st.hasWinner || int(st.winner) != int(bSeat) || !reflect.DeepEqual(st.seats, seats) {
		t.Fatalf("b's state = %+v, want b alone and the winner (seat %d)", st, bSeat)
	}
}
