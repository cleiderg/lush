import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { type HousingData, isBlocked, parseCampus, parseCollision, parseHousing } from './mapData';

const mapDir = resolve(__dirname, '../../../shared/map');
const housing: HousingData = parseHousing(readFileSync(join(mapDir, 'housing.json'), 'utf8'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const campus = parseCampus(readFileSync(join(mapDir, 'campus.json'), 'utf8'));

type Pt = [number, number];
const poly = (p: number[]): Pt[] => Array.from({ length: p.length / 2 }, (_, i) => [p[2 * i], p[2 * i + 1]]);
function inside(x: number, z: number, p: Pt[]): boolean {
  let r = false;
  for (let i = 0, j = p.length - 1; i < p.length; j = i++) {
    const [xi, zi] = p[i], [xj, zj] = p[j];
    if (zi > z !== zj > z && x < xi + ((z - zi) * (xj - xi)) / (zj - zi)) r = !r;
  }
  return r;
}
function edgeDist(x: number, z: number, p: Pt[]): number {
  let best = Infinity;
  for (let i = 0; i < p.length; i++) {
    const [ax, az] = p[i], [bx, bz] = p[(i + 1) % p.length];
    const dx = bx - ax, dz = bz - az;
    const t = Math.max(0, Math.min(1, ((x - ax) * dx + (z - az) * dz) / (dx * dx + dz * dz || 1)));
    best = Math.min(best, Math.hypot(x - (ax + t * dx), z - (az + t * dz)));
  }
  return best;
}
const walkable = (p: { x: number; z: number }) => !isBlocked(grid, p.x / 10, p.z / 10);
const cellKey = (p: { x: number; z: number }) => `${Math.floor(p.x / 10)},${Math.floor(p.z / 10)}`;

describe('housing.json', () => {
  it('rebuilds to an identical file (AC1)', () => {
    const out = mkdtempSync(join(tmpdir(), 'housing-'));
    execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });
    expect(readFileSync(join(out, 'housing.json'), 'utf8')).toBe(readFileSync(join(mapDir, 'housing.json'), 'utf8'));
  }, 120_000);

  it('has the two areas and unique house ids (AC1)', () => {
    expect(housing.units).toBe('decimetres');
    expect(housing.areas.map((a) => a.id)).toEqual(['mesa_court', 'middle_earth']);
    for (const a of housing.areas) expect(a.name.length).toBeGreaterThan(0);
    expect(new Set(housing.houses.map((h) => h.id)).size).toBe(housing.houses.length);
  });

  it('has 20 houses per area, each door on its own walkable cell by its building (AC2)', () => {
    const doors = new Set<string>();
    for (const a of housing.areas) {
      const mine = housing.houses.filter((h) => h.area === a.id);
      expect(mine).toHaveLength(20);
      for (const h of mine) {
        const b = campus.buildings.find((x) => x.id === h.osm);
        expect(b, `building ${h.osm}`).toBeDefined();
        expect(walkable(h)).toBe(true);
        expect(inside(h.x, h.z, poly(a.p))).toBe(true);
        expect(inside(h.x, h.z, poly(b!.p))).toBe(false);
        expect(edgeDist(h.x, h.z, poly(b!.p))).toBeLessThanOrEqual(15);
        expect(doors.has(cellKey(h))).toBe(false);
        doors.add(cellKey(h));
      }
    }
    expect(doors.size).toBe(40);
  });

  it('puts the thrift stall and workbench on walkable cells close together, off the doors (AC3)', () => {
    const doors = new Set(housing.houses.map(cellKey));
    for (const a of housing.areas) {
      for (const p of [a.thrift, a.workbench]) {
        expect(walkable(p)).toBe(true);
        expect(inside(p.x, p.z, poly(a.p))).toBe(true);
        expect(doors.has(cellKey(p))).toBe(false);
      }
      expect(Math.hypot(a.thrift.x - a.workbench.x, a.thrift.z - a.workbench.z)).toBeLessThanOrEqual(30);
      expect(cellKey(a.thrift)).not.toBe(cellKey(a.workbench));
    }
  });
});
