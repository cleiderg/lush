import * as THREE from 'three';
import fountainRaw from '../../../shared/map/fountain.json?raw';
import { MeshBuilder } from './campusMap';
import { parseFountain, type FountainData } from './mapData';
import { blob } from './parkNature';
import { prism } from './propKit';
import { mulberry32 } from './rng';

const DM = 0.1;

// Flat colours after the reference style (visual-style.md, Ground and nature; night-cave pond): pale stone, calm blue water.
const STONE_RIM = 0xb4ada0;
const STONE_TOP = 0xc8c1b3;
const STONE_DARK = 0x8f897e;
const WATER = 0x4f8fb8;
const WATER_EDGE = 0x2e5f80;
const ROCK = [0x8d8a83, 0x9e9a92, 0x7c7972];

/** Sides of the round basin and the water disk. */
export const FOUNTAIN_SIDES = 24;
/** Basin wall height and thickness in metres. */
const RIM_H = 0.45;
const RIM_W = 0.3;
/** Water surface height, a little below the rim's top. */
export const FOUNTAIN_WATER_Y = 0.34;
/** The water's darker edge band, as a share of the water's radius. */
export const FOUNTAIN_EDGE = 0.25;
/** Stones round the basin: how many, and which of the two shapes each one is (every third one flat). */
export const FOUNTAIN_STONES = 12;
/** Seed for the stones' spacing, size and turn. Cosmetic, but seeded so every client looks the same. */
const STONE_SEED = 0x432;
/** Ripple speed (radians per second) and how much it brightens and darkens the water. Small, so the surface stays calm. */
const RIPPLE_SPEED = 0.9;
const RIPPLE_SHADE = 0.06;
const RIPPLE_LIFT = 0.012;

const up = [0, 1, 0];
const circle = (r: number, y: number, n = FOUNTAIN_SIDES) =>
  Array.from({ length: n }, (_, i) => {
    const a = (i / n) * Math.PI * 2;
    return [Math.cos(a) * r, y, Math.sin(a) * r];
  });

/** A flat ring of quads between two circles of points at the same height. */
function band(b: MeshBuilder, a: number[][], c: number[][], color: number, want = up): void {
  for (let i = 0; i < a.length; i++) {
    const j = (i + 1) % a.length;
    b.tri(a[i], a[j], c[j], want, color);
    b.tri(a[i], c[j], c[i], want, color);
  }
}

/**
 * The basin of radius `r` metres, centred on the origin: a low round stone wall with a lighter top, a darker inner
 * face down to the water, and a small pedestal with a bowl in the middle.
 */
export function basinGeometry(r: number): THREE.BufferGeometry {
  const b = new MeshBuilder();
  const ri = r - RIM_W;
  prism(b, 0, 0, r, r, 0, RIM_H, FOUNTAIN_SIDES, STONE_RIM, false);
  band(b, circle(ri, RIM_H), circle(r, RIM_H), STONE_TOP);
  // Inner face, facing the middle, from the rim's top down to just under the water.
  const top = circle(ri, RIM_H), low = circle(ri, FOUNTAIN_WATER_Y - 0.05);
  for (let i = 0; i < FOUNTAIN_SIDES; i++) {
    const j = (i + 1) % FOUNTAIN_SIDES;
    const a = ((i + 0.5) / FOUNTAIN_SIDES) * Math.PI * 2;
    const inward = [-Math.cos(a), 0, -Math.sin(a)];
    b.tri(top[i], top[j], low[j], inward, STONE_DARK);
    b.tri(top[i], low[j], low[i], inward, STONE_DARK);
  }
  // Pedestal and bowl.
  prism(b, 0, 0, 0.22, 0.16, FOUNTAIN_WATER_Y - 0.05, 0.85, 6, STONE_RIM, false);
  prism(b, 0, 0, 0.18, 0.5, 0.85, 1.0, 8, STONE_TOP, true);
  return b.geometry();
}

/**
 * The water of radius `r` metres at FOUNTAIN_WATER_Y: lighter open water in the middle and a darker band
 * (FOUNTAIN_EDGE of the radius) along the edge, in vertex colours.
 */
export function waterGeometry(r: number): THREE.BufferGeometry {
  const b = new MeshBuilder();
  const y = FOUNTAIN_WATER_Y;
  const inner = circle(r * (1 - FOUNTAIN_EDGE), y), outer = circle(r, y);
  for (let i = 0; i < FOUNTAIN_SIDES; i++) b.tri([0, y, 0], inner[i], inner[(i + 1) % FOUNTAIN_SIDES], up, WATER);
  band(b, inner, outer, WATER_EDGE);
  return b.geometry();
}

/** The two stone shapes: a rounded boulder and a flat, five-sided slab, each about 0.3 m across at scale 1. */
export function stoneGeometries(): [THREE.BufferGeometry, THREE.BufferGeometry] {
  const boulder = new MeshBuilder();
  blob(boulder, 0, 0.1, 0, 0.17, 0.14, 0.15, ROCK[0], 91, 0.3);
  const slab = new MeshBuilder();
  prism(slab, 0, 0, 0.2, 0.16, 0, 0.08, 5, ROCK[2], true);
  return [boulder.geometry(), slab.geometry()];
}

/** The water's own material for water of `radius` metres: flat shaded, with a slow time-driven ripple in its vertex and fragment shaders. */
export function waterMaterial(time: { value: number }, radius: number): THREE.MeshPhongMaterial {
  const mat = new THREE.MeshPhongMaterial({
    vertexColors: true, shininess: 60, specular: 0x335566, flatShading: true, polygonOffset: true, polygonOffsetFactor: -1,
  });
  mat.name = 'fountain-water';
  mat.userData.uTime = time;
  mat.onBeforeCompile = (shader) => {
    shader.uniforms.uTime = time;
    shader.vertexShader = shader.vertexShader
      .replace('#include <common>', '#include <common>\nuniform float uTime;\nvarying vec2 vWater;')
      .replace('#include <begin_vertex>', [
        '#include <begin_vertex>',
        'vWater = position.xz;',
        // Still at the edge, so the water never lifts off the basin wall.
        `transformed.y += ${RIPPLE_LIFT.toFixed(4)} * sin(uTime * ${RIPPLE_SPEED.toFixed(2)} + length(position.xz) * 3.0) * (1.0 - smoothstep(0.0, ${radius.toFixed(2)}, length(position.xz)));`,
      ].join('\n'));
    shader.fragmentShader = shader.fragmentShader
      .replace('#include <common>', '#include <common>\nuniform float uTime;\nvarying vec2 vWater;')
      .replace('#include <color_fragment>', [
        '#include <color_fragment>',
        `float ripple = sin(length(vWater) * 6.0 - uTime * ${RIPPLE_SPEED.toFixed(2)} * 2.0) * sin(vWater.x * 2.3 + uTime * ${RIPPLE_SPEED.toFixed(2)});`,
        `diffuseColor.rgb *= 1.0 + ${RIPPLE_SHADE.toFixed(3)} * ripple;`,
      ].join('\n'));
  };
  mat.customProgramCacheKey = () => 'fountain-water';
  return mat;
}

function lambert(): THREE.MeshLambertMaterial {
  return new THREE.MeshLambertMaterial({ vertexColors: true });
}

/**
 * The M8 fountain from fountain.json: a round stone basin with calm, gently rippling water and a ring of stones of
 * two shapes round its foot (one instanced mesh per shape). Visual only; the basin is blocked in collision.bin.
 */
export class Fountain {
  readonly group = new THREE.Group();
  readonly basin: THREE.Mesh;
  readonly water: THREE.Mesh;
  /** One instanced mesh per stone shape: boulders, then flat slabs. */
  readonly stones: [THREE.InstancedMesh, THREE.InstancedMesh];
  private readonly time = { value: 0 };

  constructor(data: FountainData) {
    this.group.name = 'fountain';
    const r = data.r * DM;
    this.group.position.set(data.x * DM, 0, data.z * DM);
    this.basin = new THREE.Mesh(basinGeometry(r), lambert());
    this.basin.name = 'fountain-basin';
    this.water = new THREE.Mesh(waterGeometry(r - RIM_W), waterMaterial(this.time, r - RIM_W));
    this.water.name = 'fountain-water';
    this.water.receiveShadow = true;
    this.basin.castShadow = this.basin.receiveShadow = true;

    // Stones round the basin's foot: evenly spaced with a seeded jitter in angle, distance, size and turn.
    const rnd = mulberry32(STONE_SEED);
    const shapes = stoneGeometries();
    const counts = [0, 0];
    const kinds = Array.from({ length: FOUNTAIN_STONES }, (_, i) => (i % 3 === 2 ? 1 : 0));
    for (const k of kinds) counts[k]++;
    const stones = (k: 0 | 1) => {
      const mesh = new THREE.InstancedMesh(shapes[k], lambert(), counts[k]);
      mesh.name = k === 0 ? 'fountain-boulders' : 'fountain-slabs';
      mesh.castShadow = mesh.receiveShadow = true;
      mesh.count = 0;
      return mesh;
    };
    this.stones = [stones(0), stones(1)];
    const m = new THREE.Matrix4(), q = new THREE.Quaternion(), s = new THREE.Vector3(), p = new THREE.Vector3();
    const yAxis = new THREE.Vector3(0, 1, 0);
    kinds.forEach((k, i) => {
      const a = ((i + 0.5 + (rnd() - 0.5) * 0.5) / FOUNTAIN_STONES) * Math.PI * 2;
      const d = r + 0.2 + rnd() * 0.15;
      const size = 0.8 + rnd() * 0.6;
      q.setFromAxisAngle(yAxis, rnd() * Math.PI * 2);
      p.set(Math.cos(a) * d, 0, Math.sin(a) * d);
      s.set(size, size * (0.8 + rnd() * 0.4), size);
      const mesh = this.stones[k];
      mesh.setMatrixAt(mesh.count++, m.compose(p, q, s));
    });
    for (const mesh of this.stones) {
      mesh.instanceMatrix.needsUpdate = true;
      mesh.computeBoundingSphere();
    }
    this.group.add(this.basin, this.water, ...this.stones);
    this.group.updateMatrixWorld(true);
  }

  /** Advance the water's ripple to `time` seconds on the render clock. */
  update(time: number): void {
    this.time.value = time;
  }

  /** Triangles of the basin and water (the stones are counted apart). */
  get triangles(): number {
    return (this.basin.geometry.attributes.position.count + this.water.geometry.attributes.position.count) / 3;
  }

  dispose(): void {
    for (const mesh of [this.basin, this.water, ...this.stones]) {
      mesh.geometry.dispose();
      (mesh.material as THREE.Material).dispose();
    }
  }
}

let cached: FountainData | null = null;
/** The committed fountain.json, parsed once. */
export function loadFountain(): FountainData {
  return (cached ??= parseFountain(fountainRaw));
}
