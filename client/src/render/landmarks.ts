import * as THREE from 'three';
import type { MeshBuilder } from './campusMap';
import type { Building, CampusData } from './mapData';

/**
 * Hand-made low-poly landmarks (docs/specs/map.md, Landmarks). Each one is built in code on its
 * OSM footprint from campus.json and replaces that building's generic extrusion. Visual only:
 * collision still comes from the footprint. No logos or mascot (PRD 9).
 */

export const LANGSON_PODIUM = 171169252;
export const LANGSON_TOP = 1374185239;
export const ALDRICH_HALL = 62646030;
export const STUDENT_CENTER = 157979894;

export const LANDMARKS: Readonly<Record<number, string>> = {
  [LANGSON_PODIUM]: 'Langson Library',
  [LANGSON_TOP]: 'Langson Library (top storey)',
  [ALDRICH_HALL]: 'Aldrich Hall',
  [STUDENT_CENTER]: 'Student Center',
};

export const LANDMARK_IDS: readonly number[] = Object.keys(LANDMARKS).map(Number);

export function isLandmark(id: number): boolean {
  return id in LANDMARKS;
}

const DM = 0.1;
const UP = [0, 1, 0];
const DOWN = [0, -1, 0];

type P = { x: number; z: number };

interface Ring {
  pts: P[];
  /** Outward unit normal of edge i (from pts[i] to pts[i + 1]). */
  normals: P[];
}

function ring(p: number[]): Ring {
  const pts: P[] = [];
  for (let i = 0; i < p.length; i += 2) pts.push({ x: p[i] * DM, z: p[i + 1] * DM });
  let area = 0;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) area += pts[j].x * pts[i].z - pts[i].x * pts[j].z;
  const ccw = area > 0;
  const normals = pts.map((a, i) => {
    const c = pts[(i + 1) % pts.length];
    const dx = c.x - a.x, dz = c.z - a.z;
    const l = Math.hypot(dx, dz) || 1;
    return ccw ? { x: dz / l, z: -dx / l } : { x: -dz / l, z: dx / l };
  });
  return { pts, normals };
}

/** The outline moved `d` metres outward (negative: inward), mitred at the corners. */
function offset(r: Ring, d: number): P[] {
  const n = r.pts.length;
  return r.pts.map((p, i) => {
    const a = r.normals[(i + n - 1) % n], b = r.normals[i];
    const k = d / Math.max(0.25, 1 + a.x * b.x + a.z * b.z);
    return { x: p.x + (a.x + b.x) * k, z: p.z + (a.z + b.z) * k };
  });
}

function quad(b: MeshBuilder, a: number[], c: number[], d: number[], e: number[], want: number[], color: number): void {
  b.tri(a, c, d, want, color);
  b.tri(a, d, e, want, color);
}

/** Walls along a closed outline from y0 to y1, facing the ring's outward normals. */
function walls(b: MeshBuilder, r: Ring, pts: P[], y0: number, y1: number, color: number): void {
  for (let i = 0; i < pts.length; i++) {
    const a = pts[i], c = pts[(i + 1) % pts.length], n = r.normals[i];
    quad(b, [a.x, y0, a.z], [c.x, y0, c.z], [c.x, y1, c.z], [a.x, y1, a.z], [n.x, 0, n.z], color);
  }
}

/** A flat polygon at height y. */
function slab(b: MeshBuilder, pts: P[], y: number, color: number, want: number[] = UP): void {
  const v = pts.map((p) => new THREE.Vector2(p.x, p.z));
  for (const [i, j, k] of THREE.ShapeUtils.triangulateShape(v, [])) {
    b.tri([v[i].x, y, v[i].y], [v[j].x, y, v[j].y], [v[k].x, y, v[k].y], want, color);
  }
}

/** The flat band between two outlines with matching vertices (a ledge or soffit). */
function band(b: MeshBuilder, inner: P[], outer: P[], y: number, want: number[], color: number): void {
  for (let i = 0; i < inner.length; i++) {
    const j = (i + 1) % inner.length;
    quad(b, [inner[i].x, y, inner[i].z], [inner[j].x, y, inner[j].z], [outer[j].x, y, outer[j].z], [outer[i].x, y, outer[i].z], want, color);
  }
}

/**
 * A box of half-size hu (along unit u) by hv (across), from y0 to y1, centred on (cx, cz).
 * The bottom face is only added when `bottom` is set.
 */
function box(b: MeshBuilder, cx: number, cz: number, u: P, hu: number, hv: number, y0: number, y1: number, color: number, bottom = false): void {
  const v = { x: -u.z, z: u.x };
  const at = (su: number, sv: number, y: number) => [cx + u.x * su * hu + v.x * sv * hv, y, cz + u.z * su * hu + v.z * sv * hv];
  quad(b, at(-1, -1, y1), at(1, -1, y1), at(1, 1, y1), at(-1, 1, y1), UP, color);
  if (bottom) quad(b, at(-1, -1, y0), at(1, -1, y0), at(1, 1, y0), at(-1, 1, y0), DOWN, color);
  quad(b, at(1, -1, y0), at(1, 1, y0), at(1, 1, y1), at(1, -1, y1), [u.x, 0, u.z], color);
  quad(b, at(-1, -1, y0), at(-1, 1, y0), at(-1, 1, y1), at(-1, -1, y1), [-u.x, 0, -u.z], color);
  quad(b, at(-1, 1, y0), at(1, 1, y0), at(1, 1, y1), at(-1, 1, y1), [v.x, 0, v.z], color);
  quad(b, at(-1, -1, y0), at(1, -1, y0), at(1, -1, y1), at(-1, -1, y1), [-v.x, 0, -v.z], color);
}

/** A square pyramid (skylight or roof light) of half-size h, from y0 rising to y0 + rise. */
function pyramid(b: MeshBuilder, cx: number, cz: number, u: P, h: number, y0: number, rise: number, color: number): void {
  const v = { x: -u.z, z: u.x };
  const c = (su: number, sv: number) => [cx + (u.x * su + v.x * sv) * h, y0, cz + (u.z * su + v.z * sv) * h];
  const apex = [cx, y0 + rise, cz];
  const corners = [c(-1, -1), c(1, -1), c(1, 1), c(-1, 1)];
  for (let i = 0; i < 4; i++) {
    const a = corners[i], d = corners[(i + 1) % 4];
    b.tri(a, d, apex, [(a[0] + d[0]) / 2 - cx, h, (a[2] + d[2]) / 2 - cz], color);
  }
}

interface Edge { a: P; t: P; n: P; len: number }

function edges(r: Ring): Edge[] {
  return r.pts.map((a, i) => {
    const c = r.pts[(i + 1) % r.pts.length];
    const len = Math.hypot(c.x - a.x, c.z - a.z);
    return { a, t: { x: (c.x - a.x) / (len || 1), z: (c.z - a.z) / (len || 1) }, n: r.normals[i], len };
  });
}

/** Evenly spaced positions along an edge, keeping `margin` clear of both corners. */
function along(len: number, spacing: number, margin: number): number[] {
  const count = Math.floor((len - 2 * margin) / spacing) + 1;
  if (len - 2 * margin < 0 || count < 1) return [];
  const start = (len - (count - 1) * spacing) / 2;
  return Array.from({ length: count }, (_, i) => start + i * spacing);
}

/** Window panes standing just proud of the wall, one row from y0 to y1. */
function windows(b: MeshBuilder, r: Ring, y0: number, y1: number, spacing: number, width: number, color: number): void {
  const off = 0.06;
  for (const e of edges(r)) {
    for (const s of along(e.len, spacing, width / 2 + 0.6)) {
      const p0 = { x: e.a.x + e.t.x * (s - width / 2) + e.n.x * off, z: e.a.z + e.t.z * (s - width / 2) + e.n.z * off };
      const p1 = { x: p0.x + e.t.x * width, z: p0.z + e.t.z * width };
      quad(b, [p0.x, y0, p0.z], [p1.x, y0, p1.z], [p1.x, y1, p1.z], [p0.x, y1, p0.z], [e.n.x, 0, e.n.z], color);
    }
  }
}

/** One long glazed ribbon per edge, from y0 to y1. */
function ribbons(b: MeshBuilder, r: Ring, y0: number, y1: number, margin: number, color: number): void {
  const off = 0.06;
  for (const e of edges(r)) {
    if (e.len < 2 * margin + 1) continue;
    const p0 = { x: e.a.x + e.t.x * margin + e.n.x * off, z: e.a.z + e.t.z * margin + e.n.z * off };
    const p1 = { x: p0.x + e.t.x * (e.len - 2 * margin), z: p0.z + e.t.z * (e.len - 2 * margin) };
    quad(b, [p0.x, y0, p0.z], [p1.x, y0, p1.z], [p1.x, y1, p1.z], [p0.x, y1, p0.z], [e.n.x, 0, e.n.z], color);
  }
}

/** Vertical fins or pilasters standing `depth` out from the wall. */
function fins(b: MeshBuilder, r: Ring, y0: number, y1: number, spacing: number, depth: number, thick: number, color: number): void {
  for (const e of edges(r)) {
    for (const s of along(e.len, spacing, thick + 0.3)) {
      const cx = e.a.x + e.t.x * s + e.n.x * (depth / 2), cz = e.a.z + e.t.z * s + e.n.z * (depth / 2);
      box(b, cx, cz, e.t, thick / 2, depth / 2, y0, y1, color);
    }
  }
}

/** Square columns standing just inside the outline, under an overhang. */
function columns(b: MeshBuilder, r: Ring, y0: number, y1: number, spacing: number, size: number, color: number): void {
  for (const e of edges(r)) {
    for (const s of along(e.len, spacing, 1.2)) {
      const cx = e.a.x + e.t.x * s - e.n.x * (size / 2 + 0.2), cz = e.a.z + e.t.z * s - e.n.z * (size / 2 + 0.2);
      box(b, cx, cz, e.t, size / 2, size / 2, y0, y1, color);
    }
  }
}

/** A cornice: the outline pushed out by `d` from y0 to y1, with its underside and top. */
function cornice(b: MeshBuilder, r: Ring, d: number, y0: number, y1: number, color: number, top: number): void {
  const out = offset(r, d);
  walls(b, r, out, y0, y1, color);
  band(b, r.pts, out, y0, DOWN, color);
  slab(b, out, y1, top);
}

/** Entrance canopy on the longest edge: a slab standing out 0.8 m on two posts. */
function canopy(b: MeshBuilder, r: Ring, width: number, y: number, color: number): void {
  const e = edges(r).reduce((m, x) => (x.len > m.len ? x : m));
  const w = Math.min(width, e.len - 2);
  if (w < 2) return;
  const depth = 0.8;
  const mid = { x: e.a.x + e.t.x * (e.len / 2), z: e.a.z + e.t.z * (e.len / 2) };
  box(b, mid.x + e.n.x * depth / 2, mid.z + e.n.z * depth / 2, e.t, w / 2, depth / 2, y, y + 0.3, color, true);
  for (const s of [-1, 1]) {
    const px = mid.x + e.t.x * s * (w / 2 - 0.3) + e.n.x * (depth - 0.2);
    const pz = mid.z + e.t.z * s * (w / 2 - 0.3) + e.n.z * (depth - 0.2);
    box(b, px, pz, e.t, 0.12, 0.12, 0, y, color);
  }
}

function inside(pts: P[], x: number, z: number): boolean {
  let hit = false;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const a = pts[i], c = pts[j];
    if (a.z > z !== c.z > z && x < ((c.x - a.x) * (z - a.z)) / (c.z - a.z) + a.x) hit = !hit;
  }
  return hit;
}

function edgeDistance(pts: P[], x: number, z: number): number {
  let best = Infinity;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const a = pts[j], c = pts[i];
    const dx = c.x - a.x, dz = c.z - a.z;
    const t = Math.max(0, Math.min(1, ((x - a.x) * dx + (z - a.z) * dz) / (dx * dx + dz * dz || 1)));
    best = Math.min(best, Math.hypot(x - a.x - t * dx, z - a.z - t * dz));
  }
  return best;
}

interface Spot { x: number; z: number; h: number }

/**
 * Up to `count` square spots on the roof, aligned with `u`, at most `maxHalf` metres half-size,
 * fully inside the outline with a 1 m margin, not overlapping each other. Deterministic.
 */
function roofSpots(r: Ring, u: P, count: number, maxHalf: number): Spot[] {
  const v2 = r.pts.map((p) => new THREE.Vector2(p.x, p.z));
  const tris = THREE.ShapeUtils.triangulateShape(v2, []).map(([i, j, k]) => {
    const a = r.pts[i], b = r.pts[j], c = r.pts[k];
    const area = Math.abs((b.x - a.x) * (c.z - a.z) - (c.x - a.x) * (b.z - a.z)) / 2;
    return { x: (a.x + b.x + c.x) / 3, z: (a.z + b.z + c.z) / 3, area };
  });
  tris.sort((a, b) => b.area - a.area || a.x - b.x || a.z - b.z);
  const v = { x: -u.z, z: u.x };
  const spots: Spot[] = [];
  for (const t of tris) {
    if (spots.length >= count) break;
    for (let h = maxHalf; h >= 1; h -= 0.5) {
      const ok = [[-1, -1], [1, -1], [1, 1], [-1, 1], [0, 0]].every(([su, sv]) => {
        const x = t.x + (u.x * su + v.x * sv) * h, z = t.z + (u.z * su + v.z * sv) * h;
        return inside(r.pts, x, z) && edgeDistance(r.pts, x, z) >= 1;
      });
      const clear = spots.every((s) => Math.hypot(s.x - t.x, s.z - t.z) > (s.h + h) * 1.5 + 1);
      if (ok && clear) {
        spots.push({ x: t.x, z: t.z, h });
        break;
      }
    }
  }
  return spots;
}

function longestDirection(r: Ring): P {
  return edges(r).reduce((m, x) => (x.len > m.len ? x : m)).t;
}

// Flat colours. Concrete and stucco tones only: no school colours or branding.
const CONCRETE = 0xd8d2c4;
const CONCRETE_LIGHT = 0xe9e4d8;
const CONCRETE_DARK = 0xa9a294;
const GLASS_DARK = 0x3d4a55;
const GLASS = 0x6f93a8;
const SKYLIGHT = 0x9cc3d6;
const STUCCO = 0xe8dcc2;
const STUCCO_TRIM = 0xb8a88f;
const PLINTH = 0x9b8b74;
const ROOF = 0x8a8274;
const PLANT = 0xa7a49c;
const SANDSTONE = 0xd2ae86;
const SANDSTONE_TRIM = 0xb48f6a;

/**
 * Langson Library podium: a recessed dark-glass ground floor behind a colonnade, then two
 * concrete storeys with ribbon windows between deep vertical fins.
 */
function langsonPodium(b: MeshBuilder, r: Ring, h: number): void {
  const ground = 3.6;
  const inner = offset(r, -1.6);
  walls(b, r, inner, 0, ground, GLASS_DARK);
  band(b, inner, r.pts, ground, DOWN, CONCRETE_DARK);
  columns(b, r, 0, ground, 5, 0.6, CONCRETE);
  walls(b, r, r.pts, ground, h, CONCRETE);
  for (let y = ground; y + 3 <= h; y += (h - ground) / 2) windows(b, r, y + 0.7, y + 2.6, 2.5, 1.6, GLASS_DARK);
  fins(b, r, ground, h, 2.5, 0.5, 0.25, CONCRETE_LIGHT);
  slab(b, r.pts, h, ROOF);
}

/** Langson Library top storey: a glazed ribbon under a deep overhanging crown and a penthouse. */
function langsonTop(b: MeshBuilder, r: Ring, y0: number, h: number): void {
  walls(b, r, r.pts, y0, h - 0.8, CONCRETE);
  ribbons(b, r, y0 + 0.8, h - 1.6, 1.2, GLASS_DARK);
  fins(b, r, y0 + 0.8, h - 1.6, 3, 0.2, 0.15, CONCRETE_LIGHT);
  cornice(b, r, 0.5, h - 0.8, h, CONCRETE_LIGHT, ROOF);
  const u = longestDirection(r);
  for (const s of roofSpots(r, u, 1, 6)) box(b, s.x, s.z, u, s.h, s.h * 0.6, h, h + 2.4, CONCRETE_DARK);
}

/**
 * Aldrich Hall: two stucco storeys on a stone plinth, with pilasters between the windows, a
 * heavy cornice, rooftop plant and an entrance canopy.
 */
function aldrichHall(b: MeshBuilder, r: Ring, h: number): void {
  const plinth = 0.8, top = h - 0.9;
  const out = offset(r, 0.15);
  walls(b, r, out, 0, plinth, PLINTH);
  band(b, r.pts, out, plinth, UP, PLINTH);
  walls(b, r, r.pts, plinth, top, STUCCO);
  const floor = (top - plinth) / 2;
  for (let i = 0; i < 2; i++) windows(b, r, plinth + i * floor + 0.9, plinth + i * floor + 2.7, 3.2, 1.4, GLASS_DARK);
  fins(b, r, plinth, top, 3.2, 0.3, 0.4, CONCRETE_LIGHT);
  cornice(b, r, 0.4, top, h, STUCCO_TRIM, ROOF);
  canopy(b, r, 6, 3.2, STUCCO_TRIM);
  const u = longestDirection(r);
  for (const s of roofSpots(r, u, 3, 2.5)) box(b, s.x, s.z, u, s.h, s.h * 0.7, h, h + 1.6, PLANT);
}

/** Student Center: sandstone walls with a band of glazing on every floor and glass skylights. */
function studentCenter(b: MeshBuilder, r: Ring, h: number): void {
  const top = h - 1;
  walls(b, r, r.pts, 0, top, SANDSTONE);
  const floors = 3, fh = top / floors;
  for (let i = 0; i < floors; i++) windows(b, r, i * fh + 0.7, i * fh + fh - 0.9, 3, 2.4, GLASS);
  cornice(b, r, 0.3, top, h, SANDSTONE_TRIM, ROOF);
  canopy(b, r, 8, 3.4, SANDSTONE_TRIM);
  const u = longestDirection(r);
  const spots = roofSpots(r, u, 5, 4);
  spots.forEach((s, i) => {
    if (i < 3) pyramid(b, s.x, s.z, u, s.h, h, s.h * 0.8, SKYLIGHT);
    else box(b, s.x, s.z, u, s.h * 0.7, s.h * 0.5, h, h + 1.6, PLANT);
  });
}

function byId(campus: CampusData, id: number): Building | undefined {
  return campus.buildings.find((x) => x.id === id);
}

/**
 * Add the landmark model for `building` to `b`, in world metres. Returns false (adding nothing)
 * if the building is not a landmark. Reads `campus`, never changes it.
 */
export function buildLandmark(b: MeshBuilder, building: Building, campus: CampusData): boolean {
  const r = ring(building.p);
  const h = building.h * DM;
  switch (building.id) {
    case LANGSON_PODIUM:
      langsonPodium(b, r, h);
      return true;
    case LANGSON_TOP: {
      const podium = byId(campus, LANGSON_PODIUM);
      langsonTop(b, r, Math.min(podium ? podium.h * DM : h / 2, h - 3), h);
      return true;
    }
    case ALDRICH_HALL:
      aldrichHall(b, r, h);
      return true;
    case STUDENT_CENTER:
      studentCenter(b, r, h);
      return true;
    default:
      return false;
  }
}
