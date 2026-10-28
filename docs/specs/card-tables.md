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
- **TBD:** table size, turn timer length, what happens when a player disconnects mid-game, whether the house takes a small cut as a gold sink.

## First game: Shed (working name)

- Deck: 4 colours (campus-themed names and art), each with one 0, two each of 1 to 9, two Skip, two Reverse and two Draw Two; plus 4 Wild and 4 Wild Draw Four. Original names and art, no Uno branding.
- Deal 7 cards to each player; turn the top card of the draw pile face up to start the discard pile.
- On your turn, play a card that matches the top card's colour or symbol, or a Wild (choose the next colour). If you can't or don't play, draw one card and the turn passes.
- Skip skips the next player; Reverse reverses the direction (with 2 players it acts as Skip); Draw Two and Wild Draw Four make the next player draw 2 or 4 and lose their turn.
- When the draw pile runs out, shuffle the discard pile (except the top card) into a new draw pile.
- The first player to empty their hand wins the pot. No penalty for not calling the last card.
