// Milestone 8 fountain (docs/specs/map.md, Gameplay areas: Fountain; answer to #422). Metres, x = east, z = south.
// Placeholders to adjust after playtests. build.mjs places it and writes fountain.json; its basin is blocked in collision.bin.

/** Basin radius in metres: one round fountain about 4 m across. */
export const FOUNTAIN_R = 2;
/** Open paving (metres) kept between the basin and the plaza's edge, every path and every building. */
export const FOUNTAIN_PATH_CLEAR = 1.5;
/** The basin stays at least this far (metres) from the spawn, shop stall, card table plaza, bank booth and grill. */
export const FOUNTAIN_CLEAR = 10;
/** Candidate centres are searched on a grid of this step (metres) inside the plaza. */
export const FOUNTAIN_STEP = 0.5;
