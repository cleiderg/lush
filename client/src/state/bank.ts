import type { BankSlotState } from '../net/codec';
import skills from '../../../shared/map/skills.json';
import { BankResultCode } from '../protocol';
import { SHOP_DISTANCE_TILES, TILE_METRES, type Point } from './shop';

export type { BankSlotState };

/** The bank booth from shared/map/skills.json, in metres (the file is in decimetres). */
export const BANK_BOOTH: Point = { x: skills.bank.x / 10, z: skills.bank.z / 10 };

/**
 * How close (in tiles) a player must be to the booth to open the bank (skills spec, Bank): the same
 * distance as the shop stall.
 */
export const BANK_DISTANCE_TILES = SHOP_DISTANCE_TILES;

/**
 * Whether pos (metres) is within BANK_DISTANCE_TILES of the booth, measured in a straight line like the
 * server's bank reach check (bank.InReach), so the prompt never offers a bank the server would refuse as too far.
 */
export function nearBank(pos: Point, booth: Point = BANK_BOOTH): boolean {
  const reach = BANK_DISTANCE_TILES * TILE_METRES;
  return Math.hypot(pos.x - booth.x, pos.z - booth.z) <= reach;
}

/** The slots whose item name (from nameOf) contains query, ignoring case and outer spaces; all of them for an empty query. */
export function filterBankSlots(slots: readonly BankSlotState[], query: string, nameOf: (itemId: number) => string): BankSlotState[] {
  const q = query.trim().toLowerCase();
  return q ? slots.filter((s) => nameOf(s.itemId).toLowerCase().includes(q)) : [...slots];
}

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
