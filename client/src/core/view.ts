import type { GameState } from '../state/world';
import { toWorldMove, type Vec2 } from '../input/joystick';
import type { NpcView, PlayerView, WorldView } from '../render/worldView';
import { EntityKind } from '../protocol';

/**
 * Snapshot of game state for the renderer: predicted local player, interpolated remotes and
 * NPCs, ground items, and the hits received since the previous view. An entity is an NPC when
 * its EntityInfo kind is Npc; players and entities with no EntityInfo yet stay in remotes.
 * Drains game.hits, so each hit appears in exactly one view.
 */
export function buildWorldView(game: GameState, now: number): WorldView {
  const hits = game.hits.drain().flatMap((b) => b.hits.map((h) => ({ target: h.target, damage: h.damage, died: h.died })));
  const remotes: PlayerView[] = [];
  const npcs: NpcView[] = [];
  for (const r of game.remotes(now)) {
    const info = game.items.entityInfo(r.id);
    if (info?.kind === EntityKind.Npc) npcs.push({ id: r.id, npcType: info.npcType, x: r.x, z: r.z, heading: r.heading });
    else remotes.push(r);
  }
  return { local: game.local, remotes, npcs, groundItems: game.items.groundItems, hits };
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
