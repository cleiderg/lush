import { describe, expect, it } from 'vitest';
import { CameraRig, MAX_PITCH, MIN_PITCH } from './cameraRig';
import { PARTS } from './character';
import { WorldScene, countDrawCalls } from './scene';
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
    const calls = countDrawCalls(w.scene);
    console.log(`draw calls with 100 remote players: ${calls}`);
    expect(calls).toBeLessThan(20);
    const inst = w.scene.getObjectByProperty('isInstancedMesh', true) as import('three').InstancedMesh;
    expect(inst.count).toBe(100);
  });

  it('has a placeholder ground', () => {
    expect(new WorldScene().scene.getObjectByName('ground-placeholder')).toBeDefined();
  });
});
