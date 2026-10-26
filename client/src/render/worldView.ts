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

export interface WorldView {
  readonly local: PlayerView;
  readonly remotes: readonly PlayerView[];
  /** Hits that arrived since the last frame, oldest first. Missing means none. */
  readonly hits?: readonly HitView[];
}
