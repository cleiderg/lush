package session_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/cleiderg/lush/server/internal/interest"
	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/world"
)

type auditStore struct {
	name  string
	store func(t *testing.T) (store.Players, func() int)
}

var auditStores = []auditStore{
	{"memory", func(*testing.T) (store.Players, func() int) {
		m := store.NewMemory()
		return m, func() int { return len(m.AuditEntries()) }
	}},
	{"pgx", pgStore},
}

// handInRig is a player on Overdue's hand-in step, holding the book (in the
// world only, unless the test puts it in the store), standing next to the
// librarian on a fresh server.
type handInRig struct {
	*questRig
	players store.Players
	c       *client
	p       *world.Player
	tok     string
	sid     int64
}

func newHandIn(t *testing.T, players store.Players, prepare func(sid int64, tok string)) *handInRig {
	t.Helper()
	ctx := context.Background()
	q := newQuestRig(t, players)
	_, _, tok := q.join("")
	overdue, _ := q.d.QuestByKey("overdue")
	sp, err := players.LoadByTokenHash(ctx, hashOf(tok))
	if err != nil {
		t.Fatal(err)
	}
	if err := players.(store.Quests).SaveQuests(ctx, sp.ID, []store.QuestProgress{{Quest: overdue.Key, State: store.QuestInProgress, Step: 2}}); err != nil {
		t.Fatal(err)
	}
	if prepare != nil {
		prepare(sp.ID, tok)
	}
	q1 := newQuestRig(t, players)
	c, p, _ := q1.join(tok)
	q1.w.Tick()
	_, pos := q1.npcOf("librarian")
	p.Pos = world.Vec2{X: pos.X + 1, Y: pos.Y}
	return &handInRig{questRig: q1, players: players, c: c, p: p, tok: tok, sid: sp.ID}
}

// handIn talks to the librarian and picks "Here you go.".
func (h *handInRig) handIn() {
	h.t.Helper()
	npc, _ := h.npcOf("librarian")
	h.c.send(talkFrame(uint16(npc)))
	h.dialogue(h.c)
	h.c.send(replyFrame(0))
}

func (h *handInRig) stored() store.Player {
	h.t.Helper()
	sp, err := h.players.LoadByTokenHash(context.Background(), hashOf(h.tok))
	if err != nil {
		h.t.Fatal(err)
	}
	return sp
}

func (h *handInRig) storedQuests() store.QuestLog {
	h.t.Helper()
	log, err := h.players.(store.Quests).LoadQuests(context.Background(), h.sid)
	if err != nil {
		h.t.Fatal(err)
	}
	return log
}

// collect reads messages, ticking the world, until one of typ arrives, and
// returns every type seen up to and including it.
func (q *questRig) collect(c *client, typ protocol.Message) []protocol.Message {
	q.t.Helper()
	var seen []protocol.Message
	for i := 0; i < 1000; i++ {
		got, _ := q.next(c)
		seen = append(seen, got)
		if got == typ {
			return seen
		}
	}
	q.t.Fatalf("never saw message %v", typ)
	return nil
}

func count(msgs []protocol.Message, typ protocol.Message) int {
	n := 0
	for _, m := range msgs {
		if m == typ {
			n++
		}
	}
	return n
}

// AC1 (#667): a completion is committed with CompleteQuest and answered with
// Resolve; the player gets the rewards once, and after a reconnect the quest
// is complete.
func TestQuestCompletionCommitted(t *testing.T) {
	for _, tc := range auditStores {
		t.Run(tc.name, func(t *testing.T) {
			players, auditRows := tc.store(t)
			ctx := context.Background()
			h := newHandIn(t, players, nil)
			book, _ := h.d.ItemByKey("overdue_book")
			card, _ := h.d.ItemByKey("library_card")
			if _, err := h.p.Inv.Add(h.d, book.ID, 1); err != nil {
				t.Fatal(err)
			}
			before, auditBefore := h.stored(), auditRows()
			gold, xp := h.p.Inv.Gold, h.p.XP

			h.handIn()
			h.waitFor(h.c, protocol.MessageQuestCompleted)
			h.waitFor(h.c, protocol.MessageQuestLog)

			if h.p.Inv.Gold != gold+150 || h.p.XP != xp+500 || h.p.Inv.Count(card.ID) != 1 || h.p.Inv.Count(book.ID) != 0 || h.p.Inv.Locked() {
				t.Fatalf("world: gold %d (was %d) xp %d (was %d) cards %d books %d locked %v",
					h.p.Inv.Gold, gold, h.p.XP, xp, h.p.Inv.Count(card.ID), h.p.Inv.Count(book.ID), h.p.Inv.Locked())
			}
			if err := h.sess.SaveAll(ctx); err != nil {
				t.Fatal(err)
			}
			got := h.stored()
			if got.Gold != before.Gold+150 || got.XP != before.XP+500 {
				t.Fatalf("stored gold %d xp %d, want %d and %d", got.Gold, got.XP, before.Gold+150, before.XP+500)
			}
			cards, books := 0, 0
			for _, st := range got.Inventory {
				if st.Item == int32(card.ID) {
					cards += int(st.Qty)
				}
				if st.Item == int32(book.ID) {
					books += int(st.Qty)
				}
			}
			if cards != 1 || books != 0 {
				t.Fatalf("stored cards %d books %d, want 1 and 0", cards, books)
			}
			overdue, _ := h.d.QuestByKey("overdue")
			if log := h.storedQuests(); log.Points != 1 || !reflect.DeepEqual(log.Quests, []store.QuestProgress{{Quest: overdue.Key, State: store.QuestComplete}}) {
				t.Fatalf("stored quests %+v", log)
			}
			if rows := auditRows() - auditBefore; rows < 2 {
				t.Fatalf("%d audit rows written, want the gold and the card at least", rows)
			}

			// After a reconnect the quest is complete and the rewards were
			// given once.
			q2 := newQuestRig(t, players)
			c2, p2, _ := q2.join(h.tok)
			rows := []questRow{{QuestId: uint16(overdue.ID), State: protocol.QuestStateComplete}}
			if log := q2.questLog(c2); !reflect.DeepEqual(log.rows, rows) || log.points != 1 {
				t.Fatalf("log after reconnect %+v, want %+v and 1 point", log, rows)
			}
			if int64(p2.Inv.Gold) != before.Gold+150 || p2.Inv.Count(card.ID) != 1 {
				t.Fatalf("after reconnect gold %d cards %d", p2.Inv.Gold, p2.Inv.Count(card.ID))
			}
		})
	}
}

// AC2 (#667): QuestCompleted goes to the player and to players in their
// interest area, and not to a player far away.
func TestQuestCompletedAnnouncedToNearby(t *testing.T) {
	players := store.NewMemory()
	h := newHandIn(t, players, nil)
	book, _ := h.d.ItemByKey("overdue_book")
	if _, err := h.p.Inv.Add(h.d, book.ID, 1); err != nil {
		t.Fatal(err)
	}
	// The other clients share the server of the hand-in player.
	dial := func(at world.Vec2) (*client, *world.Player) {
		c := h.dial(hello(uint16(protocol.ProtocolVersionCurrent), ""))
		id, _ := h.expectWelcome(c)
		h.w.Tick()
		p := h.w.Player(world.PlayerID(id))
		p.Pos = at
		return c, p
	}
	near1, _ := dial(world.Vec2{X: h.p.Pos.X + 2, Y: h.p.Pos.Y})
	near2, _ := dial(world.Vec2{X: h.p.Pos.X, Y: h.p.Pos.Y + 3})
	far, _ := dial(world.Vec2{X: h.p.Pos.X + 20*float32(interest.ViewRadius), Y: h.p.Pos.Y})
	h.w.Tick()

	h.handIn()
	h.waitFor(h.c, protocol.MessageQuestCompleted)
	tbl := h.waitFor(near1, protocol.MessageQuestCompleted)
	var qc protocol.QuestCompleted
	qc.Init(tbl.Bytes, tbl.Pos)
	overdue, _ := h.d.QuestByKey("overdue")
	if string(qc.QuestName()) != "Overdue" || qc.QuestId() != uint16(overdue.ID) || len(qc.PlayerName()) == 0 {
		t.Fatalf("announcement quest %q id %d player %q", qc.QuestName(), qc.QuestId(), qc.PlayerName())
	}
	var rw protocol.QuestRewards
	if qc.Rewards(&rw) == nil || rw.Gold() != 150 || rw.QuestPoints() != 1 {
		t.Fatal("announcement has no rewards")
	}
	h.waitFor(near2, protocol.MessageQuestCompleted)

	// Frames arrive in order: a reply to a Talk sent now follows any
	// announcement the far client was going to get.
	far.send(talkFrame(1))
	if seen := h.collect(far, protocol.MessageQuestResult); count(seen, protocol.MessageQuestCompleted) != 0 {
		t.Fatalf("far client got the announcement: %v", seen)
	}
}

// AC3 (#667): a completion the store refuses is answered with failure,
// nothing is written and the quest stays on its last step.
func TestQuestCompletionFailures(t *testing.T) {
	for _, tc := range auditStores {
		t.Run(tc.name+"/already complete", func(t *testing.T) {
			players, auditRows := tc.store(t)
			h := newHandIn(t, players, nil)
			book, _ := h.d.ItemByKey("overdue_book")
			if _, err := h.p.Inv.Add(h.d, book.ID, 1); err != nil {
				t.Fatal(err)
			}
			overdue, _ := h.d.QuestByKey("overdue")
			// Another session completes the quest first.
			if _, err := players.(store.Quests).CompleteQuest(context.Background(), store.QuestCompletion{PlayerID: h.sid, Quest: overdue.Key, Points: 1}); err != nil {
				t.Fatal(err)
			}
			before, auditBefore := h.stored(), auditRows()

			h.handIn()
			h.settle(t)

			if got := h.stored(); got.Gold != before.Gold || !reflect.DeepEqual(got.Inventory, before.Inventory) || got.XP != before.XP {
				t.Fatalf("holdings changed: %+v -> %+v", before, got)
			}
			if auditRows() != auditBefore {
				t.Fatal("audit rows written for a failed completion")
			}
			if log := h.storedQuests(); log.Points != 1 {
				t.Fatalf("quest points %d, want only the earlier completion's 1", log.Points)
			}
			if h.p.Inv.Count(book.ID) != 1 || int64(h.p.Inv.Gold) != before.Gold {
				t.Fatalf("world gold %d books %d: the failed completion changed the player", h.p.Inv.Gold, h.p.Inv.Count(book.ID))
			}
		})
		t.Run(tc.name+"/inventory full", func(t *testing.T) {
			players, auditRows := tc.store(t)
			d := testData(t)
			trout, _ := d.ItemByKey("trout")
			book, _ := d.ItemByKey("overdue_book")
			h := newHandIn(t, players, func(sid int64, tok string) {
				// The stored inventory is full of other things, and the book
				// is in the world only, so the store has no room.
				sp, err := players.LoadByTokenHash(context.Background(), hashOf(tok))
				if err != nil {
					t.Fatal(err)
				}
				full := sp.Holdings
				for i := range full.Inventory {
					full.Inventory[i] = store.Stack{Item: int32(trout.ID), Qty: 1}
				}
				if err := players.ApplyEconomy(context.Background(), []store.EconomyChange{{PlayerID: sid, Holdings: full}}, nil); err != nil {
					t.Fatal(err)
				}
			})
			h.p.Inv.Slots[0] = world.Stack{Item: book.ID, Qty: 1}
			before, auditBefore := h.stored(), auditRows()

			h.handIn()
			for i := 0; ; i++ {
				if code := h.questResult(h.c); code == protocol.QuestResultCodeInventoryFull {
					break
				} else if i > 5 {
					t.Fatalf("QuestResult %v, want InventoryFull", code)
				}
			}
			h.settle(t)

			if got := h.stored(); got.Gold != before.Gold || !reflect.DeepEqual(got.Inventory, before.Inventory) || got.XP != before.XP {
				t.Fatalf("holdings changed: %+v -> %+v", before, got)
			}
			if auditRows() != auditBefore {
				t.Fatal("audit rows written for a failed completion")
			}
			overdue, _ := d.QuestByKey("overdue")
			if log := h.storedQuests(); log.Points != 0 || !reflect.DeepEqual(log.Quests, []store.QuestProgress{{Quest: overdue.Key, State: store.QuestInProgress, Step: 2}}) {
				t.Fatalf("stored quests %+v, want Overdue on step 2", log)
			}
			if h.p.Inv.Count(book.ID) != 1 || int64(h.p.Inv.Gold) != before.Gold {
				t.Fatalf("world gold %d books %d: the failed completion changed the player", h.p.Inv.Gold, h.p.Inv.Count(book.ID))
			}
		})
	}
}

// settle ticks the world for a while, so a commit has been answered and
// applied, and fails if the inventory is still locked.
func (h *handInRig) settle(t *testing.T) {
	t.Helper()
	for i := 0; i < 100; i++ {
		h.w.Tick()
		time.Sleep(5 * time.Millisecond)
	}
	if h.p.Inv.Locked() {
		t.Fatal("inventory still locked: the commit was never answered")
	}
}
