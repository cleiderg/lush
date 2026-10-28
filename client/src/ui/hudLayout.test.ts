import { describe, expect, it } from 'vitest';
import { hudLayout, inside, intersects } from './hudLayout';

describe('hudLayout', () => {
  for (const [w, h] of [[390, 844], [375, 667], [430, 932], [1280, 800]]) {
    it(`at ${w}×${h} keeps HUD, toggle, panel, shop and trade window on screen and off the controls`, () => {
      const l = hudLayout(w, h);
      const viewport = { x: 0, y: 0, w, h };
      expect(inside(l.controls, viewport)).toBe(true);
      for (const [name, r] of [['hud', l.hud], ['toggle', l.toggle], ['panel', l.panel], ['shop', l.shop], ['trade', l.trade],
        ['tradePrompt', l.tradePrompt], ['pvp', l.pvp]] as const) {
        expect(inside(r, viewport), name).toBe(true);
        expect(intersects(r, l.controls), name).toBe(false);
        expect(r.w, name).toBeGreaterThan(0);
        expect(r.h, name).toBeGreaterThan(0);
      }
      expect(intersects(l.hud, l.toggle)).toBe(false);
      expect(intersects(l.hud, l.panel)).toBe(false);
      expect(intersects(l.toggle, l.panel)).toBe(false);
      expect(intersects(l.hud, l.shop)).toBe(false);
      expect(intersects(l.toggle, l.shop)).toBe(false);
      for (const r of [l.trade, l.tradePrompt]) {
        expect(intersects(l.hud, r)).toBe(false);
        expect(intersects(l.toggle, r)).toBe(false);
      }
      expect(inside(l.tradePrompt, l.trade)).toBe(true);
      // The PvP warning / indicator stays clear of the HUD, the inventory toggle and every panel.
      for (const [name, r] of [['hud', l.hud], ['toggle', l.toggle], ['panel', l.panel], ['shop', l.shop], ['trade', l.trade],
        ['tradePrompt', l.tradePrompt]] as const) {
        expect(intersects(l.pvp, r), name).toBe(false);
      }
    });
  }

  for (const [w, h] of [[320, 568], [390, 844], [480, 900]]) {
    it(`at ${w}×${h} puts the chat log above the control tray, clear of the HUD, toggle and PvP strip`, () => {
      const l = hudLayout(w, h);
      expect(inside(l.chat, { x: 0, y: 0, w, h })).toBe(true);
      expect(l.chat.w).toBeGreaterThan(0);
      expect(l.chat.h).toBeGreaterThan(0);
      expect(l.chat.y + l.chat.h).toBeLessThanOrEqual(l.controls.y);
      expect(intersects(l.chat, l.controls)).toBe(false);
      for (const [name, r] of [['hud', l.hud], ['toggle', l.toggle], ['pvp', l.pvp]] as const) {
        expect(intersects(l.chat, r), name).toBe(false);
      }
    });
  }

  it('at 390×844 the control tray is the bottom 35% and the panel fills the space above it', () => {
    const l = hudLayout(390, 844);
    expect(l.controls).toEqual({ x: 0, y: 844 - 295, w: 390, h: 295 });
    expect(l.panel.y + l.panel.h).toBeLessThanOrEqual(l.controls.y);
    expect(l.panel.h).toBeGreaterThan(400);
  });

  it('intersects and inside', () => {
    const a = { x: 0, y: 0, w: 10, h: 10 };
    expect(intersects(a, { x: 10, y: 0, w: 5, h: 5 })).toBe(false);
    expect(intersects(a, { x: 9, y: 9, w: 5, h: 5 })).toBe(true);
    expect(inside({ x: 1, y: 1, w: 9, h: 9 }, a)).toBe(true);
    expect(inside({ x: 1, y: 1, w: 10, h: 9 }, a)).toBe(false);
  });
});
