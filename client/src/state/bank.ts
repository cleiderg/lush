import type { BankSlotState } from '../net/codec';
import { BankResultCode } from '../protocol';

export type { BankSlotState };

/** The last BankResult; seq goes up by one per result so a repeated code is still a new result. */
export interface BankResultState {
  code: BankResultCode;
  seq: number;
}

/** Result codes after which the server has closed the bank. */
const CLOSING_CODES: ReadonlySet<BankResultCode> = new Set([
  BankResultCode.TooFar, BankResultCode.InCombat, BankResultCode.InPvpZone, BankResultCode.NotOpen,
]);

/**
 * The bank: whether it is open, its filled slots in server order, and the
 * last result. Subscribers are told after any change.
 */
export class BankStore {
  /** Filled slots in server order while the bank is open, or null when closed. */
  slots: BankSlotState[] | null = null;
  /** The last BankResult, or null before any. */
  result: BankResultState | null = null;
  private seq = 0;
  private listeners = new Set<() => void>();

  get isOpen(): boolean {
    return this.slots !== null;
  }

  /** Calls fn after every change; returns an unsubscribe function. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private notify(): void {
    for (const fn of this.listeners) fn();
  }

  /** A BankState: opens the bank (if closed) and replaces its slots. */
  setState(slots: BankSlotState[]): void {
    this.slots = slots.map((s) => ({ ...s }));
    this.notify();
  }

  /** Records a BankResult; closes the bank for codes where the server closes it. */
  setResult(code: BankResultCode): void {
    if (CLOSING_CODES.has(code)) this.slots = null;
    this.result = { code, seq: ++this.seq };
    this.notify();
  }

  /** The client closed the bank (it sends BankClose, which has no reply). */
  close(): void {
    if (this.slots === null) return;
    this.slots = null;
    this.notify();
  }

  /** Forgets everything; used on a new Welcome. */
  clear(): void {
    if (!this.slots && !this.result) return;
    this.slots = null;
    this.result = null;
    this.notify();
  }
}
