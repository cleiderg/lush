import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { FLAME_FLICKER, GLASS_GLOW, LampLights, MAX_POINT_LIGHTS, POINT_LIGHT_FADE_MS, lampSources, type LightSource } from './lights';
import { LAMP_HEADS, Props, addLampGlow, loadProps } from './propKit';
import { WorldScene, countDrawCalls } from './scene';

const NIGHT = -0.4;
const DAY = 0.6;
const FRAME = 1000 / 30;

/** 30 sources in a row along +x, 4 m apart; lanterns where `flame` says so. */
function row(flame: (i: number) => boolean = () => false): LampLights {
  const sources: LightSource[] = Array.from({ length: 30 }, (_, i) => ({ x: i * 4, y: 2.6, z: 0, flame: flame(i) }));
  return new LampLights(sources);
}

/** Run frames until `untilMs`, calling `each` after every frame. */
function run(l: LampLights, elevation: number, cam: THREE.Vector3, fromMs: number, untilMs: number, each?: (t: number) => void): number {
  let t = fromMs;
  for (; t <= untilMs; t += FRAME) {
    l.update(elevation, cam, t);
    each?.(t);
  }
  return t;
}

/** Intensity of the pool light following source `i`, or 0. */
function intensityOf(l: LampLights, i: number): number {
  for (let k = 0; k < MAX_POINT_LIGHTS; k++) if (l.sourceOf(k) === i) return l.lights[k].intensity;
  return 0;
}

const nonZero = (l: LampLights) => l.lights.filter((p) => p.intensity > 0);

describe('lamp lights (AC1)', () => {
  it('lights at most 8 of 30 sources at night, the 8 nearest the camera', () => {
    const l = row();
    const cam = new THREE.Vector3(40, 10, 5); // nearest: x = 28..52, sources 7..13 and one of 6/14
    run(l, NIGHT, cam, 0, 2 * POINT_LIGHT_FADE_MS, () => expect(nonZero(l).length).toBeLessThanOrEqual(MAX_POINT_LIGHTS));
    expect(l.lights.length).toBe(MAX_POINT_LIGHTS);
    expect(nonZero(l).length).toBe(8);
    const nearest = l.sources
      .map((s, i) => ({ i, d: (s.x - cam.x) ** 2 + (s.y - cam.y) ** 2 + (s.z - cam.z) ** 2 }))
      .sort((a, b) => a.d - b.d || a.i - b.i)
      .slice(0, 8)
      .map((e) => e.i)
      .sort((a, b) => a - b);
    const lit = l.lights.map((_, k) => l.sourceOf(k)).filter((i) => i >= 0).sort((a, b) => a - b);
    expect(lit).toEqual(nearest);
    for (let k = 0; k < MAX_POINT_LIGHTS; k++) {
      const s = l.sources[l.sourceOf(k)];
      expect(l.lights[k].position.toArray()).toEqual([s.x, s.y, s.z]);
      expect(l.lights[k].intensity).toBeCloseTo(LampLights.baseIntensity(s));
    }
  });

  it('keeps the PointLights in the scene graph all the time, so light count never changes', () => {
    const l = row();
    const count = () => l.group.children.filter((c) => (c as THREE.PointLight).isPointLight).length;
    l.update(DAY, new THREE.Vector3(), 0);
    expect(count()).toBe(MAX_POINT_LIGHTS);
    run(l, NIGHT, new THREE.Vector3(), 0, 1000);
    expect(count()).toBe(MAX_POINT_LIGHTS);
  });
});

describe('lamp light fades (AC2)', () => {
  it('fades a light that leaves the nearest 8 out over POINT_LIGHT_FADE_MS, then fades the new one in', () => {
    const l = row();
    // Camera over source 0: sources 0..7 are the nearest.
    const t = run(l, NIGHT, new THREE.Vector3(0, 10, 0), 0, 2 * POINT_LIGHT_FADE_MS);
    expect(intensityOf(l, 0)).toBeGreaterThan(0);
    expect(intensityOf(l, 8)).toBe(0);
    // Camera east of the midpoint of 0 and 8: source 0 leaves the nearest 8 and source 8 joins.
    const cam = new THREE.Vector3(17, 10, 0);
    const base = LampLights.baseIntensity(l.sources[0]);
    let prev0 = intensityOf(l, 0);
    let prev8 = 0;
    let outAt = -1;
    let inStart = -1;
    let inAt = -1;
    const start = t;
    run(l, NIGHT, cam, t, t + 3 * POINT_LIGHT_FADE_MS, (now) => {
      const i0 = intensityOf(l, 0), i8 = intensityOf(l, 8);
      // No frame changes either light by more than one frame's share of the fade.
      expect(prev0 - i0).toBeLessThanOrEqual((base * FRAME) / POINT_LIGHT_FADE_MS + 1e-6);
      expect(i8 - prev8).toBeLessThanOrEqual((base * FRAME) / POINT_LIGHT_FADE_MS + 1e-6);
      expect(i0).toBeLessThanOrEqual(prev0 + 1e-9);
      expect(nonZero(l).length).toBeLessThanOrEqual(MAX_POINT_LIGHTS);
      if (i0 === 0 && outAt < 0) outAt = now;
      if (i8 > 0 && inStart < 0) inStart = now;
      if (i8 >= base - 1e-6 && inAt < 0) inAt = now;
      prev0 = i0;
      prev8 = i8;
    });
    // Fading out takes POINT_LIGHT_FADE_MS (within a frame), never a single frame.
    expect(outAt - start).toBeGreaterThanOrEqual(POINT_LIGHT_FADE_MS - FRAME);
    expect(outAt - start).toBeLessThanOrEqual(POINT_LIGHT_FADE_MS + FRAME);
    // The newly nearest light fades in the same way once the light is free.
    expect(inStart).toBeGreaterThanOrEqual(outAt);
    expect(inAt - inStart).toBeGreaterThanOrEqual(POINT_LIGHT_FADE_MS - 2 * FRAME);
    expect(inAt - inStart).toBeLessThanOrEqual(POINT_LIGHT_FADE_MS + FRAME);
    expect(intensityOf(l, 8)).toBeCloseTo(base);
  });

  it('fades a leaving light back in if it becomes nearest again before it is off', () => {
    const l = row();
    let t = run(l, NIGHT, new THREE.Vector3(0, 10, 0), 0, 2 * POINT_LIGHT_FADE_MS);
    t = run(l, NIGHT, new THREE.Vector3(17, 10, 0), t, t + POINT_LIGHT_FADE_MS / 2);
    const mid = intensityOf(l, 0);
    expect(mid).toBeGreaterThan(0);
    l.update(NIGHT, new THREE.Vector3(0, 10, 0), t);
    expect(intensityOf(l, 0)).toBeGreaterThan(mid);
  });

  it('never jumps a fade on a long frame', () => {
    const l = row();
    l.update(NIGHT, new THREE.Vector3(), 0);
    l.update(NIGHT, new THREE.Vector3(), 10_000);
    expect(Math.max(...l.lights.map((p) => p.intensity))).toBeLessThan(LampLights.baseIntensity(l.sources[0]));
  });
});

describe('lamps follow the sun (AC3)', () => {
  it('keeps every light off while the sun is up and lights them after sunset', () => {
    const l = row();
    const cam = new THREE.Vector3(10, 10, 0);
    let t = run(l, DAY, cam, 0, 2000, () => expect(nonZero(l).length).toBe(0));
    expect(l.isLit).toBe(false);
    expect(l.glow.value.getHex()).toBe(0);
    t = run(l, -0.01, cam, t, t + 2 * POINT_LIGHT_FADE_MS);
    expect(l.isLit).toBe(true);
    expect(nonZero(l).length).toBe(MAX_POINT_LIGHTS);
    // At sunrise they go off.
    run(l, 0.01, cam, t, t + 500, () => expect(nonZero(l).length).toBe(0));
    expect(l.glow.value.getHex()).toBe(0);
  });

  it('makes every lamp head emissive while lit, including lamps beyond the nearest 8', () => {
    const data = loadProps();
    const props = new Props(data);
    const l = new LampLights(lampSources(data), props.glow);
    expect(l.sources.length).toBeGreaterThan(MAX_POINT_LIGHTS);
    run(l, NIGHT, new THREE.Vector3(l.sources[0].x, 10, l.sources[0].z), 0, 1000);
    expect(l.sources.filter((_, i) => intensityOf(l, i) === 0).length).toBe(l.sources.length - MAX_POINT_LIGHTS);
    // One uniform colours the glass of every lamp, lit or not by a point light.
    expect(props.glow.value.getHex()).toBe(GLASS_GLOW);
    // Every lamp's glass is marked to glow: glass triangles = lamps × the 10 triangles of a glass box (5 faces).
    let glowing = 0;
    for (const c of props.group.children) {
      const g = (c as THREE.Mesh).geometry.getAttribute('glow');
      for (let i = 0; i < g.count; i++) glowing += g.getX(i);
    }
    expect(glowing / 3).toBe(l.sources.length * 10);
    // The shared material adds `glow × lampGlow` to its emissive light.
    const shader = {
      uniforms: {} as Record<string, THREE.IUniform>,
      vertexShader: THREE.ShaderLib.lambert.vertexShader,
      fragmentShader: THREE.ShaderLib.lambert.fragmentShader,
    } as THREE.WebGLProgramParametersWithUniforms;
    addLampGlow(shader, props.glow);
    expect(shader.uniforms.lampGlow).toBe(props.glow);
    expect(shader.vertexShader).toContain('vGlow = glow;');
    expect(shader.fragmentShader).toContain('totalEmissiveRadiance += vGlow * lampGlow;');
    l.update(DAY, new THREE.Vector3(), 2000);
    expect(props.glow.value.getHex()).toBe(0);
    props.dispose();
  });

  it('places a glowing head and a light on every lamp post and lantern in props.json', () => {
    const data = loadProps();
    const lamps = data.props.filter(([t]) => LAMP_HEADS[data.types[t]]);
    const sources = lampSources(data);
    expect(lamps.length).toBeGreaterThan(0);
    expect(sources.length).toBe(lamps.length);
    expect(sources.some((s) => s.flame)).toBe(true);
    expect(sources.some((s) => !s.flame)).toBe(true);
    lamps.forEach(([, x, z], i) => {
      // Within a metre of its post, above the ground.
      expect(Math.hypot(sources[i].x - x / 10, sources[i].z - z / 10)).toBeLessThan(1);
      expect(sources[i].y).toBeGreaterThan(0.5);
    });
  });

  it('lights the world scene at night and not by day, without adding draw calls', () => {
    const w = new WorldScene(() => 0);
    const local = { id: 0, x: 0, z: 0, heading: 0 };
    w.update({ local, remotes: [], sky: { elevation: DAY, azimuth: 0 } });
    const day = countDrawCalls(w.scene);
    expect(w.lamps.lights.every((p) => p.intensity === 0)).toBe(true);
    let now = 0;
    const night = new WorldScene(() => now);
    for (; now < 2000; now += FRAME) night.update({ local, remotes: [], sky: { elevation: NIGHT, azimuth: 0 } });
    expect(night.lamps.isLit).toBe(true);
    expect(nonZero(night.lamps).length).toBe(Math.min(MAX_POINT_LIGHTS, night.lamps.sources.length));
    expect(countDrawCalls(night.scene)).toBe(day);
  });
});

describe('flame flicker (AC4)', () => {
  it('varies lantern lights within ±FLAME_FLICKER of their base and keeps lamp posts steady', () => {
    const l = row((i) => i % 2 === 0);
    const cam = new THREE.Vector3(14, 10, 0);
    const t = run(l, NIGHT, cam, 0, 2 * POINT_LIGHT_FADE_MS);
    const seen = new Map<number, number[]>();
    run(l, NIGHT, cam, t, t + 5000, () => {
      for (let k = 0; k < MAX_POINT_LIGHTS; k++) {
        const src = l.sourceOf(k);
        if (!seen.has(src)) seen.set(src, []);
        seen.get(src)!.push(l.lights[k].intensity);
      }
    });
    let flames = 0;
    let lamps = 0;
    for (const [src, values] of seen) {
      const s = l.sources[src];
      const base = LampLights.baseIntensity(s);
      const lo = Math.min(...values), hi = Math.max(...values);
      if (s.flame) {
        flames++;
        expect(lo).toBeGreaterThanOrEqual(base * (1 - FLAME_FLICKER) - 1e-6);
        expect(hi).toBeLessThanOrEqual(base * (1 + FLAME_FLICKER) + 1e-6);
        // It really flickers, by a good part of the allowed range.
        expect(hi - lo).toBeGreaterThan(base * FLAME_FLICKER);
      } else {
        lamps++;
        expect(lo).toBe(base);
        expect(hi).toBe(base);
      }
    }
    expect(flames).toBeGreaterThan(0);
    expect(lamps).toBeGreaterThan(0);
  });
});
