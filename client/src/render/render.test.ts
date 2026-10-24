import { describe, expect, it } from 'vitest';
import { CameraRig, MAX_PITCH, MIN_PITCH } from './cameraRig';
import { loadCampus } from './campusMap';
import { Animation } from '../protocol';
import { PARTS } from './character';
import { WorldScene, countDrawCalls, countDrawCallsInView, countTrianglesInView } from './scene';
import type { PlayerView, WorldView } from './worldView';

function view(n: number, t = 0): WorldView {
  const remotes: PlayerView[] = [];
  for (let i = 0; i < n; i++) remotes.push({ id: i + 1, x: (i % 10) * 2 + t, z: Math.floor(i / 10) * 2, heading: i });
  return { local: { id: 0, x: t, z: 0, heading: 0 }, remotes };
}

describe('camera', () => {
  it('sits high and behind the player', () => {
    const rig = new CameraRig();
    rig.follow(10, 20);
    expect(rig.camera.position.y).toBeGreaterThan(5);
    expect(rig.camera.position.z).toBeLessThan(20); // yaw 0 looks toward +Z
    expect(rig.camera.position.x).toBeCloseTo(10);
  });

  it('rotates when dragged and clamps pitch', () => {
    const rig = new CameraRig();
    rig.follow(0, 0);
    const before = rig.camera.position.clone();
    rig.drag(100, 0);
    rig.follow(0, 0);
    expect(rig.camera.position.distanceTo(before)).toBeGreaterThan(1);
    rig.drag(0, 1e5);
    expect(rig.pitch).toBe(MAX_PITCH);
    rig.drag(0, -1e5);
    expect(rig.pitch).toBe(MIN_PITCH);
  });
});

describe('characters', () => {
  it('animates local player limbs while walking and rests when still', () => {
    const w = new WorldScene();
    w.update(view(0, 0));
    w.update(view(0, 0.5));
    const player = w.scene.children.find((c) => c.children.length === PARTS.length)!;
    const legL = player.children[PARTS.findIndex((p) => p.name === 'legL')];
    expect(Math.abs(legL.rotation.x)).toBeGreaterThan(0);
    w.update(view(0, 0.5));
    expect(Math.abs(legL.rotation.x)).toBe(0);
  });

  it('draws 100 remote players with few draw calls', () => {
    const w = new WorldScene();
    w.update(view(100));
    w.update(view(100, 0.3));
    w.rig.camera.aspect = 9 / 19.5;
    w.rig.camera.updateProjectionMatrix();
    const calls = countDrawCallsInView(w.scene, w.rig.camera);
    const tris = countTrianglesInView(w.scene, w.rig.camera);
    console.log(`in view, 100 players: ${calls} draw calls, ${tris} triangles`);
    console.log(`whole scene without culling: ${countDrawCalls(w.scene)} draw calls`);
    expect(calls).toBeLessThan(40);
    const inst = w.scene.getObjectByProperty('isInstancedMesh', true) as import('three').InstancedMesh;
    expect(inst.count).toBe(100);
  });

  it('raises the local arm for Wave and returns to idle', () => {
    const w = new WorldScene();
    const v = (animation: number): WorldView => ({ ...view(0), local: { id: 0, x: 0, z: 0, heading: 0, animation } });
    const player = () => w.scene.children.find((c) => c.children.length === PARTS.length)!;
    const arm = () => player().children[PARTS.findIndex((p) => p.name === 'armR')];
    w.update(v(Animation.Idle));
    expect(arm().rotation.z).toBe(0);
    w.update(v(Animation.Wave));
    expect(Math.abs(arm().rotation.z)).toBeGreaterThan(1);
    w.update(v(Animation.Idle));
    expect(arm().rotation.z).toBe(0);
  });

  it('raises only the waving remote player arm instance', () => {
    const w = new WorldScene();
    const base = view(3);
    w.update(base);
    const meshes = w.scene.getObjectsByProperty('isInstancedMesh', true) as import('three').InstancedMesh[];
    const armIdx = PARTS.findIndex((p) => p.name === 'armR');
    const snap = () => meshes.map((m) => Array.from(m.instanceMatrix.array.slice(0, 48)));
    const idle = snap();
    w.update({ ...base, remotes: base.remotes.map((p, i) => (i === 1 ? { ...p, animation: Animation.Wave } : p)) });
    const waved = snap();
    const row = (a: number[], i: number) => a.slice(i * 16, i * 16 + 16);
    meshes.forEach((_, k) => {
      for (let i = 0; i < 3; i++) {
        const changed = JSON.stringify(row(idle[k], i)) !== JSON.stringify(row(waved[k], i));
        expect(changed).toBe(k === armIdx && i === 1);
      }
    });
  });

  it('keeps draw calls the same with players waving', () => {
    const calls = (wave: boolean) => {
      const w = new WorldScene();
      const v = view(100);
      w.update({ ...v, remotes: v.remotes.map((p, i) => ({ ...p, animation: wave && i % 2 ? Animation.Wave : 0 })) });
      return countDrawCalls(w.scene);
    };
    expect(calls(true)).toBe(calls(false));
  });

  it('has a ground that covers the whole map', () => {
    const ground = new WorldScene().scene.getObjectByName('ground') as import('three').Mesh;
    expect(ground).toBeDefined();
    ground.geometry.computeBoundingBox();
    const box = ground.geometry.boundingBox!.clone().translate(ground.position);
    const b = loadCampus().bounds;
    expect(box.min.x).toBeLessThanOrEqual(b.minX / 10);
    expect(box.max.x).toBeGreaterThanOrEqual(b.maxX / 10);
    expect(box.min.z).toBeLessThanOrEqual(b.minZ / 10);
    expect(box.max.z).toBeGreaterThanOrEqual(b.maxZ / 10);
  });
});
