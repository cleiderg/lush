import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  Envelope, FishingSpotState, LevelUp, Message, ResourceState, Skill, SkillAction, SkillResult, SkillResultCode, SkillStats,
  SkillTarget, Vec2, Welcome,
} from '../protocol';
import { decode, encodeSkillAction } from './codec';
import { Session } from './session';
import type { Transport } from './transport';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

function skillAction(bytes: Uint8Array): SkillAction {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
  expect(env.msgType()).toBe(Message.SkillAction);
  return env.msg(new SkillAction()) as SkillAction;
}

describe('decode skill messages', () => {
  it('decodes SkillStats with XP in points from tenths', () => {
    const b = new flatbuffers.Builder(64);
    const m = SkillStats.createSkillStats(b, 15, 24005, 3, 375, 30, 1_000_000);
    expect(decode(finish(b, Message.SkillStats, m))).toEqual({
      type: 'skillStats',
      skills: [
        { skill: Skill.Woodcutting, level: 15, xp: 2400.5 },
        { skill: Skill.Fishing, level: 3, xp: 37.5 },
        { skill: Skill.Cooking, level: 30, xp: 100_000 },
      ],
    });
  });

  it('decodes ResourceState', () => {
    const b = new flatbuffers.Builder(64);
    const trees = ResourceState.createFelledTreesVector(b, [4, 17, 300]);
    ResourceState.startFishingSpotsVector(b, 2);
    FishingSpotState.createFishingSpotState(b, 2, 0);
    FishingSpotState.createFishingSpotState(b, 1, 3);
    const spots = b.endVector();
    expect(decode(finish(b, Message.ResourceState, ResourceState.createResourceState(b, trees, spots)))).toEqual({
      type: 'resourceState',
      felledTrees: [4, 17, 300],
      fishingSpots: [{ id: 1, candidate: 3 }, { id: 2, candidate: 0 }],
    });
  });

  it('decodes an empty ResourceState', () => {
    const b = new flatbuffers.Builder(32);
    ResourceState.startResourceState(b);
    expect(decode(finish(b, Message.ResourceState, ResourceState.endResourceState(b)))).toEqual({
      type: 'resourceState', felledTrees: [], fishingSpots: [],
    });
  });

  it('decodes LevelUp', () => {
    const b = new flatbuffers.Builder(32);
    expect(decode(finish(b, Message.LevelUp, LevelUp.createLevelUp(b, 42, Skill.Fishing, 10)))).toEqual({
      type: 'levelUp', entityId: 42, skill: Skill.Fishing, level: 10,
    });
  });

  it('decodes SkillResult', () => {
    const b = new flatbuffers.Builder(32);
    const m = SkillResult.createSkillResult(b, Skill.Woodcutting, SkillResultCode.NeedLevel, 0, 15);
    expect(decode(finish(b, Message.SkillResult, m))).toEqual({
      type: 'skillResult', skill: Skill.Woodcutting, code: SkillResultCode.NeedLevel, itemId: 0, level: 15,
    });
    const b2 = new flatbuffers.Builder(32);
    const m2 = SkillResult.createSkillResult(b2, Skill.Cooking, SkillResultCode.Burnt, 23, 0);
    expect(decode(finish(b2, Message.SkillResult, m2))).toEqual({
      type: 'skillResult', skill: Skill.Cooking, code: SkillResultCode.Burnt, itemId: 23, level: 0,
    });
  });
});

describe('encode SkillAction', () => {
  it('encodes a tree', () => {
    const m = skillAction(encodeSkillAction(SkillTarget.Tree, 12));
    expect([m.target(), m.targetId()]).toEqual([SkillTarget.Tree, 12]);
  });

  it('encodes a fishing spot', () => {
    const m = skillAction(encodeSkillAction(SkillTarget.FishingSpot, 3));
    expect([m.target(), m.targetId()]).toEqual([SkillTarget.FishingSpot, 3]);
  });

  it('encodes the grill with target id 0', () => {
    const m = skillAction(encodeSkillAction(SkillTarget.Grill));
    expect([m.target(), m.targetId()]).toEqual([SkillTarget.Grill, 0]);
  });

  it('is sent by Session.sendSkillAction once connected', () => {
    const sent: Uint8Array[] = [];
    const t: Transport = { onMessage: null, onOpen: null, onClose: null, send: (bytes) => sent.push(bytes), close: () => {} };
    const s = new Session({ factory: () => t, store: { get: () => '', set: () => {} }, onMessage: () => {} });
    s.start();
    const b = new flatbuffers.Builder(64);
    const tok = b.createString('tok');
    Welcome.startWelcome(b);
    Welcome.addGuestToken(b, tok);
    Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
    t.onMessage?.(finish(b, Message.Welcome, Welcome.endWelcome(b)));
    s.sendSkillAction(SkillTarget.Tree, 7);
    const m = skillAction(sent[sent.length - 1]);
    expect([m.target(), m.targetId()]).toEqual([SkillTarget.Tree, 7]);
  });
});
