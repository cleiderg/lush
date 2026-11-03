# Skills and bank

**Part of:** [[PRD]] · **Status:** Milestone 9 (D-051)

> Gathering and cooking skills, and a bank. Owner: Server world (rules), Client (presentation), Client render (trees, fishing spots, range, bank booth), Server platform (bank storage).

**Every number on this page is a placeholder** to tune after playtests, kept in server data files like the gameplay numbers ([[gameplay]]), never hard-coded. Where the places are is in [[map]] (Gameplay areas).

## Overview

Three skills, OSRS style, give players something to do besides fighting, and feed the combat loop: fish is caught, cooked, and eaten in combat. Logs are sold for gold and, from milestone 10, built into furniture ([[housing]]).

| Skill | What you do | Where | Tool |
|---|---|---|---|
| Woodcutting | Chop trees for logs | Trees around Aldrich Park | Hatchet (in the inventory or equipped as a weapon) |
| Fishing | Catch raw fish | The fishing spots at the pond | Small net or fishing rod (in the inventory) |
| Cooking | Cook raw fish into food | The grill next to the shop | None |

## Levels and XP

- Each skill has its own level, 1 to 30, using the same OSRS experience table as combat ([[gameplay]]).
- Skill XP never counts toward combat level, and combat XP never counts toward skills.
- **Total level** (the sum of combat and the three skills, 4 to 120) shows in the skills panel, and tapping another player shows "Combat N · Total N" above their head for TAP_LABEL_MS (3 s); tapping again or elsewhere hides it.
- Reaching a new level shows a level-up message in the chat log and a short effect nearby players can see.
- Skill levels and XP are saved with the player ([[persistence]]).

## Gathering (woodcutting and fishing)

- The player taps Interact next to a tree or fishing spot (range 1 tile, including diagonals). The server checks the level and tool, then the player keeps trying on its own until the inventory is full, the resource is used up, the player moves, or combat starts.
- One attempt every **4 combat ticks** (2.4 s). Each attempt succeeds with a chance that rises with level, the OSRS way: chance = low + (high − low) × (level − 1) / 29, using the low and high values in the tables. A better tool adds its bonus to both.
- A success puts one item in the inventory and gives the XP in the table.
- Logs, raw fish, cooked fish, burnt fish and tools don't stack in the inventory: each takes one slot, like food and equipment ([[gameplay]]). Every item still stacks in the bank.
- **Trees** fall after a number of logs (1 in 8 chance per log, so 8 on average) and grow back after the respawn time. While felled they show as a stump and can't be chopped.
- **Fishing spots** never run out but move to another spot at the same pond every 3 to 5 minutes.
- Equipping a hatchet as a weapon still lets it chop; holding it in the inventory works too.

| Tree | Level | XP | Low / high chance | Respawn | Log | Shop buys log for |
|---|---|---|---|---|---|---|
| Tree | 1 | 25 | 0.25 / 0.75 | 15 s | Logs | 2 |
| Oak | 15 | 37.5 | 0.12 / 0.50 | 30 s | Oak logs | 6 |
| Eucalyptus | 25 | 67.5 | 0.06 / 0.32 | 60 s | Eucalyptus logs | 15 |

| Fishing spot | Level | Tool | Catch | XP | Low / high chance | Shop buys raw for |
|---|---|---|---|---|---|---|
| Net spot | 1 | Small net | Raw shrimp | 10 | 0.25 / 0.75 | 1 |
| Rod spot | 5 | Fishing rod | Raw sardine (level 5) or raw trout (level 20) | 20 / 50 | 0.20 / 0.60 | 2 / 8 |

At a rod spot the server tries the best fish the player's level allows first (trout at 20+), then the next one down, as in OSRS.

| Tool | Kind | Level | Bonus | Shop sells for | Shop buys for | Notes |
|---|---|---|---|---|---|---|
| Bronze hatchet | Weapon / tool | Woodcutting 1 | +0.00 | 30 | 10 | Weapon stats 4 / 4 / 0, speed 5 |
| Steel hatchet | Weapon / tool | Woodcutting 15 | +0.10 | 200 | 70 | Weapon stats 8 / 9 / 0, speed 5, combat level 5 to equip |
| Small net | Tool | Fishing 1 | — | 20 | 5 | Not equippable |
| Fishing rod | Tool | Fishing 5 | — | 40 | 12 | Not equippable |

## Cooking

- The player taps Interact at the grill (range 1 tile) with raw fish in the inventory. The server cooks one fish every 4 combat ticks until there's no raw fish left, the player moves, or combat starts.
- Each attempt cooks or burns the fish. The burn chance falls with level and is zero from the "stops burning" level.
- Cooked fish is food and follows the food rules in [[gameplay]]. Burnt fish can't be eaten and the shop buys it for 0.

| Raw fish | Cooking level | XP | Burn chance at the required level | Stops burning at | Cooked | Heals | Shop buys cooked for | Shop sells cooked for |
|---|---|---|---|---|---|---|---|---|
| Raw shrimp | 1 | 30 | 50% | 15 | Shrimp | 3 | 2 | 6 |
| Raw sardine | 5 | 40 | 50% | 20 | Sardine | 4 | 3 | — |
| Raw trout | 15 | 70 | 50% | 30 | Trout | 7 | 10 | — |

The burn chance falls in a straight line from the value at the required level to 0 at the "stops burning" level.

## Bank

- A **bank booth** next to the shop stall ([[map]]). A player within 2 tiles can open it.
- **200 slots.** Every item stacks in the bank, including equipment and food, so one slot holds any number of one item.
- Deposit or withdraw one, five, all, or a typed amount, plus **Deposit inventory** and **Deposit equipment** buttons.
- Gold stays a separate balance and is not stored in the bank in milestone 9 (still never dropped on death, as in [[gameplay]]).
- Every deposit and withdrawal is one database transaction and is written to the economy audit log ([[persistence]]). The server re-checks both sides at commit, as for trades.
- The bank can't be opened during combat or in the PvP zone.
- Banked items are never lost on death. This is the reason for the bank: players can keep valuables safe before entering the PvP zone.

## Interact button

Add to the order in [[gameplay]], after the shop stall: the bank booth; the grill (when carrying raw fish); the nearest tree or fishing spot within 1 tile the player has the level and tool for. Everything after stays in the same order.

## Client

- A **Skills** panel (a tab next to the inventory) shows each skill's level, XP, and XP to the next level, plus total level.
- While gathering or cooking, the character plays a simple looping animation (chop, cast, cook) and a small progress indicator shows over the resource.
- Messages in the chat log for "You need a hatchet", "You need Woodcutting level 15", "Your inventory is full", and the like.
- A **Bank** panel like the inventory, with a search box and the buttons above. Large tap targets for mobile.

## Server

- Skills are one system on the world tick, like combat, driven by the 0.6 s combat tick.
- All chances are rolled on the server. The client is only told the result.
- Trees, fishing spots, the grill and the bank booth are placed from shared map data, so client and server agree on where they are.
- The bot load test adds gatherers: some bots chop and fish, so the skill system is covered at 1,000 bots.

## Out of scope for milestone 9

- Other skills (mining, smithing, firemaking, crafting, agility and so on).
- Gold in the bank, bank PINs, bank tabs.
- Shared or clan banks.
