// @vitest-environment happy-dom
import * as flatbuffers from 'flatbuffers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Envelope, EquipSlot, ItemAction, ItemActionKind, ItemKind, Message } from '../protocol';
import { ItemState, type ItemInfo } from '../state/items';
import { Controls } from './controls';
import { Hud } from './hud';
import { hudLayout } from './hudLayout';

function item(id: number, name: string, kind: ItemKind, equipSlot = EquipSlot.None): ItemInfo {
  return {
    id, name, kind, equipSlot, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0, attackSpeed: 0,
    heal: 0, shopBuysFor: 0, shopSells: false, shopSellsFor: 0, toolSkill: 0, toolLevel: 0, cookingLevel: 0,
  };
}

const CATALOG = [
  item(1, 'Wooden stick', ItemKind.Weapon, EquipSlot.Weapon),
  item(2, 'Hoodie', ItemKind.Armor, EquipSlot.Body),
  item(3, 'Boba', ItemKind.Food),
  item(4, 'Squirrel tail', ItemKind.Loot),
];

function emptySlots(n = 28) {
  return Array.from({ length: n }, () => ({ itemId: 0, quantity: 0 }));
}

let hud: Hud | null = null;

function setup(send?: (bytes: Uint8Array) => void) {
  const items = new ItemState();
  items.setCatalog(CATALOG);
  hud = new Hud({ parent: document.body, items, send, viewport: () => ({ width: 390, height: 844 }) });
  return { items, hud };
}

function decodeAction(bytes: Uint8Array) {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
  expect(env.msgType()).toBe(Message.ItemAction);
  const a = env.msg(new ItemAction()) as ItemAction;
  return { action: a.action(), slot: a.slot(), equipSlot: a.equipSlot() };
}

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  hud?.dispose();
  hud = null;
});

describe('Hud stats', () => {
  it('shows HP, combat level and gold and updates when state changes', () => {
    const { items, hud } = setup();
    items.setStats({ hp: 10, maxHp: 10, combatLevel: 3, xp: 0, gold: 25 });
    expect(hud.hud.textContent).toContain('HP 10/10');
    expect(hud.hud.textContent).toContain('Combat 3');
    expect(hud.hud.textContent).toContain('Gold 25');
    items.setStats({ hp: 4, maxHp: 12, combatLevel: 4, xp: 100, gold: 1250 });
    expect(hud.hud.textContent).toContain('HP 4/12');
    expect(hud.hud.textContent).toContain('Combat 4');
    expect(hud.hud.textContent).toContain('Gold 1,250');
  });
});

describe('Hud inventory panel', () => {
  it('opens and closes from the toggle', () => {
    const { hud } = setup();
    expect(hud.isOpen).toBe(false);
    expect(hud.panel.hidden).toBe(true);
    hud.toggle.click();
    expect(hud.panel.hidden).toBe(false);
    expect(hud.toggle.getAttribute('aria-expanded')).toBe('true');
    hud.toggle.click();
    expect(hud.panel.hidden).toBe(true);
    expect(hud.toggle.getAttribute('aria-expanded')).toBe('false');
  });

  it('shows one cell per slot with name and stack quantity from the catalog', () => {
    const { items, hud } = setup();
    const slots = emptySlots();
    slots[0] = { itemId: 3, quantity: 1 };
    slots[1] = { itemId: 4, quantity: 17 };
    items.setInventory({ slots, equipment: { weapon: 0, head: 0, body: 0, shield: 0 } });
    hud.toggle.click();
    const cells = hud.panel.querySelectorAll('.lush-slot');
    expect(cells).toHaveLength(28);
    expect(cells[0].querySelector('.lush-slot-name')?.textContent).toBe('Boba');
    expect(cells[0].querySelector('.lush-slot-qty')).toBeNull();
    expect(cells[1].querySelector('.lush-slot-name')?.textContent).toBe('Squirrel tail');
    expect(cells[1].querySelector('.lush-slot-qty')?.textContent).toBe('17');
    expect(cells[2].classList.contains('empty')).toBe(true);
    expect(cells[2].textContent).toBe('');

    slots[1] = { itemId: 0, quantity: 0 };
    items.setInventory({ slots, equipment: { weapon: 0, head: 0, body: 0, shield: 0 } });
    const after = hud.panel.querySelectorAll('.lush-slot');
    expect(after[1].classList.contains('empty')).toBe(true);
    expect(after[1].textContent).toBe('');
  });

  it('names items the catalog has not sent yet by id, then by name once it arrives', () => {
    const items = new ItemState();
    hud = new Hud({ parent: document.body, items, viewport: () => ({ width: 390, height: 844 }) });
    const slots = emptySlots();
    slots[0] = { itemId: 4, quantity: 2 };
    items.setInventory({ slots, equipment: { weapon: 0, head: 0, body: 0, shield: 0 } });
    expect(hud.panel.querySelector('.lush-slot-name')?.textContent).toBe('Item 4');
    items.setCatalog(CATALOG);
    expect(hud.panel.querySelector('.lush-slot-name')?.textContent).toBe('Squirrel tail');
  });

  it('shows weapon, head, body and shield with worn item names or Empty', () => {
    const { items, hud } = setup();
    items.setInventory({ slots: emptySlots(), equipment: { weapon: 1, head: 0, body: 2, shield: 0 } });
    const text = (slot: string) => hud.panel.querySelector(`.lush-equip-slot[data-slot="${slot}"]`)?.textContent;
    expect(text('weapon')).toBe('WeaponWooden stick');
    expect(text('head')).toBe('HeadEmpty');
    expect(text('body')).toBe('BodyHoodie');
    expect(text('shield')).toBe('ShieldEmpty');
    items.setInventory({ slots: emptySlots(), equipment: { weapon: 0, head: 0, body: 2, shield: 0 } });
    expect(text('weapon')).toBe('WeaponEmpty');
  });

  it('places HUD, toggle and panel at the layout rectangles', () => {
    const { hud } = setup();
    const l = hudLayout(390, 844);
    for (const [e, r] of [[hud.hud, l.hud], [hud.toggle, l.toggle], [hud.panel, l.panel]] as const) {
      expect(e.style.position).toBe('fixed');
      expect(e.style.left).toBe(`${r.x}px`);
      expect(e.style.top).toBe(`${r.y}px`);
      expect(e.style.width).toBe(`${r.w}px`);
      expect(e.style.height).toBe(`${r.h}px`);
    }
  });
});

describe('Hud item actions (AC1)', () => {
  it('tapping equipment sends Equip, food sends Eat, loot sends nothing', () => {
    const send = vi.fn<(bytes: Uint8Array) => void>();
    const { items, hud } = setup(send);
    const slots = emptySlots();
    slots[0] = { itemId: 1, quantity: 1 };
    slots[3] = { itemId: 2, quantity: 1 };
    slots[5] = { itemId: 3, quantity: 4 };
    slots[6] = { itemId: 4, quantity: 2 };
    items.setInventory({ slots, equipment: { weapon: 0, head: 0, body: 0, shield: 0 } });
    hud.toggle.click();
    const cell = (i: number) => hud.panel.querySelector(`.lush-slot[data-index="${i}"]`) as HTMLElement;

    cell(0).click();
    expect(decodeAction(send.mock.calls[0][0])).toMatchObject({ action: ItemActionKind.Equip, slot: 0 });
    cell(3).click();
    expect(decodeAction(send.mock.calls[1][0])).toMatchObject({ action: ItemActionKind.Equip, slot: 3 });
    cell(5).click();
    expect(decodeAction(send.mock.calls[2][0])).toMatchObject({ action: ItemActionKind.Eat, slot: 5 });
    cell(6).click();
    cell(7).click();
    expect(send).toHaveBeenCalledTimes(3);
  });

  it('tapping a worn item sends Unequip for its equip slot; empty equip slots send nothing', () => {
    const send = vi.fn<(bytes: Uint8Array) => void>();
    const { items, hud } = setup(send);
    items.setInventory({ slots: emptySlots(), equipment: { weapon: 1, head: 0, body: 2, shield: 0 } });
    const slot = (k: string) => hud.panel.querySelector(`.lush-equip-slot[data-slot="${k}"]`) as HTMLElement;
    slot('weapon').click();
    expect(decodeAction(send.mock.calls[0][0])).toMatchObject({ action: ItemActionKind.Unequip, equipSlot: EquipSlot.Weapon });
    slot('body').click();
    expect(decodeAction(send.mock.calls[1][0])).toMatchObject({ action: ItemActionKind.Unequip, equipSlot: EquipSlot.Body });
    slot('head').click();
    slot('shield').click();
    expect(send).toHaveBeenCalledTimes(2);
  });
});

describe('Hud with controls', () => {
  it('joystick and action buttons still receive input with the panel open', () => {
    const tray = document.createElement('div');
    tray.id = 'control-tray';
    document.body.append(tray);
    const onInteract = vi.fn();
    const controls = new Controls({ tray, getCameraYaw: () => 0, onInteract });
    const { hud } = setup();
    hud.toggle.click();
    expect(hud.isOpen).toBe(true);
    expect(hud.panel.contains(tray)).toBe(false);
    expect(tray.contains(hud.panel)).toBe(false);

    const stick = tray.querySelector('.lush-stick') as HTMLElement;
    stick.setPointerCapture = () => {};
    const ev = (type: string, x: number, y: number) =>
      new PointerEvent(type, { pointerId: 7, clientX: x, clientY: y, bubbles: true });
    stick.dispatchEvent(ev('pointerdown', 100, 100));
    stick.dispatchEvent(ev('pointermove', 100, 40));
    expect(controls.worldMove().y).toBeLessThan(-0.5);
    stick.dispatchEvent(ev('pointerup', 100, 40));
    expect(controls.worldMove()).toEqual({ x: 0, y: 0 });

    const interact = [...tray.querySelectorAll('button')].find((b) => b.textContent === 'Interact')!;
    interact.click();
    expect(onInteract).toHaveBeenCalledOnce();
    const run = [...tray.querySelectorAll('button')].find((b) => b.textContent === 'Run')!;
    run.click();
    expect(controls.isRunning).toBe(true);
    expect(hud.isOpen).toBe(true);
  });
});
