// Converts the committed OSM snapshot into the compact map file, the collision grid, the gameplay areas and the card tables.
//   node shared/map/build.mjs [outDir]      (default: this directory)
// Deterministic: same input -> byte-identical output. No Math.random, no timestamps.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { BBOX, ORIGIN, CHUNK_SIZE, CELL_SIZE } from './config.mjs';
import { ALDRICH_PARK_WAY, SPAWN, SAFE_RADIUS, SHOP, SOUTH_FIELDS, PVP_ZONE, CARD_PLAZA, CARD_TABLES } from './areas.mjs';

const here = fileURLToPath(new URL('.', import.meta.url));
const outDir = process.argv[2] ?? here;
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
};
// Card table plaza, in its own file so areas.json (and the server's area ids) stay as they are.
const cardTables = {
  version: 1,
  units: 'decimetres',
  plaza: dm(CARD_PLAZA),
  tables: CARD_TABLES.map((t) => ({ id: t.id, ...cellCentre(t, `card table ${t.id}`) })),
};

mkdirSync(outDir, { recursive: true });
writeFileSync(outDir + '/campus.json', JSON.stringify(campus) + '\n');
writeFileSync(outDir + '/collision.bin', Buffer.concat([header, Buffer.from(bits)]));
writeFileSync(outDir + '/areas.json', JSON.stringify(areas) + '\n');
writeFileSync(outDir + '/cardtables.json', JSON.stringify(cardTables) + '\n');
console.log(`buildings=${buildings.length} paths=${paths.length} plazas=${plazas.length} green=${green.length} grid=${width}x${height}`);
