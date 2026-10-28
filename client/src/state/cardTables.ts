import type { CardTableView } from '../net/codec';
import { CardTableResultCode } from '../protocol';

export type { CardTableView };

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
 * Card tables (docs/specs/card-tables.md): the latest public state of each
 * table, the local player's seat, and the last result. Only the shared table
 * system; game state such as hands comes later. Subscribers are told after
 * any change.
 */
export class CardTableStore {
  /** Latest CardTableState per table id. */
  readonly tables = new Map<number, CardTableView>();
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

  /**
   * Replaces the state of one table and updates the local player's seat:
   * seated if playerId is in it, not seated if it is the table they were at.
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
    });
    const seat = playerId > 0 ? v.seats.indexOf(playerId) : -1;
    if (seat >= 0) this.mySeat = { tableId: v.tableId, seat };
    else if (this.mySeat?.tableId === v.tableId) this.mySeat = null;
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
      if (t && t.seats[this.mySeat.seat] !== undefined) t.seats[this.mySeat.seat] = 0;
      this.mySeat = null;
    }
    this.leaving = false;
    this.result = { code, message: cardTableResultText(code), seq: ++this.seq };
    this.notify();
  }

  /** Forgets everything; used on a new Welcome. */
  clear(): void {
    if (this.tables.size === 0 && !this.mySeat && !this.result && !this.leaving) return;
    this.tables.clear();
    this.mySeat = null;
    this.result = null;
    this.leaving = false;
    this.notify();
  }
}

// Codes 6-10 are keyed by number so this compiles before the protocol adds those members.
const RESULT_TEXT: Record<number, string> = {
  [CardTableResultCode.Ok]: 'Done',
  [CardTableResultCode.UnknownTable]: 'That table does not exist',
  [CardTableResultCode.TableFull]: 'That table is full',
  [CardTableResultCode.AlreadySeated]: 'You are already seated at a table',
  [CardTableResultCode.NotSeated]: 'You are not seated at a table',
  [CardTableResultCode.InProgress]: 'A game is in progress',
  6: 'It is not your turn',
  7: 'That move is not allowed',
  8: 'No game is in progress',
  9: 'You do not have enough gold for the stake',
  10: 'You are too far from the table',
};

/** Fixed message for a CardTableResult code. */
export function cardTableResultText(code: CardTableResultCode): string {
  return RESULT_TEXT[code] ?? 'Card table action failed';
}
