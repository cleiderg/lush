import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { loadCampus } from './campusMap';
import { GREEN_GRASS, GREEN_WATER, type CampusData } from './mapData';
import { FLOWERS_PER_M2, FLOWER_COLORS, FLOWER_VARIANT, GRASS_TUFTS_PER_M2, Nature, TUFT_VARIANTS, scatterNature, type ChunkScatter } from './nature';
import { WorldScene, countDrawCalls, countDrawCallsInView, countTrianglesInView } from './scene';

// Independent of nature.ts, so the test doesn't trust the code it checks. Decimetre polygons, metre points.
function inside(p: number[], x: number, z: number): boolean {
  let c = false;
  const n = p.length / 2;
  for (let i = 0; i < n; i++) {
    const j = (i + n - 1) % n;
    const [ax, az, bx, bz] = [p[2 * i] / 10, p[2 * i + 1] / 10, p[2 * j] / 10, p[2 * j + 1] / 10];
    if ((az > z) !== (bz > z) && x < ax + ((bx - ax) * (z - az)) / (bz - az)) c = !c;
  }
  return c;
}

function distToSegment(x: number, z: number, ax: number, az: number, bx: number, bz: number): number {
  const v = new THREE.Vector2(bx - ax, bz - az);
  const w = new THREE.Vector2(x - ax, z - az);
  const t = v.lengthSq() === 0 ? 0 : THREE.MathUtils.clamp(w.dot(v) / v.lengthSq(), 0, 1);
  return Math.hypot(ax + v.x * t - x, az + v.y * t - z);
}

function box(p: number[]): [number, number, number, number] {
  const xs = p.filter((_, i) => i % 2 === 0).map((v) => v / 10);
  const zs = p.filter((_, i) => i % 2 === 1).map((v) => v / 10);
  return [Math.min(...xs), Math.min(...zs), Math.max(...xs), Math.max(...zs)];
}

/** Items whose box covers a 16 m cell, so each check only looks at nearby ones. */
class Buckets<T extends { b: number[] }> {
  private readonly cells = new Map<string, T[]>();
  constructor(items: T[]) {
    for (const it of items) {
      for (let i = Math.floor(it.b[0] / 16); i <= Math.floor(it.b[2] / 16); i++) {
        for (let j = Math.floor(it.b[1] / 16); j <= Math.floor(it.b[3] / 16); j++) {
          const k = `${i},${j}`;
          if (!this.cells.has(k)) this.cells.set(k, []);
          this.cells.get(k)!.push(it);
        }
      }
    }
  }
  near(x: number, z: number): T[] {
    return this.cells.get(`${Math.floor(x / 16)},${Math.floor(z / 16)}`) ?? [];
  }
}

/** Every placed tuft and flower, checked against grass, paths, buildings and water. */
function checkPlacement(data: CampusData, chunks: ChunkScatter[]): number {
  const withBox = (list: { p: number[] }[]) => new Buckets(list.map((g) => ({ p: g.p, b: box(g.p) })));
  const grass = withBox(data.green.filter((g) => g.k === GREEN_GRASS));
  const water = withBox(data.green.filter((g) => g.k === GREEN_WATER));
  const buildings = withBox(data.buildings);
  const list: { a: number[]; half: number; b: number[] }[] = [];
  for (const w of data.paths) {
    for (let i = 0; i + 3 < w.p.length; i += 2) {
      const a = [w.p[i] / 10, w.p[i + 1] / 10, w.p[i + 2] / 10, w.p[i + 3] / 10];
      const half = w.w / 20;
      list.push({ a, half, b: [Math.min(a[0], a[2]) - half, Math.min(a[1], a[3]) - half, Math.max(a[0], a[2]) + half, Math.max(a[1], a[3]) + half] });
    }
  }
  const segs = new Buckets(list);
  const hit = (g: Buckets<{ p: number[]; b: number[] }>, x: number, z: number) => g.near(x, z).some((x2) => inside(x2.p, x, z));
  let n = 0;
  const bad: string[] = [];
  for (const c of chunks) {
    for (const t of [...c.tufts, ...c.flowers]) {
      if (!hit(grass, t.x, t.z)) bad.push(`off grass ${t.x},${t.z}`);
      if (hit(water, t.x, t.z)) bad.push(`in water ${t.x},${t.z}`);
      if (hit(buildings, t.x, t.z)) bad.push(`in a building ${t.x},${t.z}`);
      if (segs.near(t.x, t.z).some((s) => distToSegment(t.x, t.z, s.a[0], s.a[1], s.a[2], s.a[3]) <= s.half)) bad.push(`on a path ${t.x},${t.z}`);
      n++;
    }
  }
  expect(bad).toEqual([]);
  return n;
}

/** A 64 m chunk of grass crossed by a path, with a building and a pond on it. */
const small: CampusData = {
  version: 1,
  attribution: '',
  chunkSize: 64,
  bounds: { minX: 0, minZ: 0, maxX: 640, maxZ: 640 },
  green: [
    { id: 1, k: GREEN_GRASS, p: [0, 0, 600, 0, 600, 600, 0, 600] },
    { id: 2, k: GREEN_WATER, p: [400, 400, 550, 400, 550, 550, 400, 550] },
  ],
  buildings: [{ id: 3, h: 80, p: [50, 50, 200, 50, 200, 200, 50, 200] }],
  paths: [{ id: 4, w: 40, p: [0, 300, 640, 320] }],
};

describe('grass tufts and flowers placement (AC1)', () => {
  it('places them only on grass, off paths, buildings and water, on the campus', () => {
    const data = loadCampus();
    const n = checkPlacement(data, scatterNature(data));
    expect(n).toBeGreaterThan(10_000);
  });

  it('keeps clear of a path, a building and a pond on a small map', () => {
    const chunks = scatterNature(small, { tuftsPerM2: 4, flowersPerM2: 1 });
    const n = checkPlacement(small, chunks);
    expect(n).toBeGreaterThan(5000);
    // Nothing outside the grass square either.
    for (const t of chunks.flatMap((c) => c.tufts)) expect(t.x <= 60 && t.z <= 60).toBe(true);
  });

  it('gives the same placement for a fixed seed, and a different one for another seed', () => {
    const data = loadCampus();
    const a = JSON.stringify(scatterNature(data, { seed: 7, tuftsPerM2: 0.1, flowersPerM2: 0.01 }));
    // Cleared caches: a fresh copy of the data must give the same result.
    const b = JSON.stringify(scatterNature({ ...data }, { seed: 7, tuftsPerM2: 0.1, flowersPerM2: 0.01 }));
    const c = JSON.stringify(scatterNature({ ...data }, { seed: 8, tuftsPerM2: 0.1, flowersPerM2: 0.01 }));
    expect(b).toBe(a);
    expect(c).not.toBe(a);
  });
});

const meshes = (n: Nature) => n.group.children as THREE.InstancedMesh[];
const variants = (m: THREE.InstancedMesh) => Array.from((m.geometry.getAttribute('variant') as THREE.InstancedBufferAttribute).array);

describe('grass tufts and flowers drawing (AC2)', () => {
  it('draws them with one InstancedMesh per map chunk', () => {
    const data = loadCampus();
    const nature = new Nature(data);
    const chunkM = data.chunkSize;
    expect(nature.group.children.length).toBe(scatterNature(data).length);
    const names = new Set<string>();
    for (const m of meshes(nature)) {
      expect(m.isInstancedMesh).toBe(true);
      names.add(m.name);
      const [cx, cz] = m.name.split('-')[1].split(',').map(Number);
      const x0 = data.bounds.minX / 10 + cx * chunkM, z0 = data.bounds.minZ / 10 + cz * chunkM;
      const mat = new THREE.Matrix4(), p = new THREE.Vector3();
      for (let i = 0; i < m.count; i++) {
        p.setFromMatrixPosition((m.getMatrixAt(i, mat), mat));
        expect(p.x >= x0 && p.x <= x0 + chunkM && p.z >= z0 && p.z <= z0 + chunkM).toBe(true);
      }
    }
    expect(names.size).toBe(nature.group.children.length);
    console.log(`nature: ${nature.stats.tufts} tufts, ${nature.stats.flowers} flowers in ${nature.stats.chunks} chunks`);
  });

  it('keeps the draw calls the same when the density doubles', () => {
    const data = loadCampus();
    const base = new Nature(data);
    const dense = new Nature(data, { tuftsPerM2: GRASS_TUFTS_PER_M2 * 2, flowersPerM2: FLOWERS_PER_M2 * 2 });
    expect(dense.stats.tufts).toBeGreaterThan(base.stats.tufts * 1.9);
    expect(countDrawCalls(dense.group)).toBe(countDrawCalls(base.group));
    expect(countDrawCalls(dense.group)).toBe(base.stats.chunks);
    // And the map as a whole, with the camera in the same place.
    const at = (n: Nature) => {
      const w = new WorldScene();
      w.scene.remove(w.nature.group);
      w.scene.add(n.group);
      w.update({ local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [] });
      n.cull(w.rig.camera);
      return [countDrawCalls(w.scene), countDrawCallsInView(w.scene, w.rig.camera)];
    };
    expect(at(dense)).toEqual(at(base));
  });

  it('culls the chunks off screen', () => {
    const w = new WorldScene();
    w.update({ local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [] });
    w.rig.camera.aspect = 9 / 19.5;
    w.rig.camera.updateProjectionMatrix();
    w.nature.cull(w.rig.camera);
    const inView = countDrawCallsInView(w.nature.group, w.rig.camera);
    expect(inView).toBeGreaterThan(0);
    expect(inView).toBeLessThanOrEqual(4);
    console.log(`nature in view: ${inView} draw calls, ${countTrianglesInView(w.nature.group, w.rig.camera)} triangles`);
    // Far away, other chunks show and these ones are hidden.
    const before = meshes(w.nature).map((m) => m.visible);
    w.update({ local: { id: 0, x: 200, z: 300, heading: 0 }, remotes: [] });
    expect(meshes(w.nature).map((m) => m.visible)).not.toEqual(before);
  });

  it('culls again for the camera that renders', () => {
    const w = new WorldScene();
    w.update({ local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [] });
    const cam = w.rig.camera.clone();
    cam.position.set(5000, 10, 5000);
    w.scene.onBeforeRender(undefined as never, w.scene, cam, undefined as never, undefined as never, undefined as never);
    expect(meshes(w.nature).every((m) => !m.visible)).toBe(true);
  });
});

describe('grass tufts and flowers density (AC3)', () => {
  it('uses the PRD 13 (Q2) placeholder densities', () => {
    expect(GRASS_TUFTS_PER_M2).toBe(0.5);
    expect(FLOWERS_PER_M2).toBe(0.05);
  });

  it('halves the instance count when the densities halve', () => {
    const full = new Nature(loadCampus());
    const half = new Nature(loadCampus(), { tuftsPerM2: GRASS_TUFTS_PER_M2 / 2, flowersPerM2: FLOWERS_PER_M2 / 2 });
    const count = (n: Nature) => meshes(n).reduce((s, m) => s + m.count, 0);
    expect(count(full)).toBe(full.stats.tufts + full.stats.flowers);
    expect(count(half) / count(full)).toBeGreaterThan(0.5 * 0.95);
    expect(count(half) / count(full)).toBeLessThan(0.5 * 1.05);
    const flowers = (n: Nature) => meshes(n).reduce((s, m) => s + variants(m).filter((v) => v === FLOWER_VARIANT).length, 0);
    expect(flowers(half) / flowers(full)).toBeGreaterThan(0.5 * 0.95);
    expect(flowers(half) / flowers(full)).toBeLessThan(0.5 * 1.05);
  });
});

describe('grass tufts and flowers variety (AC4)', () => {
  it('uses at least 2 tuft shapes and 3 flower colours', () => {
    const nature = new Nature(loadCampus());
    expect(TUFT_VARIANTS).toBeGreaterThanOrEqual(2);
    const used = new Set(meshes(nature).flatMap(variants));
    for (let v = 0; v < TUFT_VARIANTS; v++) expect(used).toContain(v);
    expect(used).toContain(FLOWER_VARIANT);
    // Each tuft shape is its own set of triangles in the shared geometry.
    const part = meshes(nature)[0].geometry.getAttribute('part');
    const tris = (v: number) => Array.from(part.array).filter((p) => p === v).length / 3;
    expect(tris(0)).toBeGreaterThan(0);
    expect(tris(1)).toBeGreaterThan(0);
    expect(tris(0)).not.toBe(tris(1));
    const colours = new Set<number>();
    const c = new THREE.Color();
    for (const m of meshes(nature)) {
      variants(m).forEach((v, i) => {
        if (v === FLOWER_VARIANT) colours.add((m.getColorAt(i, c), c.getHex()));
      });
    }
    expect(colours.size).toBeGreaterThanOrEqual(3);
    for (const h of colours) expect(FLOWER_COLORS).toContain(h);
  });

  it('gives each instance a seeded random rotation and scale', () => {
    const nature = new Nature(loadCampus());
    for (const m of meshes(nature)) {
      if (m.count < 2) continue;
      // Compare the rotation and scale part only: the positions differ anyway.
      const linear = new Set<string>();
      const scales = new Set<number>();
      const mat = new THREE.Matrix4();
      for (let i = 0; i < m.count; i++) {
        m.getMatrixAt(i, mat);
        linear.add(mat.elements.slice(0, 11).map((v) => v.toFixed(4)).join());
        scales.add(+mat.elements[5].toFixed(4));
      }
      expect(linear.size).toBeGreaterThan(1);
      expect(scales.size).toBeGreaterThan(1);
    }
    const all = scatterNature(loadCampus()).flatMap((c) => c.tufts);
    expect(new Set(all.map((t) => t.rot)).size).toBeGreaterThan(all.length * 0.95);
    expect(new Set(all.map((t) => t.scale)).size).toBeGreaterThan(all.length * 0.95);
  });
});
