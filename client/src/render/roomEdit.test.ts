import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { floorPlacement, furnitureDef, GHOST_BLOCKED, GHOST_VALID, pickRoom, RoomEditView } from './roomEdit';
import type { PlaceDef, Placement } from './roomEdit';
import { LAYER_FLOOR, LAYER_RUG, LAYER_SURFACE, LAYER_WALL, ROOM_SIZE, RoomView, SIDE_NORTH, SIDE_WEST } from './roomView';
import type { RoomItem } from './roomView';

const item = (o: Partial<RoomItem> & Pick<RoomItem, 'placedId' | 'itemId' | 'layer'>): RoomItem =>
  ({ x: 0, y: 0, rotation: 0, parentPlacedId: 0, slot: 0, side: 0, ...o });
const empty = { wallpaperId: 0, floorId: 0, items: [] as RoomItem[] };
const down = (x: number, z: number): THREE.Ray => new THREE.Ray(new THREE.Vector3(x, 6, z), new THREE.Vector3(0, -1, 0));
const hex = (v: RoomEditView): number => (v.ghost.material as THREE.MeshBasicMaterial).color.getHex();

// The defs of room_test.go, keyed by item id.
const DEFS: Record<number, PlaceDef> = {
  1: { w: 1, d: 1, layers: LAYER_FLOOR, slots: 0 }, // chair
  2: { w: 2, d: 1, layers: LAYER_FLOOR, slots: 0 }, // sofa
  3: { w: 2, d: 1, layers: LAYER_FLOOR, slots: 2 }, // table
  4: { w: 1, d: 1, layers: LAYER_SURFACE, slots: 0 }, // lamp
  5: { w: 2, d: 2, layers: LAYER_RUG, slots: 0 }, // rug
  6: { w: 2, d: 1, layers: LAYER_WALL, slots: 0 }, // window
};
const defOf = (id: number): PlaceDef => DEFS[id]!;
const on = (parent: number, slot: number): Placement => ({ ...floorPlacement(0, 0), parent, slot });

describe('RoomEditView', () => {
  it('AC1: the grid shows and hides over the 8x8 floor without touching placed items', () => {
    const v = new RoomView();
    const items = [item({ placedId: 1, itemId: 29, layer: LAYER_FLOOR, x: 2, y: 2 })];
    v.update({ ...empty, items });
    const mesh = v.meshOf(1)!, before = mesh.position.clone();
    const e = new RoomEditView(v);
    expect(e.gridVisible).toBe(false);
    e.setGridVisible(true);
    expect(e.gridVisible).toBe(true);
    expect(v.group.children).toContain(e.grid);
    const b = new THREE.Box3().setFromObject(e.grid);
    expect(b.max.x - b.min.x).toBeCloseTo(ROOM_SIZE);
    expect(b.max.z - b.min.z).toBeCloseTo(ROOM_SIZE);
    // 9 lines each way, two points per line
    expect(e.grid.geometry.getAttribute('position').count).toBe((ROOM_SIZE + 1) * 4);
    e.setGridVisible(false);
    expect(e.gridVisible).toBe(false);
    v.update({ ...empty, items });
    expect(v.meshOf(1)).toBe(mesh);
    expect(mesh.position.equals(before)).toBe(true);
    expect(v.itemCount).toBe(1);
  });

  it('AC2: picking maps rays to tiles, surface slots and wall positions, and nothing outside', () => {
    const v = new RoomView();
    const items = [
      item({ placedId: 1, itemId: 31, layer: LAYER_FLOOR, x: 1, y: 1 }),
      item({ placedId: 2, itemId: 49, layer: LAYER_SURFACE, parentPlacedId: 1, slot: 0 }),
      item({ placedId: 3, itemId: 57, layer: LAYER_WALL, side: 3, x: 5 }),
    ];
    v.update({ ...empty, items });
    const pick = (r: THREE.Ray) => pickRoom(v, items, furnitureDef, r);
    expect(pick(down(5.5, 6.5))).toEqual({ kind: 'tile', x: 5, y: 6 });
    expect(pick(down(0.1, 7.9))).toEqual({ kind: 'tile', x: 0, y: 7 });
    // table is 2x1 at tiles (1,1)-(2,1); its slot 1 is the east half, slot 0 holds the mug
    const east = pick(down(2.5, 1.5));
    expect(east).toMatchObject({ kind: 'surface', parentPlacedId: 1, slot: 1 });
    // hitting the item on the table also gives the table, and the nearest free slot
    const mug = pick(down(1.5, 1.5));
    expect(mug).toMatchObject({ kind: 'surface', parentPlacedId: 1, slot: 1 });
    // north wall from inside
    const wall = pick(new THREE.Ray(new THREE.Vector3(3.5, 1.5, 4), new THREE.Vector3(0, 0, -1)));
    expect(wall).toEqual({ kind: 'wall', side: SIDE_NORTH, pos: 3 });
    // a wall item is on the west wall at 5..7: hitting it gives that wall and the position under the ray
    const west = pick(new THREE.Ray(new THREE.Vector3(4, 1.2, 6.5), new THREE.Vector3(-1, 0, 0)));
    expect(west).toEqual({ kind: 'wall', side: SIDE_WEST, pos: 6 });
    // outside the room
    expect(pick(down(9.5, 3))).toBeNull();
    expect(pick(down(-1, -1))).toBeNull();
    expect(pick(new THREE.Ray(new THREE.Vector3(3, 1, 4), new THREE.Vector3(0, 1, 0)))).toBeNull();
  });

  it('AC3: the ghost is green or red the way room.go decides (room_test.go cases)', () => {
    const v = new RoomView();
    const e = new RoomEditView(v, defOf);
    const items = [
      item({ placedId: 1, itemId: 3, layer: LAYER_FLOOR, x: 0, y: 0 }), // table at 0,0
      item({ placedId: 2, itemId: 1, layer: LAYER_FLOOR, x: 5, y: 5 }), // chair
      item({ placedId: 10, itemId: 4, layer: LAYER_SURFACE, parentPlacedId: 1, slot: 0 }),
      item({ placedId: 11, itemId: 4, layer: LAYER_SURFACE, parentPlacedId: 1, slot: 1 }),
      item({ placedId: 5, itemId: 5, layer: LAYER_RUG, x: 3, y: 3 }),
    ];
    const cases: [string, number, Placement, string][] = [
      ['adjacent', 1, floorPlacement(2, 0), 'ok'],
      ['overlap', 1, floorPlacement(1, 0), 'overlap'],
      ['edge fits', 2, floorPlacement(6, 7), 'ok'],
      ['right out', 2, floorPlacement(7, 3), 'out_of_bounds'],
      ['negative', 1, floorPlacement(-1, 0), 'out_of_bounds'],
      ['rot 90 swaps', 2, floorPlacement(0, 7, 1), 'out_of_bounds'],
      ['rot 90 fits', 2, floorPlacement(5, 5, 1), 'overlap'], // chair is at 5,5 here
      ['rot 270 out', 2, floorPlacement(7, 7, 3), 'out_of_bounds'],
      ['rot 180 keeps', 2, floorPlacement(6, 4, 2), 'ok'],
      ['surface-only on floor', 4, floorPlacement(4, 4), 'bad_layer'],
      ['rug beside rug', 5, floorPlacement(4, 3), 'overlap'],
      ['rug under chair', 5, floorPlacement(5, 5), 'ok'],
      ['full surface', 4, on(1, -1), 'surface_full'],
      ['taken slot', 4, on(1, 0), 'surface_full'],
      ['bad slot', 4, on(1, 2), 'out_of_bounds'],
      ['not surface item', 1, on(1, -1), 'not_surface_item'],
      ['parent has no slots', 4, on(2, -1), 'no_surface'],
      ['missing parent', 4, on(99, -1), 'no_surface'],
      ['wall ok', 6, { ...floorPlacement(0, 0), side: SIDE_NORTH, pos: 0 }, 'ok'],
      ['wall past end', 6, { ...floorPlacement(0, 0), side: 2, pos: 7 }, 'out_of_bounds'],
    ];
    for (const [name, id, p, want] of cases) {
      const c = e.setGhost(items, id, p);
      expect(c.result, name).toBe(want);
      expect(hex(e), name).toBe(want === 'ok' ? GHOST_VALID : GHOST_BLOCKED);
      expect(e.ghost.visible, name).toBe(true);
    }
    // a free surface is green, and moving an item ignores itself
    expect(e.setGhost(items.slice(0, 2), 4, on(1, -1)).result).toBe('ok');
    expect(hex(e)).toBe(GHOST_VALID);
    expect(e.setGhost(items, 1, floorPlacement(5, 5), 2).result).toBe('ok');
    // room.go Move: a table still holding items can't move
    expect(e.setGhost(items, 3, floorPlacement(2, 2), 1).result).toBe('has_items');
    expect(hex(e)).toBe(GHOST_BLOCKED);
    e.hideGhost();
    expect(e.ghost.visible).toBe(false);
  });

  it('the ghost of a real piece stands on the tiles it would cover', () => {
    const v = new RoomView();
    const e = new RoomEditView(v);
    e.setGhost([], 31, floorPlacement(3, 4, 1));
    expect(e.ghostValid).toBe(true);
    expect(e.ghost.position.x).toBeCloseTo(3.5);
    expect(e.ghost.position.z).toBeCloseTo(5);
  });

  it('the default defs match items.json the way homeroom.go maps them', () => {
    interface Item { id: number; kind: string; furniture?: { width: number; depth: number; layer: string; surface_slots: number; on_surface: boolean } }
    const items: Item[] = JSON.parse(readFileSync(resolve(__dirname, '../../../server/internal/systems/gamedata/data/items.json'), 'utf8'));
    const bits: Record<string, number> = { floor: LAYER_FLOOR, rug: LAYER_RUG, surface: LAYER_SURFACE, wall: LAYER_WALL };
    const furniture = items.filter((i) => i.kind === 'furniture');
    expect(furniture.length).toBeGreaterThan(0);
    for (const { id, furniture: f } of furniture) {
      const want: PlaceDef = { w: f!.width, d: f!.depth, layers: bits[f!.layer]! | (f!.on_surface ? LAYER_SURFACE : 0), slots: f!.surface_slots };
      expect(furnitureDef(id), `item ${id}`).toEqual(want);
    }
    // so with the real items a rug goes under a chair and a desk lamp can't stand on the floor
    const e = new RoomEditView(new RoomView());
    const room = [item({ placedId: 1, itemId: 29, layer: LAYER_FLOOR, x: 3, y: 3 })];
    expect(e.setGhost(room, 39, floorPlacement(3, 3)).result).toBe('ok');
    expect(e.setGhost(room, 46, floorPlacement(5, 5)).result).toBe('bad_layer');
    expect(e.setGhost(room, 29, on(1, -1)).result).toBe('not_surface_item');
  });
});
