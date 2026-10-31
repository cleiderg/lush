import * as THREE from 'three';
import type { SkyView } from './worldView';

/** Used when the view carries no sky: a warm afternoon sun in the south-west, as in day-tavern.jpg. */
export const DEFAULT_SKY: SkyView = { elevation: 0.8, azimuth: 3.9 };

/** Half-width of the sun's shadow camera around the player, in metres. Covers what the camera rig frames. */
export const SHADOW_EXTENT = 40;
/** Shadow map size in texels per side. */
export const SHADOW_MAP_SIZE = 2048;
/** How far from its target the sun light sits along the sun direction, in metres. */
const SUN_DISTANCE = 120;
/** When the sun is below the horizon the moon lights the scene from opposite it, at least this high. */
const MOON_MIN_ELEVATION = 0.6;

/** Colours and intensities for one sun elevation. Colours are hex sRGB. */
export interface SkyLook {
  sky: number;
  fog: number;
  fogNear: number;
  fogFar: number;
  /** Hemisphere ambient: light from above, light from the ground. */
  ambientSky: number;
  ambientGround: number;
  ambient: number;
  /** Directional light colour and intensity (the sun by day, the moon by night). */
  light: number;
  lightIntensity: number;
}

/**
 * Looks keyed by sun elevation in radians, low to high. Placeholders tuned by eye against the
 * references: night-cave.jpg (dark, readable) and day-tavern.jpg (warm late afternoon).
 */
const KEYS: readonly [number, SkyLook][] = [
  [-0.3, { sky: 0x0a0e1c, fog: 0x0e1018, fogNear: 30, fogFar: 160, ambientSky: 0x58607a, ambientGround: 0x2a2218, ambient: 0.55, light: 0xa8b8e0, lightIntensity: 0.4 }],
  [-0.08, { sky: 0x252a4a, fog: 0x3a3450, fogNear: 40, fogFar: 180, ambientSky: 0x7a7298, ambientGround: 0x3a2c22, ambient: 0.8, light: 0xb0a0d0, lightIntensity: 0.3 }],
  [0.02, { sky: 0x6f7fae, fog: 0xd88f68, fogNear: 50, fogFar: 220, ambientSky: 0xc09a90, ambientGround: 0x5a4230, ambient: 1.1, light: 0xff9050, lightIntensity: 1.1 }],
  [0.25, { sky: 0x8db2d6, fog: 0xe8c4a0, fogNear: 70, fogFar: 260, ambientSky: 0xe6dccc, ambientGround: 0x6e5838, ambient: 1.5, light: 0xffc27a, lightIntensity: 2.6 }],
  [0.9, { sky: 0x7db4e6, fog: 0xc4dbea, fogNear: 80, fogFar: 300, ambientSky: 0xe4eeff, ambientGround: 0x6a5a3a, ambient: 1.6, light: 0xfff2de, lightIntensity: 2.5 }],
];

const ca = new THREE.Color();
const cb = new THREE.Color();
function mixHex(a: number, b: number, t: number): number {
  return ca.setHex(a).lerp(cb.setHex(b), t).getHex();
}

/** The look for a sun elevation, blended between the nearest keys. */
export function skyLook(elevation: number): SkyLook {
  if (elevation <= KEYS[0][0]) return { ...KEYS[0][1] };
  for (let i = 1; i < KEYS.length; i++) {
    const [e1, b] = KEYS[i];
    if (elevation > e1) continue;
    const [e0, a] = KEYS[i - 1];
    const t = (elevation - e0) / (e1 - e0);
    const n = (x: number, y: number) => x + (y - x) * t;
    return {
      sky: mixHex(a.sky, b.sky, t),
      fog: mixHex(a.fog, b.fog, t),
      fogNear: n(a.fogNear, b.fogNear),
      fogFar: n(a.fogFar, b.fogFar),
      ambientSky: mixHex(a.ambientSky, b.ambientSky, t),
      ambientGround: mixHex(a.ambientGround, b.ambientGround, t),
      ambient: n(a.ambient, b.ambient),
      light: mixHex(a.light, b.light, t),
      lightIntensity: n(a.lightIntensity, b.lightIntensity),
    };
  }
  return { ...KEYS[KEYS.length - 1][1] };
}

/** Unit vector toward a body at `elevation` above the horizon and `azimuth` clockwise from north (-z), east +x. */
export function skyDirection(elevation: number, azimuth: number, out = new THREE.Vector3()): THREE.Vector3 {
  const h = Math.cos(elevation);
  return out.set(h * Math.sin(azimuth), Math.sin(elevation), -h * Math.cos(azimuth));
}

/** Sun (or moon), ambient, sky and fog for the scene, following a SkyView and the local player. */
export class SkyRig {
  readonly sun = new THREE.DirectionalLight(0xffffff, 1);
  readonly ambient = new THREE.HemisphereLight(0xffffff, 0xffffff, 1);
  readonly fog = new THREE.Fog(0xffffff, 1, 2);
  readonly background = new THREE.Color();
  private readonly dir = new THREE.Vector3();
  private lastElevation = NaN;
  private lastAzimuth = NaN;

  constructor() {
    this.sun.name = 'sun';
    this.ambient.name = 'ambient';
    this.sun.castShadow = true;
    const s = this.sun.shadow;
    s.mapSize.set(SHADOW_MAP_SIZE, SHADOW_MAP_SIZE);
    const cam = s.camera;
    cam.left = -SHADOW_EXTENT;
    cam.right = SHADOW_EXTENT;
    cam.top = SHADOW_EXTENT;
    cam.bottom = -SHADOW_EXTENT;
    cam.near = 1;
    cam.far = SUN_DISTANCE * 2;
    cam.updateProjectionMatrix();
    s.bias = -0.0005;
    s.normalBias = 0.04;
    s.radius = 3;
  }

  /** Add the lights (and the sun's target) to `scene` and hand it the sky colour and fog. */
  attach(scene: THREE.Scene): void {
    scene.add(this.sun, this.sun.target, this.ambient);
    scene.background = this.background;
    scene.fog = this.fog;
  }

  /** Set the look for `sky` (DEFAULT_SKY when missing) and centre the sun and its shadow on (x, z). */
  update(sky: SkyView | undefined, x: number, z: number): void {
    const { elevation, azimuth } = sky ?? DEFAULT_SKY;
    if (elevation !== this.lastElevation || azimuth !== this.lastAzimuth) {
      this.lastElevation = elevation;
      this.lastAzimuth = azimuth;
      const look = skyLook(elevation);
      this.background.setHex(look.sky);
      this.fog.color.setHex(look.fog);
      this.fog.near = look.fogNear;
      this.fog.far = look.fogFar;
      this.ambient.color.setHex(look.ambientSky);
      this.ambient.groundColor.setHex(look.ambientGround);
      this.ambient.intensity = look.ambient;
      this.sun.color.setHex(look.light);
      this.sun.intensity = look.lightIntensity;
      if (elevation >= 0) skyDirection(elevation, azimuth, this.dir);
      else skyDirection(Math.max(MOON_MIN_ELEVATION, -elevation), azimuth + Math.PI, this.dir);
    }
    // Snap to whole shadow texels so shadow edges don't crawl as the player walks.
    const texel = (2 * SHADOW_EXTENT) / SHADOW_MAP_SIZE;
    const tx = Math.round(x / texel) * texel;
    const tz = Math.round(z / texel) * texel;
    this.sun.target.position.set(tx, 0, tz);
    this.sun.position.set(tx, 0, tz).addScaledVector(this.dir, SUN_DISTANCE);
    this.sun.target.updateMatrixWorld();
    this.sun.updateMatrixWorld();
    this.sun.shadow.updateMatrices(this.sun);
  }
}
