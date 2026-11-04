import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { doorGeometry, HousingLayer, signGeometry, stallGeometry, workbenchGeometry } from './housingLayer';
import { parseCampus, parseHousing } from './mapData';
import { PROP_BUDGET } from './modelBudget';
import { countDrawCalls, WorldScene } from './scene';

const mapDir = resolve(__dirname, '../../../shared/map');
const housing = parseHousing(readFileSync(join(mapDir, 'housing.json'), 'utf8'));
const campus = parseCampus(readFileSync(join(mapDir, 'campus.json'), 'utf8'));
const layer = new HousingLayer(housing, campus.buildings);

const at = (mesh: THREE.InstancedMesh, i: number) => {
  const m = new THREE.Matrix4();
  mesh.getMatrixAt(i, m);
  return new THREE.Vector3().setFromMatrixPosition(m);
};
/** A camera at (x, z) in metres, 30 m up, looking at the ground ahead of it. */
const cameraAt = (x: number, z: number) => {
  const c = new THREE.PerspectiveCamera(55, 9 / 19.5, 0.1, 500);
  c.position.set(x, 30, z);
  c.lookAt(x, 0, z + 40);
  return c;
};
/** Every door, stall and workbench point, in metres. */
const points = [...housing.houses, ...housing.areas.flatMap((a) => [a.thrift, a.workbench])].map((p) => [p.x / 10, p.z / 10]);
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
    layer.update(cameraAt(housing.houses[0].x / 10, housing.houses[0].z / 10), 300);
    g.add(layer.doors, layer.signs);
    expect(countDrawCalls(g)).toBeLessThanOrEqual(4);
    expect(countDrawCalls(layer.group)).toBeLessThanOrEqual(4);
    expect(layer.doors.isInstancedMesh && layer.signs.isInstancedMesh).toBe(true);
  });

  it('is hidden away from the housing areas and shown beside one', () => {
    layer.update(cameraAt(-300, 0), 300); // ~600 m from both areas
    expect(layer.group.visible).toBe(false);
    const t = housing.areas[1].thrift;
    const cam = cameraAt(t.x / 10, t.z / 10);
    layer.update(cam, 300);
    expect(layer.group.visible).toBe(true);
    cam.lookAt(t.x / 10, 100, t.z / 10 + 1); // at the stall, but looking up at the sky
    layer.update(cam, 300);
    expect(layer.group.visible).toBe(false);
  });

  it('is hidden only where every door, stall and workbench in view is past the fog', () => {
    const t = housing.areas[1].thrift;
    for (const fogFar of [160, 300]) {
      // Step away from the stall until the layer hides, looking straight back at it.
      let d = 0;
      let cam = cameraAt(t.x / 10, t.z / 10);
      for (; layer.update(cam, fogFar), layer.group.visible; d += 1) {
        cam = cameraAt(t.x / 10, t.z / 10 - d);
        cam.lookAt(t.x / 10, 0, t.z / 10);
      }
      // Daylight fog reaches 300 m, so hiding at a fixed 120 m would pop in plain view.
      expect(d).toBeGreaterThan(fogFar);
      cam.updateMatrixWorld();
      for (const p of points) {
        const v = new THREE.Vector3(p[0], 0, p[1]).applyMatrix4(cam.matrixWorldInverse);
        const ndc = new THREE.Vector3(p[0], 0, p[1]).project(cam);
        if (Math.abs(ndc.x) <= 1 && Math.abs(ndc.y) <= 1 && ndc.z <= 1) expect(-v.z).toBeGreaterThan(fogFar);
      }
    }
  });

  it('stays hidden while the player is at home by their door', () => {
    const w = new WorldScene();
    const h = housing.houses[0];
    const local = { id: 0, x: h.x / 10, z: h.z / 10, heading: 0 };
    w.update({ local, remotes: [] });
    expect(w.housing.group.visible).toBe(true);
    w.update({ local, remotes: [], home: { wallpaperId: 0, floorId: 0, items: [] } });
    expect(w.housing.group.visible).toBe(false);
    w.update({ local, remotes: [] });
    expect(w.housing.group.visible).toBe(true);
  });
});
