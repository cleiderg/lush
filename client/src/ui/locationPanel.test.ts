// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest';
import { sunTimes } from '../core/sun';
import { SharedClock } from '../state/clock';
import type { Point } from '../state/shop';
import { hudLayout, inside, intersects } from './hudLayout';
import { arcPoint, ARC, formatIrvineTime, LocationPanel, skyMarker } from './locationPanel';
import { presetTime } from './timePanel';

const MIN = 60_000;
// 21 June 2026, midday in Irvine (PDT).
const SUMMER = Date.UTC(2026, 5, 21, 19, 0);
const { sunrise, sunset } = sunTimes(SUMMER);

let panel: LocationPanel | null = null;
afterEach(() => {
  panel?.dispose();
  panel = null;
});

function mount(pos: Point, utcMs: number, width = 390, height = 844): { panel: LocationPanel; move: (p: Point) => void; clock: SharedClock } {
  let at = pos;
  const clock = new SharedClock(() => 0);
  clock.override(utcMs);
  panel = new LocationPanel({ parent: document.body, position: () => at, clock, viewport: () => ({ width, height }) });
  return { panel, move: (p) => (at = p), clock };
}

describe('location panel layout (AC1)', () => {
  for (const [w, h] of [[390, 844], [1600, 1000]]) {
    it(`at ${w}×${h} sits top left, on screen, clear of the controls, toggles, HUD and lower strips`, () => {
      const l = hudLayout(w, h);
      const r = l.location;
      expect(inside(r, { x: 0, y: 0, w, h })).toBe(true);
      expect(r.w).toBeGreaterThan(100);
      expect(r.h).toBeGreaterThan(60);
      // Top left: in the left half of the screen and the top of the game view, at the game view's left edge.
      expect(r.x + r.w).toBeLessThanOrEqual(w / 2);
      expect(r.y).toBeLessThanOrEqual(8);
      expect(r.x).toBe(l.controls.x + 8);
      expect(r.y + r.h).toBeLessThanOrEqual(l.panel.y);
      for (const [name, o] of [['controls', l.controls], ['toggle', l.toggle], ['retro', l.retro], ['hud', l.hud],
        ['pvp', l.pvp], ['chat', l.chat]] as const) {
        expect(intersects(r, o), name).toBe(false);
      }
    });

    it(`at ${w}×${h} the panel element is placed there`, () => {
      const { panel } = mount({ x: 0, z: 0 }, SUMMER, w, h);
      const r = hudLayout(w, h).location;
      expect(panel.root.style.left).toBe(`${r.x}px`);
      expect(panel.root.style.top).toBe(`${r.y}px`);
      expect(panel.root.style.width).toBe(`${r.w}px`);
    });
  }
});

describe('location panel place (AC2)', () => {
  it('shows name and type for the area the player is in, the PvP zone in red', () => {
    const { panel, move } = mount({ x: -21.5, z: -75.5 }, SUMMER);
    expect(panel.name.textContent).toBe('Aldrich Park');
    expect(panel.type.textContent).toBe('Safe');
    expect(panel.root.classList.contains('danger')).toBe(false);

    move({ x: -21.5, z: -150 });
    panel.update();
    expect([panel.name.textContent, panel.type.textContent]).toEqual(['Aldrich Park', 'PvE']);

    move({ x: -280, z: 280 });
    panel.update();
    expect([panel.name.textContent, panel.type.textContent]).toEqual(['South fields', 'PvE']);

    move({ x: 300, z: -350 });
    panel.update();
    expect([panel.name.textContent, panel.type.textContent]).toEqual(['PvP zone', 'PvP']);
    expect(panel.name.classList.contains('lush-danger')).toBe(true);
    expect(panel.root.classList.contains('danger')).toBe(true);

    move({ x: -21.5, z: -75.5 });
    panel.update();
    expect(panel.name.classList.contains('lush-danger')).toBe(false);
    expect(panel.root.classList.contains('danger')).toBe(false);
  });

  it('shows no place name outside every area', () => {
    const { panel } = mount({ x: 0, z: 1000 }, SUMMER);
    expect(panel.name.textContent).toBe('');
    expect(panel.name.hidden).toBe(true);
    expect(panel.type.hidden).toBe(true);
  });
});

describe('location panel time (AC3)', () => {
  it('formats Irvine time as 24-hour HH:MM, daylight saving included', () => {
    expect(formatIrvineTime(Date.UTC(2026, 5, 21, 19, 5))).toBe('12:05');
    expect(formatIrvineTime(Date.UTC(2026, 0, 15, 7, 3))).toBe('23:03');
    expect(formatIrvineTime(Date.UTC(2026, 0, 15, 17, 7))).toBe('09:07');
  });

  it('noon: the sun at the top of the arc', () => {
    const noon = presetTime('noon', SUMMER);
    const m = skyMarker(noon);
    expect(m.body).toBe('sun');
    expect(m.t).toBeCloseTo(0.5, 2);
    expect(arcPoint(m.t).y).toBeCloseTo(ARC.cy - ARC.r, 1);
  });

  it('dusk: the sun low on the right before sunset, the moon rising on the left after', () => {
    const before = skyMarker(sunset - 15 * MIN);
    expect(before.body).toBe('sun');
    expect(before.t).toBeGreaterThan(0.95);
    expect(arcPoint(before.t).x).toBeGreaterThan(ARC.cx + ARC.r * 0.95);
    const after = skyMarker(sunset + 15 * MIN);
    expect(after.body).toBe('moon');
    expect(after.t).toBeLessThan(0.05);
    expect(arcPoint(after.t).x).toBeLessThan(ARC.cx - ARC.r * 0.95);
  });

  it('night: the moon, along the arc by how much of the night has gone', () => {
    const early = skyMarker(sunrise - 4 * 60 * MIN);
    expect(early.body).toBe('moon');
    expect(early.t).toBeGreaterThan(0.5);
    expect(early.t).toBeLessThan(1);
    const late = skyMarker(sunTimes(SUMMER).sunset + 4 * 60 * MIN);
    expect(late.body).toBe('moon');
    expect(late.t).toBeGreaterThan(0);
    expect(late.t).toBeLessThan(early.t);
    expect(skyMarker(sunrise).body).toBe('sun');
    expect(skyMarker(sunrise - MIN).body).toBe('moon');
  });

  it('the panel shows the shared clock and follows the ?debug override', () => {
    const { panel, clock } = mount({ x: 0, z: 0 }, presetTime('noon', SUMMER));
    expect(panel.time.textContent).toMatch(/^\d\d:\d\d$/);
    expect(panel.marker.dataset.body).toBe('sun');
    expect(Number(panel.marker.dataset.t)).toBeCloseTo(0.5, 2);

    clock.override(Date.UTC(2026, 5, 21, 6, 30)); // 23:30 PDT
    panel.update();
    expect(panel.time.textContent).toBe('23:30');
    expect(panel.marker.dataset.body).toBe('moon');
    expect(panel.marker.querySelectorAll('circle')).toHaveLength(2);

    clock.override(presetTime('dusk', SUMMER));
    panel.update();
    expect(panel.marker.dataset.body).toBe('sun');
    expect(Number(panel.marker.dataset.t)).toBeGreaterThan(0.95);
  });

  it('without an override the panel shows the server time', () => {
    const clock = new SharedClock(() => 1_000);
    clock.sync(Date.UTC(2026, 5, 21, 19, 42));
    panel = new LocationPanel({ parent: document.body, position: () => ({ x: 0, z: 0 }), clock, viewport: () => ({ width: 390, height: 844 }) });
    expect(panel.time.textContent).toBe('12:42');
  });
});
