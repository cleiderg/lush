/**
 * Placeholder walk speed in metres per second. The PRD has not decided the real value
 * (the server takes it from config); keep in sync with the server's MaxSpeed.
 */
export const MOVE_SPEED = 6;

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

function step(p: Pos, i: PendingInput): void {
  const len = Math.hypot(i.moveX, i.moveZ);
  const k = len > 1 ? 1 / len : 1;
  p.x += i.moveX * k * MOVE_SPEED * i.dt;
  p.z += i.moveZ * k * MOVE_SPEED * i.dt;
}

/** Client-side prediction: apply inputs now, rewind to the server's state and replay unacknowledged ones. */
export class Prediction {
  readonly pos: Pos;
  private nextSeq = 1;
  private pending: PendingInput[] = [];

  constructor(spawn: Pos = { x: 0, z: 0 }) {
    this.pos = { x: spawn.x, z: spawn.z };
  }

  get pendingCount(): number {
    return this.pending.length;
  }

  /** Numbers and applies an input immediately. Returns it so the caller can send it. */
  applyInput(moveX: number, moveZ: number, dt: number): PendingInput {
    const input = { seq: this.nextSeq++, moveX, moveZ, dt };
    this.pending.push(input);
    step(this.pos, input);
    return input;
  }

  /** Server says: at `lastInputSeq` you were at `server`. Drop acknowledged inputs and replay the rest. */
  reconcile(server: Pos, lastInputSeq: number): void {
    this.pending = this.pending.filter((i) => i.seq > lastInputSeq);
    this.pos.x = server.x;
    this.pos.z = server.z;
    for (const i of this.pending) step(this.pos, i);
  }
}
