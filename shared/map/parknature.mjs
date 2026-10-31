// Aldrich Park nature placement (docs/specs/visual-style.md, Ground and nature; Props, Aldrich Park). Metres, x = east, z = south.
// Placeholders to adjust after playtests. build.mjs places the nature with a seeded generator and writes parknature.json.
// Visual only: nothing here changes collision.bin (`node shared/map/build.mjs [outDir] --no-nature` skips it).

/** Seed for nature placement. Change it to reshuffle the park. */
export const NATURE_SEED = 401;

/**
 * Nature types in parknature.json order (the index is the type id). `r` is the footprint radius in metres,
 * kept clear of paths, props and blocked cells; `space` is the radius kept clear of other nature
 * (a tree's canopy). `scale` is the seeded uniform scale range, `stretch` the extra seeded height range.
 */
export const NATURE_TYPES = [
  { name: 'tree_oak', r: 0.6, space: 2.6, scale: [0.75, 1.35], stretch: [0.9, 1.15] },
  { name: 'tree_pine', r: 0.5, space: 1.9, scale: [0.7, 1.4], stretch: [0.85, 1.25] },
  { name: 'tree_eucalyptus', r: 0.5, space: 2.2, scale: [0.75, 1.3], stretch: [0.9, 1.3] },
  { name: 'rock_boulder', r: 0.7, space: 0.9, scale: [0.5, 1.4], stretch: [0.7, 1.2] },
  { name: 'rock_flat', r: 0.9, space: 1.0, scale: [0.6, 1.3], stretch: [0.7, 1.3] },
  { name: 'bush', r: 0.7, space: 0.9, scale: [0.6, 1.3], stretch: [0.8, 1.2] },
  { name: 'flower_bed', r: 1.3, space: 1.4, scale: [0.8, 1.2], stretch: [1, 1] },
  { name: 'picnic_blanket', r: 1.3, space: 1.5, scale: [0.9, 1.1], stretch: [1, 1] },
  { name: 'bird', r: 0.2, space: 0.3, scale: [0.8, 1.2], stretch: [1, 1] },
];

/** Edge of a park coverage block in metres, aligned to multiples of it from the world origin. */
export const NATURE_BLOCK = 20;
/** Every block at least this share walkable park grass gets at least one tree, rock or bush. */
export const NATURE_COVER_SHARE = 0.5;
/** Trees per block of full grass (fractional part is a chance), and the same for the rest. */
export const TREES_PER_BLOCK = 1.6;
export const BUSHES_PER_BLOCK = 1.2;
export const ROCKS_PER_BLOCK = 0.5;
export const FLOWER_BEDS_PER_BLOCK = 0.2;
/** Picnic blankets and birds, placed on open park grass between these distances (metres) from the spawn. */
export const PICNIC_BLANKETS = 4;
export const PICNIC_RING = [12, 70];
export const BIRDS = 8;
export const BIRD_RING = [8, 90];

/** Nature stays at least this far (metres, plus its footprint) from the spawn, shop stall, bank booth and grill. */
export const NATURE_CLEAR = 3;
/** Bank booth and grill (M9) from docs/specs/map.md, Gameplay areas. Kept clear now so they fit later. */
export const BANK_BOOTH = { x: -12, z: -96 };
export const GRILL = { x: -32, z: -96 };
