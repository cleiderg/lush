import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { doorGeometry, HousingLayer, signGeometry, stallGeometry, workbenchGeometry } from './housingLayer';
import { parseCampus, parseHousing } from './mapData';
import { PROP_BUDGET } from './modelBudget';
import { countDrawCalls } from './scene';

const mapDir = resolve(__dirname, '../../../shared/map');
const housing = parseHousing(readFileSync(join(mapDir, 'housing.json'), 'utf8'));
const campus = parseCampus(readFileSync(join(mapDir, 'campus.json'), 'utf8'));
const layer = new HousingLayer(housing, campus.buildings);

const at = (mesh: THREE.InstancedMesh, i: number) => {
  const m = new THREE.Matrix4();
  mesh.getMatrixAt(i, m);
  return new THREE.Vector3().setFromMatrixPosition(m);
};
const tris = (g: THREE.BufferGeometry) => (g.index ? g.index.count : g.getAttribute('position').count) / 3;

describe('housing layer (AC1)', () => {
  it('has 40 houses in 2 areas', () => {
    expect(housing.houses).toHaveLength(40);
    expect(housing.areas).toHaveLength(2);
  });

  it('puts one door and one sign within 1 m of each house door point', () => {
    expect(layer.doors.count).toBe(40);
    expect(layer.signs.count).toBe(40);
    housing.houses.forEach((h, i) => {
      for (const mesh of [layer.doors, layer.signs]) {
        const p = at(mesh, i);
        expect(Math.hypot(p.x - h.x / 10, p.z - h.z / 10)).toBeLessThan(1);
      }
    });
  });

  it('puts one stall and one workbench per area at its points', () => {
    expect(layer.stalls.count).toBe(2);
    expect(layer.workbenches.count).toBe(2);
    housing.areas.forEach((a, i) => {
      const s = at(layer.stalls, i), w = at(layer.workbenches, i);
      expect([s.x, s.z]).toEqual([a.thrift.x / 10, a.thrift.z / 10].map((v) => expect.closeTo(v, 5)));
      expect([w.x, w.z]).toEqual([a.workbench.x / 10, a.workbench.z / 10].map((v) => expect.closeTo(v, 5)));
    });
  });
});

describe('housing models (AC2)', () => {
  it('each model is within the props/ triangle budget', () => {
    for (const g of [stallGeometry(), workbenchGeometry(), doorGeometry(), signGeometry()]) {
      expect(tris(g)).toBeGreaterThan(0);
      expect(tris(g)).toBeLessThanOrEqual(PROP_BUDGET.maxTriangles);
    }
  });

  it('draws all doors and signs in at most 4 draw calls', () => {
    const g = new THREE.Group();
    layer.update(housing.houses[0].x / 10, housing.houses[0].z / 10);
    g.add(layer.doors, layer.signs);
    expect(countDrawCalls(g)).toBeLessThanOrEqual(4);
    expect(countDrawCalls(layer.group)).toBeLessThanOrEqual(4);
    expect(layer.doors.isInstancedMesh && layer.signs.isInstancedMesh).toBe(true);
  });

  it('is hidden away from the housing areas and shown beside one', () => {
    layer.update(0, 0);
    expect(layer.group.visible).toBe(false);
    layer.update(housing.areas[1].thrift.x / 10, housing.areas[1].thrift.z / 10);
    expect(layer.group.visible).toBe(true);
  });
});
