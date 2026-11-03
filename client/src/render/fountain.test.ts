import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { FOUNTAIN_EDGE, FOUNTAIN_WATER_Y, Fountain, loadFountain } from './fountain';
import { isBlocked, parseAreas, parseCampus, parseCardTables, parseCollision, parseFountain, parseSkills, type CollisionGrid } from './mapData';
import { PROP_BUDGET } from './modelBudget';
import { pointInPolygon, segmentDistance2 } from './nature';
import { WorldScene, countDrawCalls } from './scene';

const mapDir = resolve(__dirname, '../../../shared/map');
const read = (f: string) => readFileSync(join(mapDir, f), 'utf8');
const sha256 = (buf: Uint8Array) => createHash('sha256').update(buf).digest('hex');
const fountain = parseFountain(read('fountain.json'));
const campus = parseCampus(read('campus.json'));
const areas = parseAreas(read('areas.json'));
const cards = parseCardTables(read('cardtables.json'));
const skills = parseSkills(read('skills.json'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const m = (v: number) => v / 10;
const fx = m(fountain.x), fz = m(fountain.z), fr = m(fountain.r);
const spawn = { x: m(areas.spawn.x), z: m(areas.spawn.z) };

/** Metres from (x, z) to a closed polygon in decimetres (0 inside). */
function polyDist(p: number[], x: number, z: number): number {
  if (pointInPolygon(p, x * 10, z * 10)) return 0;
  let best = Infinity;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    best = Math.min(best, Math.sqrt(segmentDistance2(x * 10, z * 10, p[j], p[j + 1], p[i], p[i + 1])) / 10);
  }
  return best;
}
const cellOf = (g: CollisionGrid, x: number, z: number) =>
  [Math.floor((x - g.originX) / g.cellSize), Math.floor((z - g.originZ) / g.cellSize)];
const centreOf = (g: CollisionGrid, col: number, row: number) =>
  [g.originX + (col + 0.5) * g.cellSize, g.originZ + (row + 0.5) * g.cellSize];

/** Walkable cells reachable from the spawn, moving between edge neighbours only. */
function reachable(g: CollisionGrid): Uint8Array {
  const seen = new Uint8Array(g.width * g.height);
  const blocked = (i: number) => ((g.bits[i >> 3] >> (i & 7)) & 1) === 1;
  const [c0, r0] = cellOf(g, spawn.x, spawn.z);
  const queue = new Int32Array(g.width * g.height);
  let head = 0, tail = 0;
  queue[tail++] = r0 * g.width + c0;
  seen[r0 * g.width + c0] = 1;
  while (head < tail) {
    const i = queue[head++];
    const c = i % g.width, r = (i - c) / g.width;
    for (const [dc, dr] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
      const cc = c + dc, rr = r + dr;
      if (cc < 0 || rr < 0 || cc >= g.width || rr >= g.height) continue;
      const j = rr * g.width + cc;
      if (seen[j] || blocked(j)) continue;
      seen[j] = 1;
      queue[tail++] = j;
    }
  }
  return seen;
}

describe('fountain map data (AC1)', () => {
  it('build.mjs writes fountain.json and collision.bin byte-identically on two builds, matching the committed files', () => {
    const a = mkdtempSync(join(tmpdir(), 'fountain-a-')), b = mkdtempSync(join(tmpdir(), 'fountain-b-'));
    for (const out of [a, b]) execFileSync('node', [join(mapDir, 'build.mjs'), out, '--no-nature'], { encoding: 'utf8' });
    for (const f of ['fountain.json', 'collision.bin']) {
      const x = readFileSync(join(a, f)), y = readFileSync(join(b, f));
      expect(x.equals(y), f).toBe(true);
      expect(x.equals(readFileSync(join(mapDir, f))), `${f} matches the committed file`).toBe(true);
    }
  });

  it('puts one 4 m fountain in the paved plaza nearest the spawn, clear of paths and the safe-circle places', () => {
    expect(fountain.r).toBe(20);
    const nearest = campus.plazas.slice().sort((p, q) => polyDist(p.p, spawn.x, spawn.z) - polyDist(q.p, spawn.x, spawn.z) || p.id - q.id)[0];
    expect(fountain.plaza).toBe(nearest.id);
    const plaza = nearest.p;
    for (let k = 0; k < 16; k++) {
      const a = (k / 16) * Math.PI * 2;
      expect(pointInPolygon(plaza, (fx + Math.cos(a) * fr) * 10, (fz + Math.sin(a) * fr) * 10), 'basin inside the plaza').toBe(true);
    }
    for (const w of campus.paths) {
      for (let i = 0; i + 3 < w.p.length; i += 2) {
        const d = Math.sqrt(segmentDistance2(fx * 10, fz * 10, w.p[i], w.p[i + 1], w.p[i + 2], w.p[i + 3])) / 10;
        expect(d - m(w.w) / 2, `path ${w.id}`).toBeGreaterThanOrEqual(fr);
      }
    }
    for (const p of [areas.shop, skills.bank, skills.grill, areas.spawn]) expect(Math.hypot(m(p.x) - fx, m(p.z) - fz)).toBeGreaterThan(fr + 5);
    expect(polyDist(cards.plaza, fx, fz)).toBeGreaterThan(fr + 5);
  });

  it('blocks the basin cells, keeps the cells round it walkable, and changes nothing else in collision.bin', () => {
    let inside = 0;
    for (let row = 0; row < grid.height; row++) {
      for (let col = 0; col < grid.width; col++) {
        const [x, z] = centreOf(grid, col, row);
        const d = Math.hypot(x - fx, z - fz);
        if (d <= fr) {
          expect(isBlocked(grid, x, z), `basin cell (${x}, ${z})`).toBe(true);
          inside++;
        } else if (d <= fr + 2.5) {
          expect(isBlocked(grid, x, z), `cell round the basin (${x}, ${z})`).toBe(false);
        }
      }
    }
    expect(inside).toBeGreaterThanOrEqual(12);
    // With the basin cells cleared, the grid is the one committed before the fountain (with the #403 café opened).
    const raw = new Uint8Array(readFileSync(join(mapDir, 'collision.bin')));
    const bits = raw.subarray(22);
    for (let row = 0; row < grid.height; row++) {
      for (let col = 0; col < grid.width; col++) {
        const [x, z] = centreOf(grid, col, row);
        if (Math.hypot(x - fx, z - fz) > fr) continue;
        const i = row * grid.width + col;
        bits[i >> 3] &= ~(1 << (i & 7));
      }
    }
    expect(sha256(raw)).toBe('d200274d182381d7f1d7b66e72478a8fd7812c18b90cb062ea39c235f351b07b');
  });

  it('still lets a player walk from the spawn to the shop, the card tables, the fountain and every gameplay area', () => {
    const seen = reachable(grid);
    const reached = (x: number, z: number) => {
      const [c, r] = cellOf(grid, x, z);
      return seen[r * grid.width + c] === 1;
    };
    const near = (x: number, z: number) => [[0, 0], [1, 0], [-1, 0], [0, 1], [0, -1]].some(([dx, dz]) => reached(x + dx, z + dz));
    expect(reached(m(areas.shop.x), m(areas.shop.z)), 'shop').toBe(true);
    for (const t of cards.tables) expect(reached(m(t.x), m(t.z)), `card table ${t.id}`).toBe(true);
    expect(reached(m(skills.bank.x), m(skills.bank.z)), 'bank booth').toBe(true);
    expect(reached(m(skills.grill.x), m(skills.grill.z)), 'grill').toBe(true);
    for (const t of skills.trees) expect(near(m(t.x), m(t.z)), t.id).toBe(true);
    for (const s of skills.pond.spots) expect(near(m(s.x), m(s.z)), s.id).toBe(true);
    for (let k = 0; k < 16; k++) {
      const a = (k / 16) * Math.PI * 2;
      expect(reached(fx + Math.cos(a) * (fr + 1), fz + Math.sin(a) * (fr + 1)), 'all round the fountain').toBe(true);
    }
    // Most of every area's open ground is reachable.
    for (const area of areas.areas) {
      let open = 0, got = 0;
      let x0 = Infinity, z0 = Infinity, x1 = -Infinity, z1 = -Infinity;
      for (let i = 0; i < area.p.length; i += 2) {
        x0 = Math.min(x0, m(area.p[i])); x1 = Math.max(x1, m(area.p[i])); z0 = Math.min(z0, m(area.p[i + 1])); z1 = Math.max(z1, m(area.p[i + 1]));
      }
      for (let z = Math.floor(z0) + 0.5; z < z1; z += 2) {
        for (let x = Math.floor(x0) + 0.5; x < x1; x += 2) {
          if (!pointInPolygon(area.p, x * 10, z * 10) || isBlocked(grid, x, z)) continue;
          open++;
          if (reached(x, z)) got++;
        }
      }
      expect(open, area.id).toBeGreaterThan(0);
      expect(got / open, area.id).toBeGreaterThan(0.9);
    }
  });
});

describe('fountain rendering (AC3)', () => {
  const f = new Fountain(loadFountain());

  it('draws the water with its own material and a time-driven ripple in its shaders', () => {
    const mat = f.water.material as THREE.MeshPhongMaterial;
    expect(mat).not.toBe(f.basin.material);
    for (const s of f.stones) expect(mat).not.toBe(s.material);
    expect(mat.name).toBe('fountain-water');
    const shader = {
      uniforms: {} as Record<string, THREE.IUniform>,
      vertexShader: THREE.ShaderLib.phong.vertexShader,
      fragmentShader: THREE.ShaderLib.phong.fragmentShader,
    };
    mat.onBeforeCompile(shader as unknown as THREE.WebGLProgramParametersWithUniforms, {} as THREE.WebGLRenderer);
    expect(shader.vertexShader).toContain('uniform float uTime;');
    expect(shader.vertexShader).toMatch(/transformed\.y \+= .*uTime/);
    expect(shader.fragmentShader).toContain('uniform float uTime;');
    expect(shader.fragmentShader).toMatch(/diffuseColor\.rgb \*= .*ripple/);
    expect(mat.customProgramCacheKey()).toBe('fountain-water');
    f.update(0);
    expect(shader.uniforms.uTime.value).toBe(0);
    f.update(12.5);
    expect(shader.uniforms.uTime.value).toBe(12.5);
  });

  it('has a darker band along the water edge', () => {
    const g = f.water.geometry;
    const pos = g.attributes.position, col = g.attributes.color;
    const waterR = Math.max(...Array.from({ length: pos.count }, (_, i) => Math.hypot(pos.getX(i), pos.getZ(i))));
    let centre = 0, edge = 0, nc = 0, ne = 0;
    for (let i = 0; i < pos.count; i += 3) {
      // Classify each triangle by its centroid's distance from the middle.
      let cx = 0, cz = 0, lum = 0;
      for (let k = i; k < i + 3; k++) {
        cx += pos.getX(k) / 3; cz += pos.getZ(k) / 3;
        lum += (col.getX(k) + col.getY(k) + col.getZ(k)) / 9;
        expect(pos.getY(k)).toBeCloseTo(FOUNTAIN_WATER_Y, 5);
      }
      const d = Math.hypot(cx, cz) / waterR;
      if (d > 1 - FOUNTAIN_EDGE) { edge += lum; ne++; } else { centre += lum; nc++; }
    }
    expect(ne).toBeGreaterThan(0);
    expect(nc).toBeGreaterThan(0);
    expect(edge / ne).toBeLessThan((centre / nc) * 0.8);
  });

  it('rings the basin with at least 8 stones of 2 shapes, one draw call per shape', () => {
    expect(f.stones).toHaveLength(2);
    const [a, b] = f.stones;
    expect(a.isInstancedMesh && b.isInstancedMesh).toBe(true);
    expect(a.geometry).not.toBe(b.geometry);
    // Different shapes: a rounded boulder and a much flatter slab.
    const flatness = (g: THREE.BufferGeometry) => {
      g.computeBoundingBox();
      const size = g.boundingBox!.getSize(new THREE.Vector3());
      return size.y / Math.max(size.x, size.z);
    };
    expect(flatness(b.geometry)).toBeLessThan(flatness(a.geometry) * 0.5);
    expect(a.count + b.count).toBeGreaterThanOrEqual(8);
    expect(Math.min(a.count, b.count)).toBeGreaterThanOrEqual(1);
    const m4 = new THREE.Matrix4(), p = new THREE.Vector3();
    for (const s of f.stones) {
      for (let i = 0; i < s.count; i++) {
        s.getMatrixAt(i, m4);
        p.setFromMatrixPosition(m4);
        const d = Math.hypot(p.x, p.z);
        expect(d).toBeGreaterThan(fr);
        expect(d).toBeLessThan(fr + 1);
      }
    }
    // Basin, water and one per stone shape.
    expect(countDrawCalls(f.group)).toBe(4);
    let stoneCalls = 0;
    f.group.traverse((o) => { if (o.name.startsWith('fountain-') && o !== f.basin && o !== f.water) stoneCalls++; });
    expect(stoneCalls).toBe(2);
  });
});

describe('fountain budget and shadows (AC4)', () => {
  it('stays within PROP_BUDGET triangles without the stones, and casts and receives shadows', () => {
    const f = new Fountain(loadFountain());
    console.log(`fountain: ${f.triangles} triangles without stones, ${countDrawCalls(f.group)} draw calls`);
    expect(f.triangles).toBeLessThanOrEqual(PROP_BUDGET.maxTriangles);
    expect(f.basin.castShadow && f.basin.receiveShadow).toBe(true);
    expect(f.water.receiveShadow).toBe(true);
    for (const s of f.stones) expect(s.castShadow && s.receiveShadow).toBe(true);
  });

  it('is in the world scene at its fountain.json position and follows the render clock', () => {
    let now = 0;
    const w = new WorldScene(() => now);
    expect(w.fountain.group.parent).toBe(w.scene);
    expect(w.fountain.group.position.x).toBeCloseTo(fx, 5);
    expect(w.fountain.group.position.z).toBeCloseTo(fz, 5);
    const uTime = (w.fountain.water.material as THREE.Material).userData.uTime as { value: number };
    now = 4000;
    w.update({ local: { id: 1, x: 0, z: 0, heading: 0 }, remotes: [] } as unknown as Parameters<WorldScene['update']>[0]);
    expect(uTime.value).toBeCloseTo(4, 5);
  });
});
