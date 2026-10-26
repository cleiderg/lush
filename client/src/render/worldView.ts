/** Read-only snapshot of what the renderer needs. src/state provides it; render never mutates it. */
export interface PlayerView {
  readonly id: number;
  readonly x: number;
  readonly z: number;
  /** Facing in radians around +Y; 0 faces +Z. */
  readonly heading: number;
  /** Protocol Animation id; missing means Idle. */
  readonly animation?: number;
}

/** One combat hit on an entity. Damage 0 is a miss. */
export interface HitView {
  readonly target: number;
  readonly damage: number;
  readonly died: boolean;
}

/** An item lying on the ground. Positions are metres from the map origin. */
export interface GroundItemView {
  readonly id: number;
  readonly itemId: number;
  readonly quantity: number;
  readonly x: number;
  readonly z: number;
}

/** An NPC in view. `npcType` is the NPC type id (protocol EntityInfo.npc_type). */
export interface NpcView {
  readonly id: number;
  readonly npcType: number;
  readonly x: number;
  readonly z: number;
  /** Facing in radians around +Y; 0 faces +Z. */
  readonly heading: number;
}

export interface WorldView {
  readonly local: PlayerView;
  readonly remotes: readonly PlayerView[];
  /** Hits that arrived since the last frame, oldest first. Missing means none. */
  readonly hits?: readonly HitView[];
  /** Items on the ground in view; missing means none are drawn and no marker mesh is added. */
  readonly groundItems?: readonly GroundItemView[];
  /** NPCs in view; missing means none are drawn and no NPC mesh is added. */
  readonly npcs?: readonly NpcView[];
}
