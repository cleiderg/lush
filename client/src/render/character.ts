import * as THREE from 'three';
import { mergeGeometries } from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import { Animation } from '../protocol';
import { SLOTS, equipmentMaterial, itemColor, slotGeometry, type EquipSlotName } from './equipment';
import type { EntityLookup, EquipmentView } from './worldView';

type Vec3 = readonly [number, number, number];

/** One box of a part, relative to the part's pivot. */
export interface BoxDef {
  readonly size: Vec3;
  readonly at: Vec3;
}

/** One flat-coloured piece of the character. `pivot` is the joint the part swings around. */
export interface PartDef {
  readonly name: string;
  /** Boxes merged into the part's single geometry, relative to the pivot. */
  readonly boxes: readonly BoxDef[];
  /** Joint position relative to the character origin (feet, centered). */
  readonly pivot: Vec3;
  readonly color: number;
  /** Walk swing multiplier around X (0 = rigid). */
  readonly swing: number;
  /** How the part follows idle breathing: the torso swells, parts above it ride up on it. */
  readonly breath: 'swell' | 'ride' | 'none';
}

const SKIN = 0xf1c27d;
const SHIRT = 0x3b6fb6;
const PANTS = 0x4a4a5a;
const HAIR = 0x5a3a22;
const EYES = 0x1c1c22;

const HIP_Y = 0.42;
const TORSO_H = 0.54;
const NECK_Y = HIP_Y + TORSO_H;
const HEAD: Vec3 = [0.6, 0.56, 0.56];
const SHOULDER_Y = NECK_Y - 0.06;
const ARM_LEN = 0.46;

/**
 * Chunky, big-headed character (docs/specs/visual-style.md, Characters): the head is about a third of
 * the height. Faces +Z. Eyes and hair are their own parts so they keep their own flat colours.
 */
export const PARTS: readonly PartDef[] = [
  {
    name: 'torso', pivot: [0, HIP_Y, 0], color: SHIRT, swing: 0, breath: 'swell',
    boxes: [{ size: [0.56, TORSO_H, 0.36], at: [0, TORSO_H / 2, 0] }],
  },
  {
    name: 'head', pivot: [0, NECK_Y, 0], color: SKIN, swing: 0, breath: 'ride',
    boxes: [{ size: HEAD, at: [0, HEAD[1] / 2, 0] }],
  },
  {
    // Two dark eyes standing just proud of the face.
    name: 'eyes', pivot: [0, NECK_Y, 0], color: EYES, swing: 0, breath: 'ride',
    boxes: [
      { size: [0.08, 0.12, 0.03], at: [-0.13, 0.27, HEAD[2] / 2 + 0.01] },
      { size: [0.08, 0.12, 0.03], at: [0.13, 0.27, HEAD[2] / 2 + 0.01] },
    ],
  },
  {
    // A cap of hair over the top, a fringe at the front and a longer piece down the back.
    name: 'hair', pivot: [0, NECK_Y, 0], color: HAIR, swing: 0, breath: 'ride',
    boxes: [
      { size: [0.64, 0.1, 0.6], at: [0, HEAD[1], 0] },
      { size: [0.64, 0.1, 0.06], at: [0, HEAD[1] - 0.07, HEAD[2] / 2 + 0.01] },
      { size: [0.64, 0.4, 0.08], at: [0, HEAD[1] - 0.2, -HEAD[2] / 2 - 0.02] },
    ],
  },
  {
    // Sleeve with the hand at the bottom end (see HAND_Y).
    name: 'armL', pivot: [-0.37, SHOULDER_Y, 0], color: SHIRT, swing: 1, breath: 'ride',
    boxes: [{ size: [0.17, ARM_LEN, 0.2], at: [0, -ARM_LEN / 2 + 0.04, 0] }],
  },
  {
    name: 'armR', pivot: [0.37, SHOULDER_Y, 0], color: SHIRT, swing: -1, breath: 'ride',
    boxes: [{ size: [0.17, ARM_LEN, 0.2], at: [0, -ARM_LEN / 2 + 0.04, 0] }],
  },
  {
    name: 'legL', pivot: [-0.13, HIP_Y, 0], color: PANTS, swing: -1, breath: 'none',
    boxes: [{ size: [0.22, HIP_Y, 0.26], at: [0, -HIP_Y / 2, 0] }],
  },
  {
    name: 'legR', pivot: [0.13, HIP_Y, 0], color: PANTS, swing: 1, breath: 'none',
    boxes: [{ size: [0.22, HIP_Y, 0.26], at: [0, -HIP_Y / 2, 0] }],
  },
];

/** Top of the hair above the feet. Overhead text and bars sit above this. */
export const CHARACTER_HEIGHT = NECK_Y + HEAD[1] + 0.05;
/** The hand's centre below an arm's pivot (the bottom of the sleeve). */
export const HAND_Y = -ARM_LEN + 0.12;

export const WALK_SPEED_RAD_PER_UNIT = 4; // phase advance per world unit walked
export const WALK_SWING = 0.7; // radians

export function swingAngle(phase: number, part: PartDef): number {
  return Math.sin(phase) * WALK_SWING * part.swing;
}

export const WAVE_ARM = 'armR';
export const WAVE_RAISE = 2.6; // radians around Z: arm up and out

/** Roll (around Z) a part has for the given animation id; 0 is the idle pose. */
export function poseRoll(part: PartDef, animation: number): number {
  return animation === Animation.Wave && part.name === WAVE_ARM ? WAVE_RAISE : 0;
}

/** One full breath, in seconds. */
export const BREATH_PERIOD = 3.2;
/** How much the torso widens at the top of a breath (fraction of its rest size). */
export const BREATH_AMOUNT = 0.04;

/** -1..1 breathing phase at `time` seconds; 0 at whole periods. `offset` desynchronises characters. */
export function breath(time: number, offset = 0): number {
  return Math.sin(((time + offset) / BREATH_PERIOD) * Math.PI * 2);
}

/** Scale and upward shift of a part for breath `b` (from `breath`). Rest is scale 1, lift 0. */
export function breathPose(part: PartDef, b: number, scale: THREE.Vector3): number {
  if (part.breath === 'swell') {
    scale.set(1 + BREATH_AMOUNT * b, 1 + (BREATH_AMOUNT / 2) * b, 1 + BREATH_AMOUNT * b);
    return 0;
  }
  scale.set(1, 1, 1);
  return part.breath === 'ride' ? TORSO_H * (BREATH_AMOUNT / 2) * b : 0;
}

const geometries = new Map<string, THREE.BufferGeometry>();
const materials = new Map<string, THREE.MeshLambertMaterial>();

/** The part's geometry, shared by the local player and every remote player. */
export function makePartGeometry(part: PartDef): THREE.BufferGeometry {
  let g = geometries.get(part.name);
  if (!g) {
    const boxes = part.boxes.map((b) => new THREE.BoxGeometry(...b.size).translate(...b.at));
    g = boxes.length === 1 ? boxes[0] : mergeGeometries(boxes)!;
    geometries.set(part.name, g);
  }
  return g;
}

/** The part's flat material, shared like its geometry. */
export function makePartMaterial(part: PartDef): THREE.MeshLambertMaterial {
  let m = materials.get(part.name);
  if (!m) {
    m = new THREE.MeshLambertMaterial({ color: part.color, flatShading: true });
    materials.set(part.name, m);
  }
  return m;
}

/**
 * The local player's skeleton: one joint group per part, posed every frame. Its body is drawn by
 * RemotePlayers as one more instance of the same part meshes, so local and remote players share one
 * model and the local player adds no draw calls. Equipment hangs off the joints as plain meshes.
 */
export class Character {
  readonly root = new THREE.Group();
  private readonly joints: { part: PartDef; node: THREE.Group }[] = [];
  /** Attachment mesh per equipment slot; meshes share geometry and material with every other wearer. */
  readonly attachments = new Map<EquipSlotName, THREE.Mesh>();
  private phase = 0;

  constructor() {
    for (const part of PARTS) {
      const node = new THREE.Group();
      node.name = part.name;
      node.position.set(...part.pivot);
      this.root.add(node);
      this.joints.push({ part, node });
    }
  }

  /** Joint groups in PARTS order. */
  get nodes(): readonly THREE.Group[] {
    return this.joints.map((j) => j.node);
  }

  /** The joint group of a part, by PARTS name. */
  joint(name: string): THREE.Group | undefined {
    return this.joints.find((j) => j.part.name === name)?.node;
  }

  /** Shows one attachment per worn slot; `undefined` removes them all. Cheap when nothing changed. */
  setEquipment(eq: EquipmentView | undefined, lookup?: EntityLookup): void {
    for (const def of SLOTS) {
      const itemId = eq ? eq[def.slot] : 0;
      const current = this.attachments.get(def.slot);
      if (itemId === 0) {
        if (current) {
          current.removeFromParent();
          this.attachments.delete(def.slot);
        }
        continue;
      }
      const material = equipmentMaterial(itemColor(itemId, lookup));
      if (current) {
        current.material = material;
        continue;
      }
      const mesh = new THREE.Mesh(slotGeometry(def), material);
      mesh.name = `equip:${def.slot}`;
      mesh.castShadow = true;
      this.joint(def.part)!.add(mesh);
      this.attachments.set(def.slot, mesh);
    }
  }

  /**
   * `distance` is how far the character moved since the last call; `time` is the render clock in
   * seconds, which drives idle breathing while standing.
   */
  update(x: number, z: number, heading: number, distance: number, animation = 0, time = 0): void {
    this.root.position.set(x, 0, z);
    this.root.rotation.y = heading;
    const moving = distance > 1e-4;
    this.phase = moving ? this.phase + distance * WALK_SPEED_RAD_PER_UNIT : 0;
    const b = moving ? 0 : breath(time);
    for (const { part, node } of this.joints) {
      const roll = poseRoll(part, animation);
      node.rotation.x = roll !== 0 ? 0 : swingAngle(this.phase, part);
      node.rotation.z = roll;
      node.position.y = part.pivot[1] + breathPose(part, b, node.scale);
    }
    this.root.updateMatrixWorld(true);
  }
}
