import type { QuestInfo, QuestProgressState, QuestRewardsInfo } from '../net/codec';
import { QuestRequirementKind, QuestResultCode, QuestState } from '../protocol';

export type { QuestInfo, QuestProgressState, QuestRewardsInfo };

/** A marker shown above a talking NPC: a quest to start (!) or to finish (?). */
export type QuestMarker = 'start' | 'finish';

/** The open dialogue: what the NPC says and the reply buttons (none: show a continue button). */
export interface DialogueState {
  npcId: number;
  lines: string[];
  replies: string[];
}

/** The chat line for a QuestResult code, or null for Ok. */
export function questResultText(code: QuestResultCode): string | null {
  switch (code) {
    case QuestResultCode.Ok: return null;
    case QuestResultCode.TooFar: return 'You are too far away to talk';
    case QuestResultCode.NotTalkable: return 'They have nothing to say to you';
    case QuestResultCode.RequirementsNotMet: return "You don't meet the requirements for that quest yet";
    case QuestResultCode.InventoryFull: return 'Your inventory is full';
    default: return 'Something went wrong';
  }
}

/** The chat line everyone nearby sees when a player finishes a quest. */
export function questCompletedText(playerName: string, questName: string): string {
  return `${playerName} has completed ${questName}!`;
}

/**
 * Quests: the catalog, the player's quest log, the open dialogue and the
 * quest completion scroll to show. Subscribers are told after any change.
 */
export class QuestStore {
  private catalog = new Map<number, QuestInfo>();
  private progress = new Map<number, QuestProgressState>();
  private listeners = new Set<() => void>();
  /** Quest points total from the last QuestLog. */
  questPoints = 0;
  /** The open dialogue, or null. */
  dialogue: DialogueState | null = null;
  /**
   * The latest QuestCompleted, for the completion scroll; null before any. The client doesn't know its
   * own name, so the UI shows the scroll only when the quest is complete in its own log.
   */
  completed: { playerName: string; questId: number; questName: string; rewards: QuestRewardsInfo } | null = null;

  /**
   * levelOf(skill) is the player's level in a QuestSkill (0 is combat level); used for quest
   * requirements. Defaults to level 1.
   */
  constructor(private levelOf: (questSkill: number) => number = () => 1) {}

  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private notify(): void {
    for (const fn of this.listeners) fn();
  }

  /** Every quest in catalog order. */
  get quests(): QuestInfo[] {
    return [...this.catalog.values()];
  }

  quest(id: number): QuestInfo | undefined {
    return this.catalog.get(id);
  }

  /** The player's progress in a quest; not started when the log doesn't list it. */
  progressOf(id: number): QuestProgressState {
    return this.progress.get(id) ?? { questId: id, state: QuestState.NotStarted, step: 0, count: 0 };
  }

  setCatalog(quests: QuestInfo[]): void {
    this.catalog = new Map(quests.map((q) => [q.id, q]));
    this.notify();
  }

  setLog(quests: QuestProgressState[], questPoints: number): void {
    this.progress = new Map(quests.map((p) => [p.questId, { ...p }]));
    this.questPoints = questPoints;
    this.notify();
  }

  setDialogue(d: DialogueState): void {
    this.dialogue = { npcId: d.npcId, lines: [...d.lines], replies: [...d.replies] };
    this.notify();
  }

  closeDialogue(): void {
    if (!this.dialogue) return;
    this.dialogue = null;
    this.notify();
  }

  setCompleted(playerName: string, questId: number, questName: string, rewards: QuestRewardsInfo): void {
    this.completed = { playerName, questId, questName, rewards };
    this.notify();
  }

  /** Whether the player meets every requirement to start quest q. */
  requirementsMet(q: QuestInfo): boolean {
    return q.requirements.every((r) =>
      r.kind === QuestRequirementKind.Quest
        ? this.progressOf(r.questId).state === QuestState.Complete
        : this.levelOf(r.skill) >= r.level);
  }

  /**
   * The marker for a talking NPC type: start when a quest it starts is not started and its
   * requirements are met; finish when the player is on the last step of a quest it finishes.
   */
  markerFor(npcType: number): QuestMarker | null {
    let finish = false;
    for (const q of this.catalog.values()) {
      const p = this.progressOf(q.id);
      if (q.startNpc === npcType && p.state === QuestState.NotStarted && this.requirementsMet(q)) return 'start';
      if (q.finishNpc === npcType && p.state === QuestState.InProgress && p.step >= q.steps.length - 1) finish = true;
    }
    return finish ? 'finish' : null;
  }

  /** Forgets everything; used on a new Welcome. */
  clear(): void {
    this.catalog.clear();
    this.progress.clear();
    this.questPoints = 0;
    this.dialogue = null;
    this.completed = null;
    this.notify();
  }
}
