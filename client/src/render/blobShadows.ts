import * as THREE from 'three';
import { NPC_MODELS, npcModelKey } from './npcs';

/** Shadow radius under a player character, in metres. */
export const PLAYER_SHADOW_RADIUS = 0.45;
/** Height above the ground, so the shadow doesn't fight the ground plane. */
export const SHADOW_Y = 0.02;
/** Darkness at the centre of the shadow (0..1). */
export const SHADOW_OPACITY = 0.35;
/** Initial instance slots; the pool doubles (still one InstancedMesh) past this. */
export const SHADOW_CAPACITY = 128;

const SEGMENTS = 16;

/**
 * Unit-radius disc lying flat, dark in the middle and fading to clear at the rim through vertex
 * alpha, so it reads as a soft shadow without a texture.
 */
export function makeShadowGeometry(): THREE.BufferGeometry {
  const rings: [radius: number, alpha: number][] = [
    [0.55, 1],
    [1, 0],
  ];
  const pos = [0, 0, 0];
  const col = [0, 0, 0, 1];
  for (const [r, a] of rings) {
    for (let i = 0; i < SEGMENTS; i++) {
      const t = (i / SEGMENTS) * Math.PI * 2;
      pos.push(Math.cos(t) * r, 0, Math.sin(t) * r);
      col.push(0, 0, 0, a);
    }
  }
  const index: number[] = [];
  const ring = (k: number, i: number) => 1 + k * SEGMENTS + (i % SEGMENTS);
  for (let i = 0; i < SEGMENTS; i++) {
    // Counter-clockwise seen from above, so the face points up.
    index.push(0, ring(0, i + 1), ring(0, i));
    index.push(ring(0, i), ring(0, i + 1), ring(1, i + 1));
    index.push(ring(0, i), ring(1, i + 1), ring(1, i));
  }
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
  g.setAttribute('color', new THREE.Float32BufferAttribute(col, 4));
  g.setIndex(index);
  return g;
}

const npcRadii = new Map<number, number>();

/** Shadow radius for an NPC type: a little wider than the model's footprint. */
export function npcShadowRadius(npcType: number): number {
  const key = npcModelKey(npcType);
  let r = npcRadii.get(key);
  if (r === undefined) {
    let extent = 0;
    for (const b of NPC_MODELS[key]) {
      extent = Math.max(extent, Math.abs(b.at[0]) + b.size[0] / 2, Math.abs(b.at[2]) + b.size[2] / 2);
    }
    r = Math.max(0.15, extent * 0.9);
    npcRadii.set(key, r);
  }
  return r;
}

export interface ShadowEntry {
  readonly x: number;
  readonly z: number;
  readonly radius: number;
}

/** Soft round shadows under every drawn player and NPC, all in one InstancedMesh (one draw call). */
export class BlobShadows {
  readonly group = new THREE.Group();
  private readonly geometry = makeShadowGeometry();
  private readonly material = new THREE.MeshBasicMaterial({
    color: 0xffffff,
    vertexColors: true,
    transparent: true,
    opacity: SHADOW_OPACITY,
    depthWrite: false,
    polygonOffset: true,
    polygonOffsetFactor: -1,
    polygonOffsetUnits: -1,
  });
  mesh: THREE.InstancedMesh;
  private readonly m = new THREE.Matrix4();
  private readonly pos = new THREE.Vector3();
  private readonly scale = new THREE.Vector3();
  private readonly q = new THREE.Quaternion();

  constructor() {
    this.group.name = 'blobShadows';
    this.mesh = this.make(SHADOW_CAPACITY);
  }

  private make(capacity: number): THREE.InstancedMesh {
    const mesh = new THREE.InstancedMesh(this.geometry, this.material, capacity);
    mesh.name = 'blobShadows';
    mesh.count = 0;
    mesh.frustumCulled = false;
    // Drawn before characters' transparent bits; it never writes depth, so order only matters among overlays.
    mesh.renderOrder = 1;
    this.group.add(mesh);
    return mesh;
  }

  update(entries: readonly ShadowEntry[]): void {
    let cap = this.mesh.instanceMatrix.count;
    if (entries.length > cap) {
      while (cap < entries.length) cap *= 2;
      this.group.remove(this.mesh);
      this.mesh.dispose();
      this.mesh = this.make(cap);
    }
    for (let i = 0; i < entries.length; i++) {
      const e = entries[i];
      this.m.compose(this.pos.set(e.x, SHADOW_Y, e.z), this.q, this.scale.set(e.radius, 1, e.radius));
      this.mesh.setMatrixAt(i, this.m);
    }
    this.mesh.count = entries.length;
    this.mesh.instanceMatrix.needsUpdate = true;
  }
}
