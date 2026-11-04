import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { FLOOR_IDS, FURNITURE_IDS, WALLPAPER_IDS, buildFurniture, isWallItem, surfaceMaterial } from './furniture';
import { PROP_BUDGET } from './modelBudget';

interface Item {
  id: number;
  kind: string;
  furniture?: { width: number; depth: number; layer: string; surface_slots: number };
}
const items: Item[] = JSON.parse(readFileSync(resolve(__dirname, '../../../server/internal/systems/gamedata/data/items.json'), 'utf8'));
const furniture = items.filter((i) => i.kind === 'furniture');
const EPS = 1e-6;

const box = (id: number) => new THREE.Box3().setFromBufferAttribute(buildFurniture(id).mesh.geometry.getAttribute('position') as THREE.BufferAttribute);

describe('furniture models', () => {
  it('AC1: builds a mesh for every furniture id, a material for every wallpaper and floor', () => {
    expect(furniture.length).toBe(30);
    for (const f of furniture) expect(buildFurniture(f.id).mesh).toBeInstanceOf(THREE.Mesh);
    expect(FURNITURE_IDS).toEqual(furniture.map((f) => f.id));
    const papers = items.filter((i) => i.kind === 'wallpaper').map((i) => i.id);
    const floors = items.filter((i) => i.kind === 'floor_covering').map((i) => i.id);
    expect(papers.length).toBe(6);
    expect(floors.length).toBe(6);
    expect(WALLPAPER_IDS).toEqual(papers);
    expect(FLOOR_IDS).toEqual(floors);
    for (const id of [...papers, ...floors]) expect(surfaceMaterial(id)).toBeInstanceOf(THREE.Material);
    expect(() => buildFurniture(1)).toThrow();
  });

  it('AC2: fits the footprint, wall items are thin, floor items sit on y = 0', () => {
    for (const f of furniture) {
      const { width, depth, layer } = f.furniture!;
      const bb = box(f.id);
      expect(bb.max.x - bb.min.x, `${f.id} width`).toBeLessThanOrEqual(width + EPS);
      expect(bb.max.z - bb.min.z, `${f.id} depth`).toBeLessThanOrEqual(depth + EPS);
      expect(Math.max(-bb.min.x, bb.max.x)).toBeLessThanOrEqual(width / 2 + EPS);
      expect(Math.max(-bb.min.z, bb.max.z)).toBeLessThanOrEqual(depth / 2 + EPS);
      expect(isWallItem(f.id)).toBe(layer === 'wall');
      if (layer === 'wall') {
        expect(bb.max.z - bb.min.z, `${f.id} thin`).toBeLessThanOrEqual(0.2);
        expect(bb.max.y).toBeLessThanOrEqual(1 + EPS);
      } else {
        expect(Math.abs(bb.min.y), `${f.id} on floor`).toBeLessThan(EPS);
      }
    }
  });

  it('AC3: exposes one anchor per surface slot, on the top face', () => {
    for (const f of furniture) {
      const anchors = buildFurniture(f.id).anchors;
      const { surface_slots, width, depth } = f.furniture!;
      expect(anchors.length, `${f.id} slots`).toBe(surface_slots);
      const top = box(f.id).max.y;
      for (const a of anchors) {
        expect(Math.abs(a.x)).toBeLessThanOrEqual(width / 2);
        expect(Math.abs(a.z)).toBeLessThanOrEqual(depth / 2);
        expect(a.y, `${f.id} anchor height`).toBeGreaterThan(top - 0.1);
        expect(a.y).toBeLessThanOrEqual(top + EPS);
      }
      expect(new Set(anchors.map((a) => `${a.x},${a.z}`)).size).toBe(anchors.length);
    }
  });

  it('AC4: every model stays under the prop triangle budget', () => {
    for (const f of furniture) {
      const g = buildFurniture(f.id).mesh.geometry;
      expect(g.getAttribute('position').count / 3, `${f.id} triangles`).toBeLessThan(PROP_BUDGET.maxTriangles);
    }
  });
});
