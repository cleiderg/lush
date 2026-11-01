import { Window } from 'happy-dom';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { WorldScene, countDrawCalls } from './scene';
import {
  FLOAT_TEXT_HEIGHT,
  FLOAT_TEXT_MS,
  FLOAT_TEXT_RISE_M,
  GOLD_COLOR,
  XP_COLOR,
  floatOpacity,
} from './floatText';
import type { GainView, WorldView } from './worldView';

const view = (gains?: GainView[]): WorldView => ({ local: { id: 1, x: 2, z: 3, heading: 0 }, remotes: [], gains });

function clocked() {
  let t = 1000;
  const w = new WorldScene(() => t);
  return { w, advance: (ms: number) => (t += ms) };
}
const rgb = (hex: string) => {
  const e = document.createElement('div');
  e.style.color = hex;
  return e.style.color;
};

const g = globalThis as { document?: unknown };
const prev = g.document;
let win: Window;
beforeAll(() => {
  win = new Window();
  g.document = win.document;
});
afterAll(async () => {
  g.document = prev;
  await win.happyDOM.close();
});

describe('floating gain numbers', () => {
  it('AC1: each new gain spawns one number over the local player in the theme colour', () => {
    const { w } = clocked();
    w.update(view());
    expect(w.floatTexts.group.parent).toBeNull();
    w.update(view([{ id: 1, kind: 'xp', amount: 25 }, { id: 2, kind: 'gold', amount: 10 }]));
    const [xp, gold] = w.floatTexts.active;
    expect(xp.label.element.textContent).toBe('+25 XP');
    expect(gold.label.element.textContent).toBe('+10 gold');
    expect(xp.label.element.style.color).toBe(rgb(XP_COLOR));
    expect(gold.label.element.style.color).toBe(rgb(GOLD_COLOR));
    expect(xp.label.position.x).toBe(2);
    expect(xp.label.position.z).toBe(3);
    expect(xp.label.position.y).toBe(FLOAT_TEXT_HEIGHT);
  });

  it('AC2: rises, fades to zero over FLOAT_TEXT_MS, is removed, and a seen id does not respawn', () => {
    const { w, advance } = clocked();
    const gains: GainView[] = [{ id: 5, kind: 'xp', amount: 1 }];
    w.update(view(gains));
    const t = w.floatTexts.active[0];
    advance(FLOAT_TEXT_MS / 2);
    w.update(view(gains));
    expect(w.floatTexts.active).toHaveLength(1);
    expect(t.label.position.y).toBeCloseTo(FLOAT_TEXT_HEIGHT + FLOAT_TEXT_RISE_M / 2);
    expect(t.label.element.style.opacity).toBe('1');
    advance(FLOAT_TEXT_MS / 4);
    w.update(view(gains));
    expect(Number(t.label.element.style.opacity)).toBeCloseTo(0.5);
    expect(floatOpacity(FLOAT_TEXT_MS)).toBe(0);
    advance(FLOAT_TEXT_MS);
    w.update(view(gains));
    expect(w.floatTexts.active).toHaveLength(0);
    expect(w.floatTexts.group.children).toHaveLength(0);
  });

  it('AC3: same-frame gains stack apart, and 20 cost the same draw calls as 1', () => {
    const one = clocked();
    one.w.update(view([{ id: 1, kind: 'xp', amount: 1 }]));
    const many = clocked();
    many.w.update(view(Array.from({ length: 20 }, (_, i) => ({ id: i, kind: 'gold' as const, amount: i }))));
    const ys = many.w.floatTexts.active.map((t) => t.label.position.y);
    expect(new Set(ys).size).toBe(20);
    const sorted = [...ys].sort((a, b) => a - b);
    for (let i = 1; i < sorted.length; i++) expect(sorted[i] - sorted[i - 1]).toBeGreaterThan(0.3);
    expect(many.w.floatTexts.active).toHaveLength(20);
    expect(countDrawCalls(many.w.floatTexts.group)).toBe(countDrawCalls(one.w.floatTexts.group));
  });
});
