import * as THREE from 'three';
import type { MeshBuilder } from './campusMap';
import {
  DOWN, UP, band, door, faces, framedWindow, offset, panel, quad, ring, slab, walls,
  type DoorLook, type Face, type P, type Ring, type WindowLook,
} from './facade';
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

/** Evenly spaced positions along an edge, keeping `margin` clear of both corners. */
function along(len: number, spacing: number, margin: number): number[] {
  const count = Math.floor((len - 2 * margin) / spacing) + 1;
  if (len - 2 * margin < 0 || count < 1) return [];
  const start = (len - (count - 1) * spacing) / 2;
  return Array.from({ length: count }, (_, i) => start + i * spacing);
}

/** Where a door is: the face index, its centre along that face and its width. */
interface Doorway { face: number; s: number; w: number }

/** Whether something `w` wide centred `s` along face `i` would overlap the doorway. */
function blocks(d: Doorway | undefined, i: number, s: number, w: number): boolean {
  return !!d && d.face === i && Math.abs(s - d.s) < (w + d.w) / 2 + 0.3;
}

/** A door at the middle of the longest face of `pts` (the ring's outline by default). */
function mainDoor(b: MeshBuilder, r: Ring, w: number, h: number, look: DoorLook, pts: P[] = r.pts): Doorway {
  const fs = faces(r, pts);
  const i = fs.reduce((m, f, k) => (f.len > fs[m].len ? k : m), 0);
  door(b, fs[i], fs[i].len / 2, w, h, look);
  return { face: i, s: fs[i].len / 2, w };
}

/** Framed windows standing just proud of the wall, one row from y0 to y1, clear of the doorway. */
function windows(b: MeshBuilder, r: Ring, y0: number, y1: number, spacing: number, width: number, look: WindowLook, avoid?: Doorway): void {
  faces(r).forEach((e, i) => {
    for (const s of along(e.len, spacing, width / 2 + 0.6)) {
      if (!blocks(avoid, i, s, width)) framedWindow(b, e, s, width, y0, y1, look);
    }
  });
}

/** One long framed glazed ribbon per edge, from y0 to y1. */
function ribbons(b: MeshBuilder, r: Ring, y0: number, y1: number, margin: number, look: WindowLook): void {
  for (const e of faces(r)) {
    if (e.len < 2 * margin + 1) continue;
    panel(b, e, margin - 0.12, e.len - margin + 0.12, y0 - 0.12, y1 + 0.12, 0.04, look.frame);
    panel(b.glass, e, margin, e.len - margin, y0, y1, 0.07, look.glass);
    b.windows++;
  }
}

/** Vertical fins or pilasters standing `depth` out from the wall, clear of the doorway. */
function fins(b: MeshBuilder, r: Ring, y0: number, y1: number, spacing: number, depth: number, thick: number, color: number, avoid?: Doorway): void {
  faces(r).forEach((e, i) => {
    for (const s of along(e.len, spacing, thick + 0.3)) {
      if (blocks(avoid, i, s, thick)) continue;
      const cx = e.a.x + e.t.x * s + e.n.x * (depth / 2), cz = e.a.z + e.t.z * s + e.n.z * (depth / 2);
      box(b, cx, cz, e.t, thick / 2, depth / 2, y0, y1, color);
    }
  });
}

/** Square columns standing just inside the outline, under an overhang. */
function columns(b: MeshBuilder, r: Ring, y0: number, y1: number, spacing: number, size: number, color: number): void {
  for (const e of faces(r)) {
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
  const e = faces(r).reduce((m, x) => (x.len > m.len ? x : m));
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
  return faces(r).reduce((m, x) => (x.len > m.len ? x : m)).t;
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
const DOOR_WOOD = 0x6b4630;
const STEP = 0xb5ada0;
const SANDSTONE = 0xd2ae86;
const SANDSTONE_TRIM = 0xb48f6a;

const DARK_WINDOWS: WindowLook = { frame: CONCRETE_LIGHT, glass: GLASS_DARK, blind: GLASS_DARK };
const STUCCO_WINDOWS: WindowLook = { frame: 0xf0ebe0, glass: GLASS_DARK, blind: GLASS_DARK, sill: STUCCO_TRIM };
const SANDSTONE_WINDOWS: WindowLook = { frame: SANDSTONE_TRIM, glass: GLASS, blind: GLASS_DARK, sill: SANDSTONE_TRIM };

/**
 * Langson Library podium: a recessed dark-glass ground floor behind a colonnade, then two
 * concrete storeys with ribbon windows between deep vertical fins.
 */
function langsonPodium(b: MeshBuilder, r: Ring, h: number): void {
  const ground = 3.6;
  const inner = offset(r, -1.6);
  walls(b.glass, r, inner, 0, ground, GLASS_DARK);
  mainDoor(b, r, 2.4, 2.6, { frame: CONCRETE_DARK, leaf: GLASS, step: CONCRETE }, inner);
  band(b, inner, r.pts, ground, DOWN, CONCRETE_DARK);
  columns(b, r, 0, ground, 5, 0.6, CONCRETE);
  walls(b, r, r.pts, ground, h, CONCRETE);
  for (let y = ground; y + 3 <= h; y += (h - ground) / 2) windows(b, r, y + 0.7, y + 2.6, 2.5, 1.6, DARK_WINDOWS);
  fins(b, r, ground, h, 2.5, 0.5, 0.25, CONCRETE_LIGHT);
  slab(b, r.pts, h, ROOF);
}

/** Langson Library top storey: a glazed ribbon under a deep overhanging crown and a penthouse. */
function langsonTop(b: MeshBuilder, r: Ring, y0: number, h: number): void {
  walls(b, r, r.pts, y0, h - 0.8, CONCRETE);
  ribbons(b, r, y0 + 0.8, h - 1.6, 1.2, DARK_WINDOWS);
  fins(b, r, y0 + 0.8, h - 1.6, 3, 0.2, 0.15, CONCRETE_LIGHT);
  cornice(b, r, 0.5, h - 0.8, h, CONCRETE_LIGHT, ROOF);
  const u = longestDirection(r);
  for (const s of roofSpots(r, u, 1, 6)) {
    box(b, s.x, s.z, u, s.h, s.h * 0.6, h, h + 2.4, CONCRETE_DARK);
    // Roof access door on the penthouse's long side.
    const v = { x: -u.z, z: u.x };
    const a = { x: s.x - u.x * s.h + v.x * s.h * 0.6, z: s.z - u.z * s.h + v.z * s.h * 0.6 };
    const face: Face = { a, t: u, n: v, len: 2 * s.h };
    door(b, face, s.h, 0.9, 2, { frame: CONCRETE_LIGHT, leaf: GLASS_DARK, step: CONCRETE }, h);
  }
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
  const entry = mainDoor(b, r, 1.6, 2.4, { frame: STUCCO_TRIM, leaf: DOOR_WOOD, step: PLINTH });
  for (let i = 0; i < 2; i++) windows(b, r, plinth + i * floor + 0.9, plinth + i * floor + 2.7, 3.2, 1.4, STUCCO_WINDOWS, i === 0 ? entry : undefined);
  fins(b, r, plinth, top, 3.2, 0.3, 0.4, CONCRETE_LIGHT, entry);
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
  const entry = mainDoor(b, r, 2, 2.6, { frame: SANDSTONE_TRIM, leaf: GLASS, step: STEP });
  for (let i = 0; i < floors; i++) windows(b, r, i * fh + 0.7, i * fh + fh - 0.9, 3, 2.4, SANDSTONE_WINDOWS, i === 0 ? entry : undefined);
  cornice(b, r, 0.3, top, h, SANDSTONE_TRIM, ROOF);
  canopy(b, r, 8, 3.4, SANDSTONE_TRIM);
  const u = longestDirection(r);
  const spots = roofSpots(r, u, 5, 4);
  spots.forEach((s, i) => {
    if (i < 3) pyramid(b.glass, s.x, s.z, u, s.h, h, s.h * 0.8, SKYLIGHT);
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
