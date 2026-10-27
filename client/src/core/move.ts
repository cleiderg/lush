import { encodeInput } from '../net/codec';
import { ActionFlags } from '../protocol';
import type { GameState } from '../state/world';

/** Predicts one movement input and sends it, with the Run flag set when running (D-044). */
export function sendMove(
  game: GameState,
  session: { send(bytes: Uint8Array): void },
  moveX: number,
  moveZ: number,
  dt: number,
  running: boolean,
): void {
  const input = game.applyMove(moveX, moveZ, dt, running);
  session.send(encodeInput(input.seq, moveX, moveZ, game.heading, running ? ActionFlags.Run : 0));
}
