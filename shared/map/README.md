# shared/map

Central-campus map (Aldrich Park and the ring around it), generated from OpenStreetMap. Data © OpenStreetMap contributors (ODbL).

| File | What |
|---|---|
| `source/osm-central.json` | Committed OSM snapshot of central campus (`node shared/map/fetch.mjs central` refreshes it; needs network) |
| `source/osm-housing.json` | Committed OSM snapshot of the Mesa Court and Middle Earth housing areas (`node shared/map/fetch.mjs housing` refreshes it) |
| `build.mjs` | `node shared/map/build.mjs [outDir] [--no-nature]` writes `campus.json`, `collision.bin`, `areas.json`, `cardtables.json`, `props.json`, `skills.json` and `parknature.json` (skipped with `--no-nature`). Offline and deterministic |
| `campus.json` | Compact map for rendering |
| `collision.bin` | Collision grid for client and server |
| `areas.mjs` | Gameplay places (spawn, shop, area polygons, card tables) in metres; edit here, then rebuild |
| `areas.json` | Gameplay areas for client and server |
| `cardtables.json` | Card table plaza and its 4 tables |
| `props.mjs` | Prop types, placement seed and busy-area rules; edit here, then rebuild |
| `props.json` | Campus props (benches, lamp posts, bins, ..., food carts, string lights and table-top cups, plates and pizza boxes), visual only |
| `skills.mjs` | M9 skill tree counts, pond shape and fishing spot counts, placement seed and clearances; edit here, then rebuild |
| `skills.json` | M9 woodcutting trees, pond and fishing spots, bank booth and grill, for client and server |
| `parknature.mjs` | Aldrich Park nature types, seed, densities and clearances; edit here, then rebuild |
| `parknature.json` | Aldrich Park trees, rocks, bushes, flower beds, picnic blankets and birds, visual only |
| `config.mjs` | Bounding box, fixed origin, housing outline and connecting path ids, chunk size, cell size |

## Housing areas (M10)

The map also covers **Mesa Court** and **Middle Earth** (`housing.md`) and the walk to them from Aldrich Park. They come from `source/osm-housing.json`; `build.mjs` adds only what lies wholly inside an area outline, plus the connecting paths, and never changes anything from `osm-central.json`. `bounds` and the collision grid grow to include the outlines; the origin does not move. The ids are in `config.mjs` (`HOUSING`):

| What | OSM way id |
|---|---|
| Mesa Court outline (`landuse=residential`) | 171555779 |
| Middle Earth outline (`landuse=residential`) | 171559658 |
| Connecting path to Mesa Court (footway from Aldrich Park) | 365551570 |

Middle Earth needs no connecting path: central footway 184993930 already runs into it. Buildings, plazas and green areas outside the central bounding box are kept only when wholly inside an outline; paths only when wholly inside one or listed as connecting. The outlines are not drawn and are not in `areas.json`.

## Coordinates

Metres from the origin (33.64525, -117.8425: the centre of the central-campus bounding box, fixed so it never moves when the map grows): **x = east, z = south**, y up. Files store integer **decimetres** (1 unit = 0.1 m).

## Chunks

The world is split into square chunks of **64 m** (`chunkSize`). Chunk `(cx, cz) = (floor((x - minX) / 64), floor((z - minZ) / 64))` where `minX`, `minZ` are `bounds` in metres. Bounds are whole multiples of the chunk size.

## `campus.json`

`{version, attribution, units, origin{lat,lon}, chunkSize, bounds{minX,minZ,maxX,maxZ}, buildings, paths, plazas, green}`

- `buildings[]`: `{id, h, p}`. `h` height, `p` closed footprint `[x0,z0,x1,z1,...]` (last point not repeated).
- `paths[]`: `{id, w, s, p}`. `w` width, `s` surface kind, `p` open polyline. `s` comes from the OSM `highway` tag:
  - `0` dirt: `path`
  - `1` stone: `footway`, `steps`, `cycleway`, `pedestrian` (as a line)
  - `2` paved: `service`, `living_street`, `residential`, `unclassified`, `tertiary` (service and road types)
- `plazas[]`: `{id, p}`. Paved pedestrian areas: closed `highway=pedestrian` + `area=yes` ways, written here instead of in `paths`. `p` closed polygon (last point not repeated, at least 3 points). Drawn only; plazas don't change collision.
- `green[]`: `{id, k, p}`. `k`: 0 grass, 1 wood, 2 water, 3 pitch; `p` closed polygon.

## `collision.bin`

Little-endian. 22-byte header, then bits.

| Offset | Type | Field |
|---|---|---|
| 0 | 4 bytes | magic `LCOL` |
| 4 | u8 | version (1) |
| 5 | u8 | flags (0) |
| 6 | u16 | cell size in decimetres (10 = 1 m) |
| 8 | u16 | chunk size in cells (64) |
| 10 | u16 | width in cells (x direction) |
| 12 | u16 | height in cells (z direction) |
| 14 | i32 | origin x (west edge) in decimetres |
| 18 | i32 | origin z (north edge) in decimetres |
| 22 | bytes | `ceil(width*height/8)` bytes, row-major (`index = row*width + col`, row = z, col = x), LSB-first; bit 1 = blocked |

Cell `(col,row)` covers x in `[originX + col*cell, +cell)`, z likewise. A cell is blocked when its centre is inside a building or water, except the floor and door cells of interiors (`interiors` in `areas.json`). Cells outside the file are blocked.

Go reading sketch: `blocked := data[22+idx>>3]>>(idx&7)&1 == 1`.

## `areas.json`

Gameplay places from `docs/specs/map.md` (Gameplay areas). Same units as `campus.json`: integer decimetres, x east, z south.

`{version, units, spawn{x,z}, safeRadius, shop{x,z}, areas, interiors}`

- `version`: 1.
- `spawn`: where new players and respawns appear. `safeRadius`: no combat within this distance of `spawn` (400 = 40 m).
- `shop`: NPC shop position, inside the safe circle.
- `areas[]`: `{id, kind, p}`, plus `osm` (source way id) when the polygon comes from OSM. `kind` is `"pve"` or `"pvp"`; `p` is a closed polygon `[x0,z0,x1,z1,...]` (last point not repeated).
  - `aldrich_park` (`pve`): the campus.json green polygon of way 168468694. The safe circle is cut out of it by rule, not geometry.
  - `south_fields` (`pve`)
  - `pvp_zone` (`pvp`): the only place players can attack each other.
- `interiors[]`: `{id, kind, p}`, enterable building interiors. `kind` is `"safe"`; `p` is the walkable floor, a closed polygon like the areas'. A player whose position is inside `p` is inside the interior.
  - `student_center_cafe` (`safe`): the café on the Student Center's ground floor (`CAFE` in `areas.mjs`), a 10 × 8 m room at the building's south side (OSM way 157979894). In `collision.bin` its floor cells and the door cells cut through its south wall (`CAFE.door`) are walkable; the rest of the footprint stays blocked, so the cells round the floor are its walls and the door is the only way in.

`build.mjs` snaps `spawn` and `shop` to the centre of their collision cell and fails if that cell is blocked.

## `cardtables.json`

The card table area from `docs/specs/map.md` (Gameplay areas): a small plaza about 20 m south of the spawn, inside the safe circle. Kept out of `areas.json` so the server's area ids don't change. Same units: integer decimetres, x east, z south.

`{version, units, plaza, tables}`

- `version`: 1.
- `plaza`: closed polygon `[x0,z0,x1,z1,...]` (last point not repeated) around the tables, wholly inside the safe circle.
- `tables[]`: `{id, x, z}`. `id` 1 to 4; `x`, `z` the table centre, at least 3 m from every other table.

`build.mjs` snaps each table to the centre of its collision cell and fails if that cell is blocked.

## `props.json`

Visual-only campus props (`docs/specs/visual-style.md`, Props). They never change `collision.bin`, and the server ignores them. Same units: integer decimetres, x east, z south.

`{version, units, types, props}`

- `version`: 1.
- `types[]`: prop type names; a prop's type is an index into this list (order from `PROP_TYPES` in `props.mjs`).
- `props[]`: `[type, x, z, rot]`. `x`, `z` the footprint centre; `rot` whole degrees, clockwise seen from above, 0 = front facing south.

`build.mjs` places them with one seeded generator (`PROP_SEED`), so the file is byte-identical on every build. Busy areas (spawn, shop, card plaza centre) get a 3 × 3 block of 10 × 10 m cells around their centre; every cell that is at least half walkable gets `BUSY_TYPES` different types (the build fails if one doesn't fit). Every area in `areas.json` without props gets a small cluster near its centre. No prop centre is on a blocked cell or inside a building, near the spawn point, in front of the shop stall, on a card table, or on the bank booth or grill.

## `skills.json`

Milestone 9 skill objects from `docs/specs/map.md` (Gameplay areas: Bank booth, Grill, Trees, Pond) and `docs/specs/skills.md`, so client and server agree on where they are. Kept out of `areas.json` and `collision.bin`, which don't change: the pond is not blocked in the grid yet (pond collision comes later, #399). Same units: integer decimetres, x east, z south.

`{version, units, bank, grill, trees, pond}`

- `version`: 1.
- `bank`, `grill`: `{x, z}`, the bank booth and grill (`BANK_BOOTH` and `GRILL` in `parknature.mjs`) snapped to the centre of their collision cell, next to the shop stall and inside the safe circle. The build fails if either cell is blocked or outside the safe circle, and props keep `STATION_CLEAR` m (plus their footprint) from both.
- `trees[]`: `{id, kind, x, z}`. `kind` is `"tree"`, `"oak"` or `"eucalyptus"` (12, 6 and 4, from `SKILL_TREES`); `id` is `<kind>-<n>` with n from 1, stable across builds. Each is on a cell centre of walkable park grass (same rule as the nature), outside the safe circle by `SAFE_MARGIN` m, off paths and plazas, clear of props and the pond's shore, with walkable cells on all four sides and at least `SKILL_TREE_SPACING` m from every other skill tree. Eucalyptus stand on the park's east side (x of at least 40 m).
- `pond`: `{p, active, spots}`.
  - `p`: the hand-made pond outline near (-90, -20), a closed polygon `[x0,z0,x1,z1,...]` (last point not repeated). The build moves it up to `POND_SHIFT` m from `POND_CENTRE` until it and `POND_SHORE` m around it are open park grass, clear of props and outside the safe circle.
  - `active`: `{net, rod}`, how many fishing spots of each kind exist at once (2 and 2).
  - `spots[]`: `{id, kind, x, z}`, candidate positions on the outline the active spots move between; `kind` is `"net"` or `"rod"`, `id` is `<kind>-<n>`. Spread evenly round the edge, kinds in turn, each with a walkable cell outside the pond next to it.

`build.mjs` places them after the props and before the nature, with their own seeded generator (`SKILLS_SEED`), so the file is byte-identical on every build and doesn't depend on `--no-nature`. The nature keeps its canopies off the skill trees and stays out of the pond and its shore.

## `parknature.json`

Visual-only Aldrich Park nature (`docs/specs/visual-style.md`, Ground and nature; Props, Aldrich Park): trees of three kinds, two rock shapes, bushes, flower beds, picnic blankets and birds. Like the props, it never changes `collision.bin` (`--no-nature` builds the same grid) and the server ignores it. Same units: integer decimetres, x east, z south.

`{version, units, types, items}`

- `version`: 1.
- `types[]`: nature type names; an item's type is an index into this list (order from `NATURE_TYPES` in `parknature.mjs`).
- `items[]`: `[type, x, z, rot, scale, stretch]`. `x`, `z` the centre; `rot` whole degrees, clockwise seen from above; `scale` uniform scale in percent; `stretch` extra height scale in percent.

`build.mjs` places them after the props with their own seeded generator (`NATURE_SEED`), so the file is byte-identical on every build and the props don't move. The park is split into 20 × 20 m blocks aligned to the origin; each gets trees, bushes, rocks and flower beds in proportion to its walkable grass (in the park, on a walkable cell, off paths, plazas and the card plaza), and every block at least half grass gets at least one tree, rock or bush (the build fails if one doesn't fit). Picnic blankets and small groups of birds go on open grass around the spawn. Nothing is placed on a path, plaza, blocked cell or prop, in the pond or its shore, under a skill tree's canopy, or within 3 m of the spawn point, the shop stall, or the M9 bank booth and grill.
