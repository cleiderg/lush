import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { CollisionGrid } from '../state/collision';
import {
  CAFE_DOOR, CAFE_DRAW_CALLS, CAFE_KIT, CAFE_PLACEMENTS, CafeInterior, FLOOR_WOOD, PLASTER, cafeFloorMatches, cafeLightSources,
  cafeShellGeometry, cafeTypeTriangles, onDoorCell,
} from './cafeInterior';
import { MeshBuilder, loadCampus } from './campusMap';
import { LampLights, MAX_POINT_LIGHTS, POINT_LIGHT_FADE_MS } from './lights';
import { buildLandmark, STUDENT_CENTER } from './landmarks';
import { type AreasData, parseAreas } from './mapData';
import { BUILDING_BUDGET, PROP_BUDGET } from './modelBudget';
import { WorldScene } from './scene';

const mapDir = resolve(__dirname, '../../../shared/map');
const areas: AreasData = parseAreas(readFileSync(join(mapDir, 'areas.json'), 'utf8'));
const grid = CollisionGrid.parse(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const campus = loadCampus();
const cafe = areas.interiors.find((a) => a.id === 'student_center_cafe')!;
const floor = cafe.p.map((v) => v / 10);

function inside(x: number, z: number, p: number[]): boolean {
  let r = false;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    if (p[i + 1] > z !== p[j + 1] > z && x < p[i] + ((z - p[i + 1]) * (p[j] - p[i])) / (p[j + 1] - p[i + 1])) r = !r;
  }
  return r;
}

const footprint = campus.buildings.find((b) => b.id === STUDENT_CENTER)!.p.map((v) => v / 10);
/** Door cells from collision.bin: walkable cells with their centre in the footprint but off the café floor. */
function doorCells(): [number, number][] {
  const out: [number, number][] = [];
  for (let r = 0; r < grid.height; r++) {
    for (let c = 0; c < grid.width; c++) {
      const x = grid.originX + c + 0.5, z = grid.originZ + r + 0.5;
      if (x < 30 || x > 60 || z < -360 || z > -340) continue;
      if (inside(x, z, footprint) && !inside(x, z, floor) && !grid.cellBlocked(c, r)) out.push([x, z]);
    }
  }
  return out;
}

describe('furnished café (AC1)', () => {
  it('matches the floor in areas.json and the door cells in collision.bin', () => {
    expect(cafeFloorMatches()).toBe(true);
    const cells = doorCells();
    expect(cells.length).toBeGreaterThan(0);
    for (const [x, z] of cells) expect(onDoorCell(x, z)).toBe(true);
    expect(cells.length).toBe((CAFE_DOOR[2] - CAFE_DOOR[0]) * (CAFE_DOOR[3] - CAFE_DOOR[1]));
  });

  it('holds 25+ props of 8+ types, with the required kinds, none on a door cell', () => {
    const inCafe = CAFE_PLACEMENTS.filter(([, x, z]) => inside(x, z, floor));
    expect(inCafe.length).toBe(CAFE_PLACEMENTS.length);
    expect(inCafe.length).toBeGreaterThanOrEqual(25);
    const types = new Set(inCafe.map((p) => p[0]));
    expect(types.size).toBeGreaterThanOrEqual(8);
    for (const t of ['counter', 'menu_board', 'table', 'chair', 'plant', 'rug']) expect(types.has(t)).toBe(true);
    expect(types.has('plate') || types.has('cup')).toBe(true);
    expect(types.has('fireplace') || types.has('window')).toBe(true);
    for (const t of types) expect(CAFE_KIT[t]).toBeDefined();
    const cells = doorCells();
    for (const [, x, z] of CAFE_PLACEMENTS) {
      expect(onDoorCell(x, z)).toBe(false);
      for (const [cx, cz] of cells) expect(Math.abs(x - cx) <= 0.5 && Math.abs(z - cz) <= 0.5).toBe(false);
    }
  });
});

describe('café materials and the door opening (AC2)', () => {
  const colours = (g: THREE.BufferGeometry) => {
    const c = g.getAttribute('color');
    return new Set(Array.from({ length: c.count }, (_, i) => new THREE.Color(c.getX(i), c.getY(i), c.getZ(i)).getHex()));
  };

  it('has a wood floor and plaster walls in a material of its own, apart from the outside walls', () => {
    const cols = colours(cafeShellGeometry());
    expect(cols.has(FLOOR_WOOD)).toBe(true);
    expect(cols.has(PLASTER)).toBe(true);
    const outside = new MeshBuilder();
    expect(buildLandmark(outside, campus.buildings.find((b) => b.id === STUDENT_CENTER)!, campus)).toBe(true);
    const outCols = new Set<number>();
    for (let i = 0; i < outside.col.length; i += 3) outCols.add(new THREE.Color(outside.col[i], outside.col[i + 1], outside.col[i + 2]).getHex());
    expect(outCols.has(FLOOR_WOOD)).toBe(false);
    expect(outCols.has(PLASTER)).toBe(false);
    const w = new WorldScene();
    const shellMat = w.cafe.shell.material as THREE.Material;
    for (const c of w.map.group.children) expect((c as THREE.Mesh).material).not.toBe(shellMat);
  });

  it('leaves the Student Center wall open over the door cells', () => {
    const m = new MeshBuilder();
    buildLandmark(m, campus.buildings.find((b) => b.id === STUDENT_CENTER)!, campus);
    const [x0, z0, x1, z1] = CAFE_DOOR;
    const cells = doorCells();
    expect(cells.length).toBeGreaterThan(0);
    // No upright wall triangle reaches the ground-floor opening: every vertical triangle whose centre is over
    // a door cell (or the metre in front of it) starts above the opening.
    let lintel = 0;
    const p = m.pos;
    for (let i = 0; i < p.length; i += 9) {
      const cx = (p[i] + p[i + 3] + p[i + 6]) / 3, cz = (p[i + 2] + p[i + 5] + p[i + 8]) / 3;
      const ys = [p[i + 1], p[i + 4], p[i + 7]];
      const upright = Math.abs(p[i + 1] - p[i + 4]) + Math.abs(p[i + 4] - p[i + 7]) > 0;
      if (cx < x0 || cx > x1 || cz < z0 || cz > z1 + 1 || !upright) continue;
      const flat = ys[0] === ys[1] && ys[1] === ys[2];
      if (flat) continue;
      lintel++;
      expect(Math.min(...ys)).toBeGreaterThanOrEqual(2.6 - 1e-6);
    }
    expect(lintel).toBeGreaterThan(0);
  });
});

describe('café lights (AC3)', () => {
  const night = -0.3;
  const settle = (l: LampLights, cam: THREE.Vector3, steps = 12) => {
    for (let i = 0; i <= steps; i++) l.update(night, cam, i * (POINT_LIGHT_FADE_MS / 4));
  };
  const lit = (l: LampLights) => l.lights.filter((x) => x.intensity > 0).length;

  it('registers the café lights with the scene point-light manager', () => {
    const w = new WorldScene();
    const cafeLights = cafeLightSources();
    expect(cafeLights.length).toBeGreaterThanOrEqual(4);
    for (const s of cafeLights) {
      expect(w.lamps.sources.some((x) => x.x === s.x && x.y === s.y && x.z === s.z && x.flame === s.flame)).toBe(true);
    }
  });

  it('keeps the nearest-8 limit and the fade with the café lights included', () => {
    const w = new WorldScene();
    const l = new LampLights(w.lamps.sources, w.props.glow);
    expect(l.lights.length).toBe(MAX_POINT_LIGHTS);
    const cam = new THREE.Vector3(45, 8, -345);
    settle(l, cam);
    expect(lit(l)).toBeLessThanOrEqual(MAX_POINT_LIGHTS);
    expect(lit(l)).toBe(MAX_POINT_LIGHTS);
    const cafeIdx = new Set(l.sources.map((s, i) => (cafeLightSources().some((c) => c.x === s.x && c.z === s.z && c.y === s.y) ? i : -1)).filter((i) => i >= 0));
    const followed = Array.from({ length: MAX_POINT_LIGHTS }, (_, k) => l.sourceOf(k));
    expect(followed.some((i) => cafeIdx.has(i))).toBe(true);
    // Walk away: café lights fade out over the fade time rather than snapping, and the limit still holds.
    const far = new THREE.Vector3(-150, 8, 40);
    const t0 = 20 * POINT_LIGHT_FADE_MS;
    l.update(night, far, t0);
    l.update(night, far, t0 + POINT_LIGHT_FADE_MS / 4);
    expect(lit(l)).toBeLessThanOrEqual(MAX_POINT_LIGHTS);
    const mid = l.lights.filter((x) => x.intensity > 0).length;
    expect(mid).toBeGreaterThan(0);
    for (let i = 2; i < 40; i++) l.update(night, far, t0 + i * (POINT_LIGHT_FADE_MS / 4));
    expect(lit(l)).toBeLessThanOrEqual(MAX_POINT_LIGHTS);
    expect(Array.from({ length: MAX_POINT_LIGHTS }, (_, k) => l.sourceOf(k)).some((i) => cafeIdx.has(i))).toBe(false);
  });
});

describe('café budgets (AC4)', () => {
  it('keeps every prop type within PROP_BUDGET.maxTriangles', () => {
    for (const t of Object.keys(CAFE_KIT)) expect(cafeTypeTriangles(t)).toBeLessThanOrEqual(PROP_BUDGET.maxTriangles);
  });

  it('adds a fixed number of draw calls however many props it has', () => {
    const count = (c: CafeInterior) => { let n = 0; c.group.traverse((o) => { if ((o as THREE.Mesh).isMesh) n++; }); return n; };
    const glow = { value: new THREE.Color(0) };
    const a = new CafeInterior(glow);
    const doubled = new CafeInterior(glow, [...CAFE_PLACEMENTS, ...CAFE_PLACEMENTS, ...CAFE_PLACEMENTS]);
    expect(count(a)).toBe(CAFE_DRAW_CALLS);
    expect(count(doubled)).toBe(CAFE_DRAW_CALLS);
    expect(doubled.props.geometry.getAttribute('position').count).toBeGreaterThan(a.props.geometry.getAttribute('position').count);
  });

  it('keeps the Student Center within BUILDING_BUDGET.maxTriangles', () => {
    const m = new MeshBuilder();
    buildLandmark(m, campus.buildings.find((b) => b.id === STUDENT_CENTER)!, campus);
    expect(m.triangles).toBeLessThanOrEqual(BUILDING_BUDGET.maxTriangles);
  });
});
