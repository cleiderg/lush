import { type CollisionGrid, slide } from './collision';

/**
 * Default walk and run speeds in metres per second, decided in PRD D-044.
 * The server is the source of truth: GameState replaces these with the values Welcome sends.
 */
export const WALK_SPEED = 4;
export const RUN_SPEED = 7;

export interface PendingInput {
  seq: number;
  moveX: number;
  moveZ: number;
  dt: number;
  /** Metres per second this input was sent with, so replays match what the server applied. */
  speed: number;
}

export interface Pos {
  x: number;
  z: number;
}

/** Moves p by one input; with a grid it stops and slides at walls like the server. */
function step(p: Pos, i: PendingInput, grid: CollisionGrid | null): void {
  const len = Math.hypot(i.moveX, i.moveZ);
  const k = len > 1 ? 1 / len : 1;
  const to = { x: p.x + i.moveX * k * i.speed * i.dt, z: p.z + i.moveZ * k * i.speed * i.dt };
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
  applyInput(moveX: number, moveZ: number, dt: number, speed = WALK_SPEED): PendingInput {
    const input = { seq: this.nextSeq++, moveX, moveZ, dt, speed };
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
