import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { CollisionGrid } from '../state/collision';
import { Prediction } from '../state/prediction';
import { type AreasData, type CampusData, parseAreas } from './mapData';

const mapDir = resolve(__dirname, '../../../shared/map');
const areas: AreasData = parseAreas(readFileSync(join(mapDir, 'areas.json'), 'utf8'));
const campus = JSON.parse(readFileSync(join(mapDir, 'campus.json'), 'utf8')) as CampusData;
const grid = CollisionGrid.parse(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const STUDENT_CENTER = 157979894;
const footprint = campus.buildings.find((b) => b.id === STUDENT_CENTER)!.p.map((v) => v / 10);
const cafe = areas.interiors.find((a) => a.id === 'student_center_cafe')!;
const floor = cafe.p.map((v) => v / 10);
const spawn = { x: areas.spawn.x / 10, z: areas.spawn.z / 10 };

/** Point inside polygon [x0,z0,x1,z1,...] (metres). */
function inside(x: number, z: number, p: number[]): boolean {
  let r = false;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    if (p[i + 1] > z !== p[j + 1] > z && x < p[i] + ((z - p[i + 1]) * (p[j] - p[i])) / (p[j + 1] - p[i + 1])) r = !r;
  }
  return r;
}
const centre = (c: number, r: number) => ({ x: grid.originX + c + 0.5, z: grid.originZ + r + 0.5 });
const inFloor = (c: number, r: number) => { const p = centre(c, r); return inside(p.x, p.z, floor); };
const inFootprint = (c: number, r: number) => { const p = centre(c, r); return inside(p.x, p.z, footprint); };

// Cells (col, row) of the footprint's bounding box.
const xs = footprint.filter((_, i) => i % 2 === 0), zs = footprint.filter((_, i) => i % 2 === 1);
const c0 = Math.floor(Math.min(...xs) - grid.originX), c1 = Math.ceil(Math.max(...xs) - grid.originX);
const r0 = Math.floor(Math.min(...zs) - grid.originZ), r1 = Math.ceil(Math.max(...zs) - grid.originZ);
const footprintCells: [number, number][] = [];
for (let r = r0; r <= r1; r++) for (let c = c0; c <= c1; c++) if (inFootprint(c, r)) footprintCells.push([c, r]);
// Door cells: walkable footprint cells off the floor.
const doorCells = footprintCells.filter(([c, r]) => !inFloor(c, r) && !grid.cellBlocked(c, r));

/** Cells reached from spawn moving to any of the 8 neighbours, never entering `wall` cells. */
function flood(wall: (c: number, r: number) => boolean): Uint8Array {
  const seen = new Uint8Array(grid.width * grid.height);
  const sc = Math.floor(spawn.x - grid.originX), sr = Math.floor(spawn.z - grid.originZ);
  const stack = [sc, sr];
  seen[sr * grid.width + sc] = 1;
  while (stack.length) {
    const r = stack.pop()!, c = stack.pop()!;
    for (let dr = -1; dr <= 1; dr++) for (let dc = -1; dc <= 1; dc++) {
      const nc = c + dc, nr = r + dr, i = nr * grid.width + nc;
      if (grid.cellBlocked(nc, nr) || wall(nc, nr) || seen[i]) continue;
      seen[i] = 1;
      stack.push(nc, nr);
    }
  }
  return seen;
}

describe('Student Center café in areas.json (AC1)', () => {
  it('has one safe interior, student_center_cafe, inside the Student Center footprint', () => {
    expect(areas.interiors).toHaveLength(1);
    expect(cafe.kind).toBe('safe');
    expect(Object.keys(cafe).sort()).toEqual(['id', 'kind', 'p']);
    expect(cafe.p.length % 2).toBe(0);
    expect(cafe.p.length).toBeGreaterThanOrEqual(6);
    for (const v of cafe.p) expect(Number.isInteger(v)).toBe(true);
    for (let i = 0; i < floor.length; i += 2) expect(inside(floor[i], floor[i + 1], footprint)).toBe(true);
  });

  it('holds an 8 x 6 m walkable floor', () => {
    const fx = floor.filter((_, i) => i % 2 === 0), fz = floor.filter((_, i) => i % 2 === 1);
    // The polygon is an axis-aligned rectangle of whole cells.
    expect(floor.length).toBe(8);
    const w = Math.max(...fx) - Math.min(...fx), d = Math.max(...fz) - Math.min(...fz);
    expect(Math.max(w, d)).toBeGreaterThanOrEqual(8);
    expect(Math.min(w, d)).toBeGreaterThanOrEqual(6);
    for (let x = Math.min(...fx) + 0.5; x < Math.max(...fx); x++) {
      for (let z = Math.min(...fz) + 0.5; z < Math.max(...fz); z++) expect(grid.blocked(x, z)).toBe(false);
    }
  });

  it('build.mjs writes areas.json and collision.bin byte-identically, matching the committed files', () => {
    const a = mkdtempSync(join(tmpdir(), 'cafe-a-'));
    const b = mkdtempSync(join(tmpdir(), 'cafe-b-'));
    for (const out of [a, b]) execFileSync('node', [join(mapDir, 'build.mjs'), out, '--no-nature'], { encoding: 'utf8' });
    for (const f of ['areas.json', 'collision.bin']) {
      expect(readFileSync(join(a, f)).equals(readFileSync(join(b, f)))).toBe(true);
      expect(readFileSync(join(a, f)).equals(readFileSync(join(mapDir, f)))).toBe(true);
    }
  });
});

describe('Student Center café in collision.bin (AC2)', () => {
  it('opens the floor, walls it in and keeps the rest of the footprint blocked', () => {
    let floorCells = 0;
    for (const [c, r] of footprintCells) {
      if (inFloor(c, r)) {
        floorCells++;
        expect(grid.cellBlocked(c, r)).toBe(false);
      }
    }
    expect(floorCells).toBeGreaterThanOrEqual(48);
    // A short door: a few cells, all next to the floor.
    expect(doorCells.length).toBeGreaterThan(0);
    expect(doorCells.length).toBeLessThanOrEqual(4);
    const isDoor = (c: number, r: number) => doorCells.some(([dc, dr]) => dc === c && dr === r);
    for (const [c, r] of doorCells) {
      expect([[1, 0], [-1, 0], [0, 1], [0, -1]].some(([dc, dr]) => inFloor(c + dc, r + dr))).toBe(true);
    }
    // Walls: every cell round the floor (8 neighbours) that isn't floor or door is blocked footprint.
    for (const [c, r] of footprintCells) {
      if (!inFloor(c, r)) continue;
      for (let dr = -1; dr <= 1; dr++) for (let dc = -1; dc <= 1; dc++) {
        const nc = c + dc, nr = r + dr;
        if (inFloor(nc, nr) || isDoor(nc, nr)) continue;
        expect(inFootprint(nc, nr)).toBe(true);
        expect(grid.cellBlocked(nc, nr)).toBe(true);
      }
    }
  });

  it('is reached from the spawn only through the door', () => {
    const floorIdx = footprintCells.filter(([c, r]) => inFloor(c, r)).map(([c, r]) => r * grid.width + c);
    const open = flood(() => false);
    for (const i of floorIdx) expect(open[i]).toBe(1);
    const shut = flood((c, r) => doorCells.some(([dc, dr]) => dc === c && dr === r));
    for (const i of floorIdx) expect(shut[i]).toBe(0);
  });
});

describe('prediction walks into the café (AC4)', () => {
  it('walks from outside, through the door, into the café', () => {
    const door = doorCells.map(([c, r]) => centre(c, r));
    const dx = door.reduce((a, p) => a + p.x, 0) / door.length;
    const dz = Math.max(...door.map((p) => p.z));
    const fz = floor.filter((_, i) => i % 2 === 1);
    // The door is in the south wall: start 3 m south of it, walk north into the room's middle.
    const start = { x: dx, z: dz + 3 };
    const goalZ = (Math.min(...fz) + Math.max(...fz)) / 2;
    expect(grid.blocked(start.x, start.z)).toBe(false);
    expect(inside(start.x, start.z, floor)).toBe(false);
    const p = new Prediction(start, grid);
    let throughDoor = false;
    for (let i = 0; i < 200 && p.pos.z > goalZ; i++) {
      p.applyInput(0, -1, 0.05);
      if (door.some((c) => Math.abs(p.pos.x - c.x) <= 0.5 && Math.abs(p.pos.z - c.z) <= 0.5)) throughDoor = true;
    }
    expect(throughDoor).toBe(true);
    expect(inside(p.pos.x, p.pos.z, floor)).toBe(true);
    expect(p.pos.z).toBeLessThanOrEqual(goalZ);
  });

  it('cannot walk in through a wall', () => {
    const fx = floor.filter((_, i) => i % 2 === 0), fz = floor.filter((_, i) => i % 2 === 1);
    // 1 m in from the west edge, south of the floor, away from the door.
    const start = { x: Math.min(...fx) + 1.5, z: Math.max(...fz) + 2.5 };
    expect(grid.blocked(start.x, start.z)).toBe(false);
    const p = new Prediction(start, grid);
    for (let i = 0; i < 200; i++) p.applyInput(0, -1, 0.05);
    expect(inside(p.pos.x, p.pos.z, floor)).toBe(false);
  });
});
