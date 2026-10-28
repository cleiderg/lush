// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ItemState } from '../state/items';
import { GameState } from '../state/world';
import { anyOpen, ChatLog } from './chatLog';
import { Hud } from './hud';
import { hudLayout } from './hudLayout';
import { SHOP_STALL, ShopPanel } from './shopPanel';

const VP = { width: 390, height: 844 };
let disposers: (() => void)[] = [];

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  for (const d of disposers) d();
  disposers = [];
});

function setup(covered?: () => boolean) {
  const game = new GameState();
  game.playerId = 7;
  const log = new ChatLog({
    parent: document.body,
    lines: () => game.chat.log,
    playerId: () => game.playerId,
    covered,
    viewport: () => VP,
  });
  disposers.push(() => log.dispose());
  const rows = () => [...log.el.querySelectorAll('.lush-chat-line')] as HTMLElement[];
  return { game, log, rows };
}

describe('ChatLog', () => {
  it('renders the chat log oldest first as plain text, labelling speakers, without catching pointer events', () => {
    const { game, log, rows } = setup();
    game.chat.push(3, 'hi there', 0);
    game.chat.push(7, 'hello <b>bold</b>', 1);
    game.chat.push(12, 'bye', 2);
    log.update();
    expect(rows().map((r) => r.textContent)).toEqual(['Player 3: hi there', 'You: hello <b>bold</b>', 'Player 12: bye']);
    expect(log.el.querySelector('b')).toBeNull();
    expect(log.el.style.pointerEvents).toBe('none');
    const r = hudLayout(VP.width, VP.height).chat;
    expect(log.el.style.top).toBe(`${r.y}px`);
    expect(log.el.style.height).toBe(`${r.h}px`);
  });

  it('appends new lines and leaves existing DOM nodes untouched', () => {
    const { game, log, rows } = setup();
    game.chat.push(3, 'one', 0);
    log.update();
    const first = rows()[0];
    const mo = new MutationObserver(() => undefined);
    mo.observe(log.el, { childList: true, subtree: true, characterData: true, attributes: true });

    log.update();
    log.update();
    expect(mo.takeRecords()).toHaveLength(0);
    expect(rows()).toEqual([first]);

    game.chat.push(4, 'two', 1);
    log.update();
    expect(rows()).toHaveLength(2);
    expect(rows()[0]).toBe(first);
    expect(rows()[1].textContent).toBe('Player 4: two');
    mo.disconnect();
  });

  it('drops the oldest rows as the store trims its log', () => {
    const { game, log, rows } = setup();
    for (let i = 0; i < 60; i++) game.chat.push(1, `m${i}`, i);
    log.update();
    expect(rows()).toHaveLength(game.chat.log.length);
    expect(rows()[0].textContent).toBe(`Player 1: ${game.chat.log[0].text}`);
    expect(rows().at(-1)!.textContent).toBe('Player 1: m59');
  });

  it('hides while the inventory, shop or trade panel is open', () => {
    const items = new ItemState();
    const hud = new Hud({ parent: document.body, items, viewport: () => VP });
    const shop = new ShopPanel({ parent: document.body, items, send: () => undefined, position: () => ({ ...SHOP_STALL }), viewport: () => VP });
    const trade = { isOpen: false };
    disposers.push(() => hud.dispose(), () => shop.dispose());
    const { game, log } = setup(() => anyOpen([hud, shop, trade]));
    game.chat.push(3, 'hi', 0);
    log.update();
    expect(log.el.style.display).not.toBe('none');

    hud.setOpen(true);
    log.update();
    expect(log.el.style.display).toBe('none');
    hud.setOpen(false);
    log.update();
    expect(log.el.style.display).not.toBe('none');

    expect(shop.tryOpen()).toBe(true);
    expect(shop.isOpen).toBe(true);
    log.update();
    expect(log.el.style.display).toBe('none');
    shop.close();
    log.update();
    expect(log.el.style.display).not.toBe('none');

    trade.isOpen = true;
    log.update();
    expect(log.el.style.display).toBe('none');
    trade.isOpen = false;
    log.update();
    expect(log.el.style.display).not.toBe('none');
  });
});
