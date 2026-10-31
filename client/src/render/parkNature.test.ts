import { execFileSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { MeshBuilder } from './campusMap';
import { PROP_BUDGET } from './modelBudget';
import { NATURE_KIT, ParkNature, buildNature, loadParkNature } from './parkNature';
import { pointInPolygon, segmentDistance2 } from './nature';
import {
  isBlocked, parseAreas, parseCampus, parseCardTables, parseCollision, parseParkNature, parseProps, type NatureItem,
} from './mapData';
import { WorldScene, countDrawCalls, countDrawCallsInView, countTrianglesInView } from './scene';

const mapDir = resolve(__dirname, '../../../shared/map');
const read = (f: string) => readFileSync(join(mapDir, f), 'utf8');
const nature = parseParkNature(read('parknature.json'));
const areas = parseAreas(read('areas.json'));
const campus = parseCampus(read('campus.json'));
const cards = parseCardTables(read('cardtables.json'));
const props = parseProps(read('props.json'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const park = areas.areas.find((a) => a.id === 'aldrich_park')!.p;
const items = nature.items.map(([t, x, z, rot, scale, stretch]) => ({ type: nature.types[t], x: x / 10, z: z / 10, rot, scale, stretch }));
const TREES = ['tree_oak', 'tree_pine', 'tree_eucalyptus'];
const ROCKS = ['rock_boulder', 'rock_flat'];

/** Prop footprint radii in metres, from shared/map/props.mjs (PROP_TYPES). */
const PROP_R: Record<string, number> = {
  bench: 0.9, lamp_post: 0.35, bin: 0.35, bike_rack: 1.1, sign: 0.4, planter: 0.6, picnic_table: 1.1, umbrella: 1.1,
  scooter: 0.6, fence: 1.0, low_wall: 1.0, lantern: 0.3, notice_board: 0.8, crate: 0.5, sack: 0.4,
};
/** Places to keep 3 m clear of: spawn and shop from areas.json, bank booth and grill (M9) from docs/specs/map.md. */
const CLEAR = {
  spawn: { x: areas.spawn.x / 10, z: areas.spawn.z / 10 },
  shop: { x: areas.shop.x / 10, z: areas.shop.z / 10 },
  'bank booth': { x: -12, z: -96 },
  grill: { x: -32, z: -96 },
};

interface Seg { x0: number; z0: number; x1: number; z1: number; h: number }
const segs: Seg[] = [];
for (const w of campus.paths) {
  for (let i = 0; i + 3 < w.p.length; i += 2) segs.push({ x0: w.p[i], z0: w.p[i + 1], x1: w.p[i + 2], z1: w.p[i + 3], h: w.w / 2 });
}
const nearSegs = (x0: number, z0: number, x1: number, z1: number): Seg[] =>
  segs.filter((s) => Math.max(s.x0, s.x1) + s.h >= x0 * 10 && Math.min(s.x0, s.x1) - s.h <= x1 * 10 &&
    Math.max(s.z0, s.z1) + s.h >= z0 * 10 && Math.min(s.z0, s.z1) - s.h <= z1 * 10);
const onPath = (list: Seg[], x: number, z: number): boolean =>
  list.some((s) => segmentDistance2(x * 10, z * 10, s.x0, s.z0, s.x1, s.z1) < s.h * s.h);
const paved = [...campus.plazas.map((q) => q.p), cards.plaza];
const onPaved = (x: number, z: number): boolean => paved.some((p) => pointInPolygon(p, x * 10, z * 10));
const inPark = (x: number, z: number): boolean => pointInPolygon(park, x * 10, z * 10);

describe('parknature.json build (AC1)', () => {
  it('is written by build.mjs byte-identically on two runs, matching the committed file; collision.bin is the same with nature off', () => {
    const a = mkdtempSync(join(tmpdir(), 'nature-a-'));
    const b = mkdtempSync(join(tmpdir(), 'nature-b-'));
    const off = mkdtempSync(join(tmpdir(), 'nature-off-'));
    for (const out of [a, b]) execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });
    execFileSync('node', [join(mapDir, 'build.mjs'), off, '--no-nature'], { encoding: 'utf8' });
    expect(readFileSync(join(a, 'parknature.json')).equals(readFileSync(join(b, 'parknature.json')))).toBe(true);
    expect(readFileSync(join(a, 'parknature.json')).equals(readFileSync(join(mapDir, 'parknature.json')))).toBe(true);
    expect(existsSync(join(off, 'parknature.json'))).toBe(false);
    // Nature is visual only: collision and the other map files don't change when it is turned off.
    for (const f of ['collision.bin', 'campus.json', 'areas.json', 'props.json']) {
      expect(readFileSync(join(off, f)).equals(readFileSync(join(a, f))), f).toBe(true);
    }
  }, 60_000);

  it('uses the seeded generator, not Math.random', () => {
    const src = read('build.mjs').replace(/\/\/.*$/gm, '');
    expect(src).not.toMatch(/Math\.random/);
    expect(src).toMatch(/rng\(NATURE_SEED\)/);
  });

  it('stores integer decimetres, known types, whole-degree rotations and percent scales', () => {
    expect(nature.units).toBe('decimetres');
    expect([...nature.types].sort()).toEqual([...NATURE_KIT].sort());
    for (const it of nature.items) {
      expect(it).toHaveLength(6);
      for (const v of it) expect(Number.isInteger(v)).toBe(true);
      expect(it[0]).toBeGreaterThanOrEqual(0);
      expect(it[0]).toBeLessThan(nature.types.length);
      expect(it[3]).toBeGreaterThanOrEqual(0);
      expect(it[3]).toBeLessThan(360);
      expect(it[4]).toBeGreaterThan(0);
      expect(it[5]).toBeGreaterThan(0);
    }
  });
});

describe('park nature variety (AC2)', () => {
  const count = (type: string) => items.filter((i) => i.type === type).length;

  it('has 3+ tree kinds, 2+ rock shapes, bushes, flower beds, 3+ picnic blankets and 3+ birds, all in the park', () => {
    const counts = Object.fromEntries(NATURE_KIT.map((t) => [t, count(t)]));
    console.log(`park nature: ${items.length} placements`, counts);
    expect(TREES.filter((t) => count(t) > 0).length).toBeGreaterThanOrEqual(3);
    expect(ROCKS.filter((t) => count(t) > 0).length).toBeGreaterThanOrEqual(2);
    expect(count('bush')).toBeGreaterThan(0);
    expect(count('flower_bed')).toBeGreaterThan(0);
    expect(count('picnic_blanket')).toBeGreaterThanOrEqual(3);
    expect(count('bird')).toBeGreaterThanOrEqual(3);
    for (const i of items) expect(inPark(i.x, i.z), `${i.type} at (${i.x}, ${i.z})`).toBe(true);
  });

  it('gives every placement a seeded scale and rotation, with 3+ distinct scales per tree kind', () => {
    for (const t of TREES) {
      const list = items.filter((i) => i.type === t);
      expect(new Set(list.map((i) => i.scale)).size, t).toBeGreaterThanOrEqual(3);
      expect(new Set(list.map((i) => i.rot)).size, t).toBeGreaterThanOrEqual(3);
    }
    for (const t of NATURE_KIT) {
      const list = items.filter((i) => i.type === t);
      if (list.length > 1) expect(new Set(list.map((i) => `${i.scale}/${i.rot}`)).size, t).toBeGreaterThan(1);
    }
  });
});

describe('park nature placement (AC3)', () => {
  it('puts a tree, rock or bush in every 20 x 20 m park block that is at least half walkable grass', () => {
    let x0 = Infinity, z0 = Infinity, x1 = -Infinity, z1 = -Infinity;
    for (let i = 0; i < park.length; i += 2) {
      x0 = Math.min(x0, park[i] / 10); x1 = Math.max(x1, park[i] / 10);
      z0 = Math.min(z0, park[i + 1] / 10); z1 = Math.max(z1, park[i + 1] / 10);
    }
    const solid = items.filter((i) => TREES.includes(i.type) || ROCKS.includes(i.type) || i.type === 'bush');
    let checked = 0;
    for (let bz = Math.floor(z0 / 20) * 20; bz < z1; bz += 20) {
      for (let bx = Math.floor(x0 / 20) * 20; bx < x1; bx += 20) {
        const near = nearSegs(bx, bz, bx + 20, bz + 20);
        let grass = 0;
        for (let i = 0; i < 20; i++) {
          for (let j = 0; j < 20; j++) {
            const x = bx + i + 0.5, z = bz + j + 0.5;
            if (inPark(x, z) && !isBlocked(grid, x, z) && !onPath(near, x, z) && !onPaved(x, z)) grass++;
          }
        }
        if (grass < 200) continue;
        checked++;
        const has = solid.some((i) => i.x >= bx && i.x < bx + 20 && i.z >= bz && i.z < bz + 20);
        expect(has, `block at (${bx}, ${bz}), ${grass} m² grass`).toBe(true);
      }
    }
    console.log(`park nature: ${checked} blocks at least half grass, all covered`);
    expect(checked).toBeGreaterThan(50);
  });

  it('keeps every placement off paths, blocked cells, plazas, the card table plaza and props', () => {
    const placedProps = props.props.map(([t, x, z]) => ({ type: props.types[t], x: x / 10, z: z / 10 }));
    for (const i of items) {
      const at = `${i.type} at (${i.x}, ${i.z})`;
      expect(onPath(nearSegs(i.x, i.z, i.x, i.z), i.x, i.z), at).toBe(false);
      expect(isBlocked(grid, i.x, i.z), at).toBe(false);
      expect(pointInPolygon(cards.plaza, i.x * 10, i.z * 10), at).toBe(false);
      expect(onPaved(i.x, i.z), at).toBe(false);
      for (const p of placedProps) {
        expect(Math.hypot(i.x - p.x, i.z - p.z) >= PROP_R[p.type], `${at} on ${p.type} at (${p.x}, ${p.z})`).toBe(true);
      }
    }
  });

  it('keeps 3 m clear of the spawn point, shop stall, bank booth and grill', () => {
    for (const i of items) {
      for (const [name, c] of Object.entries(CLEAR)) {
        expect(Math.hypot(i.x - c.x, i.z - c.z), `${i.type} at (${i.x}, ${i.z}) near the ${name}`).toBeGreaterThanOrEqual(3);
      }
    }
  });
});

describe('park nature models (AC4)', () => {
  it('keeps every model within PROP_BUDGET triangles', () => {
    for (const t of NATURE_KIT) {
      const b = new MeshBuilder();
      buildNature(b, t);
      console.log(`nature ${t}: ${b.triangles} triangles`);
      expect(b.triangles, t).toBeGreaterThan(0);
      expect(b.triangles, t).toBeLessThanOrEqual(PROP_BUDGET.maxTriangles);
    }
  });

  it('sets castShadow on trees and rocks, and draws flat colours without textures', () => {
    const n = new ParkNature(loadParkNature());
    for (const t of [...TREES, ...ROCKS]) {
      const mesh = n.meshes.get(t);
      expect(mesh, t).toBeDefined();
      expect(mesh!.castShadow, t).toBe(true);
    }
    expect(n.material.vertexColors).toBe(true);
    expect(n.material.map).toBeNull();
    for (const mesh of n.meshes.values()) expect(mesh.geometry.getAttribute('uv')).toBeUndefined();
    n.dispose();
  });
});

describe('park nature draw calls (AC5)', () => {
  /** The same placements twice over, the copies nudged 0.5 m east. */
  const doubled = { ...nature, items: [...nature.items, ...nature.items.map(([t, x, z, r, s, h]) => [t, x + 5, z, r, s, h] as NatureItem)] };

  it('adds at most one draw call per model type, with the placements doubled as well', () => {
    const one = new ParkNature(nature);
    const two = new ParkNature(doubled);
    const a = countDrawCalls(one.group), b = countDrawCalls(two.group);
    console.log(`park nature: ${nature.items.length} placements -> ${a} draw calls, ${doubled.items.length} -> ${b}`);
    expect(a).toBeGreaterThan(0);
    expect(a).toBeLessThanOrEqual(nature.types.length);
    expect(b).toBe(a);
    // Every placement is drawn: doubling doubles the instances of each type.
    for (const [t, mesh] of one.meshes) expect(two.meshes.get(t)!.count, t).toBe(2 * mesh.count);
    expect([...one.meshes.values()].reduce((s, m) => s + m.count, 0)).toBe(nature.items.length);
    one.dispose();
    two.dispose();
  });

  it('report: draw calls and triangles in view at the spawn with 100 players', () => {
    const w = new WorldScene();
    const sx = areas.spawn.x / 10, sz = areas.spawn.z / 10;
    const remotes = Array.from({ length: 100 }, (_, i) => ({ id: i + 1, x: sx + (i % 10) - 5, z: sz + Math.floor(i / 10) - 5, heading: 0 }));
    w.update({ local: { id: 0, x: sx, z: sz, heading: 0 }, remotes });
    w.rig.camera.aspect = 9 / 19.5;
    w.rig.camera.updateProjectionMatrix();
    const calls = countDrawCallsInView(w.scene, w.rig.camera);
    const natureCalls = countDrawCallsInView(w.parkNature.group, w.rig.camera);
    const tris = countTrianglesInView(w.scene, w.rig.camera);
    console.log(`spawn, 100 players: ${calls} draw calls in view (${natureCalls} for park nature), ${tris} triangles`);
    expect(natureCalls).toBeGreaterThan(0);
    expect(natureCalls).toBeLessThanOrEqual(nature.types.length);
    expect(calls).toBeLessThan(150);
    expect(w.parkNature.group.parent).toBe(w.scene);
    expect(w.scene.getObjectByName('park-nature')).toBeInstanceOf(THREE.Group);
  });
});
