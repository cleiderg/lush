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

  it('carries ground items as the codec gave them', () => {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 7, tickRate: 20, spawn: { x: 0, z: 0 } } as never, 0);
    const items = [
      { id: 1, itemId: 5, quantity: 3, x: 12.5, z: -4.2 },
      { id: 2, itemId: 9, quantity: 1, x: 0.1, z: 7 },
    ];
    g.handle({ type: 'groundItems', items } as never, 0);
    expect(buildWorldView(g, 0).groundItems).toEqual(items);
    expect(buildWorldView(g, 0).groundItems).toEqual(g.items.groundItems);
  });

  it('passes each hit batch once, flattened oldest first', () => {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 7, tickRate: 20, spawn: { x: 0, z: 0 } } as never, 0);
    g.handle(
      {
        type: 'combatHit',
        tick: 4,
        hits: [
          { attacker: 7, target: 9, damage: 3, died: false },
          { attacker: 9, target: 7, damage: 0, died: false },
        ],
      } as never,
      0,
    );
    g.handle({ type: 'combatHit', tick: 5, hits: [{ attacker: 7, target: 9, damage: 2, died: true }] } as never, 0);
    expect(buildWorldView(g, 0).hits).toEqual([
      { target: 9, damage: 3, died: false },
      { target: 7, damage: 0, died: false },
      { target: 9, damage: 2, died: true },
    ]);
    expect(buildWorldView(g, 0).hits).toEqual([]);
  });

  it('has no ground items or hits after a Welcome', () => {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 7, tickRate: 20, spawn: { x: 0, z: 0 } } as never, 0);
    g.handle({ type: 'groundItems', items: [{ id: 1, itemId: 5, quantity: 1, x: 1, z: 1 }] } as never, 0);
    g.handle({ type: 'combatHit', tick: 4, hits: [{ attacker: 7, target: 9, damage: 3, died: false }] } as never, 0);
    g.handle({ type: 'welcome', playerId: 7, tickRate: 20, spawn: { x: 0, z: 0 } } as never, 0);
    const v = buildWorldView(g, 0);
    expect(v.groundItems).toEqual([]);
    expect(v.hits).toEqual([]);
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
