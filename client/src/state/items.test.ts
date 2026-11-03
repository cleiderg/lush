import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import { decode } from '../net/codec';
import {
  CombatHit, EntityHealth, EntityInfo, EntityKind, Envelope, EquipSlot, GroundItem, GroundItems, HealthRatio, Hit,
  InventorySlot, InventoryState, ItemCatalog, ItemDef, ItemKind, Message, NpcCatalog, NpcDef, PlayerStats, ShopResult,
  ShopResultCode, Snapshot, Entity, Vec2, Welcome,
} from '../protocol';
import { MAX_HIT_BATCHES } from './combat';
import { ItemState } from './items';
import { GameState } from './world';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

function welcome(playerId: number): Uint8Array {
  const b = new flatbuffers.Builder(64);
  const t = b.createString('tok');
  Welcome.startWelcome(b);
  Welcome.addPlayerId(b, playerId);
  Welcome.addGuestToken(b, t);
  Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
  Welcome.addTickRate(b, 20);
  return finish(b, Message.Welcome, Welcome.endWelcome(b));
}

function stats(hp: number, maxHp: number, level: number, xp: number, gold: number): Uint8Array {
  const b = new flatbuffers.Builder(64);
  return finish(b, Message.PlayerStats, PlayerStats.createPlayerStats(b, hp, maxHp, level, xp, gold));
}

function inventory(slots: [number, number][], weapon: number, head: number, body: number, shield: number): Uint8Array {
  const b = new flatbuffers.Builder(256);
  InventoryState.startSlotsVector(b, slots.length);
  for (let i = slots.length - 1; i >= 0; i--) InventorySlot.createInventorySlot(b, slots[i][0], slots[i][1]);
  const v = b.endVector();
  return finish(b, Message.InventoryState, InventoryState.createInventoryState(b, v, weapon, head, body, shield));
}

function catalog(): Uint8Array {
  const b = new flatbuffers.Builder(256);
  const sword = ItemDef.createItemDef(b, 5, b.createString('Bronze sword'), ItemKind.Weapon, EquipSlot.Weapon, 1, 4, 3, -1, 4, 0, 10, true, 25);
  const fish = ItemDef.createItemDef(b, 9, b.createString('Fish'), ItemKind.Food, EquipSlot.None, 0, 0, 0, 0, 0, 3, 2, false, 0);
  const v = ItemCatalog.createItemsVector(b, [sword, fish]);
  return finish(b, Message.ItemCatalog, ItemCatalog.createItemCatalog(b, v));
}

function info(id: number, kind: EntityKind, npcType: number, level: number, weapon = 0, head = 0, body = 0, shield = 0): Uint8Array {
  const b = new flatbuffers.Builder(64);
  return finish(b, Message.EntityInfo, EntityInfo.createEntityInfo(b, id, kind, npcType, level, weapon, head, body, shield));
}

function health(entries: [number, number][]): Uint8Array {
  const b = new flatbuffers.Builder(64);
  EntityHealth.startEntriesVector(b, entries.length);
  for (let i = entries.length - 1; i >= 0; i--) HealthRatio.createHealthRatio(b, entries[i][0], entries[i][1]);
  const v = b.endVector();
  return finish(b, Message.EntityHealth, EntityHealth.createEntityHealth(b, v));
}

function snapshot(ids: number[]): Uint8Array {
  const b = new flatbuffers.Builder(128);
  Snapshot.startEntitiesVector(b, ids.length);
  for (let i = ids.length - 1; i >= 0; i--) Entity.createEntity(b, ids[i], 0, 0, 0, 0);
  const v = b.endVector();
  Snapshot.startSnapshot(b);
  Snapshot.addTick(b, 1);
  Snapshot.addLastInputSeq(b, 0);
  Snapshot.addEntities(b, v);
  return finish(b, Message.Snapshot, Snapshot.endSnapshot(b));
}

/** Decodes each envelope and feeds it to state, as the session does. */
function feed(game: GameState, ...envelopes: Uint8Array[]): void {
  for (const bytes of envelopes) {
    const msg = decode(bytes);
    expect(msg).not.toBeNull();
    game.handle(msg!, 0);
  }
}

function setup(): GameState {
  const game = new GameState();
  feed(game, welcome(1));
  return game;
}

describe('decode + GameState: player stats', () => {
  it('exposes HP, max HP, combat level, XP and gold', () => {
    const game = setup();
    feed(game, stats(7, 10, 3, 1234, 4000000000));
    expect(game.items.stats).toEqual({ hp: 7, maxHp: 10, combatLevel: 3, xp: 1234, gold: 4000000000 });
    feed(game, stats(10, 10, 3, 1300, 50));
    expect(game.items.stats.hp).toBe(10);
    expect(game.items.stats.gold).toBe(50);
  });
});

describe('decode + GameState: inventory', () => {
  it('exposes slots, including empty ones, and equipment', () => {
    const game = setup();
    feed(game, inventory([[5, 1], [0, 0], [9, 3]], 5, 0, 7, 0));
    expect(game.items.inventory.slots).toEqual([
      { itemId: 5, quantity: 1 },
      { itemId: 0, quantity: 0 },
      { itemId: 9, quantity: 3 },
    ]);
    expect(game.items.inventory.equipment).toEqual({ weapon: 5, head: 0, body: 7, shield: 0 });
    feed(game, inventory([[0, 0], [0, 0], [9, 2]], 0, 0, 7, 0));
    expect(game.items.inventory.slots[2].quantity).toBe(2);
    expect(game.items.inventory.equipment.weapon).toBe(0);
  });
});

describe('decode + GameState: item catalog', () => {
  it('looks items up by id', () => {
    const game = setup();
    feed(game, catalog());
    expect(game.items.item(5)).toEqual({
      id: 5, name: 'Bronze sword', kind: ItemKind.Weapon, equipSlot: EquipSlot.Weapon, levelToEquip: 1,
      accuracy: 4, strength: 3, defence: -1, attackSpeed: 4, heal: 0, shopBuysFor: 10, shopSells: true, shopSellsFor: 25, toolSkill: 0, toolLevel: 0, cookingLevel: 0,
    });
    expect(game.items.item(9)?.name).toBe('Fish');
    expect(game.items.item(9)?.heal).toBe(3);
    expect(game.items.item(9)?.shopSells).toBe(false);
    expect(game.items.item(42)).toBeUndefined();
  });
});

describe('decode + GameState: entity info and health', () => {
  it('keeps info per entity on arrival and update', () => {
    const game = setup();
    feed(game, snapshot([1, 2, 3]), info(2, EntityKind.Player, 0, 5, 5, 0, 7, 0), info(3, EntityKind.Npc, 4, 12));
    expect(game.items.entityInfo(2)).toEqual({
      id: 2, kind: EntityKind.Player, npcType: 0, combatLevel: 5, totalLevel: 0, equipment: { weapon: 5, head: 0, body: 7, shield: 0 },
    });
    expect(game.items.entityInfo(3)).toMatchObject({ kind: EntityKind.Npc, npcType: 4, combatLevel: 12 });
    feed(game, info(2, EntityKind.Player, 0, 6, 0, 8, 7, 0));
    expect(game.items.entityInfo(2)?.combatLevel).toBe(6);
    expect(game.items.entityInfo(2)?.equipment).toEqual({ weapon: 0, head: 8, body: 7, shield: 0 });
  });

  it('defaults health to full and keeps the last ratio', () => {
    const game = setup();
    feed(game, snapshot([1, 2, 3]));
    expect(game.items.healthRatio(2)).toBe(1);
    feed(game, health([[2, 0], [3, 255]]));
    expect(game.items.healthRatio(2)).toBe(0);
    expect(game.items.healthRatio(3)).toBe(1);
    feed(game, health([[2, 51]]));
    expect(game.items.healthRatio(2)).toBeCloseTo(0.2);
    expect(game.items.healthRatio(3)).toBe(1);
  });

  it('drops info and health when the entity leaves view', () => {
    const game = setup();
    feed(game, snapshot([1, 2, 3]), info(2, EntityKind.Player, 0, 5), info(3, EntityKind.Npc, 4, 12), health([[2, 100], [3, 10]]));
    feed(game, snapshot([1, 3]));
    expect(game.items.entityInfo(2)).toBeUndefined();
    expect(game.items.healthRatio(2)).toBe(1);
    expect(game.items.entityInfo(3)?.npcType).toBe(4);
    expect(game.items.healthRatio(3)).toBeCloseTo(10 / 255);
  });

  it('keeps info that arrives before the entity is in a snapshot', () => {
    const game = setup();
    feed(game, snapshot([1]), info(4, EntityKind.Npc, 2, 3), snapshot([1]));
    expect(game.items.entityInfo(4)?.npcType).toBe(2);
    feed(game, snapshot([1, 4]), snapshot([1]));
    expect(game.items.entityInfo(4)).toBeUndefined();
  });

  it('forgets per-entity data on a new Welcome', () => {
    const game = setup();
    feed(game, snapshot([1, 2]), info(2, EntityKind.Npc, 1, 1), health([[2, 9]]), welcome(1));
    expect(game.items.entityInfo(2)).toBeUndefined();
    expect(game.items.healthRatio(2)).toBe(1);
  });
});

describe('ItemState notifications', () => {
  it('notifies subscribers only on change', () => {
    const game = setup();
    let count = 0;
    const off = game.items.subscribe(() => count++);

    feed(game, stats(7, 10, 3, 0, 0));
    expect(count).toBe(1);
    feed(game, stats(7, 10, 3, 0, 0));
    expect(count).toBe(1);
    feed(game, stats(6, 10, 3, 0, 0));
    expect(count).toBe(2);

    feed(game, inventory([[5, 1]], 0, 0, 0, 0));
    expect(count).toBe(3);
    feed(game, inventory([[5, 1]], 0, 0, 0, 0));
    expect(count).toBe(3);

    feed(game, catalog());
    expect(count).toBe(4);

    feed(game, snapshot([1, 2]));
    expect(count).toBe(4);
    feed(game, info(2, EntityKind.Player, 0, 5));
    expect(count).toBe(5);
    feed(game, info(2, EntityKind.Player, 0, 5));
    expect(count).toBe(5);

    feed(game, health([[2, 100]]));
    expect(count).toBe(6);
    feed(game, health([[2, 100]]));
    expect(count).toBe(6);

    feed(game, snapshot([1, 2]));
    expect(count).toBe(6);
    feed(game, snapshot([1]));
    expect(count).toBe(7);

    off();
    feed(game, stats(1, 10, 3, 0, 0));
    expect(count).toBe(7);
  });

  it('works on its own without GameState', () => {
    const s = new ItemState();
    let count = 0;
    s.subscribe(() => count++);
    s.setVisible([1]);
    s.setEntityInfo({ id: 1, kind: EntityKind.Npc, npcType: 1, combatLevel: 2, totalLevel: 0, equipment: { weapon: 0, head: 0, body: 0, shield: 0 } });
    s.resetEntities();
    expect(count).toBe(2);
    expect(s.entityInfo(1)).toBeUndefined();
  });
});

function groundItems(items: [number, number, number, number, number][]): Uint8Array {
  const b = new flatbuffers.Builder(128);
  GroundItems.startItemsVector(b, items.length);
  for (let i = items.length - 1; i >= 0; i--) GroundItem.createGroundItem(b, ...items[i]);
  const v = b.endVector();
  return finish(b, Message.GroundItems, GroundItems.createGroundItems(b, v));
}

function npcCatalog(npcs: [number, string, number][]): Uint8Array {
  const b = new flatbuffers.Builder(128);
  const defs = npcs.map(([id, name, level]) => NpcDef.createNpcDef(b, id, b.createString(name), level));
  return finish(b, Message.NpcCatalog, NpcCatalog.createNpcCatalog(b, NpcCatalog.createNpcsVector(b, defs)));
}

function shopResult(code: ShopResultCode): Uint8Array {
  const b = new flatbuffers.Builder(32);
  return finish(b, Message.ShopResult, ShopResult.createShopResult(b, code));
}

function combatHit(tick: number, hits: [number, number, number, boolean][]): Uint8Array {
  const b = new flatbuffers.Builder(64);
  CombatHit.startHitsVector(b, hits.length);
  for (let i = hits.length - 1; i >= 0; i--) Hit.createHit(b, ...hits[i]);
  const v = b.endVector();
  return finish(b, Message.CombatHit, CombatHit.createCombatHit(b, tick, v));
}

describe('decode + GameState: ground items', () => {
  it('replaces the whole list with each message', () => {
    const game = setup();
    expect(game.items.groundItems).toEqual([]);
    feed(game, groundItems([[1, 5, 1, 100, 200], [2, 9, 3, -10, 0]]));
    expect(game.items.groundItems).toEqual([
      { id: 1, itemId: 5, quantity: 1, x: 10, z: 20 },
      { id: 2, itemId: 9, quantity: 3, x: -1, z: 0 },
    ]);
    feed(game, groundItems([[2, 9, 3, -10, 0]]));
    expect(game.items.groundItems.map((g) => g.id)).toEqual([2]);
    feed(game, groundItems([]));
    expect(game.items.groundItems).toEqual([]);
  });

  it('notifies only on change and forgets ground items on a new Welcome', () => {
    const game = setup();
    let count = 0;
    game.items.subscribe(() => count++);
    feed(game, groundItems([[1, 5, 1, 0, 0]]));
    feed(game, groundItems([[1, 5, 1, 0, 0]]));
    expect(count).toBe(1);
    feed(game, welcome(1));
    expect(game.items.groundItems).toEqual([]);
  });
});

describe('decode + GameState: NPC catalog', () => {
  it('looks NPC types up by id and replaces the catalog', () => {
    const game = setup();
    feed(game, npcCatalog([[1, 'Rat', 2], [4, 'Anteater', 15]]));
    expect(game.items.npc(4)).toEqual({ id: 4, name: 'Anteater', combatLevel: 15 });
    expect(game.items.npc(1)?.name).toBe('Rat');
    expect(game.items.npc(2)).toBeUndefined();
    feed(game, npcCatalog([[2, 'Goose', 5]]));
    expect(game.items.npc(1)).toBeUndefined();
    expect(game.items.npc(2)?.combatLevel).toBe(5);
  });
});

describe('decode + GameState: shop result', () => {
  it('keeps the last result and counts repeats as new results', () => {
    const game = setup();
    let count = 0;
    game.items.subscribe(() => count++);
    expect(game.items.shopResult).toBeNull();
    feed(game, shopResult(ShopResultCode.NotEnoughGold));
    expect(game.items.shopResult).toEqual({ code: ShopResultCode.NotEnoughGold, seq: 1 });
    feed(game, shopResult(ShopResultCode.NotEnoughGold));
    expect(game.items.shopResult).toEqual({ code: ShopResultCode.NotEnoughGold, seq: 2 });
    feed(game, shopResult(ShopResultCode.Ok));
    expect(game.items.shopResult?.code).toBe(ShopResultCode.Ok);
    expect(count).toBe(3);
  });
});

describe('decode + GameState: combat hits', () => {
  it('queues each tick of hits until render drains them', () => {
    const game = setup();
    feed(game, combatHit(10, [[1, 3, 4, false]]), combatHit(11, [[3, 1, 0, false], [1, 3, 9, true]]));
    expect(game.hits.length).toBe(2);
    expect(game.hits.drain()).toEqual([
      { tick: 10, hits: [{ attacker: 1, target: 3, damage: 4, died: false }] },
      {
        tick: 11,
        hits: [
          { attacker: 3, target: 1, damage: 0, died: false },
          { attacker: 1, target: 3, damage: 9, died: true },
        ],
      },
    ]);
    expect(game.hits.drain()).toEqual([]);
    feed(game, combatHit(12, [[1, 3, 1, false]]));
    expect(game.hits.drain().map((b) => b.tick)).toEqual([12]);
  });

  it('drops the oldest batches past the cap and clears on a new Welcome', () => {
    const game = setup();
    for (let t = 0; t < MAX_HIT_BATCHES + 5; t++) feed(game, combatHit(t, [[1, 2, 1, false]]));
    const drained = game.hits.drain();
    expect(drained.length).toBe(MAX_HIT_BATCHES);
    expect(drained[0].tick).toBe(5);
    feed(game, combatHit(1, [[1, 2, 1, false]]), welcome(1));
    expect(game.hits.drain()).toEqual([]);
  });
});
