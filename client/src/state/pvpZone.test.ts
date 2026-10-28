import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { PVP_WARNING_BAND_METRES, distanceToOutline, pointInPolygon, polygonFromDecimetres, pvpStatus, type Polygon } from './pvpZone';

const square: Polygon = [[0, 0], [100, 0], [100, 100], [0, 100]];

describe('pvpStatus', () => {
  it('the warning band is 10 m, as docs/specs/map.md says', () => {
    expect(PVP_WARNING_BAND_METRES).toBe(10);
  });

  it('reports inside, warning band and far away', () => {
    expect(pvpStatus({ x: 50, z: 50 }, [square])).toEqual({ inside: true, warning: false });
    expect(pvpStatus({ x: 1, z: 50 }, [square])).toEqual({ inside: true, warning: false });
    expect(pvpStatus({ x: -5, z: 50 }, [square])).toEqual({ inside: false, warning: true });
    expect(pvpStatus({ x: 50, z: 100 + PVP_WARNING_BAND_METRES }, [square])).toEqual({ inside: false, warning: true });
    expect(pvpStatus({ x: 50, z: 100 + PVP_WARNING_BAND_METRES + 0.1 }, [square])).toEqual({ inside: false, warning: false });
    // Diagonal off a corner: 8 m away on each axis is ~11.3 m, outside the band.
    expect(pvpStatus({ x: -8, z: -8 }, [square])).toEqual({ inside: false, warning: false });
    expect(pvpStatus({ x: -6, z: -6 }, [square])).toEqual({ inside: false, warning: true });
    expect(pvpStatus({ x: 500, z: 500 }, [square])).toEqual({ inside: false, warning: false });
    expect(pvpStatus({ x: 0, z: 0 }, [])).toEqual({ inside: false, warning: false });
  });

  it('helpers', () => {
    expect(pointInPolygon({ x: 50, z: 50 }, square)).toBe(true);
    expect(pointInPolygon({ x: 150, z: 50 }, square)).toBe(false);
    expect(distanceToOutline({ x: 50, z: 50 }, square)).toBe(50);
    expect(distanceToOutline({ x: 103, z: 104 }, square)).toBe(5);
    expect(polygonFromDecimetres([10, -20, 30, 40])).toEqual([[1, -2], [3, 4]]);
  });

  it('works on the committed PvP zone, and spawn is far from it', () => {
    const areas = JSON.parse(readFileSync(resolve(__dirname, '../../../shared/map/areas.json'), 'utf8'));
    const zones = areas.areas.filter((a: { kind: string }) => a.kind === 'pvp').map((a: { p: number[] }) => polygonFromDecimetres(a.p));
    expect(zones.length).toBeGreaterThan(0);
    expect(pvpStatus({ x: 300, z: -350 }, zones).inside).toBe(true);
    // The zone's south edge is at z = -280 m between x 220 and 400.
    expect(pvpStatus({ x: 300, z: -275 }, zones)).toEqual({ inside: false, warning: true });
    expect(pvpStatus({ x: 300, z: -260 }, zones)).toEqual({ inside: false, warning: false });
    expect(pvpStatus({ x: areas.spawn.x / 10, z: areas.spawn.z / 10 }, zones)).toEqual({ inside: false, warning: false });
  });
});
