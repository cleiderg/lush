// @vitest-environment happy-dom
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import * as flatbuffers from 'flatbuffers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Envelope, EquipSlot, ItemKind, Message, ShopBuy, ShopResultCode, ShopSell } from '../protocol';
import { ItemState, type ItemInfo } from '../state/items';
import { nearShop, SHOP_DISTANCE_TILES, shopResultText, TILE_METRES } from '../state/shop';
import { Hud } from './hud';
import { hudLayout } from './hudLayout';
import { SHOP_STALL, ShopPanel } from './shopPanel';

const repo = resolve(__dirname, '../../..');

function item(id: number, name: string, kind: ItemKind, shopBuysFor: number, shopSellsFor = 0): ItemInfo {
  return {
    id, name, kind, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0, attackSpeed: 0,
    heal: 0, shopBuysFor, shopSells: shopSellsFor > 0, shopSellsFor, toolSkill: 0, toolLevel: 0, cookingLevel: 0,
  };
}

const CATALOG = [
  item(1, 'Wooden stick', ItemKind.Weapon, 2, 5),
  item(2, 'Bronze dagger', ItemKind.Weapon, 15, 40),
  item(3, 'Boba', ItemKind.Food, 1, 5),
  item(4, 'Acorn', ItemKind.Loot, 1),
  item(5, 'Shiny trinket', ItemKind.Loot, 25),
];

let shop: ShopPanel | null = null;
let hud: Hud | null = null;

function setup(pos = { ...SHOP_STALL }) {
  const items = new ItemState();
  items.setCatalog(CATALOG);
  const send = vi.fn<(bytes: Uint8Array) => void>();
  shop = new ShopPanel({ parent: document.body, items, send, position: () => pos, viewport: () => ({ width: 390, height: 844 }) });
  return { items, send, pos, shop };
}

function envelope(bytes: Uint8Array): Envelope {
  return Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
}

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  shop?.dispose();
  hud?.dispose();
  shop = hud = null;
});

describe('shop distance (AC2)', () => {
  it('SHOP_DISTANCE_TILES equals shop_distance_tiles in the server rules', () => {
    const rules = JSON.parse(readFileSync(resolve(repo, 'server/internal/systems/gamedata/data/rules.json'), 'utf8'));
    expect(SHOP_DISTANCE_TILES).toBe(rules.shop_distance_tiles);
  });

  it('a tile is CELL_SIZE metres', () => {
    const config = readFileSync(resolve(repo, 'shared/map/config.mjs'), 'utf8');
    expect(TILE_METRES).toBe(Number(/export const CELL_SIZE = ([\d.]+);/.exec(config)?.[1]));
  });

  it('the stall is the areas.json shop point in metres', () => {
    const areas = JSON.parse(readFileSync(resolve(repo, 'shared/map/areas.json'), 'utf8'));
    expect(SHOP_STALL).toEqual({ x: areas.shop.x / 10, z: areas.shop.z / 10 });
  });

  it('nearShop counts tiles including diagonals', () => {
    const s = { x: 10, z: 20 };
    expect(nearShop({ x: 12, z: 22 }, s)).toBe(true);
    expect(nearShop({ x: 8, z: 18 }, s)).toBe(true);
    expect(nearShop({ x: 12.1, z: 20 }, s)).toBe(false);
    expect(nearShop({ x: 10, z: 17.9 }, s)).toBe(false);
  });

  it('Interact opens the panel near the stall and not far away', () => {
    const { pos, shop } = setup({ x: SHOP_STALL.x + 1.5, z: SHOP_STALL.z - 1 });
    expect(shop.isOpen).toBe(false);
    expect(shop.tryOpen()).toBe(true);
    expect(shop.isOpen).toBe(true);
    expect(shop.panel.hidden).toBe(false);

    shop.close();
    pos.x = SHOP_STALL.x + 30;
    expect(shop.tryOpen()).toBe(false);
    expect(shop.isOpen).toBe(false);
  });

  it('closes when the player walks out of range', () => {
    const { pos, shop } = setup();
    shop.tryOpen();
    shop.update();
    expect(shop.isOpen).toBe(true);
    pos.z += 5;
    shop.update();
    expect(shop.isOpen).toBe(false);
  });

  it('opening the shop closes the inventory and vice versa', () => {
    const items = new ItemState();
    const v = () => ({ width: 390, height: 844 });
    const h = hud = new Hud({ parent: document.body, items, viewport: v, onOpen: () => s.close() });
    const s = shop = new ShopPanel({ parent: document.body, items, send: () => {}, position: () => SHOP_STALL, viewport: v,
      onOpen: () => h.setOpen(false) });
    h.setOpen(true);
    s.tryOpen();
    expect(h.isOpen).toBe(false);
    h.toggle.click();
    expect(h.isOpen).toBe(true);
    expect(s.isOpen).toBe(false);
  });

  it('is placed at the layout shop rectangle', () => {
    const { shop } = setup();
    const r = hudLayout(390, 844).shop;
    expect(shop.panel.style.position).toBe('fixed');
    expect(shop.panel.style.left).toBe(`${r.x}px`);
    expect(shop.panel.style.top).toBe(`${r.y}px`);
    expect(shop.panel.style.width).toBe(`${r.w}px`);
    expect(shop.panel.style.height).toBe(`${r.h}px`);
  });
});

describe('buying and selling (AC3)', () => {
  it('lists only items the shop sells, with their shop_sells_for price; Buy sends ShopBuy', () => {
    const { send, shop } = setup();
    shop.tryOpen();
    const rows = [...shop.panel.querySelectorAll<HTMLElement>('.lush-shop-row[data-item]')];
    expect(rows.map((r) => r.querySelector('.lush-shop-name')?.textContent)).toEqual(['Wooden stick', 'Boba', 'Bronze dagger']);
    expect(rows.map((r) => r.querySelector('.lush-shop-price')?.textContent)).toEqual(['5 gp', '5 gp', '40 gp']);

    (rows[2].querySelector('.lush-shop-buy') as HTMLElement).click();
    const env = envelope(send.mock.calls[0][0]);
    expect(env.msgType()).toBe(Message.ShopBuy);
    const m = env.msg(new ShopBuy()) as ShopBuy;
    expect(m.itemId()).toBe(2);
    expect(m.quantity()).toBe(1);
  });

  it('shows each inventory slot with its shop_buys_for price; Sell sends ShopSell within the slot quantity', () => {
    const { items, send, shop } = setup();
    const slots = Array.from({ length: 28 }, () => ({ itemId: 0, quantity: 0 }));
    slots[2] = { itemId: 5, quantity: 1 };
    slots[4] = { itemId: 4, quantity: 7 };
    items.setInventory({ slots, equipment: { weapon: 0, head: 0, body: 0, shield: 0 } });
    shop.tryOpen();
    const rows = [...shop.panel.querySelectorAll<HTMLElement>('.lush-shop-row[data-slot]')];
    expect(rows.map((r) => r.dataset.slot)).toEqual(['2', '4']);
    expect(rows[0].querySelector('.lush-shop-name')?.textContent).toBe('Shiny trinket');
    expect(rows[0].querySelector('.lush-shop-price')?.textContent).toBe('25 gp');
    expect(rows[0].querySelector('.lush-shop-sell-all')).toBeNull();
    expect(rows[1].querySelector('.lush-shop-name')?.textContent).toBe('Acorn ×7');
    expect(rows[1].querySelector('.lush-shop-price')?.textContent).toBe('1 gp');

    const sold = () => send.mock.calls.map(([b]) => {
      const env = envelope(b);
      expect(env.msgType()).toBe(Message.ShopSell);
      const m = env.msg(new ShopSell()) as ShopSell;
      return [m.slot(), m.quantity()];
    });
    (rows[0].querySelector('.lush-shop-sell') as HTMLElement).click();
    (rows[1].querySelector('.lush-shop-sell') as HTMLElement).click();
    (rows[1].querySelector('.lush-shop-sell-all') as HTMLElement).click();
    expect(sold()).toEqual([[2, 1], [4, 1], [4, 7]]);
    for (const [slot, qty] of sold()) expect(qty).toBeLessThanOrEqual(slots[slot].quantity);
  });
});

describe('shop results (AC4)', () => {
  const codes = Object.values(ShopResultCode).filter((v): v is ShopResultCode => typeof v === 'number');

  it('has a distinct short message for every ShopResult code', () => {
    expect(codes).toHaveLength(5);
    const texts = codes.map(shopResultText);
    expect(new Set(texts).size).toBe(codes.length);
    for (const t of texts) expect(t.length).toBeGreaterThan(0);
    expect(shopResultText(ShopResultCode.NotEnoughGold)).toBe('Not enough gold');
    expect(shopResultText(ShopResultCode.InventoryFull)).toBe('Inventory full');
    expect(shopResultText(ShopResultCode.TooFar)).toBe('Too far from the stall');
  });

  it('shows the message for each code in the open panel, but not a result from before it opened', () => {
    const { items, shop } = setup();
    items.setShopResult(ShopResultCode.NotEnoughGold);
    shop.tryOpen();
    expect(shop.message.textContent).toBe('');
    for (const code of codes) {
      items.setShopResult(code);
      expect(shop.message.textContent).toBe(shopResultText(code));
    }
  });
});
