import * as THREE from 'three';
import natureRaw from '../../../shared/map/parknature.json?raw';
import { MeshBuilder } from './campusMap';
import { box } from './gameAreas';
import { parseParkNature, type ParkNatureData } from './mapData';
import { lump, prism } from './propKit';
import { mulberry32 } from './rng';

const DM = 0.1;

// Flat colours, after the day reference's soft greens and warm browns (visual-style.md, Ground and nature).
const BARK = 0x6b4a2e;
const BARK_DARK = 0x553a24;
const OAK_LEAF = [0x4e8a34, 0x5f9c3c, 0x6aa844];
const PINE_LEAF = [0x2f6b3a, 0x377a40, 0x2a5f34];
const EUCA_BARK = 0xcfc4ad;
const EUCA_LEAF = [0x7fa07a, 0x6f9470, 0x8cae84];
const ROCK = [0x8d8a83, 0x9e9a92, 0x7c7972];
const BUSH = [0x3f7d33, 0x4b8c3a, 0x5a9a42];
const SOIL = 0x5a3e2a;
const EDGE = 0xa9a294;
const BLOOM = [0xe8c040, 0xd85a6a, 0xf4f1ea, 0xa98ad6, 0xf09a28];
const CHECK = [0xc8322a, 0xf2eee4];
const BLUE_CHECK = [0x2f6fb0, 0xf2eee4];
const BASKET = 0xb07a48;
const PIGEON = 0x8a8f99;
const PIGEON_DARK = 0x5d626b;
const BEAK = 0xe0a030;

/** Instance tint range: each placement is drawn a little lighter or darker, so neighbours differ. */
const TINT_MIN = 0.86;
const TINT_RANGE = 0.22;
/** Seed for the tints. Cosmetic only, but seeded so every client looks the same. */
const TINT_SEED = 0x7401;

/** Deterministic value in [-1, 1) for a vertex position, so shared corners of a blob move together. */
function wobble(x: number, y: number, z: number, seed: number): number {
  const s = Math.sin(x * 12.9898 + y * 78.233 + z * 37.719 + seed * 4.1414) * 43758.5453;
  return (s - Math.floor(s)) * 2 - 1;
}

const ICO = new THREE.IcosahedronGeometry(1, 0).getAttribute('position');

/** A lumpy low-poly blob (20 faces) centred on (cx, cy, cz): canopies, bushes, boulders. */
export function blob(b: MeshBuilder, cx: number, cy: number, cz: number, rx: number, ry: number, rz: number, color: number, seed: number, amp = 0.18): void {
  const v: number[][] = [];
  for (let i = 0; i < ICO.count; i++) {
    const x = ICO.getX(i), y = ICO.getY(i), z = ICO.getZ(i);
    const k = 1 + amp * wobble(Math.round(x * 100), Math.round(y * 100), Math.round(z * 100), seed);
    v.push([cx + x * rx * k, cy + y * ry * k, cz + z * rz * k]);
  }
  for (let i = 0; i < v.length; i += 3) {
    const out = [(v[i][0] + v[i + 1][0] + v[i + 2][0]) / 3 - cx, (v[i][1] + v[i + 1][1] + v[i + 2][1]) / 3 - cy, (v[i][2] + v[i + 1][2] + v[i + 2][2]) / 3 - cz];
    b.tri(v[i], v[i + 1], v[i + 2], out, color);
  }
}

/** Tapered n-sided tube from p to q: trunks and limbs that lean. */
export function limb(b: MeshBuilder, p: number[], q: number[], r0: number, r1: number, sides: number, color: number): void {
  const d = new THREE.Vector3(q[0] - p[0], q[1] - p[1], q[2] - p[2]).normalize();
  const u = new THREE.Vector3(1, 0, 0).cross(d).normalize();
  if (u.lengthSq() < 1e-6) u.set(0, 0, 1);
  const w = d.clone().cross(u).normalize();
  const ring = (o: number[], r: number) => Array.from({ length: sides }, (_, i) => {
    const a = (i / sides) * Math.PI * 2;
    const c = Math.cos(a) * r, s = Math.sin(a) * r;
    return [o[0] + u.x * c + w.x * s, o[1] + u.y * c + w.y * s, o[2] + u.z * c + w.z * s];
  });
  const lo = ring(p, r0), hi = ring(q, r1);
  for (let i = 0; i < sides; i++) {
    const j = (i + 1) % sides;
    const a = ((i + 0.5) / sides) * Math.PI * 2;
    const out = [u.x * Math.cos(a) + w.x * Math.sin(a), u.y * Math.cos(a) + w.y * Math.sin(a), u.z * Math.cos(a) + w.z * Math.sin(a)];
    b.tri(lo[i], lo[j], hi[j], out, color);
    b.tri(lo[i], hi[j], hi[i], out, color);
  }
}

// ---------- The models. Each stands on y = 0, centred on the origin, about 1 m to a unit of scale. ----------

function oak(b: MeshBuilder): void {
  // Short, thick trunk with two limbs and a broad, clumpy crown.
  limb(b, [0, 0, 0], [0.05, 1.6, 0], 0.26, 0.17, 6, BARK);
  limb(b, [0.05, 1.3, 0], [0.75, 2.1, 0.2], 0.12, 0.07, 5, BARK_DARK);
  limb(b, [0.02, 1.4, 0], [-0.6, 2.2, -0.35], 0.11, 0.06, 5, BARK_DARK);
  blob(b, 0, 2.7, 0, 1.6, 1.05, 1.5, OAK_LEAF[0], 1);
  blob(b, 0.95, 2.35, 0.35, 0.95, 0.75, 0.9, OAK_LEAF[1], 2);
  blob(b, -0.85, 2.45, -0.4, 0.9, 0.7, 0.95, OAK_LEAF[2], 3);
  blob(b, 0.1, 3.45, -0.15, 0.95, 0.65, 0.9, OAK_LEAF[1], 4);
  blob(b, -0.2, 2.3, 0.95, 0.75, 0.6, 0.7, OAK_LEAF[2], 5);
}

function pine(b: MeshBuilder): void {
  // Slim trunk under four stacked cones, each a different shade.
  prism(b, 0, 0, 0.16, 0.12, 0, 1.0, 6, BARK);
  const tiers: [number, number, number][] = [[0.7, 1.15, 2.2], [1.5, 0.95, 3.0], [2.3, 0.72, 3.7], [3.0, 0.45, 4.4]];
  tiers.forEach(([y0, r, y1], i) => prism(b, 0, 0, r, 0, y0, y1, 7, PINE_LEAF[i % PINE_LEAF.length]));
}

function eucalyptus(b: MeshBuilder): void {
  // Tall pale trunk that forks high up, with small, airy blue-green clumps.
  limb(b, [0, 0, 0], [0.15, 2.6, 0.05], 0.2, 0.13, 6, EUCA_BARK);
  limb(b, [0.15, 2.5, 0.05], [0.75, 4.1, 0.3], 0.11, 0.06, 5, EUCA_BARK);
  limb(b, [0.15, 2.5, 0.05], [-0.45, 4.4, -0.25], 0.1, 0.05, 5, EUCA_BARK);
  blob(b, 0.8, 4.4, 0.35, 0.9, 0.55, 0.8, EUCA_LEAF[0], 11, 0.25);
  blob(b, -0.5, 4.75, -0.3, 0.85, 0.55, 0.85, EUCA_LEAF[1], 12, 0.25);
  blob(b, 0.15, 5.2, 0.05, 0.7, 0.45, 0.7, EUCA_LEAF[2], 13, 0.25);
  blob(b, 0.5, 3.6, -0.4, 0.55, 0.35, 0.5, EUCA_LEAF[1], 14, 0.25);
}

function boulder(b: MeshBuilder): void {
  // A big rounded boulder with a small one leaning on it.
  blob(b, 0, 0.38, 0, 0.75, 0.5, 0.62, ROCK[0], 21, 0.28);
  blob(b, 0.6, 0.16, 0.35, 0.32, 0.22, 0.3, ROCK[1], 22, 0.3);
}

function flatRock(b: MeshBuilder): void {
  // Low, flat-topped slabs half sunk in the grass.
  prism(b, 0, 0, 0.85, 0.7, -0.05, 0.28, 6, ROCK[1]);
  prism(b, 0.55, 0.45, 0.45, 0.36, -0.05, 0.2, 5, ROCK[2]);
  prism(b, -0.6, 0.3, 0.32, 0.25, -0.05, 0.14, 5, ROCK[0]);
}

function bush(b: MeshBuilder): void {
  blob(b, 0, 0.45, 0, 0.7, 0.5, 0.65, BUSH[0], 31, 0.22);
  blob(b, 0.45, 0.35, 0.25, 0.45, 0.38, 0.45, BUSH[1], 32, 0.22);
  blob(b, -0.4, 0.32, -0.15, 0.42, 0.34, 0.4, BUSH[2], 33, 0.22);
}

function flowerBed(b: MeshBuilder): void {
  // A round bed of soil with a stone edge, leafy lumps and dots of colour.
  prism(b, 0, 0, 1.15, 1.1, 0, 0.16, 10, EDGE);
  prism(b, 0, 0, 1.0, 1.0, 0.16, 0.18, 10, SOIL);
  const rnd = mulberry32(41);
  for (let i = 0; i < 14; i++) {
    const a = rnd() * Math.PI * 2, d = Math.sqrt(rnd()) * 0.8;
    const x = Math.cos(a) * d, z = Math.sin(a) * d;
    lump(b, x, 0.17, z, 0.17, 0.13, 0.17, BUSH[i % BUSH.length]);
    lump(b, x + 0.05, 0.38, z - 0.04, 0.07, 0.06, 0.07, BLOOM[i % BLOOM.length]);
  }
}

function picnicBlanket(b: MeshBuilder): void {
  // A checked blanket in red, blue and cream, with a basket on one corner.
  const n = 5, w = 1.8 / n, d = 1.4 / n;
  for (let i = 0; i < n; i++) {
    for (let j = 0; j < n; j++) {
      const c = (i + j) % 2 ? CHECK[1] : (i % 2 ? BLUE_CHECK[0] : CHECK[0]);
      const x0 = -0.9 + i * w, z0 = -0.7 + j * d;
      b.tri([x0, 0.03, z0], [x0 + w, 0.03, z0], [x0 + w, 0.03, z0 + d], [0, 1, 0], c);
      b.tri([x0, 0.03, z0], [x0 + w, 0.03, z0 + d], [x0, 0.03, z0 + d], [0, 1, 0], c);
    }
  }
  box(b, 0.45, 0.03, -0.55, 0.8, 0.28, -0.3, BASKET);
  limb(b, [0.47, 0.28, -0.43], [0.62, 0.45, -0.43], 0.015, 0.015, 4, BARK_DARK);
  limb(b, [0.62, 0.45, -0.43], [0.78, 0.28, -0.43], 0.015, 0.015, 4, BARK_DARK);
}

function bird(b: MeshBuilder): void {
  // A pigeon pecking about: body, head, beak and tail, facing +z.
  blob(b, 0, 0.13, 0, 0.08, 0.07, 0.13, PIGEON, 71, 0.1);
  blob(b, 0, 0.23, 0.11, 0.05, 0.05, 0.05, PIGEON_DARK, 72, 0.1);
  limb(b, [0, 0.23, 0.15], [0, 0.22, 0.2], 0.015, 0.002, 4, BEAK);
  limb(b, [0, 0.14, -0.1], [0, 0.17, -0.22], 0.04, 0.02, 4, PIGEON_DARK);
  prism(b, 0.03, 0, 0.008, 0.008, 0, 0.07, 3, BEAK, false);
  prism(b, -0.03, 0, 0.008, 0.008, 0, 0.07, 3, BEAK, false);
}

const MODELS: Record<string, (b: MeshBuilder) => void> = {
  tree_oak: oak,
  tree_pine: pine,
  tree_eucalyptus: eucalyptus,
  rock_boulder: boulder,
  rock_flat: flatRock,
  bush,
  flower_bed: flowerBed,
  picnic_blanket: picnicBlanket,
  bird,
};

/** Every nature type the park can draw. */
export const NATURE_KIT = Object.keys(MODELS);
/** Builds one nature type's model into `b`, at the origin and unrotated. */
export function buildNature(b: MeshBuilder, type: string): void {
  const f = MODELS[type];
  if (!f) throw new Error(`unknown nature type ${type}`);
  f(b);
}

/**
 * Bakes the trees, rocks, bushes, flower beds, picnic blankets and birds of Aldrich Park from
 * parknature.json into the campus map's merged chunk meshes: `at(x, z)` (decimetres) gives the builder
 * of the chunk a placement stands in. Each placement keeps its seeded rotation, scale and height stretch,
 * plus a small seeded tint. They never move, so this adds no draw calls at all (the park's chunks are
 * drawn anyway), is culled per chunk with them, and casts shadows with them. Returns placements per type.
 */
export function bakeParkNature(data: ParkNatureData, at: (xDm: number, zDm: number) => MeshBuilder): Map<string, number> {
  const models = data.types.map((type) => {
    const b = new MeshBuilder();
    buildNature(b, type);
    return b;
  });
  const counts = new Map<string, number>();
  const m = new THREE.Matrix4(), q = new THREE.Quaternion(), s = new THREE.Vector3(), p = new THREE.Vector3();
  const up = new THREE.Vector3(0, 1, 0);
  const a = new THREE.Vector3(), b = new THREE.Vector3(), c = new THREE.Vector3(), n = new THREE.Vector3(), e = new THREE.Vector3();
  const color = new THREE.Color();
  const tint = mulberry32(TINT_SEED);
  for (const [t, x, z, rot, scale, stretch] of data.items) {
    const type = data.types[t];
    counts.set(type, (counts.get(type) ?? 0) + 1);
    // Rotation is clockwise seen from above, in degrees, like props.json. Positive scales keep the winding.
    q.setFromAxisAngle(up, (-rot * Math.PI) / 180);
    s.set(scale / 100, (scale / 100) * (stretch / 100), scale / 100);
    m.compose(p.set(x * DM, 0, z * DM), q, s);
    const k = TINT_MIN + tint() * TINT_RANGE;
    const pos = models[t].pos, col = models[t].col, dst = at(x, z);
    for (let i = 0; i < pos.length; i += 9) {
      a.fromArray(pos, i).applyMatrix4(m);
      b.fromArray(pos, i + 3).applyMatrix4(m);
      c.fromArray(pos, i + 6).applyMatrix4(m);
      // The triangle's own normal, so tri() keeps the winding as it is.
      n.subVectors(b, a).cross(e.subVectors(c, a));
      // Builder colours are in the working (linear) space; tri() takes sRGB hex, as getHex() gives.
      color.setRGB(col[i] * k, col[i + 1] * k, col[i + 2] * k);
      dst.tri(a.toArray(), b.toArray(), c.toArray(), n.toArray(), color.getHex());
    }
  }
  return counts;
}

let cached: ParkNatureData | null = null;
/** The committed parknature.json, parsed once. */
export function loadParkNature(): ParkNatureData {
  return (cached ??= parseParkNature(natureRaw));
}
