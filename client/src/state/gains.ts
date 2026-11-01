/** One gain; same shape as render's GainView (state can't import render). */
export interface GainView {
  readonly id: number;
  readonly kind: 'xp' | 'gold';
  readonly amount: number;
}

/** Most gains kept when nothing reads them (for example a hidden tab). */
export const MAX_GAINS = 16;

interface Entry extends GainView {
  at: number;
}

/**
 * XP and gold gains worked out from successive PlayerStats. The first stats after reset() only set
 * the baseline; a drop (buying, a death, a stake) adds nothing. Gains older than the given
 * lifetime are dropped when read, and the list is capped at MAX_GAINS, so it stays bounded.
 */
export class GainTracker {
  private entries: Entry[] = [];
  private last: { xp: number; gold: number } | null = null;
  private nextId = 1;

  update(xp: number, gold: number, now: number): void {
    const last = this.last;
    this.last = { xp, gold };
    if (!last) return;
    if (xp > last.xp) this.add('xp', xp - last.xp, now);
    if (gold > last.gold) this.add('gold', gold - last.gold, now);
  }

  private add(kind: 'xp' | 'gold', amount: number, at: number): void {
    this.entries.push({ id: this.nextId++, kind, amount, at });
    if (this.entries.length > MAX_GAINS) this.entries.splice(0, this.entries.length - MAX_GAINS);
  }

  /** Gains younger than lifetimeMs; older ones are removed. */
  active(now: number, lifetimeMs: number): GainView[] {
    this.entries = this.entries.filter((e) => now - e.at < lifetimeMs);
    return this.entries.map(({ id, kind, amount }) => ({ id, kind, amount }));
  }

  /** New session (Welcome or reconnect): forget the baseline and pending gains. Ids keep counting. */
  reset(): void {
    this.entries = [];
    this.last = null;
  }
}
