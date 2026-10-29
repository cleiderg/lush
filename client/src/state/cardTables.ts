import type { CardTableInfoState, CardTableView, ShedCardValue, ShedHandState, ShedPublicState } from '../net/codec';
import { CardTableResultCode } from '../protocol';

export type { CardTableInfoState, CardTableView, ShedCardValue, ShedHandState, ShedPublicState };

/** The table and seat index the local player sits in. */
export interface CardTableSeat {
  tableId: number;
  seat: number;
}

/** The last CardTableResult; seq goes up by one per result so a repeated code is still a new result. */
export interface CardTableResultState {
  code: CardTableResultCode;
  message: string;
  seq: number;
}

/**
 * Card tables (docs/specs/card-tables.md): the catalog, the latest public
 * state of each table, the local player's seat, Shed's public state, the
 * local player's hand, and the last result. Subscribers are told after any
 * change.
 */
export class CardTableStore {
  /** Seat count and stake per table id, from CardTableCatalog. */
  readonly catalog = new Map<number, CardTableInfoState>();
  /** Latest CardTableState per table id. */
  readonly tables = new Map<number, CardTableView>();
  /** Latest ShedState per table id. */
  readonly shed = new Map<number, ShedPublicState>();
  /** The local player's Shed hand, or null outside a game. */
  hand: ShedHandState | null = null;
  /** Where the local player sits, or null when not seated. */
  mySeat: CardTableSeat | null = null;
  /** The last CardTableResult, or null before any. */
  result: CardTableResultState | null = null;
  private leaving = false;
  private seq = 0;
  private listeners = new Set<() => void>();

  /** Calls fn after every change; returns an unsubscribe function. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private notify(): void {
    for (const fn of this.listeners) fn();
  }

  /** The table the local player sits at, or null. */
  get myTable(): CardTableView | null {
    return this.mySeat ? (this.tables.get(this.mySeat.tableId) ?? null) : null;
  }

  /** Shed's public state at the local player's table, or null. */
  get myShed(): ShedPublicState | null {
    return this.mySeat ? (this.shed.get(this.mySeat.tableId) ?? null) : null;
  }

  /** Replaces the catalog. */
  setCatalog(tables: CardTableInfoState[]): void {
    this.catalog.clear();
    for (const t of tables) this.catalog.set(t.id, { id: t.id, seats: t.seats, stake: t.stake });
    this.notify();
  }

  setShedState(v: ShedPublicState): void {
    this.shed.set(v.tableId, {
      tableId: v.tableId,
      topCard: { ...v.topCard },
      colour: v.colour,
      handCounts: [...v.handCounts],
      drawPile: v.drawPile,
    });
    this.notify();
  }

  setHand(v: ShedHandState): void {
    this.hand = { tableId: v.tableId, cards: v.cards.map((c) => ({ ...c })) };
    this.notify();
  }

  /**
   * Replaces the state of one table and updates the local player's seat:
   * seated if playerId is in it, not seated if it is the table they were at.
   * The hand is dropped when the local player is no longer at its table or
   * the game there is over.
   */
  setState(v: CardTableView, playerId: number): void {
    this.tables.set(v.tableId, {
      tableId: v.tableId,
      seats: [...v.seats],
      inProgress: v.inProgress,
      turnSeat: v.turnSeat,
      reversed: v.reversed,
      turnTicksLeft: v.turnTicksLeft,
      winnerSeat: v.winnerSeat,
      ready: [...v.ready],
    });
    const seat = playerId > 0 ? v.seats.indexOf(playerId) : -1;
    if (seat >= 0) this.mySeat = { tableId: v.tableId, seat };
    else if (this.mySeat?.tableId === v.tableId) this.leftTable();
    if (this.hand?.tableId === v.tableId && !v.inProgress) this.hand = null;
    this.notify();
  }

  /**
   * Call when sending CardTableLeave. The server stops sending a table's
   * state once you have left it, so an Ok result for the leave is what
   * clears the local seat.
   */
  noteLeave(): void {
    this.leaving = true;
  }

  setResult(code: CardTableResultCode): void {
    const left = (code === CardTableResultCode.Ok && this.leaving) || code === CardTableResultCode.NotSeated;
    if (left && this.mySeat) {
      const t = this.tables.get(this.mySeat.tableId);
      if (t && t.seats[this.mySeat.seat] !== undefined) {
        t.seats[this.mySeat.seat] = 0;
        if (this.mySeat.seat < t.ready.length) t.ready[this.mySeat.seat] = false;
      }
      this.leftTable();
    }
    this.leaving = false;
    this.result = { code, message: cardTableResultText(code), seq: ++this.seq };
    this.notify();
  }

  /** Clears the local seat, the hand and Shed's state at that table; the server stops sending them. */
  private leftTable(): void {
    if (this.mySeat) this.shed.delete(this.mySeat.tableId);
    this.mySeat = null;
    this.hand = null;
  }

  /** Forgets everything but the catalog, which the server sends once per connection; used on a new Welcome. */
  clear(): void {
    if (this.tables.size === 0 && this.shed.size === 0 && !this.hand && !this.mySeat
      && !this.result && !this.leaving) return;
    this.tables.clear();
    this.shed.clear();
    this.hand = null;
    this.mySeat = null;
    this.result = null;
    this.leaving = false;
    this.notify();
  }
}

const RESULT_TEXT: Record<number, string> = {
  [CardTableResultCode.Ok]: 'Done',
  [CardTableResultCode.UnknownTable]: 'That table does not exist',
  [CardTableResultCode.TableFull]: 'That table is full',
  [CardTableResultCode.AlreadySeated]: 'You are already seated at a table',
  [CardTableResultCode.NotSeated]: 'You are not seated at a table',
  [CardTableResultCode.InProgress]: 'A game is in progress',
  [CardTableResultCode.NotYourTurn]: 'It is not your turn',
  [CardTableResultCode.IllegalMove]: 'That move is not allowed',
  [CardTableResultCode.NoGame]: 'No game is in progress',
  [CardTableResultCode.NotEnoughGold]: 'You do not have enough gold for the stake',
  [CardTableResultCode.TooFar]: 'You are too far from the table',
};

/** Fixed message for a CardTableResult code. */
export function cardTableResultText(code: CardTableResultCode): string {
  return RESULT_TEXT[code] ?? 'Card table action failed';
}
