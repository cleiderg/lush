# Map

**Part of:** [[PRD]] · **Status:** Draft

> How the campus map is built and shared. Owners: Client render, Server world.

- Campus layout generated from OpenStreetMap data, with hand-made landmarks.
- Map split into fixed-size chunks shared by the client loader and the server's spatial grid.
- Collision data shared by client and server.
- Visible "© OpenStreetMap contributors" attribution (license requirement).

## Phase 1 coverage

Central campus only: Aldrich Park and the ring of buildings around it (D-023).

## Landmarks

Phase 1 has three hand-made landmarks near Aldrich Park: Langson Library (OSM ways 171169252 and 1374185239), Aldrich Hall (62646030) and the Student Center (157979894). Each is built in code by client-render from low-poly, flat-colour primitives on its OSM building footprint in shared/map/campus.json, within the buildings/ budget (8,000 triangles), and replaces that building's generic extrusion. Landmarks are visual only: collision and map data still come from the OSM footprints. No UCI logos or mascot (PRD 9).

## Gameplay areas

Decided in #100. Coordinates are approximate, in metres from the world origin in `shared/map/config.mjs` (x = east, z = south; `campus.json` stores decimetres, so multiply by 10). Agents keep each place where it is listed, snap it to walkable cells, and keep spawn and shop clear of buildings and paths through doors. Don't move a place to a different part of campus without an owner decision.

| Place | Where | Rules |
|---|---|---|
| **Safe spawn point** | Centre of Aldrich Park (the park's green area, OSM way 168468694), around (-22, -76) | New players and respawns appear here. No combat within 40 m: NPCs don't enter this safe circle and attacks can't start in it. |
| **NPC shop** | A market stall about 20 m north of the spawn, around (-22, -96) | Inside the safe circle, so new players find it straight away. |
| **PvE: Aldrich Park** | The rest of Aldrich Park, outside the safe circle | Beginner NPCs, levels 1 to 5, all passive (they only fight back). |
| **PvE: South fields** | Open ground south-west of the park, around x -360 to -200, z 240 to 320 | Tougher NPCs, levels 8 to 20; some are aggressive. Roughly 2 minutes' walk from spawn. |
| **PvP zone** | Open ground north-east of the park, outside Ring Road, around x 200 to 400, z -400 to -280 | The only place players can attack each other. Its border is drawn on the ground in red, and the client shows a warning banner while a player is within 10 m outside the border, and a "PvP zone" indicator while they are inside. No trading inside it. At least 300 m from spawn. |

The areas are polygons in shared map data (one file the client and server both read), so the server decides who is where. All five are placeholders to adjust after playtests.
