// @vitest-environment happy-dom
import * as flatbuffers from 'flatbuffers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { interact } from '../core/interact';
import { Session } from '../net/session';
import type { Transport } from '../net/transport';
import {
  BankAmount, BankDeposit, BankResultCode, BankWithdraw, Envelope, EquipSlot, ItemKind, Message, Vec2, Welcome,
} from '../protocol';
import { BANK_BOOTH, BankStore } from '../state/bank';
import { ItemState, type ItemInfo } from '../state/items';
import { BankPanel, parseAmount } from './bankPanel';
import { SHOP_STALL } from './shopPanel';

function item(id: number, name: string): ItemInfo {
  return {
    id, name, kind: ItemKind.Loot, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0, attackSpeed: 0,
    heal: 0, shopBuysFor: 1, shopSells: false, shopSellsFor: 0,
  };
}

class FakeTransport implements Transport {
  onMessage: ((b: Uint8Array) => void) | null = null;
  onOpen: (() => void) | null = null;
  onClose: (() => void) | null = null;
  sent: Uint8Array[] = [];
  send(b: Uint8Array) { this.sent.push(b); }
  close() {}
}

const env = (b: Uint8Array): Envelope => Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(b));
let panel: BankPanel | null = null;

function setup(pos = { ...BANK_BOOTH }) {
  const t = new FakeTransport();
  const session = new Session({ factory: () => t, store: { get: () => '', set: () => {} }, onMessage: () => {} });
  session.start();
  const b = new flatbuffers.Builder(64);
  const tok = b.createString('tok');
  Welcome.startWelcome(b);
  Welcome.addGuestToken(b, tok);
  Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
  b.finish(Envelope.createEnvelope(b, Message.Welcome, Welcome.endWelcome(b)));
  t.onMessage?.(b.asUint8Array().slice());
  t.sent = [];

  const bank = new BankStore();
  const items = new ItemState();
  items.setCatalog([item(1, 'Acorn'), item(2, 'Bronze dagger'), item(3, 'Coyote pelt')]);
  items.setInventory({ slots: [{ itemId: 3, quantity: 1 }, { itemId: 3, quantity: 1 }, { itemId: 0, quantity: 0 }],
    equipment: { weapon: 0, head: 0, body: 0, shield: 0 } });
  const onOpen = vi.fn();
  panel = new BankPanel({ parent: document.body, bank, items, session, position: () => pos, viewport: () => ({ width: 390, height: 844 }), onOpen });
  return { t, bank, items, panel, onOpen };
}

/** Opens the bank with the server's BankState. */
function openWith(s: ReturnType<typeof setup>) {
  s.bank.setState([{ itemId: 1, quantity: 30 }, { itemId: 2, quantity: 1 }, { itemId: 3, quantity: 7 }]);
}

const row = (p: BankPanel, attr: string, id: number) => p.panel.querySelector(`[data-${attr}="${id}"]`) as HTMLElement;
const click = (r: HTMLElement, cls: string) => (r.querySelector(`.${cls}`) as HTMLButtonElement).click();

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  panel?.dispose();
  panel = null;
});

describe('bank panel (#467 AC2)', () => {
  it('opening sends BankOpen, and the panel lists the slots by catalog name once BankState arrives', () => {
    const s = setup();
    expect(s.panel.isOpen).toBe(false);
    expect(s.panel.open()).toBe(true);
    expect(s.t.sent.map((b) => env(b).msgType())).toEqual([Message.BankOpen]);
    expect(s.panel.isOpen).toBe(false);
    openWith(s);
    expect(s.panel.isOpen).toBe(true);
    expect(s.onOpen).toHaveBeenCalledTimes(1);
    const names = [...s.panel.panel.querySelectorAll('[data-item] .lush-bank-name')].map((e) => e.textContent);
    expect(names).toEqual(['Acorn', 'Bronze dagger', 'Coyote pelt']);
  });

  it('does not open away from the booth', () => {
    const s = setup({ x: BANK_BOOTH.x + 10, z: BANK_BOOTH.z });
    expect(s.panel.open()).toBe(false);
    expect(s.t.sent).toEqual([]);
  });

  it('the search box filters by name', () => {
    const s = setup();
    openWith(s);
    s.panel.search.value = 'DAG';
    s.panel.search.dispatchEvent(new Event('input'));
    expect([...s.panel.panel.querySelectorAll('[data-item]')].map((e) => (e as HTMLElement).dataset.item)).toEqual(['2']);
    s.panel.search.value = '';
    s.panel.search.dispatchEvent(new Event('input'));
    expect(s.panel.panel.querySelectorAll('[data-item]').length).toBe(3);
  });

  it('Interact at the booth sends BankOpen', () => {
    const s = setup();
    const game = { prediction: { pos: BANK_BOOTH }, remotes: () => [], items: { entityInfo: () => undefined, groundItems: [] } } as never;
    const target = interact(game, 0, SHOP_STALL, {
      openShop: vi.fn(), openBank: () => s.panel.open(), openCardTable: vi.fn(), sendPickUp: vi.fn(), sendAttackTarget: vi.fn(), sendTradeRequest: vi.fn(),
    });
    expect(target).toEqual({ type: 'bank' });
    expect(s.t.sent.map((b) => env(b).msgType())).toEqual([Message.BankOpen]);
  });
});

describe('bank panel buttons (#467 AC3)', () => {
  const sentAmounts = (s: ReturnType<typeof setup>) =>
    s.t.sent.map((b) => {
      const e = env(b);
      if (e.msgType() === Message.BankWithdraw) {
        const m = e.msg(new BankWithdraw()) as BankWithdraw;
        return ['withdraw', m.itemId(), m.amount()];
      }
      const m = e.msg(new BankDeposit()) as BankDeposit;
      return ['deposit', m.itemId(), m.amount()];
    });

  it('withdraw buttons send one, five, all and the typed amount', () => {
    const s = setup();
    openWith(s);
    const r = row(s.panel, 'item', 1);
    click(r, 'lush-bank-1');
    click(r, 'lush-bank-5');
    click(r, 'lush-bank-all');
    s.panel.amount.value = '12';
    click(r, 'lush-bank-x');
    expect(sentAmounts(s)).toEqual([['withdraw', 1, 1], ['withdraw', 1, 5], ['withdraw', 1, BankAmount.All], ['withdraw', 1, 12]]);
  });

  it('deposit buttons send the inventory item (summed over slots) with each amount', () => {
    const s = setup();
    openWith(s);
    const r = row(s.panel, 'inventory-item', 3);
    expect(r.querySelector('.lush-bank-qty')?.textContent).toBe('×2');
    click(r, 'lush-bank-1');
    click(r, 'lush-bank-5');
    click(r, 'lush-bank-all');
    s.panel.amount.value = '2';
    click(r, 'lush-bank-x');
    expect(sentAmounts(s)).toEqual([['deposit', 3, 1], ['deposit', 3, 5], ['deposit', 3, BankAmount.All], ['deposit', 3, 2]]);
  });

  it('a typed amount that is not a whole number from 1 sends nothing', () => {
    const s = setup();
    openWith(s);
    for (const v of ['', '0', '-3', '1.5', 'abc']) {
      s.panel.amount.value = v;
      click(row(s.panel, 'item', 1), 'lush-bank-x');
    }
    expect(s.t.sent).toEqual([]);
    expect(parseAmount(' 7 ')).toBe(7);
    expect(parseAmount('99999999999')).toBe(BankAmount.All - 1);
  });

  it('Deposit inventory and Deposit equipment send their messages', () => {
    const s = setup();
    openWith(s);
    (s.panel.panel.querySelector('.lush-bank-deposit-inventory') as HTMLButtonElement).click();
    (s.panel.panel.querySelector('.lush-bank-deposit-equipment') as HTMLButtonElement).click();
    expect(s.t.sent.map((b) => env(b).msgType())).toEqual([Message.BankDepositInventory, Message.BankDepositEquipment]);
  });

  it('Close sends BankClose and hides the panel', () => {
    const s = setup();
    openWith(s);
    (s.panel.panel.querySelector('.lush-bank-close') as HTMLButtonElement).click();
    expect(s.t.sent.map((b) => env(b).msgType())).toEqual([Message.BankClose]);
    expect(s.panel.isOpen).toBe(false);
    expect(s.bank.isOpen).toBe(false);
  });

  it('closes when the bank state becomes closed, without sending BankClose', () => {
    const s = setup();
    openWith(s);
    expect(s.panel.isOpen).toBe(true);
    s.bank.setResult(BankResultCode.InCombat);
    expect(s.panel.isOpen).toBe(false);
    expect(s.t.sent).toEqual([]);
  });

  it('closes when the player walks away from the booth', () => {
    const pos = { ...BANK_BOOTH };
    const s = setup(pos);
    openWith(s);
    pos.x += 10;
    s.panel.update();
    expect(s.panel.isOpen).toBe(false);
    expect(s.t.sent.map((b) => env(b).msgType())).toEqual([Message.BankClose]);
  });
});
