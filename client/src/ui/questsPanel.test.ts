// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { QuestRequirementKind, QuestSkill, QuestState } from '../protocol';
import { ItemState } from '../state/items';
import { QuestStore, type QuestInfo } from '../state/quests';
import { SkillState } from '../state/skills';
import { Hud } from './hud';
import { QuestsPanel, stepText } from './questsPanel';
import { SkillsPanel } from './skillsPanel';
import { applyTheme } from './theme';

const VP = { width: 390, height: 844 };
const NO_REWARDS = { questPoints: 1, gold: 0, items: [], xp: [] };

const QUESTS: QuestInfo[] = [
  { id: 1, name: 'Overdue', description: '', requirements: [], steps: ['Talk to the librarian.'], rewards: NO_REWARDS, startNpc: 1, finishNpc: 1 },
  {
    id: 2, name: 'Coyote Trouble', description: '', steps: ['Talk to the ranger.', 'Kill 5 coyotes.'], rewards: NO_REWARDS, startNpc: 2, finishNpc: 2,
    requirements: [{ kind: QuestRequirementKind.Level, skill: QuestSkill.Combat, level: 15, questId: 0 }],
  },
  {
    id: 3, name: 'Fish Fry', description: '', steps: ['Bring trout.'], rewards: NO_REWARDS, startNpc: 3, finishNpc: 3,
    requirements: [{ kind: QuestRequirementKind.Quest, skill: QuestSkill.Combat, level: 0, questId: 1 }],
  },
];

let disposers: (() => void)[] = [];

function setup(levelOf = () => 1) {
  const items = new ItemState();
  const store = new QuestStore(levelOf);
  store.setCatalog(QUESTS);
  const hud = new Hud({ parent: document.body, items, viewport: () => VP });
  const skills = new SkillsPanel({ parent: document.body, skills: new SkillState(), items, inventory: hud, viewport: () => VP });
  const panel = new QuestsPanel({ parent: document.body, quests: store, inventory: hud, skills, viewport: () => VP, onOpen: () => skills.setOpen(false) });
  disposers.push(() => panel.dispose(), () => skills.dispose(), () => hud.dispose());
  return { store, hud, skills, panel };
}

function row(panel: QuestsPanel, id: number) {
  const r = panel.panel.querySelector<HTMLElement>(`.lush-quest[data-quest="${id}"]`);
  if (!r) throw new Error(`no quest ${id}`);
  return { el: r, state: r.dataset.state, detail: r.querySelector('.lush-quest-detail')?.textContent ?? '' };
}

beforeEach(() => {
  document.body.replaceChildren();
  applyTheme();
});
afterEach(() => {
  for (const d of disposers) d();
  disposers = [];
});

describe('Quests tab and panel (#655)', () => {
  it('AC1: a Quests tab beside Skills opens the panel, and Close shuts it', () => {
    const { skills, panel } = setup();
    skills.setOpen(true);
    const tabs = [...skills.panel.querySelectorAll<HTMLButtonElement>('.lush-tabs button')];
    expect(tabs.map((t) => t.textContent)).toEqual(['Inventory', 'Skills', 'Quests']);
    expect(panel.isOpen).toBe(false);
    tabs[2].click();
    expect(panel.isOpen).toBe(true);
    expect(skills.isOpen).toBe(false);
    const own = [...panel.panel.querySelectorAll<HTMLButtonElement>('.lush-tabs button')];
    expect(own.map((t) => t.getAttribute('aria-selected'))).toEqual(['false', 'false', 'true']);
    own[1].click();
    expect(skills.isOpen).toBe(true);
    panel.setOpen(true);
    panel.panel.querySelector<HTMLButtonElement>('.lush-quests-close')?.click();
    expect(panel.isOpen).toBe(false);
  });

  it('AC2: shows not started (greyed when unmet), in progress with the step and count, and complete', () => {
    const { store, panel } = setup();
    store.setLog([
      { questId: 1, state: QuestState.Complete, step: 0, count: 0 },
      { questId: 2, state: QuestState.InProgress, step: 1, count: 3 },
    ], 1);
    expect(row(panel, 1)).toMatchObject({ state: 'complete', detail: 'Complete' });
    expect(row(panel, 2)).toMatchObject({ state: 'in-progress', detail: 'Kill 5 coyotes (3/5)' });
    // Fish Fry needs Overdue, which is complete: requirements met, not greyed.
    expect(row(panel, 3).state).toBe('not-started');
    expect(row(panel, 3).detail).toBe('Requires: Overdue complete');
    expect(row(panel, 3).el.classList.contains('unmet')).toBe(false);

    store.setLog([], 0);
    expect(row(panel, 3).el.classList.contains('unmet')).toBe(true);
    expect(row(panel, 2)).toMatchObject({ state: 'not-started', detail: 'Requires: Combat level 15' });
    expect(row(panel, 2).el.classList.contains('unmet')).toBe(true);
    expect(row(panel, 1)).toMatchObject({ state: 'not-started', detail: 'Not started' });
    expect(row(panel, 1).el.classList.contains('unmet')).toBe(false);
  });

  it('AC2: requirements met by level are not greyed', () => {
    const { panel } = setup(() => 20);
    expect(row(panel, 2).el.classList.contains('unmet')).toBe(false);
  });

  it('AC3: the quest points total shows at the top and updates with the QuestLog', () => {
    const { store, panel } = setup();
    const top = (): string | undefined => panel.panel.querySelector('.lush-quests-points')?.textContent ?? undefined;
    expect(top()).toBe('Quest points: 0');
    store.setLog([{ questId: 1, state: QuestState.Complete, step: 0, count: 0 }], 3);
    expect(top()).toBe('Quest points: 3');
    expect(panel.panel.children[1].classList.contains('lush-quests-head') || panel.panel.children[2] === panel.panel.querySelector('.lush-quests-points')).toBe(true);
  });

  it('stepText adds a count only when the step names a number', () => {
    expect(stepText('Talk to the librarian.', 0)).toBe('Talk to the librarian');
    expect(stepText('Kill 5 coyotes.', 0)).toBe('Kill 5 coyotes (0/5)');
  });
});
