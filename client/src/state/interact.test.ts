import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { EntityKind } from '../protocol';
import {
  ATTACK_RANGE_TILES, inPolygon, inPvpZone, pickInteractTarget, PVP_ZONES, TRADE_DISTANCE_TILES,
  type InteractEntity, type Polygon,
} from './interact';

const repo = resolve(__dirname, '../../..');
const rules = JSON.parse(readFileSync(resolve(repo, 'server/internal/systems/gamedata/data/rules.json'), 'utf8'));
const areas = JSON.parse(readFileSync(resolve(repo, 'shared/map/areas.json'), 'utf8'));

const STALL = { x: 0, z: 0 };
const FAR = { x: 100.5, z: 100.5 };
const npc = (id: number, x: number, z: number): InteractEntity => ({ id, x, z, kind: EntityKind.Npc });
const player = (id: number, x: number, z: number): InteractEntity => ({ id, x, z, kind: EntityKind.Player });
const SQUARE: Polygon = [[90, 90], [110, 90], [110, 110], [90, 110]];

describe('interact distances match the server rules (AC1)', () => {
  it('ATTACK_RANGE_TILES equals attack_range_tiles', () => {
    expect(ATTACK_RANGE_TILES).toBe(rules.attack_range_tiles);
    // The picker counts diagonals; the server must too.
    expect(rules.attack_range_diagonal).toBe(true);
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
