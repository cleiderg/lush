import * as THREE from 'three';
import type { EntityLookup, EquipmentView } from './worldView';

export type EquipSlotName = 'weapon' | 'head' | 'body' | 'shield';

/** How an equipment slot is drawn: one box attached to a body part's joint. */
export interface SlotDef {
  readonly slot: EquipSlotName;
  /** PARTS name whose joint the attachment follows (so it swings with the limb). */
  readonly part: string;
  readonly size: readonly [number, number, number];
  /** Box center relative to the part's pivot. */
  readonly offset: readonly [number, number, number];
}

export const SLOTS: readonly SlotDef[] = [
  // Gripped in the right hand at the bottom of the sleeve, pointing forward (+Z is the facing direction).
  { slot: 'weapon', part: 'armR', size: [0.06, 0.06, 0.62], offset: [0, -0.34, 0.22] },
  // Cap over the top of the head and its hair (head pivot is the neck; the head is 0.56 tall).
  { slot: 'head', part: 'head', size: [0.68, 0.2, 0.64], offset: [0, 0.55, 0] },
  // Shell slightly bigger than the torso so it covers the shirt.
  { slot: 'body', part: 'torso', size: [0.6, 0.5, 0.4], offset: [0, 0.27, 0] },
  // Flat board on the outside of the left forearm.
  { slot: 'shield', part: 'armL', size: [0.05, 0.4, 0.36], offset: [-0.11, -0.24, 0.02] },
];

/** Flat colours for known items (docs/specs/gameplay.md), by catalog name. */
const ITEM_COLORS: Record<string, number> = {
  'Wooden stick': 0x8b5a2b,
  'Bronze dagger': 0xb08d57,
  'Bike lock': 0x2f2f36,
  Hoodie: 0x2e4a7a,
  'Cardboard shield': 0xc49a6c,
  'Leather cap': 0x6b4423,
  'Bike helmet': 0xe2b13c,
  'Robot plating': 0x9aa3ab,
};

/** Fallback colours for items the catalog hasn't named yet (or unknown names), picked by item id. */
const PALETTE = [0xb5523b, 0x5b8c5a, 0x7a5ea8, 0xc9a227, 0x4f7f9e, 0x8e8e8e];

export function itemColor(itemId: number, lookup?: EntityLookup): number {
  const name = lookup?.item?.(itemId)?.name;
  const known = name === undefined ? undefined : ITEM_COLORS[name];
  return known ?? PALETTE[itemId % PALETTE.length];
}

const geometries = new Map<EquipSlotName, THREE.BoxGeometry>();
const materials = new Map<number, THREE.MeshLambertMaterial>();

/** Shared box for a slot, already offset from the part's pivot. One per slot for the whole client. */
export function slotGeometry(def: SlotDef): THREE.BoxGeometry {
  let g = geometries.get(def.slot);
  if (!g) {
    g = new THREE.BoxGeometry(...def.size);
    g.translate(...def.offset);
    geometries.set(def.slot, g);
  }
  return g;
}

/** Shared flat material for a colour. Every entity wearing the item uses the same instance. */
export function equipmentMaterial(color: number): THREE.MeshLambertMaterial {
  let m = materials.get(color);
  if (!m) {
    m = new THREE.MeshLambertMaterial({ color, flatShading: true });
    materials.set(color, m);
  }
  return m;
}

export function equipmentOf(id: number, lookup?: EntityLookup): EquipmentView | undefined {
  return lookup?.entityInfo(id)?.equipment;
}
