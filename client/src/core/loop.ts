export const STEP_MS = 50; // fixed update step (20 Hz)
export const MAX_FPS = 30;
const MIN_RENDER_INTERVAL_MS = 1000 / MAX_FPS;
const MAX_STEPS_PER_FRAME = 5;

export interface LoopOptions {
  update: (stepMs: number) => void;
  render: (alpha: number) => void;
  stepMs?: number;
}

/**
 * Fixed-step update with a render callback capped at 30 fps.
 * Drive it by calling `frame(nowMs)` from requestAnimationFrame (or a fake clock).
 */
export class GameLoop {
  private readonly stepMs: number;
  private last: number | null = null;
  private accumulator = 0;
  private lastRender = -Infinity;

  constructor(private readonly opts: LoopOptions) {
    this.stepMs = opts.stepMs ?? STEP_MS;
  }

  frame(now: number): void {
    if (this.last === null) this.last = now;
    this.accumulator += now - this.last;
    this.last = now;

    let steps = 0;
    while (this.accumulator >= this.stepMs && steps < MAX_STEPS_PER_FRAME) {
      this.opts.update(this.stepMs);
      this.accumulator -= this.stepMs;
      steps++;
    }
    if (steps === MAX_STEPS_PER_FRAME) this.accumulator %= this.stepMs;

    // Float epsilon so a 60 Hz display (16.67 ms ticks) renders every other frame.
    if (now - this.lastRender >= MIN_RENDER_INTERVAL_MS - 0.01) {
      this.lastRender = now;
      this.opts.render(this.accumulator / this.stepMs);
    }
  }

  start(): () => void {
    let id = 0;
    const tick = (t: number) => {
      this.frame(t);
      id = requestAnimationFrame(tick);
    };
    id = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(id);
  }
}
