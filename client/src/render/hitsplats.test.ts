import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { WorldScene, countDrawCalls } from './scene';
import {
  HIT_COLOR,
  HITSPLAT_MS,
  MAX_HITSPLATS,
  MISS_COLOR,
  digitSegments,
} from './hitsplats';
import type { HitView, PlayerView, WorldView } from './worldView';

function view(hits?: HitView[], dx = 0): WorldView {
  const remotes: PlayerView[] = [
    { id: 7, x: 5 + dx, z: 3, heading: 0 },
    { id: 8, x: -4, z: 2, heading: 0 },
  ];
  return { local: { id: 1, x: dx, z: 0, heading: 0 }, remotes, ...(hits ? { hits } : {}) };
}

const hit = (target: number, damage: number, died = false): HitView => ({ target, damage, died });

function clocked() {
  let t = 1000;
  const w = new WorldScene(() => t);
  return { w, advance: (ms: number) => (t += ms) };
}

function graph(root: THREE.Object3D): string[] {
  const out: string[] = [];
  root.traverse((o) => out.push(`${o.type}:${o.name}:${o.children.length}`));
  return out;
}

function discColor(w: WorldScene, i: number): number {
  const discs = w.scene.getObjectByName('hitsplat-discs') as THREE.InstancedMesh;
  const c = new THREE.Color();
  discs.getColorAt(i, c);
  return c.getHex();
}

function materialsAndTextures(root: THREE.Object3D): Set<string> {
  const ids = new Set<string>();
  root.traverse((o) => {
    const mats = (o as THREE.Mesh).material;
    if (!mats) return;
    for (const m of Array.isArray(mats) ? mats : [mats]) {
      ids.add(m.uuid);
      for (const v of Object.values(m)) if ((v as THREE.Texture)?.isTexture) ids.add((v as THREE.Texture).uuid);
    }
  });
  return ids;
}

describe('hitsplats (AC1)', () => {
  it('leaves the scene graph unchanged when the view has no hits field', () => {
    const a = new WorldScene(() => 0);
    const b = new WorldScene(() => 0);
    a.update(view());
    a.update(view(undefined, 0.5));
    b.update(view([]));
    b.update(view([], 0.5));
    expect(graph(a.scene)).toEqual(graph(new WorldScene(() => 0).scene));
    expect(graph(b.scene)).toEqual(graph(a.scene));
    expect(a.scene.getObjectByName('hitsplats')).toBeUndefined();
  });
});

describe('hitsplats (AC2)', () => {
  it('shows damage above the local player and a remote, a miss style for 0, and ignores unknown ids', () => {
    const { w } = clocked();
    w.update(view([hit(1, 12), hit(7, 0), hit(999, 5)]));
    const s = w.hitsplats.active;
    expect(s.map((x) => [x.target, x.damage, x.miss])).toEqual([
      [1, 12, false],
      [7, 0, true],
    ]);
    // Above the entity.
    expect(s[0].position.x).toBe(0);
    expect(s[0].position.z).toBe(0);
    expect(s[0].position.y).toBeGreaterThan(1.8);
    expect(s[1].position.x).toBe(5);
    expect(s[1].position.z).toBe(3);
    // Drawn: one disc each, digit segments for "12" and "0".
    const discs = w.scene.getObjectByName('hitsplat-discs') as THREE.InstancedMesh;
    const segs = w.scene.getObjectByName('hitsplat-digits') as THREE.InstancedMesh;
    expect(discs.count).toBe(2);
    const lit = (d: number) => digitSegments(d).flat().length;
    expect(segs.count).toBe(lit(12) + lit(0));
    expect(digitSegments(12)).toHaveLength(2);
    expect(discColor(w, 0)).toBe(HIT_COLOR);
    expect(discColor(w, 1)).toBe(MISS_COLOR);
    const disc0 = new THREE.Matrix4();
    discs.getMatrixAt(0, disc0);
    expect(new THREE.Vector3().setFromMatrixPosition(disc0).distanceTo(s[0].position)).toBeLessThan(1e-5);
  });

  it('ignores a batch whose every target is outside the view', () => {
    const { w } = clocked();
    w.update(view([hit(500, 3)]));
    expect(w.hitsplats.active).toHaveLength(0);
    expect(countDrawCalls(w.hitsplats.group)).toBe(0);
  });
});

describe('hitsplats (AC3)', () => {
  it('follows its entity and is removed after HITSPLAT_MS', () => {
    const { w, advance } = clocked();
    w.update(view([hit(7, 4)]));
    expect(w.hitsplats.active[0].position.x).toBe(5);
    advance(HITSPLAT_MS / 2);
    w.update(view(undefined, 2));
    expect(w.hitsplats.active).toHaveLength(1);
    expect(w.hitsplats.active[0].position.x).toBe(7);
    advance(HITSPLAT_MS / 2 - 1);
    w.update(view([], 2));
    expect(w.hitsplats.active).toHaveLength(1);
    advance(1);
    w.update(view([], 2));
    expect(w.hitsplats.active).toHaveLength(0);
    expect((w.scene.getObjectByName('hitsplat-discs') as THREE.InstancedMesh).count).toBe(0);
  });

  it('shows several hits on one entity in one batch, stacked', () => {
    const { w } = clocked();
    w.update(view([hit(8, 1), hit(8, 2), hit(8, 0)]));
    const s = w.hitsplats.active;
    expect(s.map((x) => x.damage)).toEqual([1, 2, 0]);
    expect(new Set(s.map((x) => x.position.y)).size).toBe(3);
    expect(s.every((x) => x.position.x === -4 && x.position.z === 2)).toBe(true);
  });

  it('never has more than MAX_HITSPLATS at once, dropping the oldest', () => {
    const { w, advance } = clocked();
    for (let i = 0; i < MAX_HITSPLATS + 10; i++) {
      w.update(view([hit(1, i + 1)]));
      advance(1);
    }
    expect(w.hitsplats.active).toHaveLength(MAX_HITSPLATS);
    expect(w.hitsplats.active[0].damage).toBe(11);
    expect((w.scene.getObjectByName('hitsplat-discs') as THREE.InstancedMesh).count).toBe(MAX_HITSPLATS);
  });
});

describe('hitsplats (AC4)', () => {
  it('reuses shared materials and meshes: 50 hits create nothing new after the first', () => {
    const { w, advance } = clocked();
    w.update(view([hit(1, 3)]));
    const mats = materialsAndTextures(w.scene);
    const shape = graph(w.scene);
    const calls = countDrawCalls(w.scene);
    for (let i = 0; i < 50; i++) {
      w.update(view([hit(i % 2 ? 7 : 8, i * 37)]));
      advance(10);
    }
    expect(w.hitsplats.active.length).toBeGreaterThan(1);
    expect(materialsAndTextures(w.scene)).toEqual(mats);
    expect(graph(w.scene)).toEqual(shape);
    expect(countDrawCalls(w.scene)).toBe(calls);
  });
});
