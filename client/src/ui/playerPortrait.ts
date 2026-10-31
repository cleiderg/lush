import type { PlayerStatsState } from '../net/codec';
import { hudLayout, place } from './hudLayout';

export interface PlayerPortraitOptions {
  parent: HTMLElement;
  /** The local player's stats; null (or maxHp 0) before the first PlayerStats arrives. */
  stats: () => PlayerStatsState | null;
  viewport?: () => { width: number; height: number };
}

/** The HP ring in SVG units: a circle just inside the gold rim. */
export const RING = { c: 32, r: 29 } as const;
export const RING_LENGTH = 2 * Math.PI * RING.r;

/** The ring's share filled: hp / maxHp clamped to 0..1, full before any stats arrive. */
export function hpFill(stats: PlayerStatsState | null): number {
  if (!stats || stats.maxHp <= 0) return 1;
  return Math.min(1, Math.max(0, stats.hp / stats.maxHp));
}

/** The level badge text; empty before any stats arrive. */
export function levelText(stats: PlayerStatsState | null): string {
  return stats && stats.maxHp > 0 && stats.combatLevel > 0 ? String(stats.combatLevel) : '';
}

const SVG_NS = 'http://www.w3.org/2000/svg';

function svg<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number>): SVGElementTagNameMap[K] {
  const e = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  return e;
}

/** A flat-colour, big-headed face: skin, hair, eyes. */
function face(): SVGGElement {
  const g = svg('g', { class: 'lush-portrait-face' });
  g.append(
    svg('rect', { x: 18, y: 18, width: 28, height: 30, rx: 6, fill: '#e2b48a' }),
    svg('rect', { x: 16, y: 14, width: 32, height: 10, rx: 4, fill: '#5a3a24' }),
    svg('rect', { x: 24, y: 30, width: 4, height: 5, fill: '#2a1f18' }),
    svg('rect', { x: 36, y: 30, width: 4, height: 5, fill: '#2a1f18' }),
    svg('rect', { x: 28, y: 40, width: 8, height: 2, fill: '#9a5a44' }),
  );
  return g;
}

/** Top-left, under the location panel: the player's portrait with an HP ring and a combat level badge (visual-style.md, Portraits). */
export class PlayerPortrait {
  readonly root: HTMLDivElement;
  readonly ring: SVGCircleElement;
  readonly badge: HTMLDivElement;
  /** The shown fill and level as fill:level; null before the first update. */
  private shown: string | null = null;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: PlayerPortraitOptions) {
    injectStyle();
    this.root = document.createElement('div');
    this.root.className = 'lush-portrait';
    this.root.setAttribute('role', 'status');
    this.root.setAttribute('aria-label', 'You');
    const pic = svg('svg', { class: 'lush-portrait-svg', viewBox: '0 0 64 64', 'aria-hidden': 'true' });
    this.ring = svg('circle', {
      class: 'lush-portrait-ring', cx: RING.c, cy: RING.c, r: RING.r,
      'stroke-dasharray': RING_LENGTH.toFixed(2), transform: `rotate(-90 ${RING.c} ${RING.c})`,
    });
    pic.append(
      svg('circle', { class: 'lush-portrait-bg', cx: RING.c, cy: RING.c, r: RING.r }),
      face(),
      svg('circle', { class: 'lush-portrait-track', cx: RING.c, cy: RING.c, r: RING.r }),
      this.ring,
    );
    this.badge = document.createElement('div');
    this.badge.className = 'lush-portrait-badge';
    this.root.append(pic, this.badge);
    opts.parent.append(this.root);
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.update();
  }

  /** The ring's current fill, 0..1. */
  get fill(): number {
    return Number(this.ring.dataset.fill);
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.root, hudLayout(v.width, v.height).portrait);
  }

  /** Call once per tick; the DOM changes only when the fill or the level does. */
  update(): void {
    const s = this.opts.stats();
    const fill = hpFill(s);
    const level = levelText(s);
    const key = `${fill}:${level}`;
    if (key === this.shown) return;
    this.shown = key;
    this.ring.dataset.fill = String(fill);
    this.ring.setAttribute('stroke-dashoffset', (RING_LENGTH * (1 - fill)).toFixed(2));
    this.ring.classList.toggle('low', fill <= 0.25);
    this.badge.textContent = level;
    this.badge.hidden = level === '';
    this.root.setAttribute('aria-label', s && s.maxHp > 0 ? `You: ${s.hp} of ${s.maxHp} hitpoints, level ${level}` : 'You');
  }

  dispose(): void {
    window.removeEventListener('resize', this.onResize);
    this.root.remove();
  }
}

/** The portrait's styles: round, the theme's gold rim and panel colours. */
export const PLAYER_PORTRAIT_CSS = `
.lush-portrait { box-sizing: border-box; z-index: 4; border: 2px solid var(--lush-gold); border-radius: var(--lush-radius-round); background: var(--lush-panel-bg); box-shadow: var(--lush-shadow); pointer-events: none; user-select: none; -webkit-user-select: none; }
.lush-portrait-svg { display: block; width: 100%; height: 100%; }
.lush-portrait-bg { fill: var(--lush-surface); }
.lush-portrait-track { fill: none; stroke: var(--lush-panel-bg-solid); stroke-width: 4; }
.lush-portrait-ring { fill: none; stroke: var(--lush-active); stroke-width: 4; transition: stroke-dashoffset var(--lush-transition); }
.lush-portrait-ring.low { stroke: var(--lush-danger); }
.lush-portrait-badge { position: absolute; right: -6px; bottom: -4px; min-width: 22px; height: 22px; padding: 0 4px; box-sizing: border-box; border: var(--lush-border-strong); border-radius: 11px; background: var(--lush-panel-bg-solid); color: var(--lush-gold-bright); font-family: var(--lush-font-title); font-size: var(--lush-text-sm); line-height: 20px; text-align: center; }
.lush-portrait-badge[hidden] { display: none; }
`;

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = PLAYER_PORTRAIT_CSS;
  document.head.append(s);
}
