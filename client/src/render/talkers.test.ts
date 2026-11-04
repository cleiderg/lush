import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { pointInPolygon, segmentDistance2 } from './nature';
import { isBlocked, parseAreas, parseCampus, parseCollision, parseSkills, parseSpawnAreas, parseTalkers } from './mapData';

const mapDir = resolve(__dirname, '../../../shared/map');
const read = (f: string) => readFileSync(join(mapDir, f), 'utf8');
const areas = parseAreas(read('areas.json'));
const campus = parseCampus(read('campus.json'));
const skills = parseSkills(read('skills.json'));
const talkers = parseTalkers(read('talkers.json'));
const spawnAreas = parseSpawnAreas(read('spawnareas.json'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const m = (v: number) => v / 10;
const spawn = { x: m(areas.spawn.x), z: m(areas.spawn.z) };
const safe = m(areas.safeRadius);
const area = (id: string) => areas.areas.find((a) => a.id === id)!.p;
const spawnArea = (id: string) => spawnAreas.areas.find((a) => a.id === id)!.p;
const talker = (id: string) => {
  const t = talkers.talkers.find((q) => q.id === id)!;
  return { x: m(t.x), z: m(t.z) };
};
const walkable = (x: number, z: number) => !isBlocked(grid, x, z);
const dist = (a: { x: number; z: number }, b: { x: number; z: number }) => Math.hypot(a.x - b.x, a.z - b.z);
/** Distance in metres from a point (metres) to a closed polygon's outline (decimetres). */
function edgeDist(p: number[], x: number, z: number): number {
  let best = Infinity;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    best = Math.min(best, Math.sqrt(segmentDistance2(x * 10, z * 10, p[j], p[j + 1], p[i], p[i + 1])) / 10);
  }
  return best;
}
const inside = (p: number[], x: number, z: number) => pointInPolygon(p, x * 10, z * 10);
/** Centres (metres) of the walkable 1 m cells whose centre is inside the polygon. */
function cellsIn(p: number[]): { x: number; z: number }[] {
  const xs = p.filter((_, i) => i % 2 === 0), zs = p.filter((_, i) => i % 2 === 1);
  const out: { x: number; z: number }[] = [];
  for (let z = Math.floor(m(Math.min(...zs))); z < Math.ceil(m(Math.max(...zs))); z++) {
    for (let x = Math.floor(m(Math.min(...xs))); x < Math.ceil(m(Math.max(...xs))); x++) {
      if (inside(p, x + 0.5, z + 0.5) && walkable(x + 0.5, z + 0.5)) out.push({ x: x + 0.5, z: z + 0.5 });
    }
  }
  return out;
}

/** Langson Library podium, by OSM id (LANGSON_WAY in shared/map/talkers.mjs). */
const LANGSON = campus.buildings.find((b) => b.id === 171169252)!;

describe('talkers.json', () => {
  it('lists the four spots, each on a walkable collision cell', () => {
    expect(talkers.talkers.map((t) => t.id)).toEqual(['librarian', 'park_ranger', 'food_truck_cook', 'guide']);
    for (const t of talkers.talkers) expect(walkable(m(t.x), m(t.z)), t.id).toBe(true);
  });

  it('puts the guide within 3 m of the spawn and inside the safe circle', () => {
    expect(dist(talker('guide'), spawn)).toBeLessThanOrEqual(3);
    expect(dist(talker('guide'), spawn)).toBeLessThan(safe);
  });

  it('puts the food truck cook within 3 m of the grill', () => {
    expect(dist(talker('food_truck_cook'), { x: m(skills.grill.x), z: m(skills.grill.z) })).toBeLessThanOrEqual(3);
  });

  it('puts the librarian outside Langson Library and within 10 m of it', () => {
    const l = talker('librarian');
    expect(inside(LANGSON.p, l.x, l.z)).toBe(false);
    expect(edgeDist(LANGSON.p, l.x, l.z)).toBeLessThanOrEqual(10);
  });

  it('puts the park ranger within 10 m of the south fields, on the side nearest Aldrich Park', () => {
    const r = talker('park_ranger');
    const f = area('south_fields');
    expect(inside(f, r.x, r.z) || edgeDist(f, r.x, r.z) <= 10).toBe(true);
    const xs = f.filter((_, i) => i % 2 === 0), zs = f.filter((_, i) => i % 2 === 1);
    const centroid = { x: m(xs.reduce((a, v) => a + v, 0) / xs.length), z: m(zs.reduce((a, v) => a + v, 0) / zs.length) };
    expect(dist(r, spawn)).toBeLessThan(dist(centroid, spawn));
    // Nearest of the polygon's corners to the park (its nearest side is the one holding that corner).
    const corners = xs.map((x, i) => ({ x: m(x), z: m(zs[i]) })).sort((a, b) => dist(a, spawn) - dist(b, spawn));
    expect(dist(r, corners[0])).toBeLessThan(dist(r, corners[2]));
  });
});

describe('spawnareas.json', () => {
  it('has goose_pond inside the park, clear of the pond and safe circle, with cells by the pond', () => {
    const g = spawnArea('goose_pond');
    const park = area('aldrich_park');
    for (let i = 0; i < g.length; i += 2) expect(inside(park, m(g[i]), m(g[i + 1]))).toBe(true);
    const cells = cellsIn(g);
    expect(cells.filter((c) => edgeDist(skills.pond.p, c.x, c.z) <= 8).length).toBeGreaterThanOrEqual(6);
    for (const c of cells) {
      expect(dist(c, spawn)).toBeGreaterThan(safe);
      expect(inside(skills.pond.p, c.x, c.z)).toBe(false);
    }
    for (let i = 0; i < g.length; i += 2) expect(dist({ x: m(g[i]), z: m(g[i + 1]) }, spawn)).toBeGreaterThan(safe);
  });

  it('has south_fields_far inside south_fields, farther from the spawn than its centroid', () => {
    const far = spawnArea('south_fields_far');
    const f = area('south_fields');
    const [x0, z0, x1, , , z1] = f;
    for (let i = 0; i < far.length; i += 2) {
      expect(far[i]).toBeGreaterThanOrEqual(x0);
      expect(far[i]).toBeLessThanOrEqual(x1);
      expect(far[i + 1]).toBeGreaterThanOrEqual(z0);
      expect(far[i + 1]).toBeLessThanOrEqual(z1);
    }
    const centroid = { x: m((x0 + x1) / 2), z: m((z0 + z1) / 2) };
    const cells = cellsIn(far);
    expect(cells.length).toBeGreaterThanOrEqual(4);
    for (const c of cells) expect(dist(c, spawn)).toBeGreaterThan(dist(centroid, spawn));
  });
});

describe('build', () => {
  it('rebuilds talkers.json and spawnareas.json byte-identically and leaves areas.json and collision.bin unchanged', () => {
    const out = mkdtempSync(join(tmpdir(), 'lush-talkers-'));
    execFileSync('node', [join(mapDir, 'build.mjs'), out], { stdio: 'pipe' });
    for (const f of ['talkers.json', 'spawnareas.json', 'areas.json']) {
      expect(readFileSync(join(out, f), 'utf8'), f).toBe(read(f));
    }
    expect(readFileSync(join(out, 'collision.bin')).equals(readFileSync(join(mapDir, 'collision.bin')))).toBe(true);
  }, 60_000);
});
