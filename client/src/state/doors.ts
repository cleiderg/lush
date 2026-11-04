import housing from '../../../shared/map/housing.json';
import { TILE_METRES, type Point } from './shop';

/**
 * Reach in tiles, straight-line, from a house door to open its panel.
 * Must equal DoorReachTiles in server/internal/systems/housing DefaultConfig (the reach homeroom uses for HomeEnter).
 */
export const DOOR_REACH_TILES = 3;

/** A house door: house id (index in housing.json houses[] plus 1, the server's plot id), position in metres, area and its number. */
export interface HouseDoor {
  id: number;
  x: number;
  z: number;
  areaName: string;
  /** The number in the housing.json house id, e.g. 2 for "mesa_court-2". */
  number: number;
}

const areaNames = new Map(housing.areas.map((a) => [a.id, a.name]));

/** The doors from housing.json (decimetres in the file, metres here). */
export const HOUSE_DOORS: readonly HouseDoor[] = housing.houses.map((h, j) => ({
  id: j + 1,
  x: h.x / 10,
  z: h.z / 10,
  areaName: areaNames.get(h.area) ?? h.area,
  number: Number(h.id.slice(h.id.lastIndexOf('-') + 1)),
}));

/** The nearest door within DOOR_REACH_TILES of pos in a straight line, ties to the lower id; null if none. */
export function nearestDoor(pos: Point, doors: readonly HouseDoor[] = HOUSE_DOORS): HouseDoor | null {
  const reach = DOOR_REACH_TILES * TILE_METRES;
  let best: HouseDoor | null = null;
  let bestD = Infinity;
  for (const d of doors) {
    const dist = (d.x - pos.x) ** 2 + (d.z - pos.z) ** 2;
    if (dist > reach * reach) continue;
    if (dist < bestD || (dist === bestD && best && d.id < best.id)) {
      best = d;
      bestD = dist;
    }
  }
  return best;
}
