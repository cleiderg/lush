import type { TradeItemState, TradeView } from '../net/codec';
import type { TradeResultCode } from '../protocol';

export type { TradeItemState, TradeView };

/** A TradeRequestNotice; seq goes up by one per notice so a repeat from the same player is still new. */
export interface TradeRequestState {
  fromId: number;
  seq: number;
}

/** The last TradeResult; seq goes up by one per result so a repeated code is still a new result. */
export interface TradeResultState {
  code: TradeResultCode;
  seq: number;
}

/**
 * Player trading: the latest pending request, the open trade window, and the
 * last result. Subscribers are told after any change.
 */
export class TradeStore {
  /** The latest request from another player, or null. Opening a trade clears it. */
  pendingRequest: TradeRequestState | null = null;
  /** The open trade window, or null when no trade is open. */
  open: TradeView | null = null;
  /** The last TradeResult, or null before any. */
  result: TradeResultState | null = null;
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

  setRequest(fromId: number): void {
    this.pendingRequest = { fromId, seq: ++this.seq };
    this.notify();
  }

  /** Replaces the whole trade window. */
  setState(v: TradeView): void {
    this.open = {
      partnerId: v.partnerId,
      screen: v.screen,
      myItems: v.myItems.map((i) => ({ ...i })),
      myGold: v.myGold,
      partnerItems: v.partnerItems.map((i) => ({ ...i })),
      partnerGold: v.partnerGold,
      myAccepted: v.myAccepted,
      partnerAccepted: v.partnerAccepted,
    };
    this.pendingRequest = null;
    this.notify();
  }

  /** Closes the trade window (if open) and records why. */
  setResult(code: TradeResultCode): void {
    this.open = null;
    this.result = { code, seq: ++this.seq };
    this.notify();
  }

  /** Forgets everything; used on a new Welcome. */
  clear(): void {
    if (!this.pendingRequest && !this.open && !this.result) return;
    this.pendingRequest = null;
    this.open = null;
    this.result = null;
    this.notify();
  }
}
