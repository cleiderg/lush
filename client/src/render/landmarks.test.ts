import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { CampusMap, MeshBuilder, addBuilding, loadCampus } from './campusMap';
import { LANDMARK_IDS, buildLandmark, isLandmark } from './landmarks';
import { BUILDING_BUDGET } from './modelBudget';
import { parseCampus, type Building } from './mapData';

const campusFile = resolve(__dirname, '../../../shared/map/campus.json');
const specFile = resolve(__dirname, '../../../docs/specs/map.md');

/** OSM way ids listed in the map spec's Landmarks section. */
function specLandmarkIds(): number[] {
  const md = readFileSync(specFile, 'utf8');
  const section = md.split(/^## /m).find((s) => s.startsWith('Landmarks'));
  if (!section) throw new Error('map spec has no Landmarks section');
  return [...section.matchAll(/\b\d{6,}\b/g)].map((m) => Number(m[0])).sort((a, b) => a - b);
}

function building(id: number): Building {
  const b = loadCampus().buildings.find((x) => x.id === id);
  if (!b) throw new Error(`building ${id} missing from campus.json`);
  return b;
}

function model(id: number): MeshBuilder {
  const b = new MeshBuilder();
  expect(buildLandmark(b, building(id), loadCampus())).toBe(true);
  return b;
}

type Pt = [number, number];
const footprint = (p: number[]): Pt[] => {
  const out: Pt[] = [];
  for (let i = 0; i < p.length; i += 2) out.push([p[i] / 10, p[i + 1] / 10]);
  return out;
};

function pointInPolygon(poly: Pt[], [x, z]: Pt): boolean {
  let hit = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [ax, az] = poly[i], [cx, cz] = poly[j];
    if (az > z !== cz > z && x < ((cx - ax) * (z - az)) / (cz - az) + ax) hit = !hit;
  }
  return hit;
}

function distanceToOutline(poly: Pt[], [x, z]: Pt): number {
  let best = Infinity;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [ax, az] = poly[j], [cx, cz] = poly[i];
    const dx = cx - ax, dz = cz - az;
    const t = Math.max(0, Math.min(1, ((x - ax) * dx + (z - az) * dz) / (dx * dx + dz * dz)));
    best = Math.min(best, Math.hypot(x - ax - t * dx, z - az - t * dz));
  }
  return best;
}

const bounds = (pts: Pt[]) => [
  Math.min(...pts.map((p) => p[0])), Math.min(...pts.map((p) => p[1])),
  Math.max(...pts.map((p) => p[0])), Math.max(...pts.map((p) => p[1])),
];

describe('landmark list', () => {
  it('matches the Landmarks section of docs/specs/map.md', () => {
    const ids = specLandmarkIds();
    expect(ids.length).toBeGreaterThanOrEqual(4);
    expect([...LANDMARK_IDS].sort((a, b) => a - b)).toEqual(ids);
    for (const id of ids) expect(building(id)).toBeDefined();
  });
});

describe('landmark models (AC1)', () => {
  const map = new CampusMap(loadCampus());

  it.each(specLandmarkIds())('way %i is within the buildings/ triangle budget', (id) => {
    const b = model(id);
    console.log(`landmark ${id}: ${b.triangles} triangles`);
    expect(b.triangles).toBeGreaterThan(50);
    expect(b.triangles).toBeLessThanOrEqual(BUILDING_BUDGET.maxTriangles);
  });

  it.each(specLandmarkIds())('way %i uses flat colours and no textures', (id) => {
    const geo = model(id).geometry();
    const mesh = new THREE.Mesh(geo, map.material);
    expect(geo.getAttribute('uv')).toBeUndefined();
    const mat = mesh.material as THREE.MeshLambertMaterial;
    expect(mat.vertexColors).toBe(true);
    for (const key of ['map', 'lightMap', 'aoMap', 'emissiveMap', 'bumpMap', 'normalMap', 'displacementMap', 'specularMap', 'alphaMap', 'envMap'] as const) {
      expect(mat[key]).toBeNull();
    }
    // Flat: every triangle has one colour at all three corners.
    const col = geo.getAttribute('color');
    const colours = new Set<string>();
    for (let t = 0; t < col.count; t += 3) {
      for (let k = 1; k < 3; k++) {
        expect([col.getX(t + k), col.getY(t + k), col.getZ(t + k)]).toEqual([col.getX(t), col.getY(t), col.getZ(t)]);
      }
      colours.add(`${col.getX(t)},${col.getY(t)},${col.getZ(t)}`);
    }
    expect(colours.size).toBeGreaterThanOrEqual(3);
  });
});

describe('landmarks sit on their footprints (AC2)', () => {
  it.each(specLandmarkIds())('way %i stays inside its footprint, within 1 m', (id) => {
    const poly = footprint(building(id).p);
    const pos = model(id).pos;
    const ground: Pt[] = [];
    let worst = 0;
    for (let i = 0; i < pos.length; i += 3) {
      const p: Pt = [pos[i], pos[i + 2]];
      ground.push(p);
      if (!pointInPolygon(poly, p)) worst = Math.max(worst, distanceToOutline(poly, p));
      expect(pos[i + 1]).toBeGreaterThanOrEqual(0);
    }
    expect(worst).toBeLessThanOrEqual(1);
    const fb = bounds(poly), mb = bounds(ground);
    for (let k = 0; k < 4; k++) expect(Math.abs(mb[k] - fb[k])).toBeLessThanOrEqual(1);
  });
});

describe('campus map with landmarks (AC3)', () => {
  it('draws no generic extrusion for landmarks and keeps every other one', () => {
    const data = loadCampus();
    const map = new CampusMap(data);
    const ids = specLandmarkIds();
    expect([...map.stats.landmarks].sort((a, b) => a - b)).toEqual(ids);
    const others = data.buildings.filter((b) => !ids.includes(b.id)).map((b) => b.id);
    expect(map.stats.extruded).toEqual(others);
    for (const id of ids) expect(map.stats.extruded).not.toContain(id);

    // The triangle total is exactly the non-landmark extrusions plus the landmark models,
    // so no generic extrusion was added for a landmark.
    const withoutBuildings = new CampusMap({ ...data, buildings: [] }).stats.triangles;
    const ext = new MeshBuilder();
    for (const b of data.buildings) if (!isLandmark(b.id)) addBuilding(ext, b.id, b.h, b.p);
    const lm = ids.reduce((sum, id) => sum + model(id).triangles, 0);
    expect(map.stats.triangles).toBe(withoutBuildings + ext.triangles + lm);
  });

  it('leaves the campus data it reads unchanged', () => {
    const data = parseCampus(readFileSync(campusFile, 'utf8'));
    const untouched = structuredClone(data);
    new CampusMap(data).dispose();
    expect(data).toEqual(untouched);
    const lm = new MeshBuilder();
    for (const id of specLandmarkIds()) buildLandmark(lm, data.buildings.find((b) => b.id === id)!, data);
    expect(data).toEqual(untouched);
  });
});
