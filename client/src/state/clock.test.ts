import { afterEach, describe, expect, it, vi } from 'vitest';
import { SharedClock } from './clock';
import { GameState } from './world';

const SERVER = Date.UTC(2026, 9, 30, 19, 0); // 12:00 PDT
const HOUR = 3_600_000;

function fakeLocal(start: number) {
  let t = start;
  return { now: () => t, advance: (ms: number) => { t += ms; } };
}

describe('SharedClock', () => {
  afterEach(() => vi.useRealTimers());

  it('follows the server when the local clock is 3 hours wrong', () => {
    const local = fakeLocal(SERVER - 3 * HOUR);
    const clock = new SharedClock(local.now);
    clock.sync(SERVER);
    expect(clock.now()).toBe(SERVER);
    local.advance(12_345);
    expect(clock.now()).toBe(SERVER + 12_345);
    expect(clock.serverNow()).toBe(SERVER + 12_345);
  });

  it('uses the local clock until synced', () => {
    const local = fakeLocal(1000);
    expect(new SharedClock(local.now).now()).toBe(1000);
  });

  it('is synced from Welcome.serverTimeMs by GameState', () => {
    vi.useFakeTimers();
    vi.setSystemTime(SERVER + 3 * HOUR);
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 1, tickRate: 20, spawn: { x: 0, z: 0 }, serverTimeMs: SERVER } as never, 0);
    expect(g.clock.now()).toBe(SERVER);
    vi.advanceTimersByTime(60_000);
    expect(g.clock.now()).toBe(SERVER + 60_000);
  });

  it('keeps its offset when Welcome has no server time', () => {
    vi.useFakeTimers();
    vi.setSystemTime(SERVER);
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 1, tickRate: 20, spawn: { x: 0, z: 0 } } as never, 0);
    expect(g.clock.now()).toBe(SERVER);
  });

  it('runs on from an override, freezes while paused and resumes from there', () => {
    const local = fakeLocal(0);
    const clock = new SharedClock(local.now);
    clock.sync(SERVER);
    clock.override(SERVER + HOUR);
    local.advance(1000);
    expect(clock.now()).toBe(SERVER + HOUR + 1000);
    clock.pause();
    local.advance(HOUR);
    expect(clock.now()).toBe(SERVER + HOUR + 1000);
    clock.resume();
    local.advance(500);
    expect(clock.now()).toBe(SERVER + HOUR + 1500);
    local.advance(HOUR);
    clock.live();
    // Back on the server's clock: 1000 + HOUR + 500 + HOUR ms since sync.
    expect(clock.now()).toBe(SERVER + 2 * HOUR + 1500);
    expect(clock.overridden).toBe(false);
  });
});
