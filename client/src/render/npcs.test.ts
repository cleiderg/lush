import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { WorldScene, countDrawCalls } from './scene';
import { GENERIC_NPC, NPC_MODELS, NpcType, Npcs, makeNpcGeometry, npcModelKey } from './npcs';
import { CHARACTER_BUDGET } from './modelBudget';
import type { NpcView, WorldView } from './worldView';

const TYPES = Object.values(NpcType);
const base: WorldView = { local: { id: 1, x: 0, z: 0, heading: 0 }, remotes: [{ id: 2, x: 3, z: 1, heading: 0 }] };

function sceneShape(w: WorldScene): string[] {
  const out: string[] = [];
  w.scene.traverse((o) => out.push(`${o.type}:${o.name}:${o.children.length}`));
  return out;
}

function spread(n: number): NpcView[] {
  const out: NpcView[] = [];
  for (let i = 0; i < n; i++) out.push({ id: 1000 + i, npcType: TYPES[i % TYPES.length], x: (i % 10) * 2, z: Math.floor(i / 10) * 2, heading: i * 0.3 });
  return out;
}

function npcGroup(w: WorldScene): THREE.Object3D {
  return w.scene.getObjectByName('npcs')!;
}

function instanceMatrix(mesh: THREE.InstancedMesh, i: number): THREE.Matrix4 {
  const m = new THREE.Matrix4();
  mesh.getMatrixAt(i, m);
  return m;
}

describe('npcs (AC1)', () => {
  it('adds nothing to the scene when the view has no npcs field', () => {
    const w = new WorldScene(() => 0);
    w.update(base);
    w.update({ ...base, local: { ...base.local, x: 1 } });
    const fresh = new WorldScene(() => 0);
    fresh.update(base);
    expect(sceneShape(w)).toEqual(sceneShape(fresh));
    expect(w.scene.getObjectByName('npcs')).toBeUndefined();
    expect(w.scene.getObjectByName(`npc-${NpcType.Rabbit}`)).toBeUndefined();
  });
});

describe('npc models (AC2)', () => {
  it('has a distinct model for each of the six types and a generic fallback', () => {
    expect(TYPES).toEqual([1, 2, 3, 4, 5, 6]);
    const shapes = new Set(TYPES.map((t) => JSON.stringify(NPC_MODELS[t])));
    expect(shapes.size).toBe(6);
    expect(shapes.has(JSON.stringify(NPC_MODELS[GENERIC_NPC]))).toBe(false);
    expect(npcModelKey(99)).toBe(GENERIC_NPC);
    expect(npcModelKey(-1)).toBe(GENERIC_NPC);
    expect(npcModelKey(NpcType.Crow)).toBe(NpcType.Crow);
  });

  it('builds every type under the 1,500 triangle character budget, untextured and flat-coloured', () => {
    for (const key of [...TYPES, GENERIC_NPC]) {
      const g = makeNpcGeometry(key);
      const tris = g.getAttribute('position').count / 3;
      expect(tris).toBeGreaterThan(0);
      expect(tris).toBeLessThan(CHARACTER_BUDGET.maxTriangles);
      expect(g.getAttribute('uv')).toBeUndefined();
    }
    const w = new WorldScene(() => 0);
    w.update({ ...base, npcs: [...spread(6), { id: 9, npcType: 42, x: 0, z: 0, heading: 0 }] });
    let meshes = 0;
    npcGroup(w).traverse((o) => {
      const mesh = o as THREE.Mesh;
      if (!mesh.isMesh) return;
      meshes++;
      for (const mat of Array.isArray(mesh.material) ? mesh.material : [mesh.material]) {
        expect((mat as THREE.MeshLambertMaterial).flatShading).toBe(true);
        for (const v of Object.values(mat)) expect((v as THREE.Texture)?.isTexture).not.toBe(true);
      }
    });
    expect(meshes).toBe(7);
  });
});

describe('npc instancing (AC3)', () => {
  it('uses the same draw calls for 100 NPCs over all six types as for one of each', () => {
    const one = new WorldScene(() => 0);
    one.update({ ...base, npcs: spread(6) });
    const many = new WorldScene(() => 0);
    many.update({ ...base, npcs: spread(100) });
    expect(countDrawCalls(npcGroup(one))).toBe(6);
    expect(countDrawCalls(npcGroup(many))).toBe(6);
    expect(countDrawCalls(many.scene)).toBe(countDrawCalls(one.scene));
    const drawn = TYPES.reduce((s, t) => s + (many.scene.getObjectByName(`npc-${t}`) as THREE.InstancedMesh).count, 0);
    expect(drawn).toBe(100);
  });

  it('hides an NPC removed from the view, and all of them when npcs goes away', () => {
    const w = new WorldScene(() => 0);
    const rabbits: NpcView[] = [
      { id: 10, npcType: NpcType.Rabbit, x: 1, z: 1, heading: 0 },
      { id: 11, npcType: NpcType.Rabbit, x: 4, z: 4, heading: 0 },
    ];
    w.update({ ...base, npcs: [...rabbits, { id: 12, npcType: NpcType.Crow, x: 2, z: 2, heading: 0 }] });
    const mesh = w.scene.getObjectByName(`npc-${NpcType.Rabbit}`) as THREE.InstancedMesh;
    const crow = w.scene.getObjectByName(`npc-${NpcType.Crow}`) as THREE.InstancedMesh;
    expect(mesh.count).toBe(2);
    w.update({ ...base, npcs: [rabbits[1]] });
    expect(mesh.count).toBe(1);
    expect(crow.count).toBe(0);
    expect(new THREE.Vector3().setFromMatrixPosition(instanceMatrix(mesh, 0)).x).toBe(4);
    expect(countDrawCalls(npcGroup(w))).toBe(1);
    w.update(base);
    expect(countDrawCalls(npcGroup(w))).toBe(0);
  });

  it('grows a type past its initial capacity without adding draw calls', () => {
    const n = new Npcs();
    const lots = Array.from({ length: 200 }, (_, i) => ({ id: i, npcType: NpcType.Squirrel, x: i, z: 0, heading: 0 }));
    n.update(lots);
    expect(n.meshFor(NpcType.Squirrel)!.count).toBe(200);
    expect(countDrawCalls(n.group)).toBe(1);
  });
});

describe('npc placement (AC4)', () => {
  it('follows the view position and heading', () => {
    const w = new WorldScene(() => 0);
    const at = (x: number, z: number, heading: number): WorldView => ({ ...base, npcs: [{ id: 5, npcType: NpcType.Coyote, x, z, heading }] });
    const mesh = () => w.scene.getObjectByName(`npc-${NpcType.Coyote}`) as THREE.InstancedMesh;
    for (const [x, z, h] of [
      [3, -2, 0],
      [7.5, 4, Math.PI / 2],
      [-1, 9, -2.2],
    ]) {
      w.update(at(x, z, h));
      const p = new THREE.Vector3();
      const q = new THREE.Quaternion();
      instanceMatrix(mesh(), 0).decompose(p, q, new THREE.Vector3());
      expect(p.x).toBeCloseTo(x);
      expect(p.y).toBeCloseTo(0);
      expect(p.z).toBeCloseTo(z);
      const fwd = new THREE.Vector3(0, 0, 1).applyQuaternion(q);
      expect(fwd.x).toBeCloseTo(Math.sin(h));
      expect(fwd.z).toBeCloseTo(Math.cos(h));
    }
  });
});

describe('npc hitsplats (AC5)', () => {
  it('shows a splat at an NPC that is hit', () => {
    const w = new WorldScene(() => 1000);
    w.update({ ...base, npcs: [{ id: 300, npcType: NpcType.Crow, x: 6, z: -3, heading: 0 }], hits: [{ target: 300, damage: 4, died: false }] });
    const s = w.hitsplats.active;
    expect(s).toHaveLength(1);
    expect(s[0].target).toBe(300);
    expect(s[0].position.x).toBe(6);
    expect(s[0].position.z).toBe(-3);
    expect((w.scene.getObjectByName('hitsplat-discs') as THREE.InstancedMesh).count).toBe(1);
  });

  it('ignores a hit on an NPC id that is not in the view', () => {
    const w = new WorldScene(() => 1000);
    w.update({ ...base, hits: [{ target: 300, damage: 4, died: false }] });
    expect(w.hitsplats.active).toHaveLength(0);
  });
});
