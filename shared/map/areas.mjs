// Gameplay places from docs/specs/map.md (Gameplay areas). Metres, x = east, z = south.
// Placeholders to adjust after playtests. build.mjs snaps points to cell centres and writes areas.json.

/** OSM way whose green polygon is the Aldrich Park PvE area. */
export const ALDRICH_PARK_WAY = 168468694;

/** Centre of Aldrich Park. New players and respawns appear here. */
export const SPAWN = { x: -22, z: -76 };
/** No combat within this many metres of the spawn. */
export const SAFE_RADIUS = 40;
/** Market stall about 20 m north of the spawn, inside the safe circle. */
export const SHOP = { x: -22, z: -96 };

/** Open ground south-west of the park. */
export const SOUTH_FIELDS = [[-360, 240], [-200, 240], [-200, 320], [-360, 320]];
/** Open ground north-east of the park, outside Ring Road. Corner nearest spawn is cut so every point stays 300 m+ away. */
export const PVP_ZONE = [[200, -400], [400, -400], [400, -280], [220, -280], [200, -300]];

/** Card table plaza about 20 m south of the spawn, inside the safe circle. build.mjs writes cardtables.json. */
export const CARD_PLAZA = [[-29, -63], [-15, -63], [-15, -49], [-29, -49]];
/** The plaza's 4 card tables, ids 1 to 4, 6 m apart. */
export const CARD_TABLES = [
  { id: 1, x: -25, z: -59 },
  { id: 2, x: -19, z: -59 },
  { id: 3, x: -25, z: -53 },
  { id: 4, x: -19, z: -53 },
];

/** OSM building way of the Student Center. */
export const STUDENT_CENTER_WAY = 157979894;
/**
 * Student Center café on the ground floor, in whole metres: `floor` is the walkable room [x0, z0, x1, z1)
 * (10 x 8 m, at the building's south side facing the park), `door` the cells cut through its south wall to the outside.
 * The building's cells round the floor stay blocked and are the café's walls. build.mjs writes `interiors` in areas.json.
 */
export const CAFE = { id: 'student_center_cafe', floor: [40, -355, 50, -347], door: [44, -347, 46, -346] };
