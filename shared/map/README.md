# shared/map

Central-campus map (Aldrich Park and the ring around it), generated from OpenStreetMap. Data © OpenStreetMap contributors (ODbL).

| File | What |
|---|---|
| `source/osm-central.json` | Committed OSM snapshot (`node shared/map/fetch.mjs` refreshes it; needs network) |
| `build.mjs` | `node shared/map/build.mjs [outDir]` writes `campus.json`, `collision.bin`, `areas.json` and `cardtables.json`. Offline and deterministic |
| `campus.json` | Compact map for rendering |
| `collision.bin` | Collision grid for client and server |
| `areas.mjs` | Gameplay places (spawn, shop, area polygons, card tables) in metres; edit here, then rebuild |
| `areas.json` | Gameplay areas for client and server |
| `cardtables.json` | Card table plaza and its 4 tables |
| `config.mjs` | Bounding box, origin, chunk size, cell size |

## Coordinates

Metres from the origin (centre of the bounding box): **x = east, z = south**, y up. Files store integer **decimetres** (1 unit = 0.1 m).

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

Cell `(col,row)` covers x in `[originX + col*cell, +cell)`, z likewise. A cell is blocked when its centre is inside a building or water. Cells outside the file are blocked.

Go reading sketch: `blocked := data[22+idx>>3]>>(idx&7)&1 == 1`.

## `areas.json`

Gameplay places from `docs/specs/map.md` (Gameplay areas). Same units as `campus.json`: integer decimetres, x east, z south.

`{version, units, spawn{x,z}, safeRadius, shop{x,z}, areas}`

- `version`: 1.
- `spawn`: where new players and respawns appear. `safeRadius`: no combat within this distance of `spawn` (400 = 40 m).
- `shop`: NPC shop position, inside the safe circle.
- `areas[]`: `{id, kind, p}`, plus `osm` (source way id) when the polygon comes from OSM. `kind` is `"pve"` or `"pvp"`; `p` is a closed polygon `[x0,z0,x1,z1,...]` (last point not repeated).
  - `aldrich_park` (`pve`): the campus.json green polygon of way 168468694. The safe circle is cut out of it by rule, not geometry.
  - `south_fields` (`pve`)
  - `pvp_zone` (`pvp`): the only place players can attack each other.

`build.mjs` snaps `spawn` and `shop` to the centre of their collision cell and fails if that cell is blocked.

## `cardtables.json`

The card table area from `docs/specs/map.md` (Gameplay areas): a small plaza about 20 m south of the spawn, inside the safe circle. Kept out of `areas.json` so the server's area ids don't change. Same units: integer decimetres, x east, z south.

`{version, units, plaza, tables}`

- `version`: 1.
- `plaza`: closed polygon `[x0,z0,x1,z1,...]` (last point not repeated) around the tables, wholly inside the safe circle.
- `tables[]`: `{id, x, z}`. `id` 1 to 4; `x`, `z` the table centre, at least 3 m from every other table.

`build.mjs` snaps each table to the centre of its collision cell and fails if that cell is blocked.
