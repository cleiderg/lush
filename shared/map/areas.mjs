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
