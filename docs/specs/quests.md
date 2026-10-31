# Quests and more content

**Part of:** [[PRD]] · **Status:** Milestone 11 (D-053)

> A quest system, three starter quests, talking NPCs, and more monsters and gear to fill levels 20 to 30. Owner: Server world (rules and data), Client (dialogue and quest UI), Client render (new NPC models), Protocol (new messages).

**Every number on this page is a placeholder** to tune after playtests, kept in server data files ([[gameplay]]). Where the places are is in [[map]] (Gameplay areas).

## Quest system

- Each quest is a data file: an id, name, short description, requirements (levels, other quests), a list of steps, and rewards. Adding a quest needs no new code unless it needs a new kind of step.
- **Step kinds:** talk to an NPC; bring items to an NPC (they are taken); kill an NPC type (a count); reach a level in a skill; get a quest item (from a drop or a ground spawn).
- **Quest state** per player: not started, in progress (with the current step and any counts), or complete. Saved with the player ([[persistence]]).
- **Quest items** are only given to a player on that step, can't be traded or dropped, aren't kept on death outside the quest, and are removed when the quest completes. A player who loses one gets it again from the same source.
- **Rewards:** quest points, gold, items, and XP in a named skill (combat or one of the skills in [[skills]]). Gold and items go through the economy audit log.
- **Quest points:** the total shows in the quest log. Nothing requires them yet.
- Completing a quest shows a completion scroll with the rewards, and a chat message nearby players can see ("Cleider has completed Overdue!").

## Talking NPCs

- New non-combat NPCs stand at fixed spots and can't be attacked.
- **Interact** on one opens a dialogue: a few lines of text, then up to 4 reply buttons. Dialogue is data, like quests, and can depend on quest state.
- Add "talk to the nearest talking NPC within 2 tiles" to the Interact order in [[gameplay]], after the shop stall and the bank booth ([[skills]]).
- A small marker above a talking NPC shows when they have a quest to start (!) or to finish (?).

| NPC | Where | Role |
|---|---|---|
| Librarian | Outside Langson Library | Starts and finishes Overdue |
| Park ranger | Edge of the south fields nearest the park | Starts and finishes Coyote Trouble |
| Food truck cook | Next to the grill near spawn | Starts and finishes Fish Fry |
| Guide | At the safe spawn point | Explains the controls, the shop, the bank and the PvP zone to new players. No quest. |

## Starter quests

### Overdue (beginner)
1. Talk to the Librarian: a crow stole a library book.
2. Kill crows in Aldrich Park until one drops the **Overdue book** (1 in 4 while on this step).
3. Bring it back to the Librarian.

**Rewards:** 1 quest point, 150 gold, 500 combat XP, a **Library card** (loot, shop buys for 0, a keepsake).

### Coyote Trouble (requires combat level 15)
1. Talk to the Park ranger: coyotes are attacking students.
2. Kill 5 coyotes.
3. Kill the coyote alpha. It always drops the **Alpha's fang** while you're on this step.
4. Bring the fang to the Park ranger.

**Rewards:** 2 quest points, 400 gold, 2,000 combat XP, the **Ranger's machete** (weapon, 20 / 22 / 0, speed 4, combat level 18 to equip, shop buys for 120).

### Fish Fry (requires Fishing 15 and Cooking 15)
1. Talk to the Food truck cook: the lunch rush is coming and the fish delivery didn't show.
2. Bring 5 cooked trout.

**Rewards:** 1 quest point, 250 gold, 1,500 Cooking XP, 1,000 Fishing XP, the **Chef's hat** (head armor, 0 / 0 / 2, no level to equip, shop buys for 5).

## Quest log

- A **Quests** tab next to the Skills panel ([[skills]]) lists every quest with its state: not started (with requirements, greyed out if not met), in progress (with the current step in a sentence), or complete.
- Quest points total at the top.

## More monsters

To fill the gap between level 20 and the level cap of 30. Same rules and formulas as [[gameplay]].

| NPC | Area | Population | Level | HP | Accuracy / strength / defence | Attack speed | Behaviour | Respawn | Drops |
|---|---|---|---|---|---|---|---|---|---|
| Goose | Aldrich Park, by the pond | 6 | 6 | 12 | 8 / 6 / 4 | 4 | Aggressive | 30 s | 4–12 gold; Feather (always); Goose egg (1 in 10) |
| Runaway e-scooter | South fields | 4 | 24 | 60 | 32 / 30 / 34 | 5 | Aggressive | 90 s | 50–120 gold; Lithium cell (1 in 3); Steel sword (1 in 40) |
| Night watchman bot | South fields (far end) | 1 | 30 | 90 | 40 / 38 / 40 | 4 | Aggressive | 5 min | 100–250 gold; 2 Lithium cells; Lab coat (1 in 6); Watchman's visor (1 in 12) |

The Goose is the one aggressive NPC in Aldrich Park, kept by the pond, outside the safe circle.

## More items

| Item | Kind | Slot | Accuracy / strength / defence | Attack speed | Level to equip | Shop buys for | Shop sells for | Comes from |
|---|---|---|---|---|---|---|---|---|
| Steel sword | Weapon | Weapon | 24 / 26 / 0 | 4 | 20 | 200 | — | E-scooter drop |
| Lab coat | Armor | Body | 0 / 0 / 20 | — | 22 | 250 | — | Watchman bot drop |
| Watchman's visor | Armor | Head | 2 / 0 / 9 | — | 25 | 220 | — | Watchman bot drop |
| Steel shield | Armor | Shield | 0 / 0 / 10 | — | 15 | 60 | 160 | Shop |
| Feather | Loot | — | — | — | — | 1 | — | Goose drop |
| Goose egg | Loot | — | — | — | — | 30 | — | Goose drop |
| Lithium cell | Loot | — | — | — | — | 20 | — | E-scooter and watchman bot drops |

## Out of scope for milestone 11

- Quests longer than 4 steps, branching choices with lasting effects, or cutscenes.
- Voice lines.
- Quest-point requirements for anything.
- Repeatable or daily quests.
