import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { showAttribution } from './attribution';
import { CampusMap, loadCampus } from './campusMap';
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
