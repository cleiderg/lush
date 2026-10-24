# Map

**Part of:** [[PRD]] · **Status:** Draft

> How the campus map is built and shared. Owners: Client render, Server world.

- Campus layout generated from OpenStreetMap data, with hand-made landmarks.
- Map split into fixed-size chunks shared by the client loader and the server's spatial grid.
- Collision data shared by client and server.
- Visible "© OpenStreetMap contributors" attribution (license requirement).

## Phase 1 coverage

Central campus only: Aldrich Park and the ring of buildings around it (D-023).

## Gameplay areas

Where the gameplay places go. The owner names an approximate location for each (for example, "next to <building>"); agents then place it in shared map data. Agents must not pick locations themselves.

| Place | Location |
|---|---|
| Combat area (PvE) | **TBD:** owner names the location |
| PvP zone | **TBD:** owner names the location |
| NPC shop | **TBD:** owner names the location |
| Safe spawn point | **TBD:** owner names the location |
