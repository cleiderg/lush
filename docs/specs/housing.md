# Player housing and decoration

**Part of:** [[PRD]] · **Status:** Planned for Phase 2 (D-049)

> **Not Phase 1 work.** Player housing is out of scope for Phase 1 (PRD 4.2). Agents must not build any of this, or plan issues from it, until the owner moves it into a milestone.

## Overview

Every player can own a home they furnish and decorate freely, inspired by Animal Crossing: New Horizons. Homes are a core cosmetic and social feature: players express themselves, show off rare items, and invite others over.

## Goals

- Let players place, rotate, stack and remove furniture on a grid inside their home.
- Let players customise wallpaper, flooring and rugs per room.
- Make homes visitable by other players in real time.
- Tie furniture into the economy (crafting, shops, trading, drops).

## Non-goals (first release)

- Exterior or yard decoration and terraforming.
- Multiple rooms or home expansions (one room only at first).
- A home rating system (a later "happy home" style score).

## Player experience

1. The player enters their home and taps an Edit button to switch to edit mode.
2. An inventory drawer shows owned furniture, wallpaper and flooring.
3. The player drags an item onto the grid; a ghost preview shows green if valid, red if blocked.
4. The player can rotate (90° steps), move, or return an item to inventory.
5. Small items (lamps, plants, trinkets) can sit on top of surface items (tables, desks, shelves).
6. Wallpaper and flooring apply to the whole room with one tap.
7. Changes save automatically; visitors see updates live.

## Room and grid model

- A room is a square grid (8×8 tiles to start).
- Each item has a footprint (for example 1×1, 2×1, 2×2) and a rotation (0, 90, 180, 270).
- Items occupy tiles on the floor layer; surface items expose slots that small items can occupy.
- Wall items (posters, clocks, windows) snap to wall slots along the room edges.

## Neighborhoods and ownership

Homes are real houses in UCI's housing areas, claimed first come, first served and kept by paying a monthly gold fee.

- Homes sit in replicas of real UCI housing areas: Mesa Court, Middle Earth, and others (list to be decided). These are outside the Phase 1 map, which covers central campus only, so the map has to grow first.
- Each area has a fixed number of houses. A player claims an empty house first come, first served.
- A claimed house shows on the shared world map for everyone, marked as that player's home.
- The owner keeps the house as long as they pay the monthly gold fee.
- Rent must always be paid at least one month in advance.
- If the owner falls below one month prepaid, the house goes up for a five-day auction to other players. The previous owner cannot bid.
- When a house is lost, all of its furniture returns to the previous owner's inventory.
- A player can own at most one house.

## Data model (PostgreSQL)

- **housing_areas:** area_id, name, map position.
- **house_plots:** plot_id, area_id, map position, owner_id (null when empty), claimed_at, rent_paid_until.
- **homes:** home_id, plot_id, owner_id, wallpaper_item_id, flooring_item_id, grid_size.
- **placed_items:** placed_id, home_id, item_id, x, y, rotation, layer (floor, surface or wall), parent_placed_id (null unless stacked).
- **item_defs:** item_id, name, footprint_w, footprint_h, category, is_surface, surface_slots, is_wall_item, model_path.
- The player's inventory keeps owning unplaced items. Placing moves an item from the inventory into placed_items, and removing moves it back, each as one transaction written to the economy audit log ([[persistence]]).

## Server (Go)

- The server validates every placement: ownership, tile bounds, collisions, a valid surface slot, and that the player owns the item.
- Placement messages over WebSocket: place, move, rotate, remove, set_wallpaper, set_flooring.
- The server persists changes and broadcasts them to everyone currently in that home.
- Edit actions are rate-limited to prevent spam.

## Client (Three.js)

- Low-poly furniture models consistent with the OSRS-style art direction.
- Edit mode with a visible grid overlay, ghost preview and touch-friendly controls (drag to move, tap a button to rotate).
- Mobile first: large tap targets, and an inventory drawer that slides up from the bottom.

## Social

- Players can visit other players' homes. The owner chooses who: open, friends or faction only, or locked.
- Only the owner can edit; visitors can only look.

## Economy hooks

- Furniture comes from crafting, NPC shops, player trading and rare drops.
- Rare and limited cosmetics give players a reason to show off their homes.
- Rent is a steady gold sink.

## First release

- One 8×8 room per player.
- About 30 to 50 furniture items, plus a handful of wallpapers and floors.
- Floor and surface stacking; wall items optional.
- Visiting other players' homes.

## Depends on (not in Phase 1)

- The map growing beyond central campus to include the housing areas.
- Crafting, and friends or factions, for the parts above that use them.

## Open questions

- What is the auction's starting bid, and what happens if nobody bids?
- Can homes be expanded later, and at what cost?
- Should there be a home rating or leaderboard?
- What is the monthly gold fee, and does it differ by area?
- Can a player sell or give their house to someone else?
- Which other housing areas are included, and how many houses go in each?
