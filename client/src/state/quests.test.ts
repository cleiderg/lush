import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  Dialogue, DialogueClose, DialogueReply, Envelope, Message, QuestCatalog, QuestCompleted, QuestDef, QuestLog, QuestProgress,
  QuestRequirement, QuestRequirementKind, QuestResult, QuestResultCode, QuestRewards, QuestState, Talk, Vec2, Welcome,
} from '../protocol';
import { decode, encodeDialogueReply, encodeTalk } from '../net/codec';
import { Session } from '../net/session';
import type { Transport } from '../net/transport';
import { GameState } from './world';
import { QuestStore, questResultText } from './quests';
import type { QuestInfo } from './quests';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

function feed(g: GameState, bytes: Uint8Array): void {
  const msg = decode(bytes);
  if (!msg) throw new Error('undecodable');
  g.handle(msg, 0);
}

function rewards(b: flatbuffers.Builder): flatbuffers.Offset {
  QuestRewards.startQuestRewards(b);
  QuestRewards.addQuestPoints(b, 2);
  QuestRewards.addGold(b, 400);
  return QuestRewards.endQuestRewards(b);
}

function catalogBytes(): Uint8Array {
  const b = new flatbuffers.Builder(512);
  const req = QuestRequirement.createQuestRequirement(b, QuestRequirementKind.Level, 0, 10, 0);
  const reqs = QuestDef.createRequirementsVector(b, [req]);
  const steps = QuestDef.createStepsVector(b, [b.createString('Talk to A'), b.createString('Return to B')]);
  const name = b.createString('Overdue');
  const desc = b.createString('Return a book');
  const rw = rewards(b);
  QuestDef.startQuestDef(b);
  QuestDef.addId(b, 1);
  QuestDef.addName(b, name);
  QuestDef.addDescription(b, desc);
  QuestDef.addRequirements(b, reqs);
  QuestDef.addSteps(b, steps);
  QuestDef.addRewards(b, rw);
  QuestDef.addStartNpc(b, 5);
  QuestDef.addFinishNpc(b, 6);
  const def = QuestDef.endQuestDef(b);
  return finish(b, Message.QuestCatalog, QuestCatalog.createQuestCatalog(b, QuestCatalog.createQuestsVector(b, [def])));
}

function logBytes(state: QuestState, step: number, points = 3): Uint8Array {
  const b = new flatbuffers.Builder(128);
  const p = QuestProgress.createQuestProgress(b, 1, state, step, 0);
  return finish(b, Message.QuestLog, QuestLog.createQuestLog(b, QuestLog.createQuestsVector(b, [p]), points));
}

function dialogueBytes(): Uint8Array {
  const b = new flatbuffers.Builder(128);
  const lines = Dialogue.createLinesVector(b, [b.createString('Hello'), b.createString('Well?')]);
  const replies = Dialogue.createRepliesVector(b, [b.createString('Yes'), b.createString('No')]);
  return finish(b, Message.Dialogue, Dialogue.createDialogue(b, 9, lines, replies));
}

describe('quest messages', () => {
  it('decodes catalog, log, dialogue, close, completed and result into the store (AC1, AC4)', () => {
    const g = new GameState();
    feed(g, catalogBytes());
    feed(g, logBytes(QuestState.InProgress, 1));
    feed(g, dialogueBytes());
    const q = g.quests.quest(1)!;
    expect(q).toMatchObject({ name: 'Overdue', startNpc: 5, finishNpc: 6, steps: ['Talk to A', 'Return to B'] });
    expect(q.requirements[0]).toMatchObject({ level: 10, kind: QuestRequirementKind.Level });
    expect(q.rewards).toMatchObject({ questPoints: 2, gold: 400 });
    expect(g.quests.progressOf(1)).toMatchObject({ state: QuestState.InProgress, step: 1 });
    expect(g.quests.questPoints).toBe(3);
    expect(g.quests.dialogue).toEqual({ npcId: 9, lines: ['Hello', 'Well?'], replies: ['Yes', 'No'] });

    const b = new flatbuffers.Builder(16);
    DialogueClose.startDialogueClose(b);
    feed(g, finish(b, Message.DialogueClose, DialogueClose.endDialogueClose(b)));
    expect(g.quests.dialogue).toBeNull();

    const c = new flatbuffers.Builder(128);
    const name = c.createString('Cleider');
    const qn = c.createString('Overdue');
    const rw = rewards(c);
    feed(g, finish(c, Message.QuestCompleted, (() => {
      QuestCompleted.startQuestCompleted(c);
      QuestCompleted.addPlayerName(c, name);
      QuestCompleted.addQuestId(c, 1);
      QuestCompleted.addQuestName(c, qn);
      QuestCompleted.addRewards(c, rw);
      return QuestCompleted.endQuestCompleted(c);
    })()));
    expect(g.chat.log.at(-1)).toMatchObject({ text: 'Cleider has completed Overdue!', system: true });
    expect(g.quests.completed).toMatchObject({ questId: 1, questName: 'Overdue' });
  });

  it('turns each QuestResult code into a chat line, none for Ok (AC4)', () => {
    const g = new GameState();
    const codes = Object.values(QuestResultCode).filter((v): v is QuestResultCode => typeof v === 'number');
    for (const code of codes) {
      const b = new flatbuffers.Builder(16);
      const before = g.chat.log.length;
      feed(g, finish(b, Message.QuestResult, QuestResult.createQuestResult(b, code)));
      expect(g.chat.log.length).toBe(before + (code === QuestResultCode.Ok ? 0 : 1));
      if (code !== QuestResultCode.Ok) expect(questResultText(code)).toMatch(/\S/);
    }
  });

  it('encodes Talk and DialogueReply (AC2)', () => {
    const t = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(encodeTalk(77)));
    expect(t.msgType()).toBe(Message.Talk);
    expect((t.msg(new Talk()) as Talk).npcId()).toBe(77);
    const r = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(encodeDialogueReply(2)));
    expect(r.msgType()).toBe(Message.DialogueReply);
    expect((r.msg(new DialogueReply()) as DialogueReply).index()).toBe(2);
  });
});

describe('QuestStore.markerFor (AC3)', () => {
  const quest: QuestInfo = {
    id: 1, name: 'Overdue', description: '', steps: ['a', 'b'], startNpc: 5, finishNpc: 6,
    requirements: [{ kind: QuestRequirementKind.Level, skill: 0, level: 10, questId: 0 }],
    rewards: { questPoints: 1, gold: 0, items: [], xp: [] },
  };

  it('is start only when not started and requirements are met', () => {
    let level = 5;
    const s = new QuestStore(() => level);
    s.setCatalog([quest]);
    expect(s.markerFor(5)).toBeNull();
    level = 10;
    expect(s.markerFor(5)).toBe('start');
    expect(s.markerFor(6)).toBeNull();
    s.setLog([{ questId: 1, state: QuestState.Complete, step: 0, count: 0 }], 1);
    expect(s.markerFor(5)).toBeNull();
  });

  it('is finish on the step that ends with the NPC', () => {
    const s = new QuestStore(() => 10);
    s.setCatalog([quest]);
    s.setLog([{ questId: 1, state: QuestState.InProgress, step: 0, count: 0 }], 0);
    expect(s.markerFor(6)).toBeNull();
    expect(s.markerFor(5)).toBeNull();
    s.setLog([{ questId: 1, state: QuestState.InProgress, step: 1, count: 0 }], 0);
    expect(s.markerFor(6)).toBe('finish');
  });

  it('requires a prerequisite quest to be complete', () => {
    const s = new QuestStore(() => 10);
    s.setCatalog([{ ...quest, id: 2, requirements: [{ kind: QuestRequirementKind.Quest, skill: 0, level: 0, questId: 1 }] }]);
    expect(s.markerFor(5)).toBeNull();
    s.setLog([{ questId: 1, state: QuestState.Complete, step: 0, count: 0 }], 1);
    expect(s.markerFor(5)).toBe('start');
  });
});

describe('Session sends (AC2)', () => {
  class FakeTransport implements Transport {
    onMessage: ((b: Uint8Array) => void) | null = null;
    onOpen: (() => void) | null = null;
    onClose: (() => void) | null = null;
    sent: Uint8Array[] = [];
    send(b: Uint8Array) { this.sent.push(b); }
    close() {}
  }

  it('sendTalk and sendDialogueReply send Talk and DialogueReply', () => {
    const t = new FakeTransport();
    const s = new Session({ factory: () => t, store: { get: () => '', set: () => {} }, onMessage: () => {} });
    s.start();
    const b = new flatbuffers.Builder(64);
    const tok = b.createString('tok');
    Welcome.startWelcome(b);
    Welcome.addGuestToken(b, tok);
    Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
    t.onMessage?.(finish(b, Message.Welcome, Welcome.endWelcome(b)));

    s.sendTalk(21);
    s.sendDialogueReply(3);
    const envs = t.sent.map((bytes) => Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes)));
    expect(envs.map((e) => e.msgType())).toEqual([Message.Talk, Message.DialogueReply]);
    expect((envs[0].msg(new Talk()) as Talk).npcId()).toBe(21);
    expect((envs[1].msg(new DialogueReply()) as DialogueReply).index()).toBe(3);
  });
});
