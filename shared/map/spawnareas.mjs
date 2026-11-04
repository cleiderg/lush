// M11 monster spawn polygons that are not in areas.json (its area ids are fixed). Metres, x = east, z = south.
// build.mjs checks them against the collision grid and writes spawnareas.json. Edit here, then rebuild.

/** Geese by the Aldrich Park pond: a strip of open grass along the pond's south shore, clear of the pond itself. */
export const GOOSE_POND = [[-102, -14], [-78, -14], [-78, -6], [-102, -6]];
/** Pond reach: the strip must hold at least this many walkable cells within GOOSE_REACH metres of the pond outline. */
export const GOOSE_MIN_CELLS = 6;
export const GOOSE_REACH = 8;
/** Night watchman bots at the far (west) end of the south fields. */
export const SOUTH_FIELDS_FAR = [[-360, 270], [-315, 270], [-315, 315], [-360, 315]];
/** The far end must hold at least this many walkable cells. */
export const FAR_MIN_CELLS = 4;
