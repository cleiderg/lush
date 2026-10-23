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
