import { describe, expect, it } from 'vitest';
import { Joystick, stickVector, toWorldMove } from './joystick';
import { KeyMovement } from './desktop';

describe('stickVector', () => {
  it('is zero inside the dead zone and unit at the rim', () => {
    expect(stickVector(2, 2, 50)).toEqual({ x: 0, y: 0 });
    const v = stickVector(500, 0, 50);
    expect(v.x).toBeCloseTo(1);
    expect(v.y).toBeCloseTo(0);
  });
});

describe('toWorldMove', () => {
  it('stick up moves away from the camera', () => {
    const n = toWorldMove({ x: 0, y: -1 }, 0);
    expect(n.x).toBeCloseTo(0);
    expect(n.y).toBeCloseTo(-1); // north
    const e = toWorldMove({ x: 0, y: -1 }, Math.PI / 2);
    expect(e.x).toBeCloseTo(1); // camera faces east
    expect(e.y).toBeCloseTo(0);
  });
  it('stick right strafes right of the camera', () => {
    const v = toWorldMove({ x: 1, y: 0 }, 0);
    expect(v.x).toBeCloseTo(1);
    const w = toWorldMove({ x: 1, y: 0 }, Math.PI);
    expect(w.x).toBeCloseTo(-1);
  });
  it('preserves magnitude', () => {
    const v = toWorldMove({ x: 0.3, y: -0.4 }, 1.234);
    expect(Math.hypot(v.x, v.y)).toBeCloseTo(0.5);
  });
});

describe('Joystick multi-touch', () => {
  it('ignores other pointers', () => {
    const j = new Joystick(50);
    expect(j.down(1, 100, 100)).toBe(true);
    expect(j.down(2, 300, 300)).toBe(false);
    j.move(2, 0, 0);
    expect(j.value).toEqual({ x: 0, y: 0 });
    j.move(1, 100, 50);
    expect(j.value.y).toBeCloseTo(-1);
    j.up(2);
    expect(j.active).toBe(true);
    j.up(1);
    expect(j.active).toBe(false);
    expect(j.value).toEqual({ x: 0, y: 0 });
  });
});

describe('KeyMovement', () => {
  it('maps WASD and arrows, normalizing diagonals', () => {
    const k = new KeyMovement();
    k.keyDown('W');
    k.keyDown('ArrowRight');
    expect(k.vector.x).toBeCloseTo(Math.SQRT1_2);
    expect(k.vector.y).toBeCloseTo(-Math.SQRT1_2);
    k.keyUp('w');
    expect(k.vector).toEqual({ x: 1, y: 0 });
    expect(k.keyDown('q')).toBe(false);
  });
});
