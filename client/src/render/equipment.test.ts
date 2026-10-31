import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { ItemState } from '../state/items';
import { PARTS } from './character';
import { SLOTS } from './equipment';
import { WorldScene, countDrawCallsInView } from './scene';
import type { EquipmentView, PlayerView, WorldView } from './worldView';

const NONE: EquipmentView = { weapon: 0, head: 0, body: 0, shield: 0 };
const KIT: EquipmentView = { weapon: 1, head: 6, body: 4, shield: 5 };

function items(): ItemState {
  const s = new ItemState();
  s.setCatalog(
    [
      [1, 'Wooden stick'], [4, 'Hoodie'], [5, 'Cardboard shield'], [6, 'Leather cap'], [7, 'Bike helmet'],
    ].map(([id, name]) => ({ id, name }) as never),
  );
  return s;
}

function equip(s: ItemState, id: number, equipment: EquipmentView): void {
  s.setEntityInfo({ id, kind: 0, npcType: 0, combatLevel: 3, equipment } as never);
}

function view(s: ItemState, n: number): WorldView {
  const remotes: PlayerView[] = [];
  for (let i = 0; i < n; i++) remotes.push({ id: i + 1, x: (i % 10) * 2, z: Math.floor(i / 10) * 2, heading: 0 });
  return { local: { id: 0, x: 0, z: 0, heading: 0 }, remotes, entities: s };
}

function localPlayer(w: WorldScene): THREE.Object3D {
  return w.scene.children.find((c) => c.children.length === PARTS.length)!;
}

function attachedSlots(w: WorldScene): string[] {
  const names: string[] = [];
  localPlayer(w).traverse((o) => {
    if (o.name.startsWith('equip:')) names.push(o.name.slice(6));
  });
  return names.sort();
}

function remoteEquipment(w: WorldScene): THREE.InstancedMesh[] {
  return (w.scene.getObjectsByProperty('isInstancedMesh', true) as THREE.InstancedMesh[]).filter((m) =>
    m.name.startsWith('equip:'),
  );
}

describe('equipment attachments', () => {
  it('adds and removes local attachments per slot as EntityInfo changes', () => {
    const s = items();
    const w = new WorldScene();
    w.update(view(s, 0));
    expect(attachedSlots(w)).toEqual([]);

    equip(s, 0, KIT);
    w.update(view(s, 0));
    expect(attachedSlots(w)).toEqual(['body', 'head', 'shield', 'weapon']);
    // Each attachment hangs off the joint of its body part.
    for (const def of SLOTS) {
      const mesh = localPlayer(w).getObjectByName(`equip:${def.slot}`)!;
      const joint = localPlayer(w).children[PARTS.findIndex((p) => p.name === def.part)];
      expect(mesh.parent).toBe(joint);
    }

    equip(s, 0, { ...KIT, shield: 0, head: 0 });
    w.update(view(s, 0));
    expect(attachedSlots(w)).toEqual(['body', 'weapon']);

    // Swapping an item keeps the slot but changes its look.
    const before = (localPlayer(w).getObjectByName('equip:weapon') as THREE.Mesh).material;
    equip(s, 0, { ...NONE, weapon: 7, body: 4 });
    w.update(view(s, 0));
    expect((localPlayer(w).getObjectByName('equip:weapon') as THREE.Mesh).material).not.toBe(before);

    equip(s, 0, NONE);
    w.update(view(s, 0));
    expect(attachedSlots(w)).toEqual([]);
  });

  it('adds and removes remote attachment instances per slot', () => {
    const s = items();
    const w = new WorldScene();
    equip(s, 1, KIT);
    equip(s, 2, { ...NONE, weapon: 1 });
    w.update(view(s, 3));
    const count = (slot: string) =>
      remoteEquipment(w).filter((m) => m.name.startsWith(`equip:${slot}:`)).reduce((a, m) => a + m.count, 0);
    expect(count('weapon')).toBe(2);
    expect(count('head')).toBe(1);
    expect(count('body')).toBe(1);
    expect(count('shield')).toBe(1);

    equip(s, 1, { ...KIT, head: 0, shield: 0 });
    w.update(view(s, 3));
    expect(count('weapon')).toBe(2);
    expect(count('head')).toBe(0);
    expect(count('shield')).toBe(0);
    expect(count('body')).toBe(1);
  });

  it('attachments follow the limb they hang from', () => {
    const s = items();
    const w = new WorldScene();
    equip(s, 1, KIT);
    w.update(view(s, 1));
    const weapon = remoteEquipment(w).find((m) => m.name.startsWith('equip:weapon:'))!;
    const armR = (w.scene.getObjectsByProperty('isInstancedMesh', true) as THREE.InstancedMesh[])[
      PARTS.findIndex((p) => p.name === 'armR')
    ];
    const a = new THREE.Matrix4();
    const b = new THREE.Matrix4();
    weapon.getMatrixAt(0, a);
    armR.getMatrixAt(0, b);
    expect(a.elements).toEqual(b.elements);
  });

  it('shares geometry and materials per item and keeps remote players instanced', () => {
    const s = items();
    const w = new WorldScene();
    equip(s, 0, KIT);
    for (let id = 1; id <= 100; id++) equip(s, id, id % 2 ? KIT : { ...KIT, head: 7 });
    w.update(view(s, 100));

    const eq = remoteEquipment(w);
    // 4 slots for KIT plus the bike-helmet head: one instanced mesh each, whatever the player count.
    expect(eq.filter((m) => m.count > 0)).toHaveLength(5);
    expect(eq.reduce((a, m) => a + m.count, 0)).toBe(400);
    for (const m of eq) expect(m.isInstancedMesh).toBe(true);

    // The local player's attachments reuse the very same geometry and material objects.
    for (const def of SLOTS) {
      const local = localPlayer(w).getObjectByName(`equip:${def.slot}`) as THREE.Mesh;
      const remote = eq.find((m) => m.name.startsWith(`equip:${def.slot}:`) && m.material === local.material);
      expect(remote).toBeDefined();
      expect(remote!.geometry).toBe(local.geometry);
    }

    // No per-entity materials: distinct equipment materials equal distinct worn items.
    const mats = new Set<THREE.Material>();
    w.scene.traverse((o) => {
      if (o.name.startsWith('equip:')) mats.add((o as THREE.Mesh).material as THREE.Material);
    });
    expect(mats.size).toBe(5);

    // Every player hurt as well: equipment adds one call per distinct item, health bars two in total.
    s.setHealth(Array.from({ length: 101 }, (_, id) => ({ id, ratio: 0.5 })));
    w.update(view(s, 100));
    w.rig.camera.aspect = 9 / 19.5;
    w.rig.camera.updateProjectionMatrix();
    const calls = countDrawCallsInView(w.scene, w.rig.camera);
    // The same view with only the local player, unequipped: the world in view from the origin.
    const bare = new WorldScene();
    bare.update(view(items(), 0));
    bare.rig.camera.aspect = 9 / 19.5;
    bare.rig.camera.updateProjectionMatrix();
    const world = countDrawCallsInView(bare.scene, bare.rig.camera);
    console.log(`in view, 100 equipped and hurt players: ${calls} draw calls, ${world} of them the world`);
    // The players' own calls keep the bound they had before the world grew (40 calls with 28 of world).
    expect(calls - world).toBeLessThan(12);
    // The whole view, including the bank booth, grill and skill trees in view from the origin (#451).
    expect(calls).toBeLessThan(44);
  });
});
