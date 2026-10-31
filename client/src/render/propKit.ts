import * as THREE from 'three';
import propsRaw from '../../../shared/map/props.json?raw';
import { MeshBuilder } from './campusMap';
import { box } from './gameAreas';
import { parseProps, type PropsData } from './mapData';

const DM = 0.1;

// Flat colours for the campus prop kit (visual-style.md, Props).
const WOOD = 0x8a5a34;
const WOOD_DARK = 0x6b4426;
const WOOD_LIGHT = 0xb07a48;
const IRON = 0x2e3236;
const STEEL = 0x8c939a;
const BIN_GREEN = 0x2f5d3a;
const CONCRETE = 0xb8b2a6;
const CONCRETE_DARK = 0x948d82;
const BRICK = 0xa4553c;
const SOIL = 0x4a3424;
const LEAF = 0x4f8a3a;
const LEAF_LIGHT = 0x6fa84a;
const FLOWER = [0xe8c040, 0xd85a6a, 0xf0f0f0];
const UCI_BLUE = 0x1f4f8a;
const UCI_GOLD = 0xf0b83c;
const WHITE = 0xf2eee4;
const RED = 0xc8322a;
const GLOW = 0xffd27a;
const TYRE = 0x222222;
const BIKE = [0xc8322a, 0x2f6fb0, 0x3f9a5a];
const PAPER = [0xf4efe0, 0xf0d870, 0xa8d0e8, 0xf0b0b0];
const BURLAP = 0xc8a876;
const BURLAP_DARK = 0xa88a5c;
const FRUIT = [0xd83a2a, 0xf09a28, 0xe8d040, 0x6cae3a];

type V = number[];

/** Vertical n-sided prism (or frustum when r1 differs) centred on (cx, cz). */
export function prism(b: MeshBuilder, cx: number, cz: number, r0: number, r1: number, y0: number, y1: number, sides: number, color: number, caps = true): void {
  const ring = (r: number, y: number) =>
    Array.from({ length: sides }, (_, i) => {
      const a = (i / sides) * Math.PI * 2 + Math.PI / sides;
      return [cx + Math.cos(a) * r, y, cz + Math.sin(a) * r];
    });
  const lo = ring(r0, y0), hi = ring(r1, y1);
  for (let i = 0; i < sides; i++) {
    const j = (i + 1) % sides;
    const a = (i + 0.5) / sides * Math.PI * 2 + Math.PI / sides;
    const out = [Math.cos(a), 0, Math.sin(a)];
    b.tri(lo[i], lo[j], hi[j], out, color);
    b.tri(lo[i], hi[j], hi[i], out, color);
    if (caps) {
      if (r1 > 0) b.tri([cx, y1, cz], hi[i], hi[j], [0, 1, 0], color);
      if (r0 > 0) b.tri([cx, y0, cz], lo[i], lo[j], [0, -1, 0], color);
    }
  }
}

/** A wheel: n-sided disc of radius r in the x-y plane at (cx, cy, cz), `t` thick along z. */
function wheel(b: MeshBuilder, cx: number, cy: number, cz: number, r: number, t: number, color: number): void {
  const n = 8;
  const pt = (i: number, z: number): V => {
    const a = (i / n) * Math.PI * 2;
    return [cx + Math.cos(a) * r, cy + Math.sin(a) * r, z];
  };
  for (let i = 0; i < n; i++) {
    const j = (i + 1) % n;
    const a = ((i + 0.5) / n) * Math.PI * 2;
    const out = [Math.cos(a), Math.sin(a), 0];
    b.tri(pt(i, cz - t), pt(j, cz - t), pt(j, cz + t), out, color);
    b.tri(pt(i, cz - t), pt(j, cz + t), pt(i, cz + t), out, color);
    b.tri([cx, cy, cz + t], pt(i, cz + t), pt(j, cz + t), [0, 0, 1], color);
    b.tri([cx, cy, cz - t], pt(i, cz - t), pt(j, cz - t), [0, 0, -1], color);
  }
}

/** Thin bar between two points, as a box-like quad prism of half-thickness h. */
function bar(b: MeshBuilder, p: V, q: V, h: number, color: number): void {
  const d = [q[0] - p[0], q[1] - p[1], q[2] - p[2]];
  const len = Math.hypot(d[0], d[1], d[2]);
  const u = d.map((v) => v / len);
  // Two vectors perpendicular to the bar.
  const ref = Math.abs(u[1]) < 0.9 ? [0, 1, 0] : [1, 0, 0];
  const s = [u[1] * ref[2] - u[2] * ref[1], u[2] * ref[0] - u[0] * ref[2], u[0] * ref[1] - u[1] * ref[0]];
  const sl = Math.hypot(s[0], s[1], s[2]);
  const a = s.map((v) => (v / sl) * h);
  const c = [u[1] * a[2] - u[2] * a[1], u[2] * a[0] - u[0] * a[2], u[0] * a[1] - u[1] * a[0]];
  const corner = (o: V, sa: number, sc: number): V => [o[0] + sa * a[0] + sc * c[0], o[1] + sa * a[1] + sc * c[1], o[2] + sa * a[2] + sc * c[2]];
  const signs = [[1, 1], [-1, 1], [-1, -1], [1, -1]];
  for (let i = 0; i < 4; i++) {
    const [s0, c0] = signs[i], [s1, c1] = signs[(i + 1) % 4];
    const out = [(s0 + s1) * a[0] + (c0 + c1) * c[0], (s0 + s1) * a[1] + (c0 + c1) * c[1], (s0 + s1) * a[2] + (c0 + c1) * c[2]];
    b.tri(corner(p, s0, c0), corner(p, s1, c1), corner(q, s1, c1), out, color);
    b.tri(corner(p, s0, c0), corner(q, s1, c1), corner(q, s0, c0), out, color);
  }
}

/** Low-poly lump (octahedron stretched to rx, ry, rz) sitting on y0: fruit, bushes, sacks. */
export function lump(b: MeshBuilder, cx: number, y0: number, cz: number, rx: number, ry: number, rz: number, color: number): void {
  const cy = y0 + ry;
  const top = [cx, cy + ry, cz], bot = [cx, y0, cz];
  const ring = [[cx + rx, cy, cz], [cx, cy, cz + rz], [cx - rx, cy, cz], [cx, cy, cz - rz]];
  for (let i = 0; i < 4; i++) {
    const p = ring[i], q = ring[(i + 1) % 4];
    const mx = (p[0] + q[0]) / 2 - cx, mz = (p[2] + q[2]) / 2 - cz;
    b.tri(top, p, q, [mx, ry, mz], color);
    b.tri(bot, p, q, [mx, -ry, mz], color);
  }
}

// ---------- The kit. Each model stands on y = 0, centred on the origin, front toward +z. ----------

function bench(b: MeshBuilder): void {
  // Two cast-iron ends, three seat slats, two back slats.
  for (const x of [-0.7, 0.62]) {
    box(b, x, 0, -0.22, x + 0.08, 0.42, 0.22, IRON);
    box(b, x, 0.42, -0.26, x + 0.08, 0.85, -0.18, IRON);
  }
  for (let i = 0; i < 3; i++) box(b, -0.8, 0.42, -0.22 + i * 0.15, 0.8, 0.47, -0.1 + i * 0.15, WOOD_LIGHT);
  for (let i = 0; i < 2; i++) box(b, -0.8, 0.55 + i * 0.16, -0.27, 0.8, 0.67 + i * 0.16, -0.21, WOOD_LIGHT);
}

/** Where a lit prop's glass sits in its model (metres, before rotation), and whether it holds a flame. */
export interface LampHead {
  readonly min: readonly [number, number, number];
  readonly max: readonly [number, number, number];
  readonly flame: boolean;
}

/** The glass of every prop type that lights up at night. Props marks it to glow while LampLights has it lit. */
export const LAMP_HEADS: Readonly<Record<string, LampHead>> = {
  lamp_post: { min: [-0.1, 2.72, 0.39], max: [0.1, 2.98, 0.59], flame: false },
  lantern: { min: [-0.08, 0.98, 0.18], max: [0.08, 1.18, 0.34], flame: true },
};

function glass(b: MeshBuilder, type: string): void {
  const { min, max } = LAMP_HEADS[type];
  box(b, min[0], min[1], min[2], max[0], max[1], max[2], GLOW);
}

function lampPost(b: MeshBuilder, withGlass = true): void {
  prism(b, 0, 0, 0.18, 0.14, 0, 0.3, 6, IRON);
  prism(b, 0, 0, 0.06, 0.05, 0.3, 3.2, 6, IRON);
  // Arm reaching forward and a hanging lamp head (lit by LampLights).
  box(b, -0.03, 3.1, -0.03, 0.03, 3.16, 0.55, IRON);
  box(b, -0.12, 2.68, 0.37, 0.12, 2.72, 0.61, IRON);
  if (withGlass) glass(b, 'lamp_post');
  prism(b, 0, 0.49, 0.18, 0.02, 2.98, 3.12, 4, IRON);
}

function bin(b: MeshBuilder): void {
  prism(b, 0, 0, 0.26, 0.28, 0, 0.85, 8, BIN_GREEN);
  prism(b, 0, 0, 0.3, 0.2, 0.85, 0.98, 8, IRON);
  box(b, -0.12, 0.6, 0.255, 0.12, 0.7, 0.275, WHITE);
}

function bike(b: MeshBuilder, z: number, color: number): void {
  // Wheels along x, frame between them, seat and handlebar.
  wheel(b, -0.5, 0.33, z, 0.33, 0.025, TYRE);
  wheel(b, 0.5, 0.33, z, 0.33, 0.025, TYRE);
  bar(b, [-0.5, 0.33, z], [-0.1, 0.33, z], 0.025, color);
  bar(b, [-0.1, 0.33, z], [-0.18, 0.78, z], 0.025, color);
  bar(b, [-0.5, 0.33, z], [-0.18, 0.78, z], 0.025, color);
  bar(b, [-0.18, 0.78, z], [0.38, 0.78, z], 0.025, color);
  bar(b, [-0.1, 0.33, z], [0.38, 0.78, z], 0.025, color);
  bar(b, [0.38, 0.78, z], [0.5, 0.33, z], 0.025, color);
  bar(b, [0.38, 0.78, z], [0.34, 0.95, z], 0.025, color);
  box(b, 0.31, 0.93, z - 0.22, 0.37, 0.97, z + 0.22, IRON);
  box(b, -0.3, 0.85, z - 0.06, -0.08, 0.9, z + 0.06, IRON);
}

function bikeRack(b: MeshBuilder): void {
  // Three steel hoops in a row along x, two bikes parked in it.
  for (const x of [-0.6, 0, 0.6]) {
    bar(b, [x - 0.25, 0, 0], [x - 0.25, 0.7, 0], 0.03, STEEL);
    bar(b, [x + 0.25, 0, 0], [x + 0.25, 0.7, 0], 0.03, STEEL);
    bar(b, [x - 0.28, 0.7, 0], [x + 0.28, 0.7, 0], 0.03, STEEL);
  }
  bike(b, -0.3, BIKE[0]);
  bike(b, 0.3, BIKE[1]);
}

function sign(b: MeshBuilder): void {
  // Campus wayfinding sign: post and a blue board with a gold stripe and white text bars.
  box(b, -0.05, 0, -0.05, 0.05, 1.6, 0.05, STEEL);
  box(b, -0.45, 1.1, 0.05, 0.45, 1.75, 0.1, UCI_BLUE);
  box(b, -0.45, 1.65, 0.1, 0.45, 1.7, 0.11, UCI_GOLD);
  for (let i = 0; i < 3; i++) box(b, -0.35, 1.22 + i * 0.13, 0.1, 0.15 + (i % 2) * 0.15, 1.28 + i * 0.13, 0.11, WHITE);
}

function planter(b: MeshBuilder): void {
  box(b, -0.5, 0, -0.5, 0.5, 0.5, 0.5, CONCRETE);
  box(b, -0.42, 0.5, -0.42, 0.42, 0.52, 0.42, SOIL);
  lump(b, 0, 0.5, 0, 0.38, 0.32, 0.38, LEAF);
  lump(b, 0.18, 0.62, 0.12, 0.2, 0.18, 0.2, LEAF_LIGHT);
  FLOWER.forEach((c, i) => lump(b, -0.22 + i * 0.2, 0.95 - (i % 2) * 0.1, -0.15 + i * 0.12, 0.06, 0.05, 0.06, c));
}

function picnicTable(b: MeshBuilder): void {
  box(b, -0.9, 0.72, -0.4, 0.9, 0.78, 0.4, WOOD_LIGHT);
  for (const z of [-0.7, 0.55]) box(b, -0.9, 0.42, z, 0.9, 0.47, z + 0.15, WOOD_LIGHT);
  // A-frame legs at each end.
  for (const x of [-0.7, 0.62]) {
    bar(b, [x + 0.04, 0, -0.6], [x + 0.04, 0.72, 0], 0.04, WOOD);
    bar(b, [x + 0.04, 0, 0.6], [x + 0.04, 0.72, 0], 0.04, WOOD);
    box(b, x, 0.38, -0.72, x + 0.08, 0.42, 0.72, WOOD);
  }
}

function umbrella(b: MeshBuilder): void {
  // Café table with a striped patio umbrella.
  prism(b, 0, 0, 0.25, 0.25, 0, 0.04, 6, IRON);
  prism(b, 0, 0, 0.04, 0.04, 0.04, 0.72, 6, IRON);
  prism(b, 0, 0, 0.42, 0.42, 0.72, 0.76, 8, WHITE);
  prism(b, 0, 0, 0.025, 0.025, 0.76, 2.35, 4, STEEL, false);
  // Canopy: 8 alternating panels, open underneath.
  const n = 8, r = 1.05, y0 = 1.95, y1 = 2.35;
  for (let i = 0; i < n; i++) {
    const a0 = (i / n) * Math.PI * 2, a1 = ((i + 1) / n) * Math.PI * 2;
    const p = [Math.cos(a0) * r, y0, Math.sin(a0) * r], q = [Math.cos(a1) * r, y0, Math.sin(a1) * r];
    const c = i % 2 ? WHITE : RED;
    b.tri([0, y1, 0], p, q, [0, 1, 0], c);
    b.tri([0, y1, 0], p, q, [0, -1, 0], c);
  }
}

function scooter(b: MeshBuilder): void {
  wheel(b, -0.42, 0.1, 0, 0.1, 0.03, TYRE);
  wheel(b, 0.42, 0.1, 0, 0.1, 0.03, TYRE);
  box(b, -0.45, 0.12, -0.08, 0.35, 0.17, 0.08, IRON);
  bar(b, [0.38, 0.15, 0], [0.42, 0.95, 0], 0.03, 0x2aa0a0);
  box(b, 0.39, 0.93, -0.25, 0.45, 0.98, 0.25, IRON);
}

function fence(b: MeshBuilder): void {
  // 2 m of picket fence: two posts, two rails, pickets.
  for (const x of [-1, 0.92]) box(b, x, 0, -0.05, x + 0.08, 1.0, 0.05, WOOD_DARK);
  for (const y of [0.3, 0.75]) box(b, -1, y, -0.03, 1, y + 0.07, 0.01, WOOD);
  for (let i = 0; i < 9; i++) {
    const x = -0.86 + i * 0.2;
    box(b, x, 0.05, 0.01, x + 0.1, 0.92, 0.04, WOOD);
  }
}

function lowWall(b: MeshBuilder): void {
  // 2 m of low brick wall with a concrete cap.
  box(b, -1, 0, -0.2, 1, 0.55, 0.2, BRICK);
  box(b, -1.04, 0.55, -0.24, 1.04, 0.62, 0.24, CONCRETE_DARK);
}

function lantern(b: MeshBuilder, withGlass = true): void {
  // Standing lantern: short post with a hook and a hanging glass lantern (lit by LampLights).
  box(b, -0.04, 0, -0.04, 0.04, 1.4, 0.04, IRON);
  box(b, -0.03, 1.36, -0.03, 0.03, 1.4, 0.3, IRON);
  box(b, -0.11, 0.95, 0.15, 0.11, 0.98, 0.37, IRON);
  if (withGlass) glass(b, 'lantern');
  prism(b, 0, 0.26, 0.14, 0.02, 1.18, 1.3, 4, IRON);
}

function noticeBoard(b: MeshBuilder): void {
  for (const x of [-0.75, 0.67]) box(b, x, 0, -0.05, x + 0.08, 1.9, 0.05, WOOD_DARK);
  box(b, -0.7, 0.8, -0.03, 0.7, 1.75, 0.03, WOOD);
  box(b, -0.62, 0.86, 0.03, 0.62, 1.69, 0.04, 0xc89a5a);
  // Little pitched roof.
  b.tri([-0.85, 1.9, -0.25], [0.85, 1.9, -0.25], [0.85, 2.1, 0], [0, 1, -1], WOOD_DARK);
  b.tri([-0.85, 1.9, -0.25], [0.85, 2.1, 0], [-0.85, 2.1, 0], [0, 1, -1], WOOD_DARK);
  b.tri([-0.85, 1.9, 0.25], [0.85, 1.9, 0.25], [0.85, 2.1, 0], [0, 1, 1], WOOD_DARK);
  b.tri([-0.85, 1.9, 0.25], [0.85, 2.1, 0], [-0.85, 2.1, 0], [0, 1, 1], WOOD_DARK);
  // Pinned notes.
  const notes = [[-0.5, 1.3], [-0.15, 1.45], [0.2, 1.25], [0.42, 1.5], [-0.38, 0.95], [0.1, 0.95]];
  notes.forEach(([x, y], i) => box(b, x, y, 0.04, x + 0.22, y + 0.2, 0.05, PAPER[i % PAPER.length]));
}

/** A wooden crate about 0.6 m on a side; with `fruit`, open and heaped with fruit. */
export function crate(b: MeshBuilder, x = 0, y = 0, z = 0, s = 0.6, fruit?: number): void {
  const h = s / 2;
  box(b, x - h, y, z - h, x + h, y + s * (fruit === undefined ? 1 : 0.6), z + h, WOOD_LIGHT);
  // Dark slats on the front and back.
  box(b, x - h - 0.01, y + s * 0.15, z - h - 0.01, x + h + 0.01, y + s * 0.25, z + h + 0.01, WOOD_DARK);
  if (fruit !== undefined) {
    const top = y + s * 0.6;
    for (let i = 0; i < 4; i++) {
      const fx = x - h * 0.45 + (i % 2) * h * 0.9, fz = z - h * 0.4 + Math.floor(i / 2) * h * 0.8;
      lump(b, fx, top - 0.02, fz, 0.08, 0.07, 0.08, FRUIT[(fruit + i) % FRUIT.length]);
    }
  }
}

function crates(b: MeshBuilder): void {
  crate(b, -0.05, 0, 0, 0.6);
  crate(b, 0.05, 0.6, 0.02, 0.5);
}

/** A tied burlap sack sitting on y. */
export function sack(b: MeshBuilder, x = 0, y = 0, z = 0, s = 1): void {
  prism(b, x, z, 0.24 * s, 0.2 * s, y, y + 0.45 * s, 6, BURLAP);
  prism(b, x, z, 0.2 * s, 0.06 * s, y + 0.45 * s, y + 0.55 * s, 6, BURLAP_DARK, false);
  lump(b, x, y + 0.53 * s, z, 0.08 * s, 0.05 * s, 0.08 * s, BURLAP_DARK);
}

function sacks(b: MeshBuilder): void {
  sack(b, -0.15, 0, 0);
  sack(b, 0.2, 0, 0.1, 0.85);
}

const MODELS: Record<string, (b: MeshBuilder, withGlass?: boolean) => void> = {
  bench, lamp_post: lampPost, bin, bike_rack: bikeRack, sign, planter, picnic_table: picnicTable, umbrella, scooter,
  fence, low_wall: lowWall, lantern, notice_board: noticeBoard, crate: crates, sack: sacks,
};

/** Every prop type the kit can draw. */
export const PROP_KIT = Object.keys(MODELS);

/**
 * Builds the model for one prop type into `b` (at the origin, unrotated). With `withGlass` false, lamp heads
 * (LAMP_HEADS) are left out so they can be drawn with an emissive material.
 */
export function buildProp(b: MeshBuilder, type: string, withGlass = true): void {
  const f = MODELS[type];
  if (!f) throw new Error(`unknown prop type ${type}`);
  f(b, withGlass);
}

/** One prop type's model on its own. */
export function propGeometry(type: string, withGlass = true): THREE.BufferGeometry {
  const b = new MeshBuilder();
  buildProp(b, type, withGlass);
  return b.geometry();
}

/** A lamp type's glass on its own, at the origin and unrotated. */
export function glassGeometry(type: string): THREE.BufferGeometry {
  const b = new MeshBuilder();
  glass(b, type);
  return b.geometry();
}

/**
 * Adds an emissive term to a Lambert shader for vertices whose `glow` attribute is 1, coloured by the
 * `lampGlow` uniform. Lamp glass then glows inside the merged prop meshes without a draw call of its own.
 */
export function addLampGlow(shader: THREE.WebGLProgramParametersWithUniforms, glow: { value: THREE.Color }): void {
  shader.uniforms.lampGlow = glow;
  shader.vertexShader = shader.vertexShader
    .replace('#include <common>', '#include <common>\nattribute float glow;\nvarying float vGlow;')
    .replace('#include <begin_vertex>', '#include <begin_vertex>\nvGlow = glow;');
  shader.fragmentShader = shader.fragmentShader
    .replace('#include <common>', '#include <common>\nuniform vec3 lampGlow;\nvarying float vGlow;')
    .replace('#include <emissivemap_fragment>', '#include <emissivemap_fragment>\ntotalEmissiveRadiance += vGlow * lampGlow;');
}

/**
 * The props from props.json. They never move, so every chunk's props are merged into one mesh with one
 * shared material: the props cost one draw call per 64 m chunk that has any, however many props it holds,
 * and chunks out of view are culled. Lamp glass is marked with a `glow` vertex attribute and shines with
 * `glow.value` (black by day; LampLights sets it at night).
 */
export class Props {
  readonly group = new THREE.Group();
  readonly material = new THREE.MeshLambertMaterial({ vertexColors: true });
  /** Emissive colour of every lamp head; one uniform, so all lamps glow together. */
  readonly glow = { value: new THREE.Color(0x000000) };

  constructor(data: PropsData, chunkSize = 64) {
    this.group.name = 'props';
    this.material.onBeforeCompile = (shader) => addLampGlow(shader, this.glow);
    this.material.customProgramCacheKey = () => 'props-lamp-glow';
    // Each type is its body (glow 0) plus, for lamps, its glass (glow 1).
    const models = data.types.map((t) => {
      const parts: [THREE.BufferGeometry, number][] = [[propGeometry(t, false), 0]];
      if (LAMP_HEADS[t]) parts.push([glassGeometry(t), 1]);
      return parts;
    });
    const chunks = new Map<string, number[][]>();
    for (const p of data.props) {
      const key = `${Math.floor((p[1] * DM) / chunkSize)},${Math.floor((p[2] * DM) / chunkSize)}`;
      let list = chunks.get(key);
      if (!list) chunks.set(key, (list = []));
      list.push(p);
    }
    const m = new THREE.Matrix4();
    const pos = new THREE.Vector3();
    const nrm = new THREE.Vector3();
    const normalMat = new THREE.Matrix3();
    for (const [key, list] of [...chunks].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))) {
      let verts = 0;
      for (const [t] of list) for (const [g] of models[t]) verts += g.getAttribute('position').count;
      const P = new Float32Array(verts * 3), N = new Float32Array(verts * 3), C = new Float32Array(verts * 3);
      const G = new Float32Array(verts);
      let o = 0;
      for (const [t, x, z, rot] of list) {
        // Rotation is clockwise seen from above, in degrees: 0 faces south (+z), 90 faces west.
        m.makeRotationY((-rot * Math.PI) / 180).setPosition(x * DM, 0, z * DM);
        normalMat.getNormalMatrix(m);
        for (const [g, glow] of models[t]) {
          const gp = g.getAttribute('position'), gn = g.getAttribute('normal'), gc = g.getAttribute('color');
          for (let i = 0; i < gp.count; i++, o++) {
            pos.fromBufferAttribute(gp, i).applyMatrix4(m);
            nrm.fromBufferAttribute(gn, i).applyMatrix3(normalMat);
            P.set([pos.x, pos.y, pos.z], o * 3);
            N.set([nrm.x, nrm.y, nrm.z], o * 3);
            C.set([gc.getX(i), gc.getY(i), gc.getZ(i)], o * 3);
            G[o] = glow;
          }
        }
      }
      const g = new THREE.BufferGeometry();
      g.setAttribute('position', new THREE.BufferAttribute(P, 3));
      g.setAttribute('normal', new THREE.BufferAttribute(N, 3));
      g.setAttribute('color', new THREE.BufferAttribute(C, 3));
      g.setAttribute('glow', new THREE.BufferAttribute(G, 1));
      g.computeBoundingSphere();
      const mesh = new THREE.Mesh(g, this.material);
      mesh.name = `props:${key}`;
      mesh.matrixAutoUpdate = false;
      mesh.castShadow = true;
      mesh.receiveShadow = true;
      this.group.add(mesh);
    }
    for (const parts of models) for (const [g] of parts) g.dispose();
  }

  dispose(): void {
    for (const c of this.group.children) (c as THREE.Mesh).geometry.dispose();
    this.material.dispose();
  }
}

let cached: PropsData | null = null;
/** The committed props.json, parsed once. */
export function loadProps(): PropsData {
  return (cached ??= parseProps(propsRaw));
}
