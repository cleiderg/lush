// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { interact, interactPromptText, interactTarget, type InteractActions } from '../core/interact';
import { EntityKind } from '../protocol';
import { CARD_TABLE_POSITIONS } from '../state/cardTables';
import { GameState } from '../state/world';
import { hudLayout, inside, intersects } from './hudLayout';
import { INTERACT_PROMPT_CSS, InteractPrompt } from './interactPrompt';
import { SHOP_STALL } from './shopPanel';

const NOW = 10_000;
const NO_EQUIPMENT = { weapon: 0, head: 0, body: 0, shield: 0 };
// Well away from the stall and outside the PvP zone.
const OPEN = { x: SHOP_STALL.x + 50.5, z: SHOP_STALL.z + 0.5 };
// Inside the PvP zone from areas.json.
const PVP = { x: 300.5, z: -349.5 };
const COYOTE = 7;

interface Other { id: number; x: number; z: number; kind: EntityKind; npcType?: number; combatLevel?: number }

/** A game whose local player (id 1) stands at pos, with catalogs, other entities and ground items around. */
function game(pos: { x: number; z: number }, others: Other[] = [], ground: { id: number; x: number; z: number; itemId: number }[] = []): GameState {
  const g = new GameState();
  g.handle({ type: 'welcome', playerId: 1, guestToken: 't', spawn: pos, tickRate: 20 }, 0);
  g.handle({ type: 'npcCatalog', npcs: [{ id: COYOTE, name: 'Coyote', combatLevel: 9 }] }, 0);
  g.items.setCatalog([{ id: 3, name: 'Acorn' } as never]);
  const entities = [{ id: 1, x: pos.x, z: pos.z, heading: 0, animation: 0 }, ...others.map((o) => ({ id: o.id, x: o.x, z: o.z, heading: 0, animation: 0 }))];
  g.handle({ type: 'snapshot', tick: 1, lastInputSeq: 0, entities }, 0);
  g.handle({ type: 'groundItems', items: ground.map((i) => ({ ...i, quantity: 1 })) }, 0);
  for (const o of others) {
    g.handle({ type: 'entityInfo', id: o.id, kind: o.kind, npcType: o.npcType ?? 0, combatLevel: o.combatLevel ?? 3, equipment: NO_EQUIPMENT }, 0);
  }
  return g;
}

let prompt: InteractPrompt | null = null;
beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  prompt?.dispose();
  prompt = null;
});

function mount(g: GameState, width = 390, height = 844): InteractPrompt {
  prompt = new InteractPrompt({
    parent: document.body,
    prompt: () => interactPromptText(g, interactTarget(g, NOW, SHOP_STALL)),
    viewport: () => ({ width, height }),
  });
  return prompt;
}

describe('interact prompt text (AC1)', () => {
  const cases: [string, () => GameState, string, string | null][] = [
    ['an NPC to attack', () => game(OPEN, [{ id: 5, x: OPEN.x + 1, z: OPEN.z, kind: EntityKind.Npc, npcType: COYOTE, combatLevel: 12 }]),
      'Attack Coyote · Lvl 12', 'Coyote'],
    ['a player in the PvP zone', () => game(PVP, [{ id: 6, x: PVP.x + 2, z: PVP.z, kind: EntityKind.Player }]), 'Attack a player', null],
    ['the shop', () => game({ ...SHOP_STALL }), 'Open Campus Market', 'Campus Market'],
    ['a ground item', () => game(OPEN, [], [{ id: 40, x: OPEN.x, z: OPEN.z, itemId: 3 }]), 'Pick up Acorn', 'Acorn'],
    ['a player to trade', () => game(OPEN, [{ id: 6, x: OPEN.x + 2, z: OPEN.z, kind: EntityKind.Player }]), 'Trade with a player', null],
    ['a card table', () => game({ x: CARD_TABLE_POSITIONS[0].x, z: CARD_TABLE_POSITIONS[0].z }), 'Open card table', null],
  ];
  for (const [what, make, text, name] of cases) {
    it(`for ${what} reads '${text}'${name ? `, ${name} in gold` : ''}`, () => {
      const p = mount(make());
      expect(p.visible).toBe(true);
      expect(p.root.textContent).toBe(text);
      if (name) {
        expect(p.name.textContent).toBe(name);
        expect(p.root.contains(p.name)).toBe(true);
        expect(p.name.classList.contains('lush-interact-name')).toBe(true);
      } else {
        expect(p.root.contains(p.name)).toBe(false);
      }
    });
  }

  it('styles the name with the theme gold', () => {
    expect(INTERACT_PROMPT_CSS).toMatch(/\.lush-interact-name \{[^}]*color: var\(--lush-gold\)/);
    mount(game(OPEN));
    const css = [...document.head.querySelectorAll('style')].map((s) => s.textContent).join('\n');
    expect(css).toContain(INTERACT_PROMPT_CSS);
  });

  it('is hidden with no target', () => {
    const p = mount(game(OPEN));
    expect(p.visible).toBe(false);
    expect(p.root.hidden).toBe(true);
  });
});

describe('interact prompt and the Interact button agree (AC2)', () => {
  it('names the target Interact acts on, and hides once the player walks out of range', () => {
    const g = game(OPEN, [
      { id: 5, x: OPEN.x + 1, z: OPEN.z, kind: EntityKind.Npc, npcType: COYOTE, combatLevel: 9 },
      { id: 6, x: OPEN.x + 1, z: OPEN.z, kind: EntityKind.Player },
    ]);
    const actions: InteractActions = {
      openShop: vi.fn(), openCardTable: vi.fn(), sendPickUp: vi.fn(), sendAttackTarget: vi.fn(), sendTradeRequest: vi.fn(),
    };
    const p = mount(g);
    expect(p.root.textContent).toBe('Attack Coyote · Lvl 9');
    expect(interact(g, NOW, SHOP_STALL, actions)).toEqual({ type: 'attack', id: 5 });
    expect(actions.sendAttackTarget).toHaveBeenCalledWith(5);

    // Walk well away from both.
    for (let i = 0; i < 20; i++) g.prediction.applyInput(1, 0, 0.5);
    expect(g.prediction.pos.x - OPEN.x).toBeGreaterThan(5);
    p.update();
    expect(p.visible).toBe(false);
    expect(interact(g, NOW, SHOP_STALL, actions)).toBeNull();
    expect(actions.sendAttackTarget).toHaveBeenCalledTimes(1);
    expect(actions.sendTradeRequest).not.toHaveBeenCalled();
  });
});

describe('interact prompt layout (AC3)', () => {
  for (const [w, h] of [[390, 844], [1600, 1000]]) {
    it(`at ${w}×${h} is centred in the app column, in the top third, clear of the HUD, strips and controls`, () => {
      const l = hudLayout(w, h);
      const r = l.interact;
      expect(r.w).toBeGreaterThan(0);
      expect(r.h).toBeGreaterThan(0);
      expect(inside(r, { x: 0, y: 0, w, h })).toBe(true);
      expect(r.x + r.w / 2).toBeCloseTo(l.controls.x + l.controls.w / 2, 5);
      expect(r.y + r.h).toBeLessThanOrEqual(h / 3);
      for (const [name, o] of [['location', l.location], ['hud', l.hud], ['toggle', l.toggle], ['retro', l.retro], ['pvp', l.pvp],
        ['chat', l.chat], ['controls', l.controls]] as const) {
        expect(intersects(r, o), name).toBe(false);
      }
    });

    it(`at ${w}×${h} the prompt element is placed there`, () => {
      const p = mount(game(OPEN), w, h);
      const r = hudLayout(w, h).interact;
      expect(p.root.style.left).toBe(`${r.x}px`);
      expect(p.root.style.top).toBe(`${r.y}px`);
      expect(p.root.style.width).toBe(`${r.w}px`);
    });
  }
});

describe('interact prompt theme (AC4)', () => {
  it('uses the shared panel class', () => {
    const p = mount(game(OPEN));
    expect(p.root.classList.contains('lush-panel')).toBe(true);
    expect(p.root.classList.contains('lush-interact')).toBe(true);
  });
});
