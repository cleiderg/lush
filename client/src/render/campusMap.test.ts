import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { showAttribution } from './attribution';
import { CampusMap, MAX_GREEN_LAYER, greenLayers, loadCampus } from './campusMap';
import { isBlocked, parseCollision } from './mapData';
import { mulberry32 } from './rng';

const mapDir = resolve(__dirname, '../../../shared/map');
const build = (out: string) => execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });

describe('map build script (AC1)', () => {
  it('is deterministic and matches the committed files', () => {
    const a = mkdtempSync(join(tmpdir(), 'map-a-'));
    const b = mkdtempSync(join(tmpdir(), 'map-b-'));
    build(a);
    build(b);
    for (const f of ['campus.json', 'collision.bin']) {
      expect(readFileSync(join(a, f)).equals(readFileSync(join(b, f)))).toBe(true);
      expect(readFileSync(join(a, f)).equals(readFileSync(join(mapDir, f)))).toBe(true);
    }
  });

  it('covers buildings, paths and green areas', () => {
    const c = loadCampus();
    expect(c.buildings.length).toBeGreaterThan(50);
    expect(c.paths.length).toBeGreaterThan(50);
    expect(c.green.length).toBeGreaterThan(10);
  });
});

describe('campus rendering (AC2, AC3)', () => {
  it('builds one mesh per chunk, within a triangle budget', () => {
    const m = new CampusMap(loadCampus());
    console.log(`map: ${m.stats.chunks} chunks (draw calls if all visible), ${m.stats.triangles} triangles`);
    expect(m.group.children.length).toBe(m.stats.chunks);
    expect(m.stats.triangles).toBeGreaterThan(1000);
    expect(m.stats.triangles).toBeLessThan(400_000);
  });

  it('puts overlapping green areas on different layers so they do not z-fight', () => {
    const green = loadCampus().green;
    const layers = greenLayers(green);
    const box = (p: number[]) => {
      const xs = p.filter((_, i) => i % 2 === 0), zs = p.filter((_, i) => i % 2 === 1);
      return [Math.min(...xs), Math.min(...zs), Math.max(...xs), Math.max(...zs)];
    };
    const boxes = green.map((g) => box(g.p));
    let overlaps = 0;
    for (let i = 0; i < green.length; i++) {
      for (let j = i + 1; j < green.length; j++) {
        const a = boxes[i], b = boxes[j];
        if (a[0] < b[2] && b[0] < a[2] && a[1] < b[3] && b[1] < a[3]) {
          overlaps++;
          expect(layers.get(green[i].id)).not.toBe(layers.get(green[j].id));
        }
      }
    }
    expect(overlaps).toBeGreaterThan(0);
    expect(Math.max(...layers.values())).toBeLessThanOrEqual(MAX_GREEN_LAYER);
  });

  it('seeded rng repeats', () => {
    const a = mulberry32(42), b = mulberry32(42);
    expect([a(), a(), a()]).toEqual([b(), b(), b()]);
  });
});

describe('collision grid file (AC3)', () => {
  const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
  const campus = loadCampus();

  it('has a header matching campus.json and the expected size', () => {
    expect(grid.originX * 10).toBe(campus.bounds.minX);
    expect(grid.originZ * 10).toBe(campus.bounds.minZ);
    expect(grid.width * grid.height).toBeGreaterThan(0);
    expect(grid.bits.length).toBe(Math.ceil((grid.width * grid.height) / 8));
    expect(grid.chunkCells * grid.cellSize).toBe(campus.chunkSize);
  });

  it('blocks building interiors and is open somewhere', () => {
    let blockedHits = 0;
    for (const b of campus.buildings.slice(0, 40)) {
      // pick the centre of the footprint's bounding box inside check via any blocked cell near centroid
      let cx = 0, cz = 0;
      for (let i = 0; i < b.p.length; i += 2) { cx += b.p[i]; cz += b.p[i + 1]; }
      cx /= b.p.length / 2; cz /= b.p.length / 2;
      if (isBlocked(grid, cx / 10, cz / 10)) blockedHits++;
    }
    expect(blockedHits).toBeGreaterThan(10);
    expect(isBlocked(grid, grid.originX - 5, 0)).toBe(true);
    let open = 0;
    for (let x = -50; x < 50; x += 5) if (!isBlocked(grid, x, 0)) open++;
    expect(open).toBeGreaterThan(0);
  });
});

describe('attribution (AC4)', () => {
  it('adds visible OpenStreetMap credit text', () => {
    const added: { textContent: string }[] = [];
    const mk = () => ({ style: { cssText: '' }, children: [] as unknown[], textContent: '', appendChild(c: unknown) { this.children.push(c); }, remove() {} });
    const g = globalThis as unknown as { document?: unknown };
    const prev = g.document;
    g.document = { createElement: () => { const e = mk(); added.push(e); return e; }, body: mk() };
    try {
      showAttribution();
    } finally {
      g.document = prev;
    }
    expect(added.some((e) => e.textContent === '© OpenStreetMap contributors')).toBe(true);
  });
});

describe('housing areas on the map (M10, #523)', () => {
  const readJson = (p: string) => JSON.parse(readFileSync(p, 'utf8'));
  const config = readFileSync(join(mapDir, 'config.mjs'), 'utf8');
  const readme = readFileSync(join(mapDir, 'README.md'), 'utf8');
  const origin = { lat: 33.64525, lon: -117.8425 };
  const central = readJson(join(mapDir, 'source/osm-central.json')) as { ways: { id: number }[] };
  const housing = readJson(join(mapDir, 'source/osm-housing.json')) as { ways: { id: number; pts: [number, number][] }[] };
  const outlineIds = [171555779, 171559658];
  const connecting = [365551570];
  const centralIds = new Set(central.ways.map((w) => w.id));
  const project = ([lat, lon]: [number, number]): [number, number] => [
    Math.round((lon - origin.lon) * 111320 * Math.cos((origin.lat * Math.PI) / 180) * 10),
    Math.round(-(lat - origin.lat) * 111320 * 10),
  ];
  const outlines = outlineIds.map((id) => {
    const w = housing.ways.find((x) => x.id === id);
    if (!w) throw new Error(`outline ${id} missing from osm-housing.json`);
    return w.pts.map(project);
  });
  const inPoly = (poly: [number, number][], x: number, z: number) => {
    let r = false;
    for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
      if (poly[i][1] > z !== poly[j][1] > z && x < ((poly[j][0] - poly[i][0]) * (z - poly[i][1])) / (poly[j][1] - poly[i][1]) + poly[i][0]) r = !r;
    }
    return r;
  };
  const pts = (p: number[]) => Array.from({ length: p.length / 2 }, (_, i) => [p[2 * i], p[2 * i + 1]] as [number, number]);
  const wholly = (p: number[]) => outlines.some((o) => pts(p).every(([x, z]) => inPoly(o, x, z)));
  const hash = (a: number[]) => {
    let x = 2166136261;
    for (const v of a) { x ^= v; x = Math.imul(x, 16777619) >>> 0; }
    return x;
  };

  it('keeps central campus unchanged (AC1)', () => {
    const out = mkdtempSync(join(tmpdir(), 'map-c-'));
    build(out);
    for (const f of ['areas.json', 'cardtables.json', 'skills.json', 'props.json', 'parknature.json']) {
      expect(readFileSync(join(out, f)).equals(readFileSync(join(mapDir, f)))).toBe(true);
    }
    const campus = readJson(join(out, 'campus.json'));
    expect(campus.origin).toEqual(origin);
    const before = readJson(join(__dirname, 'fixtures/campus-central-hashes.json')) as { buildings: Record<string, number>; paths: Record<string, number> };
    const b = new Map<number, number>(campus.buildings.map((x: { id: number; h: number; p: number[] }) => [x.id, hash([x.h, ...x.p])]));
    const p = new Map<number, number>(campus.paths.map((x: { id: number; w: number; s: number; p: number[] }) => [x.id, hash([x.w, x.s, ...x.p])]));
    for (const [id, h] of Object.entries(before.buildings)) expect(b.get(Number(id))).toBe(h);
    for (const [id, h] of Object.entries(before.paths)) expect(p.get(Number(id))).toBe(h);
    expect(Object.keys(before.buildings).length).toBeGreaterThan(200);
  });

  it('adds the housing buildings and paths, and nothing else outside central campus (AC2)', () => {
    const campus = loadCampus();
    const addedB = campus.buildings.filter((x) => !centralIds.has(x.id));
    const addedP = campus.paths.filter((x) => !centralIds.has(x.id));
    expect(addedB.length).toBeGreaterThan(30);
    expect(addedP.length).toBeGreaterThan(50);
    for (const x of addedB) expect(wholly(x.p), `building ${x.id}`).toBe(true);
    for (const x of addedP) expect(wholly(x.p) || connecting.includes(x.id), `path ${x.id}`).toBe(true);
    for (const id of connecting) expect(addedP.some((x) => x.id === id)).toBe(true);
    for (const id of [...outlineIds, ...connecting]) {
      expect(config).toContain(String(id));
      expect(readme).toContain(String(id));
    }
  });

  it('has a walkable route from the spawn into each housing area (AC3)', () => {
    const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
    const areas = readJson(join(mapDir, 'areas.json'));
    const campus = loadCampus();
    const col = (x: number) => Math.floor((x - grid.originX) / grid.cellSize);
    const row = (z: number) => Math.floor((z - grid.originZ) / grid.cellSize);
    const seen = new Uint8Array(grid.width * grid.height);
    const stack = [row(areas.spawn.z / 10) * grid.width + col(areas.spawn.x / 10)];
    seen[stack[0]] = 1;
    while (stack.length) {
      const i = stack.pop() as number;
      const c = i % grid.width, r = (i - c) / grid.width;
      for (const [dc, dr] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
        const nc = c + dc, nr = r + dr;
        if (nc < 0 || nr < 0 || nc >= grid.width || nr >= grid.height) continue;
        const n = nr * grid.width + nc;
        if (seen[n] || isBlocked(grid, grid.originX + (nc + 0.5) * grid.cellSize, grid.originZ + (nr + 0.5) * grid.cellSize)) continue;
        seen[n] = 1;
        stack.push(n);
      }
    }
    outlines.forEach((o, k) => {
      // a vertex of a footway lying wholly inside this outline
      const path = campus.paths.find((x) => !centralIds.has(x.id) && pts(x.p).every(([px, pz]) => inPoly(o, px, pz)));
      expect(path, `housing path in area ${k}`).toBeDefined();
      const [px, pz] = pts((path as { p: number[] }).p)[0];
      expect(isBlocked(grid, px / 10, pz / 10)).toBe(false);
      expect(seen[row(pz / 10) * grid.width + col(px / 10)]).toBe(1);
    });
  });
});
