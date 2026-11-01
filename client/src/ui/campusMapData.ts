import campusRaw from '../../../shared/map/campus.json?raw';
import skills from '../../../shared/map/skills.json';
import areas from '../../../shared/map/areas.json';
import { buildMapData, type CampusJson, type MapData } from './minimap';

let data: MapData | null = null;

/** The committed campus map for the minimap and big map, parsed once (the same campus.json the renderer draws). */
export function campusMapData(): MapData {
  data ??= buildMapData(JSON.parse(campusRaw) as CampusJson, areas, skills);
  return data;
}
