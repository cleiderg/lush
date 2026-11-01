// Converts the committed OSM snapshot into the compact map file, the collision grid, the gameplay areas, the card tables,
// the M8 fountain, the props, the M9 skill objects and the Aldrich Park nature.
//   node shared/map/build.mjs [outDir] [--no-nature]      (default: this directory; --no-nature skips parknature.json)
// Deterministic: same input -> byte-identical output. No Math.random, no timestamps.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { BBOX, ORIGIN, CHUNK_SIZE, CELL_SIZE } from './config.mjs';
import {
  ALDRICH_PARK_WAY, SPAWN, SAFE_RADIUS, SHOP, SOUTH_FIELDS, PVP_ZONE, CARD_PLAZA, CARD_TABLES, STUDENT_CENTER_WAY, CAFE,
} from './areas.mjs';
import {
  PROP_SEED, PROP_TYPES, BUSY_CELL, BUSY_TYPES, SPAWN_CLEAR, SHOP_CLEAR, TABLE_CLEAR, AREA_CLUSTER,
  FOOD_SEED, FOOD_CARTS, STRING_LIGHTS, STRING_SPAN, FOOD_AREAS, FOOD_CLEAR, WALL_CLEAR, TABLETOP_SPOTS, TABLETOP_REACH,
} from './props.mjs';
import {
  NATURE_SEED, NATURE_TYPES, NATURE_BLOCK, NATURE_COVER_SHARE, TREES_PER_BLOCK, BUSHES_PER_BLOCK, ROCKS_PER_BLOCK,
  FLOWER_BEDS_PER_BLOCK, PICNIC_BLANKETS, PICNIC_RING, BIRDS, BIRD_RING, NATURE_CLEAR, BANK_BOOTH, GRILL,
} from './parknature.mjs';
import {
  SKILLS_SEED, SKILL_TREES, SKILL_TREE_R, SKILL_TREE_SPACING, SAFE_MARGIN, POND_CENTRE, POND_RADII, POND_POINTS, POND_WOBBLE,
  POND_SHORE, POND_SHIFT, FISHING_SPOTS, STATION_CLEAR,
} from './skills.mjs';
import { FOUNTAIN_R, FOUNTAIN_PATH_CLEAR, FOUNTAIN_CLEAR, FOUNTAIN_STEP } from './fountain.mjs';

const here = fileURLToPath(new URL('.', import.meta.url));
const args = process.argv.slice(2);
const outDir = args.find((a) => !a.startsWith('--')) ?? here;
const withNature = !args.includes('--no-nature');
const src = JSON.parse(readFileSync(here + 'source/osm-central.json', 'utf8'));

const M_PER_DEG = 111320;
const cosLat = Math.cos((ORIGIN.lat * Math.PI) / 180);
/** lat/lon -> metres (x east, z south) from the origin, rounded to decimetres. */
const project = ([lat, lon]) => [
  Math.round((lon - ORIGIN.lon) * M_PER_DEG * cosLat * 10),
  Math.round(-(lat - ORIGIN.lat) * M_PER_DEG * 10),
];

/** Seeded PRNG (mulberry32), used so heights are stable per OSM id. */
function rng(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const PATH_WIDTH_DM = {
  footway: 20, path: 15, steps: 15, cycleway: 25, pedestrian: 40, service: 40,
  living_street: 60, residential: 60, unclassified: 60, tertiary: 70,
};
/** Path surface kind (`s` in campus.json): 0 dirt, 1 stone, 2 paved. */
const SURFACE = { dirt: 0, stone: 1, paved: 2 };
const PATH_SURFACE = {
  path: SURFACE.dirt, footway: SURFACE.stone, steps: SURFACE.stone, cycleway: SURFACE.stone, pedestrian: SURFACE.stone,
};

/** Project, drop consecutive duplicates, flatten to [x0,z0,x1,z1,...]. */
function flat(pts, closed) {
  const out = [];
  for (const p of pts.map(project)) {
    const n = out.length;
    if (n && out[n - 2] === p[0] && out[n - 1] === p[1]) continue;
    out.push(p[0], p[1]);
  }
  if (closed && out.length > 2 && out[0] === out[out.length - 2] && out[1] === out[out.length - 1]) out.length -= 2;
  return out;
}

const isClosed = (pts) => pts.length > 3 && pts[0][0] === pts[pts.length - 1][0] && pts[0][1] === pts[pts.length - 1][1];

const buildings = [];
const paths = [];
const plazas = [];
const green = [];
const GREEN_KIND = { grass: 0, wood: 1, water: 2, pitch: 3 };

for (const w of src.ways) {
  const t = w.tags;
  if (t.building) {
    const p = flat(w.pts, true);
    if (p.length < 6) continue;
    let h = parseFloat(t.height);
    if (!(h > 0)) h = parseFloat(t['building:levels']) * 3;
    if (!(h > 0)) h = 4 + Math.floor(rng(w.id)() * 5);
    buildings.push({ id: w.id, h: Math.round(Math.min(h, 40) * 10), p });
  } else if (t.highway === 'pedestrian' && t.area === 'yes' && isClosed(w.pts)) {
    const p = flat(w.pts, true);
    if (p.length >= 6) plazas.push({ id: w.id, p });
  } else if (t.highway) {
    const width = PATH_WIDTH_DM[t.highway];
    const p = flat(w.pts, false);
    if (!width || p.length < 4) continue;
    const w10 = t.width && parseFloat(t.width) > 0 ? Math.round(parseFloat(t.width) * 10) : width;
    paths.push({ id: w.id, w: w10, s: PATH_SURFACE[t.highway] ?? SURFACE.paved, p });
  } else {
    let kind;
    if (t.natural === 'water') kind = 'water';
    else if (t.natural === 'wood' || t.natural === 'scrub' || t.landuse === 'forest') kind = 'wood';
    else if (t.leisure === 'pitch') kind = 'pitch';
    else if (t.leisure || t.landuse || t.natural) kind = 'grass';
    const p = flat(w.pts, true);
    if (kind && p.length >= 6) green.push({ id: w.id, k: GREEN_KIND[kind], p });
  }
}

// Bounds in whole chunks around the bbox.
const corner = (lat, lon) => project([lat, lon]);
const sw = corner(BBOX.south, BBOX.west);
const ne = corner(BBOX.north, BBOX.east);
const chunkDm = CHUNK_SIZE * 10;
const minX = Math.floor(sw[0] / chunkDm) * chunkDm;
const maxX = Math.ceil(ne[0] / chunkDm) * chunkDm;
const minZ = Math.floor(ne[1] / chunkDm) * chunkDm;
const maxZ = Math.ceil(sw[1] / chunkDm) * chunkDm;

const campus = {
  version: 1,
  attribution: '© OpenStreetMap contributors (ODbL)',
  units: 'decimetres',
  origin: ORIGIN,
  chunkSize: CHUNK_SIZE,
  bounds: { minX, minZ, maxX, maxZ },
  buildings,
  paths,
  plazas,
  green,
};

// Collision grid: cell blocked when its centre is inside a building or water.
const cellDm = CELL_SIZE * 10;
const width = (maxX - minX) / cellDm;
const height = (maxZ - minZ) / cellDm;
const bits = new Uint8Array(Math.ceil((width * height) / 8));
function fill(poly) {
  const n = poly.length / 2;
  let lo = Infinity, hi = -Infinity;
  for (let i = 1; i < poly.length; i += 2) { lo = Math.min(lo, poly[i]); hi = Math.max(hi, poly[i]); }
  const r0 = Math.max(0, Math.ceil((lo - minZ) / cellDm - 0.5));
  const r1 = Math.min(height - 1, Math.floor((hi - minZ) / cellDm - 0.5));
  for (let r = r0; r <= r1; r++) {
    const z = minZ + (r + 0.5) * cellDm;
    const xs = [];
    for (let i = 0, j = n - 1; i < n; j = i++) {
      const zi = poly[2 * i + 1], zj = poly[2 * j + 1];
      if (zi > z !== zj > z) {
        const xi = poly[2 * i], xj = poly[2 * j];
        xs.push(xi + ((z - zi) * (xj - xi)) / (zj - zi));
      }
    }
    xs.sort((a, b) => a - b);
    for (let k = 0; k + 1 < xs.length; k += 2) {
      const c0 = Math.max(0, Math.ceil((xs[k] - minX) / cellDm - 0.5));
      const c1 = Math.min(width - 1, Math.floor((xs[k + 1] - minX) / cellDm - 0.5));
      for (let c = c0; c <= c1; c++) {
        const idx = r * width + c;
        bits[idx >> 3] |= 1 << (idx & 7);
      }
    }
  }
}
for (const b of buildings) fill(b.p);
for (const g of green) if (g.k === GREEN_KIND.water) fill(g.p);

// Student Center café (areas.mjs CAFE): open its floor and door cells inside the building; the building's cells
// round the floor stay blocked as its walls. Fails unless the room is wholly inside the footprint, walled on every
// side but the door, and the door leads to a walkable cell outside.
const cellIdx = (x, z) => Math.floor((z * 10 - minZ) / cellDm) * width + Math.floor((x * 10 - minX) / cellDm);
const cellBit = (x, z) => { const i = cellIdx(x, z); return (bits[i >> 3] >> (i & 7)) & 1; };
const studentCenter = buildings.find((b) => b.id === STUDENT_CENTER_WAY);
if (!studentCenter) throw new Error(`building way ${STUDENT_CENTER_WAY} (Student Center) missing`);
const inStudentCenter = (x, z) => {
  const px = x * 10, pz = z * 10, p = studentCenter.p;
  let r = false;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    if (p[i + 1] > pz !== p[j + 1] > pz && px < p[i] + ((pz - p[i + 1]) * (p[j] - p[i])) / (p[j + 1] - p[i + 1])) r = !r;
  }
  return r;
};
const inRect = (x, z, [x0, z0, x1, z1]) => x >= x0 && x < x1 && z >= z0 && z < z1;
{
  const [fx0, fz0, fx1, fz1] = CAFE.floor;
  const open = [];
  for (let z = fz0 - 1; z <= fz1; z++) {
    for (let x = fx0 - 1; x <= fx1; x++) {
      const cx = x + 0.5, cz = z + 0.5;
      if (!inStudentCenter(cx, cz) || !cellBit(cx, cz)) throw new Error(`café cell (${x}, ${z}) is not inside the Student Center`);
      if (inRect(x, z, CAFE.floor) || inRect(x, z, CAFE.door)) open.push(cellIdx(cx, cz));
    }
  }
  const [dx0, dz0, dx1, dz1] = CAFE.door;
  let outside = 0;
  for (let z = dz0; z < dz1; z++) for (let x = dx0; x < dx1; x++) {
    if (inRect(x, z, CAFE.floor)) throw new Error(`café door cell (${x}, ${z}) is on the floor`);
    for (const [ox, oz] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
      const nx = x + ox, nz = z + oz;
      if (!inRect(nx, nz, CAFE.floor) && !inRect(nx, nz, CAFE.door) && !cellBit(nx + 0.5, nz + 0.5)) outside++;
    }
  }
  if (!outside) throw new Error('café door does not lead to a walkable cell outside');
  for (const i of open) bits[i >> 3] &= ~(1 << (i & 7));
}

// Header (22 bytes, little-endian) then the packed bits. See README.md.
const header = Buffer.alloc(22);
header.write('LCOL', 0, 'ascii');
header.writeUInt8(1, 4);
header.writeUInt8(0, 5);
header.writeUInt16LE(cellDm, 6);
header.writeUInt16LE(CHUNK_SIZE / CELL_SIZE, 8);
header.writeUInt16LE(width, 10);
header.writeUInt16LE(height, 12);
header.writeInt32LE(minX, 14);
header.writeInt32LE(minZ, 18);

// Gameplay areas (areas.mjs). Points snap to the centre of their collision cell and must be walkable.
function cellCentre({ x, z }, name) {
  const c = Math.floor((x * 10 - minX) / cellDm);
  const r = Math.floor((z * 10 - minZ) / cellDm);
  const idx = r * width + c;
  if (c < 0 || r < 0 || c >= width || r >= height || (bits[idx >> 3] >> (idx & 7)) & 1) {
    throw new Error(`${name} at (${x}, ${z}) is not on a walkable cell`);
  }
  return { x: minX + c * cellDm + cellDm / 2, z: minZ + r * cellDm + cellDm / 2 };
}
const park = green.find((g) => g.id === ALDRICH_PARK_WAY);
if (!park) throw new Error(`green way ${ALDRICH_PARK_WAY} (Aldrich Park) missing`);
const dm = (pts) => pts.flatMap(([x, z]) => [Math.round(x * 10), Math.round(z * 10)]);
const areas = {
  version: 1,
  units: 'decimetres',
  spawn: cellCentre(SPAWN, 'spawn'),
  safeRadius: Math.round(SAFE_RADIUS * 10),
  shop: cellCentre(SHOP, 'shop'),
  areas: [
    { id: 'aldrich_park', kind: 'pve', osm: ALDRICH_PARK_WAY, p: park.p },
    { id: 'south_fields', kind: 'pve', p: dm(SOUTH_FIELDS) },
    { id: 'pvp_zone', kind: 'pvp', p: dm(PVP_ZONE) },
  ],
  interiors: [
    { id: CAFE.id, kind: 'safe', p: dm([[CAFE.floor[0], CAFE.floor[1]], [CAFE.floor[2], CAFE.floor[1]], [CAFE.floor[2], CAFE.floor[3]], [CAFE.floor[0], CAFE.floor[3]]]) },
  ],
};
// Card table plaza, in its own file so areas.json (and the server's area ids) stay as they are.
const cardTables = {
  version: 1,
  units: 'decimetres',
  plaza: dm(CARD_PLAZA),
  tables: CARD_TABLES.map((t) => ({ id: t.id, ...cellCentre(t, `card table ${t.id}`) })),
};

// The M8 fountain (fountain.mjs): one round basin in the paved plaza nearest the spawn, clear of the safe-circle places,
// paths and buildings. Its cells are blocked (water is not walkable) before anything else is placed, so props and nature avoid it.
function placeFountain() {
  const spots = [cellCentre(SPAWN, 'spawn'), cellCentre(SHOP, 'shop'), cellCentre(BANK_BOOTH, 'bank booth'), cellCentre(GRILL, 'grill')]
    .map((c) => ({ x: c.x / 10, z: c.z / 10 }));
  const cardPlazaDm = dm(CARD_PLAZA);
  const edges = (p) => Array.from({ length: p.length / 2 }, (_, i) => {
    const j = (i + 1) % (p.length / 2);
    return [p[2 * i] / 10, p[2 * i + 1] / 10, p[2 * j] / 10, p[2 * j + 1] / 10];
  });
  const segD = ([x0, z0, x1, z1], x, z) => {
    const dx = x1 - x0, dz = z1 - z0, len2 = dx * dx + dz * dz;
    const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, ((x - x0) * dx + (z - z0) * dz) / len2));
    return Math.hypot(x0 + t * dx - x, z0 + t * dz - z);
  };
  const polyDist = (p, x, z) => (insideDm(x, z, p) ? 0 : Math.min(...edges(p).map((e) => segD(e, x, z))));
  const pathSegs = paths.flatMap((w) => Array.from({ length: w.p.length / 2 - 1 }, (_, i) => ({
    e: [w.p[2 * i] / 10, w.p[2 * i + 1] / 10, w.p[2 * i + 2] / 10, w.p[2 * i + 3] / 10], h: w.w / 20,
  })));
  const walls = buildings.flatMap((b) => edges(b.p));
  const R = FOUNTAIN_R, pad = FOUNTAIN_PATH_CLEAR;
  // Plazas by distance from the spawn (then id), skipping any too close to the safe-circle places.
  const order = plazas
    .map((q) => ({ q, d: polyDist(q.p, spots[0].x, spots[0].z) }))
    .sort((a, b) => a.d - b.d || a.q.id - b.q.id);
  for (const { q } of order) {
    const e = edges(q.p);
    let x0 = Infinity, z0 = Infinity, x1 = -Infinity, z1 = -Infinity, cx = 0, cz = 0;
    for (const [x, z] of e) { x0 = Math.min(x0, x); x1 = Math.max(x1, x); z0 = Math.min(z0, z); z1 = Math.max(z1, z); cx += x; cz += z; }
    cx /= e.length; cz /= e.length;
    const segs = pathSegs.filter((s) => Math.min(s.e[0], s.e[2]) - s.h < x1 + R + pad && Math.max(s.e[0], s.e[2]) + s.h > x0 - R - pad &&
      Math.min(s.e[1], s.e[3]) - s.h < z1 + R + pad && Math.max(s.e[1], s.e[3]) + s.h > z0 - R - pad);
    const near = walls.filter((w) => Math.min(w[0], w[2]) < x1 + R + pad && Math.max(w[0], w[2]) > x0 - R - pad &&
      Math.min(w[1], w[3]) < z1 + R + pad && Math.max(w[1], w[3]) > z0 - R - pad);
    let best = null;
    const step = FOUNTAIN_STEP;
    for (let z = Math.ceil(z0 / step) * step; z <= z1; z += step) {
      for (let x = Math.ceil(x0 / step) * step; x <= x1; x += step) {
        if (!insideDm(x, z, q.p) || e.some((s) => segD(s, x, z) < R + pad)) continue;
        if (segs.some((s) => segD(s.e, x, z) - s.h < R + pad) || near.some((w) => segD(w, x, z) < R + pad)) continue;
        if (blockedAt(x, z) || inBuilding(x, z)) continue;
        if (spots.some((c) => Math.hypot(x - c.x, z - c.z) < R + FOUNTAIN_CLEAR) || polyDist(cardPlazaDm, x, z) < R + FOUNTAIN_CLEAR) continue;
        const d = Math.hypot(x - cx, z - cz);
        if (!best || d < best.d - 1e-9) best = { x, z, d };
      }
    }
    if (best) {
      // Snap to a cell centre so the blocked disk is symmetric on the grid.
      const c = cellCentre(best, 'fountain');
      const out = { version: 1, units: 'decimetres', plaza: q.id, x: c.x, z: c.z, r: Math.round(R * 10) };
      fillDisk(c.x, c.z, out.r);
      return out;
    }
  }
  throw new Error('no plaza has room for the fountain');
}
/** Block every cell whose centre is within r (decimetres) of (x, z). */
function fillDisk(x, z, r) {
  for (let row = 0; row < height; row++) {
    const cz = minZ + (row + 0.5) * cellDm;
    if (Math.abs(cz - z) > r) continue;
    for (let col = 0; col < width; col++) {
      const cx = minX + (col + 0.5) * cellDm;
      if (Math.hypot(cx - x, cz - z) > r) continue;
      const idx = row * width + col;
      bits[idx >> 3] |= 1 << (idx & 7);
    }
  }
}

// Props (props.mjs): visual only, kept clear of the M9 bank booth and grill (skills.mjs), so they never change collision.bin. Placed with one seeded generator in a fixed order.
const blockedAt = (x, z) => {
  const c = Math.floor((x * 10 - minX) / cellDm);
  const r = Math.floor((z * 10 - minZ) / cellDm);
  if (c < 0 || r < 0 || c >= width || r >= height) return true;
  const idx = r * width + c;
  return ((bits[idx >> 3] >> (idx & 7)) & 1) === 1;
};
/** Point (metres) inside polygon [x0,z0,...] in decimetres. */
function insideDm(x, z, p) {
  const px = x * 10, pz = z * 10;
  let inside = false;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    const xi = p[i], zi = p[i + 1], xj = p[j], zj = p[j + 1];
    if (zi > pz !== zj > pz && px < xi + ((pz - zi) * (xj - xi)) / (zj - zi)) inside = !inside;
  }
  return inside;
}
const buildingBoxes = buildings.map((b) => {
  let x0 = Infinity, z0 = Infinity, x1 = -Infinity, z1 = -Infinity;
  for (let i = 0; i < b.p.length; i += 2) {
    x0 = Math.min(x0, b.p[i]); x1 = Math.max(x1, b.p[i]); z0 = Math.min(z0, b.p[i + 1]); z1 = Math.max(z1, b.p[i + 1]);
  }
  return { p: b.p, x0, z0, x1, z1 };
});
const inBuilding = (x, z) => buildingBoxes.some((b) =>
  x * 10 >= b.x0 && x * 10 <= b.x1 && z * 10 >= b.z0 && z * 10 <= b.z1 && insideDm(x, z, b.p));
/** Share of the 10 x 10 one-metre samples in the square [x0, x0+size) x [z0, z0+size) that are walkable. */
function walkableShare(x0, z0, size) {
  let free = 0;
  for (let i = 0; i < size; i++) for (let j = 0; j < size; j++) if (!blockedAt(x0 + i + 0.5, z0 + j + 0.5)) free++;
  return free / (size * size);
}

// The fountain's basin is blocked here, before the props, skill objects and nature are placed.
const fountain = placeFountain();
const spawnM = { x: areas.spawn.x / 10, z: areas.spawn.z / 10 };
const shopM = { x: areas.shop.x / 10, z: areas.shop.z / 10 };
const tablesM = cardTables.tables.map((t) => ({ x: t.x / 10, z: t.z / 10 }));
// M9 bank booth and grill on their cell centres (decimetres), written to skills.json.
const bankDm = cellCentre(BANK_BOOTH, 'bank booth'), grillDm = cellCentre(GRILL, 'grill');
const stationsM = [bankDm, grillDm].map((c) => ({ x: c.x / 10, z: c.z / 10 }));
const plazaCentre = {
  x: CARD_PLAZA.reduce((a, [x]) => a + x, 0) / CARD_PLAZA.length,
  z: CARD_PLAZA.reduce((a, [, z]) => a + z, 0) / CARD_PLAZA.length,
};
const typeId = Object.fromEntries(PROP_TYPES.map((t, i) => [t.name, i]));
const placed = []; // {t, x, z, rot}, metres
function free(t, x, z) {
  const r = PROP_TYPES[t].r;
  if (blockedAt(x, z) || inBuilding(x, z)) return false;
  if (Math.hypot(x - spawnM.x, z - spawnM.z) < SPAWN_CLEAR + r) return false;
  const sx = x - shopM.x, sz = z - shopM.z;
  if (sx > SHOP_CLEAR.x0 - r && sx < SHOP_CLEAR.x1 + r && sz > SHOP_CLEAR.z0 - r && sz < SHOP_CLEAR.z1 + r) return false;
  if (tablesM.some((c) => Math.abs(x - c.x) < TABLE_CLEAR + r && Math.abs(z - c.z) < TABLE_CLEAR + r)) return false;
  if (stationsM.some((c) => Math.hypot(x - c.x, z - c.z) < STATION_CLEAR + r)) return false;
  return placed.every((p) => Math.hypot(x - p.x, z - p.z) >= r + PROP_TYPES[p.t].r);
}
const rand = rng(PROP_SEED);
const rotFor = (t) => (PROP_TYPES[t].square ? Math.floor(rand() * 4) * 90 : Math.floor(rand() * 360));
/** Try to place type t at a random free point (decimetre grid) in the square; true on success. */
function placeIn(t, x0, z0, size) {
  for (let k = 0; k < 120; k++) {
    const x = (Math.round(x0 * 10) + 1 + Math.floor(rand() * (size * 10 - 1))) / 10;
    const z = (Math.round(z0 * 10) + 1 + Math.floor(rand() * (size * 10 - 1))) / 10;
    if (free(t, x, z)) {
      placed.push({ t, x, z, rot: rotFor(t) });
      return true;
    }
  }
  return false;
}
const typesIn = (x0, z0, size) => new Set(placed.filter((p) => p.x >= x0 && p.x < x0 + size && p.z >= z0 && p.z < z0 + size).map((p) => p.t));

// Busy areas: every 10 x 10 m cell of the 3 x 3 block around each centre that is at least half walkable gets BUSY_TYPES types.
const busy = [
  { name: 'spawn', c: spawnM, shop: false },
  { name: 'shop', c: shopM, shop: true },
  { name: 'card plaza', c: plazaCentre, shop: false },
];
for (const { name, c, shop } of busy) {
  const pool = PROP_TYPES.map((_, i) => i).filter((i) => (shop || !PROP_TYPES[i].shop) && !PROP_TYPES[i].food);
  for (let dz = -1; dz <= 1; dz++) {
    for (let dx = -1; dx <= 1; dx++) {
      const x0 = c.x - BUSY_CELL / 2 + dx * BUSY_CELL, z0 = c.z - BUSY_CELL / 2 + dz * BUSY_CELL;
      if (walkableShare(x0, z0, BUSY_CELL) < 0.5) continue;
      const have = typesIn(x0, z0, BUSY_CELL);
      // Shuffle the pool (Fisher-Yates) so each cell gets a different mix.
      const order = pool.slice();
      for (let i = order.length - 1; i > 0; i--) {
        const j = Math.floor(rand() * (i + 1));
        [order[i], order[j]] = [order[j], order[i]];
      }
      for (const t of order) {
        if (have.size >= BUSY_TYPES) break;
        if (!have.has(t) && placeIn(t, x0, z0, BUSY_CELL)) have.add(t);
      }
      if (have.size < BUSY_TYPES) throw new Error(`${name}: cell (${x0}, ${z0}) only fits ${have.size} prop types`);
    }
  }
}
// Every gameplay area gets at least a small cluster near the walkable point closest to its centre.
for (const a of areas.areas) {
  if (placed.some((p) => insideDm(p.x, p.z, a.p))) continue;
  let cx = 0, cz = 0;
  for (let i = 0; i < a.p.length; i += 2) { cx += a.p[i] / 10; cz += a.p[i + 1] / 10; }
  cx = Math.round((cx * 2) / a.p.length); cz = Math.round((cz * 2) / a.p.length);
  let done = 0;
  for (let ring = 0; ring < 60 && !done; ring++) {
    const x0 = cx - 4 - ring, z0 = cz - 4 - ring, size = 8 + 2 * ring;
    if (!insideDm(x0 + size / 2, z0 + size / 2, a.p)) continue;
    const before = placed.length;
    for (const n of AREA_CLUSTER) placeIn(typeId[n], x0, z0, size);
    // Keep only what landed inside the area.
    for (let i = placed.length - 1; i >= before; i--) if (!insideDm(placed[i].x, placed[i].z, a.p)) placed.splice(i, 1);
    done = placed.length - before;
  }
  if (!done) throw new Error(`area ${a.id} has no room for props`);
}
placeFood();
/**
 * Food and fun (#428), with its own seeded generator: a food cart and a string light in each of FOOD_AREAS, then
 * coffee cups, plates and pizza boxes on the picnic tables near the busy areas and on the carts' serving ledges.
 * Every food prop keeps FOOD_CLEAR from the spawn, shop stall, bank booth and grill and WALL_CLEAR from building walls.
 */
function placeFood() {
  const foodRand = rng(FOOD_SEED);
  const edges = buildingBoxes.flatMap((b) => {
    const out = [];
    for (let i = 0; i < b.p.length; i += 2) {
      const j = (i + 2) % b.p.length;
      out.push([b.p[i] / 10, b.p[i + 1] / 10, b.p[j] / 10, b.p[j + 1] / 10]);
    }
    return out;
  });
  const segDist = ([x0, z0, x1, z1], x, z) => {
    const dx = x1 - x0, dz = z1 - z0, len2 = dx * dx + dz * dz;
    const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, ((x - x0) * dx + (z - z0) * dz) / len2));
    return Math.hypot(x0 + t * dx - x, z0 + t * dz - z);
  };
  const nearWall = (x, z, d) => edges.some((e) =>
    x >= Math.min(e[0], e[2]) - d && x <= Math.max(e[0], e[2]) + d && z >= Math.min(e[1], e[3]) - d && z <= Math.max(e[1], e[3]) + d &&
    segDist(e, x, z) < d);
  /** A food prop of footprint r may stand at (x, z): walkable, clear of walls and of the spawn, shop, bank booth and grill. */
  const clear = (x, z, r) => {
    for (const [ox, oz] of [[0, 0], [r, 0], [-r, 0], [0, r], [0, -r]]) if (blockedAt(x + ox, z + oz) || inBuilding(x + ox, z + oz)) return false;
    if (nearWall(x, z, WALL_CLEAR + r)) return false;
    if ([spawnM, BANK_BOOTH, GRILL].some((c) => Math.hypot(x - c.x, z - c.z) < FOOD_CLEAR + r)) return false;
    const sx = x - shopM.x, sz = z - shopM.z, m = FOOD_CLEAR + r;
    if (sx > SHOP_CLEAR.x0 - m && sx < SHOP_CLEAR.x1 + m && sz > SHOP_CLEAR.z0 - m && sz < SHOP_CLEAR.z1 + m) return false;
    return !tablesM.some((c) => Math.abs(x - c.x) < TABLE_CLEAR + r && Math.abs(z - c.z) < TABLE_CLEAR + r);
  };
  const apart = (x, z, r) => footprints(placed).every((f) => Math.hypot(x - f.x, z - f.z) >= r + f.r);
  const pick = (c) => [
    Math.round((c.x - 1.5 * BUSY_CELL + foodRand() * 3 * BUSY_CELL) * 10) / 10,
    Math.round((c.z - 1.5 * BUSY_CELL + foodRand() * 3 * BUSY_CELL) * 10) / 10,
  ];
  const t = (n) => typeId[n];
  for (const name of FOOD_AREAS) {
    const { c } = busy.find((b) => b.name === name);
    for (let n = 0; n < FOOD_CARTS; n++) {
      let ok = false;
      for (let k = 0; k < 4000 && !ok; k++) {
        const [x, z] = pick(c);
        const r = PROP_TYPES[t('food_cart')].r;
        if (!clear(x, z, r) || !apart(x, z, r)) continue;
        placed.push({ t: t('food_cart'), x, z, rot: Math.floor(foodRand() * 4) * 90 });
        ok = true;
      }
      if (!ok) throw new Error(`${name}: no room for a food cart`);
    }
    for (let n = 0; n < STRING_LIGHTS; n++) {
      let ok = false;
      for (let k = 0; k < 4000 && !ok; k++) {
        const [x, z] = pick(c);
        const rot = Math.floor(foodRand() * 180);
        const p = { t: t('string_lights'), x, z, rot };
        const [a, b] = poles(p);
        const r = PROP_TYPES[p.t].r;
        if (!clear(a.x, a.z, r) || !clear(b.x, b.z, r) || !apart(a.x, a.z, r) || !apart(b.x, b.z, r)) continue;
        // The wire runs over open ground: no wall, and no lamp post or cart canopy under it.
        const tall = placed.filter((q) => ['lamp_post', 'food_cart', 'umbrella'].includes(PROP_TYPES[q.t].name));
        const wireOk = wire(p).every((w) => !blockedAt(w.x, w.z) && !nearWall(w.x, w.z, 0.5) &&
          tall.every((q) => Math.hypot(w.x - q.x, w.z - q.z) >= PROP_TYPES[q.t].r + 0.3));
        if (!wireOk) continue;
        placed.push(p);
        ok = true;
      }
      if (!ok) throw new Error(`${name}: no room for string lights`);
    }
  }
  // Table-top items: the first spot of every host always, the second about half the time; types taken in turn.
  const tops = PROP_TYPES.map((_, i) => i).filter((i) => PROP_TYPES[i].top);
  for (let i = tops.length - 1; i > 0; i--) {
    const j = Math.floor(foodRand() * (i + 1));
    [tops[i], tops[j]] = [tops[j], tops[i]];
  }
  const hosts = placed.filter((p) => {
    const name = PROP_TYPES[p.t].name;
    if (name === 'food_cart') return true;
    return name === 'picnic_table' && busy.some((b) => Math.hypot(p.x - b.c.x, p.z - b.c.z) < TABLETOP_REACH);
  });
  let next = 0;
  for (const h of hosts) {
    TABLETOP_SPOTS[PROP_TYPES[h.t].name].forEach(([lx, lz], s) => {
      if (s > 0 && foodRand() < 0.5) return;
      const ty = tops[next % tops.length];
      const w = toWorld(h, lx, lz);
      const x = Math.round(w.x * 10) / 10, z = Math.round(w.z * 10) / 10;
      const jitter = Math.floor(foodRand() * 30) - 15;
      if (!clear(x, z, PROP_TYPES[ty].r)) return;
      placed.push({ t: ty, x, z, rot: (h.rot + jitter + 360) % 360 });
      next++;
    });
  }
}
/** A point in a prop's own frame (metres) in world metres; rotation is clockwise seen from above, as in the client. */
function toWorld(p, lx, lz) {
  const a = (-p.rot * Math.PI) / 180;
  return { x: p.x + lx * Math.cos(a) + lz * Math.sin(a), z: p.z - lx * Math.sin(a) + lz * Math.cos(a) };
}
/** The two poles of a string light. */
function poles(p) {
  return [toWorld(p, -STRING_SPAN / 2, 0), toWorld(p, STRING_SPAN / 2, 0)];
}
/** Points every half metre along a string light's wire. */
function wire(p) {
  return Array.from({ length: STRING_SPAN * 2 + 1 }, (_, i) => toWorld(p, -STRING_SPAN / 2 + i / 2, 0));
}
/** Ground footprints {x, z, r} of placed props: a string light stands on its two poles, every other prop on its centre. */
function footprints(list) {
  return list.flatMap((p) => (PROP_TYPES[p.t].name === 'string_lights'
    ? poles(p).map((q) => ({ ...q, r: PROP_TYPES[p.t].r }))
    : [{ x: p.x, z: p.z, r: PROP_TYPES[p.t].r }]));
}

const props = {
  version: 1,
  units: 'decimetres',
  types: PROP_TYPES.map((t) => t.name),
  // [type, x, z, rotation in whole degrees] per prop.
  props: placed.map((p) => [p.t, Math.round(p.x * 10), Math.round(p.z * 10), p.rot]),
};

// Walkable park grass, shared by the skill objects and the nature.
// Paths, plazas and the card plaza, with boxes in metres for quick rejection.
const segs = [];
for (const w of paths) {
  for (let i = 0; i + 3 < w.p.length; i += 2) {
    const [x0, z0, x1, z1] = [w.p[i] / 10, w.p[i + 1] / 10, w.p[i + 2] / 10, w.p[i + 3] / 10];
    const h = w.w / 20;
    segs.push({ x0, z0, x1, z1, h, b: [Math.min(x0, x1) - h, Math.min(z0, z1) - h, Math.max(x0, x1) + h, Math.max(z0, z1) + h] });
  }
}
const pavedDm = [...plazas.map((q) => q.p), cardTables.plaza];
const paved = pavedDm.map((p) => {
  let x0 = Infinity, z0 = Infinity, x1 = -Infinity, z1 = -Infinity;
  for (let i = 0; i < p.length; i += 2) {
    x0 = Math.min(x0, p[i] / 10); x1 = Math.max(x1, p[i] / 10); z0 = Math.min(z0, p[i + 1] / 10); z1 = Math.max(z1, p[i + 1] / 10);
  }
  return { p, b: [x0, z0, x1, z1] };
});
const near = (b, x, z, pad) => x >= b[0] - pad && x <= b[2] + pad && z >= b[1] - pad && z <= b[3] + pad;
const segDist = (s, x, z) => {
  const dx = s.x1 - s.x0, dz = s.z1 - s.z0, len2 = dx * dx + dz * dz;
  const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, ((x - s.x0) * dx + (z - s.z0) * dz) / len2));
  return Math.hypot(s.x0 + t * dx - x, s.z0 + t * dz - z);
};
// Segments bucketed on a coarse grid, each listed in every bucket its box (plus the largest cap) touches.
const BUCKET = 8, CAP = 4;
const buckets = new Map();
for (const s of segs) {
  for (let bz = Math.floor((s.b[1] - CAP) / BUCKET); bz <= Math.floor((s.b[3] + CAP) / BUCKET); bz++) {
    for (let bx = Math.floor((s.b[0] - CAP) / BUCKET); bx <= Math.floor((s.b[2] + CAP) / BUCKET); bx++) {
      const k = `${bx},${bz}`;
      if (!buckets.has(k)) buckets.set(k, []);
      buckets.get(k).push(s);
    }
  }
}
/** Metres of clearance past the edge of the nearest path (negative on a path), up to `cap` (at most CAP). */
const pathClear = (x, z, cap) => {
  let best = cap;
  for (const s of buckets.get(`${Math.floor(x / BUCKET)},${Math.floor(z / BUCKET)}`) ?? []) {
    if (near(s.b, x, z, cap)) best = Math.min(best, segDist(s, x, z) - s.h);
  }
  return best;
};
const onPaved = (x, z) => paved.some((q) => near(q.b, x, z, 0) && insideDm(x, z, q.p));
/** Walkable park grass: in the park, on a walkable cell, off paths, plazas and the card plaza. The test uses the same rule. */
const grassAt = (x, z) => insideDm(x, z, park.p) && !blockedAt(x, z) && pathClear(x, z, 0.01) > 0 && !onPaved(x, z);

// Milestone 9 skill objects (skills.mjs): woodcutting trees, the pond and its fishing spots, the bank booth and the grill.
// Placed after the props and before the nature, with their own seeded generator, so they don't depend on --no-nature.
const skills = placeSkills();
/** Distance (metres) from (x, z) to the edge of a closed polygon in decimetres. */
function edgeDist(p, x, z) {
  let best = Infinity;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    best = Math.min(best, segDist({ x0: p[j] / 10, z0: p[j + 1] / 10, x1: p[i] / 10, z1: p[i + 1] / 10 }, x, z));
  }
  return best;
}
/** Point (metres) is in the pond or within `pad` metres of its edge. */
function nearPond(x, z, pad) {
  return insideDm(x, z, skills.pond.p) || edgeDist(skills.pond.p, x, z) < pad;
}
function placeSkills() {
  const skRand = rng(SKILLS_SEED);
  const safe = areas.safeRadius / 10;
  const outsideSafe = (x, z, pad) => Math.hypot(x - spawnM.x, z - spawnM.z) >= safe + pad;
  const propFeet = footprints(placed);
  const clearOfProps = (x, z, pad) => propFeet.every((f) => Math.hypot(x - f.x, z - f.z) >= f.r + pad);
  const snap = (v, origin) => (origin + (Math.floor((v * 10 - origin) / cellDm) + 0.5) * cellDm) / 10;
  const pt = ({ x, z }) => ({ x: Math.round(x * 10), z: Math.round(z * 10) });
  const station = (s, name) => {
    if (Math.hypot(s.x / 10 - spawnM.x, s.z / 10 - spawnM.z) >= safe) throw new Error(`${name} is outside the safe circle`);
    return s;
  };

  // The pond: a wobbly ellipse, moved outward from POND_CENTRE until it and its shore are open park grass.
  const shape = Array.from({ length: POND_POINTS }, (_, i) => {
    const a = (i / POND_POINTS) * Math.PI * 2, k = 1 + POND_WOBBLE * (2 * skRand() - 1);
    return [Math.cos(a) * POND_RADII.x * k, Math.sin(a) * POND_RADII.z * k];
  });
  const offsets = [];
  for (let dz = -POND_SHIFT; dz <= POND_SHIFT; dz++) for (let dx = -POND_SHIFT; dx <= POND_SHIFT; dx++) {
    if (Math.hypot(dx, dz) <= POND_SHIFT) offsets.push([dx, dz]);
  }
  offsets.sort((a, b) => Math.hypot(...a) - Math.hypot(...b) || a[1] - b[1] || a[0] - b[0]);
  let pond = null;
  for (const [dx, dz] of offsets) {
    const cx = POND_CENTRE.x + dx, cz = POND_CENTRE.z + dz;
    const p = shape.flatMap(([x, z]) => [Math.round((cx + x) * 10), Math.round((cz + z) * 10)]);
    let ok = true;
    const pad = POND_SHORE + 1;
    for (let z = cz - POND_RADII.z * 2 - pad; ok && z <= cz + POND_RADII.z * 2 + pad; z += 0.5) {
      for (let x = cx - POND_RADII.x * 2 - pad; ok && x <= cx + POND_RADII.x * 2 + pad; x += 0.5) {
        if (!insideDm(x, z, p) && edgeDist(p, x, z) >= POND_SHORE + 1) continue;
        ok = grassAt(x, z) && outsideSafe(x, z, SAFE_MARGIN) && clearOfProps(x, z, 0.3) && pathClear(x, z, 0.3) >= 0.3;
      }
    }
    if (ok) { pond = p; break; }
  }
  if (!pond) throw new Error(`no room for the pond within ${POND_SHIFT} m of (${POND_CENTRE.x}, ${POND_CENTRE.z})`);

  // Fishing spot candidates evenly round the outline by length, kinds taken in turn, each with walkable shore outside it.
  const n = pond.length / 2;
  const edges = Array.from({ length: n }, (_, i) => {
    const j = (i + 1) % n;
    const e = { x0: pond[2 * i] / 10, z0: pond[2 * i + 1] / 10, x1: pond[2 * j] / 10, z1: pond[2 * j + 1] / 10 };
    return { ...e, len: Math.hypot(e.x1 - e.x0, e.z1 - e.z0) };
  });
  const perimeter = edges.reduce((a, e) => a + e.len, 0);
  const kinds = [];
  for (let k = 0; k < Math.max(...FISHING_SPOTS.map((f) => f.candidates)); k++) {
    for (const f of FISHING_SPOTS) if (k < f.candidates) kinds.push(f.kind);
  }
  const seen = {};
  const spots = kinds.map((kind, k) => {
    let d = ((k + 0.5) / kinds.length) * perimeter, e = edges[0];
    for (e of edges) { if (d <= e.len) break; d -= e.len; }
    const t = d / e.len;
    const s = { x: Math.round((e.x0 + t * (e.x1 - e.x0)) * 10) / 10, z: Math.round((e.z0 + t * (e.z1 - e.z0)) * 10) / 10 };
    seen[kind] = (seen[kind] ?? 0) + 1;
    const id = `${kind}-${seen[kind]}`;
    const shore = [[-1, -1], [0, -1], [1, -1], [-1, 0], [1, 0], [-1, 1], [0, 1], [1, 1]].some(([ox, oz]) => {
      const x = snap(s.x + ox, minX), z = snap(s.z + oz, minZ);
      return !blockedAt(x, z) && !insideDm(x, z, pond);
    });
    if (!shore) throw new Error(`fishing spot ${id} has no walkable cell next to it`);
    return { id, kind, ...pt(s) };
  });

  // Woodcutting trees, on cell centres of open park grass outside the safe circle, spread out and clear of the pond.
  const trees = [];
  let px0 = Infinity, pz0 = Infinity, px1 = -Infinity, pz1 = -Infinity;
  for (let i = 0; i < park.p.length; i += 2) {
    px0 = Math.min(px0, park.p[i] / 10); px1 = Math.max(px1, park.p[i] / 10);
    pz0 = Math.min(pz0, park.p[i + 1] / 10); pz1 = Math.max(pz1, park.p[i + 1] / 10);
  }
  const r = SKILL_TREE_R;
  const treeFits = (x, z) => {
    if (!grassAt(x, z) || !outsideSafe(x, z, SAFE_MARGIN)) return false;
    for (const [ox, oz] of [[r, 0], [-r, 0], [0, r], [0, -r]]) {
      if (!grassAt(x + ox, z + oz) || inBuilding(x + ox, z + oz)) return false;
    }
    for (const [ox, oz] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) if (blockedAt(x + ox, z + oz)) return false;
    if (pathClear(x, z, r + 0.3) < r + 0.3 || !clearOfProps(x, z, r + 0.3)) return false;
    if (insideDm(x, z, pond) || edgeDist(pond, x, z) < POND_SHORE + r) return false;
    return trees.every((t) => Math.hypot(x - t.x, z - t.z) >= SKILL_TREE_SPACING);
  };
  for (const { kind, count, minX: kx = -Infinity } of SKILL_TREES) {
    const x0 = Math.max(px0, kx);
    for (let k = 1; k <= count; k++) {
      let ok = false;
      for (let tries = 0; tries < 5000 && !ok; tries++) {
        const x = snap(x0 + skRand() * (px1 - x0), minX), z = snap(pz0 + skRand() * (pz1 - pz0), minZ);
        if (x < kx || !treeFits(x, z)) continue;
        trees.push({ id: `${kind}-${k}`, kind, x, z });
        ok = true;
      }
      if (!ok) throw new Error(`no room for skill tree ${kind}-${k}`);
    }
  }

  return {
    version: 1,
    units: 'decimetres',
    bank: station(bankDm, 'bank booth'),
    grill: station(grillDm, 'grill'),
    trees: trees.map((t) => ({ id: t.id, kind: t.kind, ...pt(t) })),
    pond: {
      p: pond,
      active: Object.fromEntries(FISHING_SPOTS.map((f) => [f.kind, f.active])),
      spots,
    },
  };
}

// Aldrich Park nature (parknature.mjs): visual only, like the props, and placed after them and the skill objects with its own
// seeded generator, clear of the skill trees and the pond.
let nature = null;
if (withNature) nature = placeNature();
function placeNature() {
  const natRand = rng(NATURE_SEED);
  const between = ([lo, hi]) => lo + natRand() * (hi - lo);
  const typeOf = Object.fromEntries(NATURE_TYPES.map((t, i) => [t.name, i]));
  const trees = new Set(['tree_oak', 'tree_pine', 'tree_eucalyptus'].map((n) => typeOf[n]));
  // Props, plus the ground under every string light's wire so no tree grows through it.
  const props = [
    ...footprints(placed),
    ...placed.filter((p) => PROP_TYPES[p.t].name === 'string_lights').flatMap((p) => wire(p).map((w) => ({ ...w, r: 1.5 }))),
  ];
  const items = []; // {t, x, z, rot, scale, stretch}, metres
  const CELL = 4;
  const hash = new Map();
  const cellKey = (x, z) => `${Math.floor(x / CELL)},${Math.floor(z / CELL)}`;
  const reach = (it) => NATURE_TYPES[it.t].space * it.scale;
  const clearOfNature = (t, x, z, scale) => {
    const me = NATURE_TYPES[t].space * scale;
    const cx = Math.floor(x / CELL), cz = Math.floor(z / CELL);
    for (let dz = -3; dz <= 3; dz++) {
      for (let dx = -3; dx <= 3; dx++) {
        for (const o of hash.get(`${cx + dx},${cz + dz}`) ?? []) {
          const need = trees.has(t) && trees.has(o.t) ? me + reach(o) : Math.max(me, reach(o));
          if (Math.hypot(x - o.x, z - o.z) < need) return false;
        }
      }
    }
    return true;
  };
  const clearPoints = [spawnM, BANK_BOOTH, GRILL];
  const skillTreesM = skills.trees.map((s) => ({ x: s.x / 10, z: s.z / 10 }));
  function fits(t, x, z, scale) {
    const r = NATURE_TYPES[t].r * scale;
    if (!insideDm(x, z, park.p) || !grassAt(x, z)) return false;
    for (const [ox, oz] of [[r, 0], [-r, 0], [0, r], [0, -r]]) {
      if (blockedAt(x + ox, z + oz) || inBuilding(x + ox, z + oz) || onPaved(x + ox, z + oz)) return false;
    }
    if (pathClear(x, z, r + 0.3) < r + 0.3) return false;
    if (clearPoints.some((c) => Math.hypot(x - c.x, z - c.z) < NATURE_CLEAR + r)) return false;
    const sx = x - shopM.x, sz = z - shopM.z, m = NATURE_CLEAR + r;
    if (sx > SHOP_CLEAR.x0 - m && sx < SHOP_CLEAR.x1 + m && sz > SHOP_CLEAR.z0 - m && sz < SHOP_CLEAR.z1 + m) return false;
    if (props.some((p) => Math.abs(x - p.x) < 4 && Math.abs(z - p.z) < 4 && Math.hypot(x - p.x, z - p.z) < p.r + r + 0.3)) return false;
    // Skill objects: decorative canopies stay off the skill trees, and the pond keeps its open shore.
    const skillGap = trees.has(t) ? SKILL_TREE_R + NATURE_TYPES[t].space * scale : SKILL_TREE_R + r + 1;
    if (skillTreesM.some((s) => Math.hypot(x - s.x, z - s.z) < skillGap)) return false;
    if (nearPond(x, z, POND_SHORE + r)) return false;
    return clearOfNature(t, x, z, scale);
  }
  function add(t, x, z, scale) {
    const it = { t, x, z, rot: Math.floor(natRand() * 360), scale, stretch: between(NATURE_TYPES[t].stretch) };
    items.push(it);
    const k = cellKey(x, z);
    if (!hash.has(k)) hash.set(k, []);
    hash.get(k).push(it);
  }
  /** Try to place type t at a random decimetre point picked by `pick`; true on success. */
  function tryPlace(t, pick, tries = 60) {
    for (let k = 0; k < tries; k++) {
      const scale = Math.round(between(NATURE_TYPES[t].scale) * 100) / 100;
      const [x, z] = pick().map((v) => Math.round(v * 10) / 10);
      if (fits(t, x, z, scale)) {
        add(t, x, z, scale);
        return true;
      }
    }
    return false;
  }
  const count = (perBlock) => Math.floor(perBlock) + (natRand() < perBlock % 1 ? 1 : 0);

  // Blocks over the park, row by row. Each gets trees, bushes, rocks and flower beds in proportion to its grass.
  let px0 = Infinity, pz0 = Infinity, px1 = -Infinity, pz1 = -Infinity;
  for (let i = 0; i < park.p.length; i += 2) {
    px0 = Math.min(px0, park.p[i] / 10); px1 = Math.max(px1, park.p[i] / 10);
    pz0 = Math.min(pz0, park.p[i + 1] / 10); pz1 = Math.max(pz1, park.p[i + 1] / 10);
  }
  const B = NATURE_BLOCK;
  const treeKinds = ['tree_oak', 'tree_pine', 'tree_eucalyptus'].map((n) => typeOf[n]);
  const rockKinds = ['rock_boulder', 'rock_flat'].map((n) => typeOf[n]);
  for (let bz = Math.floor(pz0 / B) * B; bz < pz1; bz += B) {
    for (let bx = Math.floor(px0 / B) * B; bx < px1; bx += B) {
      let grassN = 0;
      for (let i = 0; i < B; i++) for (let j = 0; j < B; j++) if (grassAt(bx + i + 0.5, bz + j + 0.5)) grassN++;
      if (grassN === 0) continue;
      const share = grassN / (B * B);
      const pick = () => [bx + natRand() * B, bz + natRand() * B];
      const plan = [];
      for (let n = count(TREES_PER_BLOCK * share); n > 0; n--) plan.push(treeKinds[Math.floor(natRand() * treeKinds.length)]);
      for (let n = count(BUSHES_PER_BLOCK * share); n > 0; n--) plan.push(typeOf.bush);
      for (let n = count(ROCKS_PER_BLOCK * share); n > 0; n--) plan.push(rockKinds[Math.floor(natRand() * rockKinds.length)]);
      for (let n = count(FLOWER_BEDS_PER_BLOCK * share); n > 0; n--) plan.push(typeOf.flower_bed);
      for (const t of plan) tryPlace(t, pick);
      if (share < NATURE_COVER_SHARE) continue;
      const covered = () => items.some((it) => it.x >= bx && it.x < bx + B && it.z >= bz && it.z < bz + B &&
        (trees.has(it.t) || rockKinds.includes(it.t) || it.t === typeOf.bush));
      for (const t of [typeOf.bush, rockKinds[0], treeKinds[0]]) if (!covered()) tryPlace(t, pick, 400);
      if (!covered()) throw new Error(`park block (${bx}, ${bz}) has no room for a tree, rock or bush`);
    }
  }
  // A few picnic blankets and small groups of birds around the spawn.
  const ring = ([lo, hi]) => () => {
    const a = natRand() * Math.PI * 2, d = lo + natRand() * (hi - lo);
    return [spawnM.x + Math.cos(a) * d, spawnM.z + Math.sin(a) * d];
  };
  for (let n = 0; n < PICNIC_BLANKETS; n++) {
    if (!tryPlace(typeOf.picnic_blanket, ring(PICNIC_RING), 2000)) throw new Error('no room for a picnic blanket');
  }
  for (let n = 0; n < BIRDS;) {
    if (!tryPlace(typeOf.bird, ring(BIRD_RING), 2000)) throw new Error('no room for a bird');
    n++;
    // Up to two more birds peck nearby.
    const lead = items[items.length - 1];
    for (let k = Math.floor(natRand() * 3); k > 0 && n < BIRDS; k--) {
      if (tryPlace(typeOf.bird, () => [lead.x + (natRand() - 0.5) * 3, lead.z + (natRand() - 0.5) * 3], 40)) n++;
    }
  }
  return {
    version: 1,
    units: 'decimetres',
    types: NATURE_TYPES.map((t) => t.name),
    // [type, x, z, rotation in whole degrees, scale in percent, extra height stretch in percent] per placement.
    items: items.map((it) => [it.t, Math.round(it.x * 10), Math.round(it.z * 10), it.rot, Math.round(it.scale * 100), Math.round(it.stretch * 100)]),
  };
}

mkdirSync(outDir, { recursive: true });
writeFileSync(outDir + '/campus.json', JSON.stringify(campus) + '\n');
writeFileSync(outDir + '/collision.bin', Buffer.concat([header, Buffer.from(bits)]));
writeFileSync(outDir + '/areas.json', JSON.stringify(areas) + '\n');
writeFileSync(outDir + '/cardtables.json', JSON.stringify(cardTables) + '\n');
writeFileSync(outDir + '/fountain.json', JSON.stringify(fountain) + '\n');
writeFileSync(outDir + '/props.json', JSON.stringify(props) + '\n');
writeFileSync(outDir + '/skills.json', JSON.stringify(skills) + '\n');
if (nature) writeFileSync(outDir + '/parknature.json', JSON.stringify(nature) + '\n');
console.log(`buildings=${buildings.length} paths=${paths.length} plazas=${plazas.length} green=${green.length} grid=${width}x${height} fountain=${fountain.x / 10},${fountain.z / 10} props=${placed.length} skillTrees=${skills.trees.length} nature=${nature ? nature.items.length : 'off'}`);
