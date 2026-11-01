import * as THREE from 'three';
import areasRaw from '../../../shared/map/areas.json?raw';
import { CAFE_DOOR } from './cafeInterior';
import { addWindowGlow } from './campusMap';
import { faces, ring } from './facade';
import { parseAreas } from './mapData';

/**
 * The Student Center's cut-away view (docs/specs/visual-style.md, Buildings and interiors): while the
 * local player is in the café, the building's roof and the walls between the camera and the player fade
 * out so the room shows; outside, the roof is solid. Visual only; reads positions, changes no game state.
 *
 * The building is one mesh with a per-vertex `cut` index (-1 fixed, 0 roof, 1.. a wall segment). Faded
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
 * Give every vertex of the building's non-indexed `geometry` a `cut` attribute: 0 for roof triangles
 * (at or above `top`), otherwise 1 + the index of the nearest wall segment.
 */
export function tagCutaway(geometry: THREE.BufferGeometry, segments: readonly WallSegment[], top: number): void {
  const pos = geometry.getAttribute('position') as THREE.BufferAttribute;
  const cut = new Float32Array(pos.count);
  for (let t = 0; t + 2 < pos.count; t += 3) {
    let x = 0, z = 0, ymin = Infinity;
    for (let k = 0; k < 3; k++) {
      x += pos.getX(t + k) / 3;
      z += pos.getZ(t + k) / 3;
      ymin = Math.min(ymin, pos.getY(t + k));
    }
    let id = 0;
    if (ymin < top - ROOF_EPS) {
      let best = Infinity;
      segments.forEach((s, i) => {
        const dx = s.bx - s.ax, dz = s.bz - s.az;
        const u = Math.max(0, Math.min(1, ((x - s.ax) * dx + (z - s.az) * dz) / (dx * dx + dz * dz || 1)));
        const d = Math.hypot(x - (s.ax + dx * u), z - (s.az + dz * u));
        if (d < best) { best = d; id = i + 1; }
      });
    }
    cut[t] = cut[t + 1] = cut[t + 2] = id;
  }
  geometry.setAttribute('cut', new THREE.BufferAttribute(cut, 1));
}

/** Patch a Lambert shader so vertices fade by `cutAlpha[cut]` (cut < 0 never fades), dithered on screen. */
function addCutaway(shader: { uniforms: Record<string, THREE.IUniform>; vertexShader: string; fragmentShader: string }, alpha: THREE.IUniform<Float32Array>, n: number): void {
  shader.uniforms.cutAlpha = alpha;
  shader.vertexShader = `attribute float cut;\nvarying float vCut;\nuniform float cutAlpha[${n}];\n` +
    shader.vertexShader.replace('#include <begin_vertex>', '#include <begin_vertex>\n\tvCut = cut < 0.0 ? 1.0 : cutAlpha[int(cut + 0.5)];');
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
  /** The building alone. Tagged here. */
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
    tagCutaway(parts.building, this.segments, parts.top);
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
    this.mesh = new THREE.Mesh(parts.building, material);
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
