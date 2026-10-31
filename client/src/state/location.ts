import { pointInPolygon, polygonFromDecimetres, type Polygon } from './pvpZone';
import type { Point } from './shop';

/** The area type shown in the location panel (docs/specs/map.md, Gameplay areas). */
export type AreaType = 'Safe' | 'PvE' | 'PvP';

export interface Place {
  name: string;
  type: AreaType;
  /** The PvP zone: shown in red with the danger style. */
  danger: boolean;
}

/** shared/map/areas.json as the client reads it (decimetres). */
export interface AreasJson {
  spawn: { x: number; z: number };
  safeRadius: number;
  areas: { id: string; kind: string; p: number[] }[];
}

export interface GameArea {
  id: string;
  name: string;
  kind: 'pve' | 'pvp';
  poly: Polygon;
}

export interface AreaMap {
  /** The safe spawn circle, in metres. */
  spawn: Point;
  safeRadius: number;
  areas: GameArea[];
}

/** Display names for the areas.json ids (docs/specs/map.md); other ids fall back to a readable form of the id. */
export const AREA_NAMES: Readonly<Record<string, string>> = {
  aldrich_park: 'Aldrich Park',
  south_fields: 'South fields',
  pvp_zone: 'PvP zone',
};

function nameFor(id: string): string {
  const name = AREA_NAMES[id];
  if (name) return name;
  const words = id.replace(/_/g, ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** Converts areas.json (decimetres) to metres, keeping only PvE and PvP areas. */
export function areaMap(json: AreasJson): AreaMap {
  return {
    spawn: { x: json.spawn.x / 10, z: json.spawn.z / 10 },
    safeRadius: json.safeRadius / 10,
    areas: json.areas
      .filter((a) => a.kind === 'pve' || a.kind === 'pvp')
      .map((a) => ({ id: a.id, name: nameFor(a.id), kind: a.kind as 'pve' | 'pvp', poly: polygonFromDecimetres(a.p) })),
  };
}

/**
 * The gameplay area at pos (metres): the PvP zone first, then the safe circle (named after the area
 * around the spawn), then any PvE area. Null outside every area.
 */
export function placeAt(pos: Point, map: AreaMap): Place | null {
  const containing = (p: Point): GameArea | undefined =>
    map.areas.find((a) => a.kind === 'pvp' && pointInPolygon(p, a.poly)) ??
    map.areas.find((a) => pointInPolygon(p, a.poly));
  const area = containing(pos);
  if (area?.kind === 'pvp') return { name: area.name, type: 'PvP', danger: true };
  if (Math.hypot(pos.x - map.spawn.x, pos.z - map.spawn.z) <= map.safeRadius) {
    const name = area?.name ?? containing(map.spawn)?.name;
    return name ? { name, type: 'Safe', danger: false } : null;
  }
  return area ? { name: area.name, type: 'PvE', danger: false } : null;
}
