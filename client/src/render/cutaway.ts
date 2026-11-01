import * as THREE from 'three';
import areasRaw from '../../../shared/map/areas.json?raw';
import { CAFE_DOOR } from './cafeInterior';
import { addWindowGlow } from './campusMap';
import { faces, ring, type Face } from './facade';
import { parseAreas } from './mapData';

/**
 * The Student Center's cut-away view (docs/specs/visual-style.md, Buildings and interiors): while the
 * local player is in the café, the building's roof and the walls between the camera and the player fade
 * out so the room shows; outside, the roof is solid. Visual only; reads positions, changes no game state.
 *
 * The building is one mesh with per-vertex `cut` and `cut2` indices (0 roof, 1.. a wall segment; a small
 * piece of trim over a segment boundary carries both and fades when either does). Faded
 * parts are dithered away in the fragment shader, so the material stays opaque and the fade needs no
 * sorting and no extra draw call. While nothing is faded the building is drawn inside its map chunk as
 * before, so being outside adds no draw call.
 */

/** Q2 (PRD section 13): how long the roof and walls take to fade in or out, in milliseconds. */
export const CUTAWAY_FADE_MS = 300;
/** Long wall faces are split into segments of at most this many metres, so only the part in the way fades. */
export const CUTAWAY_SEGMENT_M = 6;
/** Triangles starting at or above the roof line `top` (less this) are roof. */
const ROOF_EPS = 0.01;
/** Wall triangles this long or longer along their face (whole walls, glazing bands) are split at segment boundaries. */
const SPLIT_MIN_M = CUTAWAY_SEGMENT_M / 2;

export interface WallSegment { ax: number; az: number; bx: number; bz: number }

/** Interior polygon (metres) of the café from areas.json. */
export function cafePolygon(): [number, number][] {
  const c = parseAreas(areasRaw).interiors.find((a) => a.id === 'student_center_cafe');
  if (!c) throw new Error('areas.json has no student_center_cafe interior');
  const out: [number, number][] = [];
  for (let i = 0; i < c.p.length; i += 2) out.push([c.p[i] / 10, c.p[i + 1] / 10]);
  return out;
}

export function pointInPolygon(x: number, z: number, poly: readonly (readonly [number, number])[]): boolean {
  let r = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [xi, zi] = poly[i], [xj, zj] = poly[j];
    if (zi > z !== zj > z && x < xi + ((z - zi) * (xj - xi)) / (zj - zi)) r = !r;
  }
  return r;
}

/**
 * Whether the player counts as inside the café. On the floor: inside. On the door cells: unchanged,
 * so standing in the doorway or stepping back and forth over it never flips the state. Anywhere else: outside.
 */
export class CafeZone {
  inside = false;
  constructor(private readonly poly: readonly (readonly [number, number])[] = cafePolygon()) {}

  update(x: number, z: number): boolean {
    if (pointInPolygon(x, z, this.poly)) this.inside = true;
    else if (!(x >= CAFE_DOOR[0] && x < CAFE_DOOR[2] && z >= CAFE_DOOR[1] && z < CAFE_DOOR[3])) this.inside = false;
    return this.inside;
  }
}

/** The footprint's wall faces (metres, `p` in decimetres), each split into pieces of at most `maxLen`. */
export function wallSegments(p: number[], maxLen = CUTAWAY_SEGMENT_M): WallSegment[] {
  const out: WallSegment[] = [];
  for (const f of faces(ring(p))) {
    const n = Math.max(1, Math.ceil(f.len / maxLen));
    for (let k = 0; k < n; k++) {
      const s0 = (f.len * k) / n, s1 = (f.len * (k + 1)) / n;
      out.push({ ax: f.a.x + f.t.x * s0, az: f.a.z + f.t.z * s0, bx: f.a.x + f.t.x * s1, bz: f.a.z + f.t.z * s1 });
    }
  }
  return out;
}

/** Whether the segment s crosses the line from (cx, cz) to (px, pz). */
export function crosses(s: WallSegment, cx: number, cz: number, px: number, pz: number): boolean {
  const d = (ax: number, az: number, bx: number, bz: number, qx: number, qz: number) => (bx - ax) * (qz - az) - (bz - az) * (qx - ax);
  const d1 = d(cx, cz, px, pz, s.ax, s.az), d2 = d(cx, cz, px, pz, s.bx, s.bz);
  const d3 = d(s.ax, s.az, s.bx, s.bz, cx, cz), d4 = d(s.ax, s.az, s.bx, s.bz, px, pz);
  return d1 * d2 < 0 && d3 * d4 < 0;
}

/**
 * Opacity of the roof (index 0) and each wall segment (index 1..), moving toward their targets at
 * 1 / CUTAWAY_FADE_MS per millisecond, so a full fade either way takes CUTAWAY_FADE_MS.
 */
export class CutawayFade {
  readonly alpha: Float32Array;
  private last = -1;
  constructor(segments: number, readonly fadeMs = CUTAWAY_FADE_MS) {
    this.alpha = new Float32Array(segments + 1).fill(1);
  }

  /** Move toward `target` (same length as `alpha`) at the render clock `nowMs`. */
  step(nowMs: number, target: ArrayLike<number>): void {
    const dt = this.last < 0 ? 0 : Math.max(0, nowMs - this.last);
    this.last = nowMs;
    const d = dt / this.fadeMs;
    for (let i = 0; i < this.alpha.length; i++) {
      const a = this.alpha[i], t = target[i];
      const next = a < t ? Math.min(t, a + d) : Math.max(t, a - d);
      this.alpha[i] = Math.abs(next - t) < 1e-6 ? t : next; // Float32 rounding must not leave it a hair short
    }
  }

  get settled(): boolean {
    return this.alpha.every((a) => a === 1);
  }
}

/**
 * The building's non-indexed `geometry` with per-vertex `cut` and `cut2` attributes: 0 for roof triangles
 * (at or above `top`), otherwise 1 + the index of its wall segments (as in `wallSegments(footprint, maxLen)`).
 * A wall face is one quad however long it is, so long wall triangles are split at the boundaries of their
 * nearest face's segments; otherwise a segment could cross the camera's line and own no triangles. Shorter
 * ones (at most two segments, as segments are at least maxLen / 2 long) keep the first and last segment
 * they touch in `cut` and `cut2`, which adds no triangles.
 */
export function tagCutaway(geometry: THREE.BufferGeometry, footprint: number[], top: number, maxLen = CUTAWAY_SEGMENT_M): THREE.BufferGeometry {
  const fs: { f: Face; n: number; first: number }[] = [];
  for (const f of faces(ring(footprint))) {
    const n = Math.max(1, Math.ceil(f.len / maxLen));
    fs.push({ f, n, first: fs.length ? fs[fs.length - 1].first + fs[fs.length - 1].n : 0 });
  }
  const names = Object.keys(geometry.attributes);
  const attrs = names.map((k) => geometry.getAttribute(k) as THREE.BufferAttribute);
  const out: number[][] = attrs.map(() => []);
  const cut: number[] = [], cut2: number[] = [];
  const vertex = (i: number) => attrs.map((a) => Array.from({ length: a.itemSize }, (_, c) => a.getComponent(i, c)));
  const emit = (tri: number[][][], id: number, id2 = id) => {
    for (const v of tri) v.forEach((vals, a) => out[a].push(...vals));
    cut.push(id, id, id);
    cut2.push(id2, id2, id2);
  };
  const lerp = (p: number[][], q: number[][], u: number) => p.map((vals, a) => vals.map((x, c) => x + (q[a][c] - x) * u));
  /** Sutherland-Hodgman: the part of polygon `poly` where `s(v) * sign >= bound * sign`. */
  const clip = (poly: number[][][], s: (v: number[][]) => number, bound: number, sign: number) => {
    const res: number[][][] = [];
    poly.forEach((p, i) => {
      const q = poly[(i + 1) % poly.length];
      const dp = (s(p) - bound) * sign, dq = (s(q) - bound) * sign;
      if (dp >= 0) res.push(p);
      if (dp >= 0 !== dq >= 0) res.push(lerp(p, q, dp / (dp - dq)));
    });
    return res;
  };
  const pos = geometry.getAttribute('position') as THREE.BufferAttribute;
  for (let t = 0; t + 2 < pos.count; t += 3) {
    const tri = [vertex(t), vertex(t + 1), vertex(t + 2)];
    const ymin = Math.min(pos.getY(t), pos.getY(t + 1), pos.getY(t + 2));
    if (ymin >= top - ROOF_EPS) {
      emit(tri, 0);
      continue;
    }
    const x = (pos.getX(t) + pos.getX(t + 1) + pos.getX(t + 2)) / 3, z = (pos.getZ(t) + pos.getZ(t + 1) + pos.getZ(t + 2)) / 3;
    let face = fs[0], best = Infinity;
    for (const g of fs) {
      const u = Math.max(0, Math.min(g.f.len, (x - g.f.a.x) * g.f.t.x + (z - g.f.a.z) * g.f.t.z));
      const d = Math.hypot(x - (g.f.a.x + g.f.t.x * u), z - (g.f.a.z + g.f.t.z * u));
      if (d < best) { best = d; face = g; }
    }
    const { f, n, first } = face;
    const along = (v: number[][]) => (v[0][0] - f.a.x) * f.t.x + (v[0][2] - f.a.z) * f.t.z;
    const piece = f.len / n;
    const ss = tri.map(along);
    if (Math.max(...ss) - Math.min(...ss) < SPLIT_MIN_M) {
      const j = (s: number) => Math.max(0, Math.min(n - 1, Math.floor(s / piece)));
      emit(tri, first + j(Math.min(...ss) + 1e-3) + 1, first + j(Math.max(...ss) - 1e-3) + 1);
      continue;
    }
    for (let j = 0; j < n; j++) {
      let poly = tri;
      if (j > 0) poly = clip(poly, along, piece * j, 1);
      if (j < n - 1 && poly.length >= 3) poly = clip(poly, along, piece * (j + 1), -1);
      for (let k = 1; k + 1 < poly.length; k++) emit([poly[0], poly[k], poly[k + 1]], first + j + 1);
    }
  }
  const g = new THREE.BufferGeometry();
  names.forEach((k, a) => g.setAttribute(k, new THREE.BufferAttribute(new Float32Array(out[a]), attrs[a].itemSize)));
  g.setAttribute('cut', new THREE.BufferAttribute(new Float32Array(cut), 1));
  g.setAttribute('cut2', new THREE.BufferAttribute(new Float32Array(cut2), 1));
  g.computeBoundingSphere();
  return g;
}

/** Patch a Lambert shader so vertices fade by the lesser of `cutAlpha[cut]` and `cutAlpha[cut2]`, dithered on screen. */
function addCutaway(shader: { uniforms: Record<string, THREE.IUniform>; vertexShader: string; fragmentShader: string }, alpha: THREE.IUniform<Float32Array>, n: number): void {
  shader.uniforms.cutAlpha = alpha;
  shader.vertexShader = `attribute float cut;\nattribute float cut2;\nvarying float vCut;\nuniform float cutAlpha[${n}];\n` +
    shader.vertexShader.replace('#include <begin_vertex>', '#include <begin_vertex>\n\tvCut = min(cutAlpha[int(cut + 0.5)], cutAlpha[int(cut2 + 0.5)]);');
  shader.fragmentShader = 'varying float vCut;\n' +
    shader.fragmentShader.replace(
      'void main() {',
      `void main() {
	if (vCut < 0.999) {
		vec2 c = floor(mod(gl_FragCoord.xy, 4.0));
		float bayer = mod(c.x * 4.0 + c.y * 9.0 + mod(c.x * c.y, 3.0) * 5.0, 16.0) / 16.0;
		if (vCut <= bayer) discard;
	}`,
    );
}

export interface CutawayParts {
  /** The map chunk mesh that holds the building while nothing is faded. */
  chunk: THREE.Mesh;
  /** The chunk's geometry with the building in it, and without it. */
  baked: THREE.BufferGeometry;
  open: THREE.BufferGeometry;
  /** The building alone; tagged and split by segment here (tagCutaway). */
  building: THREE.BufferGeometry;
  /** Footprint `p` in decimetres and the roof line (metres). */
  footprint: number[];
  top: number;
  glow: THREE.IUniform<THREE.Color>;
}

/**
 * Swaps the building between its map chunk and its own mesh, and fades the roof and walls of the
 * latter while the local player is in the café.
 */
export class Cutaway {
  readonly zone = new CafeZone();
  readonly segments: WallSegment[];
  readonly fade: CutawayFade;
  /** The building drawn on its own; visible only while something is faded or fading. */
  readonly mesh: THREE.Mesh;
  private readonly alpha: THREE.IUniform<Float32Array>;
  private readonly target: Float32Array;

  constructor(private readonly parts: CutawayParts) {
    this.segments = wallSegments(parts.footprint);
    this.fade = new CutawayFade(this.segments.length);
    this.target = new Float32Array(this.fade.alpha.length).fill(1);
    this.alpha = { value: this.fade.alpha };
    const material = new THREE.MeshLambertMaterial({ vertexColors: true });
    const n = this.fade.alpha.length;
    material.onBeforeCompile = (shader) => {
      addWindowGlow(shader, parts.glow);
      addCutaway(shader, this.alpha, n);
    };
    material.customProgramCacheKey = () => `campus-window-glow-cutaway-${n}`;
    this.mesh = new THREE.Mesh(tagCutaway(parts.building, parts.footprint, parts.top), material);
    this.mesh.name = 'student-center-cutaway';
    this.mesh.matrixAutoUpdate = false;
    this.mesh.receiveShadow = true;
    this.mesh.castShadow = true;
    this.mesh.visible = false;
  }

  /** Roof opacity, 0..1. */
  get roof(): number {
    return this.fade.alpha[0];
  }

  /** Opacity of wall segment `i` (0-based, matching `segments`). */
  wall(i: number): number {
    return this.fade.alpha[i + 1];
  }

  /** Targets for a player at (px, pz) with the camera at (cx, cz): the walls on the line between them fade. */
  private retarget(px: number, pz: number, cx: number, cz: number): void {
    const inside = this.zone.update(px, pz);
    this.target[0] = inside ? 0 : 1;
    this.segments.forEach((s, i) => {
      this.target[i + 1] = inside && crosses(s, cx, cz, px, pz) ? 0 : 1;
    });
  }

  update(px: number, pz: number, cx: number, cz: number, nowMs: number): void {
    this.retarget(px, pz, cx, cz);
    const active = !this.fade.settled || this.target.some((t) => t < 1);
    // Swap to the standalone building before the first frame of a fade, back after the last, so it never pops.
    if (active && !this.mesh.visible) this.show(true);
    this.fade.step(nowMs, this.target);
    if (this.mesh.visible) {
      if (this.fade.settled && !this.target.some((t) => t < 1)) this.show(false);
      else this.mesh.castShadow = this.fade.alpha[0] > 0.5;
    }
  }

  private show(on: boolean): void {
    this.mesh.visible = on;
    this.parts.chunk.geometry = on ? this.parts.open : this.parts.baked;
  }

  dispose(): void {
    (this.mesh.material as THREE.Material).dispose();
    this.parts.baked.dispose();
    this.parts.open.dispose();
  }
}
