import * as THREE from 'three';
import areasRaw from '../../../shared/map/areas.json?raw';
import { MeshBuilder } from './campusMap';
import { parseAreas, type AreasData } from './mapData';

const DM = 0.1;
/** Above the campus paths so the border isn't hidden under them. */
const Y_BORDER = 0.08;
export const BORDER_WIDTH = 1;
export const BORDER_COLOR = 0xd01818;

const WOOD = 0x8a5a34;
const WOOD_DARK = 0x6b4426;
const CANOPY_A = 0xc8322a;
const CANOPY_B = 0xf2e6c8;
const GOODS = [0xe0a030, 0x7fb04a, 0xc0503a];

/** Polygon [x0,z0,...] in decimetres -> [x,z] points in metres. */
export function polygonMetres(p: number[]): [number, number][] {
  const out: [number, number][] = [];
  for (let i = 0; i < p.length; i += 2) out.push([p[i] * DM, p[i + 1] * DM]);
  return out;
}

/**
 * A flat closed strip of `width` centred on the polygon outline, mitred at the corners.
 * Vertices 2i and 2i+1 are the two edges of the strip at polygon point i, so each pair's
 * midpoint is exactly that point.
 */
export function borderGeometry(pts: [number, number][], width: number, y: number): THREE.BufferGeometry {
  const n = pts.length;
  const half = width / 2;
  const pos: number[] = [];
  for (let i = 0; i < n; i++) {
    const [px, pz] = pts[(i + n - 1) % n], [cx, cz] = pts[i], [nx, nz] = pts[(i + 1) % n];
    const l0 = Math.hypot(cx - px, cz - pz), l1 = Math.hypot(nx - cx, nz - cz);
    // Left-hand normals of the incoming and outgoing edges, averaged into a miter.
    const ax = -(cz - pz) / l0, az = (cx - px) / l0, bx = -(nz - cz) / l1, bz = (nx - cx) / l1;
    let mx = ax + bx, mz = az + bz;
    const ml = Math.hypot(mx, mz) || 1;
    mx /= ml; mz /= ml;
    const s = half / Math.max(0.3, mx * ax + mz * az);
    pos.push(cx + mx * s, y, cz + mz * s, cx - mx * s, y, cz - mz * s);
  }
  const index: number[] = [];
  for (let i = 0; i < n; i++) {
    const a = 2 * i, b = 2 * i + 1, c = 2 * ((i + 1) % n), d = c + 1;
    index.push(a, b, c, b, d, c);
  }
  const g = new THREE.BufferGeometry();
  g.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
  g.setIndex(index);
  g.computeVertexNormals();
  g.computeBoundingSphere();
  return g;
}

/** Axis-aligned box from (x0,y0,z0) to (x1,y1,z1), relative to the mesh origin. */
export function box(b: MeshBuilder, x0: number, y0: number, z0: number, x1: number, y1: number, z1: number, color: number): void {
  const p = (x: number, y: number, z: number) => [x, y, z];
  const quad = (a: number[], c: number[], d: number[], e: number[], n: number[]) => {
    b.tri(a, c, d, n, color);
    b.tri(a, d, e, n, color);
  };
  quad(p(x0, y1, z0), p(x1, y1, z0), p(x1, y1, z1), p(x0, y1, z1), [0, 1, 0]);
  quad(p(x0, y0, z0), p(x1, y0, z0), p(x1, y1, z0), p(x0, y1, z0), [0, 0, -1]);
  quad(p(x0, y0, z1), p(x1, y0, z1), p(x1, y1, z1), p(x0, y1, z1), [0, 0, 1]);
  quad(p(x0, y0, z0), p(x0, y0, z1), p(x0, y1, z1), p(x0, y1, z0), [-1, 0, 0]);
  quad(p(x1, y0, z0), p(x1, y0, z1), p(x1, y1, z1), p(x1, y1, z0), [1, 0, 0]);
}

/**
 * A market stall, about 3 m wide, 2 m deep and 2.8 m tall, centred on the origin with its
 * counter facing south (+z, toward spawn).
 */
export function stallGeometry(): THREE.BufferGeometry {
  const b = new MeshBuilder();
  const w = 1.5, d = 1;
  // Counter and the goods on it.
  box(b, -w, 0, 0.4, w, 1, d, WOOD);
  box(b, -w - 0.05, 1, 0.35, w + 0.05, 1.08, d + 0.05, WOOD_DARK);
  GOODS.forEach((c, i) => box(b, -1.1 + i * 0.85, 1.08, 0.5, -0.6 + i * 0.85, 1.35, 0.9, c));
  // Back wall and four posts.
  box(b, -w, 0, -d, w, 1.8, -d + 0.1, WOOD_DARK);
  for (const [x, z] of [[-w, -d], [w - 0.12, -d], [-w, d - 0.12], [w - 0.12, d - 0.12]]) {
    box(b, x, 0, z, x + 0.12, 2.4, z + 0.12, WOOD);
  }
  // Striped canopy sloping down toward the front.
  const stripes = 6;
  const back = -d - 0.2, front = d + 0.4, yb = 2.8, yf = 2.3;
  for (let i = 0; i < stripes; i++) {
    const x0 = -w - 0.2 + ((2 * w + 0.4) * i) / stripes, x1 = -w - 0.2 + ((2 * w + 0.4) * (i + 1)) / stripes;
    const c = i % 2 ? CANOPY_B : CANOPY_A;
    const a = [x0, yb, back], e = [x1, yb, back], f = [x1, yf, front], g = [x0, yf, front];
    b.tri(a, e, f, [0, 1, 0], c);
    b.tri(a, f, g, [0, 1, 0], c);
    b.tri(a, e, f, [0, -1, 0], c);
    b.tri(a, f, g, [0, -1, 0], c);
  }
  return b.geometry();
}

/** The PvP zone border (red, on the ground) and the shop stall, from areas.json. */
export class AreaMarkers {
  readonly group = new THREE.Group();
  readonly border: THREE.Mesh;
  readonly stall: THREE.Mesh;
  /** Border centre line in metres: one closed polygon per PvP area. */
  readonly borderPoints: [number, number][][] = [];

  constructor(data: AreasData) {
    this.group.name = 'area-markers';
    const geoms: THREE.BufferGeometry[] = [];
    for (const a of data.areas) {
      if (a.kind !== 'pvp') continue;
      const pts = polygonMetres(a.p);
      this.borderPoints.push(pts);
      geoms.push(borderGeometry(pts, BORDER_WIDTH, Y_BORDER));
    }
    // One mesh for all PvP borders: merge index-offset copies.
    const pos: number[] = [], index: number[] = [];
    for (const g of geoms) {
      const base = pos.length / 3;
      pos.push(...(g.getAttribute('position').array as Float32Array));
      for (const i of g.getIndex()!.array) index.push(i + base);
      g.dispose();
    }
    const border = new THREE.BufferGeometry();
    border.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
    border.setIndex(index);
    border.computeBoundingSphere();
    this.border = new THREE.Mesh(
      border,
      new THREE.MeshBasicMaterial({ color: BORDER_COLOR, side: THREE.DoubleSide, polygonOffset: true, polygonOffsetFactor: -2 }),
    );
    this.border.name = 'pvp-border';
    this.border.matrixAutoUpdate = false;

    this.stall = new THREE.Mesh(stallGeometry(), new THREE.MeshLambertMaterial({ vertexColors: true }));
    this.stall.name = 'shop-stall';
    this.stall.position.set(data.shop.x * DM, 0, data.shop.z * DM);
    this.stall.updateMatrix();
    this.stall.matrixAutoUpdate = false;
    this.stall.castShadow = true;
    this.stall.receiveShadow = true;
    this.group.add(this.border, this.stall);
  }

  dispose(): void {
    for (const m of [this.border, this.stall]) {
      m.geometry.dispose();
      (m.material as THREE.Material).dispose();
    }
  }
}

let cached: AreasData | null = null;
/** The committed areas.json, parsed once. */
export function loadAreas(): AreasData {
  return (cached ??= parseAreas(areasRaw));
}
