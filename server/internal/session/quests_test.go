package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/session"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

type questRig struct {
	*rig
	d *gamedata.Data
}

// newQuestRig is a rig with the quest and talking NPC systems over players.
func newQuestRig(t *testing.T, players store.Players) *questRig {
	t.Helper()
	d := testData(t)
	cfg := session.Config{TickRate: 20, MoveSpeed: 5, HelloTimeout: time.Second, Data: d}
	q := &questRig{d: d}
	q.rig = newRigSystems(t, 1024, cfg, players, nil, func(s *session.Server) []world.System {
		events, talkers, err := s.QuestSystems(1, rand.New(rand.NewPCG(1, 2)))
		if err != nil {
			t.Fatal(err)
		}
		return []world.System{talkers, events}
	})
	t.Cleanup(q.sess.Close)
	return q
}

func (q *questRig) join(token string) (*client, *world.Player, string) {
	c := q.dial(hello(uint16(protocol.ProtocolVersionCurrent), token))
	id, tok := q.expectWelcome(c)
	q.w.Tick()
	return c, q.w.Player(world.PlayerID(id)), tok
}

// npcOf is the entity id of the talking NPC of type key, and its position.
func (q *questRig) npcOf(key string) (world.NPCID, world.Vec2) {
	q.t.Helper()
	typ, ok := q.d.NPCByKey(key)
	if !ok {
		q.t.Fatalf("no npc type %q", key)
	}
	for _, n := range q.w.NPCs() {
		if n.Type == world.NPCTypeID(typ.ID) {
			return n.ID, n.Pos
		}
	}
	q.t.Fatalf("no %s standing", key)
	return 0, world.Vec2{}
}

type questRow struct {
	QuestId uint16
	State   protocol.QuestState
	Step    byte
	Count   uint16
}

type questLog struct {
	points uint16
	rows   []questRow
}

func (q *questRig) questLog(c *client) questLog {
	q.t.Helper()
	tbl := q.waitFor(c, protocol.MessageQuestLog)
	var l protocol.QuestLog
	l.Init(tbl.Bytes, tbl.Pos)
	out := questLog{points: l.QuestPoints()}
	for i := 0; i < l.QuestsLength(); i++ {
		var p protocol.QuestProgress
		l.Quests(&p, i)
		out.rows = append(out.rows, questRow{p.QuestId(), p.State(), p.Step(), p.Count()})
	}
	return out
}

func talkFrame(npc uint16) []byte {
	return envelope(protocol.MessageTalk, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.TalkStart(b)
		protocol.TalkAddNpcId(b, npc)
		return protocol.TalkEnd(b)
	})
}

func replyFrame(i byte) []byte {
	return envelope(protocol.MessageDialogueReply, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		protocol.DialogueReplyStart(b)
		protocol.DialogueReplyAddIndex(b, i)
		return protocol.DialogueReplyEnd(b)
	})
}

func (q *questRig) dialogue(c *client) (npc uint16, lines, replies []string) {
	q.t.Helper()
	tbl := q.waitFor(c, protocol.MessageDialogue)
	var d protocol.Dialogue
	d.Init(tbl.Bytes, tbl.Pos)
	for i := 0; i < d.LinesLength(); i++ {
		lines = append(lines, string(d.Lines(i)))
	}
	for i := 0; i < d.RepliesLength(); i++ {
		replies = append(replies, string(d.Replies(i)))
	}
	return d.NpcId(), lines, replies
}

func (q *questRig) questResult(c *client) protocol.QuestResultCode {
	q.t.Helper()
	tbl := q.waitFor(c, protocol.MessageQuestResult)
	var r protocol.QuestResult
	r.Init(tbl.Bytes, tbl.Pos)
	return r.Code()
}

// AC1 (#666): a joining player gets QuestCatalog with every quest and a
// QuestLog built from their stored rows.
func TestJoinSendsQuestCatalogAndStoredLog(t *testing.T) {
	mem := store.NewMemory()
	q := newQuestRig(t, mem)
	c, _, tok := q.join("")

	tbl := q.waitFor(c, protocol.MessageQuestCatalog)
	var cat protocol.QuestCatalog
	cat.Init(tbl.Bytes, tbl.Pos)
	if cat.QuestsLength() != len(q.d.Quests) {
		t.Fatalf("catalog has %d quests, want %d", cat.QuestsLength(), len(q.d.Quests))
	}
	var def protocol.QuestDef
	for i, want := range q.d.Quests {
		cat.Quests(&def, i)
		if def.Id() != uint16(want.ID) || string(def.Name()) != want.Name || def.StepsLength() != len(want.Steps) {
			t.Errorf("quest %d: id %d name %q steps %d; want %d %q %d", i, def.Id(), def.Name(), def.StepsLength(), want.ID, want.Name, len(want.Steps))
		}
		if def.StartNpc() == 0 || def.FinishNpc() == 0 {
			t.Errorf("quest %q: start npc %d finish npc %d, want both set", want.Key, def.StartNpc(), def.FinishNpc())
		}
	}
	if log := q.questLog(c); len(log.rows) != 0 || log.points != 0 {
		t.Fatalf("new player log %+v, want empty", log)
	}

	// A player with stored rows gets them in the log, from a fresh server.
	first := q.d.Quests[0]
	sp, err := mem.LoadByTokenHash(context.Background(), hashOf(tok))
	if err != nil {
		t.Fatal(err)
	}
	if err := mem.SaveQuests(context.Background(), sp.ID, []store.QuestProgress{{Quest: first.Key, State: store.QuestInProgress, Step: 1}}); err != nil {
		t.Fatal(err)
	}
	q2 := newQuestRig(t, mem)
	c2, _, _ := q2.join(tok)
	q2.waitFor(c2, protocol.MessageQuestCatalog)
	want := []questRow{{QuestId: uint16(first.ID), State: protocol.QuestStateInProgress, Step: 1}}
	if log := q2.questLog(c2); !reflect.DeepEqual(log.rows, want) {
		t.Fatalf("log rows %+v, want %+v", log.rows, want)
	}
}

// AC2 (#666): Talk within 2 tiles opens a Dialogue, a reply moves it on and
// starts the quest, and a Talk from too far gets QuestResult TooFar.
func TestTalkReplyAndTooFar(t *testing.T) {
	q := newQuestRig(t, store.NewMemory())
	c, p, _ := q.join("")
	q.w.Tick() // the talkers appear on the first tick
	npc, pos := q.npcOf("librarian")

	p.Pos = world.Vec2{X: pos.X + 10, Y: pos.Y}
	c.send(talkFrame(uint16(npc)))
	if code := q.questResult(c); code != protocol.QuestResultCodeTooFar {
		t.Fatalf("talk from afar: %v, want TooFar", code)
	}

	p.Pos = world.Vec2{X: pos.X + 1.5, Y: pos.Y}
	c.send(talkFrame(uint16(npc)))
	id, lines, replies := q.dialogue(c)
	if id != uint16(npc) || len(lines) == 0 || len(replies) != 2 {
		t.Fatalf("dialogue npc %d lines %q replies %q", id, lines, replies)
	}
	c.send(replyFrame(0)) // "I'll find it": starts the quest
	_, _, replies = q.dialogue(c)
	if len(replies) != 1 {
		t.Fatalf("next node replies %q, want one", replies)
	}
	c.send(replyFrame(0)) // finish the talk step
	q.waitFor(c, protocol.MessageDialogueClose)

	c.send(talkFrame(1)) // not an NPC
	if code := q.questResult(c); code != protocol.QuestResultCodeNotTalkable {
		t.Fatalf("talk to nothing: %v, want NotTalkable", code)
	}
}

// AC3 (#666): a quest started and moved on in dialogue is saved and sent as
// a new QuestLog, and is there when the player returns to a new server.
func TestQuestProgressSavedAndSurvivesReconnect(t *testing.T) {
	t.Run("memory", func(t *testing.T) { questProgressSurvives(t, store.NewMemory()) })
	t.Run("postgres", func(t *testing.T) {
		players, _ := pgStore(t)
		questProgressSurvives(t, players)
	})
}

func questProgressSurvives(t *testing.T, players store.Players) {
	q := newQuestRig(t, players)
	c, p, tok := q.join("")
	q.w.Tick()
	npc, pos := q.npcOf("librarian")
	p.Pos = world.Vec2{X: pos.X + 1, Y: pos.Y}

	c.send(talkFrame(uint16(npc)))
	q.dialogue(c)
	c.send(replyFrame(0)) // starts Overdue at step 0
	q.dialogue(c)
	c.send(replyFrame(0)) // finishes the talk step: now at step 1
	q.waitFor(c, protocol.MessageDialogueClose)

	overdue, _ := q.d.QuestByKey("overdue")
	var log questLog
	for range 10 { // the log after the start, then after the step
		log = q.questLog(c)
		if len(log.rows) == 1 && log.rows[0].Step == 1 {
			break
		}
	}
	want := []questRow{{QuestId: uint16(overdue.ID), State: protocol.QuestStateInProgress, Step: 1}}
	if !reflect.DeepEqual(log.rows, want) {
		t.Fatalf("log after talking %+v, want %+v", log.rows, want)
	}

	if err := q.sess.SaveAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	q2 := newQuestRig(t, players)
	c2, _, _ := q2.join(tok)
	if got := q2.questLog(c2); !reflect.DeepEqual(got.rows, want) {
		t.Fatalf("log after reconnect %+v, want %+v", got.rows, want)
	}
}

// A quest finished in dialogue is saved complete: a player who returns to a
// new server cannot hand the quest in, and take its rewards, again.
func TestQuestCompletedInDialogueStaysComplete(t *testing.T) {
	t.Run("memory", func(t *testing.T) { questCompletionSurvives(t, store.NewMemory()) })
	t.Run("postgres", func(t *testing.T) {
		players, _ := pgStore(t)
		questCompletionSurvives(t, players)
	})
}

func questCompletionSurvives(t *testing.T, players store.Players) {
	ctx := context.Background()
	q := newQuestRig(t, players)
	_, _, tok := q.join("")
	overdue, _ := q.d.QuestByKey("overdue")
	book, _ := q.d.ItemByKey("overdue_book")
	sp, err := players.LoadByTokenHash(ctx, hashOf(tok))
	if err != nil {
		t.Fatal(err)
	}
	// On the hand-in step, holding the book.
	if err := players.(store.Quests).SaveQuests(ctx, sp.ID, []store.QuestProgress{{Quest: overdue.Key, State: store.QuestInProgress, Step: 2}}); err != nil {
		t.Fatal(err)
	}
	q1 := newQuestRig(t, players)
	c, p, _ := q1.join(tok)
	q1.w.Tick()
	if _, err := p.Inv.Add(q1.d, book.ID, 1); err != nil {
		t.Fatal(err)
	}
	npc, pos := q1.npcOf("librarian")
	p.Pos = world.Vec2{X: pos.X + 1, Y: pos.Y}
	c.send(talkFrame(uint16(npc)))
	q1.dialogue(c)
	c.send(replyFrame(0)) // "Here you go.": hands the book in and completes
	q1.waitFor(c, protocol.MessageDialogueClose)
	q1.waitFor(c, protocol.MessageQuestCompleted) // the store has committed it
	if p.Inv.Count(book.ID) != 0 {
		t.Fatal("book not handed in")
	}
	q1.w.Tick()
	if err := q1.sess.SaveAll(ctx); err != nil {
		t.Fatal(err)
	}

	log, err := players.(store.Quests).LoadQuests(ctx, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []store.QuestProgress{{Quest: overdue.Key, State: store.QuestComplete}}
	if !reflect.DeepEqual(log.Quests, want) || log.Points != int64(overdue.Rewards.QuestPoints) {
		t.Fatalf("stored %+v, want %+v with %d points", log, want, overdue.Rewards.QuestPoints)
	}
	q2 := newQuestRig(t, players)
	c2, _, _ := q2.join(tok)
	rows := []questRow{{QuestId: uint16(overdue.ID), State: protocol.QuestStateComplete}}
	if got := q2.questLog(c2); !reflect.DeepEqual(got.rows, rows) {
		t.Fatalf("log after reconnect %+v, want %+v", got.rows, rows)
	}
}

func hashOf(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}
