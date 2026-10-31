# Player housing and decoration

**Part of:** [[PRD]] · **Status:** Milestone 10 (D-049, D-052)

> Owners: Server world (rules, rent, auctions), Server platform (storage), Protocol (placement messages), Client (edit mode, panels), Client render (housing areas, house exteriors, interiors, furniture models).

**Every number on this page is a placeholder** to tune after playtests, kept in server data files like the gameplay numbers ([[gameplay]]).

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
- A home rating system or leaderboard (a later "happy home" style score).
- Selling, giving or swapping a house to another player.
- Friends and factions (visiting is open or locked only).

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

- Homes sit in replicas of real UCI housing areas. The first release has two: **Mesa Court** and **Middle Earth**, with **20 houses each**. These are outside the Phase 1 map, which covers central campus only, so the map grows first to include both areas and the walking paths to them from Aldrich Park, generated from OpenStreetMap like the rest of the map ([[map]]). More areas are a later decision.
- Each house is one of the real buildings in its area (or a share of one, for large buildings), marked with a door the owner and visitors walk into. Walking in moves the player into that home's room; walking out returns them outside the door.
- Each area has a fixed number of houses. A player claims an empty house first come, first served.
- A claimed house shows on the shared world map for everyone, marked as that player's home.
- The owner keeps the house as long as they pay the monthly gold fee: **200 gold per month** (30 real days) in every area.
- **Claiming** a house costs two months' rent up front. An owner can prepay up to 6 months at a time, from a Rent button inside the house.
- Rent must always be paid at least one month in advance.
- If the owner falls below one month prepaid, the house goes up for a five-day auction to other players. The previous owner cannot bid.
- **Auction:** the starting bid is one month's rent. Bidding happens on a sign outside the house. A bid holds the gold (it can't be spent elsewhere) until the player is outbid, when it's returned. The highest bidder when the auction ends gets the house and pays their bid plus two months' rent; the bid is a gold sink. If they can no longer pay the rent, the next-highest bidder gets the offer. If nobody bids, the house becomes empty and claimable again.
- Owners get an in-game warning when they have less than 7 days left before dropping below one month prepaid: a message on login and a banner inside the house.
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

- Players can visit other players' homes. The owner chooses who: open or locked (friends and factions are later).
- At most 10 players in a home at once, owner included.
- Only the owner can edit; visitors can only look.

## Economy hooks

- Furniture comes from the furniture shop, the workbench (crafting from logs, [[skills]]), player trading and rare drops.
- **Furniture shop:** a Campus Thrift stall in each housing area, with fixed prices and unlimited stock, like the Campus Market ([[gameplay]]). It also buys furniture back for a third of its price.
- **Workbench:** next to each thrift stall. A recipe takes logs and gold and makes one piece of furniture straight away. No skill or level is needed in the first release.
- Placing and removing furniture never creates or destroys items. Furniture is an ordinary item that can be traded and banked.
- Rare and limited cosmetics give players a reason to show off their homes.
- Rent is a steady gold sink.

## First release

- One 8×8 room per player.
- About 30 furniture items, plus 6 wallpapers and 6 floors (the starter list below).
- Floor and surface stacking; wall items optional.
- Visiting other players' homes.

## Starter furniture

All low-poly and flat-coloured, original designs (no brands). Footprint is width × depth in tiles. "Thrift" is the shop price; "Workbench" is the recipe.

| Item | Footprint | Layer | Thrift | Workbench |
|---|---|---|---|---|
| Wooden chair | 1×1 | Floor | 40 | 4 Logs + 10 gold |
| Stool | 1×1 | Floor | 25 | 2 Logs + 5 gold |
| Wooden table | 2×1 | Floor, surface (2 slots) | 80 | 8 Logs + 20 gold |
| Oak dining table | 2×2 | Floor, surface (4 slots) | 300 | 6 Oak logs + 60 gold |
| Desk | 2×1 | Floor, surface (2 slots) | 120 | 4 Oak logs + 30 gold |
| Bookshelf | 2×1 | Floor, surface (2 slots) | 150 | 5 Oak logs + 30 gold |
| Single bed | 1×2 | Floor | 200 | 10 Logs + 50 gold |
| Double bed | 2×2 | Floor | 450 | 8 Oak logs + 100 gold |
| Sofa | 2×1 | Floor | 260 | — |
| Beanbag | 1×1 | Floor | 90 | — |
| Rug (small) | 2×2 | Floor (under items) | 60 | — |
| Rug (large) | 3×3 | Floor (under items) | 140 | — |
| Floor lamp | 1×1 | Floor | 70 | — |
| Eucalyptus wardrobe | 2×1 | Floor | 600 | 6 Eucalyptus logs + 150 gold |
| Eucalyptus cabinet | 1×1 | Floor, surface (1 slot) | 400 | 4 Eucalyptus logs + 100 gold |
| Potted plant | 1×1 | Floor or surface | 35 | — |
| Cactus | 1×1 | Surface | 20 | — |
| Desk lamp | 1×1 | Surface | 30 | — |
| Laptop | 1×1 | Surface | 250 | — |
| Stack of books | 1×1 | Surface | 15 | — |
| Coffee mug | 1×1 | Surface | 10 | — |
| Fish bowl | 1×1 | Surface | 120 | — |
| Mini fridge | 1×1 | Floor, surface (1 slot) | 180 | — |
| TV stand | 2×1 | Floor, surface (2 slots) | 160 | 6 Logs + 40 gold |
| Small TV | 1×1 | Surface | 300 | — |
| Wall clock | 1×1 | Wall | 50 | — |
| Poster | 1×1 | Wall | 25 | — |
| Window | 2×1 | Wall | 100 | — |
| Trophy shelf | 2×1 | Wall | 140 | 3 Oak logs + 30 gold |
| Mounted coyote pelt | 2×1 | Wall | — | 3 Coyote pelts + 50 gold |

Wallpapers and floors (6 of each, 50 gold each at the thrift stall): plain colours and simple patterns in the low-poly palette.

## Depends on

- Milestone 9 ([[skills]]) for logs and the bank.
- The map growing beyond central campus to include Mesa Court, Middle Earth and the paths to them.

## Decided for the first release (D-052)

- Auction: starting bid of one month's rent; no bids makes the house claimable again.
- Expansions, a home rating and house sales or gifts are later decisions, not in the first release.
- Rent: 200 gold per month, the same in every area.
- Areas: Mesa Court and Middle Earth, 20 houses each.
