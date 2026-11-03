import * as THREE from 'three';
import { MeshBuilder } from './campusMap';
import { box } from './gameAreas';
import { lump, prism } from './propKit';

// Starter furniture models, wallpapers and floor coverings (#562, housing.md). Keyed by item id, as carried
// by PlacedItem.item_id and HomeState. Each model is centred on the origin in x and z, front toward +z,
// width along x and depth along z at 1 m per tile. Floor and surface pieces stand on y = 0 (surface pieces
// on y = 0 of their slot anchor). Wall pieces lie against the wall plane at z = -0.5 and are thin.

const WOOD = 0xb07a48;
const WOOD_DARK = 0x6b4426;
const OAK = 0xc89a5a;
const EUCA = 0xa86a4e;
const FABRIC_BLUE = 0x3f6fa8;
const FABRIC_GREY = 0x7a8089;
const FABRIC_RED = 0xc8503c;
const CREAM = 0xf0e6cc;
const WHITE = 0xf2eee4;
const METAL = 0x8c939a;
const DARK = 0x2e3236;
const GREEN = 0x4f8a3a;
const POT = 0xb8663c;
const GLASS = 0x9cd0e8;
const SCREEN = 0x1c2a3a;
const GOLD = 0xf0b83c;
const FUR = 0xa8845a;

/** Model sizes stay inside the footprint with a small gap, so neighbours never touch. */
const GAP = 0.04;

type Build = (b: MeshBuilder, hw: number, hd: number) => void;

export interface FurnitureModel {
  /** The model's mesh, flat vertex colours. */
  mesh: THREE.Mesh;
  /** Slot positions on the top face, in the model's local space. */
  anchors: THREE.Vector3[];
}

interface Spec {
  /** Footprint in tiles. */
  w: number;
  d: number;
  /** Surface slots; their anchors sit on a shared top face of height `top`. */
  slots: number;
  top: number;
  build: Build;
}

function legs(b: MeshBuilder, hw: number, hd: number, h: number, t: number, color: number): void {
  for (const sx of [-1, 1]) for (const sz of [-1, 1]) {
    const x = sx * (hw - t / 2), z = sz * (hd - t / 2);
    box(b, x - t / 2, 0, z - t / 2, x + t / 2, h, z + t / 2, color);
  }
}

const table = (h: number, color: number): Build => (b, hw, hd) => {
  legs(b, hw, hd, h, 0.1, color);
  box(b, -hw, h, -hd, hw, h + 0.06, hd, color);
};

const cabinet = (h: number, color: number, knob: number): Build => (b, hw, hd) => {
  box(b, -hw, 0, -hd, hw, h, hd, color);
  box(b, -hw, h, -hd, hw, h + 0.05, hd, knob === 0 ? color : WOOD_DARK);
  for (const s of [-0.5, 0.5]) box(b, s * hw - 0.03, h * 0.5, hd, s * hw + 0.03, h * 0.5 + 0.12, hd + 0.03, knob === 0 ? WOOD_DARK : knob);
};

const bed = (color: number): Build => (b, hw, hd) => {
  box(b, -hw, 0.1, -hd, hw, 0.3, hd, WOOD_DARK);
  box(b, -hw + 0.04, 0.3, -hd + 0.2, hw - 0.04, 0.45, hd - 0.02, color);
  box(b, -hw, 0.1, -hd, hw, 0.8, -hd + 0.1, WOOD_DARK);
  const pillows = hw > 0.6 ? [-hw / 2, hw / 2] : [0];
  for (const x of pillows) box(b, x - 0.2, 0.45, -hd + 0.12, x + 0.2, 0.55, -hd + 0.4, WHITE);
  box(b, -hw, 0, -hd, -hw + 0.08, 0.1, -hd + 0.08, WOOD_DARK);
  box(b, hw - 0.08, 0, -hd, hw, 0.1, -hd + 0.08, WOOD_DARK);
  box(b, -hw, 0, hd - 0.08, -hw + 0.08, 0.1, hd, WOOD_DARK);
  box(b, hw - 0.08, 0, hd - 0.08, hw, 0.1, hd, WOOD_DARK);
};

const rug = (c1: number, c2: number): Build => (b, hw, hd) => {
  box(b, -hw, 0, -hd, hw, 0.02, hd, c1);
  box(b, -hw + 0.15, 0.02, -hd + 0.15, hw - 0.15, 0.03, hd - 0.15, c2);
};

const SPECS: Readonly<Record<number, Spec>> = {
  // Wooden chair
  29: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    legs(b, 0.3, 0.3, 0.4, 0.07, WOOD_DARK);
    box(b, -0.3, 0.4, -0.3, 0.3, 0.47, 0.3, WOOD);
    box(b, -0.3, 0.47, -0.3, 0.3, 0.95, -0.23, WOOD);
  } },
  // Stool
  30: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    prism(b, 0, 0, 0.22, 0.28, 0, 0.45, 6, WOOD_DARK, false);
    prism(b, 0, 0, 0.3, 0.3, 0.45, 0.52, 6, WOOD);
  } },
  // Wooden table
  31: { w: 2, d: 1, slots: 2, top: 0.76, build: table(0.7, WOOD) },
  // Oak dining table
  32: { w: 2, d: 2, slots: 4, top: 0.76, build: table(0.7, OAK) },
  // Desk
  33: { w: 2, d: 1, slots: 2, top: 0.78, build: (b, hw, hd) => {
    table(0.72, WOOD)(b, hw, hd);
    box(b, hw - 0.5, 0.2, -hd + 0.05, hw - 0.05, 0.72, hd - 0.05, WOOD_DARK);
  } },
  // Bookshelf
  34: { w: 2, d: 1, slots: 2, top: 1.5, build: (b, hw, hd) => {
    box(b, -hw, 0, -hd, hw, 1.5, -hd + 0.06, WOOD_DARK);
    for (const x of [-hw, hw - 0.06]) box(b, x, 0, -hd, x + 0.06, 1.5, hd - 0.3, WOOD);
    for (const y of [0, 0.5, 1.0, 1.44]) box(b, -hw, y, -hd, hw, y + 0.06, hd - 0.3, WOOD);
    const books = [0xc8503c, 0x3f6fa8, 0x4f8a3a, 0xf0b83c];
    for (let i = 0; i < 4; i++) for (const y of [0.06, 0.56]) box(b, -hw + 0.2 + i * 0.4, y, -hd + 0.1, -hw + 0.4 + i * 0.4, y + 0.36, -hd + 0.35, books[(i + (y > 0.3 ? 1 : 0)) % 4]);
  } },
  // Single bed
  35: { w: 1, d: 2, slots: 0, top: 0, build: bed(FABRIC_BLUE) },
  // Double bed
  36: { w: 2, d: 2, slots: 0, top: 0, build: bed(FABRIC_RED) },
  // Sofa
  37: { w: 2, d: 1, slots: 0, top: 0, build: (b, hw, hd) => {
    box(b, -hw, 0, -hd, hw, 0.25, hd, FABRIC_GREY);
    box(b, -hw + 0.2, 0.25, -hd + 0.25, hw - 0.2, 0.45, hd, 0x8a9199);
    box(b, -hw, 0.25, -hd, hw, 0.85, -hd + 0.25, FABRIC_GREY);
    for (const x of [-hw, hw - 0.2]) box(b, x, 0.25, -hd + 0.25, x + 0.2, 0.6, hd, FABRIC_GREY);
  } },
  // Beanbag
  38: { w: 1, d: 1, slots: 0, top: 0, build: (b) => lump(b, 0, 0, 0, 0.42, 0.28, 0.42, FABRIC_RED) },
  // Rug (small)
  39: { w: 2, d: 2, slots: 0, top: 0, build: rug(0x3f6fa8, CREAM) },
  // Rug (large)
  40: { w: 3, d: 3, slots: 0, top: 0, build: rug(0x8a3a3a, 0xd8b868) },
  // Floor lamp
  41: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    prism(b, 0, 0, 0.18, 0.18, 0, 0.04, 6, DARK);
    prism(b, 0, 0, 0.03, 0.03, 0.04, 1.35, 4, DARK);
    prism(b, 0, 0, 0.26, 0.16, 1.3, 1.65, 6, 0xf4e2a0);
  } },
  // Eucalyptus wardrobe
  42: { w: 2, d: 1, slots: 0, top: 0, build: (b, hw, hd) => {
    box(b, -hw, 0, -hd, hw, 1.9, hd, EUCA);
    box(b, -0.02, 0.1, hd, 0.02, 1.8, hd + 0.02, WOOD_DARK);
    for (const x of [-0.12, 0.12]) box(b, x - 0.02, 0.9, hd, x + 0.02, 1.1, hd + 0.04, GOLD);
  } },
  // Eucalyptus cabinet
  43: { w: 1, d: 1, slots: 1, top: 0.9, build: cabinet(0.85, EUCA, 0) },
  // Potted plant
  44: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    prism(b, 0, 0, 0.2, 0.28, 0, 0.35, 6, POT);
    lump(b, 0, 0.3, 0, 0.32, 0.3, 0.32, GREEN);
    lump(b, 0.12, 0.5, 0.05, 0.2, 0.25, 0.2, 0x6fa84a);
  } },
  // Cactus
  45: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    prism(b, 0, 0, 0.14, 0.18, 0, 0.14, 6, POT);
    prism(b, 0, 0, 0.09, 0.09, 0.14, 0.5, 6, 0x3f8a5a);
    prism(b, 0.14, 0, 0.05, 0.05, 0.28, 0.4, 5, 0x3f8a5a);
  } },
  // Desk lamp
  46: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    prism(b, 0, 0, 0.12, 0.12, 0, 0.03, 6, DARK);
    prism(b, 0, 0, 0.02, 0.02, 0.03, 0.34, 4, DARK);
    prism(b, 0, 0.05, 0.14, 0.07, 0.3, 0.45, 6, GOLD);
  } },
  // Laptop
  47: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    box(b, -0.25, 0, -0.18, 0.25, 0.03, 0.18, METAL);
    box(b, -0.25, 0.03, -0.18, 0.25, 0.3, -0.15, DARK);
    box(b, -0.22, 0.05, -0.15, 0.22, 0.28, -0.14, SCREEN);
  } },
  // Stack of books
  48: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    box(b, -0.2, 0, -0.15, 0.2, 0.07, 0.15, 0xc8503c);
    box(b, -0.18, 0.07, -0.13, 0.2, 0.14, 0.15, 0x3f6fa8);
    box(b, -0.2, 0.14, -0.15, 0.17, 0.2, 0.13, 0x4f8a3a);
  } },
  // Coffee mug
  49: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    prism(b, 0, 0, 0.07, 0.07, 0, 0.12, 6, WHITE);
    box(b, 0.07, 0.03, -0.015, 0.11, 0.09, 0.015, WHITE);
  } },
  // Fish bowl
  50: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    prism(b, 0, 0, 0.14, 0.14, 0, 0.04, 6, WOOD_DARK);
    prism(b, 0, 0, 0.2, 0.2, 0.04, 0.34, 6, GLASS);
    lump(b, 0, 0.1, 0, 0.07, 0.04, 0.04, 0xf09a28);
  } },
  // Mini fridge
  51: { w: 1, d: 1, slots: 1, top: 0.9, build: (b, hw, hd) => {
    box(b, -hw, 0, -hd, hw, 0.9, hd, WHITE);
    box(b, -hw, 0.55, hd, hw, 0.57, hd + 0.01, METAL);
    box(b, hw - 0.12, 0.6, hd, hw - 0.08, 0.8, hd + 0.04, METAL);
    box(b, hw - 0.12, 0.2, hd, hw - 0.08, 0.4, hd + 0.04, METAL);
  } },
  // TV stand
  52: { w: 2, d: 1, slots: 2, top: 0.5, build: (b, hw, hd) => {
    box(b, -hw, 0, -hd, hw, 0.08, hd, WOOD_DARK);
    box(b, -hw, 0.42, -hd, hw, 0.5, hd, WOOD);
    for (const x of [-hw, -0.02, hw - 0.06]) box(b, x, 0.08, -hd, x + 0.06, 0.42, hd, WOOD);
  } },
  // Small TV
  53: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    box(b, -0.15, 0, -0.1, 0.15, 0.03, 0.1, DARK);
    box(b, -0.03, 0.03, -0.03, 0.03, 0.12, 0.03, DARK);
    box(b, -0.4, 0.12, -0.05, 0.4, 0.57, 0.05, DARK);
    box(b, -0.36, 0.16, 0.05, 0.36, 0.53, 0.06, SCREEN);
  } },
  // Wall clock
  54: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    box(b, -0.32, 0.18, -0.5, 0.32, 0.82, -0.44, WOOD_DARK);
    box(b, -0.26, 0.24, -0.44, 0.26, 0.76, -0.43, WHITE);
    box(b, -0.015, 0.5, -0.43, 0.015, 0.7, -0.42, DARK);
    box(b, -0.015, 0.485, -0.43, 0.15, 0.515, -0.42, DARK);
  } },
  // Poster
  55: { w: 1, d: 1, slots: 0, top: 0, build: (b) => {
    box(b, -0.35, 0.1, -0.5, 0.35, 0.9, -0.47, WOOD_DARK);
    box(b, -0.3, 0.15, -0.47, 0.3, 0.85, -0.46, 0x3f6fa8);
    box(b, -0.15, 0.3, -0.46, 0.15, 0.6, -0.45, GOLD);
  } },
  // Window
  56: { w: 2, d: 1, slots: 0, top: 0, build: (b) => {
    box(b, -0.9, 0.05, -0.5, 0.9, 0.95, -0.4, WHITE);
    box(b, -0.8, 0.12, -0.4, 0.8, 0.88, -0.38, GLASS);
    box(b, -0.03, 0.12, -0.38, 0.03, 0.88, -0.36, WHITE);
  } },
  // Trophy shelf
  57: { w: 2, d: 1, slots: 0, top: 0, build: (b) => {
    box(b, -0.9, 0.3, -0.5, 0.9, 0.7, -0.45, WOOD_DARK);
    box(b, -0.9, 0.3, -0.5, 0.9, 0.34, -0.33, WOOD);
    for (const x of [-0.5, 0, 0.5]) {
      prism(b, x, -0.42, 0.05, 0.08, 0.34, 0.5, 6, GOLD);
    }
  } },
  // Mounted coyote pelt
  58: { w: 2, d: 1, slots: 0, top: 0, build: (b) => {
    box(b, -0.8, 0.1, -0.5, 0.8, 0.9, -0.44, WOOD_DARK);
    box(b, -0.7, 0.15, -0.44, 0.7, 0.85, -0.4, FUR);
    box(b, -0.35, 0.3, -0.4, 0.35, 0.7, -0.38, 0xc8a876);
  } },
};

const FURNITURE_FIRST = 29;
const FURNITURE_LAST = 58;

/** Item ids of the starter furniture (29-58). */
export const FURNITURE_IDS: readonly number[] = Array.from({ length: FURNITURE_LAST - FURNITURE_FIRST + 1 }, (_, i) => FURNITURE_FIRST + i);

export function isWallItem(id: number): boolean {
  return id >= 54 && id <= 58;
}

function spec(id: number): Spec {
  const s = SPECS[id];
  if (!s) throw new Error(`unknown furniture item ${id}`);
  return s;
}

const SLOT_SPACING = 1;

/** Positions of the surface slots on the top face of an item, in model space. */
export function surfaceAnchors(id: number): THREE.Vector3[] {
  const s = spec(id);
  const out: THREE.Vector3[] = [];
  for (let i = 0; i < s.slots; i++) {
    // Slots fill a w x d grid of tiles, one slot per tile, tile centres.
    const cols = s.w, rows = Math.ceil(s.slots / cols);
    const cx = (i % cols) - (cols - 1) / 2, cz = Math.floor(i / cols) - (rows - 1) / 2;
    out.push(new THREE.Vector3(cx * SLOT_SPACING, s.top, cz * SLOT_SPACING));
  }
  return out;
}

/** The geometry of a furniture item, flat vertex colours. */
export function furnitureGeometry(id: number): THREE.BufferGeometry {
  const s = spec(id);
  const b = new MeshBuilder();
  s.build(b, s.w / 2 - GAP, s.d / 2 - GAP);
  return b.geometry(false);
}

let furnitureMaterial: THREE.MeshLambertMaterial | null = null;
function sharedMaterial(): THREE.MeshLambertMaterial {
  return (furnitureMaterial ??= new THREE.MeshLambertMaterial({ vertexColors: true }));
}

/** A mesh and its slot anchors for a furniture item id. */
export function buildFurniture(id: number): FurnitureModel {
  const mesh = new THREE.Mesh(furnitureGeometry(id), sharedMaterial());
  mesh.name = `furniture_${id}`;
  return { mesh, anchors: surfaceAnchors(id) };
}

// ---------- Wallpapers and floors ----------

interface SurfaceSpec {
  base: number;
  /** Optional 2nd colour and pattern, drawn into a small tiling texture. */
  accent?: number;
  pattern?: 'stripes' | 'check' | 'boards' | 'tiles';
}

const SURFACES: Readonly<Record<number, SurfaceSpec>> = {
  59: { base: 0xf2eee4 }, // White wallpaper
  60: { base: 0xf0e2bc }, // Cream wallpaper
  61: { base: 0x9cc8e8 }, // Sky blue wallpaper
  62: { base: 0xa8c4a0 }, // Sage green wallpaper
  63: { base: 0xe8d8c0, accent: 0xa4553c, pattern: 'stripes' }, // Brick stripes wallpaper
  64: { base: 0xf2eee4, accent: 0x3f6fa8, pattern: 'check' }, // Blue check wallpaper
  65: { base: 0xd8b078, accent: 0xb8905a, pattern: 'boards' }, // Pale oak boards floor
  66: { base: 0x5a3a28, accent: 0x442a1c, pattern: 'boards' }, // Dark boards floor
  67: { base: 0x9aa0a6, accent: 0x80868c, pattern: 'tiles' }, // Grey tile floor
  68: { base: 0xf2eee4, accent: 0x2e3236, pattern: 'check' }, // Checker tile floor
  69: { base: 0x3f7a4a }, // Green carpet floor
  70: { base: 0xd8c08a }, // Sand carpet floor
};

export const WALLPAPER_IDS: readonly number[] = [59, 60, 61, 62, 63, 64];
export const FLOOR_IDS: readonly number[] = [65, 66, 67, 68, 69, 70];

const TEX = 8;

function patternTexture(s: SurfaceSpec): THREE.DataTexture {
  const a = new THREE.Color(s.base), c = new THREE.Color(s.accent ?? s.base);
  const data = new Uint8Array(TEX * TEX * 4);
  for (let y = 0; y < TEX; y++) for (let x = 0; x < TEX; x++) {
    const half = TEX / 2;
    let alt = false;
    if (s.pattern === 'stripes') alt = x < 2;
    else if (s.pattern === 'check') alt = (x < half) !== (y < half);
    else if (s.pattern === 'boards') alt = y % half === 0 || x === (y < half ? 0 : half);
    else if (s.pattern === 'tiles') alt = x === 0 || y === 0;
    const k = alt ? c : a;
    const i = (y * TEX + x) * 4;
    data[i] = Math.round(k.r * 255);
    data[i + 1] = Math.round(k.g * 255);
    data[i + 2] = Math.round(k.b * 255);
    data[i + 3] = 255;
  }
  const t = new THREE.DataTexture(data, TEX, TEX, THREE.RGBAFormat);
  t.wrapS = t.wrapT = THREE.RepeatWrapping;
  t.magFilter = t.minFilter = THREE.NearestFilter;
  t.colorSpace = THREE.SRGBColorSpace;
  t.needsUpdate = true;
  return t;
}

const surfaceCache = new Map<number, THREE.MeshLambertMaterial>();

/** The material of a wallpaper or floor covering id: a flat colour, or a small tiling pattern. */
export function surfaceMaterial(id: number): THREE.MeshLambertMaterial {
  const s = SURFACES[id];
  if (!s) throw new Error(`unknown wallpaper or floor ${id}`);
  let m = surfaceCache.get(id);
  if (!m) {
    m = s.pattern ? new THREE.MeshLambertMaterial({ map: patternTexture(s) }) : new THREE.MeshLambertMaterial({ color: s.base });
    m.name = `surface_${id}`;
    surfaceCache.set(id, m);
  }
  return m;
}
