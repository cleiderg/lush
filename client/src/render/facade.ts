import * as THREE from 'three';
import type { MeshBuilder } from './campusMap';

/**
 * Wall detail shared by generic buildings (campusMap.ts) and landmarks (landmarks.ts): framed
 * windows whose glass glows at night, doors, trim and roof edges (docs/specs/visual-style.md,
 * Buildings and interiors). Everything goes into the chunk's MeshBuilder, window glass into its
 * `glass` builder, so detail never adds draw calls per building or window.
 */

export const UP = [0, 1, 0];
export const DOWN = [0, -1, 0];

export type P = { x: number; z: number };

export interface Ring {
  pts: P[];
  /** Outward unit normal of edge i (from pts[i] to pts[i + 1]). */
  normals: P[];
}

/** A footprint in decimetres as a ring in metres. */
export function ring(p: number[], scale = 0.1): Ring {
  const pts: P[] = [];
  for (let i = 0; i < p.length; i += 2) pts.push({ x: p[i] * scale, z: p[i + 1] * scale });
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
export function offset(r: Ring, d: number): P[] {
  const n = r.pts.length;
  return r.pts.map((p, i) => {
    const a = r.normals[(i + n - 1) % n], b = r.normals[i];
    const k = d / Math.max(0.25, 1 + a.x * b.x + a.z * b.z);
    return { x: p.x + (a.x + b.x) * k, z: p.z + (a.z + b.z) * k };
  });
}

export function quad(b: MeshBuilder, a: number[], c: number[], d: number[], e: number[], want: number[], color: number): void {
  b.tri(a, c, d, want, color);
  b.tri(a, d, e, want, color);
}

/** Walls along a closed outline from y0 to y1, facing the ring's outward normals. */
export function walls(b: MeshBuilder, r: Ring, pts: P[], y0: number, y1: number, color: number): void {
  for (let i = 0; i < pts.length; i++) {
    const a = pts[i], c = pts[(i + 1) % pts.length], n = r.normals[i];
    quad(b, [a.x, y0, a.z], [c.x, y0, c.z], [c.x, y1, c.z], [a.x, y1, a.z], [n.x, 0, n.z], color);
  }
}

/** A flat polygon at height y. */
export function slab(b: MeshBuilder, pts: P[], y: number, color: number, want: number[] = UP): void {
  const v = pts.map((p) => new THREE.Vector2(p.x, p.z));
  for (const [i, j, k] of THREE.ShapeUtils.triangulateShape(v, [])) {
    b.tri([v[i].x, y, v[i].y], [v[j].x, y, v[j].y], [v[k].x, y, v[k].y], want, color);
  }
}

/** The flat band between two outlines with matching vertices (a ledge or soffit). */
export function band(b: MeshBuilder, inner: P[], outer: P[], y: number, want: number[], color: number): void {
  for (let i = 0; i < inner.length; i++) {
    const j = (i + 1) % inner.length;
    quad(b, [inner[i].x, y, inner[i].z], [inner[j].x, y, inner[j].z], [outer[j].x, y, outer[j].z], [outer[i].x, y, outer[i].z], want, color);
  }
}

/** One wall face: start point, unit tangent, outward normal and length. */
export interface Face { a: P; t: P; n: P; len: number }

export function faces(r: Ring, pts: P[] = r.pts): Face[] {
  return pts.map((a, i) => {
    const c = pts[(i + 1) % pts.length];
    const len = Math.hypot(c.x - a.x, c.z - a.z);
    return { a, t: { x: (c.x - a.x) / (len || 1), z: (c.z - a.z) / (len || 1) }, n: r.normals[i], len };
  });
}

/** The point `s` metres along a face and `off` metres out from it, at height y. */
function at(f: Face, s: number, off: number, y: number): number[] {
  return [f.a.x + f.t.x * s + f.n.x * off, y, f.a.z + f.t.z * s + f.n.z * off];
}

/** A rectangle parallel to a face, `off` metres proud of it, from s0 to s1 along it and y0 to y1 up. */
export function panel(b: MeshBuilder, f: Face, s0: number, s1: number, y0: number, y1: number, off: number, color: number): void {
  quad(b, at(f, s0, off, y0), at(f, s1, off, y0), at(f, s1, off, y1), at(f, s0, off, y1), [f.n.x, 0, f.n.z], color);
}

/**
 * A ledge standing `depth` out from a face: top and front, plus both ends (`ends`) and the
 * underside (`under`) for ledges big enough for them to show.
 */
export function ledge(b: MeshBuilder, f: Face, s0: number, s1: number, y0: number, y1: number, depth: number, color: number, ends = false, under = false): void {
  const n = [f.n.x, 0, f.n.z], t = [f.t.x, 0, f.t.z];
  quad(b, at(f, s0, 0, y1), at(f, s1, 0, y1), at(f, s1, depth, y1), at(f, s0, depth, y1), UP, color);
  quad(b, at(f, s0, depth, y0), at(f, s1, depth, y0), at(f, s1, depth, y1), at(f, s0, depth, y1), n, color);
  if (ends) {
    quad(b, at(f, s0, 0, y0), at(f, s0, depth, y0), at(f, s0, depth, y1), at(f, s0, 0, y1), [-t[0], 0, -t[2]], color);
    quad(b, at(f, s1, 0, y0), at(f, s1, depth, y0), at(f, s1, depth, y1), at(f, s1, 0, y1), t, color);
  }
  if (under) quad(b, at(f, s0, 0, y0), at(f, s1, 0, y0), at(f, s1, depth, y0), at(f, s0, depth, y0), DOWN, color);
}

export interface WindowLook {
  frame: number;
  glass: number;
  /** Colour of a window with its blinds down: drawn without glass, so it stays dark at night. */
  blind: number;
  /** Colour of the sill, or undefined for none. */
  sill?: number;
}

/** Frame border around the glass, in metres. */
const FRAME = 0.1;
/** Gap between the two panes of a wide window. */
const MULLION = 0.08;
/** Windows at least this wide get two panes. */
const SPLIT_WIDTH = 1.6;

/**
 * A framed window centred `s` metres along a face, `w` wide from y0 to y1: a frame, glass (two
 * panes when wide) in `b.glass` so it glows at night, and an optional sill. A window with its
 * blinds down (`lit` false) gets the same panes in the blind colour, in `b` itself.
 */
export function framedWindow(b: MeshBuilder, f: Face, s: number, w: number, y0: number, y1: number, look: WindowLook, lit = true): void {
  panel(b, f, s - w / 2 - FRAME, s + w / 2 + FRAME, y0 - FRAME, y1 + FRAME, 0.04, look.frame);
  const g = lit ? b.glass : b;
  const color = lit ? look.glass : look.blind;
  if (w >= SPLIT_WIDTH) {
    panel(g, f, s - w / 2, s - MULLION / 2, y0, y1, 0.07, color);
    panel(g, f, s + MULLION / 2, s + w / 2, y0, y1, 0.07, color);
  } else {
    panel(g, f, s - w / 2, s + w / 2, y0, y1, 0.07, color);
  }
  if (look.sill !== undefined) ledge(b, f, s - w / 2 - 0.16, s + w / 2 + 0.16, y0 - FRAME - 0.08, y0 - FRAME, 0.14, look.sill);
  b.windows++;
}

export interface DoorLook {
  frame: number;
  leaf: number;
  step: number;
}

export const DOOR_WIDTH = 1.3;
export const DOOR_HEIGHT = 2.3;

/** A door centred `s` metres along a face: frame, leaf, a step and a little hood above. */
export function door(b: MeshBuilder, f: Face, s: number, w: number, h: number, look: DoorLook, y = 0): void {
  panel(b, f, s - w / 2 - 0.12, s + w / 2 + 0.12, y, y + h + 0.12, 0.06, look.frame);
  panel(b, f, s - w / 2, s + w / 2, y, y + h, 0.08, look.leaf);
  ledge(b, f, s - w / 2 - 0.3, s + w / 2 + 0.3, y, y + 0.14, 0.5, look.step, true);
  ledge(b, f, s - w / 2 - 0.25, s + w / 2 + 0.25, y + h + 0.16, y + h + 0.3, 0.45, look.frame, true, true);
  b.doors++;
}

/**
 * Centres for windows `width` wide (narrower if the span is short) every `spacing` metres between
 * s0 and s1, keeping 0.5 m clear of both ends. Empty when not even a narrow window fits.
 */
export function fitWindows(s0: number, s1: number, spacing: number, width: number): { at: number[]; width: number } {
  const margin = 0.5;
  const len = s1 - s0;
  const w = Math.min(width, len - 2 * margin);
  if (w < 0.5) return { at: [], width: w };
  const count = Math.floor((len - 2 * margin - w) / spacing) + 1;
  const start = s0 + (len - (count - 1) * spacing) / 2;
  return { at: Array.from({ length: count }, (_, i) => start + i * spacing), width: w };
}
