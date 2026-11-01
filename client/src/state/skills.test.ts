import { describe, expect, it } from 'vitest';
import { Skill, SkillResultCode } from '../protocol';
import { MAX_SKILL_EVENTS, SkillState, XP_TABLE, xpToNextLevel } from './skills';
import { GameState } from './world';

describe('XP table', () => {
  it('matches the OSRS experience table', () => {
    expect(XP_TABLE[1]).toBe(0);
    expect(XP_TABLE[2]).toBe(83);
    expect(XP_TABLE[10]).toBe(1154);
    expect(XP_TABLE[15]).toBe(2411);
    expect(XP_TABLE[30]).toBe(13363);
    expect(XP_TABLE.length).toBe(31);
  });

  it('gives the XP still needed, 0 at level 30', () => {
    expect(xpToNextLevel(1, 0)).toBe(83);
    expect(xpToNextLevel(1, 37.5)).toBe(45.5);
    expect(xpToNextLevel(14, 2400.5)).toBe(10.5);
    expect(xpToNextLevel(29, 13000)).toBe(363);
    expect(xpToNextLevel(30, 13363)).toBe(0);
    expect(xpToNextLevel(30, 50000)).toBe(0);
  });
});

describe('SkillState', () => {
  it('holds each skill level and XP, XP to next level and total level', () => {
    const s = new SkillState();
    expect(s.totalLevel(3)).toBe(6);
    s.setStats([
      { skill: Skill.Woodcutting, level: 14, xp: 2400.5 },
      { skill: Skill.Fishing, level: 2, xp: 100 },
      { skill: Skill.Cooking, level: 30, xp: 14000 },
    ]);
    expect([s.level(Skill.Woodcutting), s.xp(Skill.Woodcutting), s.xpToNext(Skill.Woodcutting)]).toEqual([14, 2400.5, 10.5]);
    expect([s.level(Skill.Fishing), s.xp(Skill.Fishing), s.xpToNext(Skill.Fishing)]).toEqual([2, 100, 74]);
    expect([s.level(Skill.Cooking), s.xp(Skill.Cooking), s.xpToNext(Skill.Cooking)]).toEqual([30, 14000, 0]);
    expect(s.totalLevel(12)).toBe(12 + 14 + 2 + 30);
  });

  it('total level uses the combat level from PlayerStats in GameState', () => {
    const g = new GameState();
    g.handle({ type: 'playerStats', hp: 10, maxHp: 10, combatLevel: 5, xp: 0, gold: 0 }, 0);
    g.handle({
      type: 'skillStats',
      skills: [
        { skill: Skill.Woodcutting, level: 4, xp: 300 },
        { skill: Skill.Fishing, level: 1, xp: 0 },
        { skill: Skill.Cooking, level: 2, xp: 90 },
      ],
    }, 0);
    expect(g.skills.totalLevel(g.items.stats.combatLevel)).toBe(12);
  });

  it('ResourceState replaces felled trees and fishing spot candidates', () => {
    const g = new GameState();
    g.handle({ type: 'resourceState', felledTrees: [1, 5], fishingSpots: [{ id: 0, candidate: 2 }, { id: 1, candidate: 0 }] }, 0);
    expect(g.skills.isFelled(1)).toBe(true);
    expect(g.skills.isFelled(5)).toBe(true);
    expect(g.skills.isFelled(2)).toBe(false);
    expect(g.skills.fishingSpotCandidate(0)).toBe(2);
    expect(g.skills.fishingSpotCandidate(1)).toBe(0);

    g.handle({ type: 'resourceState', felledTrees: [2], fishingSpots: [{ id: 0, candidate: 1 }] }, 0);
    expect(g.skills.felledTrees()).toEqual([2]);
    expect(g.skills.isFelled(1)).toBe(false);
    expect(g.skills.fishingSpots()).toEqual([{ id: 0, candidate: 1 }]);
    expect(g.skills.fishingSpotCandidate(1)).toBeUndefined();

    g.handle({ type: 'resourceState', felledTrees: [], fishingSpots: [] }, 0);
    expect(g.skills.felledTrees()).toEqual([]);
    expect(g.skills.fishingSpots()).toEqual([]);
  });

  it('queues LevelUp and SkillResult events, each read once', () => {
    const g = new GameState();
    g.handle({ type: 'levelUp', entityId: 3, skill: Skill.Fishing, level: 5 }, 0);
    g.handle({ type: 'levelUp', entityId: 9, skill: Skill.Cooking, level: 2 }, 0);
    g.handle({ type: 'skillResult', skill: Skill.Woodcutting, code: SkillResultCode.Success, itemId: 30, level: 0 }, 0);
    g.handle({ type: 'skillResult', skill: Skill.Woodcutting, code: SkillResultCode.InventoryFull, itemId: 0, level: 0 }, 0);

    expect(g.skills.levelUps.drain()).toEqual([
      { entityId: 3, skill: Skill.Fishing, level: 5 },
      { entityId: 9, skill: Skill.Cooking, level: 2 },
    ]);
    expect(g.skills.levelUps.drain()).toEqual([]);
    expect(g.skills.results.drain()).toEqual([
      { skill: Skill.Woodcutting, code: SkillResultCode.Success, itemId: 30, level: 0 },
      { skill: Skill.Woodcutting, code: SkillResultCode.InventoryFull, itemId: 0, level: 0 },
    ]);
    expect(g.skills.results.length).toBe(0);
  });

  it('drops the oldest events past MAX_SKILL_EVENTS', () => {
    const s = new SkillState();
    for (let i = 0; i < MAX_SKILL_EVENTS + 5; i++) s.levelUps.push({ entityId: i, skill: Skill.Fishing, level: 2 });
    const out = s.levelUps.drain();
    expect(out.length).toBe(MAX_SKILL_EVENTS);
    expect(out[0].entityId).toBe(5);
  });

  it('resets on Welcome', () => {
    const g = new GameState();
    g.handle({ type: 'skillStats', skills: [{ skill: Skill.Fishing, level: 9, xp: 1000 }] }, 0);
    g.handle({ type: 'resourceState', felledTrees: [1], fishingSpots: [] }, 0);
    g.handle({ type: 'levelUp', entityId: 1, skill: Skill.Fishing, level: 9 }, 0);
    g.handle({ type: 'welcome', playerId: 1, guestToken: '', spawn: { x: 0, z: 0 }, tickRate: 20 }, 0);
    expect(g.skills.level(Skill.Fishing)).toBe(1);
    expect(g.skills.isFelled(1)).toBe(false);
    expect(g.skills.levelUps.length).toBe(0);
  });
});
