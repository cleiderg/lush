import { execFileSync } from 'node:child_process';
import { copyFileSync, cpSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { CampusMap, loadCampus } from './campusMap';
import { CardTables, loadCardTables } from './cardTables';
import { isBlocked, parseAreas, parseCardTables, parseCollision } from './mapData';
import { WorldScene, countDrawCalls } from './scene';

const mapDir = resolve(__dirname, '../../../shared/map');
const file = parseCardTables(readFileSync(join(mapDir, 'cardtables.json'), 'utf8'));
const areas = parseAreas(readFileSync(join(mapDir, 'areas.json'), 'utf8'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const plaza = Array.from({ length: file.plaza.length / 2 }, (_, i) => [file.plaza[2 * i] / 10, file.plaza[2 * i + 1] / 10]);
const tables = file.tables.map((t) => ({ id: t.id, x: t.x / 10, z: t.z / 10 }));
const spawn = { x: areas.spawn.x / 10, z: areas.spawn.z / 10 };
const safe = areas.safeRadius / 10;

function inside(px: number, pz: number, poly: number[][]): boolean {
  let r = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [xi, zi] = poly[i], [xj, zj] = poly[j];
    if (zi > pz !== zj > pz && px < xi + ((pz - zi) * (xj - xi)) / (zj - zi)) r = !r;
  }
  return r;
}
const fromSpawn = (x: number, z: number) => Math.hypot(x - spawn.x, z - spawn.z);
/** Metres -> the centre of its collision cell, in decimetres. */
const cellCentreDm = (v: number, origin: number) => Math.round((origin + (Math.floor((v - origin) / grid.cellSize) + 0.5) * grid.cellSize) * 10);

function build(dir: string): void {
  execFileSync('node', [join(dir, 'build.mjs'), dir], { encoding: 'utf8', stdio: 'pipe' });
}
/** A copy of the map build inputs in a temp dir, so a test can change areas.mjs. */
function buildInputs(): string {
  const dir = mkdtempSync(join(tmpdir(), 'cardtables-src-'));
  for (const f of ['build.mjs', 'config.mjs', 'areas.mjs', 'props.mjs', 'parknature.mjs', 'skills.mjs', 'fountain.mjs']) copyFileSync(join(mapDir, f), join(dir, f));
  cpSync(join(mapDir, 'source'), join(dir, 'source'), { recursive: true });
  return dir;
}

describe('cardtables.json format and build (AC1)', () => {
  it('holds a plaza polygon and 4 tables with ids 1 to 4 in integer decimetres', () => {
    expect(file.units).toBe('decimetres');
    expect(file.plaza.length % 2).toBe(0);
    expect(file.plaza.length).toBeGreaterThanOrEqual(6);
    expect(file.tables.map((t) => t.id)).toEqual([1, 2, 3, 4]);
    for (const v of [...file.plaza, ...file.tables.flatMap((t) => [t.x, t.z])]) expect(Number.isInteger(v)).toBe(true);
  });

  it('has a plaza containing (-22, -56), wholly inside the safe circle', () => {
    expect(inside(-22, -56, plaza)).toBe(true);
    for (const [x, z] of plaza) expect(fromSpawn(x, z)).toBeLessThan(safe);
  });

  it('snaps every table to a cell centre', () => {
    for (const t of tables) {
      expect(t.x * 10).toBe(cellCentreDm(t.x, grid.originX));
      expect(t.z * 10).toBe(cellCentreDm(t.z, grid.originZ));
    }
  });

  it('build.mjs writes cardtables.json deterministically, matching the committed file', () => {
    const a = mkdtempSync(join(tmpdir(), 'cardtables-a-'));
    const b = mkdtempSync(join(tmpdir(), 'cardtables-b-'));
    for (const out of [a, b]) execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });
    const f = 'cardtables.json';
    expect(readFileSync(join(a, f)).equals(readFileSync(join(b, f)))).toBe(true);
    expect(readFileSync(join(a, f)).equals(readFileSync(join(mapDir, f)))).toBe(true);
  });

  it('build.mjs fails when a table is on a blocked cell', () => {
    let blocked: [number, number] | null = null;
    for (let x = -200; x < 200 && !blocked; x++) {
      for (let z = -200; z < 200 && !blocked; z++) if (isBlocked(grid, x + 0.5, z + 0.5)) blocked = [x + 0.5, z + 0.5];
    }
    expect(blocked).not.toBeNull();
    const dir = buildInputs();
    build(dir); // the unchanged copy builds
    const src = readFileSync(join(dir, 'areas.mjs'), 'utf8');
    const moved = src.replace(/\{ id: 1, x: [-\d.]+, z: [-\d.]+ \}/, `{ id: 1, x: ${blocked![0]}, z: ${blocked![1]} }`);
    expect(moved).not.toBe(src);
    writeFileSync(join(dir, 'areas.mjs'), moved);
    expect(() => build(dir)).toThrow(/card table 1 .* not on a walkable cell/);
  });
});

describe('card table rules (AC2)', () => {
  it('puts every table inside the plaza, inside the safe circle, on a walkable cell', () => {
    for (const t of tables) {
      expect(inside(t.x, t.z, plaza)).toBe(true);
      expect(fromSpawn(t.x, t.z)).toBeLessThan(safe);
      expect(isBlocked(grid, t.x, t.z)).toBe(false);
    }
  });

  it('keeps every table at least 3 m from every other table', () => {
    for (const a of tables) {
      for (const b of tables) if (a !== b) expect(Math.hypot(a.x - b.x, a.z - b.z)).toBeGreaterThanOrEqual(3);
    }
  });

  it('puts the plaza about 20 m south of the spawn', () => {
    const cx = plaza.reduce((s, p) => s + p[0], 0) / plaza.length;
    const cz = plaza.reduce((s, p) => s + p[1], 0) / plaza.length;
    expect(cz - spawn.z).toBeGreaterThan(15); // south is +z
    expect(cz - spawn.z).toBeLessThan(25);
    expect(Math.abs(cx - spawn.x)).toBeLessThan(5);
  });
});

describe('card table meshes (AC4)', () => {
  const c = new CardTables(loadCardTables());

  it('draws 4 tables at the cardtables.json positions, standing on the ground', () => {
    expect(c.tables.count).toBe(4);
    const m = new THREE.Matrix4(), p = new THREE.Vector3();
    for (let i = 0; i < 4; i++) {
      c.tables.getMatrixAt(i, m);
      p.setFromMatrixPosition(m);
      expect(p.x).toBeCloseTo(file.tables[i].x / 10);
      expect(p.y).toBeCloseTo(0);
      expect(p.z).toBeCloseTo(file.tables[i].z / 10);
    }
    c.tables.geometry.computeBoundingBox();
    const bb = c.tables.geometry.boundingBox!;
    expect(bb.min.y).toBeCloseTo(0);
    expect(bb.max.y).toBeGreaterThan(0.6);
    expect(bb.max.y).toBeLessThan(1.2);
    expect(bb.max.x - bb.min.x).toBeLessThan(3);
    expect(bb.max.z - bb.min.z).toBeLessThan(3);
  });

  it('is low-poly and flat-coloured with no textures', () => {
    for (const mesh of [c.tables, c.plaza]) {
      const mat = mesh.material as THREE.MeshLambertMaterial;
      expect(mat.vertexColors).toBe(true);
      for (const k of ['map', 'emissiveMap', 'alphaMap', 'aoMap', 'lightMap', 'specularMap', 'envMap'] as const) {
        expect(mat[k]).toBeNull();
      }
      expect(mesh.geometry.getAttribute('uv')).toBeUndefined();
    }
    expect(c.tables.geometry.getAttribute('position').count / 3).toBeLessThan(200);
  });

  it('is in the world scene and adds 2 draw calls to the map', () => {
    expect(new WorldScene().scene.getObjectByName('card-table')).toBeDefined();
    const map = new CampusMap(loadCampus());
    const before = countDrawCalls(map.group);
    map.group.add(new CardTables(loadCardTables()).group);
    const after = countDrawCalls(map.group);
    console.log(`map draw calls: ${before} before, ${after} with card plaza and tables`);
    expect(after - before).toBe(2);
  });
});
