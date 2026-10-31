# Map

**Part of:** [[PRD]] · **Status:** Draft

> How the campus map is built and shared. Owners: Client render, Server world.

- Campus layout generated from OpenStreetMap data, with hand-made landmarks.
- Map split into fixed-size chunks shared by the client loader and the server's spatial grid.
- Collision data shared by client and server.
- Visible "© OpenStreetMap contributors" attribution (license requirement).

## Coverage

Phase 1 and milestones 8, 9 and 11: central campus only, Aldrich Park and the ring of buildings around it (D-023).

Milestone 10 adds the **Mesa Court** and **Middle Earth** housing areas and the walking paths to them from Aldrich Park ([[housing]]), from the same OpenStreetMap pipeline. Only those areas and paths are added; the rest of campus stays out.

## Landmarks

Phase 1 has three hand-made landmarks near Aldrich Park: Langson Library (OSM ways 171169252 and 1374185239), Aldrich Hall (62646030) and the Student Center (157979894). Each is built in code by client-render from low-poly, flat-colour primitives on its OSM building footprint in shared/map/campus.json, within the buildings/ budget (8,000 triangles), and replaces that building's generic extrusion. Landmarks are visual only: collision and map data still come from the OSM footprints. No UCI logos or mascot (PRD 9).

## Gameplay areas

Decided in #100. Coordinates are approximate, in metres from the world origin in `shared/map/config.mjs` (x = east, z = south; `campus.json` stores decimetres, so multiply by 10). Agents keep each place where it is listed, snap it to walkable cells, and keep spawn and shop clear of buildings and paths through doors. Don't move a place to a different part of campus without an owner decision.

| Place | Where | Rules |
|---|---|---|
| **Safe spawn point** | Centre of Aldrich Park (the park's green area, OSM way 168468694), around (-22, -76) | New players and respawns appear here. No combat within 40 m: NPCs don't enter this safe circle and attacks can't start in it. |
| **NPC shop** | A market stall about 20 m north of the spawn, around (-22, -96) | Inside the safe circle, so new players find it straight away. |
| **PvE: Aldrich Park** | The rest of Aldrich Park, outside the safe circle | Beginner NPCs, levels 1 to 5, all passive (they only fight back), plus the aggressive geese at the pond (level 6, milestone 11, [[quests]]). |
| **PvE: South fields** | Open ground south-west of the park, around x -360 to -200, z 240 to 320 | Tougher NPCs, levels 8 to 20; some are aggressive. Roughly 2 minutes' walk from spawn. |
| **PvP zone** | Open ground north-east of the park, outside Ring Road, around x 200 to 400, z -400 to -280 | The only place players can attack each other. Its border is drawn on the ground in red, and the client shows a warning banner while a player is within 10 m outside the border, and a "PvP zone" indicator while they are inside. No trading inside it. At least 300 m from spawn. |
| **Card table area** (stretch) | A small plaza about 20 m south of the spawn, around (-22, -56) | Inside the safe circle, so no combat can interrupt a game. 4 tables. |
| **Bank booth** (M9) | Next to the shop stall, around (-12, -96) | Inside the safe circle. [[skills]] Blocks walking. |
| **Grill** (M9) | Next to the shop stall on the other side, around (-32, -96) | Inside the safe circle. Cooking. [[skills]] Blocks walking. |
| **Trees** (M9) | Spread through Aldrich Park outside the safe circle: about 12 trees, 6 oaks, and 4 eucalyptus on the park's far (east) side | Woodcutting. Placed on walkable grass, not on paths. [[skills]] Each tree and stump blocks walking. |
| **Pond** (M9) | A small hand-made pond in Aldrich Park outside the safe circle, around (-90, -20) | 2 net spots and 2 rod spots that move around its edge. Water is not walkable. Agents may shift it within the park to fit the grass. [[skills]] |
| **Talking NPCs** (M11) | Librarian outside Langson Library; Park ranger at the south fields edge nearest the park; Food truck cook next to the grill; Guide at the spawn point | [[quests]] |
| **Throne room** (M12) | Inside Aldrich Hall (OSM way 62646030), with a main and a side gate | Combat zone only during the Sunday siege. [[groups]] |
| **Group halls** (M12) | Six non-landmark buildings on the ring around Aldrich Park, listed in the map data | Interiors with entrance hall, main room and vault room. [[groups]] |
| **Revival points** (M13) | The safe spawn point; a first-aid station at the south fields edge nearest the park; one just outside the PvP zone border | [[trust-and-death]] |
| **Hidden vendor** (M13) | Three spots on campus outside the safe circle; moves at midnight Irvine time | [[trust-and-death]] |
| **Housing areas** (M10) | Mesa Court and Middle Earth, at their real positions | Houses, a Campus Thrift stall and a workbench in each. No combat. [[housing]] |

The areas are polygons and points in shared map data (one file the client and server both read), so the server decides who is where. All are placeholders to adjust after playtests.
