import * as THREE from 'three';
import { furnitureFootprint, furnitureGeometry, isWallItem, surfaceAnchors } from './furniture';
import { itemPose, LAYER_FLOOR, LAYER_RUG, LAYER_SURFACE, LAYER_WALL, ROOM_SIZE, SIDE_EAST, SIDE_NORTH, SIDE_SOUTH, SIDE_WEST, WALL_HEIGHT } from './roomView';
import type { RoomItem, RoomView } from './roomView';

/**
 * Visual side of home edit mode (#607, docs/specs/housing.md): a grid overlay, tap-to-slot picking and a
 * green or red ghost. Draws and reads only; the server stays the authority on placement
 * (server/internal/world/housing/room.go), and `checkPlacement` mirrors its rules so the colour agrees.
 * Rotation is in quarter turns here, as in RoomView.
 */

/** What the room needs to know about an item (room.go ItemDef). `layers` is a bit set of LAYER_*. */
export interface PlaceDef {
  w: number;
  d: number;
  layers: number;
  slots: number;
}

/**
 * A guess from the client's furniture table: wall items go on walls, 1x1 pieces may also sit on surfaces.
 * The real layers live in the server's items.json; callers with that data pass their own defs.
 */
export function furnitureDef(itemId: number): PlaceDef {
  const { w, d } = furnitureFootprint(itemId);
  const slots = isWallItem(itemId) ? 0 : surfaceAnchors(itemId).length;
  if (isWallItem(itemId)) return { w, d, layers: LAYER_WALL, slots };
  return { w, d, layers: LAYER_FLOOR | (w === 1 && d === 1 && slots === 0 ? LAYER_SURFACE : 0), slots };
}

/** Where the player wants an item (room.go Placement). Unused fields are ignored by layer. */
export interface Placement {
  x: number;
  y: number;
  rotation: number;
  /** Placed id of the surface piece to stand on, 0 for none. */
  parent: number;
  /** Slot on the parent, or -1 for the first free one. */
  slot: number;
  side: number;
  /** Position along the wall. */
  pos: number;
}

export const floorPlacement = (x: number, y: number, rotation = 0): Placement => ({ x, y, rotation, parent: 0, slot: -1, side: 0, pos: 0 });

/** The room.go Result names. */
export type PlaceResult = 'ok' | 'out_of_bounds' | 'overlap' | 'bad_rotation' | 'bad_layer' | 'not_surface_item' | 'no_surface' | 'surface_full';

export interface Checked {
  result: PlaceResult;
  /** The layer the item would go on, 0 if none. */
  layer: number;
  /** Surface slot actually used, -1 if not a surface placement. */
  slot: number;
}

const sizeAt = (def: PlaceDef, rotation: number): { w: number; d: number } =>
  rotation % 2 !== 0 ? { w: def.d, d: def.w } : { w: def.w, d: def.d };

/**
 * Whether `def` may go at `p` among `items`, following room.go resolve. `ignoreId` is the item being moved.
 * `defOf` gives the def of an already placed item.
 */
export function checkPlacement(
  items: readonly RoomItem[],
  defOf: (itemId: number) => PlaceDef,
  def: PlaceDef,
  p: Placement,
  ignoreId = 0,
): Checked {
  const fail = (result: PlaceResult, layer = 0): Checked => ({ result, layer, slot: -1 });
  const others = items.filter((o) => o.placedId !== ignoreId);
  if (p.parent !== 0) {
    if (!(def.layers & LAYER_SURFACE)) return fail('not_surface_item');
    if (!Number.isInteger(p.rotation) || p.rotation < 0 || p.rotation > 3) return fail('bad_rotation', LAYER_SURFACE);
    const parent = others.find((o) => o.placedId === p.parent);
    const n = parent ? defOf(parent.itemId).slots : 0;
    if (!parent || parent.layer !== LAYER_FLOOR || n <= 0) return fail('no_surface', LAYER_SURFACE);
    const used = new Array<boolean>(n).fill(false);
    for (const o of others) if (o.layer === LAYER_SURFACE && o.parentPlacedId === parent.placedId && o.slot >= 0 && o.slot < n) used[o.slot] = true;
    let slot = p.slot;
    if (slot < 0) {
      slot = used.indexOf(false);
      if (slot < 0) return fail('surface_full', LAYER_SURFACE);
    } else if (slot >= n) return fail('out_of_bounds', LAYER_SURFACE);
    else if (used[slot]) return fail('surface_full', LAYER_SURFACE);
    return { result: 'ok', layer: LAYER_SURFACE, slot };
  }
  let layer: number;
  if (def.layers & LAYER_FLOOR) layer = LAYER_FLOOR;
  else if (def.layers & LAYER_RUG) layer = LAYER_RUG;
  else if (def.layers & LAYER_WALL) layer = LAYER_WALL;
  else return fail('bad_layer');
  if (layer === LAYER_WALL) {
    if (p.side < SIDE_NORTH || p.side > SIDE_WEST || !Number.isInteger(p.side)) return fail('out_of_bounds', layer);
    if (def.w < 1 || p.pos < 0 || p.pos + def.w > ROOM_SIZE) return fail('out_of_bounds', layer);
    for (const o of others) {
      if (o.layer !== LAYER_WALL || o.side !== p.side) continue;
      const ow = defOf(o.itemId).w;
      if (p.pos < o.x + ow && o.x < p.pos + def.w) return fail('overlap', layer);
    }
    return { result: 'ok', layer, slot: -1 };
  }
  if (!Number.isInteger(p.rotation) || p.rotation < 0 || p.rotation > 3) return fail('bad_rotation', layer);
  const { w, d } = sizeAt(def, p.rotation);
  if (p.x < 0 || p.y < 0 || p.x + w > ROOM_SIZE || p.y + d > ROOM_SIZE) return fail('out_of_bounds', layer);
  for (const o of others) {
    if (o.layer !== layer) continue;
    const s = sizeAt(defOf(o.itemId), o.rotation);
    if (p.x < o.x + s.w && o.x < p.x + w && p.y < o.y + s.d && o.y < p.y + d) return fail('overlap', layer);
  }
  return { result: 'ok', layer, slot: -1 };
}

// ---------------------------------------------------------------- picking

export interface TilePick {
  kind: 'tile';
  x: number;
  y: number;
}
export interface SurfacePick {
  kind: 'surface';
  /** The floor piece whose surface was hit (the parent of a surface item that was hit). */
  parentPlacedId: number;
  /** The free slot nearest the hit, -1 if the surface is full. */
  slot: number;
  /** The tile under the hit point. */
  x: number;
  y: number;
}
export interface WallPick {
  kind: 'wall';
  side: number;
  /** Tile position along the wall (room.go Pos). */
  pos: number;
}
export type RoomPick = TilePick | SurfacePick | WallPick;

const FLOOR_PLANE = new THREE.Plane(new THREE.Vector3(0, 1, 0), 0);
const tileOf = (v: number): number => Math.min(ROOM_SIZE - 1, Math.max(0, Math.floor(v)));
const inRoom = (v: number): boolean => v >= 0 && v < ROOM_SIZE;

function wallPick(side: number, p: THREE.Vector3): WallPick {
  return { kind: 'wall', side, pos: tileOf(side === SIDE_NORTH || side === SIDE_SOUTH ? p.x : p.z) };
}

/**
 * What a ray (in room space) points at: a floor tile, the surface of a piece (nearest free slot), a wall
 * position, or nothing when it leaves the room. The nearest thing along the ray wins.
 */
export function pickRoom(
  view: RoomView,
  items: readonly RoomItem[],
  defOf: (itemId: number) => PlaceDef,
  ray: THREE.Ray,
): RoomPick | null {
  view.group.updateMatrixWorld(true);
  const byId = new Map(items.map((i) => [i.placedId, i]));
  let best: { dist: number; pick: RoomPick } | null = null;
  const offer = (dist: number, pick: RoomPick): void => {
    if (!best || dist < best.dist) best = { dist, pick };
  };
  const hit = new THREE.Vector3();

  if (ray.intersectPlane(FLOOR_PLANE, hit) && inRoom(hit.x) && inRoom(hit.z)) {
    offer(ray.origin.distanceTo(hit), { kind: 'tile', x: Math.floor(hit.x), y: Math.floor(hit.z) });
  }

  const sides: [number, THREE.Mesh][] = [
    [SIDE_NORTH, view.walls.north], [SIDE_EAST, view.walls.east], [SIDE_SOUTH, view.walls.south], [SIDE_WEST, view.walls.west],
  ];
  const caster = new THREE.Raycaster(ray.origin, ray.direction);
  for (const [side, mesh] of sides) {
    const h = caster.intersectObject(mesh, false)[0];
    // Only the part of the wall inside its height counts; the planes are exactly that tall.
    if (h && h.point.y <= WALL_HEIGHT) offer(h.distance, wallPick(side, h.point));
  }

  for (const it of items) {
    const mesh = view.meshOf(it.placedId);
    const h = mesh && caster.intersectObject(mesh, false)[0];
    if (!h) continue;
    if (it.layer === LAYER_WALL) {
      offer(h.distance, wallPick(it.side, h.point));
      continue;
    }
    const parent = it.layer === LAYER_SURFACE ? byId.get(it.parentPlacedId) : it;
    if (parent && parent.layer === LAYER_FLOOR && defOf(parent.itemId).slots > 0) {
      const parentMesh = view.meshOf(parent.placedId);
      if (parentMesh) {
        offer(h.distance, {
          kind: 'surface',
          parentPlacedId: parent.placedId,
          slot: nearestFreeSlot(parent, parentMesh, items, h.point),
          x: tileOf(h.point.x),
          y: tileOf(h.point.z),
        });
        continue;
      }
    }
    // Any other piece: the tile under the hit, if it is inside the room.
    if (inRoom(h.point.x) && inRoom(h.point.z)) offer(h.distance, { kind: 'tile', x: Math.floor(h.point.x), y: Math.floor(h.point.z) });
  }
  return best === null ? null : (best as { pick: RoomPick }).pick;
}

function nearestFreeSlot(parent: RoomItem, mesh: THREE.Mesh, items: readonly RoomItem[], point: THREE.Vector3): number {
  const used = new Set<number>();
  for (const o of items) if (o.layer === LAYER_SURFACE && o.parentPlacedId === parent.placedId) used.add(o.slot);
  let best = -1, bestD = Infinity;
  surfaceAnchors(parent.itemId).forEach((a, i) => {
    if (used.has(i)) return;
    const w = a.clone().applyAxisAngle(new THREE.Vector3(0, 1, 0), mesh.rotation.y).add(mesh.position);
    const d = (w.x - point.x) ** 2 + (w.z - point.z) ** 2;
    if (d < bestD) [best, bestD] = [i, d];
  });
  return best;
}

// ---------------------------------------------------------------- overlay and ghost

export const GHOST_VALID = 0x55dd66;
export const GHOST_BLOCKED = 0xe04848;
const GRID_COLOR = 0xfff4d8;

const ghostMaterial = (color: number): THREE.MeshBasicMaterial =>
  new THREE.MeshBasicMaterial({ color, transparent: true, opacity: 0.55, depthWrite: false });
const VALID_MATERIAL = ghostMaterial(GHOST_VALID);
const BLOCKED_MATERIAL = ghostMaterial(GHOST_BLOCKED);

/** Edit-mode visuals for a RoomView. Hidden until `setGridVisible` / `setGhost`; never changes placed items. */
export class RoomEditView {
  readonly grid: THREE.LineSegments;
  readonly ghost: THREE.Mesh;
  private ghostItemId = -1;

  constructor(
    view: RoomView,
    private readonly defOf: (itemId: number) => PlaceDef = furnitureDef,
  ) {
    const pts: number[] = [];
    for (let i = 0; i <= ROOM_SIZE; i++) {
      pts.push(i, 0, 0, i, 0, ROOM_SIZE, 0, 0, i, ROOM_SIZE, 0, i);
    }
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(pts, 3));
    this.grid = new THREE.LineSegments(g, new THREE.LineBasicMaterial({ color: GRID_COLOR, transparent: true, opacity: 0.6 }));
    this.grid.name = 'room_grid';
    this.grid.position.y = 0.015; // above the floor, below a rug's top
    this.grid.visible = false;
    this.ghost = new THREE.Mesh(new THREE.BoxGeometry(1, 1, 1), VALID_MATERIAL);
    this.ghost.name = 'room_ghost';
    this.ghost.visible = false;
    view.group.add(this.grid, this.ghost);
  }

  setGridVisible(on: boolean): void {
    this.grid.visible = on;
  }

  get gridVisible(): boolean {
    return this.grid.visible;
  }

  get ghostValid(): boolean {
    return this.ghost.visible && this.ghost.material === VALID_MATERIAL;
  }

  hideGhost(): void {
    this.ghost.visible = false;
  }

  /**
   * Show the ghost of `itemId` at `p`, green if room.go would accept it and red if not. Returns the check so
   * the caller can decide whether to send. `ignoreId` is the placed item being moved, if any.
   */
  setGhost(items: readonly RoomItem[], itemId: number, p: Placement, ignoreId = 0): Checked {
    const def = this.defOf(itemId);
    const checked = checkPlacement(items, this.defOf, def, p, ignoreId);
    this.useGeometry(itemId, def);
    const layer = checked.layer || (def.layers & LAYER_WALL ? LAYER_WALL : LAYER_FLOOR);
    const probe: RoomItem = {
      placedId: -1, itemId, x: layer === LAYER_WALL ? p.pos : p.x, y: p.y, rotation: p.rotation, layer,
      parentPlacedId: p.parent, slot: Math.max(0, checked.slot < 0 ? p.slot : checked.slot), side: p.side,
    };
    const byId = new Map(items.map((i) => [i.placedId, i]));
    let pose: ReturnType<typeof itemPose>;
    try {
      pose = itemPose(probe, byId);
    } catch {
      pose = null; // furniture table doesn't know the item
    }
    // Out of bounds still shows, in red, where the player is pointing.
    pose ??= { x: p.x + def.w / 2, y: 0, z: p.y + def.d / 2, yaw: 0 };
    this.ghost.position.set(pose.x, pose.y, pose.z);
    this.ghost.rotation.set(0, pose.yaw, 0);
    this.ghost.material = checked.result === 'ok' ? VALID_MATERIAL : BLOCKED_MATERIAL;
    this.ghost.visible = true;
    return checked;
  }

  private useGeometry(itemId: number, def: PlaceDef): void {
    if (itemId === this.ghostItemId) return;
    this.ghostItemId = itemId;
    this.ghost.geometry.dispose();
    try {
      this.ghost.geometry = furnitureGeometry(itemId).clone();
    } catch {
      const g = new THREE.BoxGeometry(def.w, 0.6, def.d);
      g.translate(0, 0.3, 0);
      this.ghost.geometry = g;
    }
  }
}
