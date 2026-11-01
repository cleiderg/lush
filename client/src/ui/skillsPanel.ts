import { Skill } from '../protocol';
import type { ItemState } from '../state/items';
import { SKILL_MAX_LEVEL, SKILLS, type SkillState } from '../state/skills';
import type { Hud } from './hud';
import { hudLayout, place } from './hudLayout';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

export interface SkillsPanelOptions {
  parent: HTMLElement;
  skills: SkillState;
  /** For the combat level, which counts toward the total level. */
  items: ItemState;
  /** The inventory; its panel gets the same Inventory / Skills tabs, and the two swap in the panel area. */
  inventory: Hud;
  /** Current viewport size; defaults to the window's. */
  viewport?: () => { width: number; height: number };
  /** Called when the Skills panel opens, so other panels in its place can close. */
  onOpen?: () => void;
}

export const SKILL_NAMES: Readonly<Record<Skill, string>> = {
  [Skill.Woodcutting]: 'Woodcutting',
  [Skill.Fishing]: 'Fishing',
  [Skill.Cooking]: 'Cooking',
};

/** Whole points for display: XP comes in tenths, and a part point isn't shown (as in OSRS). */
export function wholeXp(xp: number): number {
  return Math.floor(xp + 1e-9);
}

/** XP to the next level in whole points, rounded up so it never shows 0 before the level is reached. */
export function wholeXpToNext(xpToNext: number): number {
  return Math.ceil(xpToNext - 1e-9);
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls: string, text = ''): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = cls;
  e.textContent = text;
  return e;
}

const fmt = (n: number): string => n.toLocaleString('en-US');

/**
 * The Skills panel (docs/specs/skills.md, Client): a tab beside the inventory in the panel area, with each skill's
 * level, XP and XP to the next level, and the total level (combat level plus the skill levels).
 */
export class SkillsPanel {
  readonly panel: HTMLElement;
  /** The tab row inside the inventory panel; its Skills tab opens this panel. */
  readonly inventoryTabs: HTMLElement;
  private readonly rows: HTMLElement;
  private readonly total: HTMLElement;
  private readonly motion: PanelMotion;
  private readonly unsubscribe: (() => void)[];
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: SkillsPanelOptions) {
    injectStyle();
    this.panel = el('div', 'lush-panel lush-skills');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Skills');
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    this.rows = el('div', 'lush-skills-rows');
    this.total = el('div', 'lush-skills-total');
    this.panel.append(this.tabs('skills'), el('div', 'lush-title lush-skills-title', 'Skills'), this.rows, this.total);

    this.inventoryTabs = this.tabs('inventory');
    opts.inventory.panel.prepend(this.inventoryTabs);

    opts.parent.append(this.panel);
    this.unsubscribe = [opts.skills.subscribe(() => this.render()), opts.items.subscribe(() => this.render())];
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
    place(this.panel, hudLayout(v.width, v.height).skills);
  }

  render(): void {
    const s = this.opts.skills;
    this.rows.replaceChildren(...SKILLS.map((skill) => {
      const level = s.level(skill);
      const row = el('div', 'lush-skill');
      row.dataset.skill = SKILL_NAMES[skill].toLowerCase();
      const next = level >= SKILL_MAX_LEVEL ? 'Max level' : `${fmt(wholeXpToNext(s.xpToNext(skill)))} to level ${level + 1}`;
      row.append(
        el('span', 'lush-skill-name', SKILL_NAMES[skill]),
        el('span', 'lush-skill-level', `${level}/${SKILL_MAX_LEVEL}`),
        el('span', 'lush-skill-xp', `${fmt(wholeXp(s.xp(skill)))} XP`),
        el('span', 'lush-skill-next', next),
      );
      return row;
    }));
    this.total.textContent = `Total level ${s.totalLevel(this.opts.items.stats.combatLevel)}`;
  }

  dispose(): void {
    for (const u of this.unsubscribe) u();
    this.motion.dispose();
    window.removeEventListener('resize', this.onResize);
    this.panel.remove();
    this.inventoryTabs.remove();
  }

  /** The Inventory / Skills tab row, with `current` marked; the other tab swaps the panels. */
  private tabs(current: 'inventory' | 'skills'): HTMLElement {
    const row = el('div', 'lush-tabs');
    row.setAttribute('role', 'tablist');
    for (const [name, label] of [['inventory', 'Inventory'], ['skills', 'Skills']] as const) {
      const b = el('button', `lush-tab ${BUTTON_CLASS}`, label);
      b.type = 'button';
      b.dataset.tab = name;
      b.setAttribute('role', 'tab');
      b.setAttribute('aria-selected', String(name === current));
      if (name === current) b.classList.add('on');
      b.addEventListener('click', () => {
        if (name === current) return;
        if (name === 'skills') this.setOpen(true);
        else this.opts.inventory.setOpen(true);
      });
      row.append(b);
    }
    return row;
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-skills { box-sizing: border-box; z-index: 5; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: var(--lush-text-sm); overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-skills[hidden] { display: none; }
.lush-skills-title { font-size: var(--lush-text-lg); }
.lush-tabs { display: flex; gap: var(--lush-space-1); }
.lush-panel .lush-tab { flex: 1; background: var(--lush-surface); color: var(--lush-text-muted); border-color: var(--lush-gold-dim); }
.lush-panel .lush-tab.on { background: var(--lush-surface-pressed); color: var(--lush-gold-bright); border-color: var(--lush-gold); }
.lush-skills-rows { display: flex; flex-direction: column; gap: var(--lush-space-1); }
.lush-skill { display: grid; grid-template-columns: 1fr auto; gap: 2px var(--lush-space-2); padding: var(--lush-space-2); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); }
.lush-skill-name { font: var(--lush-text-md) var(--lush-font-title); font-variant: small-caps; letter-spacing: 0.04em; color: var(--lush-text); }
.lush-skill-level { justify-self: end; font-weight: 700; color: var(--lush-gold-bright); }
.lush-skill-xp, .lush-skill-next { color: var(--lush-text-muted); font-size: var(--lush-text-xs); }
.lush-skill-next { justify-self: end; }
.lush-skills-total { margin-top: auto; padding-top: var(--lush-space-2); border-top: var(--lush-border); font: var(--lush-text-md) var(--lush-font-title); font-variant: small-caps; color: var(--lush-accent); text-align: center; }
`;
  document.head.append(s);
}
