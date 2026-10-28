# Card tables (stretch)

**Part of:** [[PRD]] · **Status:** Draft

> Shared table system and the first card game. First feature cut if Phase 1 runs late (D-022).

Players sit at a table in a dedicated area and play turn-based card games against each other.

- Server-authoritative: the server shuffles (cryptographically secure randomness), deals, and enforces rules.
- Hidden information stays hidden: a client is only ever sent its own cards and public table state.
- **Stakes:** players bet in-game gold from the main economy.
- **Hard rule:** gold can never be bought with real money, sold for real money, or exchanged for anything with real-money value. No feature may create such a path. This keeps the card tables legally a game, not gambling.
- **First game:** a simple shedding game in the style of Uno, under an original name and original card design (no Uno branding or artwork). Texas Hold'em comes after the shared table system is proven.
- Built as a shared table system (seats, turns, timers, dealing, hidden hands) with each game as a rules module on top.
- **Table rules (placeholders, tune after playtests):** 2 to 4 seats. A game starts when at least 2 players are seated and every seated player has pressed Ready; nobody can sit down while a game is in progress. Turn timer 20 s. When a player's turn times out, or they are disconnected, the game's timeout action runs (in the first game: draw one card and the turn passes); after 3 missed turns in a row, or if they leave mid-game, they forfeit and their seat is freed. No house cut in Phase 1.
