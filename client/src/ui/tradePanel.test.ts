// @vitest-environment happy-dom
import * as flatbuffers from 'flatbuffers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  Envelope, EquipSlot, ItemKind, Message, TradeAccept, TradeOffer, TradeRemove, TradeRequest, TradeResultCode, TradeSetGold,
} from '../protocol';
import { ItemState, type ItemInfo } from '../state/items';
import { TradeStore, tradeResultText, type TradeView } from '../state/trade';
import { hudLayout } from './hudLayout';
import { TradePanel } from './tradePanel';

function item(id: number, name: string): ItemInfo {
  return {
    id, name, kind: ItemKind.Loot, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0,
    attackSpeed: 0, heal: 0, shopBuysFor: 1, shopSells: false, shopSellsFor: 0,
  };
}

const VIEW: TradeView = {
  partnerId: 7,
  screen: 1,
  myItems: [{ itemId: 1, quantity: 1 }, { itemId: 2, quantity: 5 }],
  myGold: 100,
  partnerItems: [{ itemId: 3, quantity: 1 }],
  partnerGold: 25,
  myAccepted: false,
  partnerAccepted: true,
};

let panel: TradePanel | null = null;

function setup() {
  const items = new ItemState();
  items.setCatalog([item(1, 'Acorn'), item(2, 'Boba'), item(3, 'Shiny trinket')]);
  items.setInventory({
    slots: [{ itemId: 0, quantity: 0 }, { itemId: 2, quantity: 4 }, { itemId: 0, quantity: 0 }, { itemId: 1, quantity: 1 }],
    equipment: { weapon: 0, head: 0, body: 0, shield: 0 },
  });
  const trade = new TradeStore();
  const send = vi.fn<(bytes: Uint8Array) => void>();
  const onOpen = vi.fn();
  panel = new TradePanel({ parent: document.body, trade, items, send, onOpen, viewport: () => ({ width: 390, height: 844 }) });
  return { items, trade, send, onOpen, panel };
}

function envelope(bytes: Uint8Array): Envelope {
  return Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
}

function click(root: HTMLElement, selector: string): void {
  const b = root.querySelector<HTMLElement>(selector);
  if (!b) throw new Error(`no ${selector}`);
  b.click();
}

function texts(root: HTMLElement, selector: string): string[] {
  return [...root.querySelectorAll(selector)].map((e) => e.textContent ?? '');
}

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  panel?.dispose();
  panel = null;
  vi.useRealTimers();
});

describe('trade request prompt (AC1)', () => {
  it('appears for a pending request; Accept sends TradeRequest naming the requester', () => {
    const { trade, send, panel } = setup();
    expect(panel.promptOpen).toBe(false);
    trade.setRequest(7);
    expect(panel.promptOpen).toBe(true);
    expect(panel.prompt.textContent).toContain('Player 7');

    click(panel.prompt, '.lush-trade-prompt-accept');
    expect(send).toHaveBeenCalledTimes(1);
    const env = envelope(send.mock.calls[0][0]);
    expect(env.msgType()).toBe(Message.TradeRequest);
    expect((env.msg(new TradeRequest()) as TradeRequest).targetId()).toBe(7);
    expect(panel.promptOpen).toBe(false);
  });

  it('Decline dismisses the prompt without sending anything; a new request shows it again', () => {
    const { trade, send, panel } = setup();
    trade.setRequest(9);
    click(panel.prompt, '.lush-trade-prompt-decline');
    expect(panel.promptOpen).toBe(false);
    expect(send).not.toHaveBeenCalled();

    trade.setRequest(9);
    expect(panel.promptOpen).toBe(true);
  });

  it('is placed at the layout tradePrompt rectangle', () => {
    const { panel } = setup();
    const r = hudLayout(390, 844).tradePrompt;
    expect(panel.prompt.style.position).toBe('fixed');
    expect(panel.prompt.style.top).toBe(`${r.y}px`);
    expect(panel.prompt.style.height).toBe(`${r.h}px`);
  });
});

describe('trade window screen 1 (AC2)', () => {
  it('opens on TradeState, hides the prompt, and calls onOpen once', () => {
    const { trade, panel, onOpen } = setup();
    trade.setRequest(7);
    trade.setState(VIEW);
    expect(panel.isOpen).toBe(true);
    expect(panel.promptOpen).toBe(false);
    trade.setState({ ...VIEW, myGold: 5 });
    expect(onOpen).toHaveBeenCalledTimes(1);
    const r = hudLayout(390, 844).trade;
    expect(panel.panel.style.left).toBe(`${r.x}px`);
    expect(panel.panel.style.width).toBe(`${r.w}px`);
    expect(panel.panel.style.height).toBe(`${r.h}px`);
  });

  it('renders both offers, both gold amounts and both accept flags', () => {
    const { trade, panel } = setup();
    trade.setState(VIEW);
    const p = panel.panel;
    expect(texts(p, '.lush-trade-mine .lush-trade-row')).toEqual(['Acorn', 'Boba ×5']);
    expect(texts(p, '.lush-trade-theirs .lush-trade-row')).toEqual(['Shiny trinket']);
    expect(p.querySelector('.lush-trade-mine .lush-trade-gold')?.textContent).toBe('100 gp');
    expect(p.querySelector('.lush-trade-theirs .lush-trade-gold')?.textContent).toBe('25 gp');
    expect(p.querySelector('.lush-trade-mine .lush-trade-status')?.textContent).toBe('');
    expect(p.querySelector('.lush-trade-theirs .lush-trade-status')?.textContent).toBe('Accepted');

    trade.setState({ ...VIEW, myAccepted: true, partnerAccepted: false, myGold: 7, partnerGold: 0 });
    expect(p.querySelector('.lush-trade-mine .lush-trade-status')?.textContent).toBe('Accepted');
    expect(p.querySelector('.lush-trade-theirs .lush-trade-status')?.textContent).toBe('');
    expect(p.querySelector('.lush-trade-mine .lush-trade-gold')?.textContent).toBe('7 gp');
    expect(p.querySelector('.lush-trade-theirs .lush-trade-gold')?.textContent).toBe('0 gp');
  });

  it('tapping an inventory item sends TradeOffer for that slot', () => {
    const { trade, send, panel } = setup();
    trade.setState(VIEW);
    const rows = [...panel.panel.querySelectorAll<HTMLElement>('.lush-trade-inv .lush-trade-row')];
    expect(rows.map((r) => r.dataset.slot)).toEqual(['1', '3']);
    rows[0].click();
    const env = envelope(send.mock.calls[0][0]);
    expect(env.msgType()).toBe(Message.TradeOffer);
    const m = env.msg(new TradeOffer()) as TradeOffer;
    expect(m.slot()).toBe(1);
    expect(m.quantity()).toBe(4);
  });

  it('tapping your own offered item sends TradeRemove with its index; partner items do nothing', () => {
    const { trade, send, panel } = setup();
    trade.setState(VIEW);
    click(panel.panel, '.lush-trade-mine .lush-trade-row[data-index="1"]');
    const env = envelope(send.mock.calls[0][0]);
    expect(env.msgType()).toBe(Message.TradeRemove);
    expect((env.msg(new TradeRemove()) as TradeRemove).index()).toBe(1);
    click(panel.panel, '.lush-trade-theirs .lush-trade-row');
    expect(send).toHaveBeenCalledTimes(1);
  });

  it('entering gold sends TradeSetGold', () => {
    const { trade, send, panel } = setup();
    trade.setState(VIEW);
    const input = panel.panel.querySelector<HTMLInputElement>('.lush-trade-gold-input')!;
    expect(input.value).toBe('100');
    input.value = '250';
    click(panel.panel, '.lush-trade-set-gold');
    expect(send).toHaveBeenCalledTimes(1);
    const env = envelope(send.mock.calls[0][0]);
    expect(env.msgType()).toBe(Message.TradeSetGold);
    expect((env.msg(new TradeSetGold()) as TradeSetGold).gold()).toBe(250);

    input.value = '-3';
    click(panel.panel, '.lush-trade-set-gold');
    expect(send).toHaveBeenCalledTimes(1);
  });
});

describe('trade window screen 2 and buttons (AC3)', () => {
  it('screen 2 renders both final offers read-only', () => {
    const { trade, panel } = setup();
    trade.setState({ ...VIEW, screen: 2 });
    const p = panel.panel;
    expect(p.dataset.screen).toBe('2');
    expect(texts(p, '.lush-trade-mine .lush-trade-row')).toEqual(['Acorn', 'Boba ×5']);
    expect(texts(p, '.lush-trade-theirs .lush-trade-row')).toEqual(['Shiny trinket']);
    expect(p.querySelector('.lush-trade-mine .lush-trade-gold')?.textContent).toBe('100 gp');
    expect(p.querySelector('.lush-trade-theirs .lush-trade-gold')?.textContent).toBe('25 gp');
    expect(p.querySelectorAll('.lush-trade-mine button, .lush-trade-theirs button')).toHaveLength(0);
    expect(p.querySelector<HTMLElement>('.lush-trade-offer')?.hidden).toBe(true);
    expect(p.querySelectorAll('.lush-trade-inv .lush-trade-row')).toHaveLength(0);
  });

  for (const screen of [1, 2]) {
    it(`Accept sends TradeAccept with screen ${screen}; Cancel sends TradeCancel`, () => {
      const { trade, send, panel } = setup();
      trade.setState({ ...VIEW, screen });
      click(panel.panel, '.lush-trade-btns .lush-trade-accept');
      let env = envelope(send.mock.calls[0][0]);
      expect(env.msgType()).toBe(Message.TradeAccept);
      expect((env.msg(new TradeAccept()) as TradeAccept).screen()).toBe(screen);

      click(panel.panel, '.lush-trade-btns .lush-trade-cancel');
      env = envelope(send.mock.calls[1][0]);
      expect(env.msgType()).toBe(Message.TradeCancel);
    });
  }
});

describe('trade result (AC4)', () => {
  const codes = Object.values(TradeResultCode).filter((v): v is TradeResultCode => typeof v === 'number');

  it('every TradeResultCode has its own short message', () => {
    expect(codes).toHaveLength(7);
    const msgs = codes.map(tradeResultText);
    expect(new Set(msgs).size).toBe(codes.length);
    for (const m of msgs) expect(m.length).toBeLessThanOrEqual(40);
  });

  for (const code of Object.values(TradeResultCode).filter((v): v is TradeResultCode => typeof v === 'number')) {
    it(`TradeResult ${TradeResultCode[code]} closes the window and shows its message`, () => {
      vi.useFakeTimers();
      const { trade, panel } = setup();
      trade.setState(VIEW);
      expect(panel.isOpen).toBe(true);
      trade.setResult(code);
      expect(panel.isOpen).toBe(false);
      expect(panel.message.hidden).toBe(false);
      expect(panel.message.textContent).toBe(tradeResultText(code));
      vi.advanceTimersByTime(3000);
      expect(panel.message.hidden).toBe(true);
    });
  }

  it('a result from before the panel existed is not shown', () => {
    const items = new ItemState();
    const trade = new TradeStore();
    trade.setResult(TradeResultCode.Completed);
    panel = new TradePanel({ parent: document.body, trade, items, send: () => {} });
    expect(panel.message.hidden).toBe(true);
  });
});
