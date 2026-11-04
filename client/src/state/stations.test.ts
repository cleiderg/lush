// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest';
import { interact, interactPromptText, type InteractActions } from '../core/interact';
import { HOUSE_DOORS } from './doors';
import { pickInteractTarget } from './interact';
import { SHOP_DISTANCE_TILES, TILE_METRES } from './shop';
import { THRIFT_STALLS, WORKBENCHES } from './stations';
import { GameState } from './world';

const FAR = { x: 1e6, z: 1e6 };
const REACH = SHOP_DISTANCE_TILES * TILE_METRES;
const pick = (pos: { x: number; z: number }, extra: object = {}) =>
  pickInteractTarget({ pos, stall: FAR, bank: FAR, entities: [], doors: [], ...extra });

describe('thrift stalls and workbenches (AC1)', () => {
  it('has one of each per area', () => {
    expect(THRIFT_STALLS).toHaveLength(2);
    expect(WORKBENCHES).toHaveLength(2);
  });

  it('picks a thrift target within shop distance of either thrift point', () => {
    for (const t of THRIFT_STALLS) {
      expect(pick({ x: t.x + REACH - 0.01, z: t.z }, { workbenches: [] })).toEqual({ type: 'thrift' });
      expect(pick({ x: t.x, z: t.z + REACH + 0.01 }, { workbenches: [] })).toBeNull();
      // Straight line: the corner of the square is out of reach.
      expect(pick({ x: t.x + REACH, z: t.z + REACH }, { workbenches: [] })).toBeNull();
    }
  });

  it('picks a workbench target within shop distance of either workbench point', () => {
    for (const w of WORKBENCHES) {
      expect(pick({ x: w.x - REACH + 0.01, z: w.z }, { thrifts: [] })).toEqual({ type: 'workbench' });
      expect(pick({ x: w.x, z: w.z - REACH - 0.01 }, { thrifts: [] })).toBeNull();
    }
  });

  it('picks neither away from both', () => {
    expect(pick({ x: 0, z: 0 })).toBeNull();
  });
});

describe('nearest wins (AC2)', () => {
  it('prefers the nearer of the stall and the workbench', () => {
    const thrifts = [{ id: 1, x: 0, z: 0, areaName: 'A' }];
    const workbenches = [{ id: 1, x: 2, z: 0, areaName: 'A' }];
    expect(pick({ x: 0.5, z: 0 }, { thrifts, workbenches })).toEqual({ type: 'thrift' });
    expect(pick({ x: 1.5, z: 0 }, { thrifts, workbenches })).toEqual({ type: 'workbench' });
  });

  it('prefers the nearer of a door and a station', () => {
    const doors = [{ id: 7, x: 0, z: 0, areaName: 'A', number: 1 }];
    const workbenches = [{ id: 1, x: 2.5, z: 0, areaName: 'A' }];
    expect(pick({ x: 0.5, z: 0 }, { doors, workbenches, thrifts: [] })).toEqual({ type: 'door', id: 7 });
    expect(pick({ x: 1.8, z: 0 }, { doors, workbenches, thrifts: [] })).toEqual({ type: 'workbench' });
  });

  it('still finds real doors when none are passed', () => {
    const d = HOUSE_DOORS[0];
    expect(pickInteractTarget({ pos: d, stall: FAR, bank: FAR, entities: [] })).toEqual({ type: 'door', id: 1 });
  });
});

describe('Interact opens the panels (AC3)', () => {
  const actions = (): InteractActions => ({
    openShop: vi.fn(), openBank: vi.fn(), openCardTable: vi.fn(), openHouse: vi.fn(), openThrift: vi.fn(), openWorkbench: vi.fn(),
    sendPickUp: vi.fn(), sendAttackTarget: vi.fn(), sendTradeRequest: vi.fn(), sendTalk: vi.fn(), sendSkillAction: vi.fn(),
  });
  const gameAt = (pos: { x: number; z: number }): GameState => {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 1, guestToken: 't', spawn: pos, tickRate: 20 }, 0);
    g.handle({ type: 'snapshot', tick: 1, lastInputSeq: 0, entities: [{ id: 1, x: pos.x, z: pos.z, heading: 0, animation: 0 }] }, 0);
    return g;
  };

  it('opens the thrift panel with the prompt Open Campus Thrift', () => {
    const t = THRIFT_STALLS[0];
    const g = gameAt({ x: t.x, z: t.z + 0.5 });
    const a = actions();
    const target = interact(g, 0, FAR, a);
    expect(target).toEqual({ type: 'thrift' });
    expect(a.openThrift).toHaveBeenCalledTimes(1);
    expect(a.openWorkbench).not.toHaveBeenCalled();
    const p = interactPromptText(g, target);
    expect(p && [p.verb, p.name].join(' ')).toBe('Open Campus Thrift');
  });

  it('opens the workbench panel with the prompt Use Workbench', () => {
    const w = WORKBENCHES[1];
    const g = gameAt({ x: w.x + 0.5, z: w.z });
    const a = actions();
    const target = interact(g, 0, FAR, a);
    expect(target).toEqual({ type: 'workbench' });
    expect(a.openWorkbench).toHaveBeenCalledTimes(1);
    expect(a.openThrift).not.toHaveBeenCalled();
    const p = interactPromptText(g, target);
    expect(p && [p.verb, p.name].join(' ')).toBe('Use Workbench');
  });
});
