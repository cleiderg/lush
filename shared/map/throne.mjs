// M12 throne room inside Aldrich Hall (docs/specs/groups.md: The weekly siege and the throne). Metres, x = east, z = south.
// build.mjs fits the layout into the building and writes throne.json.

import { inPoly, ORIENTATIONS, rectCells } from './halls.mjs';

/** OSM way of Aldrich Hall (docs/specs/map.md: Landmarks). */
export const ALDRICH_HALL_WAY = 62646030;

/** Longest extra passage (rows) from the wall row to open ground. */
export const MAX_PASSAGE = 8;

/**
 * The interior in local whole-metre cells (u east, v towards the entrance). Rectangles are [u0, v0, u1, v1).
 * Wall row v=12 holds the two gates; wall row v=17 holds the outer opening. Every other cell of BLOCK is a wall.
 */
export const THRONE_LAYOUT = {
  block: [-1, -1, 21, 18],
  throneRoom: [0, 0, 20, 12],
  entrance: [0, 13, 20, 17],
  /** One wall row thick; the build lengthens it through the rest of the footprint (up to MAX_PASSAGE rows) to the street. */
  outerOpening: [8, 17, 12, 18],
  mainGate: [8, 12, 12, 13],
  sideGate: [1, 12, 3, 13],
  dais: [8, 1, 12, 4],
};

/**
 * Find where the throne layout fits in Aldrich Hall. `foot` is the footprint (flat, decimetres), `blocked(x, z)` the
 * collision test on a cell centre in metres. All BLOCK cells must be inside the footprint and blocked, and the cells in
 * front of the outer opening must be walkable and outside it. The placement whose opening is nearest `spawn` wins.
 * Returns {passage, lattice(u, v) -> [x, z], cell(u, v) -> [col, row]} or null; `passage` lists the local cells [u, v] of the opening, from the wall row out to the street.
 */
export function fitThrone(foot, blocked, spawn) {
  const L = THRONE_LAYOUT;
  const xs = foot.filter((_, i) => i % 2 === 0), zs = foot.filter((_, i) => i % 2 === 1);
  const x0 = Math.floor(Math.min(...xs) / 10) - 24, x1 = Math.ceil(Math.max(...xs) / 10) + 24;
  const z0 = Math.floor(Math.min(...zs) / 10) - 24, z1 = Math.ceil(Math.max(...zs) / 10) + 24;
  const inFoot = (x, z) => inPoly(foot, x * 10, z * 10);
  const [b0, b1, b2, b3] = L.block;
  const block = [[b0, b1], [b2 - 1, b1], [b0, b3 - 1], [b2 - 1, b3 - 1], ...rectCells(L.block)];
  const [o0, o1, o2] = L.outerOpening;
  let best = null;
  for (const f of ORIENTATIONS) {
    for (let oz = z0; oz <= z1; oz++) {
      for (let ox = x0; ox <= x1; ox++) {
        const lattice = (u, v) => { const [a, b] = f(u, v); return [ox + a, oz + b]; };
        const cell = (u, v) => { const [a, b] = f(u + 0.5, v + 0.5); return [Math.floor(ox + a), Math.floor(oz + b)]; };
        if (!block.every(([u, v]) => { const [c, r] = cell(u, v); return inFoot(c + 0.5, r + 0.5) && blocked(c + 0.5, r + 0.5); })) continue;
        // Each column of the opening runs out through the wall row and on through the footprint (a short passage of blocked
        // cells) to the first cell outside it, which must be walkable.
        const front = [], passage = [];
        let ok = true;
        for (let u = o0; u < o2 && ok; u++) {
          let k = 0;
          for (; k <= MAX_PASSAGE; k++) {
            const [c, r] = cell(u, o1 + k);
            if (!inFoot(c + 0.5, r + 0.5)) break;
            if (!blocked(c + 0.5, r + 0.5)) { k = MAX_PASSAGE + 1; break; }
          }
          if (k > MAX_PASSAGE) { ok = false; break; }
          const [c, r] = cell(u, o1 + k);
          if (blocked(c + 0.5, r + 0.5)) { ok = false; break; }
          front.push([c, r]);
          for (let j = 0; j < k; j++) passage.push([u, o1 + j]);
        }
        if (!ok) continue;
        const d = Math.min(...front.map(([c, r]) => Math.hypot(c + 0.5 - spawn.x, r + 0.5 - spawn.z)));
        if (!best || d < best.d) best = { d, passage, lattice, cell };
      }
    }
  }
  return best;
}
