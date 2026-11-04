import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { pointInPolygon } from './nature';
import { isBlocked, parseAreas, parseCampus, parseCollision } from './mapData';

const mapDir = resolve(__dirname, '../../../shared/map');
const read = (f: string) => readFileSync(join(mapDir, f), 'utf8');
const areas = parseAreas(read('areas.json'));
const campus = parseCampus(read('campus.json'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));

type Cell = [number, number];
interface Hall {
  id: string;
  osm: number;
  entrance: number[];
  main: number[];
  vaultRoom: number[];
  outerDoors: Cell[][];
  innerDoor: Cell[];
  vault: { x: number; z: number };
  banner: { x: number; z: number };
}
const halls = (JSON.parse(read('halls.json')) as { halls: Hall[] }).halls;

const LANDMARKS = [171169252, 1374185239, 62646030, 157979894];
const PARK_WAY = 168468694;
const m = (v: number) => v / 10;
const building = (id: number) => campus.buildings.find((b) => b.id === id)!;
const walkable = (x: number, z: number) => !isBlocked(grid, x, z);
const inFoot = (h: Hall, x: number, z: number) => pointInPolygon(building(h.osm).p, x * 10, z * 10);
const inPoly = (p: number[], x: number, z: number) => pointInPolygon(p, x * 10, z * 10);

/** Whole-metre cells (centres, metres) whose centre is inside the polygon. */
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
const doorCells = (h: Hall): Cell[] => [...h.outerDoors.flat(), ...h.innerDoor].map(([x, z]) => [m(x), m(z)]);
const floorCells = (h: Hall): Cell[] => [h.entrance, h.main, h.vaultRoom].flatMap(cellsIn);

function segmentsCross(a: Cell, b: Cell, c: Cell, d: Cell): boolean {
  const cr = (o: Cell, p: Cell, q: Cell) => (p[0] - o[0]) * (q[1] - o[1]) - (p[1] - o[1]) * (q[0] - o[0]);
  return cr(a, b, c) * cr(a, b, d) < 0 && cr(c, d, a) * cr(c, d, b) < 0;
}

describe('group halls (halls.json)', () => {
  it('AC1: lists exactly 6 halls with the documented parts', () => {
    expect(halls).toHaveLength(6);
    expect(new Set(halls.map((h) => h.id)).size).toBe(6);
    for (const h of halls) {
      expect(building(h.osm)).toBeDefined();
      for (const p of [h.entrance, h.main, h.vaultRoom]) expect(p.length).toBeGreaterThanOrEqual(8);
      expect([1, 2]).toContain(h.outerDoors.length);
      for (const d of h.outerDoors) expect(d.length).toBeGreaterThan(0);
      expect(h.innerDoor.length).toBeGreaterThan(0);
    }
  });

  it('AC2: no landmarks, no shared buildings, all on the ring with a clear line from the spawn', () => {
    const park = areas.areas.find((a) => a.id === 'aldrich_park')!.p;
    expect(campus.green.find((g) => g.id === PARK_WAY)!.p).toEqual(park);
    const ids = halls.map((h) => h.osm);
    expect(new Set(ids).size).toBe(6);
    for (const id of ids) expect(LANDMARKS).not.toContain(id);
    const spawn: Cell = [areas.spawn.x, areas.spawn.z];
    for (const h of halls) {
      const p = building(h.osm).p;
      const c: Cell = [0, 0];
      for (let i = 0; i < p.length; i += 2) { c[0] += p[i]; c[1] += p[i + 1]; }
      c[0] /= p.length / 2; c[1] /= p.length / 2;
      expect(pointInPolygon(park, c[0], c[1])).toBe(false);
      for (const o of campus.buildings) {
        if (o.id === h.osm) continue;
        expect(pointInPolygon(o.p, spawn[0], spawn[1])).toBe(false);
        expect(pointInPolygon(o.p, c[0], c[1])).toBe(false);
        for (let i = 0, j = o.p.length - 2; i < o.p.length; j = i, i += 2) {
          expect(segmentsCross(spawn, c, [o.p[j], o.p[j + 1]], [o.p[i], o.p[i + 1]])).toBe(false);
        }
      }
    }
  });

  it('AC3: floors and doors are walkable, the rest of each footprint stays blocked', () => {
    for (const h of halls) {
      const open = new Set([...floorCells(h), ...doorCells(h)].map(([x, z]) => `${x},${z}`));
      expect(cellsIn(h.entrance).length).toBeGreaterThan(0);
      for (const key of open) {
        const [x, z] = key.split(',').map(Number);
        expect(walkable(x, z)).toBe(true);
      }
      for (const [x, z] of cellsIn(building(h.osm).p)) {
        if (!open.has(`${x},${z}`)) expect(walkable(x, z)).toBe(false);
      }
    }
  });

  it('AC3: the vault room is reachable from outside only through the inner door', () => {
    for (const h of halls) {
      const key = (x: number, z: number) => `${x},${z}`;
      const inner = new Set(h.innerDoor.map(([x, z]) => key(m(x), m(z))));
      const vaultCells = new Set(cellsIn(h.vaultRoom).map(([x, z]) => key(x, z)));
      const reach = (blockInner: boolean) => {
        const door: Cell = [m(h.outerDoors[0][0][0]), m(h.outerDoors[0][0][1])];
        const seen = new Set<string>();
        // start on the walkable cells outside the footprint next to the first outer door
        const stack: Cell[] = [];
        for (const [dx, dz] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
          const q: Cell = [door[0] + dx, door[1] + dz];
          if (!inFoot(h, q[0], q[1]) && walkable(q[0], q[1])) stack.push(q);
        }
        expect(stack.length).toBeGreaterThan(0);
        while (stack.length) {
          const [x, z] = stack.pop()!;
          const k = key(x, z);
          if (seen.has(k) || !walkable(x, z) || (blockInner && inner.has(k))) continue;
          if (Math.abs(x - door[0]) > 60 || Math.abs(z - door[1]) > 60) continue;
          seen.add(k);
          for (const [dx, dz] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) stack.push([x + dx, z + dz]);
        }
        return [...vaultCells].some((c) => seen.has(c));
      };
      expect(reach(false)).toBe(true);
      expect(reach(true)).toBe(false);
    }
  });

  it('AC4: vaults and banner spots are on walkable cells, in the right places', () => {
    for (const h of halls) {
      const v = { x: m(h.vault.x), z: m(h.vault.z) };
      const b = { x: m(h.banner.x), z: m(h.banner.z) };
      expect(walkable(v.x, v.z)).toBe(true);
      expect(inPoly(h.vaultRoom, v.x, v.z)).toBe(true);
      expect(walkable(b.x, b.z)).toBe(true);
      expect(inFoot(h, b.x, b.z)).toBe(false);
    }
  });

  it('AC5: build.mjs reproduces halls.json and collision.bin byte for byte', () => {
    const out = mkdtempSync(join(tmpdir(), 'lush-halls-'));
    execFileSync('node', [join(mapDir, 'build.mjs'), out], { stdio: 'pipe' });
    for (const f of ['halls.json', 'collision.bin']) {
      expect(readFileSync(join(out, f)).equals(readFileSync(join(mapDir, f)))).toBe(true);
    }
    expect(readFileSync(resolve(__dirname, '../../../server/cmd/server/collision.bin')).equals(readFileSync(join(mapDir, 'collision.bin')))).toBe(true);
  }, 60000);
});
