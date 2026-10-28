import areas from '../../../shared/map/areas.json';
import type { Point } from '../state/shop';
import { polygonFromDecimetres, pvpStatus, type Polygon } from '../state/pvpZone';
import { hudLayout, place } from './hudLayout';

/** The PvP zone polygons from shared/map/areas.json, in metres. */
export const PVP_ZONES: Polygon[] = areas.areas.filter((a) => a.kind === 'pvp').map((a) => polygonFromDecimetres(a.p));

export interface PvpWarningOptions {
  parent: HTMLElement;
  /** The local player's position in metres. */
  position: () => Point;
  zones?: Polygon[];
  viewport?: () => { width: number; height: number };
}

const BASE = 'display:flex;align-items:center;justify-content:center;box-sizing:border-box;z-index:9;' +
  'border-radius:8px;pointer-events:none;font:600 15px system-ui,sans-serif;text-align:center;';

/**
 * Shows a warning banner while the local player is just outside a PvP zone and a "PvP zone"
 * indicator while they are inside one (docs/specs/map.md). Absent from the DOM otherwise.
 */
export class PvpWarning {
  private el: HTMLElement | null = null;
  private state: 'none' | 'warning' | 'inside' = 'none';
  private readonly zones: Polygon[];

  constructor(private readonly opts: PvpWarningOptions) {
    this.zones = opts.zones ?? PVP_ZONES;
    window.addEventListener('resize', () => this.layout());
  }

  layout(): void {
    if (!this.el) return;
    const vp = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.el, hudLayout(vp.width, vp.height).pvp);
  }

  /** Call once per tick. */
  update(): void {
    const s = pvpStatus(this.opts.position(), this.zones);
    const next = s.inside ? 'inside' : s.warning ? 'warning' : 'none';
    if (next === this.state) return;
    this.state = next;
    this.el?.remove();
    this.el = null;
    if (next === 'none') return;
    const el = document.createElement('div');
    if (next === 'warning') {
      el.id = 'pvp-warning';
      el.setAttribute('role', 'alert');
      el.textContent = 'Warning: PvP zone ahead. Other players can attack you there.';
      el.style.cssText = BASE + 'background:#c0392b;color:#fff;padding:4px 10px;';
    } else {
      el.id = 'pvp-indicator';
      el.setAttribute('role', 'status');
      el.textContent = 'PvP zone';
      el.style.cssText = BASE + 'background:rgba(20,0,0,0.75);color:#ff5040;border:2px solid #d01818;';
    }
    this.opts.parent.append(el);
    this.el = el;
    this.layout();
  }
}
