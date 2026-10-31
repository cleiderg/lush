import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { DAY_ABOVE, NIGHT_BELOW, dayPhase, irvineTime, sunPosition, sunTimes } from './sun';

const DEG = 180 / Math.PI;
const MIN = 60_000;

/** Scans a day minute by minute and returns the time and position of the highest sun. */
function highest(fromMs: number) {
  let best = { t: fromMs, ...sunPosition(fromMs) };
  for (let t = fromMs; t < fromMs + 86_400_000; t += MIN) {
    const p = sunPosition(t);
    if (p.elevation > best.elevation) best = { t, ...p };
  }
  return best;
}

describe('irvineTime', () => {
  it('handles the end of daylight saving on 2026-11-01', () => {
    const pdt = irvineTime(Date.parse('2026-11-01T08:30Z'));
    expect([pdt.hour, pdt.minute, pdt.offsetMinutes]).toEqual([1, 30, -420]);
    const pst = irvineTime(Date.parse('2026-11-01T10:30Z'));
    expect([pst.hour, pst.minute, pst.offsetMinutes]).toEqual([2, 30, -480]);
  });

  it('returns the Irvine calendar date, not the UTC one', () => {
    const t = irvineTime(Date.parse('2026-12-22T03:00Z'));
    expect([t.year, t.month, t.day, t.hour]).toEqual([2026, 12, 21, 19]);
  });
});

describe('sunPosition', () => {
  it('peaks at 32.9° due south around 11:50 PST on 2026-12-21', () => {
    const top = highest(Date.parse('2026-12-21T08:00Z'));
    expect(Math.abs(top.elevation * DEG - 32.9)).toBeLessThanOrEqual(0.3);
    expect(Math.abs(top.t - Date.parse('2026-12-21T19:50Z'))).toBeLessThanOrEqual(3 * MIN);
    expect(Math.abs(top.azimuth * DEG - 180)).toBeLessThanOrEqual(1);
  });

  it('peaks at 79.8° around 12:54 PDT on 2026-06-21', () => {
    const top = highest(Date.parse('2026-06-21T07:00Z'));
    expect(Math.abs(top.elevation * DEG - 79.8)).toBeLessThanOrEqual(0.3);
    expect(Math.abs(top.t - Date.parse('2026-06-21T19:54Z'))).toBeLessThanOrEqual(3 * MIN);
  });

  it('measures azimuth clockwise from north: east in the morning, west in the evening', () => {
    const morning = sunPosition(Date.parse('2026-06-21T14:00Z')).azimuth * DEG;
    const evening = sunPosition(Date.parse('2026-06-22T02:00Z')).azimuth * DEG;
    expect(morning).toBeGreaterThan(45);
    expect(morning).toBeLessThan(135);
    expect(evening).toBeGreaterThan(225);
    expect(evening).toBeLessThan(315);
  });
});

describe('sunTimes', () => {
  const near = (actual: number, iso: string) =>
    expect(Math.abs(actual - Date.parse(iso))).toBeLessThanOrEqual(3 * MIN);

  it('gives 06:53 and 16:48 PST on 2026-12-21', () => {
    const { sunrise, sunset } = sunTimes(Date.parse('2026-12-21T20:00Z'));
    near(sunrise, '2026-12-21T14:53Z');
    near(sunset, '2026-12-22T00:48Z');
  });

  it('gives 05:42 and 20:05 PDT on 2026-06-21', () => {
    const { sunrise, sunset } = sunTimes(Date.parse('2026-06-21T20:00Z'));
    near(sunrise, '2026-06-21T12:42Z');
    near(sunset, '2026-06-22T03:05Z');
  });

  it('uses the Irvine date late in the evening', () => {
    // 22:00 PST on Dec 21 is Dec 22 in UTC, but still Dec 21 in Irvine.
    const { sunset } = sunTimes(Date.parse('2026-12-22T06:00Z'));
    near(sunset, '2026-12-22T00:48Z');
  });
});

describe('dayPhase', () => {
  it('names thresholds of -6° and +6°', () => {
    expect(NIGHT_BELOW * DEG).toBeCloseTo(-6);
    expect(DAY_ABOVE * DEG).toBeCloseTo(6);
  });

  it.each([
    ['2026-06-21T10:00Z', 'night'], // 03:00 PDT
    ['2026-06-21T12:45Z', 'dawn'], // 05:45 PDT, just after sunrise
    ['2026-06-21T19:00Z', 'day'], // 12:00 PDT
    ['2026-06-22T03:00Z', 'dusk'], // 20:00 PDT, just before sunset
  ])('%s is %s', (iso, phase) => {
    expect(dayPhase(Date.parse(iso))).toBe(phase);
  });
});

describe('module boundary', () => {
  it('imports neither three nor src/render', () => {
    const src = readFileSync(new URL('./sun.ts', import.meta.url), 'utf8');
    expect(src).not.toMatch(/from\s+['"]three/);
    expect(src).not.toMatch(/from\s+['"][./]*render/);
    expect(src).not.toMatch(/\bimport\b/);
  });
});
