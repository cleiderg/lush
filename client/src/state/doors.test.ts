import { describe, expect, it } from 'vitest';
import { DOOR_REACH_TILES, HOUSE_DOORS, nearestDoor } from './doors';
import { pickInteractTarget } from './interact';
import { TILE_METRES } from './shop';

const FAR_STALL = { x: 1e6, z: 1e6 };
const pick = (pos: { x: number; z: number }) => pickInteractTarget({ pos, stall: FAR_STALL, bank: FAR_STALL, entities: [] });

describe('house doors (AC1)', () => {
  it('has the housing.json doors with id j+1', () => {
    expect(HOUSE_DOORS).toHaveLength(40);
    expect(HOUSE_DOORS[0].id).toBe(1);
    expect(HOUSE_DOORS[1].number).toBe(2);
  });

  it('picks the door within 3 tiles in a straight line and none beyond', () => {
    const d = HOUSE_DOORS[4];
    const reach = DOOR_REACH_TILES * TILE_METRES;
    expect(DOOR_REACH_TILES).toBe(3);
    expect(pick({ x: d.x + reach - 0.01, z: d.z })).toEqual({ type: 'door', id: 5 });
    expect(pick({ x: d.x, z: d.z + reach + 0.01 })).toBeNull();
    // Diagonal corner of a 3-tile square is beyond a straight-line reach of 3.
    expect(nearestDoor({ x: d.x + reach, z: d.z + reach })).toBeNull();
  });

  it('prefers the nearest of two doors', () => {
    const doors = [
      { id: 1, x: 0, z: 0, areaName: 'A', number: 1 },
      { id: 2, x: 2, z: 0, areaName: 'A', number: 2 },
    ];
    expect(nearestDoor({ x: 1.5, z: 0 }, doors)?.id).toBe(2);
  });
});
