# Trust and death

**Part of:** [[PRD]] · **Status:** Milestone 13 (D-055)

> Bodies and revival, the assassination item, and the private good/evil meter with its unlocks and halo. Builds on [[groups]] (milestone 12). Owners: Server world (rules), Server platform (storage), Protocol (messages), Client (meter, prompts, death screen), Client render (bodies, carrying, halo, effects).

**Every number on this page is a placeholder** to tune after playtests, kept in server data files ([[gameplay]]).

## Goals

- Every death is a story: someone has to come and get you.
- Players can choose to be corrupt or evil, with real rewards and real counterplay.
- Betrayal leaves clues, never proof, so groups have to investigate.

## Bodies and revival (every death)

This replaces the instant respawn in [[gameplay]] for **every** death: to NPCs, in the PvP zone, in sieges and heists, and by assassination.

- A dead player's **body stays where they fell**, visible to everyone, with their name. The PvP-zone drop rules still apply at the moment of death ([[gameplay]]): the body carries nothing.
- **Carrying:** any other player within 1 tile can pick up a body (Interact: "Carry *Name*"). While carrying, they walk at **2.5 m/s**, can't run, attack, eat, trade or open panels, and drop the body if they take **3 hits**. The body is shown on their shoulders, so everyone can see the rescue.
- **Revival points:** the safe spawn point, and two first-aid stations (one at the south fields edge nearest the park, one just outside the PvP zone border, [[map]]). Putting a body down at a revival point revives the player there with full HP and no penalty.
- **Waiting:** the dead player sees their body, can chat, and sees a timer. Once it runs out they may tap **Respawn alone**:
  - after **5 minutes** for a death to an NPC,
  - after **30 minutes** for a death to a player (PvP zone, siege, heist, assassination, murder).
- **Respawning alone** puts them at the spawn point with full HP and **Weakened** for 10 minutes: −10% accuracy, strength and defence. Nothing else is lost.
- A player who logs out while dead stays dead; the timer keeps running, and they respawn alone on their next login if it ran out.
- Bodies can't be attacked, looted or moved into the PvP zone. A body left in the PvP zone can be carried out.
- **Good players revive on the spot** (unlock below): 10 seconds of channeling next to the body, interrupted by any hit.

## The good/evil meter

- Every player has a meter from **−100 (evil) to +100 (good)**, starting at 0. **Only its owner can see it**, in the Skills panel.
- It moves only through the actions below, both ways, with no drift back to 0. Players can swing between good and evil at any time.
- Hunting, gathering, cooking, trading and fair fights are neutral, so normal play doesn't move it.

| Action | Change |
|---|---|
| Carry a body to a revival point | +5 |
| Revive someone on the spot | +5 |
| Complete a quest ([[quests]]) | +3 |
| Kill an NPC that is attacking another player | +1 (at most +5 a day) |
| Deposit gold in your group's vault | +1 per 500 gold (at most +5 a day) |
| Ruler sets the tax to 10% or less | +3 for the ruling group's leader, once a week |
| Kill a player in the PvP zone | −2 |
| Kill a player who is carrying a body | −10 |
| Successful heist on another group | −15 |
| Unlock your own group's door and an intruder gets through it within 5 minutes | −10 (the betrayer is never told it was counted) |
| Ruler sets the tax above 10% | −5 for the ruling group's leader, once a week |
| Use the assassination item, or murder (evil unlock) | −30 |

## Unlocks

Good and evil unlocks are built as counters to each other. An unlock works while the meter is at or past its threshold, and stops working when it falls back.

| Meter | Good unlock | Counters |
|---|---|---|
| +25 | **Read the door log** of your own group, at any rank | Betrayers' unlocked doors |
| +50 | **Sense poison:** see the residue on an assassinated body | The assassination item |
| +75 | **Revive on the spot** (above) | Bodies left to die |
| +90 | **Ward a building:** once a week, ward your group's hall for 24 hours, so the vault takes 90 s to crack and picked locks set off the alarm | Faster channeling, lock picking |

| Meter | Evil unlock | Counters |
|---|---|---|
| −25 | **Pick locks:** open a locked door after 20 s of channeling, without the alarm | Locked doors |
| −50 | **Disguise:** for 10 minutes your name and group are hidden, shown as "A stranger". Cooldown 1 hour. Attacking ends it | The door log, sense poison |
| −75 | **Faster vault cracking:** 40 s instead of 60 | Guarding the vault |
| −90 | **Murder:** kill one player outside the PvP zone and the safe circle, like the assassination item, once every 7 days | Wards, bodyguards |

## The halo

- A soft golden halo above the name of players at **+80 or more**, visible to everyone.
- The halo follows the meter **as it was 3 days ago**, so a fallen hero still looks trustworthy for a while, and a betrayer can earn a halo and then turn.

## The assassination item

- **Hemlock vial:** sold only by a hidden vendor (a hooded NPC who moves between three spots on campus every night at midnight Irvine time; see [[map]]). Price **25,000 gold**, one per buyer every **7 days**. It is hidden in the inventory: only its owner sees it, and it doesn't show in trades or the drop list (it is destroyed on death instead).
- **Use:** Interact on a player within 1 tile, outside the safe circle, then a 10 s poisoning during which the victim sees nothing. The victim then dies. The body follows the rules above, with the 30-minute timer.
- **Trace:** the body carries **poison residue** for 30 minutes. Players with Sense poison see it, and inspecting it shows the killer's group banner, **correct 70% of the time**, otherwise a random other group's. Clues, not proof.
- **Counter items:** a **Ward amulet** (3,000 gold at the hidden vendor or the Campus Market) blocks the next assassination or murder on its wearer and then breaks; the attacker's vial is used up. Bodyguards work by standing close: the poisoning needs the attacker within 1 tile for all 10 seconds.

## Caught betrayers

- Nothing marks a betrayer automatically. Groups find them from the door log, residue, and what they see, and deal with them by kicking and telling others.
- A kicked or departed member can't join another group for 24 hours ([[groups]]); after that they can rebuild trust like anyone.

## Client

- **Death screen:** the camera stays on the body, a "Waiting for someone to carry you" message, the respawn timer, and the Respawn alone button when allowed.
- **Meter** in the Skills panel: a bar from evil to good with a marker, and the unlocks with their thresholds (achieved ones lit).
- Carrying, reviving, residue and disguise all show on the interact prompt with progress rings ([[visual-style]]).

## Server

- The meter, its 3-day history (for the halo), unlock checks, bodies, carrying, the vial and wards are all decided on the server. The meter value is only ever sent to its owner.
- Every gold movement (vial, amulet) is one transaction in the economy audit log.
- The bot load test adds deaths and carrying.

## Out of scope for milestone 13

- Framing tools (planting fake residue) beyond the 70% residue accuracy.
- Public bounties, jails or trials.
- Showing anyone else's meter, ever.
