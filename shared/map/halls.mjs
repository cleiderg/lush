// M12 group halls (docs/specs/groups.md: Group halls). Metres, x = east, z = south.
// build.mjs picks a spot for the interior inside each building listed in HALL_WAYS and writes halls.json.

import { ALDRICH_PARK_WAY } from './areas.mjs';

/** The four landmark building ways (docs/specs/map.md: Landmarks); never halls. */
export const LANDMARK_WAYS = [171169252, 1374185239, 62646030, 157979894];

/** OSM building ways of the six halls, in id order (hall-1 first): non-landmark ring buildings with a clear line to the spawn that the interior fits in. */
export const HALL_WAYS = [157979881, 157979892, 631379319, 157979895, 157979887, 157979878];

/**
 * The interior in local whole-metre cells (u east, v towards the entrance), every hall the same.
 * Rooms are rectangles [u0, v0, u1, v1) of walkable floor; every other cell of BLOCK is a wall.
 */
export const LAYOUT = {
  block: [-1, -1, 9, 13],
  main: [0, 4, 8, 9],
  vaultRoom: [2, 0, 6, 3],
  /** Entrance hall: two rows, plus the archway cells through the wall row to the main room. */
  entrance: [0, 10, 8, 12],
  archway: [3, 9, 5, 10],
  innerDoor: [3, 3, 5, 4],
  outerDoors: [[2, 12, 3, 13], [5, 12, 6, 13]],
  vault: [3.5, 1.5],
};

export const rectCells = ([u0, v0, u1, v1]) => {
  const out = [];
  for (let v = v0; v < v1; v++) for (let u = u0; u < u1; u++) out.push([u, v]);
  return out;
};

/** Four ways to turn the local frame into the world: the entrance faces south, north, east or west. */
export const ORIENTATIONS = [
  (u, v) => [u, v],
  (u, v) => [-u, -v],
  (u, v) => [v, u],
  (u, v) => [-v, -u],
];

/** Point in polygon; `p` flat [x0, z0, ...] in the same units as the point. */
export function inPoly(p, x, z) {
  let r = false;
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    if (p[i + 1] > z !== p[j + 1] > z && x < p[i] + ((z - p[i + 1]) * (p[j] - p[i])) / (p[j + 1] - p[i + 1])) r = !r;
  }
  return r;
}

/** True when segment a-b crosses or lies inside the closed polygon `p` (flat). */
export function segmentHitsPoly(p, ax, az, bx, bz) {
  if (inPoly(p, ax, az) || inPoly(p, bx, bz)) return true;
  const cross = (ox, oz, px, pz, qx, qz) => (px - ox) * (qz - oz) - (pz - oz) * (qx - ox);
  for (let i = 0, j = p.length - 2; i < p.length; j = i, i += 2) {
    const cx = p[j], cz = p[j + 1], dx = p[i], dz = p[i + 1];
    const d1 = cross(ax, az, bx, bz, cx, cz), d2 = cross(ax, az, bx, bz, dx, dz);
    const d3 = cross(cx, cz, dx, dz, ax, az), d4 = cross(cx, cz, dx, dz, bx, bz);
    if (d1 * d2 < 0 && d3 * d4 < 0) return true;
  }
  return false;
}

export const centroid = (p) => {
  let x = 0, z = 0;
  const n = p.length / 2;
  for (let i = 0; i < p.length; i += 2) { x += p[i]; z += p[i + 1]; }
  return { x: x / n, z: z / n };
};

/**
 * Find where the interior fits in a building. `foot` is the footprint (flat, decimetres), `blocked(x, z)` the
 * collision test on a cell centre in metres. All BLOCK cells must be inside the footprint and blocked, and the
 * cells in front of the outer doors must be walkable and outside it. The placement whose doors are nearest `spawn` wins.
 * Returns {map(u, v) -> [x, z] lattice point, cell(u, v) -> [col, row] whole metres, banner} or null.
 */
export function fitHall(foot, blocked, spawn) {
  const xs = foot.filter((_, i) => i % 2 === 0), zs = foot.filter((_, i) => i % 2 === 1);
  const x0 = Math.floor(Math.min(...xs) / 10) - 14, x1 = Math.ceil(Math.max(...xs) / 10) + 14;
  const z0 = Math.floor(Math.min(...zs) / 10) - 14, z1 = Math.ceil(Math.max(...zs) / 10) + 14;
  const fx0 = Math.min(...xs), fx1 = Math.max(...xs), fz0 = Math.min(...zs), fz1 = Math.max(...zs);
  const inFoot = (x, z) => x * 10 >= fx0 && x * 10 <= fx1 && z * 10 >= fz0 && z * 10 <= fz1 && inPoly(foot, x * 10, z * 10);
  const [b0, b1, b2, b3] = LAYOUT.block;
  // Corners first: they reject nearly every placement cheaply.
  const block = [[b0, b1], [b2 - 1, b1], [b0, b3 - 1], [b2 - 1, b3 - 1], ...rectCells(LAYOUT.block)];
  let best = null;
  for (let o = 0; o < ORIENTATIONS.length; o++) {
    const f = ORIENTATIONS[o];
    for (let oz = z0; oz <= z1; oz++) {
      for (let ox = x0; ox <= x1; ox++) {
        const lattice = (u, v) => { const [a, b] = f(u, v); return [ox + a, oz + b]; };
        const cell = (u, v) => { const [a, b] = f(u + 0.5, v + 0.5); return [Math.floor(ox + a), Math.floor(oz + b)]; };
        if (!block.every(([u, v]) => { const [c, r] = cell(u, v); return inFoot(c + 0.5, r + 0.5) && blocked(c + 0.5, r + 0.5); })) continue;
        const front = LAYOUT.outerDoors.map((d) => cell(d[0], d[3]));
        if (!front.every(([c, r]) => !inFoot(c + 0.5, r + 0.5) && !blocked(c + 0.5, r + 0.5))) continue;
        const d = Math.min(...front.map(([c, r]) => Math.hypot(c + 0.5 - spawn.x, r + 0.5 - spawn.z)));
        if (best && best.d <= d) continue;
        // Banner: the nearest open cell outside the footprint in the two rows in front of the wall, off the doors' fronts.
        let banner = null;
        for (let v = 13; v <= 14 && !banner; v++) {
          for (let u = -1; u < 9 && !banner; u++) {
            const [c, r] = cell(u, v);
            if (inFoot(c + 0.5, r + 0.5) || blocked(c + 0.5, r + 0.5)) continue;
            if (front.some(([fc, fr]) => fc === c && fr === r)) continue;
            banner = [c, r];
          }
        }
        if (banner) best = { d, lattice, cell, banner };
      }
    }
  }
  return best;
}
