import type { AudioContextLike, AudioTarget } from './audioManager';

/** The parts of an AudioContext the click needs; a real AudioContext satisfies it. */
export interface ClickContext extends AudioContextLike {
  readonly currentTime: number;
  readonly destination: unknown;
  createOscillator(): {
    type: string;
    frequency: { setValueAtTime(v: number, t: number): unknown; exponentialRampToValueAtTime(v: number, t: number): unknown };
    connect(node: never): unknown;
    start(t?: number): void;
    stop(t?: number): void;
  };
  createGain(): {
    gain: { setValueAtTime(v: number, t: number): unknown; exponentialRampToValueAtTime(v: number, t: number): unknown };
    connect(node: never): unknown;
  };
}

/** How long one click lasts, in seconds. */
export const CLICK_SECONDS = 0.04;
const CLICK_GAIN = 0.12;

/** One soft synthesized click: a short sine blip falling in pitch, fading out fast. No audio files. */
export function playClick(ctx: ClickContext): void {
  const t = ctx.currentTime;
  const osc = ctx.createOscillator();
  const gain = ctx.createGain();
  osc.type = 'sine';
  osc.frequency.setValueAtTime(1400, t);
  osc.frequency.exponentialRampToValueAtTime(600, t + CLICK_SECONDS);
  gain.gain.setValueAtTime(CLICK_GAIN, t);
  gain.gain.exponentialRampToValueAtTime(0.001, t + CLICK_SECONDS);
  osc.connect(gain as never);
  gain.connect(ctx.destination as never);
  osc.start(t);
  osc.stop(t + CLICK_SECONDS);
}

export interface ClickAudio {
  isUnlocked(): boolean;
  context(): ClickContext | null;
}

/**
 * Plays a click whenever a button matching selector is clicked anywhere under root, once audio is unlocked.
 * Before the unlock (the first tap) nothing plays. Returns a function that removes the listener.
 */
export function installUiClicks(root: AudioTarget, audio: ClickAudio, selector: string): () => void {
  const onClick = (e: Event): void => {
    const t = e.target as Element | null;
    const b = typeof t?.closest === 'function' ? t.closest(selector) : null;
    if (!b || (b as HTMLButtonElement).disabled) return;
    const ctx = audio.context();
    if (!ctx || !audio.isUnlocked() || ctx.state !== 'running') return;
    playClick(ctx);
  };
  root.addEventListener('click', onClick);
  return () => root.removeEventListener('click', onClick);
}
