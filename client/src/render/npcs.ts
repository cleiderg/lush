import * as THREE from 'three';
import type { NpcView } from './worldView';

/** NPC type ids from server/internal/systems/gamedata/data/npcs.json. */
export const NpcType = {
  Rabbit: 1,
  Squirrel: 2,
  Crow: 3,
  Coyote: 4,
  RogueDeliveryRobot: 5,
  CoyoteAlpha: 6,
} as const;

/** Model key for a type id the client has no model for. */
export const GENERIC_NPC = 0;
/** Initial instance slots per type; a type's pool doubles (still one InstancedMesh) past this. */
export const NPC_CAPACITY = 32;

/** One box of an NPC model: size, centre (feet at the origin, facing +Z), flat colour, optional tilt around X. */
interface Box {
  readonly size: readonly [number, number, number];
  readonly at: readonly [number, number, number];
  readonly color: number;
  readonly tiltX?: number;
}

const WHITE = 0xf2f2ee;
const BLACK = 0x1c1c22;

function quadruped(scale: number, fur: number, belly: number, dark: number): Box[] {
  const s = (v: readonly [number, number, number]) => v.map((n) => n * scale) as unknown as [number, number, number];
  return [
    { size: s([0.32, 0.3, 0.8]), at: s([0, 0.52, 0]), color: fur },
    { size: s([0.26, 0.08, 0.6]), at: s([0, 0.37, 0]), color: belly },
    { size: s([0.26, 0.26, 0.28]), at: s([0, 0.72, 0.5]), color: fur },
    { size: s([0.14, 0.12, 0.2]), at: s([0, 0.66, 0.72]), color: belly },
    { size: s([0.05, 0.05, 0.05]), at: s([0, 0.69, 0.83]), color: BLACK },
    { size: s([0.07, 0.14, 0.05]), at: s([-0.08, 0.91, 0.46]), color: dark },
    { size: s([0.07, 0.14, 0.05]), at: s([0.08, 0.91, 0.46]), color: dark },
    { size: s([0.08, 0.38, 0.08]), at: s([-0.1, 0.19, 0.3]), color: fur },
    { size: s([0.08, 0.38, 0.08]), at: s([0.1, 0.19, 0.3]), color: fur },
    { size: s([0.08, 0.38, 0.08]), at: s([-0.1, 0.19, -0.3]), color: fur },
    { size: s([0.08, 0.38, 0.08]), at: s([0.1, 0.19, -0.3]), color: fur },
    { size: s([0.09, 0.09, 0.4]), at: s([0, 0.5, -0.55]), color: dark, tiltX: 0.6 },
  ];
}

/** Box layouts per model key. Low-poly, flat colours, no textures (PRD 5.2). */
export const NPC_MODELS: Readonly<Record<number, readonly Box[]>> = {
  [NpcType.Rabbit]: [
    { size: [0.22, 0.2, 0.32], at: [0, 0.14, 0], color: 0x9c8a74 },
    { size: [0.16, 0.16, 0.16], at: [0, 0.27, 0.17], color: 0x9c8a74 },
    { size: [0.04, 0.18, 0.04], at: [-0.04, 0.42, 0.15], color: 0x8a7862 },
    { size: [0.04, 0.18, 0.04], at: [0.04, 0.42, 0.15], color: 0x8a7862 },
    { size: [0.04, 0.04, 0.03], at: [0, 0.26, 0.26], color: 0xd88a9a },
    { size: [0.08, 0.08, 0.08], at: [0, 0.18, -0.18], color: WHITE },
    { size: [0.06, 0.05, 0.14], at: [-0.07, 0.025, 0.1], color: 0x8a7862 },
    { size: [0.06, 0.05, 0.14], at: [0.07, 0.025, 0.1], color: 0x8a7862 },
  ],
  [NpcType.Squirrel]: [
    { size: [0.14, 0.2, 0.22], at: [0, 0.14, 0], color: 0x9a5a2c },
    { size: [0.12, 0.12, 0.12], at: [0, 0.28, 0.1], color: 0x9a5a2c },
    { size: [0.1, 0.1, 0.04], at: [0, 0.13, 0.11], color: 0xe2c9a0 },
    { size: [0.03, 0.06, 0.03], at: [-0.04, 0.37, 0.1], color: 0x7a4420 },
    { size: [0.03, 0.06, 0.03], at: [0.04, 0.37, 0.1], color: 0x7a4420 },
    { size: [0.14, 0.34, 0.1], at: [0, 0.24, -0.16], color: 0xb06a34, tiltX: -0.3 },
    { size: [0.04, 0.06, 0.1], at: [-0.05, 0.03, 0.03], color: 0x7a4420 },
    { size: [0.04, 0.06, 0.1], at: [0.05, 0.03, 0.03], color: 0x7a4420 },
  ],
  [NpcType.Crow]: [
    { size: [0.16, 0.16, 0.3], at: [0, 0.22, 0], color: BLACK },
    { size: [0.12, 0.12, 0.12], at: [0, 0.34, 0.15], color: BLACK },
    { size: [0.04, 0.04, 0.1], at: [0, 0.33, 0.25], color: 0x55504a },
    { size: [0.03, 0.18, 0.24], at: [-0.095, 0.24, -0.02], color: 0x2c2c38 },
    { size: [0.03, 0.18, 0.24], at: [0.095, 0.24, -0.02], color: 0x2c2c38 },
    { size: [0.1, 0.03, 0.14], at: [0, 0.2, -0.2], color: 0x2c2c38, tiltX: -0.3 },
    { size: [0.02, 0.14, 0.02], at: [-0.04, 0.07, 0], color: 0x55504a },
    { size: [0.02, 0.14, 0.02], at: [0.04, 0.07, 0], color: 0x55504a },
  ],
  [NpcType.Coyote]: quadruped(1, 0xb08a5a, 0xd8c3a0, 0x6e5636),
  [NpcType.RogueDeliveryRobot]: [
    { size: [0.5, 0.36, 0.64], at: [0, 0.38, 0], color: WHITE },
    { size: [0.52, 0.06, 0.66], at: [0, 0.59, 0], color: 0x3a3a44 },
    { size: [0.3, 0.06, 0.02], at: [0, 0.48, 0.33], color: 0xd02a2a },
    { size: [0.1, 0.12, 0.12], at: [-0.27, 0.1, 0.22], color: BLACK },
    { size: [0.1, 0.12, 0.12], at: [0.27, 0.1, 0.22], color: BLACK },
    { size: [0.1, 0.12, 0.12], at: [-0.27, 0.1, 0], color: BLACK },
    { size: [0.1, 0.12, 0.12], at: [0.27, 0.1, 0], color: BLACK },
    { size: [0.1, 0.12, 0.12], at: [-0.27, 0.1, -0.22], color: BLACK },
    { size: [0.1, 0.12, 0.12], at: [0.27, 0.1, -0.22], color: BLACK },
    { size: [0.02, 0.7, 0.02], at: [0.18, 0.97, -0.24], color: 0x8a8a90 },
    { size: [0.02, 0.12, 0.16], at: [0.18, 1.26, -0.17], color: 0xf08020 },
  ],
  [NpcType.CoyoteAlpha]: [
    ...quadruped(1.35, 0x6e5a44, 0xb8a080, 0x3a2e22),
    // Pale mane across the shoulders so the alpha reads at a glance.
    { size: [0.5, 0.32, 0.22], at: [0, 0.8, 0.36], color: 0xcfc4b0 },
  ],
  [GENERIC_NPC]: [
    { size: [0.4, 0.5, 0.4], at: [0, 0.25, 0], color: 0x8a6fb0 },
    { size: [0.24, 0.24, 0.24], at: [0, 0.62, 0.04], color: 0xa58ccb },
    { size: [0.06, 0.06, 0.04], at: [0, 0.64, 0.17], color: BLACK },
  ],
};

/** Model key for an NPC type id: its own model, or the generic one when unknown. */
export function npcModelKey(npcType: number): number {
  return npcType !== GENERIC_NPC && NPC_MODELS[npcType] ? npcType : GENERIC_NPC;
}

/** One merged, vertex-coloured geometry for a model, so each type is a single draw call. */
export function makeNpcGeometry(key: number): THREE.BufferGeometry {
  const boxes = NPC_MODELS[npcModelKey(key)];
  const pos: number[] = [];
  const nor: number[] = [];
  const col: number[] = [];
  const c = new THREE.Color();
  const m = new THREE.Matrix4();
  for (const b of boxes) {
    const g = new THREE.BoxGeometry(...b.size).toNonIndexed();
    m.makeRotationX(b.tiltX ?? 0).setPosition(...b.at);
    g.applyMatrix4(m);
    c.setHex(b.color);
    const p = g.getAttribute('position');
    const n = g.getAttribute('normal');
    for (let i = 0; i < p.count; i++) {
      pos.push(p.getX(i), p.getY(i), p.getZ(i));
      nor.push(n.getX(i), n.getY(i), n.getZ(i));
      col.push(c.r, c.g, c.b);
    }
    g.dispose();
  }
  const out = new THREE.BufferGeometry();
  out.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
  out.setAttribute('normal', new THREE.Float32BufferAttribute(nor, 3));
  out.setAttribute('color', new THREE.Float32BufferAttribute(col, 3));
  return out;
}

/** Draws NPCs: one InstancedMesh per model, so draw calls grow with types in view, not NPC count. */
export class Npcs {
  readonly group = new THREE.Group();
  private readonly material = new THREE.MeshLambertMaterial({ vertexColors: true, flatShading: true });
  private readonly meshes = new Map<number, THREE.InstancedMesh>();
  private readonly m = new THREE.Matrix4();
  private readonly q = new THREE.Quaternion();
  private readonly pos = new THREE.Vector3();
  private readonly one = new THREE.Vector3(1, 1, 1);
  private readonly axisY = new THREE.Vector3(0, 1, 0);

  constructor() {
    this.group.name = 'npcs';
  }

  /** The InstancedMesh for a model key, if one has been made. */
  meshFor(key: number): THREE.InstancedMesh | undefined {
    return this.meshes.get(npcModelKey(key));
  }

  update(npcs: readonly NpcView[]): void {
    const byKey = new Map<number, NpcView[]>();
    for (const n of npcs) {
      const key = npcModelKey(n.npcType);
      let list = byKey.get(key);
      if (!list) byKey.set(key, (list = []));
      list.push(n);
    }
    for (const [key, list] of byKey) {
      const mesh = this.ensure(key, list.length);
      for (let i = 0; i < list.length; i++) {
        const n = list[i];
        this.q.setFromAxisAngle(this.axisY, n.heading);
        this.m.compose(this.pos.set(n.x, 0, n.z), this.q, this.one);
        mesh.setMatrixAt(i, this.m);
      }
      mesh.count = list.length;
      mesh.instanceMatrix.needsUpdate = true;
    }
    for (const [key, mesh] of this.meshes) if (!byKey.has(key)) mesh.count = 0;
  }

  private ensure(key: number, needed: number): THREE.InstancedMesh {
    let mesh = this.meshes.get(key);
    if (mesh && mesh.instanceMatrix.count >= needed) return mesh;
    let cap = mesh ? mesh.instanceMatrix.count : NPC_CAPACITY;
    while (cap < needed) cap *= 2;
    const geometry = mesh ? mesh.geometry : makeNpcGeometry(key);
    if (mesh) {
      this.group.remove(mesh);
      mesh.dispose();
    }
    mesh = new THREE.InstancedMesh(geometry, this.material, cap);
    mesh.name = `npc-${key}`;
    mesh.count = 0;
    mesh.frustumCulled = false;
    mesh.castShadow = true;
    this.group.add(mesh);
    this.meshes.set(key, mesh);
    return mesh;
  }
}
