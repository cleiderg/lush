/** Types and parsers for the files in shared/map (format documented in shared/map/README.md). */

export interface Building { id: number; h: number; p: number[] }
/** Path surface kinds (`s` in campus.json). */
export const SURFACE_DIRT = 0;
export const SURFACE_STONE = 1;
export const SURFACE_PAVED = 2;
export interface PathWay { id: number; w: number; s: number; p: number[] }
/** A paved pedestrian area: closed polygon, last point not repeated. */
export interface Plaza { id: number; p: number[] }
export const GREEN_GRASS = 0;
export const GREEN_WOOD = 1;
export const GREEN_WATER = 2;
export const GREEN_PITCH = 3;
export interface Green { id: number; k: number; p: number[] }

/** campus.json. All coordinates are integer decimetres: x east, z south. */
export interface CampusData {
  version: number;
  attribution: string;
  chunkSize: number;
  bounds: { minX: number; minZ: number; maxX: number; maxZ: number };
  buildings: Building[];
  paths: PathWay[];
  plazas: Plaza[];
  green: Green[];
}

export function parseCampus(json: string): CampusData {
  const d = JSON.parse(json) as CampusData;
  if (d.version !== 1) throw new Error(`unsupported campus.json version ${d.version}`);
  d.plazas ??= [];
  for (const w of d.paths) w.s ??= SURFACE_STONE;
  return d;
}

export interface CollisionGrid {
  cellSize: number; // metres
  chunkCells: number;
  width: number;
  height: number;
  originX: number; // metres
  originZ: number;
  bits: Uint8Array;
}

export const COLLISION_HEADER_BYTES = 22;

export function parseCollision(buf: Uint8Array): CollisionGrid {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const magic = String.fromCharCode(buf[0], buf[1], buf[2], buf[3]);
  if (magic !== 'LCOL' || dv.getUint8(4) !== 1) throw new Error('bad collision file');
  return {
    cellSize: dv.getUint16(6, true) / 10,
    chunkCells: dv.getUint16(8, true),
    width: dv.getUint16(10, true),
    height: dv.getUint16(12, true),
    originX: dv.getInt32(14, true) / 10,
    originZ: dv.getInt32(18, true) / 10,
    bits: buf.subarray(COLLISION_HEADER_BYTES),
  };
}

/** True when the world point (metres) is blocked. Outside the grid counts as blocked. */
export function isBlocked(g: CollisionGrid, x: number, z: number): boolean {
  const col = Math.floor((x - g.originX) / g.cellSize);
  const row = Math.floor((z - g.originZ) / g.cellSize);
  if (col < 0 || row < 0 || col >= g.width || row >= g.height) return true;
  const idx = row * g.width + col;
  return ((g.bits[idx >> 3] >> (idx & 7)) & 1) === 1;
}

export interface MapPoint { x: number; z: number }
export interface GameArea { id: string; kind: 'pve' | 'pvp'; osm?: number; p: number[] }

/** areas.json: gameplay places from docs/specs/map.md. Integer decimetres like campus.json. */
export interface AreasData {
  version: number;
  units: string;
  spawn: MapPoint;
  safeRadius: number;
  shop: MapPoint;
  areas: GameArea[];
}

export function parseAreas(json: string): AreasData {
  const d = JSON.parse(json) as AreasData;
  if (d.version !== 1) throw new Error(`unsupported areas.json version ${d.version}`);
  return d;
}

export interface CardTable { id: number; x: number; z: number }
/** cardtables.json: the card table plaza and its tables. Integer decimetres like areas.json. */
export interface CardTablesData {
  version: number;
  units: string;
  plaza: number[];
  tables: CardTable[];
}

export function parseCardTables(json: string): CardTablesData {
  const d = JSON.parse(json) as CardTablesData;
  if (d.version !== 1) throw new Error(`unsupported cardtables.json version ${d.version}`);
  return d;
}

/** One prop: [type index into `types`, x, z, rotation in whole degrees]. x and z in integer decimetres. */
export type Prop = [number, number, number, number];
/** props.json: visual-only campus props placed by shared/map/build.mjs. */
export interface PropsData {
  version: number;
  units: string;
  types: string[];
  props: Prop[];
}

export function parseProps(json: string): PropsData {
  const d = JSON.parse(json) as PropsData;
  if (d.version !== 1) throw new Error(`unsupported props.json version ${d.version}`);
  return d;
}

/**
 * One Aldrich Park nature placement: [type index into `types`, x, z, rotation in whole degrees,
 * scale in percent, extra height stretch in percent]. x and z in integer decimetres.
 */
export type NatureItem = [number, number, number, number, number, number];
/** parknature.json: visual-only trees, rocks, bushes, flower beds, picnic blankets and birds placed by shared/map/build.mjs. */
export interface ParkNatureData {
  version: number;
  units: string;
  types: string[];
  items: NatureItem[];
}

export function parseParkNature(json: string): ParkNatureData {
  const d = JSON.parse(json) as ParkNatureData;
  if (d.version !== 1) throw new Error(`unsupported parknature.json version ${d.version}`);
  return d;
}
