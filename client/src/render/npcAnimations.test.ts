import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { ATTACK_DURATION, BREATH_PERIOD, FLINCH_DURATION } from './character';
import { GENERIC_NPC, NPC_GAITS, NPC_LIMB_GLSL, NPC_MODELS, NPC_TELEPORT, NpcType, Npcs, limbPoint, makeNpcGeometry } from './npcs';
import { WorldScene, countDrawCalls } from './scene';
import type { HitView, NpcView, WorldView } from './worldView';

const FPS = 60;
const DT = 1 / FPS;
/** One combat tick (docs/specs/gameplay.md). */
const TICK = 0.6;
const LOCAL = 1;
const KEYS = [...Object.values(NpcType), GENERIC_NPC];
const LEGGED = [NpcType.Rabbit, NpcType.Squirrel, NpcType.Crow, NpcType.Coyote, NpcType.CoyoteAlpha];
const LEGLESS = [NpcType.RogueDeliveryRobot, GENERIC_NPC];

interface Sample {
  pos: THREE.Vector3;
  q: THREE.Quaternion;
  scale: THREE.Vector3;
  limb: number;
}

function sample(n: Npcs, key: number, i = 0): Sample {
  const mesh = n.meshFor(key)!;
  const m = new THREE.Matrix4();
  mesh.getMatrixAt(i, m);
  const s: Sample = { pos: new THREE.Vector3(), q: new THREE.Quaternion(), scale: new THREE.Vector3(), limb: 0 };
  m.decompose(s.pos, s.q, s.scale);
  s.limb = mesh.geometry.getAttribute('npcSwing').getX(i);
  return s;
}

/** Where the top of a body at heading 0 leans: + forward, - back. */
const lean = (s: Sample) => new THREE.Vector3(0, 1, 0).applyQuaternion(s.q).z;

/** Runs `n` for `seconds` with NPCs from `at(time)`, sampling each frame. */
function run(n: Npcs, seconds: number, at: (t: number) => NpcView[], each: (t: number) => void): void {
  for (let f = 0; f <= Math.round(seconds * FPS); f++) {
    const t = f * DT;
    n.update(at(t), t);
    each(t);
  }
}

describe('NPC idle breathing (AC1)', () => {
  it.each(KEYS)('a standing NPC of type %i swells and settles back to rest, out of step with its neighbour', (key) => {
    const n = new Npcs();
    const standing = (): NpcView[] => [
      { id: 100, npcType: key, x: 0, z: 0, heading: 0.4 },
      { id: 101, npcType: key, x: 2, z: 0, heading: 0.4 },
    ];
    const a: number[] = [];
    const b: number[] = [];
    run(n, BREATH_PERIOD * 2, standing, () => {
      const s = sample(n, key, 0);
      a.push(s.scale.y);
      b.push(sample(n, key, 1).scale.y);
      // Breathing never moves or turns the body, nor its limbs.
      expect(s.pos.x).toBe(0);
      expect(s.pos.y).toBe(0);
      expect(s.q.angleTo(new THREE.Quaternion().setFromAxisAngle(new THREE.Vector3(0, 1, 0), 0.4))).toBeCloseTo(0, 6);
      expect(s.limb).toBe(0);
    });
    // A swell above rest and a fall below it, back through rest in between.
    expect(Math.max(...a)).toBeGreaterThan(1.03);
    expect(Math.min(...a)).toBeLessThan(0.97);
    expect(a.some((v) => Math.abs(v - 1) < 0.005)).toBe(true);
    // Periodic: one breath later it is where it was.
    const period = Math.round(BREATH_PERIOD * FPS);
    expect(a[period + 5]).toBeCloseTo(a[5], 2);
    // Out of step: the two NPCs differ somewhere along the way.
    expect(Math.max(...a.map((v, i) => Math.abs(v - b[i])))).toBeGreaterThan(0.02);
  });
});

describe('NPC movement (AC2)', () => {
  const walking = (key: number) => (t: number): NpcView[] => [{ id: 200, npcType: key, x: 0, z: t * 2, heading: 0 }];

  it.each(LEGGED)('type %i swings its legs while walking', (key) => {
    const n = new Npcs();
    const limbs: number[] = [];
    run(n, 1, walking(key), () => limbs.push(sample(n, key).limb));
    const g = NPC_GAITS[key];
    expect(g.swing).toBeGreaterThan(0);
    expect(Math.max(...limbs)).toBeGreaterThan(g.swing * 0.9);
    expect(Math.min(...limbs)).toBeLessThan(-g.swing * 0.9);
    // The model has leg boxes the shader turns; a leg's foot moves forward and back.
    const legs = NPC_MODELS[key].filter((b) => b.limb);
    expect(legs.length).toBeGreaterThanOrEqual(2);
    const geo = makeNpcGeometry(key);
    const attr = geo.getAttribute('npcLimb');
    const pos = geo.getAttribute('position');
    // The limb vertex furthest below its pivot: the foot.
    const i = Array.from({ length: attr.count }, (_, k) => k)
      .filter((k) => attr.getW(k) !== 0)
      .reduce((best, k) => (attr.getY(k) - pos.getY(k) > attr.getY(best) - pos.getY(best) ? k : best));
    const limb = [attr.getX(i), attr.getY(i), attr.getZ(i), attr.getW(i)] as const;
    const foot = new THREE.Vector3(pos.getX(i), pos.getY(i), pos.getZ(i));
    const zs = limbs.map((a) => limbPoint(limb, a, foot).z - foot.z);
    expect(Math.max(...zs) - Math.min(...zs)).toBeGreaterThan(0.01);
    // Body vertices never move.
    const body = Array.from({ length: attr.count }, (_, k) => k).find((k) => attr.getW(k) === 0)!;
    const p = new THREE.Vector3(pos.getX(body), pos.getY(body), pos.getZ(body));
    expect(limbPoint([attr.getX(body), attr.getY(body), attr.getZ(body), 0], 0.7, p).distanceTo(p)).toBe(0);
  });

  it.each([NpcType.Squirrel, NpcType.Crow, NpcType.Coyote, NpcType.CoyoteAlpha])('type %i steps with its legs in turn', (key) => {
    const signs = new Set(NPC_MODELS[key].filter((b) => b.limb).map((b) => Math.sign(b.limb!)));
    expect(signs).toEqual(new Set([1, -1]));
  });

  it.each(LEGLESS)('type %i without legs bobs and tilts while moving', (key) => {
    const n = new Npcs();
    const lifts: number[] = [];
    const leans: number[] = [];
    run(n, 1, walking(key), () => {
      const s = sample(n, key);
      lifts.push(s.pos.y);
      leans.push(lean(s));
    });
    expect(NPC_MODELS[key].some((b) => b.limb)).toBe(false);
    expect(Math.max(...lifts)).toBeGreaterThan(0.01);
    expect(Math.max(...leans)).toBeGreaterThan(0.02);
    expect(Math.min(...leans)).toBeLessThan(-0.02);
  });

  it('spins the delivery robot wheels as it rolls', () => {
    const n = new Npcs();
    const limbs: number[] = [];
    run(n, 0.5, walking(NpcType.RogueDeliveryRobot), () => limbs.push(sample(n, NpcType.RogueDeliveryRobot).limb));
    expect(NPC_MODELS[NpcType.RogueDeliveryRobot].filter((b) => b.wheel)).toHaveLength(6);
    expect(new Set(limbs.map((a) => a.toFixed(3))).size).toBeGreaterThan(10);
  });

  it.each(KEYS)('type %i is at its rest pose once it stops', (key) => {
    const n = new Npcs();
    let z = 0;
    run(n, 2, (t) => [{ id: 300, npcType: key, x: 1, z: (z = Math.min(t, 1) * 2), heading: 0 }], () => undefined);
    const s = sample(n, key);
    expect(z).toBe(2);
    expect(s.limb).toBe(0);
    expect(s.pos.toArray()).toEqual([1, 0, 2]);
    expect(s.q.angleTo(new THREE.Quaternion())).toBeCloseTo(0, 6);
  });

  it('injects the limb shader into the colour and shadow materials', () => {
    const n = new Npcs();
    n.update([{ id: 1, npcType: NpcType.Coyote, x: 0, z: 0, heading: 0 }]);
    const mesh = n.meshFor(NpcType.Coyote)!;
    const check = (material: THREE.Material, source: string) => {
      const shader = { vertexShader: source } as THREE.WebGLProgramParametersWithUniforms;
      material.onBeforeCompile(shader, undefined as never);
      for (const chunk of Object.values(NPC_LIMB_GLSL)) expect(shader.vertexShader).toContain(chunk);
    };
    check(mesh.material as THREE.Material, THREE.ShaderLib.lambert.vertexShader);
    check(mesh.customDepthMaterial!, THREE.ShaderLib.depth.vertexShader);
    expect((mesh.geometry.getAttribute('npcSwing') as THREE.InstancedBufferAttribute).isInstancedBufferAttribute).toBe(true);
  });
});

/** A scene with the local player and one NPC of `key` at (3, 0) facing +Z, on a clock the test advances. */
function combatScene(key: number) {
  let t = 10;
  const w = new WorldScene(() => t * 1000);
  const npc: NpcView = { id: 500, npcType: key, x: 3, z: 0, heading: 0 };
  const frame = (hits?: HitView[]): void => {
    const v: WorldView = { local: { id: LOCAL, x: 0, z: 0, heading: 0 }, remotes: [], npcs: [npc], ...(hits ? { hits } : {}) };
    w.update(v);
  };
  const mesh = () => w.scene.getObjectByName(`npc-${key}`) as THREE.InstancedMesh;
  const pose = () => {
    const m = new THREE.Matrix4();
    mesh().getMatrixAt(0, m);
    const s: Sample = { pos: new THREE.Vector3(), q: new THREE.Quaternion(), scale: new THREE.Vector3(), limb: 0 };
    m.decompose(s.pos, s.q, s.scale);
    return s;
  };
  const play = (seconds: number, each: (s: number) => void): void => {
    const start = t;
    for (let f = 1; f <= Math.round(seconds * FPS); f++) {
      t = start + f * DT;
      frame();
      each(t - start);
    }
  };
  frame();
  return { w, frame, play, pose, id: npc.id };
}

describe('NPC attack and flinch (AC3)', () => {
  it.each(KEYS)('type %i lunges forward when attacking and is back at rest within one combat tick', (key) => {
    const { frame, play, pose, id } = combatScene(key);
    const rest = pose();
    frame([{ attacker: id, target: LOCAL, damage: 2, died: false }]);
    let reach = 0;
    let pitch = 0;
    play(TICK, (s) => {
      const p = pose();
      reach = Math.max(reach, p.pos.z - rest.pos.z);
      pitch = Math.max(pitch, lean(p));
      if (s >= ATTACK_DURATION) {
        expect(p.pos.distanceTo(rest.pos)).toBeLessThan(1e-6);
        expect(p.q.angleTo(rest.q)).toBeCloseTo(0, 6);
      }
    });
    expect(ATTACK_DURATION).toBeLessThan(TICK);
    expect(reach).toBeGreaterThan(0.1);
    expect(pitch).toBeGreaterThan(0.15);
  });

  it.each(KEYS.flatMap((k) => [[k, 3], [k, 0]]))('type %i flinches back when targeted (damage %i) and returns to rest', (key, damage) => {
    const { frame, play, pose, id } = combatScene(key);
    const rest = pose();
    frame([{ target: id, damage, died: false }]);
    let back = 0;
    play(TICK, (s) => {
      const p = pose();
      back = Math.min(back, lean(p));
      // A flinch rocks in place: the feet stay put.
      expect(p.pos.distanceTo(rest.pos)).toBeLessThan(1e-6);
      if (s >= FLINCH_DURATION) expect(p.q.angleTo(rest.q)).toBeCloseTo(0, 6);
    });
    expect(FLINCH_DURATION).toBeLessThan(TICK);
    expect(back).toBeLessThan(-0.2);
  });

  it('leaves NPCs that are neither attacker nor target alone', () => {
    const { frame, play, pose } = combatScene(NpcType.Crow);
    const rest = pose();
    frame([{ attacker: LOCAL, target: 999, damage: 2, died: false }]);
    play(TICK, () => {
      expect(pose().pos.distanceTo(rest.pos)).toBe(0);
      expect(pose().q.angleTo(rest.q)).toBeCloseTo(0, 6);
    });
  });
});

describe('NPC animations stay instanced (AC4)', () => {
  it('adds one draw call per NPC type however many walk, breathe, attack and flinch', () => {
    const types = Object.values(NpcType);
    const scene = (count: number) => {
      let t = 10;
      const w = new WorldScene(() => t * 1000);
      const npcs = (moved: number): NpcView[] =>
        Array.from({ length: count }, (_, i) => ({ id: 1000 + i, npcType: types[i % types.length], x: (i % 10) * 2, z: Math.floor(i / 10) * 2 + (i % 2 ? moved : 0), heading: 0 }));
      const hits: HitView[] = Array.from({ length: count }, (_, i) => ({ attacker: 1000 + i, target: 1000 + ((i + 1) % count), damage: i % 3, died: false }));
      w.update({ local: { id: LOCAL, x: 0, z: 0, heading: 0 }, remotes: [], npcs: npcs(0), hits });
      for (let f = 1; f <= 12; f++) {
        t = 10 + f * DT;
        w.update({ local: { id: LOCAL, x: 0, z: 0, heading: 0 }, remotes: [], npcs: npcs(f * 0.05) });
      }
      return w;
    };
    for (const count of [6, 100, 300]) {
      const w = scene(count);
      const group = w.scene.getObjectByName('npcs')!;
      expect(countDrawCalls(group)).toBe(types.length);
      let meshes = 0;
      group.traverse((o) => {
        if ((o as THREE.Mesh).isMesh) {
          meshes++;
          expect((o as THREE.InstancedMesh).isInstancedMesh).toBe(true);
        }
      });
      expect(meshes).toBe(types.length);
    }
    expect(countDrawCalls(scene(300).scene)).toBe(countDrawCalls(scene(6).scene));
  });
});

describe('NPC teleports', () => {
  it('draws an NPC that jumps further than NPC_TELEPORT in one frame at rest', () => {
    const n = new Npcs();
    n.update([{ id: 1, npcType: NpcType.Coyote, x: 0, z: 0, heading: 0 }], 0);
    n.update([{ id: 1, npcType: NpcType.Coyote, x: NPC_TELEPORT + 1, z: 0, heading: 0 }], DT);
    const s = sample(n, NpcType.Coyote);
    expect(s.limb).toBe(0);
    expect(s.pos.y).toBe(0);
  });
});
