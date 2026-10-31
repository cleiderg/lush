import * as THREE from 'three';
import { LAMP_HEADS } from './propKit';
import type { PropsData } from './mapData';

/** At most this many point lights shine at once: the ones nearest the camera (visual-style.md, Performance). */
export const MAX_POINT_LIGHTS = 8;
/** Placeholder (PRD 13, Q2): a point light fades in or out over this long. */
export const POINT_LIGHT_FADE_MS = 500;
/** Placeholder (PRD 13, Q2): flame lights flicker by up to this fraction of their base intensity. */
export const FLAME_FLICKER = 0.15;
/** A slow frame (a backgrounded tab) advances fades by at most this much, so a fade always spans frames. */
const MAX_FRAME_MS = 100;

const DM = 0.1;
/** Warm lamp light, as the fire and lanterns in night-cave.jpg. */
const LIGHT_COLOR = 0xffa955;
/** Emissive colour of lamp glass while lit. */
export const GLASS_GLOW = 0xffb347;
/** Base intensity (candela) and reach (metres) per kind. A lamp post throws a wide pool, a lantern a small one. */
const LAMP = { intensity: 14, distance: 12 };
const FLAME = { intensity: 4, distance: 6 };
/** The light sits this far below the glass's centre, so the pool lands under it. */
const LIGHT_DROP = 0.2;

/** One thing that gives light at night. Positions are metres. */
export interface LightSource {
  readonly x: number;
  readonly y: number;
  readonly z: number;
  /** Flames (lanterns) flicker; lamp posts stay steady. */
  readonly flame: boolean;
}

/** A light source on every lamp post and lantern in props.json, just under its glass. */
export function lampSources(data: PropsData): LightSource[] {
  const sources: LightSource[] = [];
  for (const [t, px, pz, rot] of data.props) {
    const head = LAMP_HEADS[data.types[t]];
    if (!head) continue;
    const cx = (head.min[0] + head.max[0]) / 2, cz = (head.min[2] + head.max[2]) / 2;
    // The same placement as Props: rotation clockwise seen from above, in degrees.
    const a = (-rot * Math.PI) / 180;
    sources.push({
      x: px * DM + cx * Math.cos(a) + cz * Math.sin(a),
      y: (head.min[1] + head.max[1]) / 2 - LIGHT_DROP,
      z: pz * DM - cx * Math.sin(a) + cz * Math.cos(a),
      flame: head.flame,
    });
  }
  return sources;
}

/** Flicker in [-1, 1]: two out-of-step waves, phase-shifted per light so lanterns don't pulse together. */
function flicker(seconds: number, i: number): number {
  const p = i * 2.399;
  return 0.6 * Math.sin(seconds * 10.7 + p) + 0.4 * Math.sin(seconds * 27 + 2 * p);
}

/**
 * Warm point lights for lamps and lanterns. While lit (from sunset to sunrise) every lamp head glows, through
 * the shared `glow` uniform that Props' material adds to the glass. A pool of MAX_POINT_LIGHTS PointLights,
 * always in the scene so the shader never recompiles, follows the sources nearest the camera: a source that drops out of the nearest set fades out over
 * POINT_LIGHT_FADE_MS and frees its light, then the newly nearest source fades in on it.
 */
export class LampLights {
  readonly group = new THREE.Group();
  readonly lights: THREE.PointLight[] = [];
  readonly sources: readonly LightSource[];
  /** Source index each pool light follows, or -1 when free. */
  private readonly slotSource = new Int32Array(MAX_POINT_LIGHTS).fill(-1);
  /** Fade level 0..1 of each pool light. */
  private readonly slotLevel = new Float32Array(MAX_POINT_LIGHTS);
  private readonly order: number[];
  private readonly dist: Float32Array;
  private readonly wanted: Uint8Array;
  private lastMs: number | null = null;
  private lit = false;

  /** `glow` is the emissive colour uniform of the lamp glass (Props.glow). */
  constructor(
    sources: readonly LightSource[],
    readonly glow: { value: THREE.Color } = { value: new THREE.Color(0x000000) },
  ) {
    this.sources = sources;
    this.group.name = 'lampLights';
    this.order = sources.map((_, i) => i);
    this.dist = new Float32Array(sources.length);
    this.wanted = new Uint8Array(sources.length);
    for (let i = 0; i < MAX_POINT_LIGHTS; i++) {
      const l = new THREE.PointLight(LIGHT_COLOR, 0, LAMP.distance, 2);
      l.name = `lampLight${i}`;
      l.castShadow = false;
      this.lights.push(l);
      this.group.add(l);
    }
  }

  /** Whether lamps are on (the sun is at or below the horizon). */
  get isLit(): boolean {
    return this.lit;
  }

  /** Source index the pool light `slot` follows, or -1. */
  sourceOf(slot: number): number {
    return this.slotSource[slot];
  }

  /**
   * `elevation` is the sun's (SkyView.elevation); `camera` the camera's world position; `nowMs` the render
   * clock. Lamps are on while the sun is at or below the horizon.
   */
  update(elevation: number, camera: THREE.Vector3, nowMs: number): void {
    const dt = this.lastMs === null ? 0 : Math.min(MAX_FRAME_MS, Math.max(0, nowMs - this.lastMs));
    this.lastMs = nowMs;
    const lit = elevation <= 0;
    if (lit !== this.lit) {
      this.lit = lit;
      this.glow.value.setHex(lit ? GLASS_GLOW : 0x000000);
    }
    const n = this.sources.length;
    this.wanted.fill(0);
    if (lit) {
      for (let i = 0; i < n; i++) {
        const s = this.sources[i];
        this.dist[i] = (s.x - camera.x) ** 2 + (s.y - camera.y) ** 2 + (s.z - camera.z) ** 2;
      }
      // Ties go to the lower index, so the choice is stable.
      this.order.sort((a, b) => this.dist[a] - this.dist[b] || a - b);
      for (let k = 0; k < Math.min(MAX_POINT_LIGHTS, n); k++) this.wanted[this.order[k]] = 1;
    } else {
      // The sun is up: every light is off at once, like a lamp on a daylight sensor.
      this.slotSource.fill(-1);
      this.slotLevel.fill(0);
    }
    const step = dt / POINT_LIGHT_FADE_MS;
    for (let k = 0; k < MAX_POINT_LIGHTS; k++) {
      const src = this.slotSource[k];
      if (src < 0) continue;
      if (this.wanted[src]) {
        this.slotLevel[k] = Math.min(1, this.slotLevel[k] + step);
        this.wanted[src] = 2; // already has a light
      } else {
        this.slotLevel[k] = Math.max(0, this.slotLevel[k] - step);
        if (this.slotLevel[k] === 0) this.slotSource[k] = -1;
      }
    }
    // Newly nearest sources take free lights and start at zero; they brighten from the next frame.
    let free = 0;
    for (let k = 0; k < Math.min(MAX_POINT_LIGHTS, n) && lit; k++) {
      const src = this.order[k];
      if (this.wanted[src] !== 1) continue;
      while (free < MAX_POINT_LIGHTS && this.slotSource[free] >= 0) free++;
      if (free === MAX_POINT_LIGHTS) break;
      this.slotSource[free] = src;
      this.slotLevel[free] = 0;
    }
    const seconds = nowMs / 1000;
    for (let k = 0; k < MAX_POINT_LIGHTS; k++) {
      const l = this.lights[k];
      const src = this.slotSource[k];
      if (src < 0) {
        l.intensity = 0;
        continue;
      }
      const s = this.sources[src];
      const kind = s.flame ? FLAME : LAMP;
      l.position.set(s.x, s.y, s.z);
      l.distance = kind.distance;
      l.intensity = kind.intensity * this.slotLevel[k] * (s.flame ? 1 + FLAME_FLICKER * flicker(seconds, src) : 1);
    }
  }

  /** Base (unfaded, unflickered) intensity of a source. */
  static baseIntensity(s: LightSource): number {
    return s.flame ? FLAME.intensity : LAMP.intensity;
  }
}
