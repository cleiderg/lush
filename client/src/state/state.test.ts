import { describe, expect, it } from 'vitest';
import { Animation } from '../protocol';
import { GameState } from './world';
import { Interpolator } from './interpolation';
import { WALK_SPEED, Prediction } from './prediction';

describe('Prediction', () => {
  it('applies inputs immediately and numbers them', () => {
    const p = new Prediction();
    expect(p.applyInput(1, 0, 0.05).seq).toBe(1);
    expect(p.applyInput(1, 0, 0.05).seq).toBe(2);
    expect(p.pos.x).toBeCloseTo(WALK_SPEED * 0.1);
  });

  it('corrects position after a server correction with 3 inputs in flight', () => {
    const p = new Prediction();
    for (let i = 0; i < 5; i++) p.applyInput(1, 0, 0.05);
    // Server processed inputs 1-2 but was pushed back (e.g. collision): at x=0.1.
    p.reconcile({ x: 0.1, z: 0 }, 2);
    expect(p.pendingCount).toBe(3);
    expect(p.pos.x).toBeCloseTo(0.1 + 3 * WALK_SPEED * 0.05);
    expect(p.pos.z).toBeCloseTo(0);
    // Everything acknowledged: position is exactly the server's.
    p.reconcile({ x: 2, z: 1 }, 5);
    expect(p.pendingCount).toBe(0);
    expect(p.pos).toEqual({ x: 2, z: 1 });
  });

  it('clamps diagonal speed', () => {
    const p = new Prediction();
    p.applyInput(1, 1, 1);
    expect(Math.hypot(p.pos.x, p.pos.z)).toBeCloseTo(WALK_SPEED);
  });
});

describe('Interpolator', () => {
  it('samples between two snapshots 100 ms behind', () => {
    const i = new Interpolator();
    i.push(1000, [{ id: 2, x: 0, z: 0, heading: 0, animation: 0 }]);
    i.push(1050, [{ id: 2, x: 10, z: 20, heading: 0, animation: 0 }]);
    const [mid] = i.sample(1125); // render time 1025
    expect(mid.x).toBeCloseTo(5);
    expect(mid.z).toBeCloseTo(10);
    expect(i.sample(1200)[0].x).toBe(10); // past newest: hold
    expect(i.sample(1000)[0].x).toBe(0); // before oldest: hold
  });

  it('takes the short way around for heading', () => {
    const i = new Interpolator();
    i.push(0, [{ id: 1, x: 0, z: 0, heading: 6.2, animation: 0 }]);
    i.push(100, [{ id: 1, x: 0, z: 0, heading: 0.2, animation: 0 }]);
    const h = i.sample(150)[0].heading;
    expect(Math.cos(h)).toBeGreaterThan(0.99);
  });
});

describe('GameState animation', () => {
  it('carries the latest snapshot animation for local and remote players', () => {
    const g = new GameState();
    g.handle({ type: 'welcome', playerId: 1, guestToken: 't', spawn: { x: 0, z: 0 }, tickRate: 20 }, 0);
    const ent = (id: number, animation: number) => ({ id, x: 0, z: 0, heading: 0, animation });
    g.handle({ type: 'snapshot', tick: 1, lastInputSeq: 0, entities: [ent(1, Animation.Wave), ent(2, Animation.Wave)] }, 0);
    expect(g.local.animation).toBe(Animation.Wave);
    expect(g.remotes(1000)[0].animation).toBe(Animation.Wave);
    g.handle({ type: 'snapshot', tick: 2, lastInputSeq: 0, entities: [ent(1, Animation.Idle), ent(2, Animation.Idle)] }, 50);
    expect(g.local.animation).toBe(Animation.Idle);
    expect(g.remotes(1000)[0].animation).toBe(Animation.Idle);
  });
});
