import * as THREE from 'three';
import campusRaw from '../../../shared/map/campus.json?raw';
import { GREEN_GRASS, GREEN_PITCH, GREEN_WATER, GREEN_WOOD, parseCampus, type CampusData, type Green, type ParkNatureData } from './mapData';
import { buildLandmark, isLandmark } from './landmarks';
import { mulberry32 } from './rng';
import { GROUND_KINDS, GROUND_PLAZA, SurfaceBuilder, makeGroundMaterial } from './groundSurfaces';
import {
  DOOR_HEIGHT, DOOR_WIDTH, DOWN, UP, band, door, faces, fitWindows, framedWindow, offset, ring, slab, walls,
  type DoorLook, type Face, type WindowLook,
} from './facade';
import { DEFAULT_SKY } from './sky';
import type { SkyView } from './worldView';
import { mergeGeometries } from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import { pointInPolygon } from './nature';
import { bakeParkNature } from './parkNature';

const DM = 0.1;
const Y_GREEN = 0.01;
/** Height between stacked green layers, so overlapping polygons don't z-fight. */
const GREEN_LAYER_STEP = 0.005;
export const MAX_GREEN_LAYER = 8;
const Y_PATH = Y_GREEN + (MAX_GREEN_LAYER + 1) * GREEN_LAYER_STEP;
/** Ground surfaces never write depth, so they draw in this order: plazas, dirt, stone, then paved on top. */
const SURFACE_ORDER = [GROUND_PLAZA, 0, 1, 2];
const ROOF_RISE = 2.5;
const PITCHED_CHANCE = 0.4;
/** Pitched roofs overhang the walls by this much. */
const EAVE = 0.35;
/** Flat roofs get a cornice standing this far out. */
const CORNICE = 0.2;

/** Wall faces at least this long (metres) always get framed windows. */
export const MIN_WINDOW_FACE = 4;
/** Storey height used to count floors, in metres. */
export const STOREY = 3.5;
/** Share of windows drawn with their blinds down (dark at night). */
const BLINDS_CHANCE = 0.15;

export const WALL_KINDS = ['brick', 'concrete', 'stucco', 'wood', 'glass'] as const;
export type WallKind = (typeof WALL_KINDS)[number];

interface WallStyle {
  walls: number[];
  trim: number;
  window: WindowLook;
  door: DoorLook;
  /** Window spacing and widest pane, in metres. */
  spacing: number;
  pane: number;
  /** Bottom and top of the windows as a share of the storey height. */
  sill: number;
  head: number;
  plinth: boolean;
  pitched: boolean;
  /** Cumulative share of buildings up to and including this kind. */
  share: number;
}

const GLASS_PANE = 0x7d9db0;
const BLIND = 0x4a4640;

/** Flat colours per wall material (PRD 5.2): no textures, so the material shows in colour and trim. */
const STYLES: Record<WallKind, WallStyle> = {
  brick: {
    walls: [0xa4553f, 0x9a4b3a, 0xb06548], trim: 0xe6dccb,
    window: { frame: 0xf0ebe0, glass: GLASS_PANE, blind: BLIND, sill: 0xe6dccb },
    door: { frame: 0xf0ebe0, leaf: 0x3f5a4a, step: 0xb5ada0 },
    spacing: 3.6, pane: 1.2, sill: 0.28, head: 0.75, plinth: true, pitched: true, share: 0.25,
  },
  concrete: {
    walls: [0xcfcfc4, 0xbdb8ac, 0xd6d0c2], trim: 0xa39d90,
    window: { frame: 0x50555a, glass: GLASS_PANE, blind: BLIND, sill: 0xa39d90 },
    door: { frame: 0x50555a, leaf: 0x6f93a8, step: 0xa39d90 },
    spacing: 4, pane: 2, sill: 0.3, head: 0.78, plinth: true, pitched: false, share: 0.55,
  },
  stucco: {
    walls: [0xe0d5bd, 0xd9c8a8, 0xe8dcc2], trim: 0xb8a88f,
    window: { frame: 0x6b4f3a, glass: GLASS_PANE, blind: BLIND, sill: 0xb8a88f },
    door: { frame: 0x6b4f3a, leaf: 0x7a4a2e, step: 0xb8a88f },
    spacing: 3.6, pane: 1.2, sill: 0.28, head: 0.75, plinth: true, pitched: true, share: 0.8,
  },
  wood: {
    walls: [0x9c6b45, 0x8a5c3a, 0xa87850], trim: 0x5e3d26,
    window: { frame: 0xe8dcc2, glass: GLASS_PANE, blind: BLIND, sill: 0x5e3d26 },
    door: { frame: 0xe8dcc2, leaf: 0x5e3d26, step: 0x7a6a5c },
    spacing: 3.2, pane: 1, sill: 0.3, head: 0.75, plinth: false, pitched: true, share: 0.9,
  },
  glass: {
    walls: [0x4f6878, 0x55606a], trim: 0xc8ccd0,
    window: { frame: 0xc8ccd0, glass: GLASS_PANE, blind: 0x3d4a55 },
    door: { frame: 0xc8ccd0, leaf: 0x9cc3d6, step: 0xa39d90 },
    spacing: 3, pane: 2.6, sill: 0.1, head: 0.88, plinth: false, pitched: false, share: 1,
  },
};

/** Wall colours of each wall material. */
export const WALL_COLORS: Readonly<Record<WallKind, readonly number[]>> = {
  brick: STYLES.brick.walls,
  concrete: STYLES.concrete.walls,
  stucco: STYLES.stucco.walls,
  wood: STYLES.wood.walls,
  glass: STYLES.glass.walls,
};

/** The wall material for a building, from its id alone, so every client draws the same one. */
export function wallKind(id: number): WallKind {
  const x = mulberry32(id ^ 0x5bd1e995)();
  return WALL_KINDS.find((k) => x < STYLES[k].share) ?? 'concrete';
}

const ROOFS = [0x8c5a4a, 0x7a6a5c, 0x9a7b5f, 0x6f7a80];
const GREEN_COLOR: Record<number, number> = {
  [GREEN_GRASS]: 0x6aa84f,
  [GREEN_WOOD]: 0x3f7d3a,
  [GREEN_WATER]: 0x4a90c8,
  [GREEN_PITCH]: 0x8bc078,
};

/**
 * Accumulates flat-shaded, vertex-coloured triangles. Window glass goes into `glass` and is merged
 * into the same geometry with a `glow` attribute of 1, so it can light up at night (see
 * CampusMap.updateSky) without a draw call of its own.
 */
export class MeshBuilder {
  private posBuf = new Float32Array(9 * 64);
  private colBuf = new Float32Array(9 * 64);
  /** Floats used in posBuf and colBuf. */
  private used = 0;
  /** Framed windows and doors added through facade.ts. */
  windows = 0;
  doors = 0;
  private lastHex = -1;
  private r = 0;
  private g = 0;
  private b = 0;
  private glassB: MeshBuilder | null = null;

  /** Window glass, kept apart so it can be marked to glow at night. */
  get glass(): MeshBuilder {
    return (this.glassB ??= new MeshBuilder());
  }

  /** Whether any glass was added. */
  get hasGlass(): boolean {
    return (this.glassB?.triangles ?? 0) > 0;
  }

  /** Add a triangle, flipping it if its normal points against `want`. */
  tri(a: number[], b: number[], c: number[], want: number[], color: number): void {
    const ux = b[0] - a[0], uy = b[1] - a[1], uz = b[2] - a[2];
    const vx = c[0] - a[0], vy = c[1] - a[1], vz = c[2] - a[2];
    const nx = uy * vz - uz * vy, ny = uz * vx - ux * vz, nz = ux * vy - uy * vx;
    const flip = nx * want[0] + ny * want[1] + nz * want[2] < 0;
    if (color !== this.lastHex) {
      const k = TMP_COLOR.setHex(color);
      this.lastHex = color;
      this.r = k.r;
      this.g = k.g;
      this.b = k.b;
    }
    const p = flip ? c : b, q = flip ? b : c;
    if (this.used + 9 > this.posBuf.length) this.grow();
    const P = this.posBuf, C = this.colBuf, i = this.used;
    P[i] = a[0]; P[i + 1] = a[1]; P[i + 2] = a[2];
    P[i + 3] = p[0]; P[i + 4] = p[1]; P[i + 5] = p[2];
    P[i + 6] = q[0]; P[i + 7] = q[1]; P[i + 8] = q[2];
    for (let k = i; k < i + 9; k += 3) {
      C[k] = this.r;
      C[k + 1] = this.g;
      C[k + 2] = this.b;
    }
    this.used += 9;
  }

  private grow(): void {
    const pos = new Float32Array(this.posBuf.length * 2), col = new Float32Array(this.colBuf.length * 2);
    pos.set(this.posBuf);
    col.set(this.colBuf);
    this.posBuf = pos;
    this.colBuf = col;
  }

  /** Corner positions so far (x, y, z per corner), not counting the glass. */
  get pos(): Float32Array {
    return this.posBuf.subarray(0, this.used);
  }

  /** Corner colours so far, matching `pos`. */
  get col(): Float32Array {
    return this.colBuf.subarray(0, this.used);
  }

  /** Triangles including the glass. */
  get triangles(): number {
    return this.used / 9 + (this.glassB?.triangles ?? 0);
  }

  /**
   * The triangles, glass last. With glass (or `glow` set), a `glow` attribute marks the glass (1)
   * from the rest (0).
   */
  geometry(glow = this.hasGlass): THREE.BufferGeometry {
    const glass = glow ? this.glassB : null;
    const n = this.used, m = glass?.used ?? 0;
    const pos = new Float32Array(n + m), col = new Float32Array(n + m);
    pos.set(this.pos);
    col.set(this.col);
    if (glass) {
      pos.set(glass.pos, n);
      col.set(glass.col, n);
    }
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.BufferAttribute(pos, 3));
    g.setAttribute('color', new THREE.BufferAttribute(col, 3));
    g.setAttribute('normal', new THREE.BufferAttribute(flatNormals(pos), 3));
    if (glow) g.setAttribute('glow', new THREE.BufferAttribute(new Float32Array((n + m) / 3).fill(1, n / 3), 1));
    g.computeBoundingSphere();
    return g;
  }
}

/** One normal per triangle, repeated at its three corners (what computeVertexNormals gives unindexed, but faster). */
function flatNormals(pos: Float32Array): Float32Array {
  const out = new Float32Array(pos.length);
  for (let i = 0; i < pos.length; i += 9) {
    const ux = pos[i + 3] - pos[i], uy = pos[i + 4] - pos[i + 1], uz = pos[i + 5] - pos[i + 2];
    const vx = pos[i + 6] - pos[i], vy = pos[i + 7] - pos[i + 1], vz = pos[i + 8] - pos[i + 2];
    let nx = uy * vz - uz * vy, ny = uz * vx - ux * vz, nz = ux * vy - uy * vx;
    const l = Math.hypot(nx, ny, nz) || 1;
    nx /= l; ny /= l; nz /= l;
    for (let k = 0; k < 9; k += 3) {
      out[i + k] = nx;
      out[i + k + 1] = ny;
      out[i + k + 2] = nz;
    }
  }
  return out;
}

const TMP_COLOR = new THREE.Color();

function polygonPoints(p: number[]): THREE.Vector2[] {
  const pts: THREE.Vector2[] = [];
  for (let i = 0; i < p.length; i += 2) pts.push(new THREE.Vector2(p[i] * DM, p[i + 1] * DM));
  return pts;
}

function signedArea(pts: THREE.Vector2[]): number {
  let a = 0;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) a += pts[j].x * pts[i].y - pts[i].x * pts[j].y;
  return a / 2;
}

function fillPolygon(b: MeshBuilder, pts: THREE.Vector2[], y: number, color: number, want: number[] = UP): void {
  const tris = THREE.ShapeUtils.triangulateShape(pts, []);
  for (const [i, j, k] of tris) {
    b.tri([pts[i].x, y, pts[i].y], [pts[j].x, y, pts[j].y], [pts[k].x, y, pts[k].y], want, color);
  }
}

export interface FacadeDetail {
  kind: WallKind;
  /** Length and framed-window count of each wall face, in footprint order. */
  faces: { length: number; windows: number }[];
  doors: number;
}

/**
 * The generic building used for everything that isn't a hand-made landmark: walls in one of the
 * wall materials, framed windows on every storey, a door on the longest face, a plinth, and a
 * cornice or overhanging pitched roof.
 */
export function addBuilding(b: MeshBuilder, id: number, h: number, p: number[]): FacadeDetail {
  const rnd = mulberry32(id);
  const kind = wallKind(id);
  const style = STYLES[kind];
  const wall = style.walls[Math.floor(rnd() * style.walls.length)];
  const roof = ROOFS[Math.floor(rnd() * ROOFS.length)];
  const pitched = rnd() < PITCHED_CHANCE && style.pitched && p.length === 8;
  const r = ring(p, DM);
  const height = h * DM;
  const floors = Math.max(1, Math.round(height / STOREY));
  const fh = height / floors;
  walls(b, r, r.pts, 0, height, wall);
  if (style.plinth) {
    const out = offset(r, 0.05);
    const top = Math.min(0.45, fh * 0.12);
    walls(b, r, out, 0, top, style.trim);
    band(b, r.pts, out, top, UP, style.trim);
  }

  const fs = faces(r);
  const doorFace = fs.reduce((m, f, i) => (f.len > fs[m].len ? i : m), 0);
  const detail: FacadeDetail = { kind, faces: [], doors: 0 };
  const doorsBefore = b.doors;
  fs.forEach((f, i) => {
    let count = 0;
    for (let k = 0; k < floors; k++) {
      let spans: [number, number][] = [[0, f.len]];
      if (i === doorFace && k === 0) spans = addDoor(b, f, floors, fh, style);
      for (const [s0, s1] of spans) {
        const fit = fitWindows(s0, s1, style.spacing, style.pane);
        for (const s of fit.at) {
          framedWindow(b, f, s, fit.width, k * fh + fh * style.sill, k * fh + fh * style.head, style.window, rnd() >= BLINDS_CHANCE);
          count++;
        }
      }
    }
    detail.faces.push({ length: f.len, windows: count });
  });
  detail.doors = b.doors - doorsBefore;

  if (pitched) {
    const eave = offset(r, EAVE);
    band(b, r.pts, eave, height, DOWN, style.trim);
    walls(b, r, eave, height, height + 0.12, style.trim);
    const cx = (r.pts[0].x + r.pts[1].x + r.pts[2].x + r.pts[3].x) / 4;
    const cz = (r.pts[0].z + r.pts[1].z + r.pts[2].z + r.pts[3].z) / 4;
    const apex = [cx, height + 0.12 + ROOF_RISE, cz];
    for (let i = 0; i < 4; i++) {
      const a = eave[i], c = eave[(i + 1) % 4], n = r.normals[i];
      b.tri([a.x, height + 0.12, a.z], [c.x, height + 0.12, c.z], apex, [n.x, 1, n.z], roof);
    }
  } else {
    const out = offset(r, CORNICE);
    walls(b, r, out, height - 0.3, height + 0.1, style.trim);
    band(b, r.pts, out, height - 0.3, DOWN, style.trim);
    slab(b, out, height + 0.1, roof);
  }
  return detail;
}

/**
 * Add the door on the ground floor of face `f` and return the spans either side of it that are
 * left for windows. The door is centred, or moved to one end when a one-storey face would
 * otherwise have no room for a window.
 */
function addDoor(b: MeshBuilder, f: Face, floors: number, fh: number, style: WallStyle): [number, number][] {
  const w = Math.max(0.6, Math.min(DOOR_WIDTH, f.len - 0.8));
  const dh = Math.min(DOOR_HEIGHT, fh * 0.8);
  const gap = 0.15;
  const spansAt = (c: number): [number, number][] => [[0, c - w / 2 - gap], [c + w / 2 + gap, f.len]];
  let c = f.len / 2;
  if (floors === 1 && spansAt(c).every(([s0, s1]) => fitWindows(s0, s1, style.spacing, style.pane).at.length === 0)) {
    c = Math.min(f.len / 2, 0.5 + w / 2);
  }
  door(b, f, c, w, dh, style.door);
  return spansAt(c);
}

type Box = [number, number, number, number];

function bbox(p: number[]): Box {
  const b: Box = [Infinity, Infinity, -Infinity, -Infinity];
  for (let i = 0; i < p.length; i += 2) {
    b[0] = Math.min(b[0], p[i]); b[1] = Math.min(b[1], p[i + 1]);
    b[2] = Math.max(b[2], p[i]); b[3] = Math.max(b[3], p[i + 1]);
  }
  return b;
}

/**
 * Layer index per green polygon id: larger polygons lower, and any two whose bounding boxes
 * overlap on different layers (e.g. lawns inside the park). Capped at MAX_GREEN_LAYER.
 */
export function greenLayers(green: Green[]): Map<number, number> {
  const items = green.map((g) => ({ id: g.id, box: bbox(g.p), area: Math.abs(signedArea(polygonPoints(g.p))), layer: 0 }));
  items.sort((a, b) => b.area - a.area || a.id - b.id);
  const layers = new Map<number, number>();
  for (let i = 0; i < items.length; i++) {
    const x = items[i];
    for (let j = 0; j < i; j++) {
      const y = items[j];
      if (x.box[0] < y.box[2] && y.box[0] < x.box[2] && x.box[1] < y.box[3] && y.box[1] < x.box[3]) {
        x.layer = Math.max(x.layer, y.layer + 1);
      }
    }
    x.layer = Math.min(x.layer, MAX_GREEN_LAYER);
    layers.set(x.id, x.layer);
  }
  return layers;
}

export interface MapStats {
  chunks: number;
  triangles: number;
  /** Building ids drawn as generic extrusions. */
  extruded: number[];
  /** Building ids drawn as hand-made landmarks (see landmarks.ts). */
  landmarks: number[];
  /** Wall material, windows and doors of each generic building, by id. */
  facades: Map<number, FacadeDetail>;
  /** Aldrich Park nature placements baked into the chunks, per type (empty without parknature.json data). */
  nature: Map<string, number>;
}

/** Warm light from inside a lit window. */
export const WINDOW_GLOW = 0xffb45a;
/** Emissive intensity of window glass at night. */
export const WINDOW_GLOW_INTENSITY = 0.8;

/**
 * Patch a Lambert shader so vertices with `glow` 1 (window glass) emit the `windowGlow` colour:
 * glass is emissive at night while everything else in the chunk mesh stays as it was.
 */
export function addWindowGlow(shader: { uniforms: Record<string, THREE.IUniform>; vertexShader: string; fragmentShader: string }, glow: THREE.IUniform<THREE.Color>): void {
  shader.uniforms.windowGlow = glow;
  shader.vertexShader = 'attribute float glow;\nvarying float vGlow;\n' +
    shader.vertexShader.replace('#include <begin_vertex>', '#include <begin_vertex>\n\tvGlow = glow;');
  shader.fragmentShader = 'uniform vec3 windowGlow;\nvarying float vGlow;\n' +
    shader.fragmentShader.replace('#include <emissivemap_fragment>', '#include <emissivemap_fragment>\n\ttotalEmissiveRadiance += windowGlow * vGlow;');
}

interface Built {
  builders: Map<string, MeshBuilder>;
  surfaces: Map<string, SurfaceBuilder[]>;
  extruded: number[];
  landmarks: number[];
  facades: Map<number, FacadeDetail>;
}

/** Built chunks per campus data, which is never changed after loading: the map is static. */
const built = new WeakMap<CampusData, Built>();

/**
 * Triangles for every chunk of `data`, built once per data object and only read afterwards, so
 * each CampusMap (one per scene) makes its own geometry without building the buildings again.
 */
function buildChunks(data: CampusData): Built {
  const cached = built.get(data);
  if (cached) return cached;
  const chunkM = data.chunkSize;
  const { minX, minZ } = data.bounds;
  const builders = new Map<string, MeshBuilder>();
  const surfaces = new Map<string, SurfaceBuilder[]>();
  const keyAt = (xDm: number, zDm: number): string => {
    const cx = Math.floor((xDm - minX) * DM / chunkM);
    const cz = Math.floor((zDm - minZ) * DM / chunkM);
    return `${cx},${cz}`;
  };
  const at = (xDm: number, zDm: number): MeshBuilder => {
    const key = keyAt(xDm, zDm);
    let b = builders.get(key);
    if (!b) builders.set(key, (b = new MeshBuilder()));
    return b;
  };
  const surfaceAt = (xDm: number, zDm: number, kind: number): SurfaceBuilder => {
    const key = keyAt(xDm, zDm);
    if (!builders.has(key)) builders.set(key, new MeshBuilder());
    let s = surfaces.get(key);
    if (!s) surfaces.set(key, (s = GROUND_KINDS.map((k) => new SurfaceBuilder(k, Y_PATH + SURFACE_ORDER.indexOf(k) * 0.002))));
    return s[kind] ?? s[GROUND_KINDS[1]];
  };
  const centre = (p: number[]): [number, number] => {
    let x0 = Infinity, x1 = -Infinity, z0 = Infinity, z1 = -Infinity;
    for (let i = 0; i < p.length; i += 2) {
      x0 = Math.min(x0, p[i]); x1 = Math.max(x1, p[i]);
      z0 = Math.min(z0, p[i + 1]); z1 = Math.max(z1, p[i + 1]);
    }
    return [(x0 + x1) / 2, (z0 + z1) / 2];
  };

  const layers = greenLayers(data.green);
  for (const g of data.green) {
    const [cx, cz] = centre(g.p);
    const rnd = mulberry32(g.id);
    const shade = 0.92 + rnd() * 0.16;
    const color = new THREE.Color(GREEN_COLOR[g.k] ?? GREEN_COLOR[GREEN_GRASS]).multiplyScalar(shade).getHex();
    fillPolygon(at(cx, cz), polygonPoints(g.p), Y_GREEN + layers.get(g.id)! * GREEN_LAYER_STEP, color);
  }
  for (const pl of data.plazas) {
    const [cx, cz] = centre(pl.p);
    surfaceAt(cx, cz, GROUND_PLAZA).polygon(pl.p.map((v) => v * DM));
  }
  for (const w of data.paths) {
    const half = (w.w * DM) / 2;
    for (let i = 0; i + 3 < w.p.length; i += 2) {
      const [x0, z0, x1, z1] = [w.p[i], w.p[i + 1], w.p[i + 2], w.p[i + 3]];
      surfaceAt((x0 + x1) / 2, (z0 + z1) / 2, w.s)
        .segment(x0 * DM, z0 * DM, x1 * DM, z1 * DM, half, i === 0, i + 4 >= w.p.length);
    }
  }
  const extruded: number[] = [];
  const landmarks: number[] = [];
  const facades = new Map<number, FacadeDetail>();
  for (const bl of data.buildings) {
    const [cx, cz] = centre(bl.p);
    if (isLandmark(bl.id) && buildLandmark(at(cx, cz), bl, data)) {
      landmarks.push(bl.id);
    } else {
      facades.set(bl.id, addBuilding(at(cx, cz), bl.id, bl.h, bl.p));
      extruded.push(bl.id);
    }
  }

  const result = { builders, surfaces, extruded, landmarks, facades };
  built.set(data, result);
  return result;
}

interface BakedNature {
  /** Extra triangles per chunk key. */
  builders: Map<string, MeshBuilder>;
  counts: Map<string, number>;
}

/** Baked nature per nature data and campus data; both are never changed after loading. */
const baked = new WeakMap<ParkNatureData, WeakMap<CampusData, BakedNature>>();

/**
 * Aldrich Park nature (parknature.json) as extra triangles per chunk. Each placement joins the chunk of
 * the largest green area it stands on (the park's own polygon): that chunk has a merged mesh already, and
 * its bounds span the area, so the nature adds no draw call and no chunk, and is culled with the park.
 */
function bakeNature(data: CampusData, nature: ParkNatureData): BakedNature {
  let byCampus = baked.get(nature);
  if (!byCampus) baked.set(nature, (byCampus = new WeakMap()));
  const cached = byCampus.get(data);
  if (cached) return cached;
  const { minX, minZ } = data.bounds;
  const keyAt = (xDm: number, zDm: number): string =>
    `${Math.floor((xDm - minX) * DM / data.chunkSize)},${Math.floor((zDm - minZ) * DM / data.chunkSize)}`;
  const greens = data.green.map((g) => {
    const box = [Infinity, Infinity, -Infinity, -Infinity];
    let area = 0;
    for (let i = 0, j = g.p.length - 2; i < g.p.length; j = i, i += 2) {
      box[0] = Math.min(box[0], g.p[i]); box[1] = Math.min(box[1], g.p[i + 1]);
      box[2] = Math.max(box[2], g.p[i]); box[3] = Math.max(box[3], g.p[i + 1]);
      area += g.p[j] * g.p[i + 1] - g.p[i] * g.p[j + 1];
    }
    // The same chunk buildChunks puts the green polygon in: the one under its bounding-box centre.
    return { g, box, area: Math.abs(area), key: keyAt((box[0] + box[2]) / 2, (box[1] + box[3]) / 2) };
  }).sort((a, b) => b.area - a.area);
  const builders = new Map<string, MeshBuilder>();
  const counts = bakeParkNature(nature, (xDm, zDm) => {
    const hit = greens.find(({ g, box }) => xDm >= box[0] && xDm <= box[2] && zDm >= box[1] && zDm <= box[3] && pointInPolygon(g.p, xDm, zDm));
    const key = hit ? hit.key : keyAt(xDm, zDm);
    let b = builders.get(key);
    if (!b) builders.set(key, (b = new MeshBuilder()));
    return b;
  });
  const result = { builders, counts };
  byCampus.set(data, result);
  return result;
}

/** Finished geometry per builder of a built map, made the first time a CampusMap needs it. */
const finished = new WeakMap<object, THREE.BufferGeometry>();

/**
 * A new geometry for `builder` sharing the attributes of the first one made, so a second map (a
 * second scene) neither rebuilds nor copies them. The attributes are never changed after building.
 */
function shared(builder: object, make: () => THREE.BufferGeometry): THREE.BufferGeometry {
  let src = finished.get(builder);
  if (!src) finished.set(builder, (src = make()));
  const g = new THREE.BufferGeometry();
  for (const [name, attr] of Object.entries(src.attributes)) g.setAttribute(name, attr);
  g.setIndex(src.index);
  g.boundingSphere = src.boundingSphere?.clone() ?? null;
  g.boundingBox = src.boundingBox?.clone() ?? null;
  return g;
}

/** The campus drawn as one merged mesh per fixed-size chunk (frustum culling drops the far ones). */
export class CampusMap {
  readonly group = new THREE.Group();
  readonly stats: MapStats;
  readonly material = new THREE.MeshLambertMaterial({ vertexColors: true });
  /** Light added to every `glow` vertex (window glass): WINDOW_GLOW at night, black by day. */
  readonly windowGlow = new THREE.Color(0);
  /** One material per ground kind (GROUND_DIRT, GROUND_STONE, GROUND_PAVED, GROUND_PLAZA). */
  readonly surfaceMaterials = GROUND_KINDS.map((k) => makeGroundMaterial(k));

  /** `nature` (parknature.json) is baked into the chunk meshes, so the park's trees and rocks cost no extra draw calls. */
  constructor(data: CampusData, nature?: ParkNatureData) {
    this.group.name = 'campus-map';
    const glow = { value: this.windowGlow };
    this.material.onBeforeCompile = (shader) => addWindowGlow(shader, glow);
    this.material.customProgramCacheKey = () => 'campus-window-glow';
    const { builders, surfaces, extruded, landmarks, facades } = buildChunks(data);
    const extra = nature ? bakeNature(data, nature) : null;
    const keys = new Set([...builders.keys(), ...(extra?.builders.keys() ?? [])]);

    let triangles = 0;
    for (const key of [...keys].sort()) {
      const b = builders.get(key) ?? new MeshBuilder();
      const nb = extra?.builders.get(key);
      triangles += b.triangles + (nb?.triangles ?? 0);
      // Every chunk mesh carries `glow` (all 0 without windows) so the patched shader never reads a stale value.
      // A chunk with only ground surfaces has no merged mesh of its own, just a holder for them.
      const geometry = nb
        ? shared(nb, () => (b.triangles > 0 ? mergeGeometries([b.geometry(true), nb.geometry(true)])! : nb.geometry(true)))
        : b.triangles > 0 ? shared(b, () => b.geometry(true)) : null;
      const chunk = geometry ? new THREE.Mesh(geometry, this.material) : new THREE.Group();
      chunk.name = `chunk-${key}`;
      chunk.matrixAutoUpdate = false;
      chunk.castShadow = true;
      chunk.receiveShadow = true;
      for (const s of surfaces.get(key) ?? []) {
        if (s.triangles === 0) continue;
        triangles += s.triangles;
        const mesh = new THREE.Mesh(shared(s, () => s.geometry()), this.surfaceMaterials[s.kind]);
        mesh.name = `ground-${key}-${s.kind}`;
        mesh.matrixAutoUpdate = false;
        mesh.receiveShadow = true;
        mesh.renderOrder = SURFACE_ORDER.indexOf(s.kind) - SURFACE_ORDER.length;
        chunk.add(mesh);
      }
      this.group.add(chunk);
    }
    this.stats = { chunks: keys.size, triangles, extruded: [...extruded], landmarks: [...landmarks], facades: new Map(facades), nature: new Map(extra?.counts) };
  }

  /** Light the windows from sunset to sunrise, as the lamps (DEFAULT_SKY when `sky` is missing). */
  updateSky(sky: SkyView | undefined): void {
    const night = (sky ?? DEFAULT_SKY).elevation <= 0;
    this.windowGlow.setHex(night ? WINDOW_GLOW : 0).multiplyScalar(night ? WINDOW_GLOW_INTENSITY : 1);
  }

  dispose(): void {
    this.group.traverse((o) => (o as THREE.Mesh).geometry?.dispose());
    this.material.dispose();
    for (const m of this.surfaceMaterials) {
      m.map?.dispose();
      m.dispose();
    }
  }
}

let cached: CampusData | null = null;
/** The committed campus.json, parsed once. */
export function loadCampus(): CampusData {
  return (cached ??= parseCampus(campusRaw));
}
