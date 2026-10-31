import { sunTimes } from '../core/sun';
import type { SharedClock } from '../state/clock';

/** The developer time control's presets (visual-style.md, "Day and night"). */
export type TimePreset = 'dawn' | 'noon' | 'dusk' | 'night';

export const TIME_PRESETS: readonly TimePreset[] = ['dawn', 'noon', 'dusk', 'night'];

const LABELS: Record<TimePreset, string> = { dawn: 'Dawn', noon: 'Noon', dusk: 'Dusk', night: 'Night' };

const MIN = 60_000;
const HOUR = 60 * MIN;

/**
 * UTC ms for a preset on the Irvine date that contains utcMs: just after sunrise, solar noon
 * (halfway between sunrise and sunset), just before sunset, and four hours before sunrise (so
 * still the same date, deep in the night).
 */
export function presetTime(preset: TimePreset, utcMs: number): number {
  const { sunrise, sunset } = sunTimes(utcMs);
  switch (preset) {
    case 'dawn':
      return sunrise + 15 * MIN;
    case 'noon':
      return Math.round((sunrise + sunset) / 2);
    case 'dusk':
      return sunset - 15 * MIN;
    case 'night':
      return sunrise - 4 * HOUR;
  }
}

/** True when the URL's query string has `debug` (with or without a value). */
export function debugEnabled(search: string): boolean {
  return new URLSearchParams(search).has('debug');
}

export interface TimePanelOptions {
  parent: HTMLElement;
  clock: SharedClock;
}

/** Developer-only time control: Dawn, Noon, Dusk and Night override the shared time; Pause freezes it; Live goes back. */
export class TimePanel {
  readonly root: HTMLDivElement;
  readonly pauseButton: HTMLButtonElement;
  private readonly buttons = new Map<TimePreset, HTMLButtonElement>();

  constructor(private readonly opts: TimePanelOptions) {
    injectStyle();
    this.root = document.createElement('div');
    this.root.className = 'lush-time-panel';
    this.root.setAttribute('aria-label', 'Debug time control');
    for (const p of TIME_PRESETS) {
      const b = button(LABELS[p], () => this.set(p));
      b.dataset.preset = p;
      this.buttons.set(p, b);
      this.root.append(b);
    }
    this.pauseButton = button('Pause', () => this.togglePause());
    this.pauseButton.dataset.action = 'pause';
    const live = button('Live', () => {
      opts.clock.live();
      this.refresh();
    });
    live.dataset.action = 'live';
    this.root.append(this.pauseButton, live);
    opts.parent.append(this.root);
    this.refresh();
  }

  /** Overrides the shared time with a preset on the current Irvine date (by the server's clock). */
  set(preset: TimePreset): void {
    this.opts.clock.override(presetTime(preset, this.opts.clock.serverNow()));
    this.refresh();
  }

  togglePause(): void {
    if (this.opts.clock.paused) this.opts.clock.resume();
    else this.opts.clock.pause();
    this.refresh();
  }

  dispose(): void {
    this.root.remove();
  }

  private refresh(): void {
    const paused = this.opts.clock.paused;
    this.pauseButton.textContent = paused ? 'Resume' : 'Pause';
    this.pauseButton.setAttribute('aria-pressed', String(paused));
  }
}

/** Mounts the time panel only when the URL has `?debug`; otherwise nothing is added to the DOM. */
export function mountTimePanel(search: string, opts: TimePanelOptions): TimePanel | null {
  return debugEnabled(search) ? new TimePanel(opts) : null;
}

function button(label: string, onClick: () => void): HTMLButtonElement {
  const b = document.createElement('button');
  b.type = 'button';
  b.textContent = label;
  b.addEventListener('click', onClick);
  return b;
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-time-panel { position: fixed; top: 80px; left: 50%; transform: translateX(-50%); z-index: 20; display: flex; gap: 4px; padding: 4px; border-radius: 8px; background: rgba(20, 16, 12, 0.8); }
.lush-time-panel button { border: 1px solid #a07a40; border-radius: 6px; background: #3a2c1c; color: #f4e4c4; font: 600 12px serif; padding: 4px 8px; touch-action: manipulation; }
.lush-time-panel button[aria-pressed="true"] { background: #6a4a24; }
`;
  document.head.append(s);
}
