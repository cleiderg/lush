import * as THREE from 'three';
import { GREEN_GRASS, GREEN_WATER, type CampusData } from './mapData';
import { mulberry32 } from './rng';

/** Grass tufts per square metre of grass. Placeholder until profiled on an iPhone 13 (PRD 13, Q2). */
export const GRASS_TUFTS_PER_M2 = 0.5;
/** Small flowers per square metre of grass. Placeholder until profiled on an iPhone 13 (PRD 13, Q2). */
export const FLOWERS_PER_M2 = 0.05;
/** Seed for the scatter, so every client places the same tufts. */
export const NATURE_SEED = 0x6e617475;
/** Extra clearance (metres) past a path's edge, so blades don't poke onto it. */
const PATH_MARGIN = 0.3;
/** Probes per chunk that decide whether it has any grass for tufts (see scatterNature). */
const ANCHOR_PROBES = 4096;
const MIN_SCALE = 0.7;
const SCALE_RANGE = 0.6;

const TUFT_BASE = 0x3f7a2e;
const TUFT_TIP = 0x8ccb5e;
const STEM = 0x3d7a33;
/** White, pink, lavender and red, like the dots of flowers on the day reference's grass. */
export const FLOWER_COLORS = [0xf4f1ea, 0xe88fb4, 0xa98ad6, 0xd9534f];

export interface NatureOptions {
  tuftsPerM2?: number;
  flowersPerM2?: number;
  seed?: number;
  /** More polygons to keep clear (decimetres, like campus.json), e.g. the card table plaza. */
  exclude?: number[][];
}

/** One placed tuft or flower. Metres; `kind` is the tuft shape or the flower colour index. */
export interface Scatter { x: number; z: number; rot: number; scale: number; kind: number }
export interface ChunkScatter { cx: number; cz: number; tufts: Scatter[]; flowers: Scatter[] }

type Box = [number, number, number, number];
interface Poly { p: number[]; box: Box }
interface Seg { x0: number; z0: number; x1: number; z1: number; r2: number; box: Box }

function bbox(p: number[], pad = 0): Box {
  const b: Box = [Infinity, Infinity, -Infinity, -Infinity];
  for (let i = 0; i < p.length; i += 2) {
    b[0] = Math.min(b[0], p[i]); b[1] = Math.min(b[1], p[i + 1]);
    b[2] = Math.max(b[2], p[i]); b[3] = Math.max(b[3], p[i + 1]);
  }
  return [b[0] - pad, b[1] - pad, b[2] + pad, b[3] + pad];
}

const overlaps = (a: Box, b: Box): boolean => a[0] <= b[2] && b[0] <= a[2] && a[1] <= b[3] && b[1] <= a[3];
const inBox = (b: Box, x: number, z: number): boolean => x >= b[0] && x <= b[2] && z >= b[1] && z <= b[3];

/** Even-odd point in polygon; `p` is a closed polygon [x0,z0,x1,z1,...]. */
export function pointInPolygon(p: number[], x: number, z: number): boolean {
  let inside = false;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    const xi = p[i], zi = p[i + 1], xj = p[j], zj = p[j + 1];
    if (zi > z !== zj > z && x < ((xj - xi) * (z - zi)) / (zj - zi) + xi) inside = !inside;
  }
  return inside;
}

/** Squared distance from (x, z) to the segment. */
export function segmentDistance2(x: number, z: number, x0: number, z0: number, x1: number, z1: number): number {
  const dx = x1 - x0, dz = z1 - z0;
  const len2 = dx * dx + dz * dz;
  const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, ((x - x0) * dx + (z - z0) * dz) / len2));
  const ex = x0 + t * dx - x, ez = z0 + t * dz - z;
  return ex * ex + ez * ez;
}

const polys = (list: { p: number[] }[]): Poly[] => list.map((g) => ({ p: g.p, box: bbox(g.p) }));
const hit = (list: Poly[], x: number, z: number): boolean => list.some((g) => inBox(g.box, x, z) && pointInPolygon(g.p, x, z));

function chunkSeed(seed: number, salt: number, cx: number, cz: number): number {
  return seed ^ Math.imul(cx + 1, 73856093) ^ Math.imul(cz + 1, 19349663) ^ Math.imul(salt, 83492791);
}

const cache = new WeakMap<CampusData, Map<string, ChunkScatter[]>>();

/**
 * Seeded tuft and flower positions, per map chunk. Candidates are spread uniformly over each chunk
 * and kept only on grass (green k=0), off paths (within half their width plus a margin), plazas,
 * buildings and water. Every candidate draws the same number of random values, so a lower density keeps a
 * prefix of the same candidates.
 */
export function scatterNature(data: CampusData, opts: NatureOptions = {}): ChunkScatter[] {
  const tuftsPerM2 = opts.tuftsPerM2 ?? GRASS_TUFTS_PER_M2;
  const flowersPerM2 = opts.flowersPerM2 ?? FLOWERS_PER_M2;
  const seed = opts.seed ?? NATURE_SEED;
  const exclude = opts.exclude ?? [];
  const key = `${tuftsPerM2}/${flowersPerM2}/${seed}/${JSON.stringify(exclude)}`;
  let byKey = cache.get(data);
  if (!byKey) cache.set(data, (byKey = new Map()));
  const cached = byKey.get(key);
  if (cached) return cached;

  // Everything below is in decimetres, like campus.json.
  const chunkDm = data.chunkSize * 10;
  const { minX, minZ, maxX, maxZ } = data.bounds;
  const grass = polys(data.green.filter((g) => g.k === GREEN_GRASS));
  const water = polys(data.green.filter((g) => g.k === GREEN_WATER));
  // Buildings, paved plazas and any extra polygons are all kept clear.
  const buildings = polys([...data.buildings, ...data.plazas, ...exclude.map((p) => ({ p }))]);
  const segs: Seg[] = [];
  for (const w of data.paths) {
    const r = w.w / 2 + PATH_MARGIN * 10;
    for (let i = 0; i + 3 < w.p.length; i += 2) {
      const s = [w.p[i], w.p[i + 1], w.p[i + 2], w.p[i + 3]];
      segs.push({ x0: s[0], z0: s[1], x1: s[2], z1: s[3], r2: r * r, box: bbox(s, r) });
    }
  }

  const out: ChunkScatter[] = [];
  const nx = Math.ceil((maxX - minX) / chunkDm), nz = Math.ceil((maxZ - minZ) / chunkDm);
  for (let cz = 0; cz < nz; cz++) {
    for (let cx = 0; cx < nx; cx++) {
      const box: Box = [minX + cx * chunkDm, minZ + cz * chunkDm, minX + (cx + 1) * chunkDm, minZ + (cz + 1) * chunkDm];
      const g = grass.filter((x) => overlaps(x.box, box));
      if (g.length === 0) continue;
      const w = water.filter((x) => overlaps(x.box, box));
      const b = buildings.filter((x) => overlaps(x.box, box));
      const s = segs.filter((x) => overlaps(x.box, box));
      const ok = (x: number, z: number): boolean =>
        hit(g, x, z) && !hit(w, x, z) && !hit(b, x, z) &&
        !s.some((q) => inBox(q.box, x, z) && segmentDistance2(x, z, q.x0, q.z0, q.x1, q.z1) < q.r2);
      const sample = (n: number, salt: number, kinds: number, max = Infinity): Scatter[] => {
        const rnd = mulberry32(chunkSeed(seed, salt, cx, cz));
        const res: Scatter[] = [];
        for (let i = 0; i < n && res.length < max; i++) {
          const x = box[0] + rnd() * chunkDm, z = box[1] + rnd() * chunkDm;
          const rot = rnd() * Math.PI * 2, scale = MIN_SCALE + rnd() * SCALE_RANGE, kind = Math.floor(rnd() * kinds);
          if (ok(x, z)) res.push({ x: x / 10, z: z / 10, rot, scale, kind });
        }
        return res;
      };
      // Which chunks have tufts, and so the meshes and draw calls, must not depend on the density:
      // a chunk is used only when a fixed set of probes finds grass, and then has at least one
      // tuft, at the first probe hit.
      const anchor = sample(ANCHOR_PROBES, 3, 1, 1);
      if (anchor.length === 0) continue;
      const area = data.chunkSize * data.chunkSize;
      const tufts = sample(Math.round(area * tuftsPerM2), 1, TUFT_SHAPES.length);
      const flowers = sample(Math.round(area * flowersPerM2), 2, FLOWER_COLORS.length);
      if (tufts.length === 0) tufts.push(anchor[0]);
      out.push({ cx, cz, tufts, flowers });
    }
  }
  byKey.set(key, out);
  return out;
}

/** Blades as [angle, lean, height, half width]; each blade is one triangle from the base to its tip. */
type Blade = [number, number, number, number];
const TUFT_SHAPES: Blade[][] = [
  // Tall and narrow: three blades.
  [[0.3, 0.16, 0.46, 0.05], [2.4, 0.14, 0.4, 0.05], [4.4, 0.17, 0.5, 0.05]],
  // Short and spread: five blades.
  [[0, 0.22, 0.3, 0.045], [1.3, 0.24, 0.25, 0.045], [2.5, 0.2, 0.34, 0.045], [3.8, 0.23, 0.27, 0.045], [5.1, 0.22, 0.32, 0.045]],
];

/** Instance variants: one per tuft shape, then the flower. */
export const TUFT_VARIANTS = TUFT_SHAPES.length;
export const FLOWER_VARIANT = TUFT_SHAPES.length;
/** Height (metres) of the culling box over each chunk's tufts and flowers. */
const CULL_HEIGHT = 0.6;

/** Accumulates vertices tagged with the variant (`part`) they belong to. */
class Builder {
  readonly pos: number[] = [];
  readonly col: number[] = [];
  readonly part: number[] = [];
  private readonly c = new THREE.Color();

  vertex(part: number, x: number, y: number, z: number, color: number): void {
    this.c.setHex(color);
    this.pos.push(x, y, z);
    this.col.push(this.c.r, this.c.g, this.c.b);
    this.part.push(part);
  }

  /** Normals point up, so tufts and flowers shade like the ground under them. */
  geometry(): THREE.BufferGeometry {
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(this.pos, 3));
    g.setAttribute('color', new THREE.Float32BufferAttribute(this.col, 3));
    const n = new Float32Array(this.pos.length);
    for (let i = 1; i < n.length; i += 3) n[i] = 1;
    g.setAttribute('normal', new THREE.BufferAttribute(n, 3));
    g.setAttribute('part', new THREE.Float32BufferAttribute(this.part, 1));
    g.computeBoundingSphere();
    return g;
  }
}

/**
 * Every tuft shape and the flower in one geometry. Each instance shows only the part matching its
 * `variant`, so a whole chunk of mixed tufts and flowers is a single InstancedMesh and draw call.
 */
export function natureGeometry(): THREE.BufferGeometry {
  const b = new Builder();
  TUFT_SHAPES.forEach((blades, part) => {
    for (const [a, lean, h, hw] of blades) {
      const dx = Math.cos(a), dz = Math.sin(a);
      const bx = dx * 0.03, bz = dz * 0.03;
      b.vertex(part, bx - dz * hw, 0, bz + dx * hw, TUFT_BASE);
      b.vertex(part, bx + dz * hw, 0, bz - dx * hw, TUFT_BASE);
      b.vertex(part, dx * lean, h, dz * lean, TUFT_TIP);
    }
  });
  // A stem and a five-petal head; the white petals take the instance colour.
  const h = 0.22, r = 0.09;
  b.vertex(FLOWER_VARIANT, -0.015, 0, 0, STEM);
  b.vertex(FLOWER_VARIANT, 0.015, 0, 0, STEM);
  b.vertex(FLOWER_VARIANT, 0, h, 0, STEM);
  for (let i = 0; i < 5; i++) {
    const a0 = (i / 5) * Math.PI * 2, a1 = ((i + 1) / 5) * Math.PI * 2;
    b.vertex(FLOWER_VARIANT, 0, h + 0.01, 0, 0xffffff);
    b.vertex(FLOWER_VARIANT, Math.cos(a0) * r, h, Math.sin(a0) * r, 0xffffff);
    b.vertex(FLOWER_VARIANT, Math.cos(a1) * r, h, Math.sin(a1) * r, 0xffffff);
  }
  return b.geometry();
}

/** Lambert, with vertices of the parts an instance doesn't show collapsed to a point. */
function natureMaterial(): THREE.MeshLambertMaterial {
  const m = new THREE.MeshLambertMaterial({ vertexColors: true, side: THREE.DoubleSide });
  m.onBeforeCompile = (shader) => {
    shader.vertexShader = shader.vertexShader
      .replace('#include <common>', '#include <common>\nattribute float part;\nattribute float variant;')
      .replace('#include <begin_vertex>', '#include <begin_vertex>\nif (abs(part - variant) > 0.5) transformed = vec3(0.0);');
    // Both sides of a blade face up, instead of the back side being flipped into the dark.
    shader.fragmentShader = shader.fragmentShader
      .replace('#include <normal_fragment_begin>', '#include <normal_fragment_begin>\nnormal = normalize(vNormal);');
  };
  m.customProgramCacheKey = () => 'nature';
  return m;
}

export interface NatureStats { chunks: number; tufts: number; flowers: number }

/** One map chunk's instances: a range of the source arrays, and a tight box for culling. */
export interface NatureChunk { cx: number; cz: number; start: number; count: number; box: THREE.Box3 }

/**
 * Grass tufts and flowers, grouped per map chunk and drawn as one InstancedMesh: each cull copies
 * the instances of the chunks in view into it, so off-screen chunks aren't drawn and the whole
 * thing is one draw call whatever the density. Chunks are culled by a tight box around their
 * instances, since a chunk's bounding sphere reaches far above the flat ground.
 */
export class Nature {
  readonly group = new THREE.Group();
  readonly mesh: THREE.InstancedMesh;
  readonly stats: NatureStats;
  readonly chunks: NatureChunk[] = [];
  private readonly geometry = natureGeometry();
  private readonly material = natureMaterial();
  private readonly matrices: Float32Array;
  private readonly colors: Float32Array;
  private readonly variants: Float32Array;
  private readonly variant: THREE.InstancedBufferAttribute;
  private readonly frustum = new THREE.Frustum();
  private readonly viewProj = new THREE.Matrix4();
  /** Indices of the chunks currently packed into the mesh, joined. */
  private shown = '';

  constructor(data: CampusData, opts: NatureOptions = {}) {
    this.group.name = 'nature';
    const scatter = scatterNature(data, opts);
    const total = scatter.reduce((n, c) => n + c.tufts.length + c.flowers.length, 0);
    this.matrices = new Float32Array(total * 16);
    this.colors = new Float32Array(total * 3);
    this.variants = new Float32Array(total);
    const color = new THREE.Color();
    let tufts = 0, flowers = 0, i = 0;
    for (const c of scatter) {
      const box = new THREE.Box3();
      const start = i;
      for (const t of [...c.tufts, ...c.flowers]) {
        // Rotation about y, uniform scale, translation (column-major).
        const cos = Math.cos(t.rot) * t.scale, sin = Math.sin(t.rot) * t.scale, a = this.matrices, o = i * 16;
        a[o] = cos; a[o + 2] = -sin;
        a[o + 5] = t.scale;
        a[o + 8] = sin; a[o + 10] = cos;
        a[o + 12] = t.x; a[o + 14] = t.z; a[o + 15] = 1;
        const flower = i - start >= c.tufts.length;
        this.variants[i] = flower ? FLOWER_VARIANT : t.kind;
        color.setHex(flower ? FLOWER_COLORS[t.kind] : 0xffffff).toArray(this.colors, i * 3);
        box.expandByPoint(new THREE.Vector3(t.x, 0, t.z));
        i++;
      }
      box.expandByScalar(0.5);
      box.min.y = 0;
      box.max.y = CULL_HEIGHT;
      this.chunks.push({ cx: c.cx, cz: c.cz, start, count: i - start, box });
      tufts += c.tufts.length;
      flowers += c.flowers.length;
    }
    this.stats = { chunks: this.chunks.length, tufts, flowers };

    this.variant = new THREE.InstancedBufferAttribute(new Float32Array(total), 1);
    this.geometry.setAttribute('variant', this.variant);
    this.mesh = new THREE.InstancedMesh(this.geometry, this.material, Math.max(total, 1));
    this.mesh.instanceColor = new THREE.InstancedBufferAttribute(new Float32Array(Math.max(total, 1) * 3), 3);
    this.mesh.name = 'nature';
    this.mesh.matrixAutoUpdate = false;
    // Culled per chunk in cull(); the mesh as a whole spans the map.
    this.mesh.frustumCulled = false;
    this.mesh.receiveShadow = true;
    this.group.add(this.mesh);
    this.show(this.chunks.map((_, k) => k));
  }

  /** Draw only the chunks whose box is in the camera's view. */
  cull(camera: THREE.Camera): void {
    camera.updateMatrixWorld();
    this.frustum.setFromProjectionMatrix(this.viewProj.multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse));
    const visible: number[] = [];
    this.chunks.forEach((c, k) => {
      if (this.frustum.intersectsBox(c.box)) visible.push(k);
    });
    this.show(visible);
  }

  /** The chunks drawn now. */
  get visibleChunks(): NatureChunk[] {
    return this.shown === '' ? [] : this.shown.split(',').map((k) => this.chunks[+k]);
  }

  private show(chunks: number[]): void {
    const key = chunks.join(',');
    if (key === this.shown && this.mesh.count > 0) return;
    this.shown = key;
    const m = this.mesh.instanceMatrix, col = this.mesh.instanceColor!, v = this.variant;
    let n = 0;
    for (const k of chunks) {
      const { start, count } = this.chunks[k];
      m.array.set(this.matrices.subarray(start * 16, (start + count) * 16), n * 16);
      col.array.set(this.colors.subarray(start * 3, (start + count) * 3), n * 3);
      v.array.set(this.variants.subarray(start, start + count), n);
      n += count;
    }
    // Upload only the packed part.
    for (const [attr, size] of [[m, 16], [col, 3], [v, 1]] as const) {
      attr.clearUpdateRanges();
      attr.addUpdateRange(0, n * size);
      attr.needsUpdate = true;
    }
    this.mesh.count = n;
    this.mesh.visible = n > 0;
  }

  dispose(): void {
    this.geometry.dispose();
    this.material.dispose();
    this.mesh.dispose();
  }
}
