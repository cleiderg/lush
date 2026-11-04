// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest';
import { EquipSlot, ItemKind, QuestSkill, QuestState } from '../protocol';
import { ItemState, type ItemInfo } from '../state/items';
import { QuestStore, type QuestRewardsInfo } from '../state/quests';
import { QuestScroll } from './questScroll';

function item(id: number, name: string): ItemInfo {
  return {
    id, name, kind: ItemKind.Loot, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0,
    attackSpeed: 0, heal: 0, shopBuysFor: 1, shopSells: false, shopSellsFor: 0, toolSkill: 0, toolLevel: 0, cookingLevel: 0, footprintW: 0, footprintD: 0, placement: 0, surfaceSlots: 0, thriftSellsFor: 0, thriftBuysFor: 0,
  };
}

/** Overdue's rewards (server quests.json). */
const OVERDUE: QuestRewardsInfo = { questPoints: 1, gold: 150, items: [{ itemId: 9, count: 1 }], xp: [{ skill: QuestSkill.Combat, xp: 500 }] };

function setup() {
  document.body.replaceChildren();
  const quests = new QuestStore();
  const items = new ItemState();
  items.setCatalog([item(9, 'Library card')]);
  const scroll = new QuestScroll({ parent: document.body, quests, items });
  return { quests, scroll };
}

const inProgress = (q: QuestStore) => q.setLog([{ questId: 1, state: QuestState.InProgress, step: 2, count: 0 }], 0);
const complete = (q: QuestStore) => q.setLog([{ questId: 1, state: QuestState.Complete, step: 3, count: 0 }], 1);

const lines = (s: QuestScroll): string[] => [...s.panel.querySelectorAll('.lush-quest-scroll-line')].map((e) => e.textContent ?? '');

describe('QuestScroll', () => {
  beforeEach(() => document.body.replaceChildren());

  it('opens for the local player with the name and every reward line (AC1)', () => {
    const { quests, scroll } = setup();
    inProgress(quests);
    complete(quests);
    quests.setCompleted('Cleider', 1, 'Overdue', OVERDUE);
    expect(scroll.isOpen).toBe(true);
    expect(scroll.panel.classList.contains('lush-panel')).toBe(true);
    expect(scroll.panel.querySelector('.lush-quest-scroll-title')?.textContent).toBe('Overdue');
    expect(lines(scroll)).toEqual(['1 quest point', '150 gold', 'Library card', '500 Combat XP']);
  });

  it('opens when the log update arrives just after QuestCompleted (AC1)', () => {
    const { quests, scroll } = setup();
    inProgress(quests);
    quests.setCompleted('Cleider', 1, 'Overdue', OVERDUE);
    expect(scroll.isOpen).toBe(false);
    complete(quests);
    expect(scroll.isOpen).toBe(true);
  });

  it("doesn't open for another player's QuestCompleted (AC2)", () => {
    const { quests, scroll } = setup();
    quests.setCompleted('Sam', 1, 'Overdue', OVERDUE);
    quests.setLog([{ questId: 1, state: QuestState.InProgress, step: 1, count: 0 }], 0);
    expect(scroll.isOpen).toBe(false);
  });

  it("doesn't open when another player finishes a quest the player finished earlier (AC2)", () => {
    const { quests, scroll } = setup();
    complete(quests); // the log at login
    quests.setCompleted('Sam', 1, 'Overdue', OVERDUE);
    complete(quests);
    expect(scroll.isOpen).toBe(false);
  });

  it('closes with its button (AC3)', () => {
    const { quests, scroll } = setup();
    inProgress(quests);
    complete(quests);
    quests.setCompleted('Cleider', 1, 'Overdue', OVERDUE);
    expect(scroll.isOpen).toBe(true);
    scroll.panel.querySelector<HTMLElement>('.lush-quest-scroll-close')!.click();
    expect(scroll.isOpen).toBe(false);
    expect(scroll.panel.hidden).toBe(true);
  });
});
