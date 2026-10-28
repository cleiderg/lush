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

/** Item ids worn per slot; 0 (EmptyItem.Id) when empty. */
export interface EquipmentView {
  readonly weapon: number;
  readonly head: number;
  readonly body: number;
  readonly shield: number;
}

/** Per-entity data from EntityInfo / EntityHealth, keyed by entity id. state's ItemState fits this shape. */
export interface EntityLookup {
  entityInfo(id: number): { readonly equipment: EquipmentView } | undefined;
  /** 0..1; 1 (full) when unknown. */
  healthRatio(id: number): number;
  /** Catalog entry, used to pick an item's colour. */
  item?(id: number): { readonly name: string } | undefined;
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

/** A speaker's current overhead message. `id` is a player's entity id; the text is plain, never markup. */
export interface ChatBubbleView {
  readonly id: number;
  readonly text: string;
}

export interface WorldView {
  readonly local: PlayerView;
  readonly remotes: readonly PlayerView[];
  /** Equipment and health; without it characters are drawn bare and without health bars. */
  readonly entities?: EntityLookup;
  /** Hits that arrived since the last frame, oldest first. Missing means none. */
  readonly hits?: readonly HitView[];
  /** Items on the ground in view; missing means none are drawn and no marker mesh is added. */
  readonly groundItems?: readonly GroundItemView[];
  /** NPCs in view; missing means none are drawn and no NPC mesh is added. */
  readonly npcs?: readonly NpcView[];
  /** Current overhead chat, at most one per speaker; missing or empty means no text is drawn. */
  readonly chat?: readonly ChatBubbleView[];
}
