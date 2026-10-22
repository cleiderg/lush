# Gameplay

**Part of:** [[PRD]] · **Status:** Draft

> Phase 1 rules for combat, items, gold, and death. Owner: Server world (rules), Client (presentation).

## Core loop

1. Spawn on central campus with a small starting amount of gold.
2. Walk, chat, wave, and interact.
3. Fight NPCs in combat areas; they drop gold and items.
4. Equip weapons and armor to change combat stats; equipment shows on the character.
5. Sell loot to an NPC shop for gold.
6. Enter the PvP zone for risk and reward.
7. Bet gold at card tables (stretch).

## Combat

- Runs on the 0.6 s combat tick (every 12th world tick at 20 Hz).
- **PvE:** NPCs in designated combat areas.
- **PvP:** only inside a clearly marked PvP zone. Players cannot attack each other anywhere else.
- Fully server-decided: hit chance, damage, and death.
- **TBD:** stats and formulas, NPC types and respawn times, attack range, health regeneration.

## Items

- **Equipment:** weapons and armor that change combat stats and appear on the character model.
- **Loot:** other items to collect or sell.
- Every item creation and transfer is written to the economy audit log ([[persistence]]).
- **TBD:** inventory size, item list for Phase 1, stacking rules.

## Gold

Sources in Phase 1:
- A small **starting amount** for new players.
- **NPC drops.**
- **Selling items** to an NPC shop.

Gold can be bet at card tables. Gold can never be bought, sold, or exchanged for real money (D-013).

**TBD:** starting amount, drop rates, shop prices, any gold sinks.

## Player trading

- Two players near each other open a trade window, each offers items and gold, and both confirm.
- The swap is saved as **one database transaction**: either everything moves or nothing does.
- Both inventories are re-checked on the server at the moment of commit; a trade fails if either side no longer has what it offered.
- Every trade is written to the economy audit log.
- **TBD:** confirmation flow (OSRS uses two confirmation screens to prevent scams), maximum trade distance, whether trading is allowed inside the PvP zone, any limits.

## Death in the PvP zone

- The player respawns at a safe spawn point.
- They **drop everything except a few items**, OSRS Wilderness style. The killer can loot the drop.
- Trades and drops must be handled as single transactions so items can never be duplicated.
- **TBD:** how many items are kept (OSRS keeps the 3 most valuable), whether loot is reserved for the killer for a short time before anyone can take it, how long drops stay on the ground.

## Death outside the PvP zone (PvE)

**TBD:** whether dying to NPCs costs anything.
