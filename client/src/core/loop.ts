export const STEP_MS = 50; // fixed update step (20 Hz)
export const MAX_FPS = 30;
const MIN_RENDER_INTERVAL_MS = 1000 / MAX_FPS;
const MAX_STEPS_PER_FRAME = 5;
// Display timestamps jitter (off-60 Hz panels, 1 ms-coarsened timers), so the
// spacing check is loose; the sliding one-second window enforces the hard cap.
const RENDER_SLACK_MS = 4;

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
  // Times of the last MAX_FPS renders; renderIdx points at the oldest.
  private readonly renderTimes: number[] = new Array<number>(MAX_FPS).fill(-Infinity);
  private renderIdx = 0;

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

    if (this.canRender(now)) {
      this.renderTimes[this.renderIdx] = now;
      this.renderIdx = (this.renderIdx + 1) % MAX_FPS;
      this.opts.render(this.accumulator / this.stepMs);
    }
  }

  private canRender(now: number): boolean {
    const lastRender = this.renderTimes[(this.renderIdx + MAX_FPS - 1) % MAX_FPS];
    if (now - lastRender < MIN_RENDER_INTERVAL_MS - RENDER_SLACK_MS) return false;
    // Never more than MAX_FPS renders in any one-second window.
    return now - this.renderTimes[this.renderIdx] >= 1000;
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
