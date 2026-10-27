import { describe, expect, it } from 'vitest';
import { EntityKind } from '../protocol';
import { OVERHEAD_CHAT_MS } from '../state/chat';
import { GameState } from '../state/world';
import { buildWorldView, controlYaw, moveForRigYaw } from './view';

describe('buildWorldView', () => {
  function withRemote(): GameState {
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
    return g;
  }

  it('passes the current overhead chat of the local player and remotes', () => {
    const g = withRemote();
    g.handle({ type: 'chat', speakerId: 7, text: 'hi' } as never, 1000);
    g.handle({ type: 'chat', speakerId: 9, text: 'yo' } as never, 1000);
    const chat = buildWorldView(g, 1000).chat ?? [];
    expect([...chat].sort((a, b) => a.id - b.id)).toEqual([
      { id: 7, text: 'hi' },
      { id: 9, text: 'yo' },
    ]);
  });

  it('leaves out messages older than OVERHEAD_CHAT_MS', () => {
    const g = withRemote();
    g.handle({ type: 'chat', speakerId: 9, text: 'old' } as never, 1000);
    g.handle({ type: 'chat', speakerId: 7, text: 'new' } as never, 1000 + OVERHEAD_CHAT_MS);
    expect(buildWorldView(g, 1000 + OVERHEAD_CHAT_MS).chat).toEqual([{ id: 7, text: 'new' }]);
  });

  it('leaves out speakers that are neither local nor in view', () => {
    const g = withRemote();
    g.handle({ type: 'chat', speakerId: 42, text: 'far away' } as never, 1000);
    g.handle({ type: 'chat', speakerId: 9, text: 'near' } as never, 1000);
    expect(buildWorldView(g, 1000).chat).toEqual([{ id: 9, text: 'near' }]);
  });

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

  it('carries the game item state as entities for the local player and remotes', () => {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 7, tickRate: 20, spawn: { x: 0, z: 0 } } as never, 0);
    g.handle(
      { type: 'snapshot', tick: 1, lastInputSeq: 0, entities: [{ id: 7, x: 0, z: 0, heading: 0 }, { id: 9, x: 1, z: 1, heading: 0 }] } as never,
      0,
    );
    const equip = (weapon: number) => ({ weapon, head: 0, body: 0, shield: 0 });
    g.handle({ type: 'entityInfo', id: 7, kind: EntityKind.Player, npcType: 0, combatLevel: 3, equipment: equip(11) } as never, 0);
    g.handle({ type: 'entityInfo', id: 9, kind: EntityKind.Player, npcType: 0, combatLevel: 3, equipment: equip(12) } as never, 0);
    g.handle({ type: 'entityHealth', entries: [{ id: 7, ratio: 0.5 }, { id: 9, ratio: 0.25 }] } as never, 0);
    const v = buildWorldView(g, 1000);
    expect(v.entities).toBe(g.items);
    expect(v.entities?.entityInfo(v.local.id)?.equipment.weapon).toBe(11);
    expect(v.entities?.healthRatio(v.local.id)).toBe(0.5);
    expect(v.remotes.map((r) => r.id)).toEqual([9]);
    expect(v.entities?.entityInfo(v.remotes[0].id)?.equipment.weapon).toBe(12);
    expect(v.entities?.healthRatio(v.remotes[0].id)).toBe(0.25);
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

describe('buildWorldView NPCs', () => {
  const equipment = { weapon: 0, head: 0, body: 0, shield: 0 };
  const info = (id: number, kind: EntityKind, npcType: number) =>
    ({ type: 'entityInfo', id, kind, npcType, combatLevel: 3, equipment }) as never;
  const snapshot = (tick: number, entities: { id: number; x: number; z: number; heading: number }[]) =>
    ({ type: 'snapshot', tick, lastInputSeq: 0, entities: [{ id: 7, x: 0, z: 0, heading: 0 }, ...entities] }) as never;

  function game(): GameState {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 7, tickRate: 20, spawn: { x: 0, z: 0 } } as never, 0);
    return g;
  }

  it('puts an Npc entity in npcs with its npcType, position and heading, not in remotes', () => {
    const g = game();
    g.handle(snapshot(1, [{ id: 20, x: 5, z: 6, heading: 0.5 }]), 0);
    g.handle(info(20, EntityKind.Npc, 4), 0);
    const v = buildWorldView(g, 1000);
    expect(v.npcs).toEqual([{ id: 20, npcType: 4, x: 5, z: 6, heading: 0.5 }]);
    expect(v.remotes).toEqual([]);
  });

  it('keeps players and entities with no EntityInfo yet in remotes', () => {
    const g = game();
    g.handle(snapshot(1, [
      { id: 9, x: 1, z: 2, heading: 0 },
      { id: 10, x: 3, z: 4, heading: 1 },
      { id: 20, x: 5, z: 6, heading: 2 },
    ]), 0);
    g.handle(info(9, EntityKind.Player, 0), 0);
    g.handle(info(20, EntityKind.Npc, 2), 0);
    const v = buildWorldView(g, 1000);
    expect(v.remotes).toEqual([
      { id: 9, x: 1, z: 2, heading: 0 },
      { id: 10, x: 3, z: 4, heading: 1 },
    ]);
    expect(v.npcs).toEqual([{ id: 20, npcType: 2, x: 5, z: 6, heading: 2 }]);
  });

  it('drops an NPC whose entity leaves the snapshot', () => {
    const g = game();
    g.handle(snapshot(1, [{ id: 20, x: 5, z: 6, heading: 0 }]), 0);
    g.handle(info(20, EntityKind.Npc, 4), 0);
    expect(buildWorldView(g, 1000).npcs).toHaveLength(1);
    g.handle(snapshot(2, []), 1000);
    const v = buildWorldView(g, 2000);
    expect(v.npcs).toEqual([]);
    expect(v.remotes).toEqual([]);
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
