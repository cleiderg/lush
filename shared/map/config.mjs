// Shared constants for the map tools. Keep in sync with README.md.
export const BBOX = { south: 33.6405, west: -117.848, north: 33.65, east: -117.837 };
/** World origin (x=0,z=0): centre of the bbox. x = east, z = south, metres. */
export const ORIGIN = { lat: (BBOX.south + BBOX.north) / 2, lon: (BBOX.west + BBOX.east) / 2 };
/** Edge length of a chunk in metres. Client meshes and the server spatial grid use this. */
export const CHUNK_SIZE = 64;
/** Collision cell edge in metres. */
export const CELL_SIZE = 1;
