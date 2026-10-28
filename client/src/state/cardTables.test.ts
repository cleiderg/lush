import { describe, expect, it } from 'vitest';
import type { CardTableView, ServerMessage } from '../net/codec';
import { CardTableResultCode, ShedColour, ShedSymbol } from '../protocol';
import { CardTableStore, cardTableResultText } from './cardTables';
import { GameState } from './world';

const welcome: ServerMessage = { type: 'welcome', playerId: 5, guestToken: 't', spawn: { x: 0, z: 0 }, tickRate: 20 };

function table(tableId: number, seats: number[], extra: Partial<CardTableView> = {}): ServerMessage {
  return {
    type: 'cardTableState', tableId, seats, inProgress: false, turnSeat: null, reversed: false, turnTicksLeft: 0,
    winnerSeat: null, ready: seats.map(() => false), ...extra,
  };
}

function joined(): GameState {
  const g = new GameState();
  g.handle(welcome, 0);
  return g;
}

describe('card table state', () => {
  it('an update replaces the earlier state for the same table and leaves other tables unchanged', () => {
    const g = joined();
    g.handle(table(1, [9, 0, 0, 0]), 0);
    g.handle(table(2, [3, 4]), 0);
    g.handle(table(1, [9, 8, 0, 0], { inProgress: true, turnSeat: 1, reversed: true, turnTicksLeft: 600 }), 0);
    expect(g.cardTables.tables.get(1)).toEqual({
      tableId: 1, seats: [9, 8, 0, 0], inProgress: true, turnSeat: 1, reversed: true, turnTicksLeft: 600, winnerSeat: null,
      ready: [false, false, false, false],
    });
    expect(g.cardTables.tables.get(2)).toEqual({
      tableId: 2, seats: [3, 4], inProgress: false, turnSeat: null, reversed: false, turnTicksLeft: 0, winnerSeat: null,
      ready: [false, false],
    });
    expect(g.cardTables.tables.size).toBe(2);

    g.handle(table(1, [9, 8, 0, 0], { winnerSeat: 0 }), 0);
    expect(g.cardTables.tables.get(1)?.winnerSeat).toBe(0);
    expect(g.cardTables.tables.get(1)?.inProgress).toBe(false);
  });

  it('copies the seat list so later changes to the message do not leak in', () => {
    const g = joined();
    const msg = table(1, [5, 0]);
    g.handle(msg, 0);
    if (msg.type === 'cardTableState') msg.seats[1] = 77;
    expect(g.cardTables.tables.get(1)?.seats).toEqual([5, 0]);
  });

  it('the local seat follows sitting, moving seats and leaving', () => {
    const g = joined();
    let calls = 0;
    g.cardTables.subscribe(() => calls++);
    expect(g.cardTables.mySeat).toBeNull();
    expect(g.cardTables.myTable).toBeNull();

    g.handle(table(2, [9, 0, 0]), 0);
    expect(g.cardTables.mySeat).toBeNull();

    // Sit: the server replies Ok and sends the table with us in it.
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.Ok }, 0);
    g.handle(table(2, [9, 5, 0]), 0);
    expect(g.cardTables.mySeat).toEqual({ tableId: 2, seat: 1 });
    expect(g.cardTables.myTable?.seats).toEqual([9, 5, 0]);

    // Another table changing does not move us.
    g.handle(table(3, [1, 2]), 0);
    expect(g.cardTables.mySeat).toEqual({ tableId: 2, seat: 1 });

    // Leave: Ok after noteLeave clears the seat, even with no further table state.
    g.cardTables.noteLeave();
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.Ok }, 0);
    expect(g.cardTables.mySeat).toBeNull();
    expect(g.cardTables.myTable).toBeNull();
    expect(g.cardTables.tables.get(2)?.seats).toEqual([9, 0, 0]);
    expect(calls).toBe(5);
  });

  it('a table state without us clears the seat at that table', () => {
    const g = joined();
    g.handle(table(1, [0, 5]), 0);
    expect(g.cardTables.mySeat).toEqual({ tableId: 1, seat: 1 });
    g.handle(table(1, [0, 0]), 0);
    expect(g.cardTables.mySeat).toBeNull();
  });

  it('a sit Ok while seated elsewhere does not clear the seat; NotSeated does', () => {
    const g = joined();
    g.handle(table(1, [5]), 0);
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.Ok }, 0);
    expect(g.cardTables.mySeat).toEqual({ tableId: 1, seat: 0 });
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.NotSeated }, 0);
    expect(g.cardTables.mySeat).toBeNull();
  });

  it('a failed leave keeps the seat', () => {
    const g = joined();
    g.handle(table(1, [5, 6]), 0);
    g.cardTables.noteLeave();
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.InProgress }, 0);
    expect(g.cardTables.mySeat).toEqual({ tableId: 1, seat: 0 });
    // The failed leave is no longer pending, so a later Ok does not clear the seat.
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.Ok }, 0);
    expect(g.cardTables.mySeat).toEqual({ tableId: 1, seat: 0 });
  });

  it('records the latest result as its code and message', () => {
    const g = joined();
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.TableFull }, 0);
    expect(g.cardTables.result).toMatchObject({ code: CardTableResultCode.TableFull, message: 'That table is full' });
    const first = g.cardTables.result!.seq;
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.TableFull }, 0);
    expect(g.cardTables.result!.seq).toBeGreaterThan(first);
  });

  it('a new Welcome forgets tables, seat and result', () => {
    const g = joined();
    g.handle(table(1, [5]), 0);
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.Ok }, 0);
    g.handle(welcome, 0);
    expect(g.cardTables.tables.size).toBe(0);
    expect(g.cardTables.mySeat).toBeNull();
    expect(g.cardTables.result).toBeNull();
  });
});

const hand: ServerMessage = {
  type: 'shedHand', tableId: 1,
  cards: [{ colour: ShedColour.One, symbol: ShedSymbol.Five }, { colour: ShedColour.None, symbol: ShedSymbol.Wild }],
};

const shed: ServerMessage = {
  type: 'shedState', tableId: 1, topCard: { colour: ShedColour.Two, symbol: ShedSymbol.Skip }, colour: ShedColour.Two,
  handCounts: [2, 7], drawPile: 85,
};

/** Joined, seated in seat 0 of table 1 with player 6, game in progress, hand and Shed state received. */
function playing(): GameState {
  const g = joined();
  g.handle(table(1, [5, 6], { inProgress: true, turnSeat: 0, ready: [true, true] }), 0);
  g.handle(shed, 0);
  g.handle(hand, 0);
  return g;
}

describe('card table catalog, ready flags, Shed state and hand', () => {
  it('keeps the catalog per table id, replaces it on a new catalog and notifies', () => {
    const g = joined();
    let calls = 0;
    g.cardTables.subscribe(() => calls++);
    g.handle({ type: 'cardTableCatalog', tables: [{ id: 1, seats: 2, stake: 50 }, { id: 2, seats: 4, stake: 200 }] }, 0);
    expect(calls).toBe(1);
    expect(g.cardTables.catalog.get(1)).toEqual({ id: 1, seats: 2, stake: 50 });
    expect(g.cardTables.catalog.get(2)).toEqual({ id: 2, seats: 4, stake: 200 });

    g.handle({ type: 'cardTableCatalog', tables: [{ id: 3, seats: 3, stake: 10 }] }, 0);
    expect(calls).toBe(2);
    expect([...g.cardTables.catalog.keys()]).toEqual([3]);
  });

  it('keeps the catalog across a new Welcome', () => {
    const g = joined();
    g.handle({ type: 'cardTableCatalog', tables: [{ id: 1, seats: 2, stake: 50 }] }, 0);
    g.handle(welcome, 0);
    expect(g.cardTables.catalog.get(1)?.stake).toBe(50);
  });

  it('keeps the ready flags per seat and notifies', () => {
    const g = joined();
    let calls = 0;
    g.cardTables.subscribe(() => calls++);
    g.handle(table(1, [5, 6, 0], { ready: [false, true, false] }), 0);
    g.handle(table(1, [5, 6, 0], { ready: [true, true, false] }), 0);
    expect(calls).toBe(2);
    expect(g.cardTables.myTable?.ready).toEqual([true, true, false]);
  });

  it('a leave clears our ready flag in the kept table state', () => {
    const g = joined();
    g.handle(table(1, [5, 6], { ready: [true, true] }), 0);
    g.cardTables.noteLeave();
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.Ok }, 0);
    expect(g.cardTables.tables.get(1)?.ready).toEqual([false, true]);
  });

  it('keeps Shed public state and the hand, copied, and notifies on each', () => {
    const g = joined();
    g.handle(table(1, [5, 6], { inProgress: true, turnSeat: 0 }), 0);
    let calls = 0;
    g.cardTables.subscribe(() => calls++);
    g.handle(shed, 0);
    expect(calls).toBe(1);
    g.handle(hand, 0);
    expect(calls).toBe(2);
    expect(g.cardTables.myShed).toEqual({
      tableId: 1, topCard: { colour: ShedColour.Two, symbol: ShedSymbol.Skip }, colour: ShedColour.Two,
      handCounts: [2, 7], drawPile: 85,
    });
    expect(g.cardTables.shed.get(1)).toBe(g.cardTables.myShed);
    expect(g.cardTables.hand).toEqual({
      tableId: 1,
      cards: [{ colour: ShedColour.One, symbol: ShedSymbol.Five }, { colour: ShedColour.None, symbol: ShedSymbol.Wild }],
    });
    if (hand.type === 'shedHand') hand.cards[0].symbol = ShedSymbol.Nine;
    if (shed.type === 'shedState') shed.handCounts[0] = 99;
    expect(g.cardTables.hand?.cards[0].symbol).toBe(ShedSymbol.Five);
    expect(g.cardTables.myShed?.handCounts[0]).toBe(2);
    if (hand.type === 'shedHand') hand.cards[0].symbol = ShedSymbol.Five;
    if (shed.type === 'shedState') shed.handCounts[0] = 2;

    // A newer hand replaces the old one.
    g.handle({ type: 'shedHand', tableId: 1, cards: [] }, 0);
    expect(g.cardTables.hand).toEqual({ tableId: 1, cards: [] });
  });

  it('a table update while the game runs keeps the hand', () => {
    const g = playing();
    g.handle(table(1, [5, 6], { inProgress: true, turnSeat: 1, ready: [true, true] }), 0);
    expect(g.cardTables.hand?.cards.length).toBe(2);
    // Another table finishing does not touch our hand.
    g.handle(table(2, [7, 8], { winnerSeat: 0 }), 0);
    expect(g.cardTables.hand?.cards.length).toBe(2);
  });
});

describe('clearing the hand', () => {
  it('a finished game clears the hand and notifies', () => {
    const g = playing();
    let calls = 0;
    g.cardTables.subscribe(() => calls++);
    g.handle(table(1, [5, 6], { winnerSeat: 1 }), 0);
    expect(calls).toBe(1);
    expect(g.cardTables.hand).toBeNull();
    expect(g.cardTables.mySeat).toEqual({ tableId: 1, seat: 0 });
  });

  it('leaving the table (Ok after noteLeave) clears the hand and Shed state', () => {
    const g = playing();
    g.cardTables.noteLeave();
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.Ok }, 0);
    expect(g.cardTables.hand).toBeNull();
    expect(g.cardTables.myShed).toBeNull();
    expect(g.cardTables.shed.has(1)).toBe(false);
  });

  it('a table state without us (forfeit) clears the hand', () => {
    const g = playing();
    g.handle(table(1, [0, 6], { inProgress: true, turnSeat: 1 }), 0);
    expect(g.cardTables.mySeat).toBeNull();
    expect(g.cardTables.hand).toBeNull();
  });

  it('NotSeated clears the hand', () => {
    const g = playing();
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.NotSeated }, 0);
    expect(g.cardTables.hand).toBeNull();
  });

  it('a new Welcome clears the hand and Shed state', () => {
    const g = playing();
    g.handle(welcome, 0);
    expect(g.cardTables.hand).toBeNull();
    expect(g.cardTables.shed.size).toBe(0);
  });

  it('clear() on an empty store does not notify', () => {
    const s = new CardTableStore();
    let calls = 0;
    s.subscribe(() => calls++);
    s.clear();
    expect(calls).toBe(0);
  });
});

describe('cardTableResultText', () => {
  it('gives each new result code a readable message', () => {
    expect(cardTableResultText(CardTableResultCode.NotYourTurn)).toBe('It is not your turn');
    expect(cardTableResultText(CardTableResultCode.IllegalMove)).toBe('That move is not allowed');
    expect(cardTableResultText(CardTableResultCode.NoGame)).toBe('No game is in progress');
    expect(cardTableResultText(CardTableResultCode.NotEnoughGold)).toBe('You do not have enough gold for the stake');
    expect(cardTableResultText(CardTableResultCode.TooFar)).toBe('You are too far from the table');
  });

  it('a new result code reaches the store as its message', () => {
    const g = joined();
    g.handle({ type: 'cardTableResult', code: CardTableResultCode.NotYourTurn }, 0);
    expect(g.cardTables.result).toMatchObject({ code: CardTableResultCode.NotYourTurn, message: 'It is not your turn' });
  });

  it('maps every CardTableResultCode to a non-empty, distinct message', () => {
    const codes = Object.values(CardTableResultCode).filter((v): v is CardTableResultCode => typeof v === 'number');
    const texts = codes.map(cardTableResultText);
    for (const t of texts) expect(t.trim()).not.toBe('');
    expect(new Set(texts).size).toBe(codes.length);
  });

  it('maps codes 0 to 10 to distinct messages and unknown codes to the fallback', () => {
    const texts = Array.from({ length: 11 }, (_, i) => cardTableResultText(i as CardTableResultCode));
    for (const t of texts) {
      expect(t.trim()).not.toBe('');
      expect(t).not.toBe('Card table action failed');
    }
    expect(new Set(texts).size).toBe(11);
    expect(texts.slice(6)).toEqual([
      'It is not your turn',
      'That move is not allowed',
      'No game is in progress',
      'You do not have enough gold for the stake',
      'You are too far from the table',
    ]);
    expect(cardTableResultText(99 as CardTableResultCode)).toBe('Card table action failed');
  });
});
