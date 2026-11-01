import { BankResultCode, Skill, SkillResultCode } from '../protocol';
import type { LevelUpEvent, SkillResultEvent } from './skills';

const SKILL_NAMES: Record<Skill, string> = {
  [Skill.Woodcutting]: 'Woodcutting',
  [Skill.Fishing]: 'Fishing',
  [Skill.Cooking]: 'Cooking',
};

export function skillName(skill: Skill): string {
  return SKILL_NAMES[skill] ?? 'Unknown';
}

function withArticle(name: string): string {
  const n = name.toLowerCase();
  return /^[aeiou]/.test(n) ? `an ${n}` : `a ${n}`;
}

const STARTED: Record<Skill, string> = {
  [Skill.Woodcutting]: 'You swing your hatchet at the tree',
  [Skill.Fishing]: 'You cast your line',
  [Skill.Cooking]: 'You start cooking',
};

/**
 * The chat-log line for a SkillResult (docs/specs/skills.md: Client). `itemName` looks up an item in the
 * catalog. The Started, Success, Burnt and Stopped lines follow OSRS's wording style; the spec only gives the
 * refusals.
 */
export function skillResultText(ev: SkillResultEvent, itemName: (id: number) => string | undefined): string {
  const item = ev.itemId ? itemName(ev.itemId) : undefined;
  switch (ev.code) {
    case SkillResultCode.Started: return STARTED[ev.skill] ?? 'You get started';
    case SkillResultCode.Success: return item ? `You get some ${item.toLowerCase()}` : 'You get something';
    case SkillResultCode.Burnt: return 'You accidentally burn the fish';
    case SkillResultCode.NeedTool: return item ? `You need ${withArticle(item)}` : 'You need a tool for that';
    case SkillResultCode.NeedLevel: return `You need ${skillName(ev.skill)} level ${ev.level}`;
    case SkillResultCode.InventoryFull: return 'Your inventory is full';
    case SkillResultCode.ResourceGone: return 'There is nothing left there';
    case SkillResultCode.NothingToCook: return 'You have nothing to cook';
    case SkillResultCode.OutOfRange: return 'You are too far away';
    case SkillResultCode.Stopped: return 'You stop';
    default: return 'Something went wrong';
  }
}

/** The chat-log line for a BankResult refusal, or null for Ok. */
export function bankResultText(code: BankResultCode): string | null {
  switch (code) {
    case BankResultCode.Ok: return null;
    case BankResultCode.TooFar: return 'You are too far from the bank booth';
    case BankResultCode.InCombat: return "You can't use the bank in combat";
    case BankResultCode.InPvpZone: return "You can't use the bank in a PvP zone";
    case BankResultCode.BankFull: return 'Your bank is full';
    case BankResultCode.InventoryFull: return 'Your inventory is full';
    case BankResultCode.NotEnoughItems: return "You don't have enough of that";
    case BankResultCode.Busy: return 'The bank is busy. Try again';
    case BankResultCode.NotOpen: return 'The bank is not open';
    case BankResultCode.SaveFailed: return 'The bank could not save. Try again';
    default: return 'Something went wrong';
  }
}

/** The chat-log line for the local player's LevelUp. */
export function levelUpText(ev: LevelUpEvent): string {
  return `You reached ${skillName(ev.skill)} level ${ev.level}`;
}
