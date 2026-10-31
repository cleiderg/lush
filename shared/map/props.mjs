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
  // Food and fun (#428): placed by their own pass after the busy areas, never by the busy-area shuffle.
  { name: 'food_cart', r: 1.3, square: true, food: true },
  { name: 'string_lights', r: 0.3, food: true },
  { name: 'coffee_cups', r: 0.15, food: true, top: true },
  { name: 'plates', r: 0.2, food: true, top: true },
  { name: 'pizza_box', r: 0.25, food: true, top: true },
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

// ---------- Food and fun (#428) ----------

/** Seed for the food-and-fun pass, so it doesn't reshuffle the props above. */
export const FOOD_SEED = 428;
/** Food carts per busy area, in the order of FOOD_AREAS. */
export const FOOD_CARTS = 1;
/** String lights per busy area. Each is two poles STRING_SPAN metres apart with bulbs hung between them. */
export const STRING_LIGHTS = 1;
/** Distance between a string light's two poles, metres; the prop's position is the midpoint. */
export const STRING_SPAN = 6;
/** Busy areas that get carts and string lights: the shop and the card table plaza (names from build.mjs). */
export const FOOD_AREAS = ['shop', 'card plaza'];
/** Food props stay at least this far (metres, plus their footprint) from the spawn, shop stall, bank booth and grill. */
export const FOOD_CLEAR = 3;
/** Food props stay at least this far (metres, plus their footprint) from every building wall, so never on a door cell. */
export const WALL_CLEAR = 1.5;
/** Height (metres) of a picnic table top and of a food cart's serving ledge: where cups, plates and pizza boxes sit. */
export const TABLETOP_Y = 0.78;
/**
 * Where table-top items may sit, in the host's own frame (metres, before rotation): two spots on each picnic
 * table and two on each cart's serving ledge (front, +z).
 */
export const TABLETOP_SPOTS = {
  picnic_table: [[-0.45, 0], [0.45, 0]],
  food_cart: [[-0.5, 0.75], [0.4, 0.75]],
};
/** Picnic tables nearer than this (metres) to a busy-area centre get table-top items. */
export const TABLETOP_REACH = 25;
