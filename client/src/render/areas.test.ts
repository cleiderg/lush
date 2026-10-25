import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { loadCampus } from './campusMap';
import { type AreasData, isBlocked, parseAreas, parseCollision } from './mapData';

const mapDir = resolve(__dirname, '../../../shared/map');
const areas: AreasData = parseAreas(readFileSync(join(mapDir, 'areas.json'), 'utf8'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const area = (id: string) => {
  const a = areas.areas.find((x) => x.id === id);
  if (!a) throw new Error(`area ${id} missing`);
  return a;
};
/** Polygon [x0,z0,...] in decimetres -> [x,z] pairs in metres. */
const pts = (p: number[]) => Array.from({ length: p.length / 2 }, (_, i) => [p[2 * i] / 10, p[2 * i + 1] / 10]);
const spawn = { x: areas.spawn.x / 10, z: areas.spawn.z / 10 };
const shop = { x: areas.shop.x / 10, z: areas.shop.z / 10 };

function segDist(px: number, pz: number, [ax, az]: number[], [bx, bz]: number[]): number {
  const dx = bx - ax, dz = bz - az;
  const t = Math.max(0, Math.min(1, ((px - ax) * dx + (pz - az) * dz) / (dx * dx + dz * dz)));
  return Math.hypot(px - (ax + t * dx), pz - (az + t * dz));
}
function inside(px: number, pz: number, poly: number[][]): boolean {
  let r = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [xi, zi] = poly[i], [xj, zj] = poly[j];
    if (zi > pz !== zj > pz && px < xi + ((pz - zi) * (xj - xi)) / (zj - zi)) r = !r;
  }
  return r;
}
function cross(o: number[], a: number[], b: number[]): number {
  return (a[0] - o[0]) * (b[1] - o[1]) - (a[1] - o[1]) * (b[0] - o[0]);
}
function segsCross(a: number[], b: number[], c: number[], d: number[]): boolean {
  const d1 = cross(c, d, a), d2 = cross(c, d, b), d3 = cross(a, b, c), d4 = cross(a, b, d);
  if (((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0))) return true;
  const on = (p: number[], q: number[], r: number[]) =>
    cross(p, q, r) === 0 && Math.min(p[0], q[0]) <= r[0] && r[0] <= Math.max(p[0], q[0]) &&
    Math.min(p[1], q[1]) <= r[1] && r[1] <= Math.max(p[1], q[1]);
  return on(c, d, a) || on(c, d, b) || on(a, b, c) || on(a, b, d);
}
function overlaps(a: number[][], b: number[][]): boolean {
  for (let i = 0; i < a.length; i++) {
    for (let j = 0; j < b.length; j++) {
      if (segsCross(a[i], a[(i + 1) % a.length], b[j], b[(j + 1) % b.length])) return true;
    }
  }
  return inside(a[0][0], a[0][1], b) || inside(b[0][0], b[0][1], a);
}

describe('areas.json format (AC1)', () => {
  it('holds spawn, safe radius, shop and the three area polygons in decimetres', () => {
    expect(areas.units).toBe('decimetres');
    expect(areas.safeRadius).toBe(400);
    for (const v of [areas.spawn.x, areas.spawn.z, areas.shop.x, areas.shop.z]) expect(Number.isInteger(v)).toBe(true);
    expect(area('aldrich_park').kind).toBe('pve');
    expect(area('south_fields').kind).toBe('pve');
    expect(area('pvp_zone').kind).toBe('pvp');
    for (const a of areas.areas) {
      expect(a.p.length % 2).toBe(0);
      expect(a.p.length).toBeGreaterThanOrEqual(6);
      for (const v of a.p) expect(Number.isInteger(v)).toBe(true);
    }
  });
});

describe('gameplay area rules (AC2)', () => {
  it('puts spawn and shop on walkable cells', () => {
    expect(isBlocked(grid, spawn.x, spawn.z)).toBe(false);
    expect(isBlocked(grid, shop.x, shop.z)).toBe(false);
  });

  it('puts the shop inside the safe circle about 20 m north of spawn', () => {
    const r = areas.safeRadius / 10;
    expect(Math.hypot(shop.x - spawn.x, shop.z - spawn.z)).toBeLessThan(r);
    expect(spawn.z - shop.z).toBeGreaterThan(15); // north is -z
    expect(spawn.z - shop.z).toBeLessThan(25);
    expect(Math.abs(shop.x - spawn.x)).toBeLessThan(5);
  });

  it('keeps every point of the PvP zone at least 300 m from spawn', () => {
    const poly = pts(area('pvp_zone').p);
    expect(inside(spawn.x, spawn.z, poly)).toBe(false);
    for (let i = 0; i < poly.length; i++) {
      expect(Math.hypot(poly[i][0] - spawn.x, poly[i][1] - spawn.z)).toBeGreaterThanOrEqual(300);
      expect(segDist(spawn.x, spawn.z, poly[i], poly[(i + 1) % poly.length])).toBeGreaterThanOrEqual(300);
    }
  });
});

describe('positions match the map spec (AC3)', () => {
  const tol = 3;
  it('has spawn near (-22, -76) and shop near (-22, -96)', () => {
    expect(Math.hypot(spawn.x + 22, spawn.z + 76)).toBeLessThan(tol);
    expect(Math.hypot(shop.x + 22, shop.z + 96)).toBeLessThan(tol);
  });

  const within = (id: string, x0: number, x1: number, z0: number, z1: number) => {
    for (const [x, z] of pts(area(id).p)) {
      expect(x).toBeGreaterThanOrEqual(x0 - tol);
      expect(x).toBeLessThanOrEqual(x1 + tol);
      expect(z).toBeGreaterThanOrEqual(z0 - tol);
      expect(z).toBeLessThanOrEqual(z1 + tol);
    }
  };
  it('puts South fields within x -360..-200, z 240..320', () => within('south_fields', -360, -200, 240, 320));
  it('puts the PvP zone within x 200..400, z -400..-280', () => within('pvp_zone', 200, 400, -400, -280));

  it('uses the campus.json green polygon 168468694 for Aldrich Park', () => {
    const park = loadCampus().green.find((g) => g.id === 168468694);
    expect(park).toBeDefined();
    expect(area('aldrich_park').p).toEqual(park?.p);
  });
});

describe('area overlap and build (AC4)', () => {
  it('keeps the PvP zone apart from both PvE areas', () => {
    const pvp = pts(area('pvp_zone').p);
    expect(overlaps(pvp, pts(area('aldrich_park').p))).toBe(false);
    expect(overlaps(pvp, pts(area('south_fields').p))).toBe(false);
  });

  it('overlap check catches overlapping polygons', () => {
    const sq = [[0, 0], [10, 0], [10, 10], [0, 10]];
    expect(overlaps(sq, [[5, 5], [15, 5], [15, 15], [5, 15]])).toBe(true);
    expect(overlaps(sq, [[2, 2], [4, 2], [4, 4]])).toBe(true);
    expect(overlaps(sq, [[20, 0], [30, 0], [30, 10]])).toBe(false);
  });

  it('build.mjs writes areas.json deterministically, matching the committed file', () => {
    const a = mkdtempSync(join(tmpdir(), 'areas-a-'));
    const b = mkdtempSync(join(tmpdir(), 'areas-b-'));
    for (const out of [a, b]) execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });
    const f = 'areas.json';
    expect(readFileSync(join(a, f)).equals(readFileSync(join(b, f)))).toBe(true);
    expect(readFileSync(join(a, f)).equals(readFileSync(join(mapDir, f)))).toBe(true);
  });
});
