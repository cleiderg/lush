import { describe, expect, it } from 'vitest';
import { BankResultCode, Skill, SkillResultCode } from '../protocol';
import { bankResultText } from './skillMessages';
import { GameState } from './world';

function game(): GameState {
  const g = new GameState();
  g.playerId = 7;
  g.items.setCatalog([{ id: 5, name: 'Hatchet' }, { id: 9, name: 'Logs' }] as never);
  return g;
}

const lines = (g: GameState) => g.chat.log.map((l) => l.text);

describe('skill result chat lines', () => {
  const cases: [SkillResultCode, number, number, string][] = [
    [SkillResultCode.Started, 0, 0, 'You swing your hatchet at the tree'],
    [SkillResultCode.Success, 9, 0, 'You get some logs'],
    [SkillResultCode.Burnt, 0, 0, 'You accidentally burn the fish'],
    [SkillResultCode.NeedTool, 5, 0, 'You need a hatchet'],
    [SkillResultCode.NeedLevel, 0, 15, 'You need Woodcutting level 15'],
    [SkillResultCode.InventoryFull, 0, 0, 'Your inventory is full'],
    [SkillResultCode.ResourceGone, 0, 0, 'There is nothing left there'],
    [SkillResultCode.NothingToCook, 0, 0, 'You have nothing to cook'],
    [SkillResultCode.OutOfRange, 0, 0, 'You are too far away'],
    [SkillResultCode.Stopped, 0, 0, 'You stop'],
  ];
  it.each(cases)('code %i becomes its line', (code, itemId, level, text) => {
    const g = game();
    g.handle({ type: 'skillResult', skill: Skill.Woodcutting, code, itemId, level }, 0);
    expect(lines(g)).toEqual([text]);
    expect(g.chat.log[0].system).toBe(true);
  });

  it('covers every code', () => {
    const n = Object.values(SkillResultCode).filter((v) => typeof v === 'number').length;
    expect(cases.length).toBe(n);
  });
});

describe('bank result chat lines', () => {
  it('adds a line for every refusal, including SaveFailed, and none for Ok', () => {
    const g = game();
    const codes = Object.values(BankResultCode).filter((v): v is BankResultCode => typeof v === 'number');
    for (const code of codes) {
      const before = g.chat.log.length;
      g.handle({ type: 'bankResult', code }, 0);
      expect(g.chat.log.length - before).toBe(code === BankResultCode.Ok ? 0 : 1);
    }
    expect(bankResultText(BankResultCode.SaveFailed)).toBeTruthy();
    expect(bankResultText(BankResultCode.Ok)).toBeNull();
  });
});

describe('level-up chat lines', () => {
  it('adds a line for the local player only', () => {
    const g = game();
    g.handle({ type: 'levelUp', entityId: 3, skill: Skill.Fishing, level: 5 }, 0);
    expect(lines(g)).toEqual([]);
    g.handle({ type: 'levelUp', entityId: 7, skill: Skill.Fishing, level: 5 }, 0);
    expect(lines(g)).toEqual(['You reached Fishing level 5']);
  });
});
