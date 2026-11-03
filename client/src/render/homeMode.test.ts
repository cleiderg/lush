import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { WorldScene } from './scene';
import { LAYER_FLOOR, ROOM_SIZE } from './roomView';
import type { RoomDescription, RoomItem } from './roomView';
import type { WorldView } from './worldView';

const item = (o: Partial<RoomItem> & Pick<RoomItem, 'placedId' | 'itemId'>): RoomItem =>
  ({ x: 0, y: 0, rotation: 0, layer: LAYER_FLOOR, parentPlacedId: 0, slot: 0, side: 0, ...o });
const room = (...items: RoomItem[]): RoomDescription => ({ wallpaperId: 0, floorId: 0, items });
const view = (home?: RoomDescription): WorldView => ({ local: { id: 0, x: 40, z: 60, heading: 0 }, remotes: [], home });
const aspect = 9 / 19.5;

function setup() {
  const w = new WorldScene();
  w.rig.camera.aspect = aspect;
  w.rig.camera.updateProjectionMatrix();
  return w;
}
const campus = (w: WorldScene) => [w.map.group, w.props.group, w.nature.group, w.skills.group, w.fountain.group, w.cafe.group];
const roomGroup = (w: WorldScene) => w.scene.children.find((c) => c.name === 'room');

describe('home room mode', () => {
  it('AC1: builds the room, hides the campus and frames all four floor corners', () => {
    const w = setup();
    w.update(view());
    expect(roomGroup(w)).toBeUndefined();
    w.update(view(room()));
    expect(roomGroup(w)).toBeDefined();
    for (const g of campus(w)) expect(g.visible).toBe(false);
    expect(w.scene.children.find((c) => c.name === 'ground')!.visible).toBe(false);
    for (const yaw of [0, 1, 2.5, -2]) {
      w.rig.yaw = yaw;
      w.update(view(room()));
      const cam = w.rig.camera;
      cam.updateMatrixWorld();
      const dir = new THREE.Vector3();
      cam.getWorldDirection(dir);
      const toCentre = new THREE.Vector3(ROOM_SIZE / 2, 1.2, ROOM_SIZE / 2).sub(cam.position).normalize();
      expect(dir.dot(toCentre)).toBeGreaterThan(0.999);
      for (const [x, z] of [[0, 0], [ROOM_SIZE, 0], [0, ROOM_SIZE], [ROOM_SIZE, ROOM_SIZE]]) {
        const p = new THREE.Vector3(x, 0, z).project(cam);
        expect(Math.abs(p.x)).toBeLessThan(1);
        expect(Math.abs(p.y)).toBeLessThan(1);
        expect(p.z).toBeLessThan(1);
      }
    }
  });

  it('AC2: changing the description updates the room in place', () => {
    const w = setup();
    w.update(view(room(item({ placedId: 1, itemId: 31, x: 1, y: 1 }), item({ placedId: 2, itemId: 29, x: 5, y: 5 }))));
    const g = roomGroup(w)!;
    const m1 = g.getObjectByName('placed_1')!;
    const m2 = g.getObjectByName('placed_2')!;
    w.update(view(room(item({ placedId: 1, itemId: 31, x: 3, y: 4 }), item({ placedId: 3, itemId: 29, x: 6, y: 1 }))));
    expect(roomGroup(w)).toBe(g);
    expect(g.getObjectByName('placed_1')).toBe(m1);
    expect(m1.position.x).toBeGreaterThan(3);
    expect(g.getObjectByName('placed_2')).toBeUndefined();
    expect(m2.parent).toBeNull();
    expect(g.getObjectByName('placed_3')).toBeDefined();
  });

  it('AC3: removing the description removes the room and restores the campus and camera', () => {
    const w = setup();
    w.update(view());
    const before = w.rig.camera.position.clone();
    w.update(view(room(item({ placedId: 1, itemId: 31 }))));
    expect(w.rig.camera.position.distanceTo(before)).toBeGreaterThan(5);
    w.update(view());
    expect(roomGroup(w)).toBeUndefined();
    for (const g of campus(w)) expect(g.visible).toBe(true);
    expect(w.scene.children.find((c) => c.name === 'ground')!.visible).toBe(true);
    expect(w.rig.camera.position.distanceTo(before)).toBeLessThan(1e-6);
  });
});
