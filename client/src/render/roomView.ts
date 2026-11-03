import * as THREE from 'three';
import { furnitureFootprint, furnitureGeometry, isWallItem, surfaceAnchors, surfaceMaterial } from './furniture';

/**
 * One 8x8 home room drawn from a plain description (#563, docs/specs/housing.md, visual-style.md). Room
 * space: x runs west to east and z north to south in metres, one tile per metre, the room spanning 0..8 on
 * both; y is up. Tile (x, y) of the server is the square x..x+1, z = y..y+1. Rotation is in quarter turns,
 * clockwise seen from above, and the corner tile (x, y) is the north-west corner of the rotated footprint
 * (server room.go Size rule: 90 and 270 swap width and depth). Reads no game state; the caller passes it in.
 */

export const ROOM_SIZE = 8;
export const WALL_HEIGHT = 2.4;
/** Height of a rug's top; floor items stand on it so a rug can lie beneath them. */
export const RUG_TOP = 0.03;

/** Layer bit values of PlacedItem.layer (room.go Layer). */
export const LAYER_FLOOR = 1;
export const LAYER_RUG = 2;
export const LAYER_SURFACE = 4;
export const LAYER_WALL = 8;
/** Wall sides of PlacedItem.side. */
export const SIDE_NORTH = 0;
export const SIDE_EAST = 1;
export const SIDE_SOUTH = 2;
export const SIDE_WEST = 3;

export interface RoomItem {
  placedId: number;
  itemId: number;
  /** Corner tile; for wall items x is the position along the wall and y is unused. */
  x: number;
  y: number;
  /** Quarter turns, 0..3. */
  rotation: number;
  layer: number;
  /** 0 unless stacked on a surface piece. */
  parentPlacedId: number;
  slot: number;
  side: number;
}

export interface RoomDescription {
  /** Item id of the wallpaper, 0 for the default. */
  wallpaperId: number;
  /** Item id of the floor covering, 0 for the default. */
  floorId: number;
  items: readonly RoomItem[];
}

// Warm plaster and boards, as in the café (cafeInterior.ts).
export const DEFAULT_WALL_MATERIAL = new THREE.MeshLambertMaterial({ color: 0xefe6d2 });
export const DEFAULT_FLOOR_MATERIAL = new THREE.MeshLambertMaterial({ color: 0x9a6a3e });
DEFAULT_WALL_MATERIAL.name = 'room_wall_default';
DEFAULT_FLOOR_MATERIAL.name = 'room_floor_default';

let furnitureMaterial: THREE.MeshLambertMaterial | null = null;
const geometries = new Map<number, THREE.BufferGeometry>();

function modelGeometry(id: number): THREE.BufferGeometry {
  let g = geometries.get(id);
  if (!g) geometries.set(id, (g = furnitureGeometry(id)));
  return g;
}

function material(id: number, fallback: THREE.MeshLambertMaterial): THREE.MeshLambertMaterial {
  if (!id) return fallback;
  try {
    return surfaceMaterial(id);
  } catch {
    return fallback;
  }
}

interface Pose {
  x: number;
  y: number;
  z: number;
  yaw: number;
}

const Y_AXIS = new THREE.Vector3(0, 1, 0);
const quarter = (r: number): number => -(((r % 4) + 4) % 4) * (Math.PI / 2);

/** Where an item stands, or null if it can't be placed (unknown parent, slot or wall item). */
function pose(item: RoomItem, byId: ReadonlyMap<number, RoomItem>, depth = 0): Pose | null {
  if (item.layer === LAYER_WALL) {
    const w = furnitureFootprint(item.itemId).w;
    const c = item.x + w / 2, e = ROOM_SIZE - 0.5;
    switch (item.side) {
      case SIDE_NORTH: return { x: c, y: 0, z: 0.5, yaw: 0 };
      case SIDE_SOUTH: return { x: c, y: 0, z: e, yaw: Math.PI };
      case SIDE_EAST: return { x: e, y: 0, z: c, yaw: -Math.PI / 2 };
      case SIDE_WEST: return { x: 0.5, y: 0, z: c, yaw: Math.PI / 2 };
      default: return null;
    }
  }
  if (item.layer === LAYER_SURFACE) {
    const parent = byId.get(item.parentPlacedId);
    if (!parent || parent.layer !== LAYER_FLOOR || depth > 0) return null;
    const pp = pose(parent, byId, depth + 1);
    const anchor = surfaceAnchors(parent.itemId)[item.slot];
    if (!pp || !anchor) return null;
    const a = anchor.clone().applyAxisAngle(Y_AXIS, pp.yaw);
    return { x: pp.x + a.x, y: pp.y + a.y, z: pp.z + a.z, yaw: quarter(item.rotation) };
  }
  let { w, d } = furnitureFootprint(item.itemId);
  if (item.rotation % 2 !== 0) [w, d] = [d, w];
  return { x: item.x + w / 2, y: item.layer === LAYER_RUG ? 0 : RUG_TOP, z: item.y + d / 2, yaw: quarter(item.rotation) };
}

const poseKey = (id: number, p: Pose): string => `${id}|${p.x.toFixed(4)}|${p.y.toFixed(4)}|${p.z.toFixed(4)}|${p.yaw.toFixed(4)}`;

function planeUV(g: THREE.PlaneGeometry, u: number, v: number): THREE.PlaneGeometry {
  const uv = g.getAttribute('uv') as THREE.BufferAttribute;
  for (let i = 0; i < uv.count; i++) uv.setXY(i, uv.getX(i) * u, uv.getY(i) * v);
  return g;
}

export class RoomView {
  readonly group = new THREE.Group();
  readonly floor: THREE.Mesh;
  readonly walls: Record<'north' | 'east' | 'south' | 'west', THREE.Mesh>;
  /** Furniture meshes by placed id. */
  private readonly meshes = new Map<number, { mesh: THREE.Mesh; key: string; itemId: number }>();
  private wallpaperId = -1;
  private floorId = -1;

  constructor() {
    this.group.name = 'room';
    const S = ROOM_SIZE;
    this.floor = new THREE.Mesh(planeUV(new THREE.PlaneGeometry(S, S), S, S), DEFAULT_FLOOR_MATERIAL);
    this.floor.name = 'room_floor';
    this.floor.rotation.x = -Math.PI / 2;
    this.floor.position.set(S / 2, 0, S / 2);
    const wall = (name: string, x: number, z: number, yaw: number): THREE.Mesh => {
      const m = new THREE.Mesh(planeUV(new THREE.PlaneGeometry(S, WALL_HEIGHT), S, WALL_HEIGHT), DEFAULT_WALL_MATERIAL);
      m.name = `room_wall_${name}`;
      m.position.set(x, WALL_HEIGHT / 2, z);
      m.rotation.y = yaw;
      return m;
    };
    // Planes face +z; each is turned to face into the room, so from outside they are culled.
    this.walls = {
      north: wall('north', S / 2, 0, 0),
      south: wall('south', S / 2, S, Math.PI),
      east: wall('east', S, S / 2, -Math.PI / 2),
      west: wall('west', 0, S / 2, Math.PI / 2),
    };
    // Warm interior lighting, as in the café.
    const warm = new THREE.HemisphereLight(0xfff0d0, 0x8a6a4a, 1.6);
    const lamp = new THREE.DirectionalLight(0xffd9a0, 1.2);
    lamp.position.set(S * 0.3, 6, S * 0.2);
    this.group.add(this.floor, ...Object.values(this.walls), warm, lamp);
  }

  /** Make the room match the description, touching only what changed. */
  update(desc: RoomDescription): void {
    if (desc.wallpaperId !== this.wallpaperId) {
      this.wallpaperId = desc.wallpaperId;
      const m = material(desc.wallpaperId, DEFAULT_WALL_MATERIAL);
      for (const w of Object.values(this.walls)) w.material = m;
    }
    if (desc.floorId !== this.floorId) {
      this.floorId = desc.floorId;
      this.floor.material = material(desc.floorId, DEFAULT_FLOOR_MATERIAL);
    }
    const byId = new Map<number, RoomItem>();
    for (const it of desc.items) byId.set(it.placedId, it);
    const seen = new Set<number>();
    for (const it of desc.items) {
      let p: Pose | null;
      try {
        p = pose(it, byId);
      } catch {
        p = null; // unknown item id
      }
      if (!p || (isWallItem(it.itemId) !== (it.layer === LAYER_WALL))) continue;
      seen.add(it.placedId);
      const key = poseKey(it.itemId, p);
      let e = this.meshes.get(it.placedId);
      if (e && e.key === key) continue;
      if (!e || e.itemId !== it.itemId) {
        if (e) this.group.remove(e.mesh);
        furnitureMaterial ??= new THREE.MeshLambertMaterial({ vertexColors: true });
        const mesh = new THREE.Mesh(modelGeometry(it.itemId), furnitureMaterial);
        mesh.name = `placed_${it.placedId}`;
        this.group.add(mesh);
        e = { mesh, key, itemId: it.itemId };
        this.meshes.set(it.placedId, e);
      }
      e.key = key;
      e.mesh.position.set(p.x, p.y, p.z);
      e.mesh.rotation.set(0, p.yaw, 0);
      e.mesh.updateMatrix();
    }
    for (const [id, e] of this.meshes) {
      if (seen.has(id)) continue;
      this.group.remove(e.mesh);
      this.meshes.delete(id);
    }
  }

  /** The mesh drawing a placed item, if it is drawn. */
  meshOf(placedId: number): THREE.Mesh | undefined {
    return this.meshes.get(placedId)?.mesh;
  }

  get itemCount(): number {
    return this.meshes.size;
  }
}
