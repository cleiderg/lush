package session

import (
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/systems/cardroom"
	"github.com/cleiderg/lush/server/internal/systems/cardtable"
	"github.com/cleiderg/lush/server/internal/systems/cardtable/shed"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

func cardSitFrame(table uint16) []byte {
	return frame(protocol.MessageCardTableSit, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.CardTableSitStart(b)
		protocol.CardTableSitAddTableId(b, table)
		return protocol.CardTableSitEnd(b)
	})
}

func cardLeaveFrame() []byte {
	return frame(protocol.MessageCardTableLeave, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.CardTableLeaveStart(b)
		return protocol.CardTableLeaveEnd(b)
	})
}

func cardReadyFrame() []byte {
	return frame(protocol.MessageCardTableReady, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.CardTableReadyStart(b)
		return protocol.CardTableReadyEnd(b)
	})
}

func shedPlayFrame(c shed.Card, chosen shed.Colour) []byte {
	return frame(protocol.MessageShedPlay, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShedPlayStart(b)
		protocol.ShedPlayAddCard(b, protocol.CreateShedCard(b, protocol.ShedColour(c.Colour), protocol.ShedSymbol(c.Symbol)))
		protocol.ShedPlayAddChosenColour(b, protocol.ShedColour(chosen))
		return protocol.ShedPlayEnd(b)
	})
}

func shedPlayNoCardFrame() []byte {
	return frame(protocol.MessageShedPlay, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShedPlayStart(b)
		return protocol.ShedPlayEnd(b)
	})
}

func shedDrawFrame() []byte {
	return frame(protocol.MessageShedDraw, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.ShedDrawStart(b)
		return protocol.ShedDrawEnd(b)
	})
}

// AC2: CardTableSit, CardTableLeave, CardTableReady, ShedPlay and ShedDraw
// decode into the card room's world commands.
func TestDecodeCardRoomCommands(t *testing.T) {
	const id = world.PlayerID(7)
	wild := shed.Card{Colour: shed.NoColour, Symbol: shed.WildDrawFour}
	tests := []struct {
		name  string
		frame []byte
		want  world.Command
	}{
		{"sit", cardSitFrame(3), cardroom.SitCommand{Player: id, TableID: 3}},
		{"sit max id", cardSitFrame(65535), cardroom.SitCommand{Player: id, TableID: 65535}},
		{"leave", cardLeaveFrame(), cardroom.LeaveCommand{Player: id}},
		{"ready", cardReadyFrame(), cardroom.ReadyCommand{Player: id, Ready: true}},
		{"play", shedPlayFrame(shed.Card{Colour: shed.Colour2, Symbol: 7}, shed.NoColour),
			cardroom.PlayCommand{Player: id, Card: shed.Card{Colour: shed.Colour2, Symbol: 7}}},
		{"play wild", shedPlayFrame(wild, shed.Colour4), cardroom.PlayCommand{Player: id, Card: wild, Chosen: shed.Colour4}},
		{"play no card", shedPlayNoCardFrame(), nil},
		{"draw", shedDrawFrame(), cardroom.DrawCommand{Player: id}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := decode(tc.frame)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.command(id); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("command = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// AC2: truncated card room frames are errors, never panics.
func TestDecodeMalformedCardRoomFrames(t *testing.T) {
	for _, f := range [][]byte{cardSitFrame(1), cardLeaveFrame(), cardReadyFrame(),
		shedPlayFrame(shed.Card{Colour: shed.Colour1, Symbol: 1}, 0), shedDrawFrame()} {
		for n := 0; n < len(f); n++ {
			m, err := decode(f[:n])
			if err == nil {
				m.command(1) // must not panic either
			}
		}
	}
}

// AC2: every card room result has a wire code except StakeFailed, and the
// new codes map one to one.
func TestCardResultCodes(t *testing.T) {
	want := map[cardroom.Result]protocol.CardTableResultCode{
		cardroom.OK:            protocol.CardTableResultCodeOk,
		cardroom.UnknownTable:  protocol.CardTableResultCodeUnknownTable,
		cardroom.AlreadySeated: protocol.CardTableResultCodeAlreadySeated,
		cardroom.TableFull:     protocol.CardTableResultCodeTableFull,
		cardroom.InProgress:    protocol.CardTableResultCodeInProgress,
		cardroom.NotSeated:     protocol.CardTableResultCodeNotSeated,
		cardroom.NoGame:        protocol.CardTableResultCodeNoGame,
		cardroom.NotYourTurn:   protocol.CardTableResultCodeNotYourTurn,
		cardroom.NotInHand:     protocol.CardTableResultCodeIllegalMove,
		cardroom.NoMatch:       protocol.CardTableResultCodeIllegalMove,
		cardroom.BadColour:     protocol.CardTableResultCodeIllegalMove,
		cardroom.IllegalMove:   protocol.CardTableResultCodeIllegalMove,
		cardroom.StartFailed:   protocol.CardTableResultCodeIllegalMove,
		cardroom.TooFar:        protocol.CardTableResultCodeTooFar,
		cardroom.NotEnoughGold: protocol.CardTableResultCodeNotEnoughGold,
	}
	for r := cardroom.OK; r <= cardroom.StakeFailed; r++ {
		got, ok := cardResultCodes[r]
		w, wantOK := want[r]
		if ok != wantOK || got != w {
			t.Errorf("%v: code %v (%v), want %v (%v)", r, got, ok, w, wantOK)
		}
		if ok {
			tbl := envelopeOf(t, encodeCardTableResult(got), protocol.MessageCardTableResult)
			var res protocol.CardTableResult
			res.Init(tbl.Bytes, tbl.Pos)
			if res.Code() != got {
				t.Errorf("%v: encoded code %v, want %v", r, res.Code(), got)
			}
		}
	}
}

// AC1: the catalog lists every card table in the game data with its seats
// and stake.
func TestEncodeCardTableCatalog(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	tbl := envelopeOf(t, encodeCardTableCatalog(d), protocol.MessageCardTableCatalog)
	var cat protocol.CardTableCatalog
	cat.Init(tbl.Bytes, tbl.Pos)
	if cat.TablesLength() != len(d.CardTables) || len(d.CardTables) == 0 {
		t.Fatalf("catalog has %d tables, want %d", cat.TablesLength(), len(d.CardTables))
	}
	var info protocol.CardTableInfo
	for i, c := range d.CardTables {
		cat.Tables(&info, i)
		if int(info.Id()) != int(c.ID) || int(info.Seats()) != c.Seats || int(info.Stake()) != c.Stake {
			t.Errorf("table %d = id %d seats %d stake %d, want %+v", i, info.Id(), info.Seats(), info.Stake(), c)
		}
	}
}

func TestEncodeCardTableState(t *testing.T) {
	snap := cardroom.Snapshot{
		Seats: []cardroom.SeatInfo{{Occupied: true, Player: 5, Ready: true}, {}, {Occupied: true, Player: 9}},
		InProgress: true, Turn: 2, Reversed: true, TicksLeft: 123, HasWinner: true, Winner: 1,
	}
	b := flatbuffers.NewBuilder(0)
	encodeCardTableState(b, 1, cardroom.Snapshot{}) // the builder is reused
	tbl := envelopeOf(t, encodeCardTableState(b, 4, snap), protocol.MessageCardTableState)
	var st protocol.CardTableState
	st.Init(tbl.Bytes, tbl.Pos)
	var seats []uint16
	var ready []bool
	for i := 0; i < st.SeatsLength(); i++ {
		seats = append(seats, st.Seats(i))
	}
	for i := 0; i < st.ReadyLength(); i++ {
		ready = append(ready, st.Ready(i))
	}
	if st.TableId() != 4 || !reflect.DeepEqual(seats, []uint16{5, 0, 9}) || !reflect.DeepEqual(ready, []bool{true, false, false}) ||
		!st.InProgress() || st.TurnSeat() != 2 || !st.Reversed() || st.TurnTicksLeft() != 123 || !st.HasWinner() || st.WinnerSeat() != 1 {
		t.Fatalf("CardTableState = table %d seats %v ready %v in progress %v turn %d reversed %v ticks %d winner %v/%d",
			st.TableId(), seats, ready, st.InProgress(), st.TurnSeat(), st.Reversed(), st.TurnTicksLeft(), st.HasWinner(), st.WinnerSeat())
	}
}

func TestEncodeShedStateAndHand(t *testing.T) {
	b := flatbuffers.NewBuilder(0)
	v := shed.PublicView{Top: shed.Card{Colour: shed.Colour3, Symbol: shed.Skip}, Colour: shed.Colour3,
		HandCounts: map[cardtable.Seat]int{0: 7, 2: 300}, DrawPile: 90}
	tbl := envelopeOf(t, encodeShedState(b, 2, 4, v), protocol.MessageShedState)
	var st protocol.ShedState
	st.Init(tbl.Bytes, tbl.Pos)
	top := st.TopCard(nil)
	if st.TableId() != 2 || top == nil || top.Colour() != protocol.ShedColourThree || top.Symbol() != protocol.ShedSymbolSkip ||
		st.Colour() != protocol.ShedColourThree || !reflect.DeepEqual(st.HandCountsBytes(), []byte{7, 0, 255, 0}) || st.DrawPile() != 90 {
		t.Fatalf("ShedState = table %d top %v colour %v counts %v draw %d", st.TableId(), top, st.Colour(), st.HandCountsBytes(), st.DrawPile())
	}

	hand := []shed.Card{{Colour: shed.Colour1, Symbol: 4}, {Symbol: shed.Wild}}
	tbl = envelopeOf(t, encodeShedHand(b, 2, hand), protocol.MessageShedHand)
	var h protocol.ShedHand
	h.Init(tbl.Bytes, tbl.Pos)
	if h.TableId() != 2 || !reflect.DeepEqual(handOf(&h), hand) {
		t.Fatalf("ShedHand = table %d cards %v, want %v", h.TableId(), handOf(&h), hand)
	}
}

func handOf(h *protocol.ShedHand) []shed.Card {
	var out []shed.Card
	var c protocol.ShedCard
	for i := 0; i < h.CardsLength(); i++ {
		h.Cards(&c, i)
		out = append(out, shed.Card{Colour: shed.Colour(c.Colour()), Symbol: shed.Symbol(c.Symbol())})
	}
	return out
}
