import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { WorldScene, countDrawCalls } from './scene';
import { GROUND_ITEM_CAPACITY, groundItemColor } from './groundItems';
import type { GroundItemView, WorldView } from './worldView';

const base: WorldView = { local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [] };

function items(n: number, dx = 0): GroundItemView[] {
  const out: GroundItemView[] = [];
  for (let i = 0; i < n; i++) out.push({ id: 100 + i, itemId: i % 5, quantity: 1, x: (i % 20) * 1.5 + dx, z: Math.floor(i / 20) * 1.5 });
  return out;
}

function markers(w: WorldScene): THREE.InstancedMesh {
  return w.scene.getObjectByName('groundItemMarkers') as THREE.InstancedMesh;
}

/** Marker positions as "x,z" keyed by instance, read back from the instance matrices. */
function positions(mesh: THREE.InstancedMesh): { x: number; y: number; z: number }[] {
  const m = new THREE.Matrix4();
  const p = new THREE.Vector3();
  const out = [];
  for (let i = 0; i < mesh.count; i++) {
    mesh.getMatrixAt(i, m);
    p.setFromMatrixPosition(m);
    out.push({ x: p.x, y: p.y, z: p.z });
  }
  return out;
}

function sceneShape(w: WorldScene): string[] {
  const out: string[] = [];
  w.scene.traverse((o) => out.push(`${o.type}:${o.name}`));
  return out;
}

describe('ground item markers', () => {
  it('leaves the scene graph unchanged when groundItems is missing (AC1)', () => {
    const a = new WorldScene();
    const b = new WorldScene();
    a.update(base);
    a.update(base);
    expect(sceneShape(a)).toEqual(sceneShape(b));
    expect(a.scene.getObjectByName('groundItems')).toBeUndefined();
    expect(countDrawCalls(a.scene)).toBe(countDrawCalls(b.scene));
  });

  it('draws one marker per item on the ground at its x/z (AC2)', () => {
    const w = new WorldScene();
    const list = [
      { id: 1, itemId: 3, quantity: 5, x: 12.5, z: -4 },
      { id: 2, itemId: 7, quantity: 1, x: -30, z: 8.25 },
    ];
    w.update({ ...base, groundItems: list });
    const mesh = markers(w);
    expect(mesh.count).toBe(2);
    const ps = positions(mesh);
    list.forEach((it, i) => {
      expect(ps[i].x).toBeCloseTo(it.x);
      expect(ps[i].z).toBeCloseTo(it.z);
      // Resting on the ground: above y=0 and below knee height.
      expect(ps[i].y).toBeGreaterThan(0);
      expect(ps[i].y).toBeLessThan(0.5);
      mesh.geometry.computeBoundingBox();
      expect(ps[i].y + mesh.geometry.boundingBox!.min.y).toBeCloseTo(0, 3);
      const c = new THREE.Color();
      mesh.getColorAt(i, c);
      expect(c.getHex()).toBe(groundItemColor(it.itemId));
    });
  });

  it('adds, moves and removes markers as the list changes (AC2)', () => {
    const w = new WorldScene();
    const a = { id: 1, itemId: 1, quantity: 1, x: 1, z: 1 };
    const b = { id: 2, itemId: 2, quantity: 1, x: 2, z: 2 };
    const c = { id: 3, itemId: 3, quantity: 1, x: 3, z: 3 };

    w.update({ ...base, groundItems: [a] });
    expect(positions(markers(w)).map((p) => [p.x, p.z])).toEqual([[1, 1]]);

    w.update({ ...base, groundItems: [a, b, c] });
    expect(positions(markers(w)).map((p) => [p.x, p.z])).toEqual([[1, 1], [2, 2], [3, 3]]);

    w.update({ ...base, groundItems: [a, { ...b, x: 9, z: -9 }, c] });
    expect(positions(markers(w)).map((p) => [p.x, p.z])).toEqual([[1, 1], [9, -9], [3, 3]]);

    w.update({ ...base, groundItems: [c] });
    expect(positions(markers(w)).map((p) => [p.x, p.z])).toEqual([[3, 3]]);

    w.update({ ...base, groundItems: [] });
    expect(markers(w).count).toBe(0);

    w.update({ ...base, groundItems: [b] });
    w.update(base);
    expect(markers(w).count).toBe(0);
  });

  it('uses the same meshes and materials for 1 and 200 items (AC3)', () => {
    const census = (n: number) => {
      const w = new WorldScene();
      w.update({ ...base, groundItems: items(n) });
      w.update({ ...base, groundItems: items(n, 0.5) });
      const group = w.scene.getObjectByName('groundItems')!;
      const meshes: THREE.Mesh[] = [];
      const materials = new Set<THREE.Material>();
      const geometries = new Set<THREE.BufferGeometry>();
      group.traverse((o) => {
        const mesh = o as THREE.Mesh;
        if (!mesh.isMesh) return;
        meshes.push(mesh);
        geometries.add(mesh.geometry);
        for (const m of Array.isArray(mesh.material) ? mesh.material : [mesh.material]) materials.add(m);
      });
      return {
        meshes: meshes.length,
        instanced: meshes.every((m) => (m as THREE.InstancedMesh).isInstancedMesh),
        materials: materials.size,
        geometries: geometries.size,
        drawCalls: countDrawCalls(w.scene),
        count: markers(w).count,
      };
    };
    const one = census(1);
    const many = census(200);
    console.log(`ground items: 1 -> ${one.drawCalls} draw calls, 200 -> ${many.drawCalls} draw calls`);
    expect(one.count).toBe(1);
    expect(many.count).toBe(200);
    expect(many.meshes).toBe(one.meshes);
    expect(many.materials).toBe(one.materials);
    expect(many.geometries).toBe(one.geometries);
    expect(many.drawCalls).toBe(one.drawCalls);
    expect(one).toMatchObject({ meshes: 1, instanced: true, materials: 1, geometries: 1 });
  });

  it('grows past the initial pool without adding meshes', () => {
    const w = new WorldScene();
    w.update({ ...base, groundItems: items(1) });
    w.update({ ...base, groundItems: items(GROUND_ITEM_CAPACITY + 10) });
    const group = w.scene.getObjectByName('groundItems')!;
    expect(group.children.length).toBe(1);
    expect(markers(w).count).toBe(GROUND_ITEM_CAPACITY + 10);
    const last = positions(markers(w)).at(-1)!;
    const want = items(GROUND_ITEM_CAPACITY + 10).at(-1)!;
    expect([last.x, last.z]).toEqual([want.x, want.z]);
  });
});
