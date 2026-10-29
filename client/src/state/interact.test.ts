import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { EntityKind } from '../protocol';
import { CARD_TABLE_POSITIONS, CARD_TABLE_REACH_TILES, inCardTableReach } from './cardTables';
import { TILE_METRES } from './shop';
import {
  ATTACK_RANGE_TILES, inPickUpRange, inPolygon, inPvpZone, nearestCardTable, nearestPickUp, pickInteractTarget, PICKUP_RANGE_TILES, PVP_ZONES,
  TRADE_DISTANCE_TILES, type InteractEntity, type InteractGroundItem, type Polygon,
} from './interact';

const repo = resolve(__dirname, '../../..');
const rules = JSON.parse(readFileSync(resolve(repo, 'server/internal/systems/gamedata/data/rules.json'), 'utf8'));
const areas = JSON.parse(readFileSync(resolve(repo, 'shared/map/areas.json'), 'utf8'));
const cardTablesFile = JSON.parse(readFileSync(resolve(repo, 'shared/map/cardtables.json'), 'utf8'));
const cardroomPositions = readFileSync(resolve(repo, 'server/internal/systems/cardroom/positions.go'), 'utf8');
/** A numeric Go constant from server/internal/systems/cardroom/positions.go. */
function goConst(name: string): number {
  const m = new RegExp(`^const ${name} = ([0-9.]+)$`, 'm').exec(cardroomPositions);
  if (!m) throw new Error(`const ${name} not found in positions.go`);
  return Number(m[1]);
}

const STALL = { x: 0, z: 0 };
const FAR = { x: 100.5, z: 100.5 };
const npc = (id: number, x: number, z: number): InteractEntity => ({ id, x, z, kind: EntityKind.Npc });
const player = (id: number, x: number, z: number): InteractEntity => ({ id, x, z, kind: EntityKind.Player });
const ground = (id: number, x: number, z: number): InteractGroundItem => ({ id, x, z });
const SQUARE: Polygon = [[90, 90], [110, 90], [110, 110], [90, 110]];

describe('interact distances match the server rules (AC1)', () => {
  it('ATTACK_RANGE_TILES equals attack_range_tiles', () => {
    expect(ATTACK_RANGE_TILES).toBe(rules.attack_range_tiles);
    // The picker counts diagonals; the server must too.
    expect(rules.attack_range_diagonal).toBe(true);
  });

  it('PICKUP_RANGE_TILES equals pickup_range_tiles, diagonals included', () => {
    expect(PICKUP_RANGE_TILES).toBe(rules.pickup_range_tiles);
    expect(rules.pickup_range_diagonal).toBe(true);
  });

  it('TRADE_DISTANCE_TILES equals trade_distance_tiles', () => {
    expect(TRADE_DISTANCE_TILES).toBe(rules.trade_distance_tiles);
  });

  it('PVP_ZONES are the areas.json pvp polygons in metres', () => {
    const pvp = areas.areas.filter((a: { kind: string }) => a.kind === 'pvp');
    expect(PVP_ZONES.length).toBe(pvp.length);
    expect(PVP_ZONES[0][0]).toEqual([pvp[0].p[0] / 10, pvp[0].p[1] / 10]);
  });
});

describe('pickInteractTarget (AC1)', () => {
  it('picks the shop near the stall, before an NPC or player in range', () => {
    const pos = { x: 0.5, z: 0.5 };
    expect(pickInteractTarget({ pos, stall: STALL, entities: [npc(5, 1, 1), player(6, 1, 1)] })).toEqual({ type: 'shop' });
  });

  it('picks the nearest NPC within attack range (diagonals count) before a player', () => {
    const entities = [player(2, 101, 100.5), npc(3, 101.5, 101.5), npc(4, 100.9, 100.5), npc(9, 102.5, 100.5)];
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities })).toEqual({ type: 'attack', id: 4 });
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: [npc(3, 101.5, 101.5)] })).toEqual({ type: 'attack', id: 3 });
  });

  it('ignores NPCs beyond attack range', () => {
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: [npc(9, 102.5, 100.5)] })).toBeNull();
  });

  it('sends a trade request to the nearest player within trade distance outside the PvP zone', () => {
    const entities = [player(7, 103, 100.5), player(8, 102, 100.5), player(10, 104, 100.5), npc(9, 103.5, 100.5)];
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities, pvpZones: [] })).toEqual({ type: 'trade', id: 8 });
  });

  it('attacks the nearest player inside the PvP zone', () => {
    const entities = [player(8, 102, 100.5)];
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities, pvpZones: [SQUARE] })).toEqual({ type: 'attack', id: 8 });
  });

  it('returns null with no target, players beyond trade distance, or entities without EntityInfo', () => {
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: [] })).toBeNull();
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: [player(8, 104, 100.5)] })).toBeNull();
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: [{ id: 3, x: 100.5, z: 100.5, kind: undefined }] })).toBeNull();
  });
});

describe('pick-up range follows the server tile rule (#243 AC2)', () => {
  // FAR is in tile (100, 100); range is by tile, not by metres.
  it('an item on the same tile is in range', () => {
    expect(inPickUpRange(FAR, { x: 100.0, z: 100.99 })).toBe(true);
  });

  it('an item on any adjacent tile, diagonals included, is in range', () => {
    for (const dx of [-1, 0, 1]) {
      for (const dz of [-1, 0, 1]) expect(inPickUpRange(FAR, { x: 100.5 + dx, z: 100.5 + dz })).toBe(true);
    }
    // Far corner of the diagonal tile: almost 2 m away in a straight line, still one tile.
    expect(inPickUpRange({ x: 100.01, z: 100.01 }, { x: 101.99, z: 101.99 })).toBe(true);
  });

  it('an item two tiles away on either axis is not in range', () => {
    expect(inPickUpRange(FAR, { x: 102.5, z: 100.5 })).toBe(false);
    expect(inPickUpRange(FAR, { x: 98.5, z: 100.5 })).toBe(false);
    expect(inPickUpRange(FAR, { x: 100.5, z: 102.5 })).toBe(false);
    expect(inPickUpRange(FAR, { x: 100.5, z: 98.5 })).toBe(false);
    expect(inPickUpRange(FAR, { x: 101.5, z: 102.5 })).toBe(false);
    // Only 1.02 m apart in a straight line, but two tiles over.
    expect(inPickUpRange({ x: 100.99, z: 100.5 }, { x: 102.01, z: 100.5 })).toBe(false);
  });

  it('nearestPickUp picks the nearest item in range (ties to the lower id) and null when none is', () => {
    expect(nearestPickUp(FAR, [ground(4, 101.5, 101.5), ground(3, 100.9, 100.5), ground(2, 102.5, 100.5)])?.id).toBe(3);
    expect(nearestPickUp(FAR, [ground(8, 101.5, 100.5), ground(7, 99.5, 100.5)])?.id).toBe(7);
    expect(nearestPickUp(FAR, [ground(2, 102.5, 100.5), ground(5, 100.5, 98.5)])).toBeNull();
    expect(nearestPickUp(FAR, [])).toBeNull();
  });
});

describe('the full Interact order from the gameplay spec (#243 AC3)', () => {
  // Shop, then ground item (pick up), then NPC (attack), then player (trade, or attack in the PvP zone), then nothing.
  const items = [ground(20, 101.5, 100.5)];
  const npcs = [npc(30, 101.5, 100.5)];
  const players = [player(40, 102, 100.5)];

  it('1. the shop wins over a ground item, NPC and player in range', () => {
    const pos = { x: 0.5, z: 0.5 };
    const groundItems = [ground(20, 0.5, 0.5)];
    expect(pickInteractTarget({ pos, stall: STALL, entities: [npc(30, 1, 1), player(40, 1, 1)], groundItems }))
      .toEqual({ type: 'shop' });
  });

  it('2. a ground item in pick-up range wins over an NPC and a player', () => {
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: [...npcs, ...players], groundItems: items, pvpZones: [] }))
      .toEqual({ type: 'pickUp', id: 20 });
  });

  it('3. an NPC in attack range when no ground item is in pick-up range', () => {
    const groundItems = [ground(21, 102.5, 100.5)];
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: [...npcs, ...players], groundItems, pvpZones: [] }))
      .toEqual({ type: 'attack', id: 30 });
  });

  it('4. a player in trade distance when there is no item or NPC: trade outside the PvP zone, attack inside', () => {
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: players, groundItems: [], pvpZones: [] }))
      .toEqual({ type: 'trade', id: 40 });
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: players, groundItems: [], pvpZones: [SQUARE] }))
      .toEqual({ type: 'attack', id: 40 });
  });

  it('5. nothing with no target', () => {
    expect(pickInteractTarget({ pos: FAR, stall: STALL, entities: [], groundItems: [] })).toBeNull();
  });
});

describe('card table reach matches the server sit reach (#325 AC2)', () => {
  it('CARD_TABLE_REACH_TILES equals cardroom ReachTiles, with tiles of the same size', () => {
    expect(CARD_TABLE_REACH_TILES).toBe(goConst('ReachTiles'));
    expect(TILE_METRES).toBe(goConst('TileSize'));
  });

  it('the server checks straight-line distance from the centre, as inCardTableReach does', () => {
    expect(cardroomPositions).toContain('return dx*dx+dy*dy <= Reach*Reach');
  });

  it('CARD_TABLE_POSITIONS are the cardtables.json tables in metres', () => {
    expect(cardTablesFile.units).toBe('decimetres');
    expect(CARD_TABLE_POSITIONS).toEqual(
      cardTablesFile.tables.map((t: { id: number; x: number; z: number }) => ({ id: t.id, x: t.x / 10, z: t.z / 10 })),
    );
    expect(CARD_TABLE_POSITIONS.length).toBeGreaterThan(0);
  });
});

describe('card tables in the Interact order (#325 AC1)', () => {
  const T = CARD_TABLE_POSITIONS[0];
  const reach = CARD_TABLE_REACH_TILES * TILE_METRES;
  const far = { x: 500.5, z: 500.5 };

  it('picks a table from cardtables.json within reach', () => {
    const pos = { x: T.x + reach, z: T.z };
    expect(pickInteractTarget({ pos, stall: far, entities: [] })).toEqual({ type: 'cardTable', id: T.id });
    const diag = { x: T.x + 1.4, z: T.z + 1.4 };
    expect(inCardTableReach(diag, T)).toBe(true);
    expect(pickInteractTarget({ pos: diag, stall: far, entities: [] })).toEqual({ type: 'cardTable', id: T.id });
  });

  it('does not pick a table out of reach, including two tiles on both axes', () => {
    for (const pos of [{ x: T.x + reach + 0.01, z: T.z }, { x: T.x + reach, z: T.z + reach }]) {
      expect(inCardTableReach(pos, T)).toBe(false);
      expect(nearestCardTable(pos, [T])).toBeNull();
      expect(pickInteractTarget({ pos, stall: far, entities: [], cardTables: [T] })).toBeNull();
    }
  });

  it('picks the nearest table in reach, ties to the lower id', () => {
    const tables = [{ id: 7, x: 10, z: 0 }, { id: 3, x: 12, z: 0 }, { id: 2, x: 12, z: 0 }];
    expect(nearestCardTable({ x: 10.5, z: 0 }, tables)?.id).toBe(7);
    expect(nearestCardTable({ x: 12, z: 0.5 }, tables)?.id).toBe(2);
  });

  it('the shop wins over a table; a table wins over a ground item, NPC and player', () => {
    const pos = { x: T.x + 1, z: T.z };
    const entities = [npc(5, pos.x, pos.z), player(6, pos.x, pos.z)];
    const groundItems = [ground(10, pos.x, pos.z)];
    expect(pickInteractTarget({ pos, stall: pos, entities, groundItems })).toEqual({ type: 'shop' });
    expect(pickInteractTarget({ pos, stall: far, entities, groundItems })).toEqual({ type: 'cardTable', id: T.id });
  });

  it('a ground item is picked once the table is out of reach', () => {
    const tables = [{ id: 1, x: 0, z: 0 }];
    const groundItems = [ground(10, 3.5, 0.5)];
    expect(pickInteractTarget({ pos: { x: 2.5, z: 0.5 }, stall: far, entities: [], groundItems, cardTables: tables }))
      .toEqual({ type: 'pickUp', id: 10 });
  });
});

describe('PvP zone test', () => {
  it('inPolygon is an even-odd test', () => {
    expect(inPolygon({ x: 100, z: 100 }, SQUARE)).toBe(true);
    expect(inPolygon({ x: 120, z: 100 }, SQUARE)).toBe(false);
  });

  it('the areas.json PvP zone contains its own interior and not the spawn', () => {
    expect(inPvpZone({ x: 300, z: -350 })).toBe(true);
    expect(inPvpZone({ x: areas.spawn.x / 10, z: areas.spawn.z / 10 })).toBe(false);
  });
});
