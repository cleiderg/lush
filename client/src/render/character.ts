import * as THREE from 'three';
import { Animation } from '../protocol';
import { SLOTS, equipmentMaterial, itemColor, slotGeometry, type EquipSlotName } from './equipment';
import type { EntityLookup, EquipmentView } from './worldView';

/** One box of the slim character. `pivot` is the joint the part swings around. */
export interface PartDef {
  readonly name: string;
  readonly size: readonly [number, number, number];
  /** Joint position relative to the character origin (feet, centered). */
  readonly pivot: readonly [number, number, number];
  /** Box center relative to the pivot. */
  readonly offset: readonly [number, number, number];
  readonly color: number;
  /** Walk swing multiplier around X (0 = rigid). */
  readonly swing: number;
}

const SKIN = 0xf1c27d;
const SHIRT = 0x3b6fb6;
const PANTS = 0x4a4a5a;

export const CHARACTER_HEIGHT = 1.8;

export const PARTS: readonly PartDef[] = [
  { name: 'torso', size: [0.4, 0.6, 0.2], pivot: [0, 0.9, 0], offset: [0, 0.3, 0], color: SHIRT, swing: 0 },
  { name: 'head', size: [0.26, 0.26, 0.26], pivot: [0, 1.5, 0], offset: [0, 0.15, 0], color: SKIN, swing: 0 },
  { name: 'armL', size: [0.12, 0.55, 0.12], pivot: [-0.27, 1.45, 0], offset: [0, -0.25, 0], color: SKIN, swing: 1 },
  { name: 'armR', size: [0.12, 0.55, 0.12], pivot: [0.27, 1.45, 0], offset: [0, -0.25, 0], color: SKIN, swing: -1 },
  { name: 'legL', size: [0.17, 0.9, 0.17], pivot: [-0.1, 0.9, 0], offset: [0, -0.45, 0], color: PANTS, swing: -1 },
  { name: 'legR', size: [0.17, 0.9, 0.17], pivot: [0.1, 0.9, 0], offset: [0, -0.45, 0], color: PANTS, swing: 1 },
];

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

export function makePartGeometry(part: PartDef): THREE.BoxGeometry {
  const g = new THREE.BoxGeometry(...part.size);
  g.translate(...part.offset);
  return g;
}

export function makePartMaterial(part: PartDef): THREE.MeshLambertMaterial {
  return new THREE.MeshLambertMaterial({ color: part.color, flatShading: true });
}

/** A single animated character (the local player). */
export class Character {
  readonly root = new THREE.Group();
  private readonly joints: { part: PartDef; node: THREE.Group }[] = [];
  /** Attachment mesh per equipment slot; meshes share geometry and material with every other wearer. */
  readonly attachments = new Map<EquipSlotName, THREE.Mesh>();
  private phase = 0;

  constructor() {
    for (const part of PARTS) {
      const node = new THREE.Group();
      node.position.set(...part.pivot);
      node.add(new THREE.Mesh(makePartGeometry(part), makePartMaterial(part)));
      this.root.add(node);
      this.joints.push({ part, node });
    }
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
      this.joints.find((j) => j.part.name === def.part)!.node.add(mesh);
      this.attachments.set(def.slot, mesh);
    }
  }

  /** `distance` is how far the character moved since the last call. */
  update(x: number, z: number, heading: number, distance: number, animation = 0): void {
    this.root.position.set(x, 0, z);
    this.root.rotation.y = heading;
    if (distance > 1e-4) {
      this.phase += distance * WALK_SPEED_RAD_PER_UNIT;
    } else {
      this.phase = 0;
    }
    for (const { part, node } of this.joints) {
      const roll = poseRoll(part, animation);
      node.rotation.x = roll !== 0 ? 0 : swingAngle(this.phase, part);
      node.rotation.z = roll;
    }
  }
}
