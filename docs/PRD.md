# Lush: Product Requirements Document

**Status:** Draft v0.9
**Owner:** Cleider
**Last updated:** 2026-10-22
**Related notes:** [[technologies]], [[Monitoring]], specs in `specs/`

> This PRD is the source of truth for humans and AI agents working on the project. Where it says **TBD**, the decision has not been made yet. Agents must not invent answers for TBD items; raise them as questions instead.

---

## 1. Summary

**Lush** (working title) is a browser-based, mobile-first MMO set on a faithful low-poly recreation of the UCI campus, in the visual style of Old School RuneScape. Students sign in, walk around campus, chat with each other, and (in later phases) join factions, fight in PvP zones, trade, and own homes.

The project has two goals:
1. A game UCI students actually want to play together.
2. A portfolio project that shows real-time networking and server engineering at scale, backed by measured numbers.

**TBD:** one-sentence pitch.

---

## 2. Goals and success metrics

Phase 1 is done when both of these are true:

1. **Features work end to end.** Every Phase 1 feature in 4.1 works on the live production server, played from a phone browser.
2. **Load test passes.** The bot load tester, running against staging in CI at a time when no testers are online, holds **1,000 bots at 20 Hz** with:
   - server tick p99 **under 25 ms** (half the 50 ms budget),
   - zero dropped frames and zero failed connections,
   - per-player download under 15 KB/s in the one-plaza siege scenario.

**Target date:** end of UCI Fall quarter, mid-December 2026.

---

## 3. Target players

- **Phase 1:** the owner and a small group of invited testers only. No real students yet.
- **Later phases:** UCI students, starting with a pilot group before a campus-wide launch.

---

## 4. Scope

### 4.1 Phase 1 (this PRD)

An internal test build, deployed and playable by invited testers:

- Walking around the campus map with the mobile controls in 5.1.
- **Guest accounts:** the browser stores a token, and the server creates a player for it.
- **Local chat:** overhead messages and a chat log.
- **Emotes and interactions:** wave, interact with nearby players and NPCs.
- **Basic combat** on the 0.6 s tick: fight simple NPCs in a combat area (PvE), and other players only inside a clearly marked PvP zone. Dying in the PvP zone drops everything except a few items.
- **Inventory and items:** equipment that changes combat stats and shows on the character, plus loot to collect or sell.
- **Gold:** a starting amount, NPC drops, and a simple NPC shop that buys items and sells food and starter gear (D-047).
- **Player trading:** two players open a trade window and swap items and gold, saved as one database transaction.
- **Card table area (stretch, first to cut):** players sit at a table and play card games against each other (poker / Texas Hold'em, simple card games in the style of Uno). See 5.4.
- Progress saved to Postgres, deployed to the production stack, with Sentry.
- **Map:** central campus only: Aldrich Park and the ring of buildings around it, generated from OpenStreetMap, with hand-made landmarks.

**Must-have vs. stretch.** Everything above is must-have except the card tables. If the mid-December date is at risk, card tables move to Phase 2 first.

### 4.2 Out of scope for Phase 1

Agents must not build these in Phase 1:

- Factions, territory control, and elections.
- Player housing. It is planned for Phase 2 and specified in [[housing]] (D-049).
- Texas Hold'em (only the first card game, and only if card tables happen).
- Native app-store apps. Phase 1 is the browser and the home-screen web app only.
- UCI email login (guest accounts only).
- Chat moderation tools (required before any public launch).
- WebTransport (WebSocket only).
- Multiple worlds or world hopping.
- Map areas beyond central campus.
- Any public launch to UCI students.

Never in any phase: buying, selling, or cashing out gold or items for real money (D-013).

---

## 5. Player experience

### 5.1 Controls (decided)

Vertical (portrait) mobile browser layout, based on the interactive prototype:

- **Camera:** third-person, high angle behind the player, OSRS style. Drag on the world to rotate.
- **Walking mode:** joystick at bottom-left; action buttons at bottom-right (Chat, Interact (target order in the gameplay spec), Wave, Run).
- **Chat mode:** tapping Chat replaces the controls area with an in-game keyboard and message field. Sending shows the message above the player's head and in the chat log, then returns to walking mode.
- **Desktop:** WASD or arrow keys to walk, Enter to open chat, physical keyboard for typing.

### 5.2 Visual style (decided)

- Low-poly, flat colors, no textures. Slim, OSRS-like character proportions.
- Optional retro post-process: low internal resolution, dithering, grain, vignette.

### 5.3 Core loop

1. A new player spawns on central campus with a **small starting amount of gold**.
2. They walk around, chat with others, and wave or interact.
3. In **combat areas** they fight NPCs, which drop **gold and items**.
4. **Equipment** (weapons, armor) changes combat stats and shows on the character. Other items are loot to collect or **sell to an NPC shop** for gold.
5. Braver players enter the **PvP zone**. Dying there drops **everything except a few items**, which the killer can loot.
6. Gold can be bet at the **card tables** (stretch).

Full rules: [[gameplay]] (`specs/gameplay.md`).

### 5.4 Card tables (stretch)

Players bet in-game gold at tables in a dedicated area. First game: an original Uno-style shedding game. Gold can never be bought or cashed out for real money (D-013). Full spec: [[card-tables]] (`specs/card-tables.md`).

---

## 6. Technical stack

See [[technologies]] for the full list and reasoning.

| Layer | Choice |
|---|---|
| Client | TypeScript, Three.js (pinned via npm), HTML, CSS, built with Vite |
| Game server | Go, single binary |
| Real-time transport | WebSocket (Phase 1); WebTransport later, with WebSocket fallback |
| Client hosting | Cloudflare Pages |
| Server hosting | Vultr Optimized Cloud Compute (CPU Optimized), 2 dedicated vCPU, 4 GB, Los Angeles; Caddy for HTTPS |
| Database | Vultr Managed PostgreSQL, Los Angeles, same private network as the server |
| Error tracking | Sentry (client and server, source maps uploaded on build). See [[Monitoring]] |
| CI/CD | GitHub Actions |

---

## 7. Architecture

A **modular monolith**: one Go server binary and one TypeScript client, each split into modules with strict dependency rules. Game logic never depends on the network, the database, or Three.js. The server runs one fixed 20 Hz tick; only the world loop changes world state.

| Spec | Covers |
|---|---|
| [[architecture]] (`specs/architecture.md`) | Repo layout, module boundaries, dependency rules, the `System` interface, tick and threading model |
| [[networking]] (`specs/networking.md`) | FlatBuffers protocol, interest management, prediction, reconnect, versioning |
| [[persistence]] (`specs/persistence.md`) | Postgres, migrations, save strategy, versioned saves, economy audit log |
| [[map]] (`specs/map.md`) | OpenStreetMap pipeline, chunks, shared collision, attribution |
| [[gameplay]] (`specs/gameplay.md`) | Core loop, combat, items, gold, PvP death rules |
| [[card-tables]] (`specs/card-tables.md`) | Table system and the first card game (stretch) |
| [[operations]] (`specs/operations.md`) | Deployment, environments, monitoring, restarts |

---

## 8. Performance budgets and evidence

Load test (Go test server, 2-vCPU shared container, server and bots on the same machine, 10 Hz tick):

| Scenario | Players | Tick p50 / p99 (budget 100 ms) | Server upload | Per-player download |
|---|---|---|---|---|
| Spread across campus | 1,000 | 1.6 / 8.8 ms | 0.9 MB/s | 0.9 KB/s |
| Siege, one plaza | 1,000 | 14.0 / 29.2 ms | 8.3 MB/s | 8.4 KB/s |
| Spread across campus | 2,000 | 4.7 / 22.2 ms | 3.5 MB/s | 1.7 KB/s |

No dropped frames or failed connections in any scenario.

**Phase 1 targets:**
- **Server:** see section 2 (1,000 bots at 20 Hz, tick p99 under 25 ms).
- **Device floor:** recent phones only, iPhone 13 or newer and comparable Android phones, in the current mobile Safari and Chrome.
- **Client frame rate:** steady **30 fps** on the device floor (frame-rate cap at 30 to save battery and heat).

---

## 9. Security, safety and moderation

- The server decides everything that matters: movement limits, combat, loot, money.
- Secrets in environment files on the server, never in git.
- OpenStreetMap attribution; no official UCI logos or mascot without permission.

- **Phase 1 login:** guest accounts (random token stored in the browser). UCI email login is required before any public launch.

- **Chat moderation:** none in Phase 1 (trusted testers only). A word filter, mute, block, report, and admin bans are **required before any public launch**.

---

## 10. Deployment and operations

Cloudflare Pages for the client, a Vultr VM in Los Angeles for the server (staging runs on the same VM on a separate port), deploys from GitHub Actions, Sentry on both sides. Full spec: [[operations]] (`specs/operations.md`).

---

## 11. Milestones

1. **Foundations:** monorepo, Vite + Three.js, Go server skeleton, protocol schema and codegen, CI.
2. **Walking together:** WebSocket, movement, prediction and interpolation, campus map with attribution, shared collision, reconnect, bot load tests.
3. **Saving progress:** player identity, Postgres, migrations, periodic saves, HTTP API skeleton.
4. **Going live:** domain, Cloudflare Pages, Vultr VM with Caddy and systemd, secrets, deploys from CI, graceful restart.
5. **Mobile polish:** PWA manifest, audio unlock, glTF pipeline.
6. **Playing together:** emotes and interactions, PvE and PvP combat, inventory and equipment, gold and NPC shop, player trading.
7. **Card tables (stretch).**

**Target:** all milestones complete by mid-December 2026 (end of Fall quarter). This is tight; see the must-have vs. stretch split in 4.1 for what gets cut first.

---

## 12. Decisions log

| ID | Decision | Reason |
|---|---|---|
| D-001 | Browser client in TypeScript + Three.js | Instant access from any phone; matches existing prototype |
| D-002 | Go game server | Proven by 1,000-bot test; simpler than Rust |
| D-003 | WebSocket first, WebTransport later with fallback | WebTransport not universal on older iPhones and UDP-blocking networks |
| D-004 | Modular monolith, one binary | Clean boundaries without running multiple services |
| D-005 | Server-authoritative simulation on a fixed tick | Fairness, simpler debugging, cheat resistance |
| D-006 | Vultr LA, dedicated CPU | Closest region to Irvine; avoids burstable CPU throttling |
| D-007 | Managed Postgres in the same region and private network | Low-latency saves, backups included |
| D-008 | Sentry for error tracking; Lantern integration optional | Sentry captures errors 24/7; Lantern can triage on top |
| D-009 | Phase 1 is internal: owner + invited testers | Validate tech and fun before real students |
| D-010 | Guest accounts in Phase 1; UCI email before public launch | Fastest path to persistence and reconnect |
| D-011 | PRD is a short main doc plus linked spec notes | Agents load only the specs relevant to their task |
| D-012 | Card tables bet in-game gold | More meaningful games; ties into the economy |
| D-013 | Gold is never purchasable or redeemable for real money | Keeps card games out of gambling law |
| D-014 | First card game is an original Uno-style shedding game | Simplest rules to prove the table system; avoids trademark issues |
| D-015 | 20 Hz movement tick, combat every 12th tick | Lower perceived latency; CPU headroom shown in load tests |
| D-016 | FlatBuffers for the shared protocol | Compact, fast, existing Go and TS code generation |
| D-017 | Phase 1 combat: PvE anywhere in combat areas, PvP only in a marked zone | Test both without making the whole campus dangerous |
| D-018 | Done = features work end to end + 1,000-bot load test passes in CI | Clear, measurable finish line |
| D-019 | Target end of Fall quarter, mid-December 2026 | Owner's deadline; scope will be cut by priority if needed |
| D-020 | Agents divided by module ~~with a lead agent~~ (lead replaced by the runner, D-038) | Matches the modular architecture; clear ownership |
| D-021 | Working title: Lush | Owner's choice |
| D-022 | Card tables are stretch scope, first to cut | Protects the mid-December date |
| D-023 | Phase 1 map is central campus (Aldrich Park and surrounding ring) | Enough to test everything; keeps map work small |
| D-024 | Device floor: iPhone 13+ and comparable Android | Phase 1 is internal testers; widen before public launch |
| D-025 | Gold sources: starting amount, NPC drops, NPC shop | Enough gold flow to test combat, shop, and card tables |
| D-026 | Items: equipment (stats + visible) and loot | Gives combat and loot a purpose |
| D-027 | PvP death drops everything except a few items | OSRS-style risk makes the PvP zone meaningful |
| D-028 | No chat moderation in Phase 1; full moderation required before public launch | Trusted testers only |
| D-029 | Simple player-to-player trading in Phase 1, one transaction per trade | Tests the economy's hardest correctness case early |
| D-030 | Out of scope: factions, housing, Hold'em, native apps (see 4.2) | Protects the mid-December date |
| D-031 | Client capped at 30 fps on iPhone 13+ | OSRS-paced game; saves battery and heat |
| D-032 | Staging on the production VM, separate port | Lower cost while only testers play; load tests run at quiet times |
| D-033 | Agents run unattended on an always-on Mac under a supervisor (now the runner, D-038) that handles usage limits | Work continues without the owner, including across limit resets |
| D-034 | Merges happen without human review, gated by CI, path guard, and a reviewer agent (the runner merges since D-038) | Owner wants zero manual calls; CI is the safety net |
| D-035 | Gate files (`.github/`, `scripts/ci/`, `.claude/`, `CONSTITUTION.md`, `CLAUDE.md`) are owner-only | Agents can't weaken the rules they're checked against |
| D-036 | Production released at most daily via gated `release-*` tags | Keeps testers' sessions stable; every gate re-checked by CI |
| D-037 | Discord webhook for updates; ~~up to 3 module agents in parallel~~ (2, see D-041) | Owner stays informed without polling |
| D-038 | A deterministic runner (Python, no AI) replaces the AI lead: it picks issues, moves labels, opens PRs, reads CI, merges, reverts and releases; agents only code, review and plan. State lives on GitHub (labels + one runner comment per issue). | Bookkeeping costs no tokens and can't be misjudged; modeled on Orbi and Sortie |
| D-039 | Agents run on a separate Claude Pro plan. The reviewer is the only agent on Opus (high effort); coders and the planner use Sonnet. On the Opus limit, reviews wait and coding continues. | Best review quality per token on Pro; nothing merges on a weaker review |
| D-040 | The reviewer fixes the blocker and major problems it finds; a fresh reviewer session re-reviews the fixes. At most 4 rounds, then the issue is parked for the owner. Merges are pinned to the reviewed commit. | Fewer round trips than sending work back; no one approves their own fixes |
| D-041 | Up to 2 coding agents in parallel, never two in the same area | Balances speed against Pro usage limits and merge conflicts |
| D-042 | Every issue follows one format (Goal, Context, Acceptance criteria, Out of scope, Blocked by); the runner won't start one that doesn't | Acceptance criteria are the contract the reviewer checks |
| D-043 | When fewer than 3 issues are in the pipeline, the planner drafts up to 10 from this PRD; the reviewer checks the batch and they start without owner approval. TBDs become question issues. | Work never stalls waiting for the owner, without inventing scope |
| D-044 | Movement speed: walk 4 m/s, run 7 m/s. The server's config is the only source of these numbers, and the server sends them to the client in Welcome so prediction always matches (until Welcome carries them, the client constant must equal the server default). Tuned after playtests. | Server authority (D-005); client and server speeds that differ cause rubber-banding |
| D-045 | Phase 1 gameplay is milestone 6 "Playing together" (emotes and interactions, combat, inventory and equipment, gold and the NPC shop, trading); card tables are milestone 7 (stretch) | M1 to M5 keep their meaning, and the planner files gameplay work in order (#97) |
| D-046 | Gameplay areas: spawn and shop at the centre of Aldrich Park, beginner NPCs in the rest of the park, tougher NPCs in the south fields, and the PvP zone north-east of the park outside Ring Road (positions in the map spec) | A safe start, danger a short walk away, and PvP far from new players (#100) |
| D-047 | Phase 1 gameplay numbers are OSRS-style placeholders kept in server data files (gameplay spec): one combat level 1 to 30, melee only, 28 inventory slots, 100 starting gold, six NPC types, a shop that also sells food and starter gear, PvP only within 5 levels, gold never dropped, a 2 s wave | A complete, playable loop now, tuned after playtests; selling at the shop gives gold a use (#96, #98, #99) |
| D-048 | Coders and the planner also run on Opus (amends D-039). While the bot is at its Opus limit they run on Sonnet, and reviews wait for Opus. Estimated-cost caps raised to $40 per run and $150 per issue. | The bot plan had usage to spare; stronger coders mean fewer review rounds and CI fixes, and nothing merges on a weaker review |
| D-049 | Player housing and decoration (Animal Crossing-style homes in replicas of UCI housing areas, kept by monthly gold rent) is planned for Phase 2 and specified in [[housing]]. It stays out of Phase 1. | Keeps Phase 1 on track for mid-December; housing needs the map to grow beyond central campus, and crafting and friends that Phase 1 doesn't have |

---

## 13. Open questions

Grouped by where the answer will live. Agents raise these as question issues (label `question`); they are not to be decided silently.

**Product**
- One-sentence pitch.

**Gameplay** ([[gameplay]])
- Every gameplay number is a placeholder to tune after playtests (D-047): the combat formulas and level curve, the NPC and item tables, shop prices, wave timing, trade distance, the PvP zone warning distance and the PvP death rules.
- Whether Phase 1 needs a bank. Without one, players carry everything they own, so a PvP death is costly (gold is never dropped for this reason).
- More attack styles (ranged, magic) and skills beyond combat. Not in Phase 1 unless decided here.

**Card tables** ([[card-tables]], stretch)
- Table size, turn timer, disconnect mid-game, house cut as a gold sink.

**Client and art** ([[architecture]])
- glTF model budgets per model type (placeholders from #82, enforced by the CI check in #86): `characters/` 1,500 triangles / 150 KB, `props/` 1,000 / 100 KB, `buildings/` 8,000 / 500 KB, anything else 3,000 / 200 KB. Tune after profiling on an iPhone 13.

**Housing** ([[housing]], Phase 2)
- Auction starting bid, and what happens if nobody bids.
- Home expansions later, and their cost.
- A home rating or leaderboard.
- The monthly gold fee, and whether it differs by area.
- Whether a player can sell or give their house to someone else.
- Which other housing areas are included, and how many houses each has.

**Later phases**
- Launch plan to avoid an empty world (clubs, residence halls, scheduled events).
- UCI email login, chat moderation, and wider device support before public launch.

---

## 14. Risks

- **Empty world:** not enough students online at once. Mitigation TBD (launch plan).
- **Single server is a single point of failure** in Phase 1. Needs a written recovery plan.
- **Bandwidth in crowded areas.** Mitigated later by delta snapshots and update priority.
- **Staging shares the production VM.** Load tests compete with live players for CPU and skew production metrics. Run them only when no testers are online; move staging to its own VM before public launch.
- **Item duplication.** Trades and PvP drops are the classic MMO dupe paths. Every transfer is one transaction and every change goes to the audit log.

---

## 15. Notes for agents

### 15.1 How work is divided

Agents work **by module**. A runner program (no AI) coordinates them.

| Agent | Model | Owns | Main folders |
|---|---|---|---|
| Runner (not an agent) | none | Picking work, labels, pull requests, CI, merges, reverts, releases | reads everything; writes only through git and GitHub |
| Reviewer | Opus | Reviewing every PR against its issue, this PRD and `CONSTITUTION.md`; fixing what it finds; checking planned issues | the PR's own folders |
| Planner | Sonnet | Drafting the next milestone's issues from this PRD | none (output only) |
| Protocol | Sonnet | FlatBuffers schema, code generation, versioning | `protocol/`, generated code |
| Server world | Sonnet | Simulation, tick loop, feature systems, interest management | `server/internal/world`, `systems`, `interest` |
| Server platform | Sonnet | Transport, sessions, storage, HTTP API, ops | `server/internal/transport`, `session`, `store`, `api`, `ops`, `db/migrations` |
| Client | Sonnet | Game state, networking, prediction, input, UI, audio | `client/src/core`, `net`, `state`, `input`, `ui`, `audio` |
| Client render | Sonnet | Three.js scene, characters, map rendering, effects | `client/src/render`, `shared/map` pipeline |
| Infra | Sonnet | Build, deployment, Caddy, systemd, Sentry, load tests | `Makefile`, `deploy/`, `tools/bots` (CI workflows are owner-only gate files) |

### 15.2 Orchestration

Agents run unattended on an always-on Mac with its own Claude Pro plan. Full setup: [[agent-setup/README|agent setup]] (`agent-setup/README.md`).

- The runner starts a coder for each ready issue (2 at a time, one per area) in its own git worktree, with a session ID it creates, so any run can be resumed.
- A coder's `done` counts only with evidence: commits on its branch, pushed. The runner then opens the PR.
- Red CI goes back to the coder's own session with the failing log. Green CI goes to the reviewer (Opus), which approves or fixes; a fresh reviewer session checks any fixes. The runner merges only the exact reviewed commit.
- Limits stop loops: 4 review rounds, 3 CI fixes, 3 attempts, the same failure 3 times in a row, a per-issue budget. Hitting one parks the issue (`needs-owner`) and posts to Discord.
- If `main` fails CI twice, merges freeze and the runner reverts the merge that broke it.
- Usage limits: the runner reads the reset time, pauses, and resumes each session. On the Opus limit only reviews wait.
- When fewer than 3 issues are in the pipeline, the planner drafts the next batch from this PRD and the reviewer checks it before it is created.
- Production releases: at most once a day, as a `release-*` tag, re-checked by the deploy workflow (CI green, real load test in the last 24 hours, nobody online).
- The owner is needed only for `question` issues, parked issues, and changes to gate files (`.github/`, `scripts/ci/`, `.claude/`, `CONSTITUTION.md`, `CLAUDE.md`).

### 15.3 Rules for every agent

- Read this PRD, `CONSTITUTION.md`, and the spec for your module before starting.
- Never invent answers to **TBD** items. Finish with status `needs_owner`; the runner turns it into a question issue for the owner.
- Follow the dependency rules in 7.1. Never import across a boundary the rules forbid.
- Only the Protocol agent changes the schema. Others request changes by finishing with status `blocked_by_issue`.
- Every decision that changes the design gets a row in the decisions log (section 12). The owner adds it when answering a question issue.
- New code ships with tests, and every acceptance criterion is proven by one. Server changes must keep the bot load test passing.
- Never add any path to buy, sell, or cash out gold for real money (D-013).
