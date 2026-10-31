import * as THREE from 'three';
import { ATTACK_DURATION, BREATH_PERIOD, type CombatAnims, breath, flinchAngle } from './character';
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

/**
 * One box of an NPC model: size, centre (feet at the origin, facing +Z), flat colour, optional tilt around
 * X. A `limb` box (leg or foot) swings around X from its top by `limb` times the instance's limb angle, so
 * opposite signs step in turn; a `wheel` box spins around X through its centre.
 */
interface Box {
  readonly size: readonly [number, number, number];
  readonly at: readonly [number, number, number];
  readonly color: number;
  readonly tiltX?: number;
  readonly limb?: number;
  readonly wheel?: true;
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
    // A trot: diagonal legs step together.
    { size: s([0.08, 0.38, 0.08]), at: s([-0.1, 0.19, 0.3]), color: fur, limb: 1 },
    { size: s([0.08, 0.38, 0.08]), at: s([0.1, 0.19, 0.3]), color: fur, limb: -1 },
    { size: s([0.08, 0.38, 0.08]), at: s([-0.1, 0.19, -0.3]), color: fur, limb: -1 },
    { size: s([0.08, 0.38, 0.08]), at: s([0.1, 0.19, -0.3]), color: fur, limb: 1 },
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
    // Hind feet kick together: a rabbit hops.
    { size: [0.06, 0.05, 0.14], at: [-0.07, 0.025, 0.1], color: 0x8a7862, limb: 1 },
    { size: [0.06, 0.05, 0.14], at: [0.07, 0.025, 0.1], color: 0x8a7862, limb: 1 },
  ],
  [NpcType.Squirrel]: [
    { size: [0.14, 0.2, 0.22], at: [0, 0.14, 0], color: 0x9a5a2c },
    { size: [0.12, 0.12, 0.12], at: [0, 0.28, 0.1], color: 0x9a5a2c },
    { size: [0.1, 0.1, 0.04], at: [0, 0.13, 0.11], color: 0xe2c9a0 },
    { size: [0.03, 0.06, 0.03], at: [-0.04, 0.37, 0.1], color: 0x7a4420 },
    { size: [0.03, 0.06, 0.03], at: [0.04, 0.37, 0.1], color: 0x7a4420 },
    { size: [0.14, 0.34, 0.1], at: [0, 0.24, -0.16], color: 0xb06a34, tiltX: -0.3 },
    { size: [0.04, 0.06, 0.1], at: [-0.05, 0.03, 0.03], color: 0x7a4420, limb: 1 },
    { size: [0.04, 0.06, 0.1], at: [0.05, 0.03, 0.03], color: 0x7a4420, limb: -1 },
  ],
  [NpcType.Crow]: [
    { size: [0.16, 0.16, 0.3], at: [0, 0.22, 0], color: BLACK },
    { size: [0.12, 0.12, 0.12], at: [0, 0.34, 0.15], color: BLACK },
    { size: [0.04, 0.04, 0.1], at: [0, 0.33, 0.25], color: 0x55504a },
    { size: [0.03, 0.18, 0.24], at: [-0.095, 0.24, -0.02], color: 0x2c2c38 },
    { size: [0.03, 0.18, 0.24], at: [0.095, 0.24, -0.02], color: 0x2c2c38 },
    { size: [0.1, 0.03, 0.14], at: [0, 0.2, -0.2], color: 0x2c2c38, tiltX: -0.3 },
    { size: [0.02, 0.14, 0.02], at: [-0.04, 0.07, 0], color: 0x55504a, limb: 1 },
    { size: [0.02, 0.14, 0.02], at: [0.04, 0.07, 0], color: 0x55504a, limb: -1 },
  ],
  [NpcType.Coyote]: quadruped(1, 0xb08a5a, 0xd8c3a0, 0x6e5636),
  [NpcType.RogueDeliveryRobot]: [
    { size: [0.5, 0.36, 0.64], at: [0, 0.38, 0], color: WHITE },
    { size: [0.52, 0.06, 0.66], at: [0, 0.59, 0], color: 0x3a3a44 },
    { size: [0.3, 0.06, 0.02], at: [0, 0.48, 0.33], color: 0xd02a2a },
    { size: [0.1, 0.12, 0.12], at: [-0.27, 0.1, 0.22], color: BLACK, wheel: true },
    { size: [0.1, 0.12, 0.12], at: [0.27, 0.1, 0.22], color: BLACK, wheel: true },
    { size: [0.1, 0.12, 0.12], at: [-0.27, 0.1, 0], color: BLACK, wheel: true },
    { size: [0.1, 0.12, 0.12], at: [0.27, 0.1, 0], color: BLACK, wheel: true },
    { size: [0.1, 0.12, 0.12], at: [-0.27, 0.1, -0.22], color: BLACK, wheel: true },
    { size: [0.1, 0.12, 0.12], at: [0.27, 0.1, -0.22], color: BLACK, wheel: true },
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

/** How a model moves while walking. All of it is 0 (the rest pose) when the NPC stands still. */
export interface Gait {
  /** Step-cycle phase advance per metre walked, in radians: short legs step faster. */
  readonly stride: number;
  /** Peak swing of the `limb` boxes, in radians; 0 for types without legs. */
  readonly swing: number;
  /** Wheel spin per metre walked, in radians (1 / wheel radius); 0 without wheels. */
  readonly wheel: number;
  /** Peak rise of the whole body on each step, in metres. */
  readonly bob: number;
  /** Peak forward-and-back rock of the whole body from the feet, in radians. */
  readonly tilt: number;
}

export const NPC_GAITS: Readonly<Record<number, Gait>> = {
  [NpcType.Rabbit]: { stride: 9, swing: 0.8, wheel: 0, bob: 0.08, tilt: 0.12 },
  [NpcType.Squirrel]: { stride: 14, swing: 0.7, wheel: 0, bob: 0.03, tilt: 0 },
  [NpcType.Crow]: { stride: 16, swing: 0.6, wheel: 0, bob: 0.02, tilt: 0.06 },
  [NpcType.Coyote]: { stride: 6, swing: 0.6, wheel: 0, bob: 0.025, tilt: 0 },
  // No legs: the robot rolls on its wheels, bobbing and rocking a little on the paving.
  [NpcType.RogueDeliveryRobot]: { stride: 3, swing: 0, wheel: 1 / 0.06, bob: 0.015, tilt: 0.04 },
  [NpcType.CoyoteAlpha]: { stride: 4.5, swing: 0.6, wheel: 0, bob: 0.03, tilt: 0 },
  [GENERIC_NPC]: { stride: 5, swing: 0, wheel: 0, bob: 0.05, tilt: 0.08 },
};

/** How much a standing NPC swells at the top of a breath: height by this fraction, width by half of it. */
export const NPC_BREATH_AMOUNT = 0.05;
/** A jump further than this between frames (m) is a spawn or snap, not a step: the NPC is drawn at rest. */
export const NPC_TELEPORT = 2;
/** How far an attack lunge carries the body forward, as a fraction of the model's height. */
export const NPC_LUNGE = 0.35;
/** How far the body pitches forward at the peak of a lunge, in radians. */
export const NPC_LUNGE_PITCH = 0.25;

function smooth(u: number): number {
  return u * u * (3 - 2 * u);
}

/** 0..1 lunge `elapsed` seconds into an attack: a quick strike forward and an easier draw back; 0 outside. */
export function lungeAmount(elapsed: number): number {
  if (!(elapsed > 0 && elapsed < ATTACK_DURATION)) return 0;
  const u = elapsed / ATTACK_DURATION;
  return u < 0.3 ? smooth(u / 0.3) : 1 - smooth((u - 0.3) / 0.7);
}

const heights = new Map<number, number>();

/** Height of a model's top, in metres. */
export function npcHeight(key: number): number {
  const k = npcModelKey(key);
  let h = heights.get(k);
  if (h === undefined) {
    h = Math.max(...NPC_MODELS[k].map((b) => b.at[1] + b.size[1] / 2));
    heights.set(k, h);
  }
  return h;
}

/** One NPC's pose this frame, applied to its instance (see Npcs.update). Every field is 0 at rest. */
export interface NpcPose {
  /** Angle for the limb shader: the legs' swing, or the wheels' spin. */
  readonly limb: number;
  /** Rise of the whole body, in metres. */
  readonly lift: number;
  /** Pitch of the whole body from the feet around its own X axis; positive leans forward. */
  readonly pitch: number;
  /** Shift of the whole body along its heading, in metres. */
  readonly forward: number;
  /** -1..1 breath (from `breath`): scales the body around its feet. */
  readonly breath: number;
}

export const NPC_REST: NpcPose = { limb: 0, lift: 0, pitch: 0, forward: 0, breath: 0 };

/**
 * The pose of an NPC of model `key`: `phase` is its step cycle (0 when standing), `b` its breath, and
 * `attack` / `struck` the seconds since it attacked and was last the target of a hit or miss.
 */
export function npcPose(key: number, phase: number, b: number, attack?: number, struck?: number): NpcPose {
  const k = npcModelKey(key);
  const g = NPC_GAITS[k];
  const lunge = attack === undefined ? 0 : lungeAmount(attack);
  const flinch = struck === undefined ? 0 : flinchAngle(struck);
  const step = Math.sin(phase);
  const limb = g.wheel > 0 ? ((phase / g.stride) * g.wheel) % (Math.PI * 2) : g.swing * step;
  return {
    limb,
    lift: g.bob * Math.abs(step),
    pitch: g.tilt * step + NPC_LUNGE_PITCH * lunge + flinch,
    forward: NPC_LUNGE * npcHeight(k) * lunge,
    breath: b,
  };
}

/**
 * Vertex shader for limb boxes. Each vertex carries its box's pivot and limb weight (`npcLimb`), each
 * instance its limb angle (`npcSwing`); a vertex turns around X through its pivot by weight times angle.
 * Body vertices have weight 0 and stay put, so a model stays one merged geometry and one draw call.
 */
export const NPC_LIMB_GLSL = {
  common: /* glsl */ `#include <common>
attribute vec4 npcLimb;
attribute float npcSwing;
mat3 npcLimbRotation() {
  float a = npcLimb.w * npcSwing;
  float c = cos(a);
  float s = sin(a);
  return mat3(1.0, 0.0, 0.0, 0.0, c, s, 0.0, -s, c);
}`,
  normal: /* glsl */ `#include <beginnormal_vertex>
objectNormal = npcLimbRotation() * objectNormal;`,
  position: /* glsl */ `#include <begin_vertex>
transformed = npcLimb.xyz + npcLimbRotation() * (transformed - npcLimb.xyz);`,
};

/** Adds the limb shader to a material (the colour material, and the depth material for sun shadows). */
export function withLimbShader<M extends THREE.Material>(material: M): M {
  material.onBeforeCompile = (shader) => {
    shader.vertexShader = shader.vertexShader
      .replace('#include <common>', NPC_LIMB_GLSL.common)
      .replace('#include <beginnormal_vertex>', NPC_LIMB_GLSL.normal)
      .replace('#include <begin_vertex>', NPC_LIMB_GLSL.position);
  };
  material.customProgramCacheKey = () => 'npc-limbs';
  return material;
}

/** Where the limb shader puts point `p` of a vertex with `npcLimb` = `limb` at limb angle `angle` (the CPU twin). */
export function limbPoint(limb: readonly [number, number, number, number], angle: number, p: THREE.Vector3): THREE.Vector3 {
  const pivot = new THREE.Vector3(limb[0], limb[1], limb[2]);
  return p.clone().sub(pivot).applyAxisAngle(new THREE.Vector3(1, 0, 0), limb[3] * angle).add(pivot);
}

/** One merged, vertex-coloured geometry for a model, so each type is a single draw call. */
export function makeNpcGeometry(key: number): THREE.BufferGeometry {
  const boxes = NPC_MODELS[npcModelKey(key)];
  const pos: number[] = [];
  const nor: number[] = [];
  const col: number[] = [];
  const limb: number[] = [];
  const c = new THREE.Color();
  const m = new THREE.Matrix4();
  for (const b of boxes) {
    const g = new THREE.BoxGeometry(...b.size).toNonIndexed();
    m.makeRotationX(b.tiltX ?? 0).setPosition(...b.at);
    g.applyMatrix4(m);
    c.setHex(b.color);
    // Legs swing from their top; wheels spin through their centre.
    const pivot = b.wheel ? b.at : b.limb ? [b.at[0], b.at[1] + b.size[1] / 2, b.at[2]] : [0, 0, 0];
    const weight = b.wheel ? 1 : (b.limb ?? 0);
    const p = g.getAttribute('position');
    const n = g.getAttribute('normal');
    for (let i = 0; i < p.count; i++) {
      pos.push(p.getX(i), p.getY(i), p.getZ(i));
      nor.push(n.getX(i), n.getY(i), n.getZ(i));
      col.push(c.r, c.g, c.b);
      limb.push(pivot[0], pivot[1], pivot[2], weight);
    }
    g.dispose();
  }
  const out = new THREE.BufferGeometry();
  out.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
  out.setAttribute('normal', new THREE.Float32BufferAttribute(nor, 3));
  out.setAttribute('color', new THREE.Float32BufferAttribute(col, 3));
  out.setAttribute('npcLimb', new THREE.Float32BufferAttribute(limb, 4));
  return out;
}

/** Step cycle of one drawn NPC, kept between frames to tell walking from standing. */
interface Track {
  x: number;
  z: number;
  phase: number;
}

/**
 * Draws NPCs: one InstancedMesh per model, so draw calls grow with types in view, not NPC count. Breathing,
 * bobbing, tilting, lunges and flinches go in the instance matrix; leg swing and wheel spin go in a
 * per-instance `npcSwing` angle that the limb shader applies.
 */
export class Npcs {
  readonly group = new THREE.Group();
  private readonly material = withLimbShader(new THREE.MeshLambertMaterial({ vertexColors: true, flatShading: true }));
  private readonly depthMaterial = withLimbShader(new THREE.MeshDepthMaterial());
  private readonly meshes = new Map<number, THREE.InstancedMesh>();
  private readonly tracks = new Map<number, Track>();
  private readonly m = new THREE.Matrix4();
  private readonly q = new THREE.Quaternion();
  private readonly tilt = new THREE.Quaternion();
  private readonly pos = new THREE.Vector3();
  private readonly scale = new THREE.Vector3();
  private readonly axisX = new THREE.Vector3(1, 0, 0);
  private readonly axisY = new THREE.Vector3(0, 1, 0);

  constructor() {
    this.group.name = 'npcs';
  }

  /** The InstancedMesh for a model key, if one has been made. */
  meshFor(key: number): THREE.InstancedMesh | undefined {
    return this.meshes.get(npcModelKey(key));
  }

  /**
   * `time` is the render clock in seconds, which drives breathing; `combat`, when given, supplies attack
   * lunges and flinches.
   */
  update(npcs: readonly NpcView[], time = 0, combat?: CombatAnims): void {
    const byKey = new Map<number, NpcView[]>();
    for (const n of npcs) {
      const key = npcModelKey(n.npcType);
      let list = byKey.get(key);
      if (!list) byKey.set(key, (list = []));
      list.push(n);
    }
    const seen = new Set<number>();
    for (const [key, list] of byKey) {
      const mesh = this.ensure(key, list.length);
      const swing = mesh.geometry.getAttribute('npcSwing') as THREE.InstancedBufferAttribute;
      const stride = NPC_GAITS[key].stride;
      for (let i = 0; i < list.length; i++) {
        const n = list[i];
        seen.add(n.id);
        let t = this.tracks.get(n.id);
        if (!t) this.tracks.set(n.id, (t = { x: n.x, z: n.z, phase: 0 }));
        const d = Math.hypot(n.x - t.x, n.z - t.z);
        const moving = d > 1e-4 && d < NPC_TELEPORT;
        t.phase = moving ? t.phase + d * stride : 0;
        t.x = n.x;
        t.z = n.z;
        // Offset by id so a herd doesn't breathe in step.
        const b = moving ? 0 : breath(time, (n.id * 0.618) % BREATH_PERIOD);
        const pose = npcPose(key, t.phase, b, combat?.attackElapsed(n.id, time), combat?.struckElapsed(n.id, time));
        this.q.setFromAxisAngle(this.axisY, n.heading).multiply(this.tilt.setFromAxisAngle(this.axisX, pose.pitch));
        this.pos.set(n.x + Math.sin(n.heading) * pose.forward, pose.lift, n.z + Math.cos(n.heading) * pose.forward);
        const s = NPC_BREATH_AMOUNT * pose.breath;
        this.m.compose(this.pos, this.q, this.scale.set(1 + s / 2, 1 + s, 1 + s / 2));
        mesh.setMatrixAt(i, this.m);
        swing.setX(i, pose.limb);
      }
      mesh.count = list.length;
      mesh.instanceMatrix.needsUpdate = true;
      swing.needsUpdate = true;
    }
    for (const [key, mesh] of this.meshes) if (!byKey.has(key)) mesh.count = 0;
    for (const id of this.tracks.keys()) if (!seen.has(id)) this.tracks.delete(id);
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
    geometry.setAttribute('npcSwing', new THREE.InstancedBufferAttribute(new Float32Array(cap), 1));
    mesh = new THREE.InstancedMesh(geometry, this.material, cap);
    mesh.name = `npc-${key}`;
    mesh.count = 0;
    mesh.frustumCulled = false;
    mesh.castShadow = true;
    // The sun shadow swings the legs too.
    mesh.customDepthMaterial = this.depthMaterial;
    this.group.add(mesh);
    this.meshes.set(key, mesh);
    return mesh;
  }
}
