import type { HitEvent } from '../net/codec';

export type { HitEvent };

/** Hits of one combat tick, as received in a CombatHit message. */
export interface HitBatch {
  tick: number;
  hits: HitEvent[];
}

/** Most batches kept when nothing drains the queue (for example a hidden tab). */
export const MAX_HIT_BATCHES = 64;

/**
 * CombatHit events waiting for render (hitsplats, deaths). Each message is one
 * batch; render takes everything with drain(). Oldest batches are dropped past
 * MAX_HIT_BATCHES so an undrained queue can't grow without bound.
 */
export class HitQueue {
  private batches: HitBatch[] = [];

  get length(): number {
    return this.batches.length;
  }

  push(tick: number, hits: HitEvent[]): void {
    this.batches.push({ tick, hits: hits.map((h) => ({ ...h })) });
    if (this.batches.length > MAX_HIT_BATCHES) this.batches.splice(0, this.batches.length - MAX_HIT_BATCHES);
  }

  /** Returns every queued batch, oldest first, and empties the queue. */
  drain(): HitBatch[] {
    const out = this.batches;
    this.batches = [];
    return out;
  }

  clear(): void {
    this.batches = [];
  }
}
