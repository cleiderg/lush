import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { CampusMap, loadCampus } from './campusMap';
import { EDGE_BAND, GROUND_DIRT, GROUND_KINDS, GROUND_PAVED, GROUND_PLAZA, GROUND_STONE } from './groundSurfaces';
import { SURFACE_DIRT, SURFACE_PAVED, SURFACE_STONE, type CampusData } from './mapData';
import { countDrawCalls } from './scene';

const mapDir = resolve(__dirname, '../../../shared/map');
const build = (out: string) => execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });
const sha256 = (buf: Buffer) => createHash('sha256').update(buf).digest('hex');

describe('path surfaces and plazas in the map build (AC1)', () => {
  const out = [mkdtempSync(join(tmpdir(), 'surf-a-')), mkdtempSync(join(tmpdir(), 'surf-b-'))];
  for (const d of out) build(d);
  const campus = JSON.parse(readFileSync(join(out[0], 'campus.json'), 'utf8')) as CampusData;

  it('is deterministic: two runs give byte-identical output, matching the committed file', () => {
    for (const f of ['campus.json', 'collision.bin', 'areas.json', 'cardtables.json']) {
      expect(readFileSync(join(out[0], f)).equals(readFileSync(join(out[1], f)))).toBe(true);
    }
    expect(readFileSync(join(out[0], 'campus.json')).equals(readFileSync(join(mapDir, 'campus.json')))).toBe(true);
  });

  it('gives each path a surface kind from its highway tag, and writes pedestrian areas as plazas', () => {
    const src = JSON.parse(readFileSync(join(mapDir, 'source/osm-central.json'), 'utf8')) as {
      ways: { id: number; tags: Record<string, string>; pts: number[][] }[];
    };
    const tags = new Map(src.ways.map((w) => [w.id, w.tags]));
    const expected = (h: string) =>
      h === 'path' ? SURFACE_DIRT : ['footway', 'steps', 'cycleway', 'pedestrian'].includes(h) ? SURFACE_STONE : SURFACE_PAVED;
    const seen = new Set<number>();
    for (const p of campus.paths) {
      const t = tags.get(p.id)!;
      expect(p.s, `path ${p.id} (${t.highway})`).toBe(expected(t.highway));
      expect(t.highway === 'pedestrian' && t.area === 'yes').toBe(false);
      seen.add(p.s);
    }
    expect([...seen].sort()).toEqual([SURFACE_DIRT, SURFACE_STONE, SURFACE_PAVED]);
    const areaIds = src.ways
      .filter((w) => w.tags.highway === 'pedestrian' && w.tags.area === 'yes')
      .filter((w) => w.pts.length > 3 && w.pts[0].join() === w.pts[w.pts.length - 1].join())
      .map((w) => w.id);
    expect(areaIds.length).toBeGreaterThan(0);
    expect(campus.plazas.map((p) => p.id).sort()).toEqual(areaIds.sort());
  });

  it('writes every plaza as a closed polygon with at least 3 points', () => {
    expect(campus.plazas.length).toBeGreaterThan(0);
    for (const pl of campus.plazas) {
      expect(pl.p.length % 2).toBe(0);
      expect(pl.p.length).toBeGreaterThanOrEqual(6);
      for (const v of pl.p) expect(Number.isInteger(v)).toBe(true);
      // Closed by convention: last point not repeated, and the ring encloses an area.
      expect([pl.p[0], pl.p[1]]).not.toEqual(pl.p.slice(-2));
      let area = 0;
      for (let i = 0, n = pl.p.length / 2, j = n - 1; i < n; j = i++) {
        area += pl.p[2 * j] * pl.p[2 * i + 1] - pl.p[2 * i] * pl.p[2 * j + 1];
      }
      expect(Math.abs(area)).toBeGreaterThan(0);
    }
  });
});

describe('collision and areas unchanged (AC2)', () => {
  // SHA-256 of the files as committed before path surfaces were added (#379).
  const BEFORE: Record<string, string> = {
    'collision.bin': 'e9c7039c59482b687c4135a03cf9f3def25b1bf3aad51297dac85edad82dfedc',
    'areas.json': 'f1c864b4121ad2e40e7ff03c1a2d46204dea17c6abbaacecaed5e260528a2428',
    'cardtables.json': '4b50c37498188920ab254d2bd62389dac46482cc442acd471718c42d88ce2ee9',
  };
  it('builds collision.bin, areas.json and cardtables.json byte-identical to before', () => {
    const out = mkdtempSync(join(tmpdir(), 'surf-c-'));
    build(out);
    for (const [f, hash] of Object.entries(BEFORE)) {
      expect(sha256(readFileSync(join(out, f))), f).toBe(hash);
      expect(sha256(readFileSync(join(mapDir, f))), f).toBe(hash);
    }
  });
});

/** A one-chunk map with `n` paths of each surface and `n` plazas, all inside the first 64 m chunk. */
function smallMap(n: number): CampusData {
  const paths = [];
  const plazas = [];
  for (let i = 0; i < n; i++) {
    for (const s of [SURFACE_DIRT, SURFACE_STONE, SURFACE_PAVED]) {
      const z = 20 + ((i * 3 + s) % 400);
      paths.push({ id: 1000 + i * 3 + s, w: 20 + s * 10, s, p: [20, z, 300, z + 37, 600, z - 5] });
    }
    const x = 20 + (i * 13) % 500;
    plazas.push({ id: 5000 + i, p: [x, 450, x + 60, 450, x + 80, 520, x, 560] });
  }
  return {
    version: 1,
    attribution: '',
    chunkSize: 64,
    bounds: { minX: 0, minZ: 0, maxX: 640, maxZ: 640 },
    buildings: [{ id: 1, h: 60, p: [500, 500, 600, 500, 600, 600, 500, 600] }],
    paths,
    plazas,
    green: [{ id: 2, k: 0, p: [0, 0, 630, 0, 630, 630, 0, 630] }],
  };
}

function meshes(root: THREE.Object3D): THREE.Mesh[] {
  const out: THREE.Mesh[] = [];
  root.traverse((o) => { if ((o as THREE.Mesh).isMesh) out.push(o as THREE.Mesh); });
  return out;
}

describe('ground surface materials (AC3)', () => {
  it('draws grass, dirt, stone, paved paths and plazas with distinct materials', () => {
    const m = new CampusMap(smallMap(2));
    const used = new Set(meshes(m.group).map((x) => x.material as THREE.Material));
    expect(used.size).toBeGreaterThanOrEqual(5);
    const kinds = [GROUND_DIRT, GROUND_STONE, GROUND_PAVED, GROUND_PLAZA];
    const mats = kinds.map((k) => m.surfaceMaterials[k]);
    expect(new Set([m.material, ...mats]).size).toBe(5);
    for (const mat of mats) {
      expect(used.has(mat)).toBe(true);
      expect(mat.map).toBeInstanceOf(THREE.DataTexture);
      expect(mat.map!.wrapS).toBe(THREE.RepeatWrapping);
      expect(mat.map!.image.width).toBeLessThanOrEqual(64);
    }
    // Each texture has its own look.
    const pixels = mats.map((mat) => Buffer.from((mat.map as THREE.DataTexture).image.data as Uint8Array).toString('hex'));
    expect(new Set(pixels).size).toBe(4);
    expect(m.material.map).toBeNull();
  });

  it('uses all ground materials on the campus map', () => {
    const m = new CampusMap(loadCampus());
    const used = new Set(meshes(m.group).map((x) => x.material));
    for (const k of GROUND_KINDS) expect(used.has(m.surfaceMaterials[k])).toBe(true);
    expect(used.has(m.material)).toBe(true);
  });
});

interface Tri { a: number[]; b: number[]; c: number[] } // [x, z, alpha]
function triangles(mesh: THREE.Mesh): Tri[] {
  const pos = mesh.geometry.getAttribute('position');
  const col = mesh.geometry.getAttribute('color');
  expect(col.itemSize).toBe(4);
  const v = (i: number) => [pos.getX(i), pos.getZ(i), col.getW(i)];
  const out: Tri[] = [];
  for (let i = 0; i < pos.count; i += 3) out.push({ a: v(i), b: v(i + 1), c: v(i + 2) });
  return out;
}
/** Interpolated alpha at (x, z) if the point is inside the triangle, else null. */
function alphaAt(t: Tri, x: number, z: number): number | null {
  const { a, b, c } = t;
  const d = (b[1] - c[1]) * (a[0] - c[0]) + (c[0] - b[0]) * (a[1] - c[1]);
  if (Math.abs(d) < 1e-12) return null;
  const l1 = ((b[1] - c[1]) * (x - c[0]) + (c[0] - b[0]) * (z - c[1])) / d;
  const l2 = ((c[1] - a[1]) * (x - c[0]) + (a[0] - c[0]) * (z - c[1])) / d;
  const l3 = 1 - l1 - l2;
  const e = -1e-6;
  if (l1 < e || l2 < e || l3 < e) return null;
  return l1 * a[2] + l2 * b[2] + l3 * c[2];
}
const faded = (tris: Tri[], x: number, z: number) =>
  tris.some((t) => {
    const a = alphaAt(t, x, z);
    return a !== null && a > 0.05 && a < 0.95;
  });

/** Checks a soft edge on both sides of every path segment and outside every plaza edge. */
function checkSoftEdges(data: CampusData): number {
  const m = new CampusMap(data);
  const cache = new Map<string, Tri[]>();
  const tris = (xDm: number, zDm: number, kind: number) => {
    const cx = Math.floor(((xDm - data.bounds.minX) / 10) / data.chunkSize);
    const cz = Math.floor(((zDm - data.bounds.minZ) / 10) / data.chunkSize);
    const name = `ground-${cx},${cz}-${kind}`;
    if (!cache.has(name)) cache.set(name, triangles(m.group.getObjectByName(name) as THREE.Mesh));
    return cache.get(name)!;
  };
  let checked = 0;
  for (const w of data.paths) {
    const half = w.w / 20;
    for (let i = 0; i + 3 < w.p.length; i += 2) {
      const [x0, z0, x1, z1] = w.p.slice(i, i + 4);
      const t = tris((x0 + x1) / 2, (z0 + z1) / 2, w.s);
      const len = Math.hypot(x1 - x0, z1 - z0);
      const nx = -(z1 - z0) / len, nz = (x1 - x0) / len;
      const mx = (x0 + x1) / 20, mz = (z0 + z1) / 20;
      for (const side of [1, -1]) {
        const off = side * (half + EDGE_BAND / 2);
        expect(faded(t, mx + nx * off, mz + nz * off), `path ${w.id} segment ${i / 2} side ${side}`).toBe(true);
      }
      checked++;
    }
  }
  for (const pl of data.plazas) {
    let x0 = Infinity, x1 = -Infinity, z0 = Infinity, z1 = -Infinity;
    for (let i = 0; i < pl.p.length; i += 2) {
      x0 = Math.min(x0, pl.p[i]); x1 = Math.max(x1, pl.p[i]);
      z0 = Math.min(z0, pl.p[i + 1]); z1 = Math.max(z1, pl.p[i + 1]);
    }
    const t = tris((x0 + x1) / 2, (z0 + z1) / 2, GROUND_PLAZA);
    const n = pl.p.length / 2;
    let area = 0;
    for (let i = 0, j = n - 1; i < n; j = i++) area += pl.p[2 * j] * pl.p[2 * i + 1] - pl.p[2 * i] * pl.p[2 * j + 1];
    const out = area > 0 ? 1 : -1;
    for (let i = 0; i < n; i++) {
      const ax = pl.p[2 * i] / 10, az = pl.p[2 * i + 1] / 10;
      const bx = pl.p[(2 * i + 2) % pl.p.length] / 10, bz = pl.p[(2 * i + 3) % pl.p.length] / 10;
      const len = Math.hypot(bx - ax, bz - az);
      const ox = ((bz - az) / len) * out * (EDGE_BAND / 2), oz = (-(bx - ax) / len) * out * (EDGE_BAND / 2);
      expect(faded(t, (ax + bx) / 2 + ox, (az + bz) / 2 + oz), `plaza ${pl.id} edge ${i}`).toBe(true);
      checked++;
    }
  }
  return checked;
}

describe('soft edges against the grass (AC4)', () => {
  it('fades both sides of every path segment and every plaza edge in a small map', () => {
    expect(checkSoftEdges(smallMap(3))).toBe(3 * 3 * 2 + 3 * 4);
  });

  it('fades both sides of every path segment and every plaza edge on the campus map', () => {
    const c = loadCampus();
    const segments = c.paths.reduce((n, w) => n + w.p.length / 2 - 1, 0);
    const edges = c.plazas.reduce((n, p) => n + p.p.length / 2, 0);
    expect(checkSoftEdges(c)).toBe(segments + edges);
  });

  it('is opaque on the path itself and fully transparent at the outer edge of the band', () => {
    const m = new CampusMap(smallMap(1));
    const path = smallMap(1).paths[0];
    const t = triangles(m.group.getObjectByName(`ground-0,0-${path.s}`) as THREE.Mesh);
    const alphas = new Set(t.flatMap((x) => [x.a[2], x.b[2], x.c[2]]));
    expect([...alphas].sort()).toEqual([0, 1]);
    const mat = m.surfaceMaterials[path.s];
    expect(mat.transparent).toBe(true);
    expect(mat.vertexColors).toBe(true);
    expect(mat.depthWrite).toBe(false);
  });
});

describe('draw calls per chunk (AC5)', () => {
  const PER_CHUNK = 1 + GROUND_KINDS.length;

  it('does not grow with the number of paths and plazas in a chunk', () => {
    const few = new CampusMap(smallMap(1));
    const many = new CampusMap(smallMap(60));
    expect(few.stats.chunks).toBe(1);
    expect(many.stats.chunks).toBe(1);
    expect(many.stats.triangles).toBeGreaterThan(few.stats.triangles * 10);
    expect(countDrawCalls(many.group)).toBe(countDrawCalls(few.group));
    expect(countDrawCalls(many.group)).toBeLessThanOrEqual(PER_CHUNK);
  });

  it(`is at most ${PER_CHUNK} per chunk on the campus map`, () => {
    const m = new CampusMap(loadCampus());
    for (const chunk of m.group.children) expect(countDrawCalls(chunk)).toBeLessThanOrEqual(PER_CHUNK);
    const calls = countDrawCalls(m.group);
    console.log(`map: ${calls} draw calls over ${m.stats.chunks} chunks (if all visible)`);
    expect(calls).toBeLessThanOrEqual(PER_CHUNK * m.stats.chunks);
  });
});
