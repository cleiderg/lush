import type { Vec2 } from './joystick';

const KEY_DIRS: Record<string, Vec2> = {
  w: { x: 0, y: -1 },
  arrowup: { x: 0, y: -1 },
  s: { x: 0, y: 1 },
  arrowdown: { x: 0, y: 1 },
  a: { x: -1, y: 0 },
  arrowleft: { x: -1, y: 0 },
  d: { x: 1, y: 0 },
  arrowright: { x: 1, y: 0 },
};

/** Tracks held WASD/arrow keys and yields a normalized screen-space vector. */
export class KeyMovement {
  private held = new Set<string>();

  /** Returns true if the key is a movement key. */
  keyDown(key: string): boolean {
    const k = key.toLowerCase();
    if (!(k in KEY_DIRS)) return false;
    this.held.add(k);
    return true;
  }

  keyUp(key: string): void {
    this.held.delete(key.toLowerCase());
  }

  clear(): void {
    this.held.clear();
  }

  get vector(): Vec2 {
    let x = 0;
    let y = 0;
    for (const k of this.held) {
      x += KEY_DIRS[k].x;
      y += KEY_DIRS[k].y;
    }
    const len = Math.hypot(x, y);
    return len === 0 ? { x: 0, y: 0 } : { x: x / len, y: y / len };
  }
}
