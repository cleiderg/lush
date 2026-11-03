import { effectivePreset, QualityChooser, type QualityChoice, type QualityPreset, type QualityStore } from './quality';

/** The part of GameRenderer the controller drives. */
export interface QualityTarget {
  setQuality(preset: QualityPreset): void;
}

/**
 * Joins the stored Quality choice, the automatic chooser and the renderer: a stored Low, Medium or High applies at
 * once; Auto applies the chooser's preset, which steps down as frame times arrive.
 */
export class QualityController {
  private current: QualityChoice;
  private lastMs: number | null = null;

  constructor(
    private readonly target: QualityTarget,
    private readonly store: QualityStore,
    private readonly chooser: QualityChooser,
  ) {
    this.current = store.get();
    this.apply();
  }

  get choice(): QualityChoice {
    return this.current;
  }

  /** The preset the renderer is using. */
  get preset(): QualityPreset {
    return effectivePreset(this.current, this.chooser.preset);
  }

  setChoice(choice: QualityChoice): void {
    this.current = choice;
    this.store.set(choice);
    this.apply();
  }

  /** Feed one rendered frame's time in seconds. */
  frame(dt: number): void {
    this.chooser.frame(dt);
    this.apply();
  }

  /** Call once per rendered frame with the clock in ms; the time since the previous call is fed to the chooser. */
  rendered(nowMs: number): void {
    if (this.lastMs !== null) this.frame((nowMs - this.lastMs) / 1000);
    this.lastMs = nowMs;
  }

  /**
   * Call when the page is hidden: no frames are drawn until it is shown again, so the gap before the next frame is
   * not fed to the chooser (it would read as one very slow frame and step Auto down).
   */
  pause(): void {
    this.lastMs = null;
  }

  private apply(): void {
    this.target.setQuality(this.preset);
  }
}
