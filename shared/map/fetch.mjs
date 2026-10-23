// Downloads the OpenStreetMap snapshot for the central-campus bounding box.
// Run rarely (network needed); the result is committed so build.mjs is offline and reproducible.
//   node shared/map/fetch.mjs
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { BBOX } from './config.mjs';

const { south, west, north, east } = BBOX;
const box = `(${south},${west},${north},${east})`;
const query = `[out:json][timeout:90];(
way["building"]${box};
way["highway"~"footway|path|pedestrian|service|cycleway|steps|residential|living_street|unclassified|tertiary"]${box};
way["leisure"~"park|garden|pitch"]${box};
way["landuse"~"grass|recreation_ground|forest"]${box};
way["natural"~"wood|scrub|grassland|water"]${box};
);out geom;`;

const res = await fetch('https://overpass-api.de/api/interpreter', {
  method: 'POST',
  headers: { 'User-Agent': 'lush-map/1.0', 'Content-Type': 'application/x-www-form-urlencoded' },
  body: 'data=' + encodeURIComponent(query),
});
if (!res.ok) throw new Error(`Overpass ${res.status}`);
const raw = await res.json();
const keep = ['building', 'building:levels', 'height', 'highway', 'leisure', 'landuse', 'natural', 'area', 'width'];
const ways = raw.elements
  .filter((e) => e.type === 'way' && e.geometry)
  .map((e) => ({
    id: e.id,
    tags: Object.fromEntries(keep.filter((k) => k in e.tags).map((k) => [k, e.tags[k]])),
    pts: e.geometry.map((g) => [g.lat, g.lon]),
  }))
  .sort((a, b) => a.id - b.id);
const out = fileURLToPath(new URL('./source/osm-central.json', import.meta.url));
writeFileSync(out, JSON.stringify({ attribution: '© OpenStreetMap contributors (ODbL)', bbox: BBOX, ways }) + '\n');
console.log(`wrote ${ways.length} ways to ${out}`);
