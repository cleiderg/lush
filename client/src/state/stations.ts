import housing from '../../../shared/map/housing.json';
import { SHOP_DISTANCE_TILES, TILE_METRES, type Point } from './shop';

/** A Campus Thrift stall or a workbench: position in metres and the area it is in. */
export interface Station {
  /** Index in housing.json areas[] plus 1; breaks ties between equally near stations. */
  id: number;
  x: number;
  z: number;
  areaName: string;
}

const toStations = (key: 'thrift' | 'workbench'): readonly Station[] =>
  housing.areas.map((a, j) => ({ id: j + 1, x: a[key].x / 10, z: a[key].z / 10, areaName: a.name }));

/** The Campus Thrift stalls from housing.json (decimetres in the file, metres here). */
export const THRIFT_STALLS: readonly Station[] = toStations('thrift');
/** The workbenches from housing.json. */
export const WORKBENCHES: readonly Station[] = toStations('workbench');

/**
 * The nearest station within SHOP_DISTANCE_TILES of pos in a straight line (the server's shop
 * reach), ties to the lower id; null if none.
 */
export function nearestStation(pos: Point, stations: readonly Station[]): (Station & { dist: number }) | null {
  const reach = SHOP_DISTANCE_TILES * TILE_METRES;
  let best: (Station & { dist: number }) | null = null;
  for (const s of stations) {
    const dist = (s.x - pos.x) ** 2 + (s.z - pos.z) ** 2;
    if (dist > reach * reach) continue;
    if (!best || dist < best.dist || (dist === best.dist && s.id < best.id)) best = { ...s, dist };
  }
  return best;
}
