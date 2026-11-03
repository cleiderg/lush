/** Quality presets, ordered from lowest to highest. */
export const QUALITY_PRESETS = ['low', 'medium', 'high'] as const;
export type QualityPreset = (typeof QUALITY_PRESETS)[number];
export type QualityChoice = 'auto' | QualityPreset;

/** Q1 / PRD 13 placeholders, tuned after profiling on an iPhone 13. */
export const QUALITY_KEY = 'lush.quality';
export const QUALITY_WARMUP_S = 10;
export const QUALITY_WINDOW_S = 5;
export const QUALITY_MIN_FPS = 27;
/** A phone has a coarse pointer and a short screen side under this many CSS px. */
export const PHONE_MAX_SHORT_SIDE = 600;

export interface DeviceSignals {
  coarsePointer: boolean;
  /** Shorter side of the screen in CSS px. */
  shortSidePx: number;
}

export function isPhone(d: DeviceSignals): boolean {
  return d.coarsePointer && d.shortSidePx < PHONE_MAX_SHORT_SIDE;
}

/** Medium on phones, High everywhere else. */
export function startingPreset(d: DeviceSignals): QualityPreset {
  return isPhone(d) ? 'medium' : 'high';
}

/** Reads the signals from the browser. */
export function detectDevice(): DeviceSignals {
  const coarse = typeof matchMedia === 'function' && matchMedia('(pointer: coarse)').matches;
  const s = typeof screen !== 'undefined' ? screen : undefined;
  const short = s ? Math.min(s.width, s.height) : Math.min(window.innerWidth, window.innerHeight);
  return { coarsePointer: coarse, shortSidePx: short };
}

/**
 * Steps the preset down one level when the average frame rate over the last
 * QUALITY_WINDOW_S seconds stays under QUALITY_MIN_FPS, once QUALITY_WARMUP_S
 * has passed. It never steps up and never goes below Low.
 */
export class QualityChooser {
  private current: QualityPreset;
  private elapsed = 0;
  private window: number[] = [];
  private windowSum = 0;

  constructor(start: QualityPreset) {
    this.current = start;
  }

  get preset(): QualityPreset {
    return this.current;
  }

  /** Feed one frame time in seconds. Returns the (possibly new) automatic preset. */
  frame(dt: number): QualityPreset {
    if (!(dt > 0)) return this.current;
    this.elapsed += dt;
    if (this.elapsed <= QUALITY_WARMUP_S) return this.current;
    this.window.push(dt);
    this.windowSum += dt;
    // Keep only the last QUALITY_WINDOW_S seconds of frames.
    while (this.window.length > 1 && this.windowSum - this.window[0]! >= QUALITY_WINDOW_S) {
      this.windowSum -= this.window.shift()!;
    }
    if (this.windowSum >= QUALITY_WINDOW_S) {
      const fps = this.window.length / this.windowSum;
      if (fps < QUALITY_MIN_FPS) {
        const i = QUALITY_PRESETS.indexOf(this.current);
        if (i > 0) this.current = QUALITY_PRESETS[i - 1]!;
        // Judge the new preset on fresh frames.
        this.window = [];
        this.windowSum = 0;
      }
    }
    return this.current;
  }
}

/** The preset to render with: a stored Low, Medium or High wins; Auto uses the chooser. */
export function effectivePreset(choice: QualityChoice, auto: QualityPreset): QualityPreset {
  return choice === 'auto' ? auto : choice;
}

export function parseChoice(v: string | null): QualityChoice {
  return v === 'low' || v === 'medium' || v === 'high' ? v : 'auto';
}

export interface QualityStore {
  get(): QualityChoice;
  set(choice: QualityChoice): void;
}

/** The override in localStorage; storage can throw (private mode, disabled), so every access is guarded. */
export function localQualityStore(): QualityStore {
  return {
    get() {
      try {
        return parseChoice(localStorage.getItem(QUALITY_KEY));
      } catch {
        return 'auto';
      }
    },
    set(choice) {
      try {
        localStorage.setItem(QUALITY_KEY, choice);
      } catch {
        /* ignore */
      }
    },
  };
}
