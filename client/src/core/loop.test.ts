import { describe, expect, it } from 'vitest';
import { GameLoop, STEP_MS } from './loop';

function run(frameMs: number, totalMs: number) {
  let updates = 0;
  let renders = 0;
  const renderTimes: number[] = [];
  const loop = new GameLoop({
    update: (dt) => {
      expect(dt).toBe(STEP_MS);
      updates++;
    },
    render: () => {
      renders++;
      renderTimes.push(now);
    },
  });
  let now = 0;
  for (now = 0; now <= totalMs; now += frameMs) loop.frame(now);
  return { updates, renders, renderTimes };
}

describe('GameLoop', () => {
  it('runs fixed-size updates regardless of frame rate', () => {
    for (const frameMs of [8, 16.67, 33, 100]) {
      const { updates } = run(frameMs, 10_000);
      expect(Math.abs(updates - 10_000 / STEP_MS)).toBeLessThanOrEqual(2);
    }
  });

  it('never renders more than 30 times per second', () => {
    for (const frameMs of [4, 8, 16.67, 33.3]) {
      const { renderTimes } = run(frameMs, 5_000);
      for (let i = 0; i < renderTimes.length; i++) {
        const inWindow = renderTimes.filter(
          (t) => t >= renderTimes[i] && t < renderTimes[i] + 1000,
        ).length;
        expect(inWindow).toBeLessThanOrEqual(30);
      }
    }
  });

  it('caps catch-up steps after a long stall', () => {
    let updates = 0;
    const loop = new GameLoop({ update: () => updates++, render: () => {} });
    loop.frame(0);
    loop.frame(60_000);
    expect(updates).toBeLessThanOrEqual(5);
  });
});
