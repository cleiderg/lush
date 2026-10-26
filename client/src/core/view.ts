import type { GameState } from '../state/world';
import { toWorldMove, type Vec2 } from '../input/joystick';
import type { WorldView } from '../render/worldView';

/**
 * Snapshot of game state for the renderer: predicted local player, interpolated remotes,
 * ground items, and the hits received since the previous view. Drains game.hits, so each
 * hit appears in exactly one view.
 */
export function buildWorldView(game: GameState, now: number): WorldView {
  const hits = game.hits.drain().flatMap((b) => b.hits.map((h) => ({ target: h.target, damage: h.damage, died: h.died })));
  return { local: game.local, remotes: game.remotes(now), groundItems: game.items.groundItems, hits };
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
