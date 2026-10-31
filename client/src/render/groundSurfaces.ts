import * as THREE from 'three';
import { SURFACE_DIRT, SURFACE_PAVED, SURFACE_STONE } from './mapData';
import { mulberry32 } from './rng';

/** Ground surface kinds drawn over the grass: the three path surfaces, then plazas. */
export const GROUND_DIRT = SURFACE_DIRT;
export const GROUND_STONE = SURFACE_STONE;
export const GROUND_PAVED = SURFACE_PAVED;
export const GROUND_PLAZA = 3;
export const GROUND_KINDS = [GROUND_DIRT, GROUND_STONE, GROUND_PAVED, GROUND_PLAZA] as const;
export const GROUND_NAMES = ['dirt', 'stone', 'paved', 'plaza'];

/** Width (metres) of the band that fades a path or plaza into the grass. */
export const EDGE_BAND = 0.6;
/** Metres covered by one repeat of each kind's texture. */
const TILE_M = [3, 2, 4, 2.4];
const TEX = 32;

type Rgb = [number, number, number];

function hex(c: number): Rgb {
  return [(c >> 16) & 255, (c >> 8) & 255, c & 255];
}

/** A small seamless texture per kind, generated from a fixed seed (no image files to load). */
function texturePixels(kind: number): Uint8Array {
  const rnd = mulberry32(0x9a7e + kind);
  const px = new Uint8Array(TEX * TEX * 4);
  const set = (x: number, y: number, c: Rgb, k: number) => {
    const i = (((y + TEX) % TEX) * TEX + ((x + TEX) % TEX)) * 4;
    px[i] = Math.max(0, Math.min(255, c[0] * k));
    px[i + 1] = Math.max(0, Math.min(255, c[1] * k));
    px[i + 2] = Math.max(0, Math.min(255, c[2] * k));
    px[i + 3] = 255;
  };
  if (kind === GROUND_DIRT) {
    const base = hex(0xa47b52);
    for (let y = 0; y < TEX; y++) for (let x = 0; x < TEX; x++) set(x, y, base, 0.9 + rnd() * 0.18);
    for (let n = 0; n < 14; n++) {
      const x = Math.floor(rnd() * TEX), y = Math.floor(rnd() * TEX), k = rnd() < 0.5 ? 0.72 : 1.15;
      set(x, y, base, k); set(x + 1, y, base, k);
    }
  } else if (kind === GROUND_STONE) {
    // Flagstones: rows of slabs with random widths, each slab its own shade, darker grout between.
    const base = hex(0xb9b1a2), grout = hex(0x7d776c);
    const rowH = 8;
    for (let row = 0; row < TEX / rowH; row++) {
      const x0 = Math.floor(rnd() * 6);
      const starts: number[] = [];
      for (let x = x0; x < x0 + TEX; x += 7 + Math.floor(rnd() * 6)) starts.push(x);
      starts.push(x0 + TEX);
      for (let s = 0; s + 1 < starts.length; s++) {
        const k = 0.88 + rnd() * 0.2;
        for (let x = starts[s]; x < starts[s + 1]; x++) {
          for (let y = row * rowH; y < (row + 1) * rowH; y++) {
            const edge = x === starts[s] || y === row * rowH;
            set(x, y, edge ? grout : base, edge ? 1 : k * (0.96 + rnd() * 0.06));
          }
        }
      }
    }
  } else if (kind === GROUND_PAVED) {
    const base = hex(0x8d8c88);
    for (let y = 0; y < TEX; y++) for (let x = 0; x < TEX; x++) set(x, y, base, 0.93 + rnd() * 0.12);
    for (let x = 0; x < TEX; x++) set(x, 0, base, 0.82);
  } else {
    // Plaza: square tiles with thin light grout.
    const base = hex(0xd6c4a2), grout = hex(0xeee4cf);
    const tile = 8;
    for (let ty = 0; ty < TEX / tile; ty++) {
      for (let tx = 0; tx < TEX / tile; tx++) {
        const k = 0.9 + rnd() * 0.15;
        for (let y = 0; y < tile; y++) {
          for (let x = 0; x < tile; x++) {
            const edge = x === 0 || y === 0;
            set(tx * tile + x, ty * tile + y, edge ? grout : base, edge ? 1 : k * (0.97 + rnd() * 0.05));
          }
        }
      }
    }
  }
  return px;
}

/** One material per kind: its tiling texture, vertex colours with alpha for the soft edge. */
export function makeGroundMaterial(kind: number): THREE.MeshLambertMaterial {
  const tex = new THREE.DataTexture(texturePixels(kind), TEX, TEX, THREE.RGBAFormat);
  tex.wrapS = tex.wrapT = THREE.RepeatWrapping;
  tex.magFilter = THREE.NearestFilter;
  tex.minFilter = THREE.LinearMipmapLinearFilter;
  tex.generateMipmaps = true;
  tex.colorSpace = THREE.SRGBColorSpace;
  tex.needsUpdate = true;
  const m = new THREE.MeshLambertMaterial({ map: tex, vertexColors: true, transparent: true, depthWrite: false });
  m.name = `ground-${GROUND_NAMES[kind]}`;
  return m;
}

/**
 * Triangles of one ground kind in one chunk. Colour has 4 components: the alpha is 1 on the
 * surface and fades to 0 across the edge band, so the surface blends into the grass under it.
 */
export class SurfaceBuilder {
  readonly pos: number[] = [];
  readonly col: number[] = [];
  readonly uv: number[] = [];

  constructor(readonly kind: number, private readonly y: number, private readonly shade = 1) {}

  private vert(x: number, z: number, alpha: number): void {
    const t = TILE_M[this.kind];
    this.pos.push(x, this.y, z);
    this.col.push(this.shade, this.shade, this.shade, alpha);
    this.uv.push(x / t, z / t);
  }

  /** Triangle given as [x, z, alpha] corners, wound so its normal points up. */
  tri(a: number[], b: number[], c: number[]): void {
    const cross = (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0]);
    // y up with x east, z south: an upward normal means clockwise in (x, z).
    const [p, q, r] = cross < 0 ? [a, b, c] : [a, c, b];
    this.vert(p[0], p[1], p[2]);
    this.vert(q[0], q[1], q[2]);
    this.vert(r[0], r[1], r[2]);
  }

  private quad(a: number[], b: number[], c: number[], d: number[]): void {
    this.tri(a, b, c);
    this.tri(a, c, d);
  }

  /**
   * One path segment (metres): the surface strip plus a fade band on both sides. `startCap` and
   * `endCap` add a fade past the ends of the path; inner joints extend the strip to cover bends.
   */
  segment(x0: number, z0: number, x1: number, z1: number, halfW: number, startCap: boolean, endCap: boolean): void {
    const dx = x1 - x0, dz = z1 - z0;
    const len = Math.hypot(dx, dz);
    if (len === 0) return;
    const ux = dx / len, uz = dz / len;
    const nx = -uz, nz = ux;
    const ext0 = startCap ? 0 : halfW, ext1 = endCap ? 0 : halfW;
    const sx = x0 - ux * ext0, sz = z0 - uz * ext0;
    const ex = x1 + ux * ext1, ez = z1 + uz * ext1;
    const at = (x: number, z: number, side: number, a: number) => [x + nx * side, z + nz * side, a];
    const o = halfW + EDGE_BAND;
    this.quad(at(sx, sz, halfW, 1), at(ex, ez, halfW, 1), at(ex, ez, -halfW, 1), at(sx, sz, -halfW, 1));
    this.quad(at(sx, sz, halfW, 1), at(ex, ez, halfW, 1), at(ex, ez, o, 0), at(sx, sz, o, 0));
    this.quad(at(sx, sz, -halfW, 1), at(ex, ez, -halfW, 1), at(ex, ez, -o, 0), at(sx, sz, -o, 0));
    const cap = (x: number, z: number, dir: number) => {
      const fx = x + ux * dir * EDGE_BAND, fz = z + uz * dir * EDGE_BAND;
      this.quad(at(x, z, halfW, 1), at(x, z, -halfW, 1), at(fx, fz, -halfW, 0), at(fx, fz, halfW, 0));
    };
    if (startCap) cap(sx, sz, -1);
    if (endCap) cap(ex, ez, 1);
  }

  /** A filled polygon (metres, [x0,z0,x1,z1,...]) with a fade band outside every edge and corner. */
  polygon(p: number[]): void {
    const pts: THREE.Vector2[] = [];
    for (let i = 0; i < p.length; i += 2) pts.push(new THREE.Vector2(p[i], p[i + 1]));
    const n = pts.length;
    if (n < 3) return;
    for (const [i, j, k] of THREE.ShapeUtils.triangulateShape(pts, [])) {
      this.tri([pts[i].x, pts[i].y, 1], [pts[j].x, pts[j].y, 1], [pts[k].x, pts[k].y, 1]);
    }
    // Outward normal of edge a->b: (dz, -dx) for a counter-clockwise ring in (x, z), else flipped.
    const ccw = THREE.ShapeUtils.isClockWise(pts) ? -1 : 1;
    const normal = (a: THREE.Vector2, b: THREE.Vector2): [number, number] => {
      const dx = b.x - a.x, dz = b.y - a.y, len = Math.hypot(dx, dz) || 1;
      return [(dz / len) * ccw * EDGE_BAND, (-dx / len) * ccw * EDGE_BAND];
    };
    for (let i = 0; i < n; i++) {
      const a = pts[i], b = pts[(i + 1) % n], c = pts[(i + 2) % n];
      const [ox, oz] = normal(a, b);
      this.quad([a.x, a.y, 1], [b.x, b.y, 1], [b.x + ox, b.y + oz, 0], [a.x + ox, a.y + oz, 0]);
      // Fill the wedge at corner b between this edge's band and the next one's.
      const [px, pz] = normal(b, c);
      this.tri([b.x, b.y, 1], [b.x + ox, b.y + oz, 0], [b.x + px, b.y + pz, 0]);
    }
  }

  get triangles(): number {
    return this.pos.length / 9;
  }

  geometry(): THREE.BufferGeometry {
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(this.pos, 3));
    g.setAttribute('color', new THREE.Float32BufferAttribute(this.col, 4));
    g.setAttribute('uv', new THREE.Float32BufferAttribute(this.uv, 2));
    const normal = new Float32Array(this.pos.length);
    for (let i = 1; i < normal.length; i += 3) normal[i] = 1;
    g.setAttribute('normal', new THREE.BufferAttribute(normal, 3));
    g.computeBoundingSphere();
    return g;
  }
}
