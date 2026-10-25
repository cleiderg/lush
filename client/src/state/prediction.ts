import { type CollisionGrid, slide } from './collision';

/**
 * Walk speed in metres per second, decided in PRD D-044 (walk 4 m/s, run 7 m/s).
 * The server is the source of truth: this must match the server's LUSH_MOVE_SPEED default,
 * and will be replaced by the value the server sends in Welcome.
 */
export const MOVE_SPEED = 4;

export interface PendingInput {
  seq: number;
  moveX: number;
  moveZ: number;
  dt: number;
}

export interface Pos {
  x: number;
  z: number;
}

/** Moves p by one input; with a grid it stops and slides at walls like the server. */
function step(p: Pos, i: PendingInput, grid: CollisionGrid | null): void {
  const len = Math.hypot(i.moveX, i.moveZ);
  const k = len > 1 ? 1 / len : 1;
  const to = { x: p.x + i.moveX * k * MOVE_SPEED * i.dt, z: p.z + i.moveZ * k * MOVE_SPEED * i.dt };
  const next = grid ? slide(grid, p, to) : to;
  p.x = next.x;
  p.z = next.z;
}

/** Client-side prediction: apply inputs now, rewind to the server's state and replay unacknowledged ones. */
export class Prediction {
  readonly pos: Pos;
  private nextSeq = 1;
  private pending: PendingInput[] = [];

  /** Without a grid (null), movement is free. */
  constructor(spawn: Pos = { x: 0, z: 0 }, public grid: CollisionGrid | null = null) {
    this.pos = { x: spawn.x, z: spawn.z };
  }

  get pendingCount(): number {
    return this.pending.length;
  }

  /** Numbers and applies an input immediately. Returns it so the caller can send it. */
  applyInput(moveX: number, moveZ: number, dt: number): PendingInput {
    const input = { seq: this.nextSeq++, moveX, moveZ, dt };
    this.pending.push(input);
    step(this.pos, input, this.grid);
    return input;
  }

  /** Server says: at `lastInputSeq` you were at `server`. Drop acknowledged inputs and replay the rest. */
  reconcile(server: Pos, lastInputSeq: number): void {
    this.pending = this.pending.filter((i) => i.seq > lastInputSeq);
    this.pos.x = server.x;
    this.pos.z = server.z;
    for (const i of this.pending) step(this.pos, i, this.grid);
  }
}
