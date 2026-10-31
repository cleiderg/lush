import { promptString, type InteractPromptText } from '../core/interact';
import { hudLayout, place } from './hudLayout';

export interface InteractPromptOptions {
  parent: HTMLElement;
  /** What Interact will do now (interactPromptText of interactTarget), or null for nothing. */
  prompt: () => InteractPromptText | null;
  viewport?: () => { width: number; height: number };
}

function span(cls: string): HTMLSpanElement {
  const e = document.createElement('span');
  e.className = cls;
  return e;
}

/** Top-centre prompt saying what the Interact button will do, the name in gold (visual-style.md, Interact prompt). */
export class InteractPrompt {
  readonly root: HTMLDivElement;
  readonly verb: HTMLSpanElement;
  readonly name: HTMLSpanElement;
  readonly detail: HTMLSpanElement;
  /** The shown text ('' for hidden); null before the first update. */
  private shown: string | null = null;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: InteractPromptOptions) {
    injectStyle();
    this.root = document.createElement('div');
    this.root.className = 'lush-panel lush-interact';
    this.root.setAttribute('role', 'status');
    this.root.setAttribute('aria-label', 'Interact');
    this.verb = span('lush-interact-verb');
    this.name = span('lush-interact-name');
    this.detail = span('lush-title lush-interact-detail');
    opts.parent.append(this.root);
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.update();
  }

  get visible(): boolean {
    return !this.root.hidden;
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.root, hudLayout(v.width, v.height).interact);
  }

  /** Call once per tick; the DOM changes only when the prompt does. */
  update(): void {
    const p = this.opts.prompt();
    const key = p ? promptString(p) : '';
    if (key === this.shown) return;
    this.shown = key;
    this.root.hidden = !p;
    this.verb.textContent = p?.verb ?? '';
    this.name.textContent = p?.name ?? '';
    this.detail.textContent = p?.detail ?? '';
    // Spaces as text so the prompt reads as one line ('Attack Coyote · Lvl 9').
    const parts: (Node | string)[] = [this.verb];
    if (p?.name) parts.push(' ', this.name);
    if (p?.detail) parts.push(' ', this.detail);
    this.root.replaceChildren(...parts);
  }

  dispose(): void {
    window.removeEventListener('resize', this.onResize);
    this.root.remove();
  }
}

/** The prompt's styles; the name uses the theme's gold. */
export const INTERACT_PROMPT_CSS = `
.lush-interact { box-sizing: border-box; z-index: 4; display: block; text-align: center; line-height: 34px; white-space: nowrap; text-overflow: ellipsis; padding: 0 var(--lush-space-3); border-radius: 999px; font-size: var(--lush-text-md); overflow: hidden; pointer-events: none; user-select: none; -webkit-user-select: none; }
.lush-interact[hidden] { display: none; }
.lush-interact-name { color: var(--lush-gold); font-weight: 700; }
.lush-interact-detail { color: var(--lush-text-muted); font-size: var(--lush-text-xs); }
`;

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = INTERACT_PROMPT_CSS;
  document.head.append(s);
}
