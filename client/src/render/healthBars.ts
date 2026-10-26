import * as THREE from 'three';
import { CHARACTER_HEIGHT } from './character';

export const BAR_WIDTH = 0.6;
export const BAR_HEIGHT = 0.08;
/** Bar centre height above the feet. */
export const BAR_Y = CHARACTER_HEIGHT + 0.3;
/** One more than the remote cap, for the local player. */
export const MAX_BARS = 101;

export interface BarEntry {
  readonly x: number;
  readonly z: number;
  /** 0..1 */
  readonly ratio: number;
}

/**
 * Health bars over entities that are hurt (ratio below 1). Two InstancedMeshes (red background,
 * green fill), billboarded to the camera, so they cost at most two draw calls in total.
 */
export class HealthBars {
  readonly group = new THREE.Group();
  readonly back: THREE.InstancedMesh;
  readonly fill: THREE.InstancedMesh;
  private readonly m = new THREE.Matrix4();
  private readonly pos = new THREE.Vector3();
  private readonly scale = new THREE.Vector3();
  private readonly right = new THREE.Vector3();

  constructor(capacity = MAX_BARS) {
    const plane = () => new THREE.PlaneGeometry(BAR_WIDTH, BAR_HEIGHT);
    const material = (color: number) =>
      new THREE.MeshBasicMaterial({ color, depthTest: false, side: THREE.DoubleSide });
    this.back = new THREE.InstancedMesh(plane(), material(0xc0392b), capacity);
    // Fill grows from its left edge: origin at the left end.
    this.fill = new THREE.InstancedMesh(plane().translate(BAR_WIDTH / 2, 0, 0), material(0x2ecc40), capacity);
    for (const mesh of [this.back, this.fill]) {
      mesh.count = 0;
      mesh.frustumCulled = false;
      this.group.add(mesh);
    }
    this.back.name = 'healthBack';
    this.fill.name = 'healthFill';
    this.back.renderOrder = 10;
    this.fill.renderOrder = 11;
  }

  /** Draws a bar for each entry with ratio below 1, facing `facing` (the camera's world rotation). */
  update(entries: readonly BarEntry[], facing: THREE.Quaternion): void {
    const capacity = this.back.instanceMatrix.count;
    this.right.set(1, 0, 0).applyQuaternion(facing);
    let n = 0;
    for (const e of entries) {
      if (n >= capacity) break;
      if (!(e.ratio < 1)) continue;
      const ratio = Math.max(0, e.ratio);
      this.pos.set(e.x, BAR_Y, e.z);
      this.m.compose(this.pos, facing, this.scale.set(1, 1, 1));
      this.back.setMatrixAt(n, this.m);
      // Fill starts at the bar's left end; renderOrder draws it over the background.
      this.pos.addScaledVector(this.right, -BAR_WIDTH / 2);
      this.m.compose(this.pos, facing, this.scale.set(Math.max(ratio, 1e-3), 1, 1));
      this.fill.setMatrixAt(n, this.m);
      n++;
    }
    for (const mesh of [this.back, this.fill]) {
      mesh.count = n;
      mesh.instanceMatrix.needsUpdate = true;
    }
  }
}
