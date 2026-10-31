import areas from '../../../shared/map/areas.json';
import { irvineTime, sunTimes } from '../core/sun';
import type { SharedClock } from '../state/clock';
import { areaMap, placeAt, type AreaMap } from '../state/location';
import type { Point } from '../state/shop';
import { hudLayout, place } from './hudLayout';

/** The gameplay areas from shared/map/areas.json, in metres. */
export const AREA_MAP: AreaMap = areaMap(areas);

const DAY_MS = 86_400_000;

export interface SkyMarker {
  body: 'sun' | 'moon';
  /** How far along the arc, 0 at the left (sunrise or sunset) to 1 at the right (sunset or sunrise). */
  t: number;
}

/** The sun between sunTimes sunrise and sunset, the moon otherwise; t is the share of that day or night gone. */
export function skyMarker(utcMs: number): SkyMarker {
  const { sunrise, sunset } = sunTimes(utcMs);
  let body: SkyMarker['body'], start: number, end: number;
  if (utcMs >= sunrise && utcMs < sunset) [body, start, end] = ['sun', sunrise, sunset];
  else if (utcMs < sunrise) [body, start, end] = ['moon', sunTimes(utcMs - DAY_MS).sunset, sunrise];
  else [body, start, end] = ['moon', sunset, sunTimes(utcMs + DAY_MS).sunrise];
  return { body, t: Math.min(1, Math.max(0, (utcMs - start) / (end - start))) };
}

/** Irvine local time as 24-hour HH:MM. */
export function formatIrvineTime(utcMs: number): string {
  const { hour, minute } = irvineTime(utcMs);
  return `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`;
}

/** The arc in SVG units: a half circle over the horizon line. */
export const ARC = { cx: 22, cy: 21, r: 17 } as const;

/** Where the marker sits on the arc for t (0 left, 0.5 top, 1 right). */
export function arcPoint(t: number): { x: number; y: number } {
  const a = Math.PI * t;
  return { x: ARC.cx - ARC.r * Math.cos(a), y: ARC.cy - ARC.r * Math.sin(a) };
}

export interface LocationPanelOptions {
  parent: HTMLElement;
  /** The local player's position in metres. */
  position: () => Point;
  clock: SharedClock;
  map?: AreaMap;
  viewport?: () => { width: number; height: number };
}

const SVG_NS = 'http://www.w3.org/2000/svg';

function svg<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number>): SVGElementTagNameMap[K] {
  const e = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  return e;
}

function div(cls: string): HTMLDivElement {
  const e = document.createElement('div');
  e.className = cls;
  return e;
}

/** Top-left panel: the Irvine time with the sun or moon on an arc, the area type and the place name (visual-style.md). */
export class LocationPanel {
  readonly root: HTMLDivElement;
  readonly time: HTMLDivElement;
  readonly type: HTMLDivElement;
  readonly name: HTMLDivElement;
  readonly marker: SVGGElement;
  private readonly map: AreaMap;
  private minute = NaN;
  /** The shown place as type:name ('' for none); null before the first update. */
  private placeKey: string | null = null;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: LocationPanelOptions) {
    injectStyle();
    this.map = opts.map ?? AREA_MAP;
    this.root = div('lush-panel lush-location');
    this.root.setAttribute('role', 'status');
    this.root.setAttribute('aria-label', 'Location');

    const sky = svg('svg', { class: 'lush-location-arc', viewBox: '0 0 44 24', 'aria-hidden': 'true' });
    sky.append(
      svg('path', { class: 'lush-location-path', d: `M${ARC.cx - ARC.r} ${ARC.cy} A${ARC.r} ${ARC.r} 0 0 1 ${ARC.cx + ARC.r} ${ARC.cy}` }),
      svg('line', { class: 'lush-location-horizon', x1: 1, y1: ARC.cy, x2: 43, y2: ARC.cy }),
    );
    this.marker = svg('g', { class: 'lush-location-marker' });
    sky.append(this.marker);
    this.time = div('lush-location-time');
    const row = div('lush-location-row');
    row.append(sky, this.time);
    this.type = div('lush-title lush-location-type');
    this.name = div('lush-location-name');
    this.root.append(row, this.type, this.name);

    opts.parent.append(this.root);
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.update();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.root, hudLayout(v.width, v.height).location);
  }

  /** Call once per tick; the DOM changes only when the minute or the place does. */
  update(): void {
    const now = this.opts.clock.now();
    const minute = Math.floor(now / 60_000);
    if (minute !== this.minute) {
      this.minute = minute;
      this.time.textContent = formatIrvineTime(now);
      this.drawMarker(skyMarker(now));
    }
    const p = placeAt(this.opts.position(), this.map);
    const key = p ? `${p.type}:${p.name}` : '';
    if (key === this.placeKey) return;
    this.placeKey = key;
    this.type.textContent = p?.type ?? '';
    this.name.textContent = p?.name ?? '';
    this.type.hidden = this.name.hidden = !p;
    const danger = p?.danger ?? false;
    this.root.classList.toggle('danger', danger);
    this.name.classList.toggle('lush-danger', danger);
    this.type.classList.toggle('lush-danger', danger);
  }

  dispose(): void {
    window.removeEventListener('resize', this.onResize);
    this.root.remove();
  }

  private drawMarker(m: SkyMarker): void {
    const { x, y } = arcPoint(m.t);
    this.marker.dataset.body = m.body;
    this.marker.dataset.t = m.t.toFixed(3);
    if (m.body === 'sun') {
      this.marker.replaceChildren(svg('circle', { class: 'lush-location-sun', cx: x, cy: y, r: 3 }));
    } else {
      // A crescent: the moon's disc with a panel-coloured disc over its right side.
      this.marker.replaceChildren(
        svg('circle', { class: 'lush-location-moon', cx: x, cy: y, r: 3 }),
        svg('circle', { class: 'lush-location-shade', cx: x + 1.6, cy: y - 1, r: 2.6 }),
      );
    }
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-location { box-sizing: border-box; z-index: 5; display: flex; flex-direction: column; justify-content: center; gap: 1px; padding: var(--lush-space-1) var(--lush-space-2); overflow: hidden; pointer-events: none; user-select: none; -webkit-user-select: none; }
.lush-location.danger { border: 1px solid var(--lush-danger); }
.lush-location-row { display: flex; align-items: center; gap: var(--lush-space-2); }
.lush-location-arc { flex: 0 0 auto; width: 44px; height: 24px; overflow: visible; }
.lush-location-path { fill: none; stroke: var(--lush-text-muted); stroke-width: 0.8; stroke-dasharray: 1 2; }
.lush-location-horizon { stroke: var(--lush-text-muted); stroke-width: 0.8; }
.lush-location-sun { fill: var(--lush-accent); }
.lush-location-moon { fill: #dfe6f2; }
.lush-location-shade { fill: var(--lush-panel-bg-solid); }
.lush-location-time { font-family: var(--lush-font-title); font-size: var(--lush-text-lg); letter-spacing: 0.08em; font-variant-numeric: tabular-nums; color: var(--lush-text); }
.lush-location-type { font-size: var(--lush-text-xs); white-space: nowrap; }
.lush-location-name { font-size: var(--lush-text-md); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.lush-location .lush-danger { color: var(--lush-danger); font-weight: 700; }
.lush-location [hidden] { display: none; }
`;
  document.head.append(s);
}
