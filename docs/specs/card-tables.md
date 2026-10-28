# Card tables (stretch)

**Part of:** [[PRD]] · **Status:** Draft

> Shared table system and the first card game. First feature cut if Phase 1 runs late (D-022).

Players sit at a table in a dedicated area and play turn-based card games against each other.

- Server-authoritative: the server shuffles (cryptographically secure randomness), deals, and enforces rules.
- Hidden information stays hidden: a client is only ever sent its own cards and public table state.
- **Stakes:** players bet in-game gold from the main economy.
  - Each table has a fixed stake (placeholder amounts in server data), shown before a player sits. When a game starts, each seated player's stake moves into the pot in one database transaction, written to the economy audit log. The winner receives the whole pot in one transaction, also audited. A player who forfeits loses their stake to the pot. If the server stops mid-game, every stake is returned.
- **Hard rule:** gold can never be bought with real money, sold for real money, or exchanged for anything with real-money value. No feature may create such a path. This keeps the card tables legally a game, not gambling.
- **First game:** a simple shedding game in the style of Uno, under an original name and original card design (no Uno branding or artwork). Texas Hold'em comes after the shared table system is proven.
- Built as a shared table system (seats, turns, timers, dealing, hidden hands) with each game as a rules module on top.
- **TBD:** table size, turn timer length, what happens when a player disconnects mid-game, whether the house takes a small cut as a gold sink.
