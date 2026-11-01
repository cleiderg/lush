import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import {
  CampusMap, MIN_WINDOW_FACE, MeshBuilder, WALL_COLORS, WALL_KINDS, WINDOW_GLOW, addBuilding, addWindowGlow, loadCampus, wallKind,
} from './campusMap';
import { faces, ring } from './facade';
import { LANDMARK_IDS, buildLandmark } from './landmarks';
import { parseCampus, type Building, type CampusData } from './mapData';
import { BUILDING_BUDGET } from './modelBudget';
import { WorldScene, countDrawCalls } from './scene';
import type { WorldView } from './worldView';

const campusFile = resolve(__dirname, '../../../shared/map/campus.json');
const freshCampus = (): CampusData => parseCampus(readFileSync(campusFile, 'utf8'));
const campus = loadCampus();
const generic = campus.buildings.filter((b) => !LANDMARK_IDS.includes(b.id));

const bytes = (a: THREE.TypedArray) => Buffer.from(a.buffer, a.byteOffset, a.byteLength);
const sameBytes = (a: THREE.TypedArray, b: THREE.TypedArray) => bytes(a).equals(bytes(b));

function landmark(b: Building): MeshBuilder {
  const m = new MeshBuilder();
  expect(buildLandmark(m, b, campus)).toBe(true);
  return m;
}

/**
 * Faces of `b`'s footprint at least MIN_WINDOW_FACE long that have no window geometry on them: no
 * triangle standing 0.03 to 0.08 m proud of the face (where frames and panes sit) within its span.
 */
function facesWithoutWindows(b: Building, m: MeshBuilder): number[] {
  const tris = [m.pos, m.glass.pos];
  const missing: number[] = [];
  faces(ring(b.p)).forEach((f, i) => {
    if (f.len < MIN_WINDOW_FACE) return;
    const onFace = (pos: Float32Array, t: number) => {
      for (let k = t; k < t + 9; k += 3) {
        const dx = pos[k] - f.a.x, dz = pos[k + 2] - f.a.z;
        const s = dx * f.t.x + dz * f.t.z, off = dx * f.n.x + dz * f.n.z;
        if (s < 0 || s > f.len || off < 0.03 || off > 0.08) return false;
      }
      return true;
    };
    const hit = tris.some((pos) => {
      for (let t = 0; t < pos.length; t += 9) if (onFace(pos, t)) return true;
      return false;
    });
    if (!hit) missing.push(i);
  });
  return missing;
}

describe('framed windows and doors (AC1)', () => {
  it(`gives every generic building face of ${MIN_WINDOW_FACE} m or more framed windows, and every building a door`, () => {
    const map = new CampusMap(campus);
    let faces = 0, windows = 0;
    for (const b of generic) {
      const d = map.stats.facades.get(b.id)!;
      expect(d, `building ${b.id}`).toBeDefined();
      expect(d.doors, `doors on ${b.id}`).toBeGreaterThanOrEqual(1);
      for (const f of d.faces) {
        if (f.length < MIN_WINDOW_FACE) continue;
        faces++;
        expect(f.windows, `face of ${b.id}, ${f.length.toFixed(1)} m`).toBeGreaterThanOrEqual(1);
      }
      windows += d.faces.reduce((n, f) => n + f.windows, 0);
    }
    console.log(`${generic.length} generic buildings: ${faces} faces of ${MIN_WINDOW_FACE} m or more, ${windows} framed windows`);
    expect(faces).toBeGreaterThan(500);
  });

  it('draws a frame and panes on each of those faces, and counts each window and door it draws', () => {
    for (const b of generic) {
      const m = new MeshBuilder();
      const d = addBuilding(m, b.id, b.h, b.p);
      expect(facesWithoutWindows(b, m), `building ${b.id}`).toEqual([]);
      expect(m.windows).toBe(d.faces.reduce((n, f) => n + f.windows, 0));
      expect(m.doors).toBe(d.doors);
    }
  });

  it.each(LANDMARK_IDS)('gives landmark %i framed windows on every long face and at least one door', (id) => {
    const b = campus.buildings.find((x) => x.id === id)!;
    const m = landmark(b);
    expect(m.doors).toBeGreaterThanOrEqual(1);
    expect(m.windows).toBeGreaterThan(0);
    expect(facesWithoutWindows(b, m)).toEqual([]);
  });
});

describe('wall materials (AC2)', () => {
  it('uses at least 3 wall materials, chosen from the building id alone', () => {
    const kinds = new Set(generic.map((b) => wallKind(b.id)));
    console.log(`wall materials: ${[...kinds].join(', ')}`);
    expect(kinds.size).toBeGreaterThanOrEqual(3);
    for (const b of generic) expect(wallKind(b.id)).toBe(wallKind(b.id));
    // The palettes are distinct, so each material shows as its own colours.
    const all = WALL_KINDS.flatMap((k) => WALL_COLORS[k]);
    expect(new Set(all).size).toBe(all.length);
  });

  it('draws each building in its material, the same way both times the map is built', () => {
    const a = new CampusMap(freshCampus()), b = new CampusMap(freshCampus());
    const kinds = new Map<number, string>();
    for (const [id, d] of a.stats.facades) {
      expect(d.kind).toBe(wallKind(id));
      expect(b.stats.facades.get(id)!.kind).toBe(d.kind);
      kinds.set(id, d.kind);
    }
    expect(new Set(kinds.values()).size).toBeGreaterThanOrEqual(3);
    for (const bl of generic) {
      // Walls come first: the first triangle carries the wall colour.
      const m = new MeshBuilder();
      addBuilding(m, bl.id, bl.h, bl.p);
      const c = new THREE.Color().setRGB(m.col[0], m.col[1], m.col[2]).getHex();
      const palette = WALL_COLORS[wallKind(bl.id)].map((h) => new THREE.Color(h).getHex());
      expect(palette, `building ${bl.id}`).toContain(c);
    }
    a.group.children.forEach((chunk, i) => {
      const ga = (chunk as THREE.Mesh).geometry, gb = (b.group.children[i] as THREE.Mesh).geometry;
      if (!ga) return;
      expect(chunk.name).toBe(b.group.children[i].name);
      for (const name of ['position', 'color', 'glow']) {
        expect(sameBytes(ga.getAttribute(name).array, gb.getAttribute(name).array), `${chunk.name} ${name}`).toBe(true);
      }
    });
  });
});

describe('windows lit at night (AC3)', () => {
  const night = { elevation: -0.2, azimuth: 0 };
  const noon = { elevation: 1.3, azimuth: Math.PI };

  it('makes window glass emissive while the sun is below the horizon and not at noon', () => {
    const map = new CampusMap(campus);
    expect(map.windowGlow.getHex()).toBe(0);
    map.updateSky(night);
    expect(map.windowGlow.getHex()).not.toBe(0);
    expect(map.windowGlow.r).toBeGreaterThan(map.windowGlow.b);
    map.updateSky(noon);
    expect(map.windowGlow.getHex()).toBe(0);
    map.updateSky({ elevation: -0.01, azimuth: 0 });
    expect(map.windowGlow.getHex()).not.toBe(0);
    map.updateSky({ elevation: 0.01, azimuth: 0 });
    expect(map.windowGlow.getHex()).toBe(0);
    map.updateSky(undefined);
    expect(map.windowGlow.getHex()).toBe(0);
  });

  it('marks exactly the window glass to glow in the chunk meshes', () => {
    const map = new CampusMap(campus);
    let lit = 0, unlit = 0, other = 0;
    for (const chunk of map.group.children) {
      const g = (chunk as THREE.Mesh).geometry;
      if (!g) continue;
      const glow = g.getAttribute('glow');
      expect(glow, chunk.name).toBeDefined();
      for (const v of glow.array) {
        if (v === 1) lit++;
        else if (v === 0) unlit++;
        else other++;
      }
    }
    // As many glowing corners as there are glass triangles in the buildings and landmarks.
    let glass = 0;
    for (const b of campus.buildings) {
      const m = new MeshBuilder();
      if (!buildLandmark(m, b, campus)) addBuilding(m, b.id, b.h, b.p);
      glass += m.glass.triangles;
    }
    expect(other).toBe(0);
    expect(lit).toBe(glass * 3);
    expect(lit).toBeGreaterThan(1000);
    expect(unlit).toBeGreaterThan(lit);
  });

  it('adds the glow to the emissive light of the map material', () => {
    const map = new CampusMap(campus);
    const lib = THREE.ShaderLib.lambert;
    const shader = { uniforms: {} as Record<string, THREE.IUniform>, vertexShader: lib.vertexShader, fragmentShader: lib.fragmentShader };
    map.material.onBeforeCompile(shader as never, {} as never);
    expect(shader.uniforms.windowGlow.value).toBe(map.windowGlow);
    expect(shader.vertexShader).toContain('attribute float glow;');
    expect(shader.vertexShader).toContain('vGlow = glow;');
    expect(shader.fragmentShader).toContain('totalEmissiveRadiance += windowGlow * vGlow;');
    // The emissive term comes after the material's own emissive colour is set.
    const fs = shader.fragmentShader;
    expect(fs.indexOf('vec3 totalEmissiveRadiance')).toBeLessThan(fs.indexOf('totalEmissiveRadiance += windowGlow'));
    const plain = { uniforms: {}, vertexShader: 'x', fragmentShader: 'y' };
    addWindowGlow(plain, { value: new THREE.Color(WINDOW_GLOW) });
    expect(plain.fragmentShader).not.toContain('+= windowGlow');
  });

  it('follows the view sky in the world scene', () => {
    const w = new WorldScene(() => 0);
    const view = (elevation: number): WorldView => ({ local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: [], sky: { elevation, azimuth: 0 } });
    w.update(view(-0.3));
    expect(w.map.windowGlow.getHex()).not.toBe(0);
    w.update(view(1.2));
    expect(w.map.windowGlow.getHex()).toBe(0);
  });
});

describe('draw calls (AC4)', () => {
  it('adds at most one draw call per chunk for buildings, however many buildings and windows', () => {
    const data = freshCampus();
    const none = new CampusMap({ ...data, buildings: [] });
    const all = new CampusMap(data);
    // Every building twice, so each chunk holds twice the buildings and windows.
    const doubled = new CampusMap({ ...data, buildings: [...data.buildings, ...data.buildings.map((b) => ({ ...b, id: b.id + 1 }))] });
    const [n0, n1, n2] = [none, all, doubled].map((m) => countDrawCalls(m.group));
    console.log(`map draw calls: ${n0} without buildings, ${n1} with, ${n2} with every building twice (${all.stats.chunks} chunks)`);
    expect(n1 - n0).toBeLessThanOrEqual(all.stats.chunks);
    expect(n2).toBe(n1);
    expect(doubled.stats.triangles).toBeGreaterThan(all.stats.triangles * 1.5);
    // Window glass is in the chunk mesh itself, not a mesh of its own.
    for (const chunk of all.group.children) {
      const own = chunk.children.filter((c) => !c.name.startsWith('ground-') && c.name !== 'student-center-cutaway'); // the cut-away building is hidden unless the player is in the café
      expect(own, chunk.name).toEqual([]);
    }
  });
});

describe('landmark budgets (AC5)', () => {
  it.each(LANDMARK_IDS)('landmark %i on its own, glass and all, is within BUILDING_BUDGET', (id) => {
    const m = landmark(campus.buildings.find((x) => x.id === id)!);
    console.log(`landmark ${id}: ${m.triangles} triangles (${m.glass.triangles} glass), ${m.windows} windows, ${m.doors} doors`);
    expect(m.glass.triangles).toBeGreaterThan(0);
    expect(m.triangles).toBeLessThanOrEqual(BUILDING_BUDGET.maxTriangles);
  });
});
