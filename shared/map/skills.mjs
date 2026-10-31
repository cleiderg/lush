// Milestone 9 skill objects (docs/specs/map.md, Gameplay areas; docs/specs/skills.md). Metres, x = east, z = south.
// Placeholders to adjust after playtests. build.mjs places them with a seeded generator and writes skills.json.
// Not in collision.bin yet: pond collision comes later (#399), so the pond outline is data only for now.

/** Seed for tree, pond and fishing spot placement. Change it to reshuffle them. */
export const SKILLS_SEED = 444;

/** Woodcutting trees per kind, in skills.json order. Ids are `<kind>-<n>`, n from 1. */
export const SKILL_TREES = [
  { kind: 'tree', count: 12 },
  { kind: 'oak', count: 6 },
  // Eucalyptus only on the park's far (east) side.
  { kind: 'eucalyptus', count: 4, minX: 40 },
];
/** A skill tree's footprint radius in metres, kept clear of paths, props and decorative nature. */
export const SKILL_TREE_R = 0.8;
/** Skill trees stay at least this far (metres) from each other, so they spread through the park. */
export const SKILL_TREE_SPACING = 14;
/** Skill trees and the pond stay at least this far (metres) outside the safe circle's edge. */
export const SAFE_MARGIN = 3;

/** Hand-made pond: centre searched outward from here, an outline of POND_POINTS points between these radii. */
export const POND_CENTRE = { x: -90, z: -20 };
export const POND_RADII = { x: 7, z: 5 };
export const POND_POINTS = 14;
/** Seeded wobble of each outline point's radius, as a share of it. */
export const POND_WOBBLE = 0.15;
/** Open grass (metres) kept around the pond: no path, prop, skill tree or decorative nature, so players can reach the edge. */
export const POND_SHORE = 2;
/** How far (metres) the pond may move from POND_CENTRE to fit the grass. */
export const POND_SHIFT = 30;

/** Fishing spots active at once per kind (docs/specs/skills.md), and candidate edge positions per kind they move between. */
export const FISHING_SPOTS = [
  { kind: 'net', active: 2, candidates: 4 },
  { kind: 'rod', active: 2, candidates: 4 },
];

/** Props keep this far (metres, plus their footprint) from the bank booth and grill, so the stations stand on open ground. */
export const STATION_CLEAR = 1.5;
