// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { hudLayout, inside, intersects } from './hudLayout';
import { localRetroStore, RETRO_DEFAULT, RETRO_KEY, RetroToggle } from './retroToggle';

function fakeStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial));
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => { data.set(k, v); },
  });
  return data;
}

let toggle: RetroToggle | null = null;

function setup() {
  const setRetro = vi.fn<(enabled: boolean) => void>();
  toggle = new RetroToggle({
    parent: document.body,
    target: { setRetro },
    store: localRetroStore(),
    viewport: () => ({ width: 390, height: 844 }),
  });
  return { setRetro, toggle };
}

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  toggle?.dispose();
  toggle = null;
  vi.unstubAllGlobals();
});

describe('RetroToggle', () => {
  it('is off by default (Q2) when nothing is saved', () => {
    fakeStorage();
    expect(RETRO_DEFAULT).toBe(false);
    const { setRetro, toggle } = setup();
    expect(setRetro).toHaveBeenCalledTimes(1);
    expect(setRetro).toHaveBeenLastCalledWith(false);
    expect(toggle.enabled).toBe(false);
    expect(toggle.button.getAttribute('aria-pressed')).toBe('false');
  });

  it('applies a saved on at startup', () => {
    fakeStorage({ [RETRO_KEY]: '1' });
    const { setRetro, toggle } = setup();
    expect(setRetro).toHaveBeenLastCalledWith(true);
    expect(toggle.enabled).toBe(true);
    expect(toggle.button.classList.contains('on')).toBe(true);
  });

  it('applies a saved off at startup', () => {
    fakeStorage({ [RETRO_KEY]: '0' });
    const { setRetro, toggle } = setup();
    expect(setRetro).toHaveBeenLastCalledWith(false);
    expect(toggle.enabled).toBe(false);
  });

  it('tapping flips the setting, calls setRetro and saves the choice', () => {
    const data = fakeStorage();
    const { setRetro, toggle } = setup();
    toggle.button.click();
    expect(setRetro).toHaveBeenLastCalledWith(true);
    expect(toggle.enabled).toBe(true);
    expect(data.get(RETRO_KEY)).toBe('1');
    toggle.button.click();
    expect(setRetro).toHaveBeenLastCalledWith(false);
    expect(toggle.enabled).toBe(false);
    expect(data.get(RETRO_KEY)).toBe('0');
  });

  it('survives storage that throws, falling back to the default', () => {
    const boom = () => { throw new Error('SecurityError'); };
    vi.stubGlobal('localStorage', { getItem: boom, setItem: boom });
    const { setRetro, toggle } = setup();
    expect(setRetro).toHaveBeenLastCalledWith(RETRO_DEFAULT);
    expect(() => toggle.button.click()).not.toThrow();
    expect(setRetro).toHaveBeenLastCalledWith(!RETRO_DEFAULT);
  });

  it('is placed at its hudLayout rect', () => {
    fakeStorage();
    const { toggle } = setup();
    const r = hudLayout(390, 844).retro;
    expect(toggle.button.style.left).toBe(`${r.x}px`);
    expect(toggle.button.style.top).toBe(`${r.y}px`);
    expect(toggle.button.style.width).toBe(`${r.w}px`);
  });
});

describe('Retro toggle layout', () => {
  for (const [w, h] of [[320, 568], [375, 667], [390, 844], [430, 932], [1280, 800]]) {
    it(`at ${w}×${h} stays on screen, off the controls tray and clear of the other HUD pieces`, () => {
      const l = hudLayout(w, h);
      expect(inside(l.retro, { x: 0, y: 0, w, h })).toBe(true);
      expect(l.retro.w).toBeGreaterThanOrEqual(44);
      expect(l.retro.h).toBeGreaterThanOrEqual(44);
      // The tray holds the joystick and action buttons (or the chat keyboard).
      expect(intersects(l.retro, l.controls)).toBe(false);
      for (const [name, r] of [['hud', l.hud], ['toggle', l.toggle], ['panel', l.panel], ['shop', l.shop], ['trade', l.trade],
        ['cardTable', l.cardTable], ['tradePrompt', l.tradePrompt], ['pvp', l.pvp], ['chat', l.chat]] as const) {
        expect(intersects(l.retro, r), name).toBe(false);
      }
    });
  }
});
