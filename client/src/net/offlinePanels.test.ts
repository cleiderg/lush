import { describe, expect, it } from 'vitest';
import { SKILLS, XP_TABLE } from '../state/skills';
import { GameState } from '../state/world';
import { decode, type ServerMessage } from './codec';
import { nearBank } from '../state/bank';
import { encodeWelcome, OFFLINE_PANELS, OFFLINE_PLACES, OFFLINE_PLAYER_ID, offlineOptions, type OfflineOptions } from './offline';
import { OFFLINE_TABLE_ID, offlinePanelMessages, withOfflinePanels } from './offlinePanels';
import { Session } from './session';
import type { Transport } from './transport';

/** The panel-relevant state a session ends up with. */
function panelState(game: GameState) {
  return {
    stats: game.items.stats,
    inventory: game.items.inventory,
    catalog: [...game.items.catalogItems()],
    trade: game.trade.open,
    tableCatalog: [...game.cardTables.catalog.values()],
    tables: [...game.cardTables.tables.values()],
    shed: [...game.cardTables.shed.values()],
    hand: game.cardTables.hand,
    mySeat: game.cardTables.mySeat,
    chat: game.chat.log,
    bank: game.bank.slots,
    skills: SKILLS.map((sk) => ({ level: game.skills.level(sk), xp: game.skills.xp(sk) })),
  };
}

/** A transport that answers Hello with a Welcome and nothing else, like a server before any game. */
class WelcomeOnlyTransport implements Transport {
  onMessage: ((bytes: Uint8Array) => void) | null = null;
  onOpen: (() => void) | null = null;
  onClose: (() => void) | null = null;
  send(): void {
    this.onMessage?.(encodeWelcome(OFFLINE_PLAYER_ID, OFFLINE_PLACES.spawn, 0));
  }
  close(): void {}
}

/** Starts a Session the way main.ts does for this URL query and returns the game state after Welcome. */
function sessionFor(search: string): GameState {
  const game = new GameState();
  const offline = offlineOptions(search);
  const transport = new WelcomeOnlyTransport();
  const session = new Session({
    factory: () => transport,
    store: { get: () => '', set: () => {} },
    onMessage: withOfflinePanels(offline, (msg) => game.handle(msg, 0)),
    onReload: () => {},
  });
  session.start();
  transport.onOpen?.();
  return game;
}

function stateFor(opts: OfflineOptions): ReturnType<typeof panelState> {
  const game = new GameState();
  const handle = withOfflinePanels(opts, (m) => game.handle(m, 0));
  const welcome = decode(encodeWelcome(OFFLINE_PLAYER_ID, opts.spawn, 0));
  if (!welcome) throw new Error('welcome did not decode');
  handle(welcome);
  return panelState(game);
}

describe('offline panel data', () => {
  it('reads the panel from the URL and starts within reach of the shop and card table', () => {
    expect(offlineOptions('?offline&panel=inventory')?.panel).toBe('inventory');
    expect(offlineOptions('?offline&panel=nope')?.panel).toBeUndefined();
    expect(offlineOptions('?offline&panel=shop')?.spawn).toEqual(OFFLINE_PLACES.shop);
    expect(offlineOptions('?offline&panel=card-table')?.spawn).toEqual(OFFLINE_PLACES.cardtable);
    expect(offlineOptions('?offline&panel=chat-log')?.spawn).toEqual(OFFLINE_PLACES.spawn);
    expect(offlineOptions('?offline&panel=shop&place=park')?.spawn).toEqual(OFFLINE_PLACES.park);
  });

  it('gives the same panel state for the same seed', () => {
    for (const panel of OFFLINE_PANELS) {
      const a = stateFor({ ...offlineOptions(`?offline&seed=7&panel=${panel}`)! });
      const b = stateFor({ ...offlineOptions(`?offline&seed=7&panel=${panel}`)! });
      expect(a).toEqual(b);
    }
    expect(offlinePanelMessages({ seed: 3, panel: 'trade' })).toEqual(offlinePanelMessages({ seed: 3, panel: 'trade' }));
    expect(offlinePanelMessages({ seed: 3, panel: 'trade' })).not.toEqual(offlinePanelMessages({ seed: 4, panel: 'trade' }));
  });

  it('fills every panel: inventory, shop catalog, a trade with offers on both sides, a seated card table and chat', () => {
    const s = stateFor(offlineOptions('?offline&seed=1&panel=trade')!);
    expect(s.inventory.slots.filter((x) => x.itemId).length).toBeGreaterThan(0);
    expect(s.catalog.filter((d) => d.shopSells).length).toBeGreaterThan(0);
    expect(s.stats.gold).toBeGreaterThan(0);
    expect(s.trade).not.toBeNull();
    expect(s.trade!.myItems.length).toBeGreaterThan(0);
    expect(s.trade!.partnerItems.length).toBeGreaterThan(0);
    expect(s.trade!.partnerId).not.toBe(OFFLINE_PLAYER_ID);
    const table = s.tables.find((t) => t.tableId === OFFLINE_TABLE_ID);
    expect(table?.seats.filter((id) => id !== 0 && id !== OFFLINE_PLAYER_ID).length).toBeGreaterThan(0);
    expect(s.mySeat).toEqual({ tableId: OFFLINE_TABLE_ID, seat: 0 });
    expect(s.hand?.cards.length).toBeGreaterThan(0);
    expect(s.shed.length).toBe(1);
    expect(s.chat.length).toBeGreaterThan(0);
  });

  it('opens the bank only for the bank panel, starting within reach of the booth', () => {
    expect(offlineOptions('?offline&panel=bank')?.spawn).toEqual(OFFLINE_PLACES.bank);
    expect(nearBank(OFFLINE_PLACES.bank)).toBe(true);
    const s = stateFor(offlineOptions('?offline&seed=1&panel=bank')!);
    expect(s.bank?.length).toBeGreaterThan(0);
    for (const slot of s.bank!) expect(s.catalog.some((d) => d.id === slot.itemId)).toBe(true);
    for (const panel of OFFLINE_PANELS.filter((p) => p !== 'bank')) {
      expect(stateFor(offlineOptions(`?offline&panel=${panel}`)!).bank).toBeNull();
    }
  });

  it('gives each skill a level above 1 or some XP, with XP inside its level (#466)', () => {
    for (const seed of [1, 2, 3, 4, 5]) {
      const s = stateFor(offlineOptions(`?offline&seed=${seed}&panel=skills`)!);
      expect(s.skills).toHaveLength(3);
      for (const { level, xp } of s.skills) {
        expect(level).toBeGreaterThanOrEqual(1);
        expect(xp).toBeGreaterThan(0);
        expect(xp).toBeGreaterThanOrEqual(XP_TABLE[level]);
        expect(xp).toBeLessThan(XP_TABLE[level + 1]);
      }
    }
  });

  it('opens the trade window only for the trade panel, since it takes the panel area', () => {
    for (const panel of OFFLINE_PANELS.filter((p) => p !== 'trade')) {
      expect(stateFor(offlineOptions(`?offline&panel=${panel}`)!).trade).toBeNull();
    }
  });

  it('only uses item ids that are in its own catalog', () => {
    const msgs = offlinePanelMessages({ seed: 1, panel: 'trade' });
    const catalog = msgs.find((m): m is Extract<ServerMessage, { type: 'itemCatalog' }> => m.type === 'itemCatalog');
    const ids = new Set(catalog?.items.map((d) => d.id));
    const inv = msgs.find((m) => m.type === 'inventory');
    const trade = msgs.find((m) => m.type === 'tradeState');
    const used = [
      ...(inv?.type === 'inventory' ? inv.slots.filter((x) => x.itemId).map((x) => x.itemId) : []),
      ...(trade?.type === 'tradeState' ? [...trade.myItems, ...trade.partnerItems].map((x) => x.itemId) : []),
    ];
    expect(used.length).toBeGreaterThan(0);
    for (const id of used) expect(ids.has(id)).toBe(true);
  });
});

describe('offline panel data stays offline', () => {
  const empty = panelState(new GameState());

  it('never reaches a session without ?offline', () => {
    for (const panel of OFFLINE_PANELS) {
      const game = sessionFor(`?panel=${panel}&seed=1`);
      expect(game.playerId).toBe(OFFLINE_PLAYER_ID);
      expect(panelState(game)).toEqual(empty);
    }
  });

  it('reaches an offline session that asks for a panel, through the same Session path', () => {
    const game = sessionFor('?offline&panel=inventory');
    expect(panelState(game)).not.toEqual(empty);
    expect(game.items.inventory.slots.some((s) => s.itemId)).toBe(true);
  });

  it('leaves plain offline sessions (the world shots) alone', () => {
    expect(panelState(sessionFor('?offline'))).toEqual(empty);
  });

  it('passes the handler through untouched when not offline', () => {
    const handle = (): void => {};
    expect(withOfflinePanels(null, handle)).toBe(handle);
    expect(withOfflinePanels(offlineOptions(''), handle)).toBe(handle);
  });
});
