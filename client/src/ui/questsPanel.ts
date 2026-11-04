import { QuestState } from '../protocol';
import type { QuestInfo, QuestStore } from '../state/quests';
import type { Hud } from './hud';
import { hudLayout, place } from './hudLayout';
import type { SkillsPanel } from './skillsPanel';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

export interface QuestsPanelOptions {
  parent: HTMLElement;
  quests: QuestStore;
  /** The inventory and Skills panels; the Quests tab is added to their tab rows and the three swap in the panel area. */
  inventory: Hud;
  skills: SkillsPanel;
  /** Current viewport size; defaults to the window's. */
  viewport?: () => { width: number; height: number };
  /** Called when the Quests panel opens, so other panels in its place can close. */
  onOpen?: () => void;
}

/** QuestSkill values (0 is combat) to display names. */
const QUEST_SKILL_NAMES = ['Combat', 'Woodcutting', 'Fishing', 'Cooking'];

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls: string, text = ''): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = cls;
  e.textContent = text;
  return e;
}

/** The requirement list for a quest, e.g. "Combat level 15, Overdue complete". */
export function requirementsText(q: QuestInfo, questName: (id: number) => string): string {
  return q.requirements.map((r) => (r.kind === 0
    ? `${QUEST_SKILL_NAMES[r.skill] ?? 'Skill'} level ${r.level}`
    : `${questName(r.questId)} complete`)).join(', ');
}

/**
 * The current step as a sentence. The step text carries the target ("Kill 5 coyotes."), so a count
 * from the quest log shows as "Kill 5 coyotes (3/5)".
 */
export function stepText(sentence: string, count: number): string {
  const total = /\d+/.exec(sentence);
  const base = sentence.replace(/\.$/, '');
  return total && Number(total[0]) > 1 ? `${base} (${count}/${total[0]})` : base;
}

/**
 * The Quests panel (docs/specs/quests.md, Quest log): a tab beside Inventory and Skills, with the quest points
 * total and every quest's state.
 */
export class QuestsPanel {
  readonly panel: HTMLElement;
  private readonly rows: HTMLElement;
  private readonly points: HTMLElement;
  private readonly motion: PanelMotion;
  private readonly unsubscribe: () => void;
  private readonly extraTabs: HTMLElement[] = [];
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: QuestsPanelOptions) {
    injectStyle();
    this.panel = el('div', 'lush-panel lush-quests');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Quests');
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    this.rows = el('div', 'lush-quests-rows');
    this.points = el('div', 'lush-quests-points');
    const head = el('div', 'lush-quests-head');
    const close = el('button', `lush-quests-close ${BUTTON_CLASS}`, 'Close');
    close.type = 'button';
    close.addEventListener('click', () => this.setOpen(false));
    head.append(el('span', 'lush-title lush-quests-title', 'Quests'), close);

    // Own tab row, plus a Quests tab on the other two panels' rows.
    const own = el('div', 'lush-tabs');
    own.setAttribute('role', 'tablist');
    own.append(this.tab('inventory', 'Inventory', false), this.tab('skills', 'Skills', false), this.tab('quests', 'Quests', true));
    this.panel.append(own, head, this.points, this.rows);
    for (const row of [opts.skills.inventoryTabs, opts.skills.panel.querySelector<HTMLElement>('.lush-tabs')]) {
      if (!row) continue;
      const t = this.tab('quests', 'Quests', false);
      row.append(t);
      this.extraTabs.push(t);
    }

    opts.parent.append(this.panel);
    this.unsubscribe = opts.quests.subscribe(() => this.render());
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  setOpen(open: boolean): void {
    if (open && !this.isOpen) this.opts.onOpen?.();
    this.motion.set(open);
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.panel, hudLayout(v.width, v.height).quests);
  }

  render(): void {
    const store = this.opts.quests;
    const nameOf = (id: number): string => store.quest(id)?.name ?? `Quest ${id}`;
    this.points.textContent = `Quest points: ${store.questPoints}`;
    this.rows.replaceChildren(...store.quests.map((q) => {
      const p = store.progressOf(q.id);
      const row = el('div', 'lush-quest');
      row.dataset.quest = String(q.id);
      let detail: string;
      if (p.state === QuestState.Complete) {
        row.dataset.state = 'complete';
        detail = 'Complete';
      } else if (p.state === QuestState.InProgress) {
        row.dataset.state = 'in-progress';
        detail = stepText(q.steps[p.step] ?? '', p.count);
      } else {
        row.dataset.state = 'not-started';
        const met = store.requirementsMet(q);
        row.classList.toggle('unmet', !met);
        const reqs = requirementsText(q, nameOf);
        detail = reqs ? `Requires: ${reqs}` : 'Not started';
      }
      row.append(el('span', 'lush-quest-name', q.name), el('span', 'lush-quest-detail', detail));
      return row;
    }));
  }

  dispose(): void {
    this.unsubscribe();
    this.motion.dispose();
    window.removeEventListener('resize', this.onResize);
    this.panel.remove();
    for (const t of this.extraTabs) t.remove();
  }

  private tab(name: 'inventory' | 'skills' | 'quests', label: string, current: boolean): HTMLButtonElement {
    const b = el('button', `lush-tab ${BUTTON_CLASS}`, label);
    b.type = 'button';
    b.dataset.tab = name;
    b.setAttribute('role', 'tab');
    b.setAttribute('aria-selected', String(current));
    if (current) b.classList.add('on');
    b.addEventListener('click', () => {
      if (current) return;
      if (name === 'quests') this.setOpen(true);
      else if (name === 'skills') this.opts.skills.setOpen(true);
      else this.opts.inventory.setOpen(true);
    });
    return b;
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-quests { box-sizing: border-box; z-index: 5; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: var(--lush-text-sm); overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-quests[hidden] { display: none; }
.lush-quests-head { display: flex; align-items: center; justify-content: space-between; gap: var(--lush-space-2); }
.lush-quests-title { font-size: var(--lush-text-lg); }
.lush-quests-points { font: var(--lush-text-md) var(--lush-font-title); font-variant: small-caps; color: var(--lush-accent); text-align: center; padding-bottom: var(--lush-space-2); border-bottom: var(--lush-border); }
.lush-quests-rows { display: flex; flex-direction: column; gap: var(--lush-space-1); }
.lush-quest { display: flex; flex-direction: column; gap: 2px; padding: var(--lush-space-2); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); }
.lush-quest.unmet { opacity: 0.5; }
.lush-quest-name { font: var(--lush-text-md) var(--lush-font-title); font-variant: small-caps; letter-spacing: 0.04em; color: var(--lush-text); }
.lush-quest[data-state="complete"] .lush-quest-name { color: var(--lush-gold-bright); }
.lush-quest-detail { color: var(--lush-text-muted); font-size: var(--lush-text-xs); }
`;
  document.head.append(s);
}
