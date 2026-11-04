import { describe, expect, it } from 'vitest';
import skillsMap from '../../../shared/map/skills.json';
import { EntityKind, EquipSlot, ItemKind, Skill, SkillResultCode, SkillTarget, ToolSkill } from '../protocol';
import type { ItemInfo } from '../net/codec';
import { BANK_BOOTH } from './bank';
import { pickInteractTarget } from './interact';
import { GRILL, hasRawFish, usableResources } from './skillTargets';
import { ItemState } from './items';
import { SkillState } from './skills';
import { interactPromptText, promptString, interact } from '../core/interact';
import { GameState } from './world';

const item = (id: number, o: Partial<ItemInfo> = {}): ItemInfo => ({
  id, name: `item${id}`, kind: ItemKind.Loot, equipSlot: EquipSlot.None, levelToEquip: 0, accuracy: 0, strength: 0, defence: 0,
  attackSpeed: 0, heal: 0, shopBuysFor: 0, shopSells: false, shopSellsFor: 0, toolSkill: ToolSkill.None, toolLevel: 0, cookingLevel: 0, footprintW: 0, footprintD: 0, placement: 0, surfaceSlots: 0, thriftSellsFor: 0, thriftBuysFor: 0, ...o,
});
const HATCHET = 20, BIG_HATCHET = 21, NET = 22, RAW = 23;
const catalog = [
  item(HATCHET, { toolSkill: ToolSkill.Woodcutting, toolLevel: 1 }),
  item(BIG_HATCHET, { toolSkill: ToolSkill.Woodcutting, toolLevel: 10 }),
  item(NET, { toolSkill: ToolSkill.Fishing }),
  item(RAW, { cookingLevel: 1 }),
];
const dm = (v: number) => v / 10;
const treeIdx = (id: string) => skillsMap.trees.findIndex((t) => t.id === id);
const treePos = (id: string) => ({ x: dm(skillsMap.trees[treeIdx(id)].x), z: dm(skillsMap.trees[treeIdx(id)].z) });

function world(opts: { held?: number[]; weapon?: number; wc?: number; fish?: number } = {}) {
  const items = new ItemState();
  items.setCatalog(catalog);
  items.setInventory({
    slots: (opts.held ?? []).map((itemId) => ({ itemId, quantity: 1 })),
    equipment: { weapon: opts.weapon ?? 0, head: 0, body: 0, shield: 0 },
  });
  const skills = new SkillState();
  skills.setStats([{ skill: Skill.Woodcutting, level: opts.wc ?? 1, xp: 0 }, { skill: Skill.Fishing, level: opts.fish ?? 1, xp: 0 }]);
  skills.setCatalog(
    [{ kind: 'tree', level: 1 }, { kind: 'oak', level: 5 }, { kind: 'eucalyptus', level: 10 }],
    [{ kind: 'net', level: 1, toolItemId: NET }, { kind: 'rod', level: 5, toolItemId: 99 }],
  );
  return { items, skills };
}
const pick = (pos: { x: number; z: number }, w: ReturnType<typeof world>) =>
  pickInteractTarget({
    pos, stall: { x: 1000, z: 1000 }, entities: [],
    skill: { hasRawFish: hasRawFish(w.items), resources: usableResources(w.items, w.skills) },
  });

describe('skill Interact order (AC1)', () => {
  it('puts the bank first, then the grill with raw fish, then trees', () => {
    const w = world({ held: [HATCHET, RAW] });
    w.skills.setResources([], []);
    const atGrill = { x: GRILL.x + 0.5, z: GRILL.z + 0.5 };
    expect(pick(atGrill, w)).toEqual({ type: 'grill' });
    // Bank booth and grill together: the bank wins.
    const both = pickInteractTarget({
      pos: GRILL, stall: { x: 1000, z: 1000 }, bank: GRILL, entities: [], skill: { hasRawFish: true, resources: [] },
    });
    expect(both).toEqual({ type: 'bank' });
    expect(BANK_BOOTH).toBeDefined();
    // A tree at the grill: the grill wins; without raw fish the tree does.
    const treeAt = { type: 'tree' as const, id: 3, x: GRILL.x, z: GRILL.z, kind: 'oak' };
    const base = { pos: GRILL, stall: { x: 1000, z: 1000 }, entities: [] };
    expect(pickInteractTarget({ ...base, skill: { hasRawFish: true, resources: [treeAt] } })).toEqual({ type: 'grill' });
    expect(pickInteractTarget({ ...base, skill: { hasRawFish: false, resources: [treeAt] } })).toEqual({ type: 'tree', id: 3, kind: 'oak' });
  });

  it('counts the grill within one tile including diagonals, and only with raw fish', () => {
    const w = world({ held: [RAW] });
    expect(pick({ x: GRILL.x + 1, z: GRILL.z + 1 }, w)).toEqual({ type: 'grill' });
    expect(pick({ x: GRILL.x + 2.5, z: GRILL.z }, w)).toBeNull();
    expect(pick(GRILL, world({ held: [HATCHET] }))).toBeNull();
  });

  it('picks the nearest of tree and active fishing spot, and leaves card tables/ground items after', () => {
    const w = world({ held: [HATCHET, NET] });
    const spot = skillsMap.pond.spots[0];
    w.skills.setResources([], [{ id: 7, candidate: 0 }]);
    const p = { x: dm(spot.x), z: dm(spot.z) };
    expect(pick(p, w)).toEqual({ type: 'fishingSpot', id: 7, kind: 'net' });
    const ground = pickInteractTarget({
      pos: p, stall: { x: 1000, z: 1000 }, entities: [], groundItems: [{ id: 1, x: p.x, z: p.z }],
      skill: { hasRawFish: false, resources: usableResources(w.items, w.skills) },
    });
    expect(ground).toEqual({ type: 'fishingSpot', id: 7, kind: 'net' });
    expect(pick(treePos('tree-1'), w)).toEqual({ type: 'tree', id: treeIdx('tree-1'), kind: 'tree' });
  });

  it('puts the grill and resources before a card table, ground item, NPC and player in range', () => {
    const tree = { type: 'tree' as const, id: 3, x: GRILL.x, z: GRILL.z, kind: 'oak' };
    const crowd = {
      pos: GRILL, stall: { x: 1000, z: 1000 }, cardTables: [{ id: 1, x: GRILL.x, z: GRILL.z }],
      groundItems: [{ id: 2, x: GRILL.x, z: GRILL.z }],
      entities: [
        { id: 5, x: GRILL.x, z: GRILL.z, kind: EntityKind.Npc },
        { id: 6, x: GRILL.x, z: GRILL.z, kind: EntityKind.Player },
      ],
    };
    expect(pickInteractTarget({ ...crowd, skill: { hasRawFish: true, resources: [tree] } })).toEqual({ type: 'grill' });
    expect(pickInteractTarget({ ...crowd, skill: { hasRawFish: false, resources: [tree] } })).toEqual({ type: 'tree', id: 3, kind: 'oak' });
    expect(pickInteractTarget({ ...crowd, skill: { hasRawFish: false, resources: [] } })).toEqual({ type: 'cardTable', id: 1 });
  });
});

describe('usable resources (AC2)', () => {
  it('skips trees below the catalog level', () => {
    const w = world({ held: [HATCHET], wc: 4 });
    expect(pick(treePos('oak-1'), w)).toBeNull();
    w.skills.setStats([{ skill: Skill.Woodcutting, level: 5, xp: 0 }]);
    expect(pick(treePos('oak-1'), w)).toEqual({ type: 'tree', id: treeIdx('oak-1'), kind: 'oak' });
  });

  it('needs a hatchet whose tool level fits, held or wielded', () => {
    expect(pick(treePos('tree-1'), world())).toBeNull();
    expect(pick(treePos('tree-1'), world({ held: [BIG_HATCHET], wc: 9 }))).toBeNull();
    expect(pick(treePos('tree-1'), world({ held: [BIG_HATCHET], wc: 10 }))).not.toBeNull();
    expect(pick(treePos('tree-1'), world({ weapon: HATCHET }))).not.toBeNull();
  });

  it('skips felled trees', () => {
    const w = world({ held: [HATCHET] });
    w.skills.setResources([treeIdx('tree-1')], []);
    expect(pick(treePos('tree-1'), w)).toBeNull();
  });

  it('skips fishing spots without the catalog tool or level', () => {
    const spot = skillsMap.pond.spots[0];
    const p = { x: dm(spot.x), z: dm(spot.z) };
    const noTool = world({ held: [HATCHET] });
    noTool.skills.setResources([], [{ id: 1, candidate: 0 }]);
    expect(pick(p, noTool)).toBeNull();
    const rod = skillsMap.pond.spots.findIndex((s) => s.kind === 'rod');
    const r = skillsMap.pond.spots[rod];
    const rp = { x: dm(r.x), z: dm(r.z) };
    // The rod (catalog tool 99) is held, so only the Fishing level decides.
    const lowLevel = world({ held: [99], fish: 4 });
    lowLevel.skills.setResources([], [{ id: 2, candidate: rod }]);
    expect(pick(rp, lowLevel)).toBeNull();
    const enough = world({ held: [99], fish: 5 });
    enough.skills.setResources([], [{ id: 2, candidate: rod }]);
    expect(pick(rp, enough)).toEqual({ type: 'fishingSpot', id: 2, kind: 'rod' });
  });
});

describe('Interact sends SkillAction and names the action (AC3)', () => {
  function game(pos: { x: number; z: number }, w: ReturnType<typeof world>) {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 1, guestToken: 't', spawn: pos, tickRate: 20 }, 0);
    g.handle({ type: 'snapshot', tick: 1, lastInputSeq: 0, entities: [{ id: 1, x: pos.x, z: pos.z, heading: 0, animation: 0 }] }, 0);
    g.items.setCatalog(catalog);
    g.items.setInventory(w.items.inventory);
    g.skills.setStats([{ skill: Skill.Woodcutting, level: 5, xp: 0 }, { skill: Skill.Fishing, level: 1, xp: 0 }]);
    g.skills.setCatalog(
      [{ kind: 'tree', level: 1 }, { kind: 'oak', level: 5 }, { kind: 'eucalyptus', level: 10 }],
      [{ kind: 'net', level: 1, toolItemId: NET }, { kind: 'rod', level: 5, toolItemId: 99 }],
    );
    g.skills.setResources([], [{ id: 7, candidate: 0 }]);
    return g;
  }
  const run = (g: GameState) => {
    const sent: [SkillTarget, number][] = [];
    const none = () => {};
    const t = interact(g, 0, { x: 1000, z: 1000 }, {
      openShop: none, openBank: none, openCardTable: none, openHouse: none, openThrift: none, openWorkbench: none, sendPickUp: none, sendAttackTarget: none, sendTradeRequest: none, sendTalk: none,
      sendSkillAction: (target, id) => sent.push([target, id]),
    });
    const p = interactPromptText(g, t);
    return { sent, prompt: p && promptString(p) };
  };

  it('chops a tree by its index in trees[]', () => {
    const g = game(treePos('oak-3'), world({ held: [HATCHET] }));
    expect(run(g)).toEqual({ sent: [[SkillTarget.Tree, treeIdx('oak-3')]], prompt: 'Chop Oak' });
  });

  it('fishes a spot by its FishingSpotState id at pond.spots[candidate]', () => {
    const s = skillsMap.pond.spots[0];
    const g = game({ x: dm(s.x), z: dm(s.z) }, world({ held: [NET] }));
    expect(run(g)).toEqual({ sent: [[SkillTarget.FishingSpot, 7]], prompt: 'Fish' });
  });

  it('cooks at the grill', () => {
    const g = game(GRILL, world({ held: [RAW] }));
    expect(run(g)).toEqual({ sent: [[SkillTarget.Grill, 0]], prompt: 'Cook' });
  });

  it('makes the sent target active once the server answers Started (#508 AC2)', () => {
    const started = { type: 'skillResult', skill: 0, code: SkillResultCode.Started, itemId: 0, level: 0 } as const;
    const candidateOf = (id: number) => g.skills.fishingSpotCandidate(id);
    let g = game(treePos('oak-3'), world({ held: [HATCHET] }));
    run(g);
    g.handle(started, 0);
    expect(g.gathering.activeResource(candidateOf)).toBe('oak-3');
    const s = skillsMap.pond.spots[0];
    g = game({ x: dm(s.x), z: dm(s.z) }, world({ held: [NET] }));
    run(g);
    g.handle(started, 0);
    expect(g.gathering.activeResource(candidateOf)).toBe(s.id);
    g = game(GRILL, world({ held: [RAW] }));
    run(g);
    g.handle(started, 0);
    expect(g.gathering.activeResource(candidateOf)).toBe('grill');
  });
});
