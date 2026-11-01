import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CollisionGrid } from '../state/collision';
import { GameState } from '../state/world';
import { decode } from './codec';
import {
  botEntities, encodeSnapshot, encodeWelcome, makeBots, memoryTokenStore, OFFLINE_PLACES, OFFLINE_PLAYER_ID, OFFLINE_SERVER_TIME_MS,
  OfflineTransport, offlineOptions, type OfflineTimers,
} from './offline';
import { Session } from './session';
import areas from '../../../shared/map/areas.json';

/** Timers run by hand: one-shots on flush(), repeats on tick(). */
function manualTimers() {
  const once: (() => void)[] = [];
  const repeats = new Map<number, () => void>();
  let next = 1;
  const timers: OfflineTimers = {
    setTimer: (fn) => once.push(fn),
    setRepeat: (fn) => {
      repeats.set(next, fn);
      return next++;
    },
    clearRepeat: (id) => repeats.delete(id as number),
  };
  return {
    timers,
    flush: () => once.splice(0).forEach((f) => f()),
    tick: () => repeats.forEach((f) => f()),
    repeats,
  };
}

function offlineSession(search: string) {
  const t = manualTimers();
  const opts = offlineOptions(search);
  if (!opts) throw new Error('offline not enabled');
  const game = new GameState();
  const transports: OfflineTransport[] = [];
  const session = new Session({
    factory: () => {
      const tr = new OfflineTransport(opts, t.timers);
      transports.push(tr);
      return tr;
    },
    store: memoryTokenStore(),
    onMessage: (msg) => game.handle(msg, 0),
  });
  session.start();
  t.flush();
  return { session, game, t, transports };
}

describe('offline options', () => {
  it('is off without the offline flag', () => {
    expect(offlineOptions('')).toBeNull();
    expect(offlineOptions('?debug')).toBeNull();
  });

  it('reads seed, place and bots, with defaults', () => {
    expect(offlineOptions('?offline')).toEqual({ seed: 1, spawn: OFFLINE_PLACES.spawn, bots: 8 });
    expect(offlineOptions('?offline&seed=7&place=park&bots=3')).toEqual({ seed: 7, spawn: OFFLINE_PLACES.park, bots: 3 });
    expect(offlineOptions('?offline&place=nowhere&bots=999')?.spawn).toEqual(OFFLINE_PLACES.spawn);
    expect(offlineOptions('?offline&bots=999')?.bots).toBe(64);
  });

  it('the spawn matches areas.json', () => {
    expect(OFFLINE_PLACES.spawn).toEqual({ x: -21.5, z: -75.5 });
  });
});

describe('offline bots', () => {
  it('the same seed gives the same bot positions', () => {
    const a = makeBots(42, 8, OFFLINE_PLACES.spawn);
    const b = makeBots(42, 8, OFFLINE_PLACES.spawn);
    expect(a).toEqual(b);
    expect(botEntities(a, 100)).toEqual(botEntities(b, 100));
  });

  it('a different seed gives different positions', () => {
    const a = makeBots(1, 8, OFFLINE_PLACES.spawn);
    const b = makeBots(2, 8, OFFLINE_PLACES.spawn);
    expect(a.map((x) => [x.x, x.z])).not.toEqual(b.map((x) => [x.x, x.z]));
  });

  it('bots stay near the spawn with ids after the local player', () => {
    const bots = makeBots(3, 20, OFFLINE_PLACES.spawn);
    expect(new Set(bots.map((b) => b.id)).size).toBe(20);
    for (const b of bots) {
      expect(b.id).toBeGreaterThan(OFFLINE_PLAYER_ID);
      for (const e of [0, 37, 500].flatMap((tick) => botEntities([b], tick))) {
        expect(Math.hypot(e.x - OFFLINE_PLACES.spawn.x, e.z - OFFLINE_PLACES.spawn.z)).toBeLessThan(16);
      }
    }
  });

  it('both offline places are walkable', () => {
    const grid = CollisionGrid.parse(new Uint8Array(readFileSync(join(__dirname, '../../../shared/map/collision.bin'))));
    for (const p of Object.values(OFFLINE_PLACES)) expect(grid.blocked(p.x, p.z)).toBe(false);
  });
});

describe('offline cafe place', () => {
  it('is on a walkable cell inside the café polygon from areas.json', () => {
    const grid = CollisionGrid.parse(new Uint8Array(readFileSync(join(__dirname, '../../../shared/map/collision.bin'))));
    const poly = areas.interiors.find((a) => a.id === 'student_center_cafe')!.p.map((v) => v / 10);
    const { x, z } = OFFLINE_PLACES.cafe;
    let inside = false;
    for (let i = 0, j = poly.length - 2; i < poly.length; j = i, i += 2) {
      if (poly[i + 1] > z !== poly[j + 1] > z && x < poly[i] + ((z - poly[i + 1]) * (poly[j] - poly[i])) / (poly[j + 1] - poly[i + 1])) inside = !inside;
    }
    expect(inside).toBe(true);
    expect(grid.blocked(x, z)).toBe(false);
  });
});

describe('offline encoding', () => {
  it('Welcome and Snapshot decode as the server sends them', () => {
    expect(decode(encodeWelcome(1, { x: -21.5, z: -75.5 }, OFFLINE_SERVER_TIME_MS))).toMatchObject({
      type: 'welcome', playerId: 1, spawn: { x: -21.5, z: -75.5 }, tickRate: 20, serverTimeMs: OFFLINE_SERVER_TIME_MS,
    });
    const ents = [{ id: 2, x: 1.2, z: -3.4, heading: Math.PI, animation: 1 }];
    const snap = decode(encodeSnapshot(9, 0, ents));
    expect(snap).toMatchObject({ type: 'snapshot', tick: 9, lastInputSeq: 0 });
    if (snap?.type !== 'snapshot') throw new Error('not a snapshot');
    expect(snap.entities[0].x).toBeCloseTo(1.2);
    expect(snap.entities[0].z).toBeCloseTo(-3.4);
    expect(snap.entities[0].heading).toBeCloseTo(Math.PI);
    expect(snap.entities[0].animation).toBe(1);
  });
});

describe('offline session', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('feeds Welcome and bot snapshots and creates no WebSocket', () => {
    const ws = vi.fn();
    vi.stubGlobal('WebSocket', ws);
    const { session, game, t, transports } = offlineSession('?offline&seed=5&bots=6');
    expect(session.connected).toBe(true);
    expect(game.playerId).toBe(OFFLINE_PLAYER_ID);
    expect(game.prediction.pos).toEqual(OFFLINE_PLACES.spawn);
    expect(game.clock.serverNow()).toBeGreaterThanOrEqual(OFFLINE_SERVER_TIME_MS);
    t.tick();
    t.tick();
    expect(game.remotes(1e9)).toHaveLength(6);
    expect(transports).toHaveLength(1);
    expect(t.repeats.size).toBe(1);
    expect(ws).not.toHaveBeenCalled();
    session.stop();
    expect(t.repeats.size).toBe(0);
  });

  it('the same seed gives the same remote players', () => {
    const remotes = (seed: number) => {
      const { game, t } = offlineSession(`?offline&seed=${seed}`);
      t.tick();
      return game.remotes(1e9).map((r) => ({ id: r.id, x: r.x, z: r.z }));
    };
    const a = remotes(11);
    expect(a).toHaveLength(8);
    expect(remotes(11)).toEqual(a);
    expect(remotes(12)).not.toEqual(a);
  });
});
