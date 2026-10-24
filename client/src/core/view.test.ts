import { describe, expect, it } from 'vitest';
import { GameState } from '../state/world';
import { buildWorldView, controlYaw, moveForRigYaw } from './view';

describe('buildWorldView', () => {
  it('uses the predicted local player and interpolated remotes', () => {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 7, tickRate: 20, spawn: { x: 3, z: 4 } } as never, 0);
    g.handle(
      {
        type: 'snapshot',
        tick: 1,
        lastInputSeq: 0,
        entities: [
          { id: 7, x: 3, z: 4, heading: 0 },
          { id: 9, x: 10, z: 11, heading: 1 },
        ],
      } as never,
      0,
    );
    const v = buildWorldView(g, 1000);
    expect(v.local).toMatchObject({ id: 7, x: 3, z: 4 });
    expect(v.remotes).toEqual([{ id: 9, x: 10, z: 11, heading: 1 }]);
  });
});

describe('camera yaw for controls', () => {
  it('pushing up moves away from the camera for any rig yaw', () => {
    for (const yaw of [0, 0.7, Math.PI / 2, -2]) {
      const m = moveForRigYaw({ x: 0, y: -1 }, yaw);
      expect(m.x).toBeCloseTo(Math.sin(yaw));
      expect(m.y).toBeCloseTo(Math.cos(yaw));
    }
  });

  it('direction changes when the camera yaw changes', () => {
    const a = moveForRigYaw({ x: 0, y: -1 }, 0);
    const b = moveForRigYaw({ x: 0, y: -1 }, Math.PI / 2);
    expect(a.y).toBeCloseTo(1); // yaw 0: camera north of player, forward is south
    expect(b.x).toBeCloseTo(1);
    expect(controlYaw(0)).toBeCloseTo(Math.PI);
  });
});
