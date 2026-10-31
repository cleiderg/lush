import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { AREA_NAMES, areaMap, placeAt } from './location';

const json = JSON.parse(readFileSync(resolve(__dirname, '../../../shared/map/areas.json'), 'utf8'));
const map = areaMap(json);

describe('placeAt (areas.json)', () => {
  it('reads the safe circle from areas.json in metres', () => {
    expect(map.spawn).toEqual({ x: json.spawn.x / 10, z: json.spawn.z / 10 });
    expect(map.safeRadius).toBe(json.safeRadius / 10);
    expect(map.areas.map((a) => a.id).sort()).toEqual(['aldrich_park', 'pvp_zone', 'south_fields']);
  });

  it('Aldrich Park, Safe inside the safe circle', () => {
    expect(placeAt(map.spawn, map)).toEqual({ name: 'Aldrich Park', type: 'Safe', danger: false });
    const edge = { x: map.spawn.x + map.safeRadius - 1, z: map.spawn.z };
    expect(placeAt(edge, map)).toEqual({ name: 'Aldrich Park', type: 'Safe', danger: false });
  });

  it('Aldrich Park, PvE elsewhere in the park', () => {
    const p = { x: map.spawn.x, z: map.spawn.z - map.safeRadius - 30 };
    expect(placeAt(p, map)).toEqual({ name: 'Aldrich Park', type: 'PvE', danger: false });
  });

  it('South fields, PvE', () => {
    expect(placeAt({ x: -280, z: 280 }, map)).toEqual({ name: 'South fields', type: 'PvE', danger: false });
  });

  it('PvP zone, PvP, with danger', () => {
    expect(placeAt({ x: 300, z: -350 }, map)).toEqual({ name: 'PvP zone', type: 'PvP', danger: true });
  });

  it('no place outside every area', () => {
    expect(placeAt({ x: 0, z: 1000 }, map)).toBeNull();
    expect(placeAt({ x: -600, z: -600 }, map)).toBeNull();
  });

  it('every area id in areas.json has a display name', () => {
    for (const a of json.areas) expect(AREA_NAMES[a.id], a.id).toBeTruthy();
  });
});
