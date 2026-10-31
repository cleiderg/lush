// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { dayPhase, irvineTime } from '../core/sun';
import { SharedClock } from '../state/clock';
import { debugEnabled, mountTimePanel, presetTime, TIME_PRESETS, TimePanel } from './timePanel';

// 30 Oct 2026, 10:00 PDT; the local clock is 3 hours off.
const SERVER = Date.UTC(2026, 9, 30, 17, 0);
const HOUR = 3_600_000;

function click(panel: TimePanel, sel: string): void {
  (panel.root.querySelector(sel) as HTMLButtonElement).click();
}

describe('TimePanel', () => {
  let clock: SharedClock;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(SERVER + 3 * HOUR);
    clock = new SharedClock();
    clock.sync(SERVER);
    document.body.innerHTML = '';
  });
  afterEach(() => vi.useRealTimers());

  it('is not in the DOM without ?debug', () => {
    expect(mountTimePanel('', { parent: document.body, clock })).toBeNull();
    expect(mountTimePanel('?foo=1', { parent: document.body, clock })).toBeNull();
    expect(document.querySelector('.lush-time-panel')).toBeNull();
    expect(document.body.querySelectorAll('button').length).toBe(0);
  });

  it('shows Dawn, Noon, Dusk, Night and Pause with ?debug', () => {
    const panel = mountTimePanel('?debug', { parent: document.body, clock });
    expect(panel).not.toBeNull();
    expect(document.querySelector('.lush-time-panel')).toBe(panel!.root);
    const labels = [...panel!.root.querySelectorAll('button')].map((b) => b.textContent);
    expect(labels).toEqual(expect.arrayContaining(['Dawn', 'Noon', 'Dusk', 'Night', 'Pause']));
  });

  it('accepts ?debug with a value or among other params', () => {
    expect(debugEnabled('?debug=1')).toBe(true);
    expect(debugEnabled('?a=b&debug')).toBe(true);
    expect(debugEnabled('?debugger')).toBe(false);
  });

  it.each([
    ['dawn', 'dawn'],
    ['noon', 'day'],
    ['dusk', 'dusk'],
    ['night', 'night'],
  ] as const)('%s gives dayPhase %s on the current Irvine date', (preset, phase) => {
    const panel = new TimePanel({ parent: document.body, clock });
    click(panel, `[data-preset="${preset}"]`);
    expect(clock.overridden).toBe(true);
    expect(dayPhase(clock.now())).toBe(phase);
    const d = irvineTime(clock.now());
    expect([d.year, d.month, d.day]).toEqual([2026, 10, 30]);
  });

  it('gives the matching phase on other dates, summer and winter', () => {
    for (const t of [Date.UTC(2026, 5, 21, 19), Date.UTC(2026, 11, 21, 20), Date.UTC(2027, 2, 14, 18)]) {
      expect(TIME_PRESETS.map((p) => dayPhase(presetTime(p, t)))).toEqual(['dawn', 'day', 'dusk', 'night']);
    }
  });

  it('freezes the time while paused and runs on after Resume', () => {
    const panel = new TimePanel({ parent: document.body, clock });
    click(panel, '[data-preset="noon"]');
    click(panel, '[data-action="pause"]');
    expect(panel.pauseButton.getAttribute('aria-pressed')).toBe('true');
    const frozen = clock.now();
    vi.advanceTimersByTime(10 * 60_000);
    expect(clock.now()).toBe(frozen);
    click(panel, '[data-preset="dusk"]');
    const dusk = clock.now();
    vi.advanceTimersByTime(60_000);
    expect(clock.now()).toBe(dusk);
    click(panel, '[data-action="pause"]');
    expect(panel.pauseButton.textContent).toBe('Pause');
    vi.advanceTimersByTime(1000);
    expect(clock.now()).toBe(dusk + 1000);
  });

  it('Live goes back to the server time', () => {
    const panel = new TimePanel({ parent: document.body, clock });
    click(panel, '[data-preset="night"]');
    click(panel, '[data-action="pause"]');
    click(panel, '[data-action="live"]');
    expect(clock.paused).toBe(false);
    expect(clock.now()).toBe(SERVER);
  });
});
