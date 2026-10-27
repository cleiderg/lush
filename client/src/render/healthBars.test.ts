import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { ItemState } from '../state/items';
import { WorldScene } from './scene';
import type { WorldView } from './worldView';

function view(s: ItemState): WorldView {
  return {
    local: { id: 0, x: 0, z: 0, heading: 0 },
    remotes: [
      { id: 1, x: 2, z: 0, heading: 0 },
      { id: 2, x: 4, z: 0, heading: 0 },
    ],
    entities: s,
  };
}

/** Ratio as the codec delivers it: protocol byte / 255. */
const health = (s: ItemState, id: number, byte: number) => s.setHealth([{ id, ratio: byte / 255 }]);

describe('health bars', () => {
  it('shows bars only for entities below full health', () => {
    const s = new ItemState();
    const w = new WorldScene();
    w.update(view(s));
    expect(w.healthBars.back.count).toBe(0);
    expect(w.healthBars.fill.count).toBe(0);

    health(s, 1, 128);
    health(s, 0, 254);
    health(s, 2, 255);
    w.update(view(s));
    expect(w.healthBars.back.count).toBe(2);
    expect(w.healthBars.fill.count).toBe(2);

    // The fill is scaled to the ratio.
    const m = new THREE.Matrix4();
    const scales: number[] = [];
    for (let i = 0; i < 2; i++) {
      w.healthBars.fill.getMatrixAt(i, m);
      scales.push(new THREE.Vector3().setFromMatrixScale(m).x);
    }
    expect(scales.sort()).toEqual([128 / 255, 254 / 255].map((v) => expect.closeTo(v, 5)));
  });

  it('hides a bar again when health returns to full', () => {
    const s = new ItemState();
    const w = new WorldScene();
    health(s, 1, 10);
    w.update(view(s));
    expect(w.healthBars.back.count).toBe(1);
    health(s, 1, 255);
    w.update(view(s));
    expect(w.healthBars.back.count).toBe(0);
    expect(w.healthBars.fill.count).toBe(0);
  });

  it('draws bars above the entity, facing the camera', () => {
    const s = new ItemState();
    const w = new WorldScene();
    health(s, 2, 0);
    w.update(view(s));
    const m = new THREE.Matrix4();
    w.healthBars.back.getMatrixAt(0, m);
    const pos = new THREE.Vector3();
    const rot = new THREE.Quaternion();
    m.decompose(pos, rot, new THREE.Vector3());
    expect(pos.x).toBeCloseTo(4);
    expect(pos.y).toBeGreaterThan(1.8);
    expect(rot.angleTo(w.rig.camera.quaternion)).toBeCloseTo(0);
  });
});
