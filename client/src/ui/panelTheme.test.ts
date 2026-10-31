// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { EquipSlot, ItemKind, ShedColour, ShedSymbol } from '../protocol';
import { CardTableStore } from '../state/cardTables';
import { ItemState, type ItemInfo } from '../state/items';
import { TradeStore } from '../state/trade';
import { CardTablePanel } from './cardTablePanel';
import { ChatLog } from './chatLog';
import { Hud } from './hud';
import { SHOP_STALL, ShopPanel } from './shopPanel';
import { applyTheme, MIN_TAP_PX, PANEL_ANIM_MS, SHARED_CSS, THEME } from './theme';
import { TradePanel } from './tradePanel';

const VP = { width: 390, height: 844 };
const ME = 42;

function item(id: number, name: string, extra: Partial<ItemInfo> = {}): ItemInfo {
  return {
    id, name, kind: ItemKind.Food, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0,
    attackSpeed: 0, heal: 1, shopBuysFor: 2, shopSells: true, shopSellsFor: 5, ...extra,
  };
}

/** One opener / closer per panel, with content in it so every kind of button is rendered. */
interface Case {
  name: string;
  el: () => HTMLElement;
  open: () => void;
  close: () => void;
  isOpen: () => boolean;
}

let disposers: (() => void)[] = [];

function panels(): Case[] {
  const items = new ItemState();
  items.setCatalog([item(1, 'Acorn'), item(2, 'Boba')]);
  items.setInventory({
    slots: [{ itemId: 1, quantity: 3 }, { itemId: 2, quantity: 1 }, { itemId: 0, quantity: 0 }],
    equipment: { weapon: 0, head: 0, body: 0, shield: 0 },
  });
  const send = (): void => undefined;

  const hud = new Hud({ parent: document.body, items, send, viewport: () => VP });
  const shop = new ShopPanel({ parent: document.body, items, send, position: () => ({ ...SHOP_STALL }), viewport: () => VP });
  const store = new TradeStore();
  const trade = new TradePanel({ parent: document.body, trade: store, items, send, viewport: () => VP });
  const tables = new CardTableStore();
  tables.setCatalog([{ id: 1, seats: 2, stake: 50 }]);
  const session = {
    sendCardTableSit: vi.fn(), sendCardTableReady: vi.fn(), sendCardTableLeave: vi.fn(), sendShedPlay: vi.fn(), sendShedDraw: vi.fn(),
  };
  const ctable = new CardTablePanel({ parent: document.body, tables, session, viewport: () => VP });
  let covered = true;
  const chat = new ChatLog({ parent: document.body, lines: () => [], playerId: () => ME, covered: () => covered, viewport: () => VP });
  disposers.push(() => hud.dispose(), () => shop.dispose(), () => trade.dispose(), () => ctable.dispose(), () => chat.dispose());

  return [
    { name: 'inventory', el: () => hud.panel, open: () => hud.setOpen(true), close: () => hud.setOpen(false), isOpen: () => hud.isOpen },
    { name: 'shop', el: () => shop.panel, open: () => void shop.tryOpen(), close: () => shop.close(), isOpen: () => shop.isOpen },
    {
      name: 'trade',
      el: () => trade.panel,
      open: () => store.setState({
        partnerId: 7, screen: 1, myItems: [{ itemId: 1, quantity: 1 }], myGold: 10, partnerItems: [], partnerGold: 0,
        myAccepted: false, partnerAccepted: false,
      }),
      close: () => store.setResult(0),
      isOpen: () => trade.isOpen,
    },
    {
      name: 'trade request',
      el: () => trade.prompt,
      open: () => store.setRequest(7),
      close: () => trade.prompt.querySelector<HTMLButtonElement>('.lush-trade-prompt-decline')!.click(),
      isOpen: () => trade.promptOpen,
    },
    {
      name: 'card table',
      el: () => ctable.panel,
      open: () => {
        ctable.open(1);
        tables.setState({ tableId: 1, seats: [ME, 9], ready: [true, true], inProgress: true, turnSeat: 0, reversed: false,
          turnTicksLeft: 0, winnerSeat: null }, ME);
        tables.setShedState({ tableId: 1, topCard: { colour: ShedColour.Two, symbol: ShedSymbol.Three }, colour: ShedColour.Two,
          handCounts: [2, 2], drawPile: 50 });
        tables.setHand({ tableId: 1, cards: [{ colour: ShedColour.Two, symbol: ShedSymbol.Five }, { colour: ShedColour.None, symbol: ShedSymbol.Wild }] });
        ctable.play({ colour: ShedColour.None, symbol: ShedSymbol.Wild });
      },
      close: () => ctable.close(),
      isOpen: () => ctable.isOpen,
    },
    {
      name: 'chat log',
      el: () => chat.el,
      open: () => {
        covered = false;
        chat.update();
      },
      close: () => {
        covered = true;
        chat.update();
      },
      isOpen: () => !covered && chat.el.style.display !== 'none',
    },
  ];
}

/** Drawn at all: not display:none, either from the hidden attribute or inline. */
function drawn(el: HTMLElement): boolean {
  return getComputedStyle(el).display !== 'none';
}

beforeEach(() => {
  // The panels add their stylesheets once per module, so the head is kept between tests.
  document.body.replaceChildren();
  applyTheme();
});
afterEach(() => {
  for (const d of disposers) d();
  disposers = [];
  vi.useRealTimers();
});

describe('panel theme (#387 AC1)', () => {
  it('the shared panel class is a dark translucent background, gold border and rounded corners', () => {
    const rule = SHARED_CSS.match(/\.lush-panel \{([^}]*)\}/)?.[1] ?? '';
    expect(rule).toContain('background: var(--lush-panel-bg)');
    expect(rule).toContain('border: var(--lush-border)');
    expect(rule).toContain('border-radius: var(--lush-radius)');
    expect(THEME['--lush-panel-bg']).toMatch(/^rgba\(\s*\d+,\s*\d+,\s*\d+,\s*0?\.\d+\)$/);
    expect(THEME['--lush-border']).toContain('var(--lush-gold-dim)');
    expect(parseFloat(THEME['--lush-radius'])).toBeGreaterThan(0);
    const title = SHARED_CSS.match(/\.lush-title \{([^}]*)\}/)?.[1] ?? '';
    expect(title).toContain('font-variant: small-caps');
    expect(title).toContain('var(--lush-font-title)');
    expect(THEME['--lush-font-title']).toMatch(/serif$/);
  });

  it('each panel uses the panel class and has a serif small-caps title', () => {
    for (const p of panels()) {
      if (p.name === 'trade request') continue;
      p.open();
      const el = p.el();
      expect(el.classList.contains('lush-panel'), p.name).toBe(true);
      const title = el.querySelector('.lush-title');
      expect(title, p.name).not.toBeNull();
      expect(title?.textContent, p.name).not.toBe('');
      const cs = getComputedStyle(title as HTMLElement);
      expect(cs.fontVariant, p.name).toBe('small-caps');
    }
  });

  it('the trade request prompt uses the panel class too', () => {
    const trade = panels().find((p) => p.name === 'trade request')!;
    expect(trade.el().classList.contains('lush-panel')).toBe(true);
  });
});

describe('panel open and close animations (#387 AC2)', () => {
  it('opening and closing applies a transition of 150 to 200 ms', () => {
    expect(PANEL_ANIM_MS).toBeGreaterThanOrEqual(150);
    expect(PANEL_ANIM_MS).toBeLessThanOrEqual(200);
    for (const p of panels()) {
      const el = p.el();
      const durations = [...el.style.transition.matchAll(/(\d+)ms/g)].map((m) => Number(m[1]));
      expect(durations.length, p.name).toBeGreaterThan(0);
      for (const d of durations) {
        expect(d, p.name).toBeGreaterThanOrEqual(150);
        expect(d, p.name).toBeLessThanOrEqual(200);
      }
      expect(el.style.transition, p.name).toMatch(/opacity/);
      p.open();
      expect(el.classList.contains('lush-shut'), p.name).toBe(false);
      p.close();
      // The shut look (opacity 0, shifted) is what the transition animates to.
      expect(el.classList.contains('lush-shut'), p.name).toBe(true);
      expect(el.style.transition, p.name).toMatch(/opacity/);
    }
  });

  it('a closing panel stays drawn for the animation, then is out of the tab order and ignores pointer events', () => {
    vi.useFakeTimers();
    for (const p of panels()) {
      const el = p.el();
      p.open();
      expect(p.isOpen(), p.name).toBe(true);
      expect(drawn(el), p.name).toBe(true);
      expect(el.hasAttribute('inert'), p.name).toBe(false);
      // The chat log never catches pointers, so camera drags over it reach the canvas.
      if (p.name !== 'chat log') expect(getComputedStyle(el).pointerEvents, p.name).not.toBe('none');

      p.close();
      expect(p.isOpen(), p.name).toBe(false);
      expect(drawn(el), `${p.name} still drawn while closing`).toBe(true);
      vi.advanceTimersByTime(PANEL_ANIM_MS - 10);
      expect(drawn(el), `${p.name} still drawn while closing`).toBe(true);
      vi.advanceTimersByTime(10);

      expect(drawn(el), p.name).toBe(false);
      expect(el.hasAttribute('inert'), p.name).toBe(true);
      expect(getComputedStyle(el).pointerEvents, p.name).toBe('none');
      // Reopening brings it straight back.
      p.open();
      expect(drawn(el), p.name).toBe(true);
      expect(el.hasAttribute('inert'), p.name).toBe(false);
      p.close();
      vi.advanceTimersByTime(PANEL_ANIM_MS);
    }
  });
});

describe('panel buttons (#387 AC3)', () => {
  it('every button gets a pressed state on pointer down and a tap target of at least 44 × 44 px', () => {
    let checked = 0;
    for (const p of panels()) {
      p.open();
      const buttons = [...p.el().querySelectorAll<HTMLButtonElement>('button')].filter((b) => !b.disabled);
      if (p.name === 'inventory' || p.name === 'chat log') expect(buttons, p.name).toHaveLength(0);
      else expect(buttons.length, p.name).toBeGreaterThan(0);
      for (const b of buttons) {
        const label = `${p.name}: ${b.className} ${b.textContent}`;
        b.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
        expect(b.classList.contains('pressed'), label).toBe(true);
        b.dispatchEvent(new PointerEvent('pointerup', { bubbles: true }));
        expect(b.classList.contains('pressed'), label).toBe(false);
        const cs = getComputedStyle(b);
        expect(parseFloat(cs.minWidth), label).toBeGreaterThanOrEqual(MIN_TAP_PX);
        expect(parseFloat(cs.minHeight), label).toBeGreaterThanOrEqual(MIN_TAP_PX);
        checked++;
      }
    }
    expect(checked).toBeGreaterThan(10);
    expect(MIN_TAP_PX).toBeGreaterThanOrEqual(44);
  });

  it('inventory slots and the bag toggle get the pressed state too', () => {
    panels()[0].open();
    const cell = document.querySelector<HTMLElement>('.lush-slot.usable')!;
    cell.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
    expect(cell.classList.contains('pressed')).toBe(true);
    cell.dispatchEvent(new PointerEvent('pointercancel', { bubbles: true }));
    expect(cell.classList.contains('pressed')).toBe(false);
    const toggle = document.querySelector<HTMLElement>('.lush-inv-toggle')!;
    toggle.dispatchEvent(new PointerEvent('pointerdown'));
    expect(toggle.classList.contains('pressed')).toBe(true);
  });
});
