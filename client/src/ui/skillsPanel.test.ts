// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { Skill } from '../protocol';
import { ItemState } from '../state/items';
import { SkillState } from '../state/skills';
import { GameState } from '../state/world';
import { Hud } from './hud';
import { hudLayout } from './hudLayout';
import { SkillsPanel, wholeXp, wholeXpToNext } from './skillsPanel';
import { applyTheme, BUTTON_CLASS, MIN_TAP_PX } from './theme';

const VP = { width: 390, height: 844 };

let disposers: (() => void)[] = [];

function setup(skills = new SkillState(), items = new ItemState()) {
  const hud = new Hud({ parent: document.body, items, viewport: () => VP, onOpen: () => panel.setOpen(false) });
  const panel: SkillsPanel = new SkillsPanel({
    parent: document.body, skills, items, inventory: hud, viewport: () => VP, onOpen: () => hud.setOpen(false),
  });
  disposers.push(() => panel.dispose(), () => hud.dispose());
  return { hud, panel, skills, items };
}

function row(panel: SkillsPanel, name: string) {
  const r = panel.panel.querySelector<HTMLElement>(`.lush-skill[data-skill="${name}"]`);
  if (!r) throw new Error(`no ${name} row`);
  const text = (cls: string): string => r.querySelector(`.lush-skill-${cls}`)?.textContent ?? '';
  return { name: text('name'), level: text('level'), xp: text('xp'), next: text('next') };
}

beforeEach(() => {
  document.body.replaceChildren();
  applyTheme();
});
afterEach(() => {
  for (const d of disposers) d();
  disposers = [];
});

describe('Skills tab and panel (#466 AC1)', () => {
  it('a Skills tab sits beside the Inventory tab and opens the panel in the inventory panel place', () => {
    const { hud, panel } = setup();
    hud.setOpen(true);
    const tabs = [...hud.panel.querySelectorAll<HTMLButtonElement>('.lush-tabs button')];
    expect(tabs.map((t) => t.textContent)).toEqual(['Inventory', 'Skills']);
    expect(tabs[0].getAttribute('aria-selected')).toBe('true');
    expect(tabs[0].parentElement).toBe(tabs[1].parentElement);
    expect(panel.isOpen).toBe(false);

    tabs[1].click();
    expect(panel.isOpen).toBe(true);
    expect(hud.isOpen).toBe(false);
    const l = hudLayout(VP.width, VP.height);
    expect(l.skills).toEqual(l.panel);
    expect(panel.panel.style.top).toBe(`${l.panel.y}px`);
    expect(panel.panel.style.height).toBe(`${l.panel.h}px`);

    // The panel has the same tabs, with Skills selected; Inventory swaps back.
    const own = [...panel.panel.querySelectorAll<HTMLButtonElement>('.lush-tabs button')];
    expect(own.map((t) => [t.textContent, t.getAttribute('aria-selected')])).toEqual([['Inventory', 'false'], ['Skills', 'true']]);
    own[0].click();
    expect(hud.isOpen).toBe(true);
    expect(panel.isOpen).toBe(false);
  });

  it('closes from its own Close button, leaving the inventory closed', () => {
    const { hud, panel } = setup();
    panel.setOpen(true);
    panel.panel.querySelector<HTMLButtonElement>('.lush-skills-close')!.click();
    expect(panel.isOpen).toBe(false);
    expect(hud.isOpen).toBe(false);
  });

  it('lists Woodcutting, Fishing and Cooking with level, whole-point XP and XP to the next level', () => {
    const { panel, skills } = setup();
    skills.setStats([
      { skill: Skill.Woodcutting, level: 5, xp: 400.5 },
      { skill: Skill.Fishing, level: 1, xp: 37.5 },
      { skill: Skill.Cooking, level: 30, xp: 13400 },
    ]);
    panel.setOpen(true);
    expect([...panel.panel.querySelectorAll('.lush-skill-name')].map((e) => e.textContent)).toEqual(['Woodcutting', 'Fishing', 'Cooking']);
    // Level 6 starts at 512 XP: 111.5 to go shows as 112.
    expect(row(panel, 'woodcutting')).toEqual({ name: 'Woodcutting', level: '5/30', xp: '400 XP', next: '112 to level 6' });
    // Level 2 starts at 83: 45.5 to go.
    expect(row(panel, 'fishing')).toEqual({ name: 'Fishing', level: '1/30', xp: '37 XP', next: '46 to level 2' });
    expect(row(panel, 'cooking')).toEqual({ name: 'Cooking', level: '30/30', xp: '13,400 XP', next: 'Max level' });
  });

  it('shows whole points only', () => {
    expect(wholeXp(37.5)).toBe(37);
    expect(wholeXp(12.3 + 0.7)).toBe(13);
    expect(wholeXpToNext(45.5)).toBe(46);
    expect(wholeXpToNext(10)).toBe(10);
    expect(wholeXpToNext(0.1)).toBe(1);
  });
});

describe('Skills panel total level and updates (#466 AC2)', () => {
  it('shows the total level (combat plus skill levels) and updates on SkillStats and combat level changes', () => {
    const game = new GameState();
    const { panel } = setup(game.skills, game.items);
    panel.setOpen(true);
    const total = (): string => panel.panel.querySelector('.lush-skills-total')?.textContent ?? '';
    game.handle({ type: 'playerStats', hp: 10, maxHp: 10, combatLevel: 3, xp: 0, gold: 0 }, 0);
    expect(total()).toBe('Total level 6');

    game.handle({
      type: 'skillStats',
      skills: [
        { skill: Skill.Woodcutting, level: 15, xp: 2411 },
        { skill: Skill.Fishing, level: 2, xp: 90 },
        { skill: Skill.Cooking, level: 1, xp: 0 },
      ],
    }, 0);
    expect(total()).toBe('Total level 21');
    expect(row(panel, 'woodcutting')).toEqual({ name: 'Woodcutting', level: '15/30', xp: '2,411 XP', next: '335 to level 16' });
    expect(row(panel, 'fishing').xp).toBe('90 XP');

    game.handle({ type: 'playerStats', hp: 10, maxHp: 10, combatLevel: 10, xp: 0, gold: 0 }, 0);
    expect(total()).toBe('Total level 28');
  });
});

describe('Skills panel theme (#466 AC3)', () => {
  it('uses the shared panel and title classes, and its buttons the shared button class at MIN_TAP_PX or more', () => {
    const { hud, panel } = setup();
    panel.setOpen(true);
    expect(panel.panel.classList.contains('lush-panel')).toBe(true);
    const title = panel.panel.querySelector<HTMLElement>('.lush-title');
    expect(title?.textContent).toBe('Skills');
    expect(getComputedStyle(title!).fontVariant).toBe('small-caps');
    const buttons = [...panel.panel.querySelectorAll<HTMLButtonElement>('button'), ...hud.panel.querySelectorAll<HTMLButtonElement>('button')];
    expect(buttons).toHaveLength(5);
    for (const b of buttons) {
      expect(b.classList.contains(BUTTON_CLASS), b.textContent ?? '').toBe(true);
      const cs = getComputedStyle(b);
      expect(parseFloat(cs.minWidth), b.textContent ?? '').toBeGreaterThanOrEqual(MIN_TAP_PX);
      expect(parseFloat(cs.minHeight), b.textContent ?? '').toBeGreaterThanOrEqual(MIN_TAP_PX);
      b.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
      expect(b.classList.contains('pressed')).toBe(true);
      b.dispatchEvent(new PointerEvent('pointerup', { bubbles: true }));
    }
  });
});
