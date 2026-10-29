import areas from '../../../shared/map/areas.json';
import { EntityKind } from '../protocol';
import { CARD_TABLE_POSITIONS, inCardTableReach, type CardTablePosition } from './cardTables';
import { nearShop, TILE_METRES, type Point } from './shop';

/**
 * Attack range in tiles, counting diagonals (Chebyshev), as the server's combat range check.
 * Must equal attack_range_tiles in server/internal/systems/gamedata/data/rules.json;
 * a test fails if they differ (D-044 precedent).
 */
export const ATTACK_RANGE_TILES = 1;
/**
 * Pick-up range in tiles, counting diagonals (Chebyshev), as the server's ground item range check.
 * Must equal pickup_range_tiles in rules.json; a test fails if they differ.
 */
export const PICKUP_RANGE_TILES = 1;
/**
 * How close (in tiles, straight-line) another player must be to send a trade request.
 * Must equal trade_distance_tiles in rules.json; a test fails if they differ.
 */
export const TRADE_DISTANCE_TILES = 3;

/** A polygon as [x, z] points in metres. */
export type Polygon = readonly (readonly [number, number])[];

/** The PvP zone polygons from shared/map/areas.json, in metres (the file is in decimetres). */
export const PVP_ZONES: readonly Polygon[] = areas.areas
  .filter((a) => a.kind === 'pvp')
  .map((a) => {
    const pts: [number, number][] = [];
    for (let i = 0; i < a.p.length; i += 2) pts.push([a.p[i] / 10, a.p[i + 1] / 10]);
    return pts;
  });

/** Even-odd ray-crossing test, like the server's areas lookup. */
export function inPolygon(p: Point, poly: Polygon): boolean {
  let inside = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [xi, zi] = poly[i];
    const [xj, zj] = poly[j];
    if (zi > p.z !== zj > p.z && p.x < ((xj - xi) * (p.z - zi)) / (zj - zi) + xi) inside = !inside;
  }
  return inside;
}

export function inPvpZone(p: Point, zones: readonly Polygon[] = PVP_ZONES): boolean {
  return zones.some((z) => inPolygon(p, z));
}

/** A nearby entity: position in metres and its EntityInfo kind (undefined until EntityInfo arrives). */
export interface InteractEntity {
  id: number;
  x: number;
  z: number;
  kind: EntityKind | undefined;
}

/** A ground item: its ground item id and position in metres. */
export interface InteractGroundItem {
  id: number;
  x: number;
  z: number;
}

export interface InteractInput {
  /** The local player's position in metres. */
  pos: Point;
  stall: Point;
  /** Other entities (never the local player). */
  entities: readonly InteractEntity[];
  /**
   * Ground items in view. The server hides drops reserved for someone else (gameplay spec),
   * so every listed item is one the player may take.
   */
  groundItems?: readonly InteractGroundItem[];
  /** Card table centres in metres; CARD_TABLE_POSITIONS when omitted. */
  cardTables?: readonly CardTablePosition[];
  pvpZones?: readonly Polygon[];
}

export type InteractTarget =
  | { type: 'shop' }
  | { type: 'cardTable'; id: number }
  | { type: 'pickUp'; id: number }
  | { type: 'attack'; id: number }
  | { type: 'trade'; id: number };

const tile = (v: number): number => Math.floor(v / TILE_METRES);

/** Tiles between a and b, a diagonal step counting as one. */
function tileDistance(a: Point, b: Point): number {
  return Math.max(Math.abs(tile(a.x) - tile(b.x)), Math.abs(tile(a.z) - tile(b.z)));
}

/** Whether a ground item at item is within pick-up range of pos, by the server's tile rule. */
export function inPickUpRange(pos: Point, item: Point): boolean {
  return tileDistance(pos, item) <= PICKUP_RANGE_TILES;
}

/** The nearest ground item within pick-up range of pos, ties to the lower id; null if none. */
export function nearestPickUp(pos: Point, items: readonly InteractGroundItem[]): InteractGroundItem | null {
  return nearest(pos, items.filter((g) => inPickUpRange(pos, g)));
}

function nearest<T extends { id: number; x: number; z: number }>(pos: Point, list: readonly T[]): T | null {
  let best: T | null = null;
  let bestD = Infinity;
  for (const e of list) {
    const d = (e.x - pos.x) ** 2 + (e.z - pos.z) ** 2;
    if (d < bestD || (d === bestD && best && e.id < best.id)) {
      best = e;
      bestD = d;
    }
  }
  return best;
}

/** The nearest card table within reach of pos, ties to the lower id; null if none. */
export function nearestCardTable(
  pos: Point,
  tables: readonly CardTablePosition[] = CARD_TABLE_POSITIONS,
): CardTablePosition | null {
  return nearest(pos, tables.filter((t) => inCardTableReach(pos, t)));
}

/**
 * What the Interact button does (gameplay spec, Interact button): the shop stall within shop
 * distance; else the nearest card table within reach (open its panel); else the nearest ground item within pick-up range (pick up); else the nearest NPC within attack range (attack); else the nearest player within
 * trade distance (a trade request outside the PvP zone, an attack inside it). Null: nothing.
 */
export function pickInteractTarget(input: InteractInput): InteractTarget | null {
  const { pos } = input;
  if (nearShop(pos, input.stall)) return { type: 'shop' };
  const table = nearestCardTable(pos, input.cardTables);
  if (table) return { type: 'cardTable', id: table.id };
  const item = nearestPickUp(pos, input.groundItems ?? []);
  if (item) return { type: 'pickUp', id: item.id };
  const npc = nearest(
    pos,
    input.entities.filter((e) => e.kind === EntityKind.Npc && tileDistance(pos, e) <= ATTACK_RANGE_TILES),
  );
  if (npc) return { type: 'attack', id: npc.id };
  const reach = TRADE_DISTANCE_TILES * TILE_METRES;
  const player = nearest(
    pos,
    input.entities.filter((e) => e.kind === EntityKind.Player && (e.x - pos.x) ** 2 + (e.z - pos.z) ** 2 <= reach * reach),
  );
  if (!player) return null;
  return inPvpZone(pos, input.pvpZones) ? { type: 'attack', id: player.id } : { type: 'trade', id: player.id };
}
