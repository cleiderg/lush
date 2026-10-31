import type areas from '../../../shared/map/areas.json';
import { distanceToOutline, pointInPolygon, polygonFromDecimetres, type Polygon } from '../state/pvpZone';
import type { Point } from '../state/shop';
import { hudLayout, place } from './hudLayout';
import { addPressFeedbackWithin, PanelMotion } from './theme';

/** How far around the player the minimap shows, in metres (PRD 13 placeholder). */
export const MINIMAP_RANGE_M = 60;
/** The minimap redraws which features it shows once the player has moved this far (metres). */
const REFRESH_M = 1;

export type MapFeatureKind = 'building' | 'path' | 'plaza' | 'pvp';

/** One drawable shape in metres: a closed polygon, or an open polyline for paths. */
export interface MapFeature {
  readonly kind: MapFeatureKind;
  readonly id: string;
  readonly pts: Polygon;
  /** Path width in metres; 0 for polygons. */
  readonly width: number;
  readonly minX: number;
  readonly minZ: number;
  readonly maxX: number;
  readonly maxZ: number;
}

export interface MapData {
  readonly features: readonly MapFeature[];
  /** The Campus Market stall, in metres. */
  readonly shop: Point;
  readonly bounds: { minX: number; minZ: number; maxX: number; maxZ: number };
}

export interface CampusJson {
  bounds: { minX: number; minZ: number; maxX: number; maxZ: number };
  buildings: { id: number; p: number[] }[];
  paths: { id: number; w: number; p: number[] }[];
  plazas?: { id: number; p: number[] }[];
}

function feature(kind: MapFeatureKind, id: string, p: number[], widthDm = 0): MapFeature {
  const pts = polygonFromDecimetres(p);
  let minX = Infinity, minZ = Infinity, maxX = -Infinity, maxZ = -Infinity;
  for (const [x, z] of pts) {
    minX = Math.min(minX, x);
    maxX = Math.max(maxX, x);
    minZ = Math.min(minZ, z);
    maxZ = Math.max(maxZ, z);
  }
  return { kind, id, pts, width: widthDm / 10, minX, minZ, maxX, maxZ };
}

/** Builds the map from campus.json and areas.json text/objects (decimetres) into metres. */
export function buildMapData(campus: CampusJson, gameAreas: typeof areas): MapData {
  const features: MapFeature[] = [
    ...(campus.plazas ?? []).map((a) => feature('plaza', `plaza-${a.id}`, a.p)),
    ...campus.paths.map((w) => feature('path', `path-${w.id}`, w.p, w.w)),
    ...campus.buildings.map((b) => feature('building', `building-${b.id}`, b.p)),
    ...gameAreas.areas.filter((a) => a.kind === 'pvp').map((a) => feature('pvp', a.id, a.p)),
  ];
  const b = campus.bounds;
  return {
    features,
    shop: { x: gameAreas.shop.x / 10, z: gameAreas.shop.z / 10 },
    bounds: { minX: b.minX / 10, minZ: b.minZ / 10, maxX: b.maxX / 10, maxZ: b.maxZ / 10 },
  };
}

function segmentDistance(p: Point, ax: number, az: number, bx: number, bz: number): number {
  const dx = bx - ax, dz = bz - az;
  const len2 = dx * dx + dz * dz;
  const t = len2 > 0 ? Math.max(0, Math.min(1, ((p.x - ax) * dx + (p.z - az) * dz) / len2)) : 0;
  return Math.hypot(p.x - (ax + t * dx), p.z - (az + t * dz));
}

/** Shortest distance from pos to the feature: 0 inside a polygon, the nearest edge otherwise. */
export function featureDistance(f: MapFeature, pos: Point): number {
  if (f.kind !== 'path') return pointInPolygon(pos, f.pts) ? 0 : distanceToOutline(pos, f.pts);
  let best = Infinity;
  for (let i = 1; i < f.pts.length; i++) {
    best = Math.min(best, segmentDistance(pos, f.pts[i - 1][0], f.pts[i - 1][1], f.pts[i][0], f.pts[i][1]));
  }
  return f.pts.length === 1 ? Math.hypot(pos.x - f.pts[0][0], pos.z - f.pts[0][1]) : best;
}

/** The features with any part within range metres of pos, in draw order (plazas, paths, buildings, PvP zone). */
export function featuresInRange(data: MapData, pos: Point, range = MINIMAP_RANGE_M): MapFeature[] {
  return data.features.filter((f) =>
    f.minX <= pos.x + range && f.maxX >= pos.x - range && f.minZ <= pos.z + range && f.maxZ >= pos.z - range
    && featureDistance(f, pos) <= range);
}

/**
 * How far the map turns, in radians clockwise on screen, for the camera rig's yaw (its position angle around the
 * player; it looks along (sin yaw, cos yaw)). The camera's view direction points up the minimap, so at yaw π
 * (looking north) the map is north-up.
 */
export function mapRotation(rigYaw: number): number {
  const r = (rigYaw + Math.PI) % (2 * Math.PI);
  return r < 0 ? r + 2 * Math.PI : r;
}

/** A world offset (metres, x east, z south) from the player to minimap screen units (x right, y down). */
export function toMinimap(dx: number, dz: number, rigYaw: number): { x: number; y: number } {
  const r = mapRotation(rigYaw);
  const c = Math.cos(r), s = Math.sin(r);
  return { x: dx * c - dz * s, y: dx * s + dz * c };
}

/** Where the N marker sits on the rim, in degrees clockwise from the top: always toward world north. */
export function northMarkerAngle(rigYaw: number): number {
  return (mapRotation(rigYaw) * 180) / Math.PI;
}

const SVG_NS = 'http://www.w3.org/2000/svg';

function svg<K extends keyof SVGElementTagNameMap>(tag: K, attrs: Record<string, string | number>): SVGElementTagNameMap[K] {
  const e = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, String(v));
  return e;
}

function pointList(pts: Polygon): string {
  return pts.map(([x, z]) => `${x.toFixed(1)},${z.toFixed(1)}`).join(' ');
}

/** One SVG shape for a feature, in world metres. */
function featureShape(f: MapFeature): SVGElement {
  const attrs = { class: `lush-map-${f.kind}`, 'data-id': f.id, points: pointList(f.pts) };
  if (f.kind === 'path') return svg('polyline', { ...attrs, 'stroke-width': Math.max(1.5, f.width).toFixed(1) });
  return svg('polygon', attrs);
}

/** The shop icon: a small market stall with an awning, centred on the origin, upright on screen. */
function shopIcon(scale: number): SVGGElement {
  const g = svg('g', { class: 'lush-map-shop' });
  const inner = svg('g', { transform: `scale(${scale})` });
  inner.append(
    svg('circle', { class: 'lush-map-shop-bg', cx: 0, cy: 0, r: 6 }),
    svg('rect', { class: 'lush-map-shop-awning', x: -4, y: -3.5, width: 8, height: 3 }),
    svg('rect', { class: 'lush-map-shop-body', x: -3, y: -0.5, width: 6, height: 4 }),
  );
  g.append(inner);
  return g;
}

function playerMarker(r: number): SVGGElement {
  const g = svg('g', { class: 'lush-map-player' });
  g.append(svg('circle', { cx: 0, cy: 0, r }));
  return g;
}

export interface MinimapOptions {
  parent: HTMLElement;
  /** The local player's position in metres. */
  position: () => Point;
  /** The camera rig's yaw in radians (renderer.world.rig.yaw). */
  yaw: () => number;
  /** Other players' positions in metres. */
  remotes: () => readonly { id: number; x: number; z: number }[];
  /** Tapped: open the big map. */
  onOpen?: () => void;
  /** The map to draw (campusMapData() in the game). */
  data: MapData;
  range?: number;
  viewport?: () => { width: number; height: number };
}

/**
 * Top-right round minimap (visual-style.md, Minimap): a gold rim and an N marker, the paths, buildings and PvP zone
 * (in red) within MINIMAP_RANGE_M, the shop icon, the player at the centre and other players as dots. It turns with
 * the camera. Tapping it opens the big map.
 */
export class Minimap {
  readonly root: HTMLButtonElement;
  readonly svg: SVGSVGElement;
  /** World-space layer: translated to the player and rotated with the camera. */
  readonly world: SVGGElement;
  readonly features: SVGGElement;
  readonly dots: SVGGElement;
  readonly shop: SVGGElement;
  readonly north: SVGGElement;
  private readonly data: MapData;
  private readonly range: number;
  private shownAt: Point | null = null;
  private shownKey = '';
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: MinimapOptions) {
    injectStyle();
    this.data = opts.data;
    this.range = opts.range ?? MINIMAP_RANGE_M;
    const r = this.range;
    this.root = document.createElement('button');
    this.root.type = 'button';
    this.root.className = 'lush-minimap';
    this.root.setAttribute('aria-label', 'Open map');
    this.root.addEventListener('click', () => opts.onOpen?.());
    // The rim's radius is r; the N marker sits on it, so the view box leaves room around it.
    const pad = r * 0.12;
    this.svg = svg('svg', { class: 'lush-minimap-svg', viewBox: `${-r - pad} ${-r - pad} ${2 * (r + pad)} ${2 * (r + pad)}`, 'aria-hidden': 'true' });
    const clipId = `lush-minimap-clip-${Math.random().toString(36).slice(2, 8)}`;
    const defs = svg('defs', {});
    const clip = svg('clipPath', { id: clipId });
    clip.append(svg('circle', { cx: 0, cy: 0, r }));
    defs.append(clip);
    const disc = svg('g', { 'clip-path': `url(#${clipId})` });
    this.world = svg('g', { class: 'lush-minimap-world' });
    this.features = svg('g', {});
    this.dots = svg('g', {});
    this.world.append(this.features, this.dots);
    this.shop = shopIcon(r / 40);
    disc.append(svg('circle', { class: 'lush-minimap-ground', cx: 0, cy: 0, r }), this.world, this.shop, playerMarker(r / 24));
    this.north = svg('g', { class: 'lush-minimap-north' });
    const n = svg('text', { x: 0, y: -r, 'text-anchor': 'middle', 'dominant-baseline': 'central', 'font-size': r * 0.2 });
    n.textContent = 'N';
    this.north.append(svg('circle', { cx: 0, cy: -r, r: r * 0.13 }), n);
    this.svg.append(defs, disc, svg('circle', { class: 'lush-minimap-rim', cx: 0, cy: 0, r }), this.north);
    this.root.append(this.svg);
    opts.parent.append(this.root);
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.update();
  }

  /** The N marker's angle on the rim, degrees clockwise from the top. */
  get northAngle(): number {
    return Number(this.north.dataset.angle);
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.root, hudLayout(v.width, v.height).minimap);
  }

  /** Call once per tick. */
  update(): void {
    const pos = this.opts.position();
    const yaw = this.opts.yaw();
    const deg = northMarkerAngle(yaw);
    this.world.setAttribute('transform', `rotate(${deg.toFixed(2)}) translate(${(-pos.x).toFixed(2)} ${(-pos.z).toFixed(2)})`);
    this.north.setAttribute('transform', `rotate(${deg.toFixed(2)})`);
    this.north.dataset.angle = deg.toFixed(2);
    // The N letter stays upright as it moves round the rim.
    this.north.querySelector('text')?.setAttribute('transform', `rotate(${(-deg).toFixed(2)} 0 ${-this.range})`);

    if (!this.shownAt || Math.hypot(pos.x - this.shownAt.x, pos.z - this.shownAt.z) >= REFRESH_M) {
      this.shownAt = { x: pos.x, z: pos.z };
      const shown = featuresInRange(this.data, pos, this.range);
      const key = shown.map((f) => f.id).join(',');
      if (key !== this.shownKey) {
        this.shownKey = key;
        this.features.replaceChildren(...shown.map(featureShape));
      }
    }

    const shop = toMinimap(this.data.shop.x - pos.x, this.data.shop.z - pos.z, yaw);
    const shopIn = Math.hypot(shop.x, shop.y) <= this.range;
    this.shop.style.display = shopIn ? '' : 'none';
    if (shopIn) this.shop.setAttribute('transform', `translate(${shop.x.toFixed(2)} ${shop.y.toFixed(2)})`);

    const dotR = (this.range / 30).toFixed(2);
    const remotes = this.opts.remotes().filter((p) => Math.hypot(p.x - pos.x, p.z - pos.z) <= this.range);
    while (this.dots.childElementCount > remotes.length) this.dots.lastElementChild?.remove();
    while (this.dots.childElementCount < remotes.length) this.dots.append(svg('circle', { class: 'lush-map-remote', r: dotR }));
    remotes.forEach((p, i) => {
      const c = this.dots.children[i];
      c.setAttribute('cx', p.x.toFixed(2));
      c.setAttribute('cy', p.z.toFixed(2));
      c.setAttribute('data-id', String(p.id));
    });
  }

  dispose(): void {
    window.removeEventListener('resize', this.onResize);
    this.root.remove();
  }
}

export interface BigMapOptions {
  parent: HTMLElement;
  position: () => Point;
  remotes: () => readonly { id: number; x: number; z: number }[];
  data: MapData;
  viewport?: () => { width: number; height: number };
  /** Called when the panel opens, so other panels in its place can close. */
  onOpen?: () => void;
}

/** The big map panel, opened from the minimap: the whole campus north-up in the shared panel theme. */
export class BigMap {
  readonly panel: HTMLElement;
  readonly closeButton: HTMLButtonElement;
  svg: SVGSVGElement | null = null;
  private dots: SVGGElement | null = null;
  private player: SVGGElement | null = null;
  private readonly data: MapData;
  private readonly motion: PanelMotion;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: BigMapOptions) {
    injectStyle();
    this.data = opts.data;
    this.panel = document.createElement('div');
    this.panel.className = 'lush-panel lush-bigmap';
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Campus map');
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    const head = document.createElement('div');
    head.className = 'lush-bigmap-head';
    const title = document.createElement('span');
    title.className = 'lush-title lush-bigmap-title';
    title.textContent = 'Campus map';
    this.closeButton = document.createElement('button');
    this.closeButton.type = 'button';
    this.closeButton.className = 'lush-bigmap-close';
    this.closeButton.textContent = 'Close';
    this.closeButton.addEventListener('click', () => this.close());
    head.append(title, this.closeButton);
    this.panel.append(head);
    opts.parent.append(this.panel);
    window.addEventListener('resize', this.onResize);
    this.layout();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  open(): void {
    if (this.isOpen) return;
    // The whole campus is drawn once, the first time the map opens.
    if (!this.svg) this.build();
    this.motion.set(true);
    this.opts.onOpen?.();
    this.update();
  }

  close(): void {
    this.motion.set(false);
  }

  toggle(): void {
    if (this.isOpen) this.close();
    else this.open();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.panel, hudLayout(v.width, v.height).map);
  }

  /** Call once per tick: moves the player marker and the other players' dots while open. */
  update(): void {
    if (!this.isOpen || !this.player || !this.dots) return;
    const pos = this.opts.position();
    this.player.setAttribute('transform', `translate(${pos.x.toFixed(1)} ${pos.z.toFixed(1)})`);
    const remotes = this.opts.remotes();
    while (this.dots.childElementCount > remotes.length) this.dots.lastElementChild?.remove();
    while (this.dots.childElementCount < remotes.length) this.dots.append(svg('circle', { class: 'lush-map-remote', r: 6 }));
    remotes.forEach((p, i) => {
      const c = this.dots!.children[i];
      c.setAttribute('cx', p.x.toFixed(1));
      c.setAttribute('cy', p.z.toFixed(1));
      c.setAttribute('data-id', String(p.id));
    });
  }

  dispose(): void {
    this.motion.dispose();
    window.removeEventListener('resize', this.onResize);
    this.panel.remove();
  }

  private build(): void {
    const b = this.data.bounds;
    const s = svg('svg', {
      class: 'lush-bigmap-svg', viewBox: `${b.minX} ${b.minZ} ${b.maxX - b.minX} ${b.maxZ - b.minZ}`,
      preserveAspectRatio: 'xMidYMid meet', 'aria-hidden': 'true',
    });
    const features = svg('g', {});
    features.append(...this.data.features.map(featureShape));
    const shop = shopIcon(2.5);
    shop.setAttribute('transform', `translate(${this.data.shop.x} ${this.data.shop.z})`);
    this.dots = svg('g', {});
    this.player = playerMarker(8);
    const north = svg('text', {
      class: 'lush-bigmap-north', x: b.maxX - 40, y: b.minZ + 60, 'text-anchor': 'middle', 'font-size': 60,
    });
    north.textContent = 'N ↑';
    s.append(svg('rect', { class: 'lush-minimap-ground', x: b.minX, y: b.minZ, width: b.maxX - b.minX, height: b.maxZ - b.minZ }),
      features, shop, this.dots, this.player, north);
    this.svg = s;
    this.panel.append(s);
  }
}

/** Minimap and big map styles: the theme's gold rim and panel colours, the PvP zone in the danger red. */
export const MINIMAP_CSS = `
.lush-minimap { box-sizing: border-box; z-index: 4; padding: 0; border: 0; border-radius: var(--lush-radius-round); background: none; cursor: pointer; touch-action: manipulation; -webkit-tap-highlight-color: transparent; user-select: none; -webkit-user-select: none; filter: drop-shadow(0 4px 8px rgba(0, 0, 0, 0.45)); transition: transform var(--lush-transition); }
.lush-minimap.pressed, .lush-minimap:active { transform: scale(0.96); }
.lush-minimap-svg { display: block; width: 100%; height: 100%; overflow: visible; }
.lush-minimap-ground { fill: #3f5a2c; }
.lush-minimap-rim { fill: none; stroke: var(--lush-gold); stroke-width: 4; }
.lush-minimap-north circle { fill: var(--lush-panel-bg-solid); stroke: var(--lush-gold); stroke-width: 1.5; }
.lush-minimap-north text { fill: var(--lush-gold-bright); font-family: var(--lush-font-title); font-weight: 700; }
.lush-map-plaza { fill: #9c917c; }
.lush-map-path { fill: none; stroke: #c8b48a; stroke-linecap: round; stroke-linejoin: round; }
.lush-map-building { fill: #8a6f55; stroke: #2e241b; stroke-width: 0.6; }
.lush-map-pvp { fill: rgba(229, 83, 61, 0.28); stroke: var(--lush-danger); stroke-width: 1.5; }
.lush-map-remote { fill: #f2e14c; stroke: #2a1f18; stroke-width: 0.5; }
.lush-map-player circle { fill: #ffffff; stroke: #2a1f18; stroke-width: 0.8; }
.lush-map-shop-bg { fill: var(--lush-panel-bg-solid); stroke: var(--lush-gold); stroke-width: 1; }
.lush-map-shop-awning { fill: var(--lush-accent); }
.lush-map-shop-body { fill: var(--lush-gold-bright); }
.lush-bigmap { box-sizing: border-box; z-index: 6; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); user-select: none; -webkit-user-select: none; }
.lush-bigmap[hidden] { display: none; }
.lush-bigmap-head { display: flex; align-items: center; justify-content: space-between; gap: var(--lush-space-2); }
.lush-bigmap-title { font-size: var(--lush-text-lg); }
.lush-bigmap-svg { flex: 1; min-height: 0; width: 100%; border: var(--lush-border-strong); border-radius: var(--lush-radius-sm); }
.lush-bigmap .lush-map-path { stroke-width: 4; }
.lush-bigmap .lush-map-pvp { stroke-width: 4; }
.lush-bigmap-north { fill: var(--lush-gold-bright); font-family: var(--lush-font-title); font-weight: 700; }
`;

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = MINIMAP_CSS;
  document.head.append(s);
}
