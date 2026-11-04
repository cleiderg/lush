import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { pointInPolygon } from './nature';
import { isBlocked, parseCampus, parseCollision } from './mapData';

const mapDir = resolve(__dirname, '../../../shared/map');
const read = (f: string) => readFileSync(join(mapDir, f), 'utf8');
const campus = parseCampus(read('campus.json'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));

type Cell = [number, number];
interface Throne {
  osm: number;
  entrance: number[];
  throneRoom: number[];
  outerOpening: Cell[];
  mainGate: Cell[];
  sideGate: Cell[];
  dais: number[];
}
const throne = JSON.parse(read('throne.json')) as Throne;
const hall = campus.buildings.find((b) => b.id === 62646030)!;

const m = (v: number) => v / 10;
const walkable = (x: number, z: number) => !isBlocked(grid, x, z);
const inPoly = (p: number[], x: number, z: number) => pointInPolygon(p, x * 10, z * 10);
const key = (x: number, z: number) => `${Math.floor(x)},${Math.floor(z)}`;
const cm = (cells: Cell[]): Cell[] => cells.map(([x, z]) => [m(x), m(z)]);

function cellsIn(p: number[]): Cell[] {
  const xs = p.filter((_, i) => i % 2 === 0), zs = p.filter((_, i) => i % 2 === 1);
  const out: Cell[] = [];
  for (let z = Math.floor(m(Math.min(...zs))); z < Math.ceil(m(Math.max(...zs))); z++) {
    for (let x = Math.floor(m(Math.min(...xs))); x < Math.ceil(m(Math.max(...xs))); x++) {
      if (inPoly(p, x + 0.5, z + 0.5)) out.push([x + 0.5, z + 0.5]);
    }
  }
  return out;
}

/** Cells reachable (4-way) from `start` over walkable cells within the hall's bounding box plus a margin, minus `closed`. */
function flood(start: Cell, closed: Set<string>): Set<string> {
  const xs = hall.p.filter((_, i) => i % 2 === 0), zs = hall.p.filter((_, i) => i % 2 === 1);
  const x0 = Math.floor(m(Math.min(...xs))) - 20, x1 = Math.ceil(m(Math.max(...xs))) + 20;
  const z0 = Math.floor(m(Math.min(...zs))) - 20, z1 = Math.ceil(m(Math.max(...zs))) + 20;
  const seen = new Set<string>([key(...start)]);
  const queue: Cell[] = [start];
  while (queue.length) {
    const [x, z] = queue.pop()!;
    for (const [dx, dz] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
      const nx = x + dx, nz = z + dz;
      if (nx < x0 || nx > x1 || nz < z0 || nz > z1) continue;
      const k = key(nx, nz);
      if (seen.has(k) || closed.has(k) || !walkable(nx, nz)) continue;
      seen.add(k);
      queue.push([nx, nz]);
    }
  }
  return seen;
}

/** A walkable cell next to the outer opening that is outside the hall. */
function outsideStart(): Cell {
  for (const [x, z] of cm(throne.outerOpening)) {
    for (const [dx, dz] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
      const nx = x + dx, nz = z + dz;
      if (walkable(nx, nz) && !pointInPolygon(hall.p, nx * 10, nz * 10)) return [nx, nz];
    }
  }
  throw new Error('no walkable cell outside the opening');
}

describe('throne room (throne.json)', () => {
  it('AC1: has the documented parts for Aldrich Hall', () => {
    expect(throne.osm).toBe(62646030);
    for (const p of [throne.entrance, throne.throneRoom, throne.dais]) expect(p.length).toBeGreaterThanOrEqual(8);
    expect(throne.outerOpening.length).toBeGreaterThan(0);
    expect(throne.mainGate.length).toBeGreaterThan(0);
    expect(throne.sideGate.length).toBeGreaterThan(0);
  });

  it('AC2: the entrance is reachable from outside; the throne room only through a gate', () => {
    const start = outsideStart();
    const open = flood(start, new Set());
    const entranceCells = cellsIn(throne.entrance);
    expect(entranceCells.length).toBeGreaterThan(0);
    for (const [x, z] of entranceCells) expect(open.has(key(x, z))).toBe(true);
    const dais = cellsIn(throne.dais);
    expect(dais.length).toBeGreaterThan(0);
    for (const [x, z] of dais) expect(open.has(key(x, z))).toBe(true);

    const gates = new Set([...cm(throne.mainGate), ...cm(throne.sideGate)].map(([x, z]) => key(x, z)));
    const sealed = flood(start, gates);
    for (const [x, z] of dais) expect(sealed.has(key(x, z))).toBe(false);
    for (const [x, z] of cellsIn(throne.throneRoom)) expect(sealed.has(key(x, z))).toBe(false);
    // Each gate alone still lets you through.
    for (const one of [throne.mainGate, throne.sideGate]) {
      const closed = new Set(cm(one).map(([x, z]) => key(x, z)));
      expect(flood(start, closed).has(key(...dais[0]))).toBe(true);
    }
  });

  it('AC3: floor, gate and opening cells are walkable; the dais is in the throne room; the rest stays blocked', () => {
    const floor = new Set<string>();
    for (const [x, z] of [...cellsIn(throne.entrance), ...cellsIn(throne.throneRoom), ...cm(throne.outerOpening), ...cm(throne.mainGate), ...cm(throne.sideGate)]) {
      expect(walkable(x, z)).toBe(true);
      floor.add(key(x, z));
    }
    for (const [x, z] of cellsIn(throne.dais)) expect(inPoly(throne.throneRoom, x, z)).toBe(true);
    let blocked = 0;
    const xs = hall.p.filter((_, i) => i % 2 === 0), zs = hall.p.filter((_, i) => i % 2 === 1);
    for (let z = Math.floor(m(Math.min(...zs))); z < Math.ceil(m(Math.max(...zs))); z++) {
      for (let x = Math.floor(m(Math.min(...xs))); x < Math.ceil(m(Math.max(...xs))); x++) {
        if (!pointInPolygon(hall.p, (x + 0.5) * 10, (z + 0.5) * 10) || floor.has(key(x, z))) continue;
        expect(walkable(x + 0.5, z + 0.5)).toBe(false);
        blocked++;
      }
    }
    expect(blocked).toBeGreaterThan(1000);
  });

  it('AC4: build.mjs reproduces throne.json and collision.bin byte for byte', () => {
    const out = mkdtempSync(join(tmpdir(), 'lush-throne-'));
    execFileSync('node', [join(mapDir, 'build.mjs'), out], { stdio: 'pipe' });
    for (const f of ['throne.json', 'collision.bin']) {
      expect(readFileSync(join(out, f)).equals(readFileSync(join(mapDir, f)))).toBe(true);
    }
    expect(readFileSync(resolve(__dirname, '../../../server/cmd/server/collision.bin')).equals(readFileSync(join(mapDir, 'collision.bin')))).toBe(true);
  }, 60000);
});
