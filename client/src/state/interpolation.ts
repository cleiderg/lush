export const INTERP_DELAY_MS = 100;
const MAX_BUFFER = 32;

export interface RemoteState {
  id: number;
  x: number;
  z: number;
  heading: number;
}

interface Frame {
  time: number;
  entities: Map<number, RemoteState>;
}

function lerpAngle(a: number, b: number, t: number): number {
  const tau = Math.PI * 2;
  let d = (b - a) % tau;
  if (d > Math.PI) d -= tau;
  else if (d < -Math.PI) d += tau;
  return a + d * t;
}

/** Buffers snapshots by arrival time and samples remote players ~100 ms in the past. */
export class Interpolator {
  private frames: Frame[] = [];

  constructor(private readonly delayMs = INTERP_DELAY_MS) {}

  push(time: number, entities: readonly RemoteState[]): void {
    this.frames.push({ time, entities: new Map(entities.map((e) => [e.id, e])) });
    if (this.frames.length > MAX_BUFFER) this.frames.shift();
  }

  clear(): void {
    this.frames = [];
  }

  sample(now: number): RemoteState[] {
    const fs = this.frames;
    if (fs.length === 0) return [];
    const t = now - this.delayMs;
    if (t <= fs[0].time) return [...fs[0].entities.values()];
    let i = 0;
    while (i < fs.length - 2 && fs[i + 1].time < t) i++;
    const a = fs[i];
    const b = fs[Math.min(i + 1, fs.length - 1)];
    // Drop frames we have passed, keeping `a` as the lower bound.
    if (i > 0) this.frames = fs.slice(i);
    if (t >= b.time || b === a) return [...b.entities.values()];
    const f = (t - a.time) / (b.time - a.time);
    const out: RemoteState[] = [];
    for (const eb of b.entities.values()) {
      const ea = a.entities.get(eb.id);
      if (!ea) {
        out.push(eb);
        continue;
      }
      out.push({
        id: eb.id,
        x: ea.x + (eb.x - ea.x) * f,
        z: ea.z + (eb.z - ea.z) * f,
        heading: lerpAngle(ea.heading, eb.heading, f),
      });
    }
    return out;
  }
}
