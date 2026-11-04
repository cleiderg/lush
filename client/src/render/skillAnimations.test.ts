import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { Animation } from '../protocol';
import { SKILL_LOOP_PERIOD } from './character';
import { LEVEL_UP_EFFECT_MS } from './levelUp';
import { WorldScene, countDrawCalls } from './scene';
import type { PlayerView, WorldView } from './worldView';

const REMOTE = 7;

function view(animation: number, levelUps?: { entityId: number }[], x = 5): WorldView {
  const remotes: PlayerView[] = [{ id: REMOTE, x, z: 3, heading: 0, animation }];
  return { local: { id: 1, x: 0, z: 0, heading: 0, animation }, remotes, ...(levelUps ? { levelUps } : {}) };
}

/** X rotations of both arms of the remote (instance 0) and the local player (last instance). */
function arms(w: WorldScene): number[] {
  const out: number[] = [];
  for (const part of ['armL', 'armR']) {
    const mesh = w.scene.getObjectByName(`player:${part}`) as THREE.InstancedMesh;
    for (const i of [0, mesh.count - 1]) {
      const m = new THREE.Matrix4();
      mesh.getMatrixAt(i, m);
      const q = new THREE.Quaternion().setFromRotationMatrix(m);
      out.push(Math.round(2 * Math.atan2(q.x, q.w) * 1e4) / 1e4);
    }
  }
  return out;
}

function poseAt(a: number, t: number): number[] {
  let now = t * 1000;
  const w = new WorldScene(() => now);
  w.update(view(a));
  now += 1;
  return arms(w);
}

describe('skill animations (AC1)', () => {
  const idle = poseAt(Animation.Idle, 0.5);

  it('each skill animation has its own pose, different from idle', () => {
    const poses = [Animation.Chop, Animation.Fish, Animation.Cook].map((a) => JSON.stringify(poseAt(a, 0.5)));
    expect(new Set([...poses, JSON.stringify(idle)]).size).toBe(4);
  });

  it('loops: same pose one period later, moving in between', () => {
    for (const a of [Animation.Chop, Animation.Fish, Animation.Cook]) {
      const p = SKILL_LOOP_PERIOD[a];
      expect(poseAt(a, 0.3)).toEqual(poseAt(a, 0.3 + p));
      expect(poseAt(a, 0.3)).not.toEqual(poseAt(a, 0.3 + p * 0.4));
    }
  });

  it('switching back to Idle restores the idle pose', () => {
    let now = 500;
    const w = new WorldScene(() => now);
    w.update(view(Animation.Chop));
    now += 100;
    w.update(view(Animation.Chop));
    expect(arms(w)).not.toEqual(idle);
    now = 500;
    w.update(view(Animation.Idle));
    expect(arms(w)).toEqual(idle);
  });
});

describe('level-up effect (AC2, AC3)', () => {
  function setup() {
    let now = 1000;
    const w = new WorldScene(() => now);
    return { w, advance: (ms: number) => (now += ms) };
  }
  const sparks = (w: WorldScene) => w.scene.getObjectByName('level-up-sparks') as THREE.InstancedMesh;
  const centre = (w: WorldScene) => {
    const m = new THREE.Matrix4();
    sparks(w).getMatrixAt(0, m);
    return new THREE.Vector3().setFromMatrixPosition(m);
  };

  it('LEVEL_UP_EFFECT_MS is the 2000 ms placeholder', () => {
    expect(LEVEL_UP_EFFECT_MS).toBe(2000);
  });

  it('appears over the entity, follows it and is removed after LEVEL_UP_EFFECT_MS', () => {
    const { w, advance } = setup();
    w.update(view(0));
    expect(sparks(w)).toBeUndefined();
    w.update(view(0, [{ entityId: REMOTE }]));
    expect(sparks(w).count).toBeGreaterThan(0);
    advance(500);
    w.update(view(0, undefined, 5));
    const before = centre(w);
    advance(100);
    w.update(view(0, undefined, 9));
    expect(centre(w).x - before.x).toBeGreaterThan(3);
    advance(LEVEL_UP_EFFECT_MS - 601);
    w.update(view(0, undefined, 9));
    expect(sparks(w).count).toBeGreaterThan(0);
    advance(1);
    w.update(view(0, undefined, 9));
    expect(sparks(w).count).toBe(0);
    expect(w.levelUps.active.length).toBe(0);
  });

  it('ignores unknown entities, and adds one draw call and no light', () => {
    const { w } = setup();
    w.update(view(0));
    const base = countDrawCalls(w.scene);
    const lights = () => {
      let n = 0;
      w.scene.traverse((o) => { if ((o as THREE.Light).isLight) n++; });
      return n;
    };
    const lightsBefore = lights();
    w.update(view(0, [{ entityId: 999 }]));
    expect(w.levelUps.active.length).toBe(0);
    w.update(view(0, [{ entityId: REMOTE }, { entityId: 1 }]));
    expect(countDrawCalls(w.scene)).toBe(base + 1);
    expect(lights()).toBe(lightsBefore);
    w.scene.traverse((o) => expect(o instanceof THREE.PointLight && o.parent === w.levelUps.group).toBe(false));
    let inGroup = 0;
    w.levelUps.group.traverse((o) => { if ((o as THREE.Light).isLight) inGroup++; });
    expect(inGroup).toBe(0);
  });
});
