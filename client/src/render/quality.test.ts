import { afterEach, describe, expect, it, vi } from 'vitest';

// No WebGL in node: stub the renderer, keep the rest of three.
vi.mock('three', async (importOriginal) => {
  const three = await importOriginal<typeof import('three')>();
  class FakeWebGLRenderer {
    shadowMap = { enabled: false, type: three.BasicShadowMap };
    toneMapping: number = three.NoToneMapping;
    toneMappingExposure = 1;
    outputColorSpace = '';
    setPixelRatio(): void {}
    setSize(): void {}
    render(): void {}
    dispose(): void {}
  }
  return { ...three, WebGLRenderer: FakeWebGLRenderer };
});

import * as THREE from 'three';
import { createRenderer } from './renderer';
import { QUALITY_GRASS, QUALITY_SHADOW_MAP, type QualityPreset } from './quality';
import { SHADOW_MAP_SIZE } from './sky';
import { MAX_POINT_LIGHTS } from './lights';
import { WorldScene, countDrawCallsInView, countTrianglesInView } from './scene';
import { loadAreas } from './gameAreas';

vi.setConfig({ testTimeout: 60_000 });
const g = globalThis as unknown as { document?: unknown; window?: unknown };
const prev = { document: g.document, window: g.window };
afterEach(() => {
  g.document = prev.document;
  g.window = prev.window;
});
const fakeDom = () => {
  g.document = { createElement: () => ({ style: {}, children: [], appendChild() {}, remove() {} }), body: { appendChild() {} } };
  g.window = { devicePixelRatio: 1 };
  return { clientWidth: 390, clientHeight: 844, addEventListener() {}, removeEventListener() {} } as unknown as HTMLCanvasElement;
};
const natureCalls = (w: WorldScene) => {
  let n = 0;
  w.nature.group.traverseVisible((o) => { if ((o as THREE.Mesh).isMesh) n++; });
  return n;
};
const meshes = (w: WorldScene) => {
  let n = 0;
  w.scene.traverse((o) => { if ((o as THREE.Mesh).isMesh) n++; });
  return n;
};

describe('quality presets: shadows (AC1)', () => {
  it('High keeps full-size sun shadows, Medium one smaller map, Low none', () => {
    const r = createRenderer(fakeDom());
    const sun = r.world.sky.sun;
    expect(r.quality).toBe('high');
    expect(r.gl.shadowMap.enabled).toBe(true);
    expect(sun.castShadow).toBe(true);
    expect(sun.shadow.mapSize.x).toBe(SHADOW_MAP_SIZE);
    expect(QUALITY_SHADOW_MAP.high).toBe(SHADOW_MAP_SIZE);

    r.setQuality('medium');
    expect(r.quality).toBe('medium');
    expect(r.gl.shadowMap.enabled).toBe(true);
    expect(sun.castShadow).toBe(true);
    expect(sun.shadow.mapSize.x).toBe(1024);
    expect(sun.shadow.mapSize.y).toBe(1024);
    expect(r.world.scene.getObjectsByProperty('isDirectionalLight', true).filter((l) => (l as THREE.DirectionalLight).castShadow)).toHaveLength(1);

    r.setQuality('low');
    expect(r.gl.shadowMap.enabled).toBe(false);
    expect(sun.castShadow).toBe(false);

    r.setQuality('high');
    expect(r.gl.shadowMap.enabled).toBe(true);
    expect(sun.castShadow).toBe(true);
    expect(sun.shadow.mapSize.x).toBe(SHADOW_MAP_SIZE);
    r.dispose();
  });
});

describe('quality presets: nature (AC2)', () => {
  it('Medium and Low scale the High counts by the density factors, with the same draw calls', () => {
    const w = new WorldScene();
    const high = { ...w.nature.stats };
    const calls = natureCalls(w);
    expect(high.tufts).toBeGreaterThan(1000);
    for (const p of ['medium', 'low'] as const) {
      w.setQuality(p);
      const s = w.nature.stats;
      const f = QUALITY_GRASS[p];
      // Per-chunk rounding and the grass mask make this approximate.
      expect(Math.abs(s.tufts - high.tufts * f)).toBeLessThanOrEqual(high.tufts * f * 0.05);
      expect(Math.abs(s.flowers - high.flowers * f)).toBeLessThanOrEqual(high.flowers * f * 0.1);
      expect(s.chunks).toBe(high.chunks);
      expect(natureCalls(w)).toBe(calls);
    }
    w.setQuality('high');
    expect(w.nature.stats).toEqual(high);
  });
});

describe('quality presets: lights, props and sky (AC3)', () => {
  it('Low still lights MAX_POINT_LIGHTS lamps at night; props, glow and sky colours match', () => {
    const snap = (p: QualityPreset) => {
      const w = new WorldScene();
      w.setQuality(p);
      const sx = loadAreas().spawn.x / 10, sz = loadAreas().spawn.z / 10;
      let t = 0;
      const night = () => w.update({ local: { id: 0, x: sx, z: sz, heading: 0 }, remotes: [], sky: { elevation: -0.3, azimuth: 1 } });
      // Step the clock so the fade-in completes.
      for (let i = 0; i < 40; i++) { t += 100; (w as unknown as { now: () => number }).now = () => t; night(); }
      const lit = w.lamps.lights.filter((l) => l.intensity > 0).length;
      return {
        lit,
        props: w.props.group.children.length,
        propInstances: w.props.group.children.map((c) => (c as THREE.InstancedMesh).count ?? 0),
        glow: w.props.glow.value.getHex(),
        sky: [w.sky.background.getHex(), w.sky.fog.color.getHex(), w.sky.ambient.color.getHex(), w.sky.sun.color.getHex(), w.sky.sun.intensity],
      };
    };
    const ref = snap('high');
    expect(ref.lit).toBe(MAX_POINT_LIGHTS);
    for (const p of ['medium', 'low'] as const) expect(snap(p)).toEqual(ref);
  });
});

describe('quality presets: budget on Medium (AC4)', () => {
  it('spawn with 100 players: under 150 draw calls and 300,000 triangles', () => {
    const w = new WorldScene();
    w.setQuality('medium');
    const sx = loadAreas().spawn.x / 10, sz = loadAreas().spawn.z / 10;
    const remotes = Array.from({ length: 100 }, (_, i) => ({ id: i + 1, x: sx + (i % 10) - 5, z: sz + Math.floor(i / 10) - 5, heading: 0 }));
    w.update({ local: { id: 0, x: sx, z: sz, heading: 0 }, remotes });
    w.rig.camera.aspect = 9 / 19.5;
    w.rig.camera.updateProjectionMatrix();
    const calls = countDrawCallsInView(w.scene, w.rig.camera);
    const tris = countTrianglesInView(w.scene, w.rig.camera);
    console.log(`Medium, spawn, 100 players: ${calls} draw calls, ${tris} triangles`);
    expect(calls).toBeLessThan(150);
    expect(tris).toBeLessThan(300_000);
  });
});

describe('quality presets: disposal (AC5)', () => {
  it('disposes replaced geometries and shadow maps, and leaks nothing over repeated switches', () => {
    const w = new WorldScene();
    const sun = w.sky.sun;
    const live = new Set<THREE.WebGLRenderTarget>();
    const spies: ReturnType<typeof vi.spyOn>[] = [];
    // Stand in for the render target three makes on the first shadow render.
    const giveMap = () => {
      if (sun.shadow.map) return;
      const t = new THREE.WebGLRenderTarget(4, 4);
      live.add(t);
      spies.push(vi.spyOn(t, 'dispose').mockImplementation(() => { live.delete(t); }));
      sun.shadow.map = t;
    };
    giveMap();
    const meshesBefore = meshes(w);
    const liveBefore = live.size;

    const geoms = new Set<THREE.BufferGeometry>();
    w.nature.group.traverse((o) => { if ((o as THREE.Mesh).isMesh) geoms.add((o as THREE.Mesh).geometry); });
    const geomSpies = [...geoms].map((x) => vi.spyOn(x, 'dispose'));

    w.setQuality('low');
    expect(live.size).toBe(0);
    for (const s of geomSpies) expect(s).toHaveBeenCalled();

    for (let i = 0; i < 2; i++) {
      w.setQuality('high');
      giveMap();
      const gs = new Set<THREE.BufferGeometry>();
      w.nature.group.traverse((o) => { if ((o as THREE.Mesh).isMesh) gs.add((o as THREE.Mesh).geometry); });
      const sp = [...gs].map((x) => vi.spyOn(x, 'dispose'));
      w.setQuality('medium');
      expect(live.size).toBe(0);
      sp.forEach((s) => expect(s).toHaveBeenCalled());
      w.setQuality('low');
      w.setQuality('high');
      giveMap();
    }
    expect(meshes(w)).toBe(meshesBefore);
    expect(live.size).toBe(liveBefore);
    expect(spies.length).toBeGreaterThan(1);
  });
});
