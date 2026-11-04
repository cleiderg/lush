import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { surfaceMaterial } from './furniture';
import { DEFAULT_FLOOR_MATERIAL, DEFAULT_WALL_MATERIAL, LAYER_FLOOR, LAYER_RUG, LAYER_SURFACE, LAYER_WALL, ROOM_SIZE, RoomView } from './roomView';
import type { RoomItem } from './roomView';

const item = (o: Partial<RoomItem> & Pick<RoomItem, 'placedId' | 'itemId' | 'layer'>): RoomItem =>
  ({ x: 0, y: 0, rotation: 0, parentPlacedId: 0, slot: 0, side: 0, ...o });
const empty = { wallpaperId: 0, floorId: 0, items: [] as RoomItem[] };
const bounds = (o: THREE.Object3D) => { o.updateMatrixWorld(true); return new THREE.Box3().setFromObject(o); };

describe('RoomView', () => {
  it('AC1: an empty room has an 8x8 floor, four walls and default materials', () => {
    const v = new RoomView();
    v.update(empty);
    const fb = bounds(v.floor);
    expect(fb.max.x - fb.min.x).toBeCloseTo(ROOM_SIZE);
    expect(fb.max.z - fb.min.z).toBeCloseTo(ROOM_SIZE);
    expect(Object.keys(v.walls)).toHaveLength(4);
    for (const w of Object.values(v.walls)) {
      expect(v.group.children).toContain(w);
      expect(w.material).toBe(DEFAULT_WALL_MATERIAL);
    }
    expect(v.floor.material).toBe(DEFAULT_FLOOR_MATERIAL);
    expect(v.itemCount).toBe(0);
    v.update({ ...empty, wallpaperId: 64, floorId: 66 });
    expect(v.walls.north.material).toBe(surfaceMaterial(64));
    expect(v.floor.material).toBe(surfaceMaterial(66));
  });

  it('AC2: a rotated 2x1 item is centred on the 1x2 tiles it covers; a rug lies beneath a floor item', () => {
    const v = new RoomView();
    v.update({ ...empty, items: [item({ placedId: 1, itemId: 31, layer: LAYER_FLOOR, x: 3, y: 4, rotation: 1 })] });
    const m = v.meshOf(1)!;
    expect(m.position.x).toBeCloseTo(3.5);
    expect(m.position.z).toBeCloseTo(5);
    const b = bounds(m);
    expect(b.min.x).toBeGreaterThanOrEqual(3 - 1e-6);
    expect(b.max.x).toBeLessThanOrEqual(4 + 1e-6);
    expect(b.min.z).toBeGreaterThanOrEqual(4 - 1e-6);
    expect(b.max.z).toBeLessThanOrEqual(6 + 1e-6);
    expect(b.max.z - b.min.z).toBeGreaterThan(1.8);

    v.update({ ...empty, items: [
      item({ placedId: 2, itemId: 39, layer: LAYER_RUG, x: 2, y: 2 }),
      item({ placedId: 3, itemId: 29, layer: LAYER_FLOOR, x: 2, y: 2 }),
    ] });
    const rug = bounds(v.meshOf(2)!), chair = bounds(v.meshOf(3)!);
    expect(v.meshOf(2)!.position.x).toBeCloseTo(3);
    expect(v.meshOf(3)!.position.x).toBeCloseTo(2.5);
    expect(chair.min.y).toBeGreaterThanOrEqual(rug.max.y - 1e-6);
  });

  it('AC3: a surface item sits on its slot anchor; a 2-wide wall item sits flush on the east wall', () => {
    const v = new RoomView();
    v.update({ ...empty, items: [
      item({ placedId: 1, itemId: 31, layer: LAYER_FLOOR, x: 1, y: 1 }),
      item({ placedId: 2, itemId: 49, layer: LAYER_SURFACE, parentPlacedId: 1, slot: 1 }),
      item({ placedId: 3, itemId: 57, layer: LAYER_WALL, side: 1, x: 2 }),
    ] });
    const table = v.meshOf(1)!.position, mug = v.meshOf(2)!.position;
    expect(table.x).toBeCloseTo(2);
    expect(mug.x).toBeCloseTo(table.x + 0.5);
    expect(mug.z).toBeCloseTo(table.z);
    expect(mug.y).toBeCloseTo(table.y + 0.76);
    const b = bounds(v.meshOf(3)!);
    expect(b.max.x).toBeCloseTo(ROOM_SIZE);
    expect(b.min.z).toBeGreaterThanOrEqual(2 - 1e-6);
    expect(b.max.z).toBeLessThanOrEqual(4 + 1e-6);
    expect(v.meshOf(3)!.position.z).toBeCloseTo(3);
    // the surface item follows a rotated parent
    v.update({ ...empty, items: [
      item({ placedId: 1, itemId: 31, layer: LAYER_FLOOR, x: 1, y: 1, rotation: 1 }),
      item({ placedId: 2, itemId: 49, layer: LAYER_SURFACE, parentPlacedId: 1, slot: 1 }),
    ] });
    const t = v.meshOf(1)!.position, m = v.meshOf(2)!.position;
    expect(m.x).toBeCloseTo(t.x);
    expect(Math.abs(m.z - t.z)).toBeCloseTo(0.5);
  });

  it('AC4: updating adds, moves and removes meshes without rebuilding unchanged items', () => {
    const v = new RoomView();
    const a = item({ placedId: 1, itemId: 29, layer: LAYER_FLOOR, x: 0, y: 0 });
    const b = item({ placedId: 2, itemId: 30, layer: LAYER_FLOOR, x: 5, y: 5 });
    v.update({ ...empty, items: [a, b] });
    const ma = v.meshOf(1)!, mb = v.meshOf(2)!;
    const c = item({ placedId: 3, itemId: 44, layer: LAYER_FLOOR, x: 3, y: 3 });
    v.update({ ...empty, items: [a, { ...b, x: 6 }, c] });
    expect(v.meshOf(1)).toBe(ma);
    expect(v.meshOf(2)).toBe(mb);
    expect(mb.position.x).toBeCloseTo(6.5);
    expect(v.meshOf(3)).toBeDefined();
    expect(v.itemCount).toBe(3);
    v.update({ ...empty, items: [a, c] });
    expect(v.meshOf(2)).toBeUndefined();
    expect(v.group.children).not.toContain(mb);
    expect(v.meshOf(1)).toBe(ma);
    expect(v.itemCount).toBe(2);
  });
});
