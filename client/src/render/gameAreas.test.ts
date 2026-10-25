import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { CampusMap, loadCampus } from './campusMap';
import { AreaMarkers, BORDER_WIDTH, loadAreas } from './gameAreas';
import { parseAreas } from './mapData';
import { WorldScene, countDrawCalls } from './scene';

const areasFile = parseAreas(readFileSync(join(resolve(__dirname, '../../../shared/map'), 'areas.json'), 'utf8'));
const pvp = areasFile.areas.find((a) => a.kind === 'pvp')!;

describe('PvP zone border (AC1)', () => {
  const m = new AreaMarkers(loadAreas());

  it('follows the areas.json PvP polygon, converted from decimetres to metres', () => {
    const want = Array.from({ length: pvp.p.length / 2 }, (_, i) => [pvp.p[2 * i] / 10, pvp.p[2 * i + 1] / 10]);
    expect(m.borderPoints).toEqual([want]);
    const pos = m.border.geometry.getAttribute('position');
    expect(pos.count).toBe(want.length * 2);
    for (let i = 0; i < want.length; i++) {
      const a = new THREE.Vector3().fromBufferAttribute(pos, 2 * i);
      const b = new THREE.Vector3().fromBufferAttribute(pos, 2 * i + 1);
      expect((a.x + b.x) / 2).toBeCloseTo(want[i][0], 3);
      expect((a.z + b.z) / 2).toBeCloseTo(want[i][1], 3);
      expect(a.distanceTo(b)).toBeGreaterThanOrEqual(BORDER_WIDTH - 1e-6);
      expect(a.y).toBeGreaterThan(0);
      expect(a.y).toBeLessThan(0.2);
    }
  });

  it('is red', () => {
    const c = (m.border.material as THREE.MeshBasicMaterial).color;
    expect(c.r).toBeGreaterThan(0.5);
    expect(c.g).toBeLessThan(0.15);
    expect(c.b).toBeLessThan(0.15);
  });

  it('is in the world scene', () => {
    expect(new WorldScene().scene.getObjectByName('pvp-border')).toBeDefined();
  });
});

describe('shop stall (AC2)', () => {
  const m = new AreaMarkers(loadAreas());

  it('stands on the ground at the areas.json shop point', () => {
    expect(m.stall.position.x).toBeCloseTo(areasFile.shop.x / 10);
    expect(m.stall.position.z).toBeCloseTo(areasFile.shop.z / 10);
    m.stall.geometry.computeBoundingBox();
    const bb = m.stall.geometry.boundingBox!;
    expect(bb.min.y).toBeCloseTo(0);
    expect(bb.max.y).toBeGreaterThan(2);
    expect(bb.max.x - bb.min.x).toBeLessThan(5);
    const world = new THREE.Vector3();
    m.stall.updateMatrixWorld();
    expect(m.stall.getWorldPosition(world).x).toBeCloseTo(areasFile.shop.x / 10);
  });

  it('is low-poly and flat-coloured with no textures', () => {
    const mat = m.stall.material as THREE.MeshLambertMaterial;
    expect(mat.vertexColors).toBe(true);
    for (const k of ['map', 'emissiveMap', 'alphaMap', 'aoMap', 'lightMap', 'specularMap', 'envMap'] as const) {
      expect(mat[k]).toBeNull();
    }
    expect((m.border.material as THREE.MeshBasicMaterial).map).toBeNull();
    expect(m.stall.geometry.getAttribute('uv')).toBeUndefined();
    expect(m.stall.geometry.getAttribute('position').count / 3).toBeLessThan(500);
  });
});

describe('draw calls (AC3)', () => {
  it('adds at most 2 draw calls to the map', () => {
    const map = new CampusMap(loadCampus());
    const before = countDrawCalls(map.group);
    const markers = new AreaMarkers(loadAreas());
    map.group.add(markers.group);
    const after = countDrawCalls(map.group);
    console.log(`map draw calls: ${before} before, ${after} with PvP border and shop stall`);
    expect(after - before).toBeGreaterThan(0);
    expect(after - before).toBeLessThanOrEqual(2);
  });
});
