import type { GameState } from '../state/world';
import { toWorldMove, type Vec2 } from '../input/joystick';

/** Snapshot of game state for the renderer: predicted local player plus interpolated remotes. */
export function buildWorldView(game: GameState, now: number) {
  return { local: game.local, remotes: game.remotes(now) };
}

/**
 * The camera rig's yaw is its position angle around the player (it looks along (sin yaw, cos yaw)),
 * while toWorldMove expects yaw 0 to look north (-z). Converts between the two.
 */
export function controlYaw(rigYaw: number): number {
  return Math.PI - rigYaw;
}

/** World move for a screen-space stick vector given the camera rig's yaw. */
export function moveForRigYaw(stick: Vec2, rigYaw: number): Vec2 {
  return toWorldMove(stick, controlYaw(rigYaw));
}
