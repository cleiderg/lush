import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { CollisionGrid, slide, type Vec } from './collision';
import { MOVE_SPEED, Prediction } from './prediction';
import { GameState } from './world';

/** Builds a collision.bin like the server's gridRows test helper: 1 m cells, origin (0,0), '#' blocked. */
function gridRows(...rows: string[]): CollisionGrid {
  const w = rows[0].length, h = rows.length;
  const b = new Uint8Array(22 + Math.ceil((w * h) / 8));
  b.set([...'LCOL'].map((c) => c.charCodeAt(0)));
  b[4] = 1;
  b[6] = 10;
  b[8] = 64;
  b[10] = w;
  b[12] = h;
  rows.forEach((row, r) => [...row].forEach((ch, c) => {
    if (ch === '#') {
      const i = r * w + c;
      b[22 + (i >> 3)] |= 1 << (i & 7);
    }
  }));
  return CollisionGrid.parse(b);
}

describe('collision.bin reader (AC1)', () => {
  const grid = CollisionGrid.parse(new Uint8Array(readFileSync(join(__dirname, '../../../shared/map/collision.bin'))));

  it('reads the header', () => {
    expect(grid.cellSize).toBe(1);
    expect(grid.width).toBeGreaterThan(0);
    expect(grid.height).toBeGreaterThan(0);
  });

  it('spawn is walkable', () => {
    expect(grid.blocked(-22, -76)).toBe(false);
  });

  it('a cell inside a building is blocked', () => {
    // Inside OSM building way 35598840 (footprint centre about (313, -471)).
    expect(grid.blocked(313.5, -470.5)).toBe(true);
  });

  it('positions outside the grid are blocked', () => {
    const maxX = grid.originX + grid.width * grid.cellSize;
    const maxZ = grid.originZ + grid.height * grid.cellSize;
    expect(grid.blocked(grid.originX - 0.5, -76)).toBe(true);
    expect(grid.blocked(maxX + 0.5, -76)).toBe(true);
    expect(grid.blocked(-22, grid.originZ - 0.5)).toBe(true);
    expect(grid.blocked(-22, maxZ + 0.5)).toBe(true);
    expect(grid.blocked(NaN, 0)).toBe(true);
  });

  it('rejects malformed files', () => {
    expect(() => CollisionGrid.parse(new Uint8Array(10))).toThrow();
    const bad = new Uint8Array(readFileSync(join(__dirname, '../../../shared/map/collision.bin')));
    bad[0] = 0;
    expect(() => CollisionGrid.parse(bad)).toThrow(/magic/);
  });
});

/**
 * Walks toward `to` like the server's movement system ticking at 20 Hz with MaxSpeed 6 for 60
 * ticks (movement_test.go TestMovementCollision): a fully blocked step stops the move.
 */
function walk(g: CollisionGrid, start: Vec, to: Vec): Vec {
  const maxStep = 6 / 20;
  let pos = { ...start };
  for (let i = 0; i < 60; i++) {
    const dx = to.x - pos.x, dz = to.z - pos.z;
    const dist = Math.hypot(dx, dz);
    if (dist === 0) break;
    const k = Math.min(1, maxStep / dist);
    const want = k === 1 ? { ...to } : { x: pos.x + dx * k, z: pos.z + dz * k };
    const next = slide(g, pos, want);
    if (next.x === pos.x && next.z === pos.z) break;
    pos = next;
  }
  return pos;
}

describe('slide matches the server movement rule (AC2)', () => {
  // Same cases and expected positions as server/internal/systems/movement/movement_test.go.
  const cases: { name: string; rows: string[]; start: Vec; to: Vec; check: (p: Vec) => boolean }[] = [
    { name: 'head-on wall stops', rows: ['...#..', '...#..', '...#..', '...#..', '...#..', '...#..'],
      start: { x: 2.5, z: 2.5 }, to: { x: 5.5, z: 2.5 }, check: (p) => p.x < 3 && p.x > 2.9 && p.z === 2.5 },
    { name: 'diagonal slides along wall in X', rows: ['......', '......', '......', '######', '......', '......'],
      start: { x: 1.5, z: 2.5 }, to: { x: 4.5, z: 5.5 }, check: (p) => p.x > 4.4 && p.z < 3 && p.z >= 2.5 },
    { name: 'corner slides around single cell', rows: ['......', '......', '......', '...#..', '......', '......'],
      start: { x: 2.5, z: 2.5 }, to: { x: 3.5, z: 3.5 }, check: (p) => p.x === 3.5 && p.z < 3 },
    { name: 'enclosed stays put', rows: ['###', '#.#', '###'],
      start: { x: 1.5, z: 1.5 }, to: { x: 1.9, z: 1.9 }, check: (p) => p.x >= 1 && p.x < 2 && p.z >= 1 && p.z < 2 },
    { name: 'leaving the grid is blocked', rows: ['...', '...', '...'],
      start: { x: 2.5, z: 1.5 }, to: { x: 6, z: 1.5 }, check: (p) => p.x < 3 && p.z === 1.5 },
    { name: 'short step into wall is blocked', rows: ['...#..', '...#..', '...#..'],
      start: { x: 2.9, z: 1.5 }, to: { x: 3.1, z: 1.5 }, check: (p) => p.x < 3 && p.z === 1.5 },
    { name: 'open ground unaffected (free move)', rows: ['....', '....', '....'],
      start: { x: 0.5, z: 0.5 }, to: { x: 3.5, z: 2.5 }, check: (p) => p.x === 3.5 && p.z === 2.5 },
    // Client-side additions for the remaining AC2 cases.
    { name: 'diagonal slides along wall in Z', rows: ['...#..', '...#..', '...#..', '...#..', '...#..', '...#..'],
      start: { x: 2.5, z: 0.5 }, to: { x: 5.5, z: 4.5 }, check: (p) => p.x < 3 && p.x >= 2.5 && p.z > 4.4 },
    { name: 'corner stop', rows: ['.#', '##'],
      start: { x: 0.5, z: 0.5 }, to: { x: 1.5, z: 1.5 }, check: (p) => p.x < 1 && p.z < 1 },
  ];
  for (const tc of cases) {
    it(tc.name, () => {
      const p = walk(gridRows(...tc.rows), tc.start, tc.to);
      expect(tc.check(p), `final pos ${JSON.stringify(p)}`).toBe(true);
    });
  }

  it('a single long step cannot jump a one-cell wall', () => {
    // 3 m in one step: without subdivision both ends are free and the wall at x=3 would be skipped.
    const g = gridRows('...#...', '...#...', '...#...');
    const p = slide(g, { x: 1.5, z: 1.5 }, { x: 4.5, z: 1.5 });
    expect(p.x).toBeLessThan(3);
    expect(p.x).toBeGreaterThan(2.5);
    expect(p.z).toBe(1.5);
  });

  it('a fully blocked step does not move', () => {
    const g = gridRows('...#..', '...#..', '...#..');
    expect(slide(g, { x: 2.9, z: 1.5 }, { x: 3.1, z: 1.5 })).toEqual({ x: 2.9, z: 1.5 });
  });
});

describe('prediction with collision (AC2, AC3)', () => {
  const wall = gridRows('...#..', '...#..', '...#..', '...#..');

  it('stops at a wall instead of walking through it', () => {
    const p = new Prediction({ x: 2.5, z: 1.5 }, wall);
    for (let i = 0; i < 20; i++) p.applyInput(1, 0, 0.05);
    expect(p.pos.x).toBeLessThan(3);
    expect(p.pos.x).toBeGreaterThan(2.5);
  });

  it('slides along a wall when moving diagonally', () => {
    const p = new Prediction({ x: 2.5, z: 0.5 }, wall);
    for (let i = 0; i < 5; i++) p.applyInput(1, 1, 0.05);
    expect(p.pos.x).toBeLessThan(3);
    expect(p.pos.z).toBeGreaterThan(1);
  });

  it('replays unacknowledged inputs through collision on reconcile', () => {
    const p = new Prediction({ x: 1.5, z: 1.5 }, wall);
    for (let i = 0; i < 10; i++) p.applyInput(1, 0, 0.05);
    p.reconcile({ x: 2.5, z: 1.5 }, 2);
    expect(p.pendingCount).toBe(8);
    expect(p.pos.x).toBeLessThan(3);
  });

  it('moves freely without a grid', () => {
    const p = new Prediction({ x: 2.5, z: 1.5 });
    for (let i = 0; i < 20; i++) p.applyInput(1, 0, 0.05);
    expect(p.pos.x).toBeCloseTo(2.5 + MOVE_SPEED);
  });

  it('GameState hands its grid to prediction, before and after Welcome', () => {
    const g = new GameState();
    g.setCollision(wall);
    expect(g.prediction.grid).toBe(wall);
    g.handle({ type: 'welcome', playerId: 1, guestToken: 't', spawn: { x: 2.5, z: 1.5 }, tickRate: 20 }, 0);
    expect(g.prediction.grid).toBe(wall);
    for (let i = 0; i < 20; i++) g.prediction.applyInput(1, 0, 0.05);
    expect(g.local.x).toBeLessThan(3);
  });
});
