// Downloads the OpenStreetMap snapshots. Run rarely (network needed); the results are committed so build.mjs is
// offline and reproducible.
//   node shared/map/fetch.mjs [central|housing]     (default: both)
//   node shared/map/fetch.mjs housing --from raw.json   build the housing snapshot from a saved Overpass response
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { BBOX, HOUSING } from './config.mjs';

const args = process.argv.slice(2);
const only = args.find((a) => a === 'central' || a === 'housing');
const fromIdx = args.indexOf('--from');
const fromFile = fromIdx >= 0 ? args[fromIdx + 1] : null;

const filters = (box) => `
way["building"]${box};
way["highway"~"footway|path|pedestrian|service|cycleway|steps|residential|living_street|unclassified|tertiary"]${box};
way["leisure"~"park|garden|pitch"]${box};
way["landuse"~"grass|recreation_ground|forest"]${box};
way["natural"~"wood|scrub|grassland|water"]${box};`;
const keep = ['building', 'building:levels', 'height', 'highway', 'leisure', 'landuse', 'natural', 'area', 'width'];

async function overpass(query) {
  const res = await fetch('https://overpass-api.de/api/interpreter', {
    method: 'POST',
    headers: { 'User-Agent': 'lush-map/1.0', 'Content-Type': 'application/x-www-form-urlencoded' },
    body: 'data=' + encodeURIComponent(query),
  });
  if (!res.ok) throw new Error(`Overpass ${res.status}`);
  return res.json();
}

const toWays = (elements, tagKeys = keep) =>
  elements
    .filter((e) => e.type === 'way' && e.geometry)
    .map((e) => ({
      id: e.id,
      tags: Object.fromEntries(tagKeys.filter((k) => k in e.tags).map((k) => [k, e.tags[k]])),
      pts: e.geometry.map((g) => [g.lat, g.lon]),
    }))
    .sort((a, b) => a.id - b.id);

const write = (name, data) => {
  const out = fileURLToPath(new URL(`./source/${name}`, import.meta.url));
  writeFileSync(out, JSON.stringify(data) + '\n');
  console.log(`wrote ${data.ways.length} ways to ${out}`);
};
const attribution = '© OpenStreetMap contributors (ODbL)';

if (!only || only === 'central') {
  if (fromFile) throw new Error('--from only applies to housing');
  const { south, west, north, east } = BBOX;
  const raw = await overpass(`[out:json][timeout:90];(${filters(`(${south},${west},${north},${east})`)}\n);out geom;`);
  write('osm-central.json', { attribution, bbox: BBOX, ways: toWays(raw.elements) });
}

if (!only || only === 'housing') {
  const { south, west, north, east } = HOUSING.fetchBox;
  const ids = [...HOUSING.areas.map((a) => a.outline), ...HOUSING.connecting];
  const raw = fromFile
    ? JSON.parse(readFileSync(fromFile, 'utf8'))
    : await overpass(`[out:json][timeout:90];(${filters(`(${south},${west},${north},${east})`)}\n${ids.map((i) => `way(${i});`).join('')}\n);out geom;`);
  const ways = toWays(raw.elements);
  const inside = (p, [lat, lon]) => {
    let r = false;
    for (let i = 0, j = p.length - 1; i < p.length; j = i++) {
      if (p[i][1] > lon !== p[j][1] > lon && lat < ((p[j][0] - p[i][0]) * (lon - p[i][1])) / (p[j][1] - p[i][1]) + p[i][0]) r = !r;
    }
    return r;
  };
  const outlines = HOUSING.areas.map((a) => {
    const w = ways.find((x) => x.id === a.outline);
    if (!w) throw new Error(`outline way ${a.outline} (${a.id}) missing`);
    return w.pts;
  });
  // Keep the outlines, the connecting paths and anything touching an outline; build.mjs applies the "wholly inside" rule.
  const kept = ways.filter((w) => ids.includes(w.id) || w.pts.some((pt) => outlines.some((o) => inside(o, pt))));
  write('osm-housing.json', { attribution, bbox: HOUSING.fetchBox, ways: kept });
}
