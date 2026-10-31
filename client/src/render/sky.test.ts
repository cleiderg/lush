import { describe, expect, it } from 'vitest';
import * as THREE from 'three';
import { WorldScene, countDrawCalls } from './scene';
import { DEFAULT_SKY, SHADOW_EXTENT, skyLook } from './sky';
import type { SkyView, WorldView } from './worldView';

function view(x: number, z: number, sky?: SkyView): WorldView {
  return { local: { id: 0, x, z, heading: 0 }, remotes: [{ id: 1, x: x + 2, z, heading: 0 }], sky };
}

const NOON: SkyView = { elevation: 1.3, azimuth: Math.PI };
const DUSK: SkyView = { elevation: 0.02, azimuth: 4.5 };
const NIGHT: SkyView = { elevation: -0.5, azimuth: 0.5 };

/** Sun position relative to its target, normalised. */
function sunDir(w: WorldScene): THREE.Vector3 {
  return w.sky.sun.position.clone().sub(w.sky.sun.target.position).normalize();
}

describe('sky (AC1)', () => {
  it('renders with a daytime default when the view has no sky', () => {
    const w = new WorldScene();
    w.update(view(0, 0));
    expect(w.sky.sun.intensity).toBeGreaterThan(1);
    expect(w.sky.sun.position.y).toBeGreaterThan(w.sky.sun.target.position.y);
    expect(w.scene.background).toBeInstanceOf(THREE.Color);
    expect((w.scene.background as THREE.Color).getHex()).toBe(new THREE.Color(skyLook(DEFAULT_SKY.elevation).sky).getHex());
    expect(w.scene.fog).toBeInstanceOf(THREE.Fog);
    expect(countDrawCalls(w.scene)).toBeGreaterThan(0);
    // The same scene as passing the default explicitly.
    const d = new WorldScene();
    d.update(view(0, 0, DEFAULT_SKY));
    expect(d.sky.sun.position.toArray()).toEqual(w.sky.sun.position.toArray());
  });
});

describe('sun direction and follow (AC2)', () => {
  it('puts the sun straight above its target at elevation π/2', () => {
    const w = new WorldScene();
    w.update(view(0, 0, { elevation: Math.PI / 2, azimuth: 1 }));
    const d = sunDir(w);
    expect(d.x).toBeCloseTo(0, 6);
    expect(d.y).toBeCloseTo(1, 6);
    expect(d.z).toBeCloseTo(0, 6);
  });

  it('puts the sun north (-z) of its target at elevation π/4, azimuth 0', () => {
    const w = new WorldScene();
    w.update(view(0, 0, { elevation: Math.PI / 4, azimuth: 0 }));
    const d = sunDir(w);
    expect(d.x).toBeCloseTo(0, 6);
    expect(d.y).toBeCloseTo(Math.SQRT1_2, 6);
    expect(d.z).toBeCloseTo(-Math.SQRT1_2, 6);
  });

  it('puts the sun east (+x) at azimuth π/2', () => {
    const w = new WorldScene();
    w.update(view(0, 0, { elevation: Math.PI / 4, azimuth: Math.PI / 2 }));
    expect(sunDir(w).x).toBeCloseTo(Math.SQRT1_2, 6);
  });

  it('moves the light target and shadow camera with the player', () => {
    const w = new WorldScene();
    const sky = { elevation: 0.6, azimuth: 3.9 };
    w.update(view(0, 0, sky));
    const sun = w.sky.sun;
    const t0 = sun.target.position.clone();
    const p0 = sun.position.clone();
    const shadowCentre = () => new THREE.Vector3(0, 0, 0).applyMatrix4(sun.shadow.camera.matrixWorld);
    const c0 = shadowCentre();
    w.update(view(300, 0, sky));
    const tex = (2 * SHADOW_EXTENT) / sun.shadow.mapSize.x;
    expect(sun.target.position.x - t0.x).toBeCloseTo(300, 0);
    expect(Math.abs(sun.target.position.x - 300)).toBeLessThanOrEqual(tex);
    expect(sun.target.position.z).toBeCloseTo(t0.z, 6);
    expect(sun.position.clone().sub(p0).x).toBeCloseTo(300, 0);
    const c1 = shadowCentre();
    expect(c1.x - c0.x).toBeCloseTo(300, 0);
    expect(c1.z - c0.z).toBeCloseTo(0, 6);
    // The player stays inside the shadow camera's box.
    const local = new THREE.Vector3(300, 0, 0).applyMatrix4(sun.shadow.camera.matrixWorldInverse);
    expect(Math.abs(local.x)).toBeLessThan(SHADOW_EXTENT);
    expect(Math.abs(local.y)).toBeLessThan(SHADOW_EXTENT);
  });
});

describe('shadows (AC3)', () => {
  it('has a shadow-casting sun', () => {
    expect(new WorldScene().sky.sun.castShadow).toBe(true);
  });

  it('casts from buildings, landmarks and characters and receives on ground and buildings', () => {
    const w = new WorldScene();
    w.update({ ...view(0, 0), npcs: [{ id: 9, npcType: 1, x: 3, z: 3, heading: 0 }] });
    // Buildings and landmarks are merged into the map's chunk meshes.
    expect(w.map.stats.landmarks.length).toBeGreaterThan(0);
    expect(w.map.stats.extruded.length).toBeGreaterThan(0);
    const chunks = w.map.group.children as THREE.Mesh[];
    expect(chunks.length).toBeGreaterThan(0);
    for (const c of chunks) {
      expect(c.castShadow).toBe(true);
      expect(c.receiveShadow).toBe(true);
    }
    expect((w.scene.getObjectByName('ground') as THREE.Mesh).receiveShadow).toBe(true);
    // Characters: the local player, every remote-player part, NPCs.
    const remote = w.scene.getObjectByName('remote-players')!.children as THREE.Mesh[];
    expect(remote.length).toBeGreaterThan(0);
    for (const m of remote) expect(m.castShadow).toBe(true);
    const npcs = w.scene.getObjectByName('npcs')!.children;
    expect(npcs.length).toBeGreaterThan(0);
    for (const m of npcs) expect(m.castShadow).toBe(true);
    // The local player is drawn as the last instance of those same part meshes: one remote player
    // in the view, plus the local one.
    const parts = remote.filter((m) => m.name.startsWith('player:'));
    expect(parts.length).toBeGreaterThan(0);
    for (const m of parts) {
      expect((m as THREE.InstancedMesh).count).toBe(2);
      expect(m.castShadow).toBe(true);
    }
  });
});

describe('time of day (AC4)', () => {
  const at = (sky: SkyView) => {
    const w = new WorldScene();
    w.update(view(0, 0, sky));
    return {
      sky: (w.scene.background as THREE.Color).getHex(),
      fog: (w.scene.fog as THREE.Fog).color.getHex(),
      ambient: w.sky.ambient.intensity,
      sun: w.sky.sun.intensity,
    };
  };

  it('changes sky colour, fog colour and ambient between noon, dusk and night', () => {
    const noon = at(NOON);
    const dusk = at(DUSK);
    const night = at(NIGHT);
    expect(new Set([noon.sky, dusk.sky, night.sky]).size).toBe(3);
    expect(new Set([noon.fog, dusk.fog, night.fog]).size).toBe(3);
    expect(night.ambient).toBeLessThan(dusk.ambient);
    expect(dusk.ambient).toBeLessThan(noon.ambient);
    expect(night.ambient).toBeGreaterThan(0);
    expect(night.sun).toBeLessThan(noon.sun);
  });

  it('keeps a dim moon above the horizon at night', () => {
    const w = new WorldScene();
    w.update(view(0, 0, NIGHT));
    expect(sunDir(w).y).toBeGreaterThan(0.5);
    expect(w.sky.sun.intensity).toBeGreaterThan(0);
  });

  it('makes the night sky darker than the noon sky', () => {
    const lum = (hex: number) => {
      const c = new THREE.Color(hex);
      return c.r + c.g + c.b;
    };
    expect(lum(at(NIGHT).sky)).toBeLessThan(lum(at(DUSK).sky));
    expect(lum(at(DUSK).sky)).toBeLessThan(lum(at(NOON).sky));
  });

  it('blends smoothly with elevation', () => {
    let prev = skyLook(-1).ambient;
    for (let e = -0.6; e <= 1.6; e += 0.05) {
      const a = skyLook(e).ambient;
      expect(a).toBeGreaterThanOrEqual(prev);
      expect(a - prev).toBeLessThan(0.2);
      prev = a;
    }
  });
});
