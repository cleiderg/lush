// Shared constants for the map tools. Keep in sync with README.md.
export const BBOX = { south: 33.6405, west: -117.848, north: 33.65, east: -117.837 };
/**
 * World origin (x=0,z=0). x = east, z = south, metres. Fixed: it is the centre of the central-campus BBOX, and must
 * never move, so every coordinate on central campus stays put when the map grows.
 */
export const ORIGIN = { lat: 33.64525, lon: -117.8425 };
/**
 * M10 housing areas (source/osm-housing.json). `outline` is the OSM landuse way of each area. Buildings, plazas, green
 * areas and paths lying wholly inside an outline are added to the map. `connecting` lists the OSM ids of the only paths
 * outside the outlines (and outside BBOX) that are added, so the areas can be walked to from Aldrich Park.
 */
export const HOUSING = {
  areas: [
    { id: 'mesa_court', name: 'Mesa Court', outline: 171555779 },
    { id: 'middle_earth', name: 'Middle Earth', outline: 171559658 },
  ],
  connecting: [365551570],
  /** Rectangle fetched around the outlines. */
  fetchBox: { south: 33.6405, west: -117.848, north: 33.6545, east: -117.835 },
};
/** Houses per housing area (housing.md): the largest buildings wholly inside the outline. */
export const HOUSES_PER_AREA = 20;
/** Thrift stall and workbench: the workbench stands this far (m, min and max) from the stall. */
export const WORKBENCH_DIST = [1.5, 2.5];
/** Edge length of a chunk in metres. Client meshes and the server spatial grid use this. */
export const CHUNK_SIZE = 64;
/** Collision cell edge in metres. */
export const CELL_SIZE = 1;
