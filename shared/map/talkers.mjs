// M11 talking NPC spots (docs/specs/map.md, Gameplay areas: Talking NPCs; docs/specs/quests.md). Metres, x = east, z = south.
// build.mjs snaps each spot to a walkable collision cell centre and writes talkers.json. Edit here, then rebuild.

/** OSM way of the Langson Library podium (campus.json building); the librarian stands outside it. */
export const LANGSON_WAY = 171169252;
/** The librarian stands this far (min, max metres) outside the Langson footprint, on the open cell nearest the spawn. */
export const LIBRARIAN_GAP = [2, 4];
/** The guide stands near the spawn point: offset from it, metres. */
export const GUIDE_OFFSET = { x: 2, z: 0 };
/** The food truck cook stands next to the grill: offset from it, metres. */
export const COOK_OFFSET = { x: 0, z: 2 };
/** The park ranger's target at the south fields edge nearest Aldrich Park (the corner nearest the spawn), metres. */
export const RANGER_TARGET = { x: -205, z: 245 };
/** Talker ids, in file order. */
export const TALKER_IDS = ['librarian', 'park_ranger', 'food_truck_cook', 'guide'];
