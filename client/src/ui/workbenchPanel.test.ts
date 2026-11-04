// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EquipSlot, ItemKind } from '../protocol';
import { ItemState, type ItemInfo } from '../state/items';
import { WorkbenchStore } from '../state/workbench';
import { canCraft, WorkbenchPanel } from './workbenchPanel';

function item(id: number, name: string): ItemInfo {
  return {
    id, name, kind: ItemKind.Loot, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0, attackSpeed: 0,
    heal: 0, shopBuysFor: 1, shopSells: false, shopSellsFor: 0, toolSkill: 0, toolLevel: 0, cookingLevel: 0, footprintW: 0, footprintD: 0, placement: 0, surfaceSlots: 0, thriftSellsFor: 0, thriftBuysFor: 0,
  };
}

const RECIPES = [
  { outputItemId: 40, inputs: [{ itemId: 1, quantity: 4 }], gold: 30 },
  { outputItemId: 41, inputs: [{ itemId: 1, quantity: 10 }, { itemId: 2, quantity: 2 }], gold: 80 },
];

let panel: WorkbenchPanel | null = null;

function setup(logs: number, planks: number, gold: number) {
  const items = new ItemState();
  items.setCatalog([item(1, 'Logs'), item(2, 'Nails'), item(40, 'Beanbag'), item(41, 'Study desk')]);
  const slots = Array.from({ length: 28 }, () => ({ itemId: 0, quantity: 0 }));
  slots[0] = { itemId: 1, quantity: logs };
  slots[3] = { itemId: 2, quantity: planks };
  items.setInventory({ slots, equipment: { weapon: 0, head: 0, body: 0, shield: 0 } });
  items.setStats({ hp: 10, maxHp: 10, combatLevel: 3, xp: 0, gold });
  const workbench = new WorkbenchStore();
  workbench.setRecipes(RECIPES);
  const sendCraft = vi.fn<(id: number) => void>();
  panel = new WorkbenchPanel({ parent: document.body, workbench, items, sendCraft, viewport: () => ({ width: 390, height: 844 }) });
  panel.open();
  return { items, sendCraft, panel };
}

const row = (id: number): HTMLElement => document.querySelector(`.lush-bench-row[data-output="${id}"]`) as HTMLElement;

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  panel?.dispose();
  panel = null;
});

describe('workbench panel', () => {
  it('lists every recipe with output name, inputs held over needed and gold (AC1)', () => {
    setup(6, 1, 50);
    expect(document.querySelectorAll('.lush-bench-row')).toHaveLength(RECIPES.length);
    expect(row(40).querySelector('.lush-bench-name')?.textContent).toBe('Beanbag');
    expect(row(40).querySelector('.lush-bench-input')?.textContent).toBe('Logs 6/4');
    expect(row(40).querySelector('.lush-bench-gold')?.textContent).toBe('30 gp');
    expect(row(41).querySelector('.lush-bench-name')?.textContent).toBe('Study desk');
    expect([...row(41).querySelectorAll('.lush-bench-input')].map((e) => e.textContent)).toEqual(['Logs 6/10', 'Nails 1/2']);
    expect(row(41).querySelector('.lush-bench-gold')?.textContent).toBe('80 gp');
  });

  it('disables Craft without an input or the gold, and sends the output id otherwise (AC2)', () => {
    const { sendCraft, items } = setup(6, 1, 50);
    const craft = (id: number) => row(id).querySelector('button') as HTMLButtonElement;
    expect(craft(41).disabled).toBe(true);
    craft(41).click();
    expect(sendCraft).not.toHaveBeenCalled();
    expect(craft(40).disabled).toBe(false);
    craft(40).click();
    expect(sendCraft).toHaveBeenCalledExactlyOnceWith(40);

    items.setStats({ hp: 10, maxHp: 10, combatLevel: 3, xp: 0, gold: 29 });
    expect(craft(40).disabled).toBe(true);
    expect(canCraft(items, RECIPES[0])).toBe(false);
  });
});

describe('workbench catalog arriving after the panel opens', () => {
  it('fills the list', () => {
    const { panel: p } = setup(6, 1, 50);
    expect(p).toBeTruthy();
    const items = new ItemState();
    const workbench = new WorkbenchStore();
    const late = new WorkbenchPanel({ parent: document.body, workbench, items, sendCraft: () => undefined, viewport: () => ({ width: 390, height: 844 }) });
    late.open();
    expect(late.panel.querySelectorAll('.lush-bench-row')).toHaveLength(0);
    workbench.setRecipes(RECIPES);
    expect(late.panel.querySelectorAll('.lush-bench-row')).toHaveLength(2);
    late.dispose();
  });
});
