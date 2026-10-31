import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { ItemState } from '../state/items';
import { BREATH_PERIOD, CHARACTER_HEIGHT, Character, HAND_Y, PARTS, makePartGeometry } from './character';
import { SLOTS, slotGeometry } from './equipment';
import { CHARACTER_BUDGET } from './modelBudget';
import { NpcType } from './npcs';
import { WorldScene, countDrawCalls } from './scene';
import type { NpcView, PlayerView, WorldView } from './worldView';

/** Bounding box of a part (or its attachment geometry) in character space, for a posed Character. */
function boxOf(c: Character, part: string, geometry = makePartGeometry(PARTS.find((p) => p.name === part)!)): THREE.Box3 {
  geometry.computeBoundingBox();
  return geometry.boundingBox!.clone().applyMatrix4(c.joint(part)!.matrixWorld);
}

function idle(): Character {
  const c = new Character();
  c.update(0, 0, 0, 0);
  return c;
}

function tris(g: THREE.BufferGeometry): number {
  return (g.index ? g.index.count : g.getAttribute('position').count) / 3;
}

function remotes(n: number): PlayerView[] {
  return Array.from({ length: n }, (_, i) => ({ id: i + 1, x: (i % 10) * 2, z: Math.floor(i / 10) * 2, heading: 0 }));
}

function view(n: number, npcs?: NpcView[]): WorldView {
  return { local: { id: 0, x: 0, z: 0, heading: 0 }, remotes: remotes(n), npcs };
}

function partMesh(w: WorldScene, name: string): THREE.InstancedMesh {
  return w.scene.getObjectByName(`player:${name}`) as THREE.InstancedMesh;
}

describe('chunky character model', () => {
  it('has a head about a third of its height and stays within the triangle budget (AC1)', () => {
    const c = idle();
    const all = new THREE.Box3();
    for (const p of PARTS) all.union(boxOf(c, p.name));
    const total = all.max.y - all.min.y;
    expect(all.min.y).toBeCloseTo(0);
    expect(total).toBeCloseTo(CHARACTER_HEIGHT);

    const head = boxOf(c, 'head');
    const withHair = head.clone().union(boxOf(c, 'hair')).union(boxOf(c, 'eyes'));
    for (const box of [head, withHair]) {
      const ratio = (box.max.y - box.min.y) / total;
      expect(ratio).toBeGreaterThanOrEqual(0.28);
      expect(ratio).toBeLessThanOrEqual(0.4);
    }

    const body = PARTS.reduce((a, p) => a + tris(makePartGeometry(p)), 0);
    const equipped = body + SLOTS.reduce((a, s) => a + tris(slotGeometry(s)), 0);
    console.log(`character: ${body} triangles, ${equipped} fully equipped`);
    expect(equipped).toBeLessThanOrEqual(CHARACTER_BUDGET.maxTriangles);
    expect(CHARACTER_BUDGET.maxTriangles).toBe(1500);
  });

  it('has separate flat-coloured eye and hair meshes that cast shadows (AC2)', () => {
    const w = new WorldScene(() => 0);
    w.update(view(3));
    const eyes = partMesh(w, 'eyes');
    const hair = partMesh(w, 'hair');
    const head = partMesh(w, 'head');
    for (const m of [eyes, hair, head]) {
      expect(m.isInstancedMesh).toBe(true);
      const mat = m.material as THREE.MeshLambertMaterial;
      expect(mat.map).toBeNull();
      expect(mat.vertexColors).toBe(false);
    }
    const colors = new Set([eyes, hair, head].map((m) => (m.material as THREE.MeshLambertMaterial).color.getHex()));
    expect(colors.size).toBe(3);
    expect(eyes.geometry).not.toBe(head.geometry);
    expect(hair.geometry).not.toBe(head.geometry);
    for (const p of PARTS) expect(partMesh(w, p.name).castShadow).toBe(true);
  });

  it('draws local and remote players with the same instanced meshes (AC2)', () => {
    const w = new WorldScene(() => 0);
    w.update(view(5));
    // The local player is the last instance of each part mesh; nothing else draws a body.
    for (const p of PARTS) expect(partMesh(w, p.name).count).toBe(6);
    const bodies: THREE.Mesh[] = [];
    w.scene.traverse((o) => {
      if ((o as THREE.Mesh).isMesh && PARTS.some((p) => (o as THREE.Mesh).geometry === makePartGeometry(p))) bodies.push(o as THREE.Mesh);
    });
    expect(bodies).toHaveLength(PARTS.length);
    // An idle remote at the origin with the local's breathing phase poses exactly like the local player.
    const twin = new WorldScene(() => 0);
    twin.update({ local: { id: 0, x: 50, z: 50, heading: 0 }, remotes: [{ id: 0, x: 0, z: 0, heading: 0 }] });
    const a = new THREE.Matrix4();
    const b = new THREE.Matrix4();
    for (const p of PARTS) {
      partMesh(w, p.name).getMatrixAt(5, a);
      partMesh(twin, p.name).getMatrixAt(0, b);
      a.elements.forEach((v, i) => expect(v).toBeCloseTo(b.elements[i], 6));
    }
  });

  it('keeps draw calls the same whatever the number of remote players (AC2)', () => {
    const calls = (n: number) => {
      const w = new WorldScene(() => 0);
      w.update(view(n));
      return countDrawCalls(w.scene);
    };
    expect(calls(100)).toBe(calls(1));
    expect(calls(100)).toBe(calls(0));
  });
});

describe('equipment on the chunky model (AC3)', () => {
  it('puts a hat in the head box and the weapon in the hand', () => {
    const s = new ItemState();
    s.setEntityInfo({ id: 0, kind: 0, npcType: 0, combatLevel: 3, equipment: { weapon: 1, head: 6, body: 4, shield: 5 } } as never);
    const w = new WorldScene(() => 0);
    w.update({ ...view(0), entities: s });
    const local = w.scene.children.find((o) => o.children.length === PARTS.length)!;
    const c = new Character();
    c.update(0, 0, 0, 0);

    const def = (slot: string) => SLOTS.find((d) => d.slot === slot)!;
    const hat = boxOf(c, 'head', slotGeometry(def('head')));
    const head = boxOf(c, 'head');
    expect(head.containsPoint(hat.getCenter(new THREE.Vector3()))).toBe(true);
    expect(hat.intersectsBox(head)).toBe(true);
    // The hat covers the hair on top.
    expect(hat.max.y).toBeGreaterThanOrEqual(boxOf(c, 'hair').max.y);

    const hand = new THREE.Vector3(0, HAND_Y, 0).applyMatrix4(c.joint('armR')!.matrixWorld);
    const arm = boxOf(c, 'armR');
    expect(arm.containsPoint(hand)).toBe(true);
    expect(boxOf(c, 'armR', slotGeometry(def('weapon'))).containsPoint(hand)).toBe(true);
    expect(boxOf(c, 'torso', slotGeometry(def('body'))).intersectsBox(boxOf(c, 'torso'))).toBe(true);
    expect(boxOf(c, 'armL', slotGeometry(def('shield'))).intersectsBox(boxOf(c, 'armL'))).toBe(true);

    // In the scene each worn slot hangs off its body part's joint.
    for (const d of SLOTS) {
      const mesh = local.getObjectByName(`equip:${d.slot}`) as THREE.Mesh;
      expect(mesh.parent!.name).toBe(d.part);
      expect(mesh.castShadow).toBe(true);
    }
  });
});

describe('blob shadows (AC4)', () => {
  const npcs: NpcView[] = [
    { id: 500, npcType: NpcType.Rabbit, x: 3, z: 3, heading: 0 },
    { id: 501, npcType: NpcType.Coyote, x: 5, z: 3, heading: 0 },
    { id: 502, npcType: 999, x: 7, z: 3, heading: 0 },
  ];

  function shadowMeshes(w: WorldScene): THREE.InstancedMesh[] {
    const out: THREE.InstancedMesh[] = [];
    w.scene.traverse((o) => {
      if (o.name === 'blobShadows' && (o as THREE.InstancedMesh).isInstancedMesh) out.push(o as THREE.InstancedMesh);
    });
    return out;
  }

  it('draws one shadow per drawn player and NPC with a single instanced mesh', () => {
    const w = new WorldScene(() => 0);
    for (const [n, list] of [[0, undefined], [10, npcs], [100, npcs], [150, npcs], [3, []]] as const) {
      w.update(view(n, list as NpcView[] | undefined));
      const meshes = shadowMeshes(w);
      expect(meshes).toHaveLength(1);
      expect(meshes[0].count).toBe(1 + Math.min(n, 100) + (list?.length ?? 0));
    }
  });

  it('places each shadow flat on the ground under its entity', () => {
    const w = new WorldScene(() => 0);
    w.update(view(2, npcs));
    const mesh = shadowMeshes(w)[0];
    const m = new THREE.Matrix4();
    const p = new THREE.Vector3();
    const q = new THREE.Quaternion();
    const s = new THREE.Vector3();
    const expected = [[0, 0], [0, 0], [2, 0], [3, 3], [5, 3], [7, 3]];
    // Local first, then remotes, then NPCs.
    expected.forEach(([x, z], i) => {
      mesh.getMatrixAt(i, m);
      m.decompose(p, q, s);
      expect(p.x).toBeCloseTo(x);
      expect(p.z).toBeCloseTo(z);
      expect(p.y).toBeGreaterThan(0);
      expect(p.y).toBeLessThan(0.05);
      expect(s.x).toBeGreaterThan(0.1);
    });
    // The coyote is bigger than the rabbit, so its shadow is too.
    mesh.getMatrixAt(3, m);
    const rabbit = new THREE.Vector3().setFromMatrixScale(m).x;
    mesh.getMatrixAt(4, m);
    expect(new THREE.Vector3().setFromMatrixScale(m).x).toBeGreaterThan(rabbit);

    const mat = mesh.material as THREE.MeshBasicMaterial;
    expect(mat.transparent).toBe(true);
    expect(mat.depthWrite).toBe(false);
    expect(mat.map).toBeNull();
    // Soft: dark centre, clear rim.
    const color = mesh.geometry.getAttribute('color');
    expect(color.itemSize).toBe(4);
    expect(color.getW(0)).toBe(1);
    expect(color.getW(color.count - 1)).toBe(0);
  });
});

describe('idle breathing (AC5)', () => {
  it('swells a standing character body over time and returns to rest', () => {
    const c = new Character();
    const torso = c.joint('torso')!;
    const head = c.joint('head')!;
    c.update(0, 0, 0, 0, 0, 0);
    const rest = torso.scale.clone();
    const restHead = head.position.y;
    expect(rest.toArray()).toEqual([1, 1, 1]);

    c.update(0, 0, 0, 0, 0, BREATH_PERIOD / 4);
    expect(torso.scale.x).toBeGreaterThan(1.01);
    expect(head.position.y).toBeGreaterThan(restHead);
    c.update(0, 0, 0, 0, 0, (3 * BREATH_PERIOD) / 4);
    expect(torso.scale.x).toBeLessThan(0.99);

    c.update(0, 0, 0, 0, 0, BREATH_PERIOD);
    torso.scale.toArray().forEach((v) => expect(v).toBeCloseTo(1, 9));
    expect(head.position.y).toBeCloseTo(restHead, 9);

    // Walking holds the rest scale.
    c.update(0.5, 0, 0, 0.5, 0, BREATH_PERIOD / 4);
    expect(torso.scale.x).toBe(1);
  });

  it('breathes remote players too, on the scene clock', () => {
    let t = 0;
    const w = new WorldScene(() => t * 1000);
    const torso = partMesh(w, 'torso');
    const m = new THREE.Matrix4();
    const scaleAt = (time: number) => {
      t = time;
      w.update(view(1));
      torso.getMatrixAt(0, m);
      return new THREE.Vector3().setFromMatrixScale(m).x;
    };
    const values = [0, 0.4, 0.8, 1.2, 1.6, 2.0, 2.4, 2.8].map((x) => scaleAt(x * (BREATH_PERIOD / 3.2)));
    expect(Math.max(...values) - Math.min(...values)).toBeGreaterThan(0.02);
    const first = scaleAt(10 * BREATH_PERIOD);
    expect(scaleAt(11 * BREATH_PERIOD)).toBeCloseTo(first, 6);
  });
});
