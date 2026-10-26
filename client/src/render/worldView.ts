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

export interface WorldView {
  readonly local: PlayerView;
  readonly remotes: readonly PlayerView[];
  /** Equipment and health; without it characters are drawn bare and without health bars. */
  readonly entities?: EntityLookup;
}
