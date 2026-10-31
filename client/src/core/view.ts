import type { GameState } from '../state/world';
import { toWorldMove, type Vec2 } from '../input/joystick';
import type { ChatBubbleView, NpcView, PlayerView, WorldView } from '../render/worldView';
import { EntityKind } from '../protocol';
import { sunPosition } from './sun';

/**
 * Snapshot of game state for the renderer: predicted local player, interpolated remotes and
 * NPCs, entity state (equipment and health) from game.items, ground items, and the hits received since the previous view. An entity is an NPC when
 * its EntityInfo kind is Npc; players and entities with no EntityInfo yet stay in remotes.
 * Drains game.hits, so each hit appears in exactly one view. Chat holds the current overhead
 * message of the local player and of each remote player in view; other speakers are left out.
 * The sky is the sun's position at the shared (server) time, or the debug override.
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
  const local = game.local;
  const speakers = new Set([local.id, ...remotes.map((r) => r.id)]);
  const chat: ChatBubbleView[] = game.chat
    .activeOverheads(now)
    .filter((o) => speakers.has(o.speakerId))
    .map((o) => ({ id: o.speakerId, text: o.text }));
  return { local, remotes, npcs, entities: game.items, groundItems: game.items.groundItems, hits, chat, sky: sunPosition(game.clock.now()) };
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
