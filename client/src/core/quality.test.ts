import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  QUALITY_KEY,
  QUALITY_MIN_FPS,
  QUALITY_WARMUP_S,
  QualityChooser,
  effectivePreset,
  localQualityStore,
  startingPreset,
} from './quality';

/** Feeds `seconds` of frames at `fps`; returns the final preset. */
function run(c: QualityChooser, fps: number, seconds: number): void {
  const n = Math.round(fps * seconds);
  for (let i = 0; i < n; i++) c.frame(1 / fps);
}

describe('startingPreset (AC1)', () => {
  it('is Medium for a phone', () => {
    expect(startingPreset({ coarsePointer: true, shortSidePx: 390 })).toBe('medium');
  });
  it('is High for a desktop', () => {
    expect(startingPreset({ coarsePointer: false, shortSidePx: 900 })).toBe('high');
  });
  it('is High for a tablet (coarse pointer, large screen)', () => {
    expect(startingPreset({ coarsePointer: true, shortSidePx: 768 })).toBe('high');
  });
  it('is High for a small desktop window with a fine pointer', () => {
    expect(startingPreset({ coarsePointer: false, shortSidePx: 500 })).toBe('high');
  });
});

describe('QualityChooser (AC2)', () => {
  it('ignores slow frames during warm-up', () => {
    const c = new QualityChooser('high');
    run(c, 10, QUALITY_WARMUP_S);
    expect(c.preset).toBe('high');
  });
  it('steps down one preset when the window stays under the threshold', () => {
    const c = new QualityChooser('high');
    run(c, 10, QUALITY_WARMUP_S);
    run(c, 20, 5.5);
    expect(c.preset).toBe('medium');
  });
  it('steps down again only after a fresh window, never below low', () => {
    const c = new QualityChooser('high');
    run(c, 10, QUALITY_WARMUP_S);
    run(c, 20, 5.5);
    expect(c.preset).toBe('medium');
    run(c, 20, 5.5);
    expect(c.preset).toBe('low');
    run(c, 20, 30);
    expect(c.preset).toBe('low');
  });
  it('stays put when the average is at or above the threshold', () => {
    const c = new QualityChooser('high');
    run(c, 10, QUALITY_WARMUP_S);
    run(c, QUALITY_MIN_FPS + 3, 30);
    expect(c.preset).toBe('high');
  });
  it('averages over the window: one hitch does not step down', () => {
    const c = new QualityChooser('high');
    run(c, 10, QUALITY_WARMUP_S);
    run(c, 30, 4);
    c.frame(0.5);
    run(c, 30, 4);
    expect(c.preset).toBe('high');
  });
  it('never steps up on its own', () => {
    const c = new QualityChooser('medium');
    run(c, 10, QUALITY_WARMUP_S);
    run(c, 20, 5.5);
    expect(c.preset).toBe('low');
    run(c, 60, 60);
    expect(c.preset).toBe('low');
  });
});

describe('effectivePreset (AC3)', () => {
  it.each(['low', 'medium', 'high'] as const)('returns a stored %s unchanged', (p) => {
    for (const auto of ['low', 'medium', 'high'] as const) {
      expect(effectivePreset(p, auto)).toBe(p);
    }
  });
  it('falls back to the automatic choice on auto', () => {
    expect(effectivePreset('auto', 'medium')).toBe('medium');
    const c = new QualityChooser('high');
    run(c, 10, QUALITY_WARMUP_S);
    run(c, 20, 5.5);
    expect(effectivePreset('auto', c.preset)).toBe('medium');
    expect(effectivePreset('high', c.preset)).toBe('high');
  });
});

describe('localQualityStore (AC4)', () => {
  afterEach(() => vi.unstubAllGlobals());

  function stubStorage(data: Map<string, string>): void {
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => data.get(k) ?? null,
      setItem: (k: string, v: string) => void data.set(k, v),
    });
  }

  it('round-trips under lush.quality', () => {
    const data = new Map<string, string>();
    stubStorage(data);
    const s = localQualityStore();
    expect(s.get()).toBe('auto');
    s.set('low');
    expect(data.get(QUALITY_KEY)).toBe('low');
    expect(QUALITY_KEY).toBe('lush.quality');
    expect(s.get()).toBe('low');
    s.set('auto');
    expect(s.get()).toBe('auto');
  });
  it('treats unknown stored values as auto', () => {
    stubStorage(new Map([[QUALITY_KEY, 'ultra']]));
    expect(localQualityStore().get()).toBe('auto');
  });
  it('falls back to auto when localStorage throws', () => {
    const boom = (): never => {
      throw new Error('denied');
    };
    vi.stubGlobal('localStorage', { getItem: boom, setItem: boom });
    const s = localQualityStore();
    expect(s.get()).toBe('auto');
    expect(() => s.set('high')).not.toThrow();
  });
  it('falls back to auto when localStorage itself is inaccessible', () => {
    vi.stubGlobal('localStorage', undefined);
    const s = localQualityStore();
    expect(s.get()).toBe('auto');
    expect(() => s.set('low')).not.toThrow();
  });
});
