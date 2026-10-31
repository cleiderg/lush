// Campus prop kit placement (docs/specs/visual-style.md, Props). Metres, x = east, z = south.
// Placeholders to adjust after playtests. build.mjs places props with a seeded generator and writes props.json.

/** Seed for prop placement. Change it to reshuffle every prop. */
export const PROP_SEED = 380;

/**
 * Prop types in props.json order (the index is the type id). `r` is the footprint radius in metres,
 * used to keep props apart; `square` props turn in steps of 90 degrees, the rest turn freely.
 * `shop` types are only placed around the shop stall.
 */
export const PROP_TYPES = [
  { name: 'bench', r: 0.9, square: true },
  { name: 'lamp_post', r: 0.35 },
  { name: 'bin', r: 0.35 },
  { name: 'bike_rack', r: 1.1, square: true },
  { name: 'sign', r: 0.4 },
  { name: 'planter', r: 0.6, square: true },
  { name: 'picnic_table', r: 1.1, square: true },
  { name: 'umbrella', r: 1.1 },
  { name: 'scooter', r: 0.6 },
  { name: 'fence', r: 1.0, square: true },
  { name: 'low_wall', r: 1.0, square: true },
  { name: 'lantern', r: 0.3 },
  { name: 'notice_board', r: 0.8, square: true },
  { name: 'crate', r: 0.5, shop: true },
  { name: 'sack', r: 0.4, shop: true },
];

/** Edge of a busy-area density cell in metres; busy areas get a 3 x 3 block of them around their centre. */
export const BUSY_CELL = 10;
/** Different prop types placed in every busy cell that is at least half walkable (the spec asks for 5). */
export const BUSY_TYPES = 6;
/** Props stay this far from the spawn point. */
export const SPAWN_CLEAR = 2;
/** Clear box around the shop stall, relative to the shop point: the stall and room in front (south) to use it. */
export const SHOP_CLEAR = { x0: -3, z0: -2, x1: 3, z1: 4 };
/** Props stay out of this half-size square around each card table (table and stools). */
export const TABLE_CLEAR = 2;
/** Types placed near the centre of a gameplay area that has no props yet. */
export const AREA_CLUSTER = ['sign', 'bench', 'bin', 'lamp_post'];
