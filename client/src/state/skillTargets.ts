import skillsMap from '../../../shared/map/skills.json';
import { ToolSkill } from '../protocol';
import type { Point } from './shop';
import type { ItemState } from './items';
import type { SkillState } from './skills';
import { Skill } from '../protocol';

/** The cooking grill from shared/map/skills.json, in metres (the file is in decimetres). */
export const GRILL: Point = { x: skillsMap.grill.x / 10, z: skillsMap.grill.z / 10 };

/** A tree or fishing spot the player could work now: its target id (id contract in skills.md) and position in metres. */
export interface SkillResource {
  type: 'tree' | 'fishingSpot';
  /** A tree's index in trees[]; a fishing spot's FishingSpotState.id. */
  id: number;
  x: number;
  z: number;
  /** The tree kind ("tree", "oak", "eucalyptus"); "net" or "rod" for a spot. */
  kind: string;
}

/** What the Interact picker needs for skills. */
export interface SkillInteractInput {
  /** Whether the inventory holds an item with cooking_level > 0 (a raw fish). */
  hasRawFish: boolean;
  /** Usable trees and fishing spots (see usableResources). */
  resources: readonly SkillResource[];
}

const dm = (v: number): number => v / 10;

/** Whether the inventory holds an item with cooking_level > 0. */
export function hasRawFish(items: ItemState): boolean {
  return items.inventory.slots.some((s) => s.itemId !== 0 && s.quantity > 0 && (items.item(s.itemId)?.cookingLevel ?? 0) > 0);
}

/** Whether a hatchet is in the inventory or equipped as the weapon with tool_level at most the Woodcutting level. */
export function hasUsableHatchet(items: ItemState, woodcuttingLevel: number): boolean {
  const { slots, equipment } = items.inventory;
  const ids = [equipment.weapon, ...slots.filter((s) => s.quantity > 0).map((s) => s.itemId)];
  return ids.some((id) => {
    const info = id ? items.item(id) : undefined;
    return !!info && info.toolSkill === ToolSkill.Woodcutting && info.toolLevel <= woodcuttingLevel;
  });
}

/**
 * Every standing tree and active fishing spot that the player has the level and tool for: felled trees
 * are left out, a tree needs a hatchet and the SkillCatalog Woodcutting level, a spot needs its catalog
 * tool in the inventory and the Fishing level.
 */
export function usableResources(items: ItemState, skills: SkillState): SkillResource[] {
  const out: SkillResource[] = [];
  const wc = skills.level(Skill.Woodcutting);
  if (hasUsableHatchet(items, wc)) {
    skillsMap.trees.forEach((t, index) => {
      const kind = skills.treeKind(t.kind);
      if (!kind || kind.level > wc || skills.isFelled(index)) return;
      out.push({ type: 'tree', id: index, x: dm(t.x), z: dm(t.z), kind: t.kind });
    });
  }
  const fl = skills.level(Skill.Fishing);
  for (const { id, candidate } of skills.fishingSpots()) {
    const spot = skillsMap.pond.spots[candidate];
    const kind = spot && skills.fishingSpotKind(spot.kind);
    if (!spot || !kind || kind.level > fl) continue;
    if (!items.inventory.slots.some((s) => s.itemId === kind.toolItemId && s.quantity > 0)) continue;
    out.push({ type: 'fishingSpot', id, x: dm(spot.x), z: dm(spot.z), kind: spot.kind });
  }
  return out;
}
