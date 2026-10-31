import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { ItemState } from '../state/items';
import {
  ATTACK_DURATION,
  Character,
  FLINCH_DURATION,
  PARTS,
  RUN_SWING,
  RUN_THRESHOLD,
  WALK_SPEED_RAD_PER_UNIT,
  WALK_SWING,
  makePartGeometry,
  swingAngle,
} from './character';
import { WorldScene, countDrawCalls } from './scene';
import type { HitView, PlayerView, WorldView } from './worldView';

const FPS = 60;
const DT = 1 / FPS;
/** One combat tick (docs/specs/gameplay.md). */
const TICK = 0.6;
const WALK = 4;
const RUN = 7;
const LOCAL = 1;
const REMOTE = 7;

/** X rotation of a remote's part instance, for a player facing heading 0 and not flinching. */
function instanceAngleX(w: WorldScene, part: string, i: number): number {
  const mesh = w.scene.getObjectByName(`player:${part}`) as THREE.InstancedMesh;
  const m = new THREE.Matrix4();
  mesh.getMatrixAt(i, m);
  const q = new THREE.Quaternion().setFromRotationMatrix(m);
  return 2 * Math.atan2(q.x, q.w);
}

/** Ground-plane position of a part instance (breathing moves it up and down, so y is dropped). */
function instancePos(w: WorldScene, part: string, i: number): THREE.Vector3 {
  const mesh = w.scene.getObjectByName(`player:${part}`) as THREE.InstancedMesh;
  const m = new THREE.Matrix4();
  mesh.getMatrixAt(i, m);
  return new THREE.Vector3().setFromMatrixPosition(m).setY(0);
}

/** Walks a Character straight along +z at `speed` m/s for `seconds`; returns the leg angle every frame. */
function walkCharacter(speed: number, seconds: number): number[] {
  const c = new Character();
  const leg = c.joint('legL')!;
  const out: number[] = [];
  for (let f = 0; f <= seconds * FPS; f++) {
    c.update(0, f * DT * speed, 0, f === 0 ? 0 : DT * speed, 0, f * DT);
    out.push(leg.rotation.x);
  }
  return out;
}

function zeroCrossings(xs: number[]): number {
  let n = 0;
  for (let i = 1; i < xs.length; i++) if (Math.sign(xs[i]) !== Math.sign(xs[i - 1]) && xs[i] !== 0) n++;
  return n;
}

const maxAbs = (xs: number[]) => Math.max(...xs.map(Math.abs));

describe('run cycle (AC1)', () => {
  it('has a named threshold between the walk and run speeds', () => {
    expect(RUN_THRESHOLD).toBeGreaterThan(WALK);
    expect(RUN_THRESHOLD).toBeLessThan(RUN);
    expect(RUN_SWING).toBeGreaterThan(WALK_SWING);
  });

  it('swings the limbs faster and further above the threshold', () => {
    const walk = walkCharacter(WALK, 2);
    const run = walkCharacter(RUN, 2);
    expect(zeroCrossings(run)).toBeGreaterThan(zeroCrossings(walk) * 1.5);
    expect(maxAbs(walk)).toBeLessThanOrEqual(WALK_SWING + 1e-9);
    expect(maxAbs(walk)).toBeGreaterThan(WALK_SWING * 0.95);
    expect(maxAbs(run)).toBeGreaterThan(WALK_SWING * 1.3);
    expect(maxAbs(run)).toBeLessThanOrEqual(RUN_SWING + 1e-9);
  });

  it('keeps the walk cycle unchanged below the threshold', () => {
    const leg = PARTS.find((p) => p.name === 'legL')!;
    for (const speed of [1, WALK, RUN_THRESHOLD - 0.1]) {
      walkCharacter(speed, 1).forEach((angle, f) => {
        expect(angle).toBeCloseTo(swingAngle(f * DT * speed * WALK_SPEED_RAD_PER_UNIT, leg), 9);
      });
    }
  });

  it('tells a walk from a run when the position moves in 20 Hz steps rendered at 30 fps', () => {
    // The game loop (core/loop.ts) moves the local player in 50 ms steps and renders every other 60 Hz
    // display frame, so most frames see one step (0.2 m in 33 ms when walking) and some see none.
    const legs = (speed: number) => {
      const c = new Character();
      const leg = c.joint('legL')!;
      let z = 0;
      let last = 0;
      let lastStep = 0;
      const out: number[] = [];
      for (let ms = 0; ms <= 3000; ms += 1000 / 60) {
        while (ms - lastStep >= 50) {
          lastStep += 50;
          z += speed * 0.05;
        }
        if (Math.round(ms / (1000 / 60)) % 2 !== 0) continue;
        c.update(0, z, 0, z - last, 0, ms / 1000);
        last = z;
        if (ms > 1000) out.push(leg.rotation.x);
      }
      return out;
    };
    expect(maxAbs(legs(WALK))).toBeLessThanOrEqual(WALK_SWING + 1e-9);
    expect(maxAbs(legs(RUN))).toBeGreaterThan(WALK_SWING * 1.3);
  });

  it('runs remote players too, from their speed between frames', () => {
    const legs = (speed: number) => {
      let t = 0;
      const w = new WorldScene(() => t * 1000);
      const out: number[] = [];
      for (let f = 0; f <= 2 * FPS; f++) {
        t = f * DT;
        w.update({ local: { id: LOCAL, x: 0, z: 0, heading: 0 }, remotes: [{ id: REMOTE, x: 3, z: t * speed, heading: 0 }] });
        out.push(instanceAngleX(w, 'legL', 0));
      }
      return out;
    };
    const walk = legs(WALK);
    const run = legs(RUN);
    expect(maxAbs(walk)).toBeLessThanOrEqual(WALK_SWING + 1e-6);
    expect(maxAbs(run)).toBeGreaterThan(WALK_SWING * 1.3);
    expect(zeroCrossings(run)).toBeGreaterThan(zeroCrossings(walk) * 1.5);
  });
});

/** A scene with the local player and one remote (both armed), on a clock the test advances. */
function combatScene() {
  let t = 10;
  const w = new WorldScene(() => t * 1000);
  const items = new ItemState();
  for (const id of [LOCAL, REMOTE]) {
    items.setEntityInfo({ id, kind: 0, npcType: 0, combatLevel: 3, equipment: { weapon: 1, head: 0, body: 0, shield: 0 } } as never);
  }
  const remotes: PlayerView[] = [{ id: REMOTE, x: 3, z: 0, heading: 0 }];
  const frame = (hits?: HitView[]): void => {
    const v: WorldView = { local: { id: LOCAL, x: 0, z: 0, heading: 0 }, remotes, entities: items, ...(hits ? { hits } : {}) };
    w.update(v);
  };
  /** Runs frames for `seconds`, calling `sample` after each. */
  const play = (seconds: number, sample: (time: number) => void): void => {
    const start = t;
    for (let f = 1; f <= Math.round(seconds * FPS); f++) {
      t = start + f * DT;
      frame();
      sample(t - start);
    }
  };
  frame();
  const root = w.scene.children.find((o) => o.children.length === PARTS.length && o.getObjectByName('armR'))!;
  const local = { root, joint: (name: string) => root.getObjectByName(name) };
  return { w, frame, play, local };
}

describe('attack swing (AC2)', () => {
  it('swings the local player weapon arm and returns to rest within one combat tick', () => {
    const { frame, play, local } = combatScene();
    const arm = local.joint('armR')!;
    const weapon = local.root.getObjectByName('equip:weapon')!;
    expect(weapon.parent).toBe(arm);
    const rest = arm.quaternion.clone();
    frame([{ attacker: LOCAL, target: 300, damage: 3, died: false }]);
    let peak = 0;
    let doneAt = Infinity;
    play(1, (s) => {
      peak = Math.min(peak, arm.rotation.x);
      if (arm.rotation.x === 0 && arm.rotation.z === 0 && doneAt === Infinity && s > 0.05) doneAt = s;
      // The other arm and legs stay still: only the weapon arm swings.
      expect(local.joint('armL')!.rotation.x).toBe(0);
    });
    expect(peak).toBeLessThan(-1.5);
    expect(ATTACK_DURATION).toBeLessThan(TICK);
    expect(doneAt).toBeLessThanOrEqual(TICK);
    expect(arm.quaternion.angleTo(rest)).toBeCloseTo(0, 9);
  });

  it('swings a drawn remote player weapon arm, with the weapon instance following', () => {
    const { w, frame, play } = combatScene();
    const restArm = instanceAngleX(w, 'armR', 0);
    expect(restArm).toBeCloseTo(0, 9);
    frame([{ attacker: REMOTE, target: 300, damage: 2, died: false }]);
    let weapon!: THREE.InstancedMesh;
    w.scene.traverse((o) => {
      if (o.name.startsWith('equip:weapon:')) weapon = o as THREE.InstancedMesh;
    });
    let peak = 0;
    const a = new THREE.Matrix4();
    const b = new THREE.Matrix4();
    play(TICK, (s) => {
      const angle = instanceAngleX(w, 'armR', 0);
      peak = Math.min(peak, angle);
      (w.scene.getObjectByName('player:armR') as THREE.InstancedMesh).getMatrixAt(0, a);
      weapon.getMatrixAt(0, b);
      expect(b.equals(a)).toBe(true);
      if (s >= ATTACK_DURATION) expect(angle).toBeCloseTo(0, 9);
    });
    expect(peak).toBeLessThan(-1.5);
  });
});

describe('hit flinch and misses (AC3)', () => {
  it('rocks a hit local player back and returns to rest', () => {
    const { frame, play, local } = combatScene();
    const restHead = new THREE.Vector3().setFromMatrixPosition(local.joint('head')!.matrixWorld).setY(0);
    frame([{ attacker: REMOTE, target: LOCAL, damage: 4, died: false }]);
    let peak = 0;
    play(TICK, (s) => {
      peak = Math.min(peak, local.root.rotation.x);
      if (s >= FLINCH_DURATION) expect(local.root.rotation.x).toBe(0);
    });
    expect(FLINCH_DURATION).toBeLessThan(TICK);
    expect(peak).toBeLessThan(-0.2);
    const head = new THREE.Vector3().setFromMatrixPosition(local.joint('head')!.matrixWorld).setY(0);
    expect(head.distanceTo(restHead)).toBeLessThan(1e-9);
  });

  it('rocks a hit remote player back, head away from its facing, and returns to rest', () => {
    const { w, frame, play } = combatScene();
    const rest = instancePos(w, 'head', 0);
    frame([{ attacker: LOCAL, target: REMOTE, damage: 4, died: false }]);
    let back = 0;
    play(TICK, (s) => {
      const head = instancePos(w, 'head', 0);
      back = Math.min(back, head.z - rest.z);
      if (s >= FLINCH_DURATION) expect(head.distanceTo(rest)).toBeLessThan(1e-6);
    });
    expect(back).toBeLessThan(-0.2);
  });

  it('plays the attack swing on a miss, without a flinch', () => {
    const { w, frame, play, local } = combatScene();
    const rest = instancePos(w, 'head', 0);
    frame([{ attacker: LOCAL, target: REMOTE, damage: 0, died: false }]);
    let peak = 0;
    play(TICK, () => {
      peak = Math.min(peak, local.joint('armR')!.rotation.x);
      expect(instancePos(w, 'head', 0).distanceTo(rest)).toBeLessThan(1e-6);
    });
    expect(peak).toBeLessThan(-1.5);
  });
});

describe('animations keep players instanced (AC4)', () => {
  it('draws every body with the same part meshes and draw calls during combat', () => {
    const { w, frame, play } = combatScene();
    const before = countDrawCalls(w.scene);
    frame([
      { attacker: LOCAL, target: REMOTE, damage: 4, died: false },
      { attacker: REMOTE, target: LOCAL, damage: 0, died: false },
    ]);
    play(0.2, () => undefined);
    const bodies: THREE.Mesh[] = [];
    w.scene.traverse((o) => {
      if ((o as THREE.Mesh).isMesh && PARTS.some((p) => (o as THREE.Mesh).geometry === makePartGeometry(p))) bodies.push(o as THREE.Mesh);
    });
    expect(bodies).toHaveLength(PARTS.length);
    for (const b of bodies) {
      expect((b as THREE.InstancedMesh).isInstancedMesh).toBe(true);
      expect((b as THREE.InstancedMesh).count).toBe(2);
    }
    // Hitsplats join with the first hit; nothing else is added for the animations.
    expect(countDrawCalls(w.scene) - countDrawCalls(w.hitsplats.group)).toBe(before);
  });
});
