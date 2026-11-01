import { describe, expect, it } from 'vitest';
import { GameState } from './world';
import { GainTracker, MAX_GAINS } from './gains';

const LIFE = 1200;

describe('GainTracker', () => {
  it('adds an xp and a gold gain with the difference and unique ids', () => {
    const g = new GainTracker();
    g.update(10, 100, 0);
    g.update(25, 100, 1);
    g.update(25, 130, 2);
    g.update(30, 140, 3);
    const out = g.active(3, LIFE);
    expect(out.map((e) => [e.kind, e.amount])).toEqual([['xp', 15], ['gold', 30], ['xp', 5], ['gold', 10]]);
    expect(new Set(out.map((e) => e.id)).size).toBe(4);
  });

  it('adds both gains from one message', () => {
    const g = new GainTracker();
    g.update(0, 0, 0);
    g.update(5, 7, 1);
    expect(g.active(1, LIFE).map((e) => e.kind)).toEqual(['xp', 'gold']);
  });

  it('adds nothing for the first stats, after reset, or on a drop', () => {
    const g = new GainTracker();
    g.update(100, 500, 0);
    expect(g.active(0, LIFE)).toEqual([]);
    g.update(100, 200, 1);
    expect(g.active(1, LIFE)).toEqual([]);
    g.update(100, 300, 2);
    expect(g.active(2, LIFE)).toHaveLength(1);
    g.reset();
    g.update(900, 900, 3);
    expect(g.active(3, LIFE)).toEqual([]);
  });

  it('drops a gain after its lifetime and stays bounded', () => {
    const g = new GainTracker();
    g.update(0, 0, 0);
    g.update(1, 0, 0);
    expect(g.active(LIFE - 1, LIFE)).toHaveLength(1);
    expect(g.active(LIFE, LIFE)).toHaveLength(0);
    for (let i = 2; i < 200; i++) g.update(i, 0, 5000);
    expect(g.active(5000, LIFE).length).toBeLessThanOrEqual(MAX_GAINS);
  });
});

describe('GameState gains', () => {
  it('feeds PlayerStats into gains and clears on Welcome', () => {
    const game = new GameState();
    const stats = (xp: number, gold: number) => ({ type: 'playerStats', hp: 1, maxHp: 1, combatLevel: 1, xp, gold }) as never;
    game.handle(stats(0, 0), 0);
    game.handle(stats(4, 0), 10);
    expect(game.gains.active(10, 1200)).toHaveLength(1);
    game.handle({ type: 'welcome', playerId: 1, token: 't', spawn: { x: 0, z: 0 }, tickRate: 20 } as never, 20);
    expect(game.gains.active(20, 1200)).toEqual([]);
    game.handle(stats(50, 50), 30);
    expect(game.gains.active(30, 1200)).toEqual([]);
  });
});
