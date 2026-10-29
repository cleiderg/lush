# Card tables (stretch)

**Part of:** [[PRD]] · **Status:** Draft

> Shared table system and the first card game. First feature cut if Phase 1 runs late (D-022).

Players sit at a table in a dedicated area and play turn-based card games against each other.

- Server-authoritative: the server shuffles (cryptographically secure randomness), deals, and enforces rules.
- Hidden information stays hidden: a client is only ever sent its own cards and public table state.
- **Stakes:** players bet in-game gold from the main economy.
  - Each table has a fixed stake (placeholder amounts in server data), shown before a player sits. Placeholder stakes: tables 1 to 4 stake 10, 25, 50 and 100 gold. Sitting and pressing Ready need at least the stake in carried gold; at game start a seated player without it is unseated before any stake moves. When a game starts, each seated player's stake moves into the pot in one database transaction, written to the economy audit log. The winner receives the whole pot in one transaction, also audited. A player who forfeits loses their stake to the pot. If the server stops mid-game, every stake is returned.
- **Hard rule:** gold can never be bought with real money, sold for real money, or exchanged for anything with real-money value. No feature may create such a path. This keeps the card tables legally a game, not gambling.
- **First game:** a simple shedding game in the style of Uno, under an original name and original card design (no Uno branding or artwork). Texas Hold'em comes after the shared table system is proven.
- Built as a shared table system (seats, turns, timers, dealing, hidden hands) with each game as a rules module on top.
- **Table rules (placeholders, tune after playtests):** 2 to 4 seats. A game starts when at least 2 players are seated and every seated player has pressed Ready; nobody can sit down while a game is in progress. Turn timer 20 s. When a player's turn times out, or they are disconnected, the game's timeout action runs (in the first game: draw one card and the turn passes); after 3 missed turns in a row, or if they leave mid-game, they forfeit and their seat is freed. No house cut in Phase 1. A player opens a table with Interact within 2 tiles of it; the panel shows the stake before the Sit button. A disconnected player who is removed from the world (after the logout time) leaves their table: mid-game this is a forfeit.

## First game: Shed (working name)

- Deck: 4 colours (campus-themed names and art), each with one 0, two each of 1 to 9, two Skip, two Reverse and two Draw Two; plus 4 Wild and 4 Wild Draw Four. Original names and art, no Uno branding.
- Colours (placeholders): Aldrich Green, Anteater Blue, Golden Hour Yellow, Brick Red. Phase 1 cards are flat-colour with text symbols (0–9, Skip, Reverse, +2, Wild, Wild +4); no image art.
- Deal 7 cards to each player; turn the top card of the draw pile face up to start the discard pile. If the first face-up card is a Wild or Wild Draw Four, it goes back into the draw pile and another card is turned; a starting Skip, Reverse or Draw Two has no effect. A player who must draw when no card is left, even after reshuffling, draws nothing and the turn passes.
- On your turn, play a card that matches the top card's colour or symbol, or a Wild (choose the next colour). If you can't or don't play, draw one card and the turn passes.
- Skip skips the next player; Reverse reverses the direction (with 2 players it acts as Skip); Draw Two and Wild Draw Four make the next player draw 2 or 4 and lose their turn.
- When the draw pile runs out, shuffle the discard pile (except the top card) into a new draw pile.
- The first player to empty their hand wins the pot. No penalty for not calling the last card.
