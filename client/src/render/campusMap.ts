import * as THREE from 'three';
import campusRaw from '../../../shared/map/campus.json?raw';
import { GREEN_GRASS, GREEN_PITCH, GREEN_WATER, GREEN_WOOD, parseCampus, type CampusData, type Green } from './mapData';
import { buildLandmark, isLandmark } from './landmarks';
import { mulberry32 } from './rng';
import { GROUND_KINDS, GROUND_PLAZA, SurfaceBuilder, makeGroundMaterial } from './groundSurfaces';

const DM = 0.1;
const Y_GREEN = 0.01;
/** Height between stacked green layers, so overlapping polygons don't z-fight. */
const GREEN_LAYER_STEP = 0.005;
export const MAX_GREEN_LAYER = 8;
const Y_PATH = Y_GREEN + (MAX_GREEN_LAYER + 1) * GREEN_LAYER_STEP;
/** Ground surfaces never write depth, so they draw in this order: plazas, dirt, stone, then paved on top. */
const SURFACE_ORDER = [GROUND_PLAZA, 0, 1, 2];
const ROOF_RISE = 2.5;
const PITCHED_CHANCE = 0.4;

const WALLS = [0xd9c8a8, 0xc9b79a, 0xe0d5bd, 0xbfa88a, 0xcfcfc4];
const ROOFS = [0x8c5a4a, 0x7a6a5c, 0x9a7b5f, 0x6f7a80];
const GREEN_COLOR: Record<number, number> = {
  [GREEN_GRASS]: 0x6aa84f,
  [GREEN_WOOD]: 0x3f7d3a,
  [GREEN_WATER]: 0x4a90c8,
  [GREEN_PITCH]: 0x8bc078,
};

/** Accumulates flat-shaded, vertex-coloured triangles. */
export class MeshBuilder {
  readonly pos: number[] = [];
  readonly col: number[] = [];
  private readonly c = new THREE.Color();

  /** Add a triangle, flipping it if its normal points against `want`. */
  tri(a: number[], b: number[], c: number[], want: number[], color: number): void {
    const ux = b[0] - a[0], uy = b[1] - a[1], uz = b[2] - a[2];
    const vx = c[0] - a[0], vy = c[1] - a[1], vz = c[2] - a[2];
    const nx = uy * vz - uz * vy, ny = uz * vx - ux * vz, nz = ux * vy - uy * vx;
    const flip = nx * want[0] + ny * want[1] + nz * want[2] < 0;
    const pts = flip ? [a, c, b] : [a, b, c];
    this.c.setHex(color);
    for (const p of pts) {
      this.pos.push(p[0], p[1], p[2]);
      this.col.push(this.c.r, this.c.g, this.c.b);
    }
  }

  get triangles(): number {
    return this.pos.length / 9;
  }

  geometry(): THREE.BufferGeometry {
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(this.pos, 3));
    g.setAttribute('color', new THREE.Float32BufferAttribute(this.col, 3));
    g.computeVertexNormals();
    g.computeBoundingSphere();
    return g;
  }
}

const UP = [0, 1, 0];

function polygonPoints(p: number[]): THREE.Vector2[] {
  const pts: THREE.Vector2[] = [];
  for (let i = 0; i < p.length; i += 2) pts.push(new THREE.Vector2(p[i] * DM, p[i + 1] * DM));
  return pts;
}

function signedArea(pts: THREE.Vector2[]): number {
  let a = 0;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) a += pts[j].x * pts[i].y - pts[i].x * pts[j].y;
  return a / 2;
}

function fillPolygon(b: MeshBuilder, pts: THREE.Vector2[], y: number, color: number, want: number[] = UP): void {
  const tris = THREE.ShapeUtils.triangulateShape(pts, []);
  for (const [i, j, k] of tris) {
    b.tri([pts[i].x, y, pts[i].y], [pts[j].x, y, pts[j].y], [pts[k].x, y, pts[k].y], want, color);
  }
}

/** The generic extrusion used for every building that isn't a hand-made landmark. */
export function addBuilding(b: MeshBuilder, id: number, h: number, p: number[]): void {
  const rnd = mulberry32(id);
  const wall = WALLS[Math.floor(rnd() * WALLS.length)];
  const roof = ROOFS[Math.floor(rnd() * ROOFS.length)];
  const pitched = rnd() < PITCHED_CHANCE;
  const pts = polygonPoints(p);
  const ccw = signedArea(pts) > 0;
  const height = h * DM;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const a = pts[j], c = pts[i];
    const dx = c.x - a.x, dz = c.y - a.y;
    const out = ccw ? [dz, 0, -dx] : [-dz, 0, dx];
    const a0 = [a.x, 0, a.y], c0 = [c.x, 0, c.y], a1 = [a.x, height, a.y], c1 = [c.x, height, c.y];
    b.tri(a0, c0, c1, out, wall);
    b.tri(a0, c1, a1, out, wall);
  }
  if (pitched && pts.length === 4) {
    const cx = (pts[0].x + pts[1].x + pts[2].x + pts[3].x) / 4;
    const cz = (pts[0].y + pts[1].y + pts[2].y + pts[3].y) / 4;
    const apex = [cx, height + ROOF_RISE, cz];
    for (let i = 0, j = 3; i < 4; j = i++) {
      const a = pts[j], c = pts[i];
      const dx = c.x - a.x, dz = c.y - a.y;
      const out = ccw ? [dz, 0, -dx] : [-dz, 0, dx];
      b.tri([a.x, height, a.y], [c.x, height, c.y], apex, [out[0], 1, out[2]], roof);
    }
  } else {
    fillPolygon(b, pts, height, roof);
  }
}

type Box = [number, number, number, number];

function bbox(p: number[]): Box {
  const b: Box = [Infinity, Infinity, -Infinity, -Infinity];
  for (let i = 0; i < p.length; i += 2) {
    b[0] = Math.min(b[0], p[i]); b[1] = Math.min(b[1], p[i + 1]);
    b[2] = Math.max(b[2], p[i]); b[3] = Math.max(b[3], p[i + 1]);
  }
  return b;
}

/**
 * Layer index per green polygon id: larger polygons lower, and any two whose bounding boxes
 * overlap on different layers (e.g. lawns inside the park). Capped at MAX_GREEN_LAYER.
 */
export function greenLayers(green: Green[]): Map<number, number> {
  const items = green.map((g) => ({ id: g.id, box: bbox(g.p), area: Math.abs(signedArea(polygonPoints(g.p))), layer: 0 }));
  items.sort((a, b) => b.area - a.area || a.id - b.id);
  const layers = new Map<number, number>();
  for (let i = 0; i < items.length; i++) {
    const x = items[i];
    for (let j = 0; j < i; j++) {
      const y = items[j];
      if (x.box[0] < y.box[2] && y.box[0] < x.box[2] && x.box[1] < y.box[3] && y.box[1] < x.box[3]) {
        x.layer = Math.max(x.layer, y.layer + 1);
      }
    }
    x.layer = Math.min(x.layer, MAX_GREEN_LAYER);
    layers.set(x.id, x.layer);
  }
  return layers;
}

export interface MapStats {
  chunks: number;
  triangles: number;
  /** Building ids drawn as generic extrusions. */
  extruded: number[];
  /** Building ids drawn as hand-made landmarks (see landmarks.ts). */
  landmarks: number[];
}

/** The campus drawn as one merged mesh per fixed-size chunk (frustum culling drops the far ones). */
export class CampusMap {
  readonly group = new THREE.Group();
  readonly stats: MapStats;
  readonly material = new THREE.MeshLambertMaterial({ vertexColors: true });
  /** One material per ground kind (GROUND_DIRT, GROUND_STONE, GROUND_PAVED, GROUND_PLAZA). */
  readonly surfaceMaterials = GROUND_KINDS.map((k) => makeGroundMaterial(k));

  constructor(data: CampusData) {
    this.group.name = 'campus-map';
    const chunkM = data.chunkSize;
    const { minX, minZ } = data.bounds;
    const builders = new Map<string, MeshBuilder>();
    const surfaces = new Map<string, SurfaceBuilder[]>();
    const keyAt = (xDm: number, zDm: number): string => {
      const cx = Math.floor((xDm - minX) * DM / chunkM);
      const cz = Math.floor((zDm - minZ) * DM / chunkM);
      return `${cx},${cz}`;
    };
    const at = (xDm: number, zDm: number): MeshBuilder => {
      const key = keyAt(xDm, zDm);
      let b = builders.get(key);
      if (!b) builders.set(key, (b = new MeshBuilder()));
      return b;
    };
    const surfaceAt = (xDm: number, zDm: number, kind: number): SurfaceBuilder => {
      const key = keyAt(xDm, zDm);
      if (!builders.has(key)) builders.set(key, new MeshBuilder());
      let s = surfaces.get(key);
      if (!s) surfaces.set(key, (s = GROUND_KINDS.map((k) => new SurfaceBuilder(k, Y_PATH + SURFACE_ORDER.indexOf(k) * 0.002))));
      return s[kind] ?? s[GROUND_KINDS[1]];
    };
    const centre = (p: number[]): [number, number] => {
      let x0 = Infinity, x1 = -Infinity, z0 = Infinity, z1 = -Infinity;
      for (let i = 0; i < p.length; i += 2) {
        x0 = Math.min(x0, p[i]); x1 = Math.max(x1, p[i]);
        z0 = Math.min(z0, p[i + 1]); z1 = Math.max(z1, p[i + 1]);
      }
      return [(x0 + x1) / 2, (z0 + z1) / 2];
    };

    const layers = greenLayers(data.green);
    for (const g of data.green) {
      const [cx, cz] = centre(g.p);
      const rnd = mulberry32(g.id);
      const shade = 0.92 + rnd() * 0.16;
      const color = new THREE.Color(GREEN_COLOR[g.k] ?? GREEN_COLOR[GREEN_GRASS]).multiplyScalar(shade).getHex();
      fillPolygon(at(cx, cz), polygonPoints(g.p), Y_GREEN + layers.get(g.id)! * GREEN_LAYER_STEP, color);
    }
    for (const pl of data.plazas) {
      const [cx, cz] = centre(pl.p);
      surfaceAt(cx, cz, GROUND_PLAZA).polygon(pl.p.map((v) => v * DM));
    }
    for (const w of data.paths) {
      const half = (w.w * DM) / 2;
      for (let i = 0; i + 3 < w.p.length; i += 2) {
        const [x0, z0, x1, z1] = [w.p[i], w.p[i + 1], w.p[i + 2], w.p[i + 3]];
        surfaceAt((x0 + x1) / 2, (z0 + z1) / 2, w.s)
          .segment(x0 * DM, z0 * DM, x1 * DM, z1 * DM, half, i === 0, i + 4 >= w.p.length);
      }
    }
    const extruded: number[] = [];
    const landmarks: number[] = [];
    for (const bl of data.buildings) {
      const [cx, cz] = centre(bl.p);
      if (isLandmark(bl.id) && buildLandmark(at(cx, cz), bl, data)) {
        landmarks.push(bl.id);
      } else {
        addBuilding(at(cx, cz), bl.id, bl.h, bl.p);
        extruded.push(bl.id);
      }
    }

    let triangles = 0;
    for (const key of [...builders.keys()].sort()) {
      const b = builders.get(key)!;
      triangles += b.triangles;
      // A chunk with only ground surfaces has no merged mesh of its own, just a holder for them.
      const chunk = b.triangles > 0 ? new THREE.Mesh(b.geometry(), this.material) : new THREE.Group();
      chunk.name = `chunk-${key}`;
      chunk.matrixAutoUpdate = false;
      chunk.castShadow = true;
      chunk.receiveShadow = true;
      for (const s of surfaces.get(key) ?? []) {
        if (s.triangles === 0) continue;
        triangles += s.triangles;
        const mesh = new THREE.Mesh(s.geometry(), this.surfaceMaterials[s.kind]);
        mesh.name = `ground-${key}-${s.kind}`;
        mesh.matrixAutoUpdate = false;
        mesh.receiveShadow = true;
        mesh.renderOrder = SURFACE_ORDER.indexOf(s.kind) - SURFACE_ORDER.length;
        chunk.add(mesh);
      }
      this.group.add(chunk);
    }
    this.stats = { chunks: builders.size, triangles, extruded, landmarks };
  }

  dispose(): void {
    this.group.traverse((o) => (o as THREE.Mesh).geometry?.dispose());
    this.material.dispose();
    for (const m of this.surfaceMaterials) {
      m.map?.dispose();
      m.dispose();
    }
  }
}

let cached: CampusData | null = null;
/** The committed campus.json, parsed once. */
export function loadCampus(): CampusData {
  return (cached ??= parseCampus(campusRaw));
}
