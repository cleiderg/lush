import type { Point } from './shop';

/**
 * How far outside the PvP border (metres) the client shows the warning banner
 * (docs/specs/map.md, PvP zone; decided in #181).
 */
export const PVP_WARNING_BAND_METRES = 10;

/** A polygon as [x, z] points in metres. */
export type Polygon = [number, number][];

export interface PvpStatus {
  /** Inside a PvP zone polygon. */
  inside: boolean;
  /** Outside every PvP zone but within PVP_WARNING_BAND_METRES of one's border. */
  warning: boolean;
}

/** Polygon [x0,z0,...] in decimetres (areas.json) -> [x,z] points in metres. */
export function polygonFromDecimetres(p: number[]): Polygon {
  const out: Polygon = [];
  for (let i = 0; i + 1 < p.length; i += 2) out.push([p[i] / 10, p[i + 1] / 10]);
  return out;
}

/** Even-odd ray cast: whether pos is inside poly. */
export function pointInPolygon(pos: Point, poly: Polygon): boolean {
  let inside = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [xi, zi] = poly[i], [xj, zj] = poly[j];
    if (zi > pos.z !== zj > pos.z && pos.x < ((xj - xi) * (pos.z - zi)) / (zj - zi) + xi) inside = !inside;
  }
  return inside;
}

/** Shortest distance from pos to the polygon's outline. */
export function distanceToOutline(pos: Point, poly: Polygon): number {
  let best = Infinity;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const [ax, az] = poly[j], [bx, bz] = poly[i];
    const dx = bx - ax, dz = bz - az;
    const len2 = dx * dx + dz * dz;
    const t = len2 === 0 ? 0 : Math.max(0, Math.min(1, ((pos.x - ax) * dx + (pos.z - az) * dz) / len2));
    best = Math.min(best, Math.hypot(pos.x - (ax + t * dx), pos.z - (az + t * dz)));
  }
  return best;
}

/** Whether pos (metres) is inside any of the PvP zones, or close enough outside one to warn. */
export function pvpStatus(pos: Point, zones: Polygon[], band = PVP_WARNING_BAND_METRES): PvpStatus {
  let near = false;
  for (const z of zones) {
    if (pointInPolygon(pos, z)) return { inside: true, warning: false };
    if (distanceToOutline(pos, z) <= band) near = true;
  }
  return { inside: false, warning: near };
}
