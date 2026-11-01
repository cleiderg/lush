import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { WorldScene, countDrawCalls } from './scene';
import { POND_EDGE, SKILL_TREE_KINDS, SkillObjects, Y_WATER, loadSkills } from './skillObjects';
import type { SkillsData } from './mapData';

const skills = loadSkills();
const DM = 0.1;

/** World position of instance i of an instanced mesh. */
function instancePos(mesh: THREE.InstancedMesh, i: number): THREE.Vector3 {
  const m = new THREE.Matrix4();
  mesh.getMatrixAt(i, m);
  return new THREE.Vector3().setFromMatrixPosition(m);
}

/** Every instance position (x, z, rounded to centimetres) drawn by a mesh. */
function positions(mesh: THREE.InstancedMesh): string[] {
  return Array.from({ length: mesh.count }, (_, i) => {
    const p = instancePos(mesh, i);
    return `${p.x.toFixed(2)},${p.z.toFixed(2)}`;
  }).sort();
}
/** A camera looking straight down over the whole park, so every tree is in view. */
function overPark(): THREE.Camera {
  const c = new THREE.OrthographicCamera(-200, 200, 200, -200, 1, 1000);
  c.position.set(-20, 500, -80);
  c.lookAt(-20, 0, -80);
  c.updateMatrixWorld();
  return c;
}
const key = (t: { x: number; z: number }) => `${(t.x * DM).toFixed(2)},${(t.z * DM).toFixed(2)}`;

describe('skill object models (AC1)', () => {
  it('draws each tree kind, the stump, the bank booth, the grill and the pond with its own geometry', () => {
    const s = new SkillObjects(skills);
    const geos = [...SKILL_TREE_KINDS.map((k) => s.trees[k].geometry), s.stumps.geometry, s.bank.geometry, s.grill.geometry, s.pond.geometry];
    expect(new Set(geos).size).toBe(geos.length);
    // Distinct models, not the same shape: vertex counts or heights differ between the tree kinds and the stump.
    const shapes = [...SKILL_TREE_KINDS.map((k) => s.trees[k]), s.stumps].map((m) => {
      m.geometry.computeBoundingBox();
      return `${m.geometry.getAttribute('position').count}:${m.geometry.boundingBox!.max.y.toFixed(2)}`;
    });
    expect(new Set(shapes).size).toBe(shapes.length);
    // Heights: stump low, then tree < oak < eucalyptus.
    const h = (g: THREE.BufferGeometry) => g.boundingBox!.max.y;
    expect(h(s.stumps.geometry)).toBeLessThan(0.6);
    expect(h(s.trees.tree.geometry)).toBeLessThan(h(s.trees.oak.geometry));
    expect(h(s.trees.oak.geometry)).toBeLessThan(h(s.trees.eucalyptus.geometry));
    for (const g of geos) expect(g.getAttribute('position').count / 3, 'low-poly').toBeLessThan(2000);
    s.dispose();
  });

  it('puts every tree at its skills.json position, in the mesh for its kind', () => {
    const s = new SkillObjects(skills);
    for (const k of SKILL_TREE_KINDS) {
      expect(positions(s.trees[k]), k).toEqual(skills.trees.filter((t) => t.kind === k).map(key).sort());
      for (let i = 0; i < s.trees[k].count; i++) expect(instancePos(s.trees[k], i).y).toBe(0);
    }
    expect(s.stumps.count).toBe(0);
    s.dispose();
  });

  it('puts the bank booth and grill at their skills.json positions', () => {
    const s = new SkillObjects(skills);
    for (const [mesh, p] of [[s.bank, skills.bank], [s.grill, skills.grill]] as const) {
      const w = new THREE.Vector3().setFromMatrixPosition(mesh.matrixWorld.copy(mesh.matrix));
      expect(w.x, mesh.name).toBeCloseTo(p.x * DM, 5);
      expect(w.z, mesh.name).toBeCloseTo(p.z * DM, 5);
    }
    s.dispose();
  });

  it('draws the pond water over its skills.json outline, darker at the edge than in the middle', () => {
    const s = new SkillObjects(skills);
    const g = s.pond.geometry;
    const pos = g.getAttribute('position'), col = g.getAttribute('color');
    const out = skills.pond.p;
    const onOutline = (x: number, z: number) => {
      for (let i = 0; i < out.length; i += 2) if (Math.hypot(out[i] * DM - x, out[i + 1] * DM - z) < 1e-4) return true;
      return false;
    };
    // Water triangles lie flat at the water level; group them by colour.
    const byColour = new Map<number, { lum: number; touchesShore: boolean; xs: number[] }>();
    for (let t = 0; t < pos.count; t += 3) {
      if (![0, 1, 2].every((k) => Math.abs(pos.getY(t + k) - Y_WATER) < 1e-6)) continue;
      const lum = col.getX(t) + col.getY(t) + col.getZ(t);
      const c = byColour.get(lum) ?? { lum, touchesShore: false, xs: [] };
      for (let k = 0; k < 3; k++) {
        c.xs.push(pos.getX(t + k));
        if (onOutline(pos.getX(t + k), pos.getZ(t + k))) c.touchesShore = true;
      }
      byColour.set(lum, c);
    }
    expect(byColour.size).toBe(2);
    const [dark, light] = [...byColour.values()].sort((p, q) => p.lum - q.lum);
    expect(dark.touchesShore, 'darker band reaches the shore').toBe(true);
    expect(light.touchesShore, 'open water stays off the shore').toBe(false);
    // The water spans the outline's extent along x.
    const xs = out.filter((_, i) => i % 2 === 0).map((v) => v * DM);
    expect(Math.min(...dark.xs)).toBeCloseTo(Math.min(...xs), 4);
    expect(Math.max(...dark.xs)).toBeCloseTo(Math.max(...xs), 4);
    expect(POND_EDGE).toBeGreaterThan(0);
    s.dispose();
  });

  it('is part of the world scene', () => {
    const w = new WorldScene(() => 0);
    const names = new Set<string>();
    w.scene.traverse((o) => names.add(o.name));
    for (const n of ['skill-tree', 'skill-oak', 'skill-eucalyptus', 'skill-stump', 'bank-booth', 'grill', 'pond']) expect(names.has(n), n).toBe(true);
  });
});

describe('skill object instancing (AC2)', () => {
  it('keeps the draw call count the same when the tree count doubles', () => {
    const doubled: SkillsData = {
      ...skills,
      trees: [...skills.trees, ...skills.trees.map((t) => ({ ...t, id: `${t.id}-b`, x: t.x + 30, z: t.z + 30 }))],
    };
    const one = new SkillObjects(skills), two = new SkillObjects(doubled);
    expect(two.trees.tree.count).toBe(2 * one.trees.tree.count);
    expect(countDrawCalls(two.group)).toBe(countDrawCalls(one.group));
    // Felling half the trees adds the stump mesh once, whatever the count.
    const half = (d: SkillsData) => new Set(d.trees.filter((_, i) => i % 2 === 0).map((t) => t.id));
    one.update(half(skills));
    two.update(half(doubled));
    expect(countDrawCalls(two.group)).toBe(countDrawCalls(one.group));
    console.log(`skill objects: ${countDrawCalls(one.group)} draw calls for ${skills.trees.length} trees, some felled`);
    one.dispose();
    two.dispose();
  });
});

describe('skill tree culling', () => {
  it('draws only the trees near the view, and no tree draw call when none are', () => {
    const s = new SkillObjects(skills);
    s.cull(overPark());
    expect(SKILL_TREE_KINDS.reduce((n, k) => n + s.trees[k].count, 0)).toBe(skills.trees.length);
    const all = countDrawCalls(s.group);
    // Looking at empty ground far from the park.
    const away = new THREE.PerspectiveCamera(50, 0.5, 0.1, 50);
    away.position.set(2000, 20, 2000);
    away.lookAt(2000, 0, 2010);
    s.cull(away);
    expect(SKILL_TREE_KINDS.reduce((n, k) => n + s.trees[k].count, 0)).toBe(0);
    expect(countDrawCalls(s.group)).toBe(all - SKILL_TREE_KINDS.length);
    // A view over one tree draws it, with its kind's mesh only.
    const t = skills.trees.find((x) => x.kind === 'oak')!;
    const near = new THREE.OrthographicCamera(-2, 2, 2, -2, 1, 100);
    near.position.set(t.x * DM, 50, t.z * DM);
    near.lookAt(t.x * DM, 0, t.z * DM);
    s.cull(near);
    expect(positions(s.trees.oak)).toContain(key(t));
    s.dispose();
  });
});

describe('felled trees (AC3)', () => {
  const felled = new Set(['tree-1', 'oak-2', 'eucalyptus-3']);

  it('draws felled trees from the world view as stumps and the rest as trees', () => {
    const w = new WorldScene(() => 0);
    w.update({ local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [], felledTrees: felled });
    const s = w.skills;
    s.cull(overPark());
    expect(positions(s.stumps)).toEqual(skills.trees.filter((t) => felled.has(t.id)).map(key).sort());
    for (const k of SKILL_TREE_KINDS) {
      expect(positions(s.trees[k]), k).toEqual(skills.trees.filter((t) => t.kind === k && !felled.has(t.id)).map(key).sort());
    }
    // Back to standing once regrown (missing set means none felled).
    w.update({ local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [] });
    s.cull(overPark());
    expect(s.stumps.count).toBe(0);
    expect(SKILL_TREE_KINDS.reduce((n, k) => n + s.trees[k].count, 0)).toBe(skills.trees.length);
  });

  it('picks up a change to the same set object', () => {
    const s = new SkillObjects(skills);
    const set = new Set<string>();
    s.update(set);
    set.add('tree-2');
    s.update(set);
    expect(positions(s.stumps)).toEqual([key(skills.trees.find((t) => t.id === 'tree-2')!)]);
    s.dispose();
  });

  it('ignores unknown ids', () => {
    const s = new SkillObjects(skills);
    s.update(new Set(['no-such-tree']));
    expect(s.stumps.count).toBe(0);
    s.dispose();
  });
});

describe('fishing spot ripples (AC1)', () => {
  const cands = skills.pond.spots;
  const at = (c: { x: number; z: number }) => `${(c.x * DM).toFixed(2)},${(c.z * DM).toFixed(2)}`;
  const view = (fishingSpots?: Set<string>) => ({ local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [], fishingSpots });

  it('draws a ripple at each listed candidate and moves them when the set changes', () => {
    const w = new WorldScene(() => 0);
    const ids = [cands[0].id, cands[3].id, cands[5].id];
    w.update(view(new Set(ids)));
    expect(positions(w.skills.ripples)).toEqual(cands.filter((c) => ids.includes(c.id)).map(at).sort());
    const next = [cands[1].id, cands[3].id];
    w.update(view(new Set(next)));
    expect(positions(w.skills.ripples)).toEqual(cands.filter((c) => next.includes(c.id)).map(at).sort());
    expect(instancePos(w.skills.ripples, 0).y).toBeGreaterThan(Y_WATER);
    w.update(view());
    expect(w.skills.ripples.count).toBe(0);
  });
});

describe('resource progress indicator (AC2)', () => {
  const view = (activeResource?: string, resourceProgress?: number) =>
    ({ local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [], activeResource, resourceProgress });
  const fillWidth = (s: SkillObjects) => (s.progress.children[1] as THREE.Mesh).scale.x;

  it('appears over a tree, a fishing spot or the grill, reflects progress and hides with no resource', () => {
    const w = new WorldScene(() => 0);
    const s = w.skills;
    expect(s.progress.visible).toBe(false);
    const tree = skills.trees.find((t) => t.kind === 'oak')!;
    w.update(view(tree.id, 0.25));
    expect(s.progress.visible).toBe(true);
    expect(s.progress.position.x).toBeCloseTo(tree.x * DM);
    expect(s.progress.position.z).toBeCloseTo(tree.z * DM);
    expect(s.progress.position.y).toBeGreaterThan(2);
    const quarter = fillWidth(s);
    w.update(view(tree.id, 0.75));
    expect(fillWidth(s)).toBeCloseTo(quarter * 3);
    const spot = skills.pond.spots[2];
    w.update(view(spot.id, 1));
    expect([s.progress.position.x, s.progress.position.z]).toEqual([spot.x * DM, spot.z * DM]);
    w.update(view('grill', 0.5));
    expect([s.progress.position.x, s.progress.position.z]).toEqual([skills.grill.x * DM, skills.grill.z * DM]);
    w.update(view(undefined, 0.5));
    expect(s.progress.visible).toBe(false);
    w.update(view('no-such-id', 0.5));
    expect(s.progress.visible).toBe(false);
  });
});
