// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { GameState } from '../state/world';
import { hudLayout } from './hudLayout';
import { PLAYER_PORTRAIT_CSS, PlayerPortrait, RING_LENGTH } from './playerPortrait';
import { THEME } from './theme';

let portrait: PlayerPortrait | null = null;
beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  portrait?.dispose();
  portrait = null;
});

function stats(hp: number, maxHp: number, combatLevel: number) {
  return { type: 'playerStats' as const, hp, maxHp, combatLevel, xp: 0, gold: 0 };
}

function mount(g: GameState): PlayerPortrait {
  portrait = new PlayerPortrait({ parent: document.body, stats: () => g.items.stats, viewport: () => ({ width: 390, height: 844 }) });
  return portrait;
}

describe('PlayerPortrait', () => {
  it('fills the HP ring as hp / maxHp and shows the combat level, updating on each PlayerStats', () => {
    const g = new GameState();
    const p = mount(g);
    for (const [hp, fill] of [[40, 1], [20, 0.5], [0, 0]] as const) {
      g.handle(stats(hp, 40, 12), 0);
      p.update();
      expect(p.fill).toBe(fill);
      expect(Number(p.ring.getAttribute('stroke-dashoffset'))).toBeCloseTo(RING_LENGTH * (1 - fill), 1);
      expect(p.badge.hidden).toBe(false);
      expect(p.badge.textContent).toBe('12');
    }
    g.handle(stats(30, 40, 13), 0);
    p.update();
    expect(p.fill).toBe(0.75);
    expect(p.badge.textContent).toBe('13');
  });

  it('before any PlayerStats the ring is full and no level is shown', () => {
    const g = new GameState();
    let p!: PlayerPortrait;
    expect(() => { p = mount(g); p.update(); }).not.toThrow();
    expect(p.fill).toBe(1);
    expect(Number(p.ring.getAttribute('stroke-dashoffset'))).toBeCloseTo(0, 5);
    expect(p.badge.hidden).toBe(true);
    expect(p.badge.textContent).toBe('');
    portrait?.dispose();
    portrait = new PlayerPortrait({ parent: document.body, stats: () => null });
    expect(portrait.fill).toBe(1);
    expect(portrait.badge.hidden).toBe(true);
  });

  it('sits at the layout portrait rect', () => {
    const p = mount(new GameState());
    const r = hudLayout(390, 844).portrait;
    expect(p.root.style.left).toBe(`${r.x}px`);
    expect(p.root.style.top).toBe(`${r.y}px`);
    expect(p.root.style.width).toBe(`${r.w}px`);
  });

  it('is round with the shared theme gold border and panel colours', () => {
    const p = mount(new GameState());
    expect(p.root.classList.contains('lush-portrait')).toBe(true);
    const rule = PLAYER_PORTRAIT_CSS.match(/\.lush-portrait \{([^}]*)\}/)?.[1] ?? '';
    expect(rule).toMatch(/border: [^;]*var\(--lush-gold\)/);
    expect(rule).toContain('background: var(--lush-panel-bg)');
    expect(rule).toContain('border-radius: var(--lush-radius-round)');
    expect(PLAYER_PORTRAIT_CSS).toContain('var(--lush-border-strong)');
    for (const name of PLAYER_PORTRAIT_CSS.match(/--lush-[a-z0-9-]+/g) ?? []) expect(THEME[name], name).toBeTruthy();
    expect(document.head.textContent).toContain('.lush-portrait');
  });
});
