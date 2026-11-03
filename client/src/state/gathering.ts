import skillsMap from '../../../shared/map/skills.json';
import { SkillResultCode, SkillTarget } from '../protocol';

/** Seconds per combat tick (docs/specs/skills.md: Gathering). */
export const COMBAT_TICK_S = 0.6;
/** Combat ticks between gathering attempts. */
export const ATTEMPT_TICKS = 4;
/** One gathering attempt in ms; the progress indicator fills over this. */
export const ATTEMPT_MS = ATTEMPT_TICKS * COMBAT_TICK_S * 1000;
/** The grill's id in the world view (matches render GRILL_ID). */
export const GRILL_RESOURCE_ID = 'grill';

/** skills.json id of a SkillAction target, or undefined when the index is unknown. */
export function resourceId(target: SkillTarget, targetId: number, candidateOf: (spotId: number) => number | undefined): string | undefined {
  if (target === SkillTarget.Grill) return GRILL_RESOURCE_ID;
  if (target === SkillTarget.Tree) return skillsMap.trees[targetId]?.id;
  const candidate = candidateOf(targetId);
  return candidate === undefined ? undefined : skillsMap.pond.spots[candidate]?.id;
}

/** The resource the local player is gathering from, and the progress of the current attempt. */
export class GatheringState {
  private pending?: { target: SkillTarget; targetId: number };
  private active?: { target: SkillTarget; targetId: number; id: string };
  private attemptStart = 0;

  /** Remember the SkillAction just sent; a following Started makes it active. */
  noteAction(target: SkillTarget, targetId: number): void {
    this.pending = { target, targetId };
  }

  /** Apply a SkillResult received at `now` (ms). */
  onResult(code: SkillResultCode, now: number, candidateOf: (spotId: number) => number | undefined): void {
    if (code === SkillResultCode.Started) {
      const p = this.pending;
      this.pending = undefined;
      if (!p) return;
      const id = resourceId(p.target, p.targetId, candidateOf);
      this.active = id === undefined ? undefined : { ...p, id };
      this.attemptStart = now;
    } else if (code === SkillResultCode.Success || code === SkillResultCode.Burnt) {
      this.attemptStart = now;
    } else {
      // Keep the pending action: switching targets mid-gather sends Stopped
      // for the old one just before Started for the new one.
      this.active = undefined;
    }
  }

  /** The local player started moving, or the world reset. */
  clear(): void {
    this.pending = undefined;
    this.active = undefined;
  }

  /** The skills.json id being gathered from, if any. */
  activeResource(candidateOf: (spotId: number) => number | undefined): string | undefined {
    const a = this.active;
    if (!a) return undefined;
    // A fishing spot moves: follow its current candidate.
    return a.target === SkillTarget.FishingSpot ? resourceId(a.target, a.targetId, candidateOf) ?? a.id : a.id;
  }

  /** Progress of the current attempt, 0 to 1; restarts every ATTEMPT_MS. */
  progress(now: number): number {
    if (!this.active) return 0;
    return ((Math.max(0, now - this.attemptStart)) % ATTEMPT_MS) / ATTEMPT_MS;
  }
}
