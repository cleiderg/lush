# Gameplay

**Part of:** [[PRD]] · **Status:** Draft

> Phase 1 rules for combat, NPCs, items, gold, the shop, emotes, trading and death. Owner: Server world (rules), Client (presentation).

**Every number on this page is a placeholder** (decided in #96, #98 and #99; D-047), to tune after playtests. The server keeps them in data files (rule constants, the NPC table, the item table and the shop list), never hard-coded, and sends the client what it needs to show. Where the places are is in [[map]] (Gameplay areas).

## Core loop

1. Spawn in the centre of Aldrich Park with 100 gold and a starting kit.
2. Walk, chat, wave, and interact.
3. Fight NPCs: beginner animals in Aldrich Park, tougher ones in the south fields. They drop gold and items, and fighting raises your combat level.
4. Equip weapons and armor to change combat stats; equipment shows on the character.
5. Sell loot to the NPC shop for gold, and buy food and better gear there.
6. Enter the PvP zone for risk and reward.
7. Bet gold at card tables (stretch).

## Combat

- Runs on the 0.6 s combat tick (every 12th world tick at 20 Hz).
- **PvE:** NPCs in designated combat areas.
- **PvP:** only inside a clearly marked PvP zone, and only against players within 5 combat levels of you. Players cannot attack each other anywhere else.
- Fully server-decided: hit chance, damage, and death.
- **Melee only** in Phase 1. Attack range is 1 tile, including diagonals. Each weapon has an attack speed in combat ticks; unarmed is 4 (2.4 s).
- **Combat level:** one level per player, 1 to 30. You gain 4 XP per point of damage you deal. Levels follow the OSRS experience table (level 2 at 83 XP, level 30 at 13,363 XP).
- **Hitpoints:** maximum HP is 9 + combat level (10 at level 1, 39 at level 30).
- **Hit chance** (the OSRS formula): attack roll A = (level + 8) × (accuracy + 64); defence roll D = (level + 8) × (defence + 64), using the attacker's accuracy and the defender's defence from equipment. If A > D, the chance is 1 − (D + 2) / (2 × (A + 1)); otherwise it is A / (2 × (D + 1)).
- **Damage:** a hit deals a whole number from 1 to the max hit, chosen uniformly; a miss deals 0. Max hit = floor(0.5 + (level + 8) × (strength + 64) / 640), at least 1.
- NPCs use the same formulas with the level and bonuses from the NPC table.
- **Health regeneration:** 1 HP every 6 s, starting 10 s after you last dealt or took damage.
- **Food** heals instantly. Eating delays your next attack by 1 combat tick.
- **Aggression:** passive NPCs only fight back. Aggressive NPCs attack a player within 4 tiles. Every NPC gives up and walks home once it is more than 12 tiles from its spawn spot.
- **Logging out:** a player can't log out within 10 s of combat; a player who disconnects stays in the world for 10 s.

## NPCs

| NPC | Area | Population | Level | HP | Accuracy / strength / defence | Attack speed | Behaviour | Respawn | Drops |
|---|---|---|---|---|---|---|---|---|---|
| Rabbit | Aldrich Park | 12 | 1 | 3 | 0 / 0 / 0 | 4 | Passive | 15 s | 1–3 gold |
| Squirrel | Aldrich Park | 10 | 2 | 5 | 2 / 0 / 2 | 4 | Passive | 20 s | 2–6 gold; Acorn (1 in 2) |
| Crow | Aldrich Park | 8 | 4 | 8 | 5 / 3 / 3 | 4 | Passive | 30 s | 3–10 gold; Shiny trinket (1 in 8) |
| Coyote | South fields | 8 | 9 | 20 | 12 / 10 / 8 | 4 | Aggressive | 45 s | 8–25 gold; Coyote pelt (1 in 2); Leather cap (1 in 20) |
| Rogue delivery robot | South fields | 5 | 15 | 35 | 20 / 18 / 25 | 5 | Aggressive | 60 s | 20–50 gold; Scrap metal (1 in 2); Bike lock (1 in 32); Robot plating (1 in 64) |
| Coyote alpha | South fields | 1 | 20 | 50 | 28 / 26 / 22 | 4 | Aggressive | 3 min | 40–100 gold; 3 Coyote pelts; Bike helmet (1 in 8) |

Every kill credits the gold straight to the killer's balance; each item rolls separately. Item drops appear on the ground where the NPC died and follow the same rules as PvP drops below, with the killer as owner.

## Items

- **Equipment:** weapons and armor that change combat stats and appear on the character model.
- **Loot:** other items to collect or sell.
- Every item creation and transfer is written to the economy audit log ([[persistence]]).
- **Inventory:** 28 slots. Equipped items don't take a slot. Gold is a separate balance, not an item, and never takes a slot.
- **Equipment slots:** weapon, head, body, shield.
- **Stacking:** loot stacks without limit in one slot; equipment and food don't stack.
- **Picking up:** a player within 1 tile of a ground item, including diagonals, can pick it up (subject to the drop reservation below).
- **Starting kit:** Wooden stick and Hoodie (both equipped) and 3 Boba.

| Item | Kind | Slot | Accuracy / strength / defence | Attack speed | Level to equip | Shop buys for | Shop sells for | Comes from |
|---|---|---|---|---|---|---|---|---|
| Wooden stick | Weapon | Weapon | 3 / 2 / 0 | 4 | 1 | 2 | 5 | Starting kit, shop |
| Bronze dagger | Weapon | Weapon | 8 / 5 / 0 | 3 | 1 | 15 | 40 | Shop |
| Bike lock | Weapon | Weapon | 14 / 16 / 0 | 5 | 10 | 60 | — | Robot drop |
| Hoodie | Armor | Body | 0 / 0 / 4 | — | 1 | 5 | 15 | Starting kit, shop |
| Cardboard shield | Armor | Shield | 0 / 0 / 3 | — | 1 | 3 | 10 | Shop |
| Leather cap | Armor | Head | 0 / 0 / 3 | — | 5 | 10 | — | Coyote drop |
| Bike helmet | Armor | Head | 0 / 0 / 6 | — | 10 | 30 | 80 | Shop, coyote alpha drop |
| Robot plating | Armor | Body | −2 / 0 / 14 | — | 12 | 150 | — | Robot drop |
| Boba | Food | — | Heals 4 | — | 1 | 1 | 5 | Starting kit, shop |
| Burrito | Food | — | Heals 10 | — | 1 | 3 | 15 | Shop |
| Acorn | Loot | — | — | — | — | 1 | — | Squirrel drop |
| Shiny trinket | Loot | — | — | — | — | 25 | — | Crow drop |
| Coyote pelt | Loot | — | — | — | — | 12 | — | Coyote drops |
| Scrap metal | Loot | — | — | — | — | 8 | — | Robot drop |

## Gold

Sources in Phase 1:
- A **starting amount** of 100 gold for new players.
- **NPC drops.**
- **Selling items** to the NPC shop.

**Sinks:** buying food and gear at the shop. Gold can be bet at card tables. Gold can never be bought, sold, or exchanged for real money (D-013).

## NPC shop

- The Campus Market stall next to the spawn point ([[map]]). A player within 2 tiles can open it.
- It buys any item for the "Shop buys for" price and sells the items with a "Shop sells for" price, with unlimited stock. Prices are fixed in Phase 1.
- Each purchase or sale is one database transaction, and is written to the economy audit log.

## Emotes

- **Wave:** shows for 2 s (40 world ticks at 20 Hz). A new wave is ignored while one is showing, and starting to walk ends it. Nearby players see it.

## Interact button

- Interact picks, in order: the shop stall within shop distance; the nearest NPC within attack range (attack); the nearest player within trade distance (a trade request outside the PvP zone, an attack inside it). With no target it does nothing.

## Player trading

- Two players near each other open a trade window, each offers items and gold, and both confirm.
- The swap is saved as **one database transaction**: either everything moves or nothing does.
- Both inventories are re-checked on the server at the moment of commit; a trade fails if either side no longer has what it offered.
- Every trade is written to the economy audit log.
- **Confirmation:** two screens, as in OSRS. The second shows the final offer from both sides; changing anything returns both players to the first screen.
- **Distance:** the players must be within 3 tiles of each other.
- **PvP zone:** trading is not allowed inside it.
- **Limits:** none beyond the above.

## Death in the PvP zone

- The player respawns at the safe spawn point with full HP.
- They **drop everything except their 3 most valuable items** (by shop buy price), OSRS Wilderness style. Gold is never dropped in Phase 1, because there is no bank yet.
- Drops are reserved for the killer for 60 s, then anyone can take them. They disappear after 3 minutes.
- Trades and drops must be handled as single transactions so items can never be duplicated.

## Death outside the PvP zone (PvE)

Dying to NPCs costs nothing: the player respawns at the safe spawn point with full HP and keeps everything.
