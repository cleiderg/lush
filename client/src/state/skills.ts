import { Skill } from '../protocol';
import type { FishingSpotKindInfo, FishingSpotPosition, LevelUpEvent, SkillResultEvent, SkillValue, TreeKindInfo } from '../net/codec';

export type { FishingSpotKindInfo, TreeKindInfo, FishingSpotPosition, LevelUpEvent, SkillResultEvent, SkillValue };

/** Highest skill level (docs/specs/skills.md: Levels and XP). */
export const SKILL_MAX_LEVEL = 30;
/** The skills in Skill enum order. */
export const SKILLS: readonly Skill[] = [Skill.Woodcutting, Skill.Fishing, Skill.Cooking];

/** XP_TABLE[l] is the XP at which level l starts (OSRS experience table), as in the server's formula.XPForLevel. */
export const XP_TABLE: readonly number[] = (() => {
  const t = [0, 0];
  let points = 0;
  for (let l = 1; l < SKILL_MAX_LEVEL; l++) {
    points += Math.floor(l + 300 * 2 ** (l / 7));
    t.push(Math.floor(points / 4));
  }
  return t;
})();

/** XP still needed to reach level + 1 from xp; 0 at SKILL_MAX_LEVEL. */
export function xpToNextLevel(level: number, xp: number): number {
  if (level >= SKILL_MAX_LEVEL) return 0;
  const next = XP_TABLE[Math.max(level, 1) + 1];
  // XP comes in tenths; round so 12.3 doesn't show as 12.299999.
  return Math.max(0, Math.round((next - xp) * 10) / 10);
}

/** Most events kept when nothing drains a queue (for example a hidden tab). */
export const MAX_SKILL_EVENTS = 64;

/** Events read once by whoever drains them (chat log, level-up effect). */
class EventQueue<T> {
  private items: T[] = [];

  get length(): number {
    return this.items.length;
  }

  push(item: T): void {
    this.items.push({ ...item });
    if (this.items.length > MAX_SKILL_EVENTS) this.items.splice(0, this.items.length - MAX_SKILL_EVENTS);
  }

  /** Returns every queued event, oldest first, and empties the queue. */
  drain(): T[] {
    const out = this.items;
    this.items = [];
    return out;
  }

  clear(): void {
    this.items = [];
  }
}

/**
 * The local player's skills, the shared resource state (felled trees, fishing
 * spot positions) and skill events, from SkillStats, ResourceState, LevelUp
 * and SkillResult.
 */
export class SkillState {
  private values = new Map<Skill, SkillValue>(SKILLS.map((s) => [s, { skill: s, level: 1, xp: 0 }]));
  private felled = new Set<number>();
  private spots = new Map<number, number>();
  private treeKinds = new Map<string, TreeKindInfo>();
  private spotKinds = new Map<string, FishingSpotKindInfo>();
  readonly levelUps = new EventQueue<LevelUpEvent>();
  readonly results = new EventQueue<SkillResultEvent>();
  private listeners = new Set<() => void>();

  /** Calls fn after every change to levels or XP; returns an unsubscribe function. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  /** Apply a SkillStats message. */
  setStats(skills: SkillValue[]): void {
    for (const v of skills) this.values.set(v.skill, { ...v });
    for (const fn of this.listeners) fn();
  }

  level(skill: Skill): number {
    return this.values.get(skill)?.level ?? 1;
  }

  /** XP in points (may have a half point). */
  xp(skill: Skill): number {
    return this.values.get(skill)?.xp ?? 0;
  }

  xpToNext(skill: Skill): number {
    return xpToNextLevel(this.level(skill), this.xp(skill));
  }

  /** Combat level plus the three skill levels (4 to 120). */
  totalLevel(combatLevel: number): number {
    return SKILLS.reduce((sum, s) => sum + this.level(s), combatLevel);
  }

  /** Apply a SkillCatalog message; it replaces the old catalog. */
  setCatalog(trees: TreeKindInfo[], fishingSpots: FishingSpotKindInfo[]): void {
    this.treeKinds = new Map(trees.map((t) => [t.kind, { ...t }]));
    this.spotKinds = new Map(fishingSpots.map((f) => [f.kind, { ...f }]));
  }

  /** The tree kind by its shared/map/skills.json name ("tree", "oak", "eucalyptus"). */
  treeKind(kind: string): TreeKindInfo | undefined {
    return this.treeKinds.get(kind);
  }

  /** The fishing spot kind by its shared/map/skills.json name ("net", "rod"). */
  fishingSpotKind(kind: string): FishingSpotKindInfo | undefined {
    return this.spotKinds.get(kind);
  }

  /** Apply a ResourceState message; it replaces the whole state. */
  setResources(felledTrees: number[], fishingSpots: FishingSpotPosition[]): void {
    this.felled = new Set(felledTrees);
    this.spots = new Map(fishingSpots.map((f) => [f.id, f.candidate]));
  }

  isFelled(treeId: number): boolean {
    return this.felled.has(treeId);
  }

  felledTrees(): number[] {
    return [...this.felled];
  }

  /** Current candidate index of a fishing spot, or undefined if the server hasn't sent it. */
  fishingSpotCandidate(spotId: number): number | undefined {
    return this.spots.get(spotId);
  }

  fishingSpots(): FishingSpotPosition[] {
    return [...this.spots].map(([id, candidate]) => ({ id, candidate }));
  }

  /** Clear everything, for example on a new Welcome. */
  reset(): void {
    this.setStats(SKILLS.map((s) => ({ skill: s, level: 1, xp: 0 })));
    this.setResources([], []);
    this.levelUps.clear();
    this.results.clear();
  }
}
