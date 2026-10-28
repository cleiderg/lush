import { describe, expect, it } from 'vitest';
import type { CardTableView, ServerMessage } from '../net/codec';
import { CardTableResultCode } from '../protocol';
import { cardTableResultText } from './cardTables';
import { GameState } from './world';

const welcome: ServerMessage = { type: 'welcome', playerId: 5, guestToken: 't', spawn: { x: 0, z: 0 }, tickRate: 20 };

function table(tableId: number, seats: number[], extra: Partial<CardTableView> = {}): ServerMessage {
  return {
    type: 'cardTableState', tableId, seats, inProgress: false, turnSeat: null, reversed: false, turnTicksLeft: 0,
    winnerSeat: null, ...extra,
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
    });
    expect(g.cardTables.tables.get(2)).toEqual({
      tableId: 2, seats: [3, 4], inProgress: false, turnSeat: null, reversed: false, turnTicksLeft: 0, winnerSeat: null,
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

describe('cardTableResultText', () => {
  it('maps every CardTableResultCode to a non-empty, distinct message', () => {
    const codes = Object.values(CardTableResultCode).filter((v): v is CardTableResultCode => typeof v === 'number');
    expect(codes.length).toBe(6);
    const texts = codes.map(cardTableResultText);
    for (const t of texts) expect(t.trim()).not.toBe('');
    expect(new Set(texts).size).toBe(codes.length);
  });
});
