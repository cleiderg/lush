import { QuestSkill, QuestState } from '../protocol';
import type { ItemState } from '../state/items';
import type { QuestRewardsInfo, QuestStore } from '../state/quests';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

export interface QuestScrollOptions {
  parent: HTMLElement;
  quests: QuestStore;
  items: ItemState;
}

const SKILL_NAMES: Record<number, string> = {
  [QuestSkill.Combat]: 'Combat',
  [QuestSkill.Woodcutting]: 'Woodcutting',
  [QuestSkill.Fishing]: 'Fishing',
  [QuestSkill.Cooking]: 'Cooking',
};

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls: string, text = ''): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = cls;
  e.textContent = text;
  return e;
}

/**
 * The quest completion scroll: the quest name and its rewards, closed with a button.
 * QuestCompleted is also sent for other players, and the client doesn't know its own name, so the
 * scroll opens only when the quest also goes from in progress to complete in the player's own quest
 * log; the log update may arrive just before or just after the QuestCompleted. A quest the player
 * finished earlier never changes again, so another player finishing it doesn't open the scroll.
 */
export class QuestScroll {
  readonly panel: HTMLElement;
  private readonly title: HTMLElement;
  private readonly list: HTMLElement;
  private readonly motion: PanelMotion;
  private readonly unsubscribe: () => void;
  private seenSeq: number;
  /** The latest QuestCompleted not yet matched to a completion in the player's own log. */
  private pending: { questId: number; questName: string; rewards: QuestRewardsInfo } | null = null;
  /** Each quest's state in the log at the last sync. */
  private states = new Map<number, QuestState>();
  /** Quests the player's own log moved from in progress to complete, not yet matched to a QuestCompleted. */
  private readonly finished = new Set<number>();

  constructor(private readonly opts: QuestScrollOptions) {
    injectStyle();
    this.seenSeq = opts.quests.completedSeq;
    this.states = new Map(opts.quests.log.map((p) => [p.questId, p.state]));
    this.panel = el('div', 'lush-panel lush-quest-scroll');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Quest complete');
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    this.title = el('div', 'lush-title lush-quest-scroll-title');
    this.list = el('ul', 'lush-quest-scroll-rewards');
    const close = el('button', `lush-quest-scroll-close ${BUTTON_CLASS}`, 'Close');
    close.type = 'button';
    close.addEventListener('click', () => this.close());
    this.panel.append(el('div', 'lush-quest-scroll-head', 'Quest complete!'), this.title, this.list, close);
    opts.parent.append(this.panel);
    this.unsubscribe = opts.quests.subscribe(() => this.sync());
  }

  get isOpen(): boolean {
    return this.motion.isShown;
  }

  close(): void {
    this.motion.set(false);
  }

  destroy(): void {
    this.unsubscribe();
    this.panel.remove();
  }

  private sync(): void {
    const { quests } = this.opts;
    const states = new Map(quests.log.map((p) => [p.questId, p.state]));
    for (const [id, state] of states) {
      if (state === QuestState.Complete && this.states.get(id) === QuestState.InProgress) this.finished.add(id);
    }
    this.states = states;
    if (quests.completedSeq !== this.seenSeq && quests.completed) {
      this.seenSeq = quests.completedSeq;
      const { questId, questName, rewards } = quests.completed;
      this.pending = { questId, questName, rewards };
    }
    const c = this.pending;
    if (!c || !this.finished.has(c.questId)) return;
    this.finished.delete(c.questId);
    this.pending = null;
    this.show(c.questName, c.rewards);
  }

  private show(name: string, r: QuestRewardsInfo): void {
    this.title.textContent = name;
    const lines: [string, string][] = [];
    if (r.questPoints > 0) lines.push(['points', `${r.questPoints} quest point${r.questPoints === 1 ? '' : 's'}`]);
    if (r.gold > 0) lines.push(['gold', `${r.gold} gold`]);
    for (const i of r.items) {
      const n = this.opts.items.item(i.itemId)?.name ?? `Item ${i.itemId}`;
      lines.push(['item', i.count > 1 ? `${i.count} x ${n}` : n]);
    }
    for (const x of r.xp) lines.push(['xp', `${x.xp} ${SKILL_NAMES[x.skill] ?? 'Skill'} XP`]);
    this.list.replaceChildren(...lines.map(([kind, text]) => el('li', `lush-quest-scroll-line lush-quest-scroll-${kind}`, text)));
    this.motion.set(true);
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-quest-scroll { position: fixed; left: 50%; top: 50%; transform: translate(-50%, -50%); box-sizing: border-box; width: min(320px, 92vw); max-height: 80vh; overflow-y: auto; z-index: 10; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-3); text-align: center; font-size: var(--lush-text-sm); }
.lush-quest-scroll[hidden] { display: none; }
.lush-quest-scroll-head { color: var(--lush-text-muted); font: var(--lush-text-xs) var(--lush-font-title); font-variant: small-caps; letter-spacing: 0.08em; }
.lush-quest-scroll-title { font-size: var(--lush-text-lg); }
.lush-quest-scroll-rewards { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--lush-space-1); }
.lush-quest-scroll-line { padding: var(--lush-space-1); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: rgba(0, 0, 0, 0.25); }
.lush-quest-scroll-gold { color: var(--lush-gold); }
.lush-quest-scroll-close { min-height: 44px; font-size: var(--lush-text-md); }
`;
  document.head.append(s);
}
