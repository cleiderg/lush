import { describe, expect, it, vi } from 'vitest';
import { AudioManager } from './audioManager';

class Target {
  private l = new Map<string, Set<() => void>>();
  visibilityState = 'visible';
  addEventListener(t: string, f: () => void): void {
    if (!this.l.has(t)) this.l.set(t, new Set());
    this.l.get(t)!.add(f);
  }
  removeEventListener(t: string, f: () => void): void {
    this.l.get(t)?.delete(f);
  }
  fire(t: string): void {
    for (const f of [...(this.l.get(t) ?? [])]) f();
  }
  count(): number {
    let n = 0;
    for (const s of this.l.values()) n += s.size;
    return n;
  }
}

function setup() {
  const win = new Target();
  const doc = new Target();
  const ctx = { state: 'suspended', resume: vi.fn(() => Promise.resolve()) };
  const factory = vi.fn(() => ctx);
  const mgr = new AudioManager({ factory, window: win, document: doc });
  return { win, doc, ctx, factory, mgr };
}

describe('AudioManager', () => {
  it('creates nothing before a gesture', () => {
    const { factory, mgr } = setup();
    expect(factory).not.toHaveBeenCalled();
    expect(mgr.context()).toBeNull();
    expect(mgr.isUnlocked()).toBe(false);
  });

  it.each(['pointerdown', 'touchend', 'keydown'])('%s unlocks once and removes listeners', async (type) => {
    const { win, doc, ctx, factory, mgr } = setup();
    win.fire(type);
    expect(factory).toHaveBeenCalledTimes(1);
    expect(ctx.resume).toHaveBeenCalledTimes(1);
    expect(mgr.context()).toBe(ctx);
    await Promise.resolve();
    expect(mgr.isUnlocked()).toBe(true);
    win.fire('pointerdown');
    win.fire('touchend');
    win.fire('keydown');
    doc.fire('keydown');
    expect(factory).toHaveBeenCalledTimes(1);
    expect(ctx.resume).toHaveBeenCalledTimes(1);
    expect(win.count()).toBe(0);
    expect(doc.count()).toBe(1); // visibilitychange stays
  });

  it('resumes on visibilitychange when suspended or interrupted, not when running', () => {
    const { win, doc, ctx } = setup();
    doc.fire('visibilitychange');
    expect(ctx.resume).not.toHaveBeenCalled();
    win.fire('pointerdown');
    ctx.resume.mockClear();
    ctx.state = 'running';
    doc.fire('visibilitychange');
    expect(ctx.resume).not.toHaveBeenCalled();
    ctx.state = 'suspended';
    doc.fire('visibilitychange');
    ctx.state = 'interrupted';
    doc.fire('visibilitychange');
    expect(ctx.resume).toHaveBeenCalledTimes(2);
    doc.visibilityState = 'hidden';
    doc.fire('visibilitychange');
    expect(ctx.resume).toHaveBeenCalledTimes(2);
  });
});
