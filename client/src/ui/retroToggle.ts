import { hudLayout, place } from './hudLayout';

export const RETRO_KEY = 'lush.retro';
/** Q2 / PRD 5.2: the retro post-process is off until the player turns it on. */
export const RETRO_DEFAULT = false;

export interface RetroStore {
  get(): boolean;
  set(enabled: boolean): void;
}

/** The Retro choice in localStorage; storage can throw (private mode, disabled), so every access is guarded. */
export function localRetroStore(): RetroStore {
  return {
    get() {
      try {
        const v = localStorage.getItem(RETRO_KEY);
        return v === null ? RETRO_DEFAULT : v === '1';
      } catch {
        return RETRO_DEFAULT;
      }
    },
    set(enabled) {
      try {
        localStorage.setItem(RETRO_KEY, enabled ? '1' : '0');
      } catch {
        /* ignore */
      }
    },
  };
}

/** The part of GameRenderer the toggle drives. */
export interface RetroTarget {
  setRetro(enabled: boolean): void;
}

export interface RetroToggleOptions {
  parent: HTMLElement;
  target: RetroTarget;
  store: RetroStore;
  /** Current viewport size; defaults to the window's. */
  viewport?: () => { width: number; height: number };
}

/** The HUD's Retro button (Q2): applies the saved choice at startup and flips it on tap. */
export class RetroToggle {
  readonly button: HTMLButtonElement;
  private on: boolean;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: RetroToggleOptions) {
    injectStyle();
    this.button = document.createElement('button');
    this.button.type = 'button';
    this.button.className = 'lush-retro-toggle';
    this.button.textContent = 'Retro';
    this.button.setAttribute('aria-label', 'Retro look');
    this.button.addEventListener('click', () => this.set(!this.on));
    this.on = opts.store.get();
    this.apply();
    opts.parent.append(this.button);
    window.addEventListener('resize', this.onResize);
    this.layout();
  }

  get enabled(): boolean {
    return this.on;
  }

  set(enabled: boolean): void {
    this.on = enabled;
    this.opts.store.set(enabled);
    this.apply();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.button, hudLayout(v.width, v.height).retro);
  }

  dispose(): void {
    window.removeEventListener('resize', this.onResize);
    this.button.remove();
  }

  private apply(): void {
    this.opts.target.setRetro(this.on);
    this.button.setAttribute('aria-pressed', String(this.on));
    this.button.classList.toggle('on', this.on);
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-retro-toggle { box-sizing: border-box; z-index: 5; border: 0; border-radius: 12px; background: #3b3b52; color: #fff; font: 600 12px sans-serif; touch-action: manipulation; }
.lush-retro-toggle.on { background: #4a8f4a; }
`;
  document.head.append(s);
}
