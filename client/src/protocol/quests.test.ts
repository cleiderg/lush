import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  Dialogue, DialogueClose, DialogueReply, Envelope, ItemDef, Message, NpcDef, QuestCatalog, QuestCompleted,
  QuestDef, QuestItemReward, QuestLog, QuestProgress, QuestRequirement, QuestRequirementKind, QuestResult,
  QuestResultCode, QuestRewards, QuestSkill, QuestState, QuestXpReward, Talk,
} from './index.js';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array();
}

function open<T>(buf: Uint8Array, type: Message, m: T): T {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(buf));
  expect(env.msgType()).toBe(type);
  return env.msg(m as never) as T;
}

function rewards(b: flatbuffers.Builder): flatbuffers.Offset {
  const it = QuestItemReward.createQuestItemReward(b, 301, 1);
  const xp = QuestXpReward.createQuestXpReward(b, QuestSkill.Cooking, 1500);
  return QuestRewards.createQuestRewards(b, 1, 250,
    QuestRewards.createItemsVector(b, [it]), QuestRewards.createXpVector(b, [xp]));
}

function checkRewards(r: QuestRewards) {
  expect([r.questPoints(), r.gold(), r.itemsLength(), r.xpLength()]).toEqual([1, 250, 1, 1]);
  expect([r.items(0)!.itemId(), r.items(0)!.count()]).toEqual([301, 1]);
  expect([r.xp(0)!.skill(), r.xp(0)!.xp()]).toEqual([QuestSkill.Cooking, 1500]);
}

describe('quest protocol', () => {
  it('round-trips QuestCatalog', () => {
    const b = new flatbuffers.Builder(512);
    const reqs = QuestDef.createRequirementsVector(b, [
      QuestRequirement.createQuestRequirement(b, QuestRequirementKind.Level, QuestSkill.Combat, 5),
      QuestRequirement.createQuestRequirement(b, QuestRequirementKind.Quest, 0, 0, 2),
    ]);
    const steps = QuestDef.createStepsVector(b, [b.createString('Find the book.'), b.createString('Return it.')]);
    const rw = rewards(b);
    QuestDef.startQuestDef(b);
    QuestDef.addId(b, 1);
    QuestDef.addName(b, b.createString('Overdue'));
    QuestDef.addDescription(b, b.createString('Return the book.'));
    QuestDef.addRequirements(b, reqs);
    QuestDef.addSteps(b, steps);
    QuestDef.addRewards(b, rw);
    QuestDef.addStartNpc(b, 7);
    QuestDef.addFinishNpc(b, 8);
    const q = QuestDef.endQuestDef(b);
    const buf = finish(b, Message.QuestCatalog,
      QuestCatalog.createQuestCatalog(b, QuestCatalog.createQuestsVector(b, [q])));
    const m = open(buf, Message.QuestCatalog, new QuestCatalog());
    const d = m.quests(0)!;
    expect([m.questsLength(), d.id(), d.name(), d.description(), d.stepsLength(), d.steps(1), d.startNpc(), d.finishNpc()])
      .toEqual([1, 1, 'Overdue', 'Return the book.', 2, 'Return it.', 7, 8]);
    expect([d.requirements(0)!.kind(), d.requirements(0)!.skill(), d.requirements(0)!.level()])
      .toEqual([QuestRequirementKind.Level, QuestSkill.Combat, 5]);
    expect([d.requirements(1)!.kind(), d.requirements(1)!.questId()]).toEqual([QuestRequirementKind.Quest, 2]);
    checkRewards(d.rewards()!);
  });

  it('round-trips QuestLog', () => {
    const b = new flatbuffers.Builder(128);
    const p1 = QuestProgress.createQuestProgress(b, 1, QuestState.InProgress, 2, 3);
    const p2 = QuestProgress.createQuestProgress(b, 2);
    const buf = finish(b, Message.QuestLog,
      QuestLog.createQuestLog(b, QuestLog.createQuestsVector(b, [p1, p2]), 4));
    const m = open(buf, Message.QuestLog, new QuestLog());
    const a = m.quests(0)!;
    const e = m.quests(1)!;
    expect([m.questsLength(), m.questPoints(), a.questId(), a.state(), a.step(), a.count()])
      .toEqual([2, 4, 1, QuestState.InProgress, 2, 3]);
    expect([e.state(), e.step(), e.count()]).toEqual([QuestState.NotStarted, 0, 0]);
  });

  it('round-trips Talk, DialogueReply, Dialogue and DialogueClose', () => {
    let b = new flatbuffers.Builder(64);
    expect(open(finish(b, Message.Talk, Talk.createTalk(b, 42)), Message.Talk, new Talk()).npcId()).toBe(42);
    b = new flatbuffers.Builder(64);
    expect(open(finish(b, Message.DialogueReply, DialogueReply.createDialogueReply(b, 3)),
      Message.DialogueReply, new DialogueReply()).index()).toBe(3);
    b = new flatbuffers.Builder(256);
    const lines = Dialogue.createLinesVector(b, [b.createString('Hello.'), b.createString('Need help?')]);
    const replies = Dialogue.createRepliesVector(b, ['a', 'b', 'c', 'd'].map((s) => b.createString(s)));
    const d = open(finish(b, Message.Dialogue, Dialogue.createDialogue(b, 42, lines, replies)),
      Message.Dialogue, new Dialogue());
    expect([d.npcId(), d.linesLength(), d.lines(1), d.repliesLength(), d.replies(3)])
      .toEqual([42, 2, 'Need help?', 4, 'd']);
    b = new flatbuffers.Builder(16);
    open(finish(b, Message.DialogueClose, DialogueClose.createDialogueClose(b)), Message.DialogueClose, new DialogueClose());
  });

  it('round-trips QuestCompleted and QuestResult', () => {
    let b = new flatbuffers.Builder(256);
    const rw = rewards(b);
    QuestCompleted.startQuestCompleted(b);
    QuestCompleted.addPlayerName(b, b.createString('Cleider'));
    QuestCompleted.addQuestId(b, 1);
    QuestCompleted.addQuestName(b, b.createString('Overdue'));
    QuestCompleted.addRewards(b, rw);
    const buf = finish(b, Message.QuestCompleted, QuestCompleted.endQuestCompleted(b));
    const m = open(buf, Message.QuestCompleted, new QuestCompleted());
    expect([m.playerName(), m.questId(), m.questName()]).toEqual(['Cleider', 1, 'Overdue']);
    checkRewards(m.rewards()!);
    for (const code of [QuestResultCode.TooFar, QuestResultCode.NotTalkable,
      QuestResultCode.RequirementsNotMet, QuestResultCode.InventoryFull]) {
      b = new flatbuffers.Builder(16);
      const r = open(finish(b, Message.QuestResult, QuestResult.createQuestResult(b, code)),
        Message.QuestResult, new QuestResult());
      expect(r.code()).toBe(code);
    }
  });

  it('reads quest_item and talkable as false when absent', () => {
    let b = new flatbuffers.Builder(64);
    b.finish(ItemDef.createItemDef(b, 9, 0));
    expect(ItemDef.getRootAsItemDef(new flatbuffers.ByteBuffer(b.asUint8Array())).questItem()).toBe(false);
    b = new flatbuffers.Builder(64);
    b.finish(NpcDef.createNpcDef(b, 9, 0));
    expect(NpcDef.getRootAsNpcDef(new flatbuffers.ByteBuffer(b.asUint8Array())).talkable()).toBe(false);
    b = new flatbuffers.Builder(64);
    ItemDef.startItemDef(b);
    ItemDef.addQuestItem(b, true);
    b.finish(ItemDef.endItemDef(b));
    expect(ItemDef.getRootAsItemDef(new flatbuffers.ByteBuffer(b.asUint8Array())).questItem()).toBe(true);
    b = new flatbuffers.Builder(64);
    NpcDef.startNpcDef(b);
    NpcDef.addTalkable(b, true);
    b.finish(NpcDef.endNpcDef(b));
    expect(NpcDef.getRootAsNpcDef(new flatbuffers.ByteBuffer(b.asUint8Array())).talkable()).toBe(true);
  });
});
