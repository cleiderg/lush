import * as THREE from 'three';
import { CHARACTER_HEIGHT } from './character';
import type { HitView, PlayerView } from './worldView';

/** How long a hitsplat stays up. */
export const HITSPLAT_MS = 1200;
/** Most hitsplats shown at once; the oldest goes first past this. */
export const MAX_HITSPLATS = 32;
/** Largest number a splat can show; bigger hits show this. */
export const MAX_SHOWN_DAMAGE = 999;
/** Splat centre height above an entity's feet (metres). */
export const HITSPLAT_HEIGHT = CHARACTER_HEIGHT + 0.35;
/** Vertical gap between splats stacked on one entity. */
export const HITSPLAT_STACK = 0.55;

export const HIT_COLOR = 0xc0262d;
export const MISS_COLOR = 0x2f6fd0;
const DIGIT_COLOR = 0xffffff;

const RADIUS = 0.24;
const DIGIT_W = 0.12;
const DIGIT_H = 0.22;
const SEG_T = 0.035;
const DIGIT_GAP = 0.06;
const MAX_DIGITS = String(MAX_SHOWN_DAMAGE).length;

/** Seven-segment layout: a top, b top-right, c bottom-right, d bottom, e bottom-left, f top-left, g middle. */
const SEGMENTS: readonly (readonly [x: number, y: number, horizontal: boolean])[] = [
  [0, DIGIT_H / 2, true],
  [DIGIT_W / 2, DIGIT_H / 4, false],
  [DIGIT_W / 2, -DIGIT_H / 4, false],
  [0, -DIGIT_H / 2, true],
  [-DIGIT_W / 2, -DIGIT_H / 4, false],
  [-DIGIT_W / 2, DIGIT_H / 4, false],
  [0, 0, true],
];
const DIGIT_SEGMENTS = ['abcdef', 'bc', 'abdeg', 'abcdg', 'bcfg', 'acdfg', 'acdefg', 'abc', 'abcdefg', 'abcdfg'].map((s) =>
  [...s].map((ch) => ch.charCodeAt(0) - 97),
);

/** Segment indices lit for each digit of `damage`, left to right. */
export function digitSegments(damage: number): number[][] {
  const n = Math.max(0, Math.min(MAX_SHOWN_DAMAGE, Math.floor(damage)));
  return [...String(n)].map((d) => DIGIT_SEGMENTS[Number(d)]);
}

export interface Hitsplat {
  readonly target: number;
  readonly damage: number;
  readonly miss: boolean;
  readonly bornMs: number;
  /** World position of the splat centre, updated each frame. */
  readonly position: THREE.Vector3;
}

/**
 * Damage numbers over hit entities. A disc (red hit, blue miss) and seven-segment
 * digits, drawn as two InstancedMeshes facing the camera: two draw calls at most,
 * and nothing is created per hit.
 */
export class Hitsplats {
  readonly group = new THREE.Group();
  private readonly discs: THREE.InstancedMesh;
  private readonly segs: THREE.InstancedMesh;
  private readonly splats: Hitsplat[] = [];
  private readonly m = new THREE.Matrix4();
  private readonly local = new THREE.Matrix4();
  private readonly scale = new THREE.Vector3();
  private readonly offset = new THREE.Vector3();
  private readonly color = new THREE.Color();
  private readonly noRot = new THREE.Quaternion();

  constructor() {
    this.discs = new THREE.InstancedMesh(
      new THREE.CircleGeometry(RADIUS, 10),
      new THREE.MeshBasicMaterial({ depthTest: false, transparent: true }),
      MAX_HITSPLATS,
    );
    this.discs.setColorAt(0, this.color.set(HIT_COLOR));
    this.discs.renderOrder = 10;
    this.segs = new THREE.InstancedMesh(
      new THREE.PlaneGeometry(1, 1),
      new THREE.MeshBasicMaterial({ color: DIGIT_COLOR, depthTest: false, transparent: true }),
      MAX_HITSPLATS * MAX_DIGITS * SEGMENTS.length,
    );
    this.segs.renderOrder = 11;
    for (const mesh of [this.discs, this.segs]) {
      mesh.count = 0;
      mesh.frustumCulled = false;
      mesh.name = mesh === this.discs ? 'hitsplat-discs' : 'hitsplat-digits';
      this.group.add(mesh);
    }
    this.group.name = 'hitsplats';
  }

  /** Splats currently shown, oldest first. */
  get active(): readonly Hitsplat[] {
    return this.splats;
  }

  /** Adds new hits on entities in the view, drops expired ones, and places the rest facing `facing`. */
  update(view: { local: PlayerView; remotes: readonly PlayerView[]; hits?: readonly HitView[] }, nowMs: number, facing: THREE.Quaternion): void {
    const where = new Map<number, PlayerView>();
    where.set(view.local.id, view.local);
    for (const p of view.remotes) where.set(p.id, p);

    for (const h of view.hits ?? []) {
      if (!where.has(h.target)) continue;
      this.splats.push({ target: h.target, damage: h.damage, miss: h.damage <= 0, bornMs: nowMs, position: new THREE.Vector3() });
    }
    // Drop expired splats, splats whose entity left the view, then the oldest past the cap.
    for (let i = this.splats.length - 1; i >= 0; i--) {
      const s = this.splats[i];
      if (nowMs - s.bornMs >= HITSPLAT_MS || !where.has(s.target)) this.splats.splice(i, 1);
    }
    if (this.splats.length > MAX_HITSPLATS) this.splats.splice(0, this.splats.length - MAX_HITSPLATS);

    const stack = new Map<number, number>();
    let seg = 0;
    this.splats.forEach((s, i) => {
      const p = where.get(s.target)!;
      const k = stack.get(s.target) ?? 0;
      stack.set(s.target, k + 1);
      s.position.set(p.x, HITSPLAT_HEIGHT + k * HITSPLAT_STACK, p.z);

      this.m.compose(s.position, facing, this.scale.set(1, 1, 1));
      this.discs.setMatrixAt(i, this.m);
      this.discs.setColorAt(i, this.color.set(s.miss ? MISS_COLOR : HIT_COLOR));

      const digits = digitSegments(s.damage);
      const width = digits.length * DIGIT_W + (digits.length - 1) * DIGIT_GAP;
      const fit = Math.min(1, (RADIUS * 1.5) / width);
      digits.forEach((lit, d) => {
        const dx = (-width / 2 + DIGIT_W / 2 + d * (DIGIT_W + DIGIT_GAP)) * fit;
        for (const idx of lit) {
          const [sx, sy, horizontal] = SEGMENTS[idx];
          this.scale.set(horizontal ? DIGIT_W * fit : SEG_T, horizontal ? SEG_T : (DIGIT_H / 2) * fit, 1);
          this.local.compose(this.offset.set(dx + sx * fit, sy * fit, 0.01), this.noRot, this.scale);
          this.m.compose(s.position, facing, this.scale.set(1, 1, 1)).multiply(this.local);
          this.segs.setMatrixAt(seg++, this.m);
        }
      });
    });
    this.discs.count = this.splats.length;
    this.segs.count = seg;
    this.discs.instanceMatrix.needsUpdate = true;
    if (this.discs.instanceColor) this.discs.instanceColor.needsUpdate = true;
    this.segs.instanceMatrix.needsUpdate = true;
  }
}
