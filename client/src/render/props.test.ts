import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { MeshBuilder } from './campusMap';
import { stallGeometry } from './gameAreas';
import { PROP_BUDGET } from './modelBudget';
import { PROP_KIT, Props, buildProp, loadProps } from './propKit';
import { isBlocked, parseAreas, parseCampus, parseCardTables, parseCollision, parseProps, type Prop } from './mapData';
import { WorldScene, countDrawCalls, countDrawCallsInView, countTrianglesInView } from './scene';

const mapDir = resolve(__dirname, '../../../shared/map');
const read = (f: string) => readFileSync(join(mapDir, f), 'utf8');
const props = parseProps(read('props.json'));
const areas = parseAreas(read('areas.json'));
const campus = parseCampus(read('campus.json'));
const cards = parseCardTables(read('cardtables.json'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const placed = props.props.map(([t, x, z]) => ({ type: props.types[t], x: x / 10, z: z / 10 }));

/** Point (metres) inside polygon [x0,z0,...] in decimetres. */
function inside(x: number, z: number, p: number[]): boolean {
  let r = false;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    const xi = p[i] / 10, zi = p[i + 1] / 10, xj = p[j] / 10, zj = p[j + 1] / 10;
    if (zi > z !== zj > z && x < xi + ((z - zi) * (xj - xi)) / (zj - zi)) r = !r;
  }
  return r;
}

describe('props.json build (AC1)', () => {
  it('is written by build.mjs byte-identically on two runs, matching the committed file, and leaves collision.bin unchanged', () => {
    const a = mkdtempSync(join(tmpdir(), 'props-a-'));
    const b = mkdtempSync(join(tmpdir(), 'props-b-'));
    for (const out of [a, b]) execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });
    for (const f of ['props.json', 'collision.bin']) {
      expect(readFileSync(join(a, f)).equals(readFileSync(join(b, f))), f).toBe(true);
      expect(readFileSync(join(a, f)).equals(readFileSync(join(mapDir, f))), f).toBe(true);
    }
  });

  it('uses the seeded generator, not Math.random', () => {
    const src = read('build.mjs').replace(/\/\/.*$/gm, '');
    expect(src).not.toMatch(/Math\.random/);
    expect(src).toMatch(/rng\(PROP_SEED\)/);
  });

  it('stores integer decimetres, known types and whole-degree rotations', () => {
    expect(props.units).toBe('decimetres');
    for (const p of props.props) {
      expect(p).toHaveLength(4);
      for (const v of p) expect(Number.isInteger(v)).toBe(true);
      expect(p[0]).toBeGreaterThanOrEqual(0);
      expect(p[0]).toBeLessThan(props.types.length);
      expect(p[3]).toBeGreaterThanOrEqual(0);
      expect(p[3]).toBeLessThan(360);
    }
  });
});

describe('prop density in busy areas (AC2)', () => {
  const plazaCentre = (() => {
    const n = cards.plaza.length / 2;
    let x = 0, z = 0;
    for (let i = 0; i < n; i++) { x += cards.plaza[2 * i] / 10; z += cards.plaza[2 * i + 1] / 10; }
    return { x: x / n, z: z / n };
  })();
  const centres = {
    spawn: { x: areas.spawn.x / 10, z: areas.spawn.z / 10 },
    shop: { x: areas.shop.x / 10, z: areas.shop.z / 10 },
    'card plaza': plazaCentre,
  };

  for (const [name, c] of Object.entries(centres)) {
    it(`every at-least-half-walkable 10 x 10 m cell around the ${name} holds 5+ prop types`, () => {
      let checked = 0;
      for (let dz = -1; dz <= 1; dz++) {
        for (let dx = -1; dx <= 1; dx++) {
          const x0 = c.x - 5 + dx * 10, z0 = c.z - 5 + dz * 10;
          let walkable = 0;
          for (let i = 0; i < 10; i++) for (let j = 0; j < 10; j++) if (!isBlocked(grid, x0 + i + 0.5, z0 + j + 0.5)) walkable++;
          if (walkable < 50) continue;
          checked++;
          const types = new Set(placed.filter((p) => p.x >= x0 && p.x < x0 + 10 && p.z >= z0 && p.z < z0 + 10).map((p) => p.type));
          expect(types.size, `cell at (${x0}, ${z0})`).toBeGreaterThanOrEqual(5);
        }
      }
      expect(checked).toBeGreaterThan(0);
    });
  }

  it('puts at least one prop in every gameplay area in areas.json', () => {
    for (const a of areas.areas) expect(placed.some((p) => inside(p.x, p.z, a.p)), a.id).toBe(true);
  });
});

describe('prop placement (AC3)', () => {
  it('never puts a prop centre on a blocked cell or inside a building footprint', () => {
    for (const p of placed) {
      expect(isBlocked(grid, p.x, p.z), `${p.type} at (${p.x}, ${p.z})`).toBe(false);
      for (const b of campus.buildings) expect(inside(p.x, p.z, b.p), `${p.type} in building ${b.id}`).toBe(false);
    }
  });
});

describe('prop kit (AC4)', () => {
  const outdoor = ['bench', 'lamp_post', 'bin', 'bike_rack', 'sign', 'planter', 'picnic_table', 'umbrella', 'scooter', 'fence', 'low_wall'];

  it('has the 11 outdoor types plus lanterns and a notice board, all placed by props.json', () => {
    for (const t of [...outdoor, 'lantern', 'notice_board']) expect(PROP_KIT).toContain(t);
    expect(PROP_KIT.length).toBeGreaterThanOrEqual(13);
    expect([...props.types].sort()).toEqual([...PROP_KIT].sort());
    for (const t of [...outdoor, 'lantern', 'notice_board']) expect(placed.some((p) => p.type === t), t).toBe(true);
  });

  it('keeps every prop type within PROP_BUDGET triangles', () => {
    for (const t of PROP_KIT) {
      const b = new MeshBuilder();
      buildProp(b, t);
      console.log(`prop ${t}: ${b.triangles} triangles`);
      expect(b.triangles, t).toBeGreaterThan(0);
      expect(b.triangles, t).toBeLessThanOrEqual(PROP_BUDGET.maxTriangles);
    }
  });

  it('dresses the shop stall with crates, sacks, fruit and a striped awning within PROP_BUDGET', () => {
    const g = stallGeometry();
    const tris = g.getAttribute('position').count / 3;
    console.log(`dressed shop stall: ${tris} triangles`);
    expect(tris).toBeLessThanOrEqual(PROP_BUDGET.maxTriangles);
    // The parts show up as their colours: red and cream awning stripes, crate wood, burlap, fruit.
    const colours = new Set<string>();
    const c = g.getAttribute('color');
    const col = new THREE.Color();
    for (let i = 0; i < c.count; i++) colours.add(col.setRGB(c.getX(i), c.getY(i), c.getZ(i)).getHexString());
    for (const hex of ['c8322a', 'f2e6c8', 'b07a48', 'c8a876', 'f09a28', 'd83a2a', 'e8d040']) expect(colours, hex).toContain(hex);
  });

  it('draws flat colours without textures', () => {
    const p = new Props(loadProps());
    expect(p.material.vertexColors).toBe(true);
    expect(p.material.map).toBeNull();
    for (const m of p.group.children) expect((m as THREE.Mesh).geometry.getAttribute('uv')).toBeUndefined();
    p.dispose();
  });
});

describe('prop draw calls (AC5)', () => {
  /** The same props twice over, the copies nudged 0.5 m east. */
  const doubled = { ...props, props: [...props.props, ...props.props.map(([t, x, z, r]) => [t, x + 5, z, r] as Prop)] };

  it('adds at most one draw call per prop type, with the placements doubled as well', () => {
    const one = new Props(props);
    const two = new Props(doubled);
    const a = countDrawCalls(one.group), b = countDrawCalls(two.group);
    console.log(`props: ${props.props.length} placements -> ${a} draw calls, ${doubled.props.length} -> ${b}`);
    expect(a).toBeGreaterThan(0);
    expect(a).toBeLessThanOrEqual(props.types.length);
    expect(b).toBeLessThanOrEqual(props.types.length);
    expect(b).toBe(a);
    // Doubling doubles the drawn triangles: every placement is drawn.
    const tris = (p: Props) => p.group.children.reduce((s, m) => s + (m as THREE.Mesh).geometry.getAttribute('position').count / 3, 0);
    expect(tris(two)).toBe(2 * tris(one));
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
    const propCalls = countDrawCallsInView(w.props.group, w.rig.camera);
    const tris = countTrianglesInView(w.scene, w.rig.camera);
    console.log(`spawn, 100 players: ${calls} draw calls in view (${propCalls} for props), ${tris} triangles`);
    expect(propCalls).toBeGreaterThan(0);
    expect(propCalls).toBeLessThanOrEqual(props.types.length);
    expect(calls).toBeLessThan(150);
  });
});
