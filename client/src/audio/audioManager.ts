const GESTURES = ['pointerdown', 'touchend', 'keydown'] as const;

export interface AudioContextLike {
  state: string;
  resume(): Promise<void>;
}

export interface AudioTarget {
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
}

export interface AudioManagerOptions<C extends AudioContextLike> {
  factory: () => C;
  window: AudioTarget;
  document: AudioTarget & { visibilityState: string };
}

/** Creates the AudioContext only after the first user gesture (iOS Safari and mobile Chrome require it). */
export class AudioManager<C extends AudioContextLike = AudioContextLike> {
  private ctx: C | null = null;
  private unlocked = false;
  private readonly onGesture = (): void => this.unlock();
  private readonly onVisibility = (): void => {
    if (!this.ctx || this.opts.document.visibilityState !== 'visible') return;
    if (this.ctx.state === 'suspended' || this.ctx.state === 'interrupted') {
      void this.ctx.resume().catch(() => undefined);
    }
  };

  constructor(private readonly opts: AudioManagerOptions<C>) {
    for (const g of GESTURES) opts.window.addEventListener(g, this.onGesture);
    opts.document.addEventListener('keydown', this.onGesture);
    opts.document.addEventListener('visibilitychange', this.onVisibility);
  }

  isUnlocked(): boolean {
    return this.unlocked;
  }

  context(): C | null {
    return this.ctx;
  }

  private unlock(): void {
    if (this.ctx) return;
    for (const g of GESTURES) this.opts.window.removeEventListener(g, this.onGesture);
    this.opts.document.removeEventListener('keydown', this.onGesture);
    const ctx = this.opts.factory();
    this.ctx = ctx;
    void ctx.resume().then(
      () => {
        this.unlocked = true;
      },
      () => undefined,
    );
  }
}
