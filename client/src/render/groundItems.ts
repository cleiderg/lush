import * as THREE from 'three';
import type { GroundItemView } from './worldView';

/** Initial marker slots; the pool doubles (still one InstancedMesh) if more items are in view. */
export const GROUND_ITEM_CAPACITY = 256;
/** Marker size in metres: a small sack-like box sitting on the ground. */
const SIZE = 0.35;
/** A few flat colours so neighbouring item kinds are easy to tell apart. */
const PALETTE = [0xd4a017, 0xb8b8c0, 0x8b5a2b, 0x4f8fd0, 0xc04040, 0x6fbf4f, 0xe8e0c8, 0x8050a0];

/** Flat colour for an item id; the same id always gets the same colour. */
export function groundItemColor(itemId: number): number {
  return PALETTE[(itemId >>> 0) % PALETTE.length];
}

/** Draws ground items as one InstancedMesh with shared geometry and material, so cost does not grow with item count. */
export class GroundItems {
  readonly group = new THREE.Group();
  private readonly geometry = new THREE.OctahedronGeometry(SIZE / 2).scale(1, 0.7, 1);
  private readonly material = new THREE.MeshLambertMaterial({ color: 0xffffff, flatShading: true });
  private mesh: THREE.InstancedMesh;
  private readonly m = new THREE.Matrix4();
  private readonly q = new THREE.Quaternion();
  private readonly pos = new THREE.Vector3();
  private readonly one = new THREE.Vector3(1, 1, 1);
  private readonly axisY = new THREE.Vector3(0, 1, 0);
  private readonly color = new THREE.Color();

  constructor(capacity = GROUND_ITEM_CAPACITY) {
    this.group.name = 'groundItems';
    this.mesh = this.makeMesh(capacity);
  }

  /** The single InstancedMesh holding every marker; instance i is items[i]. */
  get markers(): THREE.InstancedMesh {
    return this.mesh;
  }

  update(items: readonly GroundItemView[]): void {
    if (items.length > this.mesh.instanceMatrix.count) {
      let cap = this.mesh.instanceMatrix.count;
      while (cap < items.length) cap *= 2;
      this.group.remove(this.mesh);
      this.mesh.dispose();
      this.mesh = this.makeMesh(cap);
    }
    const y = (SIZE / 2) * 0.7;
    for (let i = 0; i < items.length; i++) {
      const it = items[i];
      // Turn each marker by its id so piles of the same item don't look stamped.
      this.q.setFromAxisAngle(this.axisY, ((it.id * 2654435761) >>> 0) / 4294967296 * Math.PI);
      this.pos.set(it.x, y, it.z);
      this.m.compose(this.pos, this.q, this.one);
      this.mesh.setMatrixAt(i, this.m);
      this.mesh.setColorAt(i, this.color.setHex(groundItemColor(it.itemId)));
    }
    this.mesh.count = items.length;
    this.mesh.instanceMatrix.needsUpdate = true;
    if (this.mesh.instanceColor) this.mesh.instanceColor.needsUpdate = true;
  }

  private makeMesh(capacity: number): THREE.InstancedMesh {
    const mesh = new THREE.InstancedMesh(this.geometry, this.material, capacity);
    mesh.name = 'groundItemMarkers';
    mesh.count = 0;
    mesh.frustumCulled = false;
    // Allocate instance colours up front so the shader variant never changes.
    mesh.setColorAt(0, this.color.setHex(PALETTE[0]));
    this.group.add(mesh);
    return mesh;
  }
}
