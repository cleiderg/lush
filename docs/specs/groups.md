# Groups and conquest

**Part of:** [[PRD]] · **Status:** Milestone 12 (D-054)

> Player groups, group halls with doors and a vault, the heist, and the weekly siege for the throne that decides who rules the world. Owners: Server world (rules), Server platform (storage), Protocol (messages), Client (group panels, prompts), Client render (halls, doors, throne room, banners).

**Every number on this page is a placeholder** to tune after playtests, kept in server data files ([[gameplay]]). Trust, betrayal and the good/evil meter that sits on top of this are in [[trust-and-death]] (milestone 13).

## Goals

- Give groups a recurring reason to log in: the weekly fight for rule.
- Make trust a resource: who holds keys, who gets promoted, who is let into the vault room.
- Let betrayal happen through normal tools (keys, doors, intel), leaving clues but not proof.

## Groups

- Any player can found a group for **500 gold** (a gold sink), with a name of 3 to 20 letters, numbers and spaces, checked against a blocked-word list, and a banner colour from 12 presets.
- **Ranks:** Leader, Officer, Member, Recruit. New members join as Recruits.

| Permission | Leader | Officer | Member | Recruit |
|---|---|---|---|---|
| Invite players | ✓ | ✓ | | |
| Promote and demote (below own rank) | ✓ | ✓ | | |
| Kick (below own rank) | ✓ | ✓ | | |
| Keys: lock and unlock hall doors | ✓ | ✓ | set by leader | |
| Enter the vault room | ✓ | ✓ | set by leader | |
| Withdraw from the group vault | ✓ | set by leader | | |
| Read the door log | ✓ | ✓ | | |
| Claim or give up a hall, declare for the siege | ✓ | | | |

- The leader can switch "keys", "vault room" and "withdraw" on or off for Members and Officers where the table says "set by leader".
- At most **30 members** per group. A player is in one group at a time.
- **Leaving:** a player who leaves or is kicked can't join another group for **24 hours**.
- **Leader handover:** the leader can pass leadership to an Officer. If the leader hasn't logged in for 14 days, the longest-serving Officer becomes leader.
- **Group chat:** a channel in the chat log only members see.
- **Group name and banner** show under the player's name, so everyone can see who belongs to which group.
- Disbanding returns the vault gold to the leader, and gives up the hall.

## Group halls

- **Six campus buildings on the ring around Aldrich Park** are group halls (agents pick six non-landmark buildings from the OSM footprints and list them in the map data). A group claims an empty hall for **2,000 gold** and keeps it with **500 gold a week** of upkeep, paid from the group vault every Monday at 00:00 Irvine time. A group that can't pay loses the hall.
- A group holds at most one hall.
- Every hall has the same simple layout inside (built with the interior and cut-away view from [[visual-style]]): an **entrance hall** behind 1 or 2 outer doors, a **main room**, and a **vault room** behind an inner door, with the **vault** in it.
- The group's banner hangs outside, so everyone knows who owns it.
- Halls are not safe areas: anyone may walk in through an open door. Combat inside a hall is allowed only against intruders (players not in the owning group) who are past a closed door, and only by the owning group's members, or by intruders defending themselves.

## Doors

- Three states: **open**, **closed**, **locked**. Anyone can open or close an unlocked door. Only members with the key permission can lock or unlock.
- **A closed but unlocked door looks exactly like a locked one** until someone tries it. That is what makes an inside job work.
- **Forcing a locked door:** anyone can attack it. A door has 300 HP (placeholder), uses the hit formulas in [[gameplay]] with defence 40, and breaks open after taking enough damage. The first hit on a door sets off the hall's **alarm**: every online member of the owning group gets a message and a sound, and the door shows a "being forced" effect to everyone nearby. A broken door repairs itself after 10 minutes, or straight away if a member with keys repairs it for 50 gold.
- **Door log:** every lock, unlock, open, close and break is logged with the player and time, kept for 7 days. Leaders and Officers can read it. It records *who unlocked a door*, never who walked through it afterwards: suspicion, not proof.

## Group vault

- A gold store owned by the group. Any member can deposit; withdrawals follow the permissions above. Every deposit and withdrawal is one database transaction and goes to the economy audit log ([[persistence]]).
- Members deposit at the vault itself (inside the vault room) or from the group panel while in the hall.

## The heist

1. An intruder gets into the vault room: through a door a betrayer left unlocked, a forced door, or (milestone 13) a picked lock.
2. They tap Interact on the vault and **channel for 60 seconds**. Starting to channel sets off the hall's alarm.
3. **Each hit taken removes 10 seconds** of progress. Moving more than 1 tile from the vault, or being stunned, resets it to zero.
4. On success the intruder chooses **one** reward:

| Reward | Effect |
|---|---|
| **Gold** | 10% of the vault's gold, at most 2,000, moved to the intruder in one transaction |
| **Sabotage** | The group's gates start the next siege with 25% less HP |
| **Intel** | The group's member list with ranks, and its door log for the last 24 hours |
| **Backdoor** | A hidden key to one of the hall's outer doors, valid for 24 hours, that the intruder can give to one other player |

- A hall can be heisted at most **once every 24 hours**. After a successful heist the vault shows "recently robbed" to its members.
- The vault can never be emptied by heists: gold rewards are capped, and the cap is per heist.

## The weekly siege and the throne

- **The throne** is in **Aldrich Hall** (already a landmark next to the park, [[map]]): a throne room inside with two **gates** (one main, one side) between the entrance and the throne.
- **Siege time:** every **Sunday from 7:00 to 8:00 pm Irvine time**. Groups that want to fight **declare** by Saturday midnight, for 1,000 gold. Only members of declared groups and the ruling group can enter Aldrich Hall during the siege.
- During the siege Aldrich Hall is a combat zone between members of different groups. Siege deaths drop nothing (no item loss), but follow the body rules in [[trust-and-death]] once milestone 13 lands.
- **Capturing:** a player stands on the throne dais and channels for **30 seconds**; any hit resets it. A capture makes their group the **holder**, and the holder's banner shows on the throne. The gates belong to the holder and have 2,000 HP each; attackers must break one to reach the throne room. When the holder changes, the gates reset to full HP for the new holder.
- **Winning:** the group holding the throne at 8:00 pm rules the world until the next siege ends.
- **Ruling gets harder:** for every consecutive week a group has ruled, its gates start the siege with **20% less HP** (down to 20%), and its members take **5% more damage** in the siege (up to 25%).
- No declared groups: the ruler keeps the throne for another week (and the "harder" counter still climbs).

## Ruling the world

The ruling group's leader opens a **Rule** panel at the throne, and chooses for the week:

| Setting | Choices |
|---|---|
| **World music** | One of 6 background tracks for everyone outside interiors |
| **Tax** | 0% to 20% on what non-ruling players get from selling to the NPC shop; the tax goes to the ruler's group vault. Over 10% counts as over-taxing ([[trust-and-death]]) |
| **Banner** | The ruler's banner flies over Aldrich Park and the spawn point |
| **Proclamation** | One of 12 fixed proclamations shown to every player on login ("Long live the rulers!", "Trade freely this week", and so on). Fixed texts because there is no chat moderation yet (PRD 9) |
| **Group tax** | 0% to 10% of every other group's weekly upkeep, paid to the ruler's vault |

The settings take effect at 8:00 pm Sunday and last a week. The ruling group's name shows in every player's location panel ("Ruled by …").

## Client

- **Group panel** (a tab next to the quest log): members with rank and online status, the hall, the vault balance, invite and rank buttons, the door log (for those allowed), and the declare-for-siege button.
- **Prompts:** "Lock door", "Unlock door", "Force door", "Crack vault (60 s)", "Claim the throne (30 s)" in the interact prompt ([[visual-style]]), with a progress ring while channeling.
- **Alarm:** a red banner at the top of the screen and a sound for members when their hall's alarm goes off.
- **Siege countdown** on the location panel from Saturday, and a siege scoreboard (holder, gate HP, time left) during it.

## Server

- Groups, ranks, halls, doors, the door log, the vault, heists, the siege and rule settings are saved in Postgres; every gold movement is one transaction in the economy audit log.
- The siege schedule and upkeep run on Irvine time (America/Los_Angeles), like the world clock.
- The bot load test adds groups: bots that join groups, open and close doors, and fight a siege at 1,000 bots.

## Out of scope for milestone 12

- Elections, alliances between groups, wars outside the siege.
- Player-written group descriptions or proclamations (needs chat moderation, PRD 9).
- More than one hall per group, or building new halls.
- Mercenary contracts. Paying someone is just a trade ([[gameplay]]).
