import type { ChatLine } from '../state/chat';
import { hudLayout, place } from './hudLayout';

export interface ChatLogOptions {
  parent: HTMLElement;
  /** The chat log, oldest first (GameState.chat.log). */
  lines: () => readonly ChatLine[];
  /** The local player's entity id. */
  playerId: () => number;
  /** True while the inventory, shop or trade panel is open; the log hides so it never covers them. */
  covered?: () => boolean;
  viewport?: () => { width: number; height: number };
}

const STYLE = 'box-sizing:border-box;z-index:4;display:flex;flex-direction:column;justify-content:flex-end;' +
  'overflow:hidden;padding:4px 8px;border-radius:8px;background:rgba(0,0,0,0.35);color:#fff;' +
  'font:13px/16px system-ui,sans-serif;text-shadow:0 1px 2px #000;pointer-events:none;user-select:none;' +
  '-webkit-user-select:none;';

/** True while any of the panels is open (for ChatLogOptions.covered). */
export function anyOpen(panels: readonly { readonly isOpen: boolean }[]): boolean {
  return panels.some((p) => p.isOpen);
}

export function speakerLabel(speakerId: number, playerId: number): string {
  return speakerId === playerId ? 'You' : `Player ${speakerId}`;
}

/**
 * The local chat log over the game canvas, just above the control tray (PRD 4.1, 5.1). Lines are
 * plain text and the element ignores pointer events, so camera drags over it reach the canvas.
 */
export class ChatLog {
  readonly el: HTMLElement;
  private readonly rows = new Map<ChatLine, HTMLElement>();
  private last: ChatLine | undefined;
  private count = 0;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: ChatLogOptions) {
    this.el = document.createElement('div');
    this.el.id = 'chat-log';
    this.el.setAttribute('role', 'log');
    this.el.setAttribute('aria-live', 'polite');
    this.el.style.cssText = STYLE;
    opts.parent.append(this.el);
    this.layout();
    window.addEventListener('resize', this.onResize);
    this.update();
  }

  dispose(): void {
    window.removeEventListener('resize', this.onResize);
    this.el.remove();
  }

  layout(): void {
    const vp = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.el, hudLayout(vp.width, vp.height).chat);
  }

  /** Call once per frame; touches the DOM only when lines arrive or a panel opens or closes. */
  update(): void {
    // Inline display:flex would override the hidden attribute, so toggle display directly.
    const display = this.opts.covered?.() ? 'none' : 'flex';
    if (this.el.style.display !== display) this.el.style.display = display;

    const lines = this.opts.lines();
    const last = lines[lines.length - 1];
    if (lines.length === this.count && last === this.last) return;
    this.count = lines.length;
    this.last = last;

    const keep = new Set(lines);
    for (const [line, row] of this.rows) {
      if (!keep.has(line)) {
        row.remove();
        this.rows.delete(line);
      }
    }
    const me = this.opts.playerId();
    for (const line of lines) {
      if (this.rows.has(line)) continue;
      const row = document.createElement('div');
      row.className = 'lush-chat-line';
      row.style.cssText = 'word-break:break-word;';
      const who = document.createElement('span');
      who.style.cssText = 'font-weight:600;color:#ffd54f;';
      who.textContent = `${speakerLabel(line.speakerId, me)}: `;
      const text = document.createElement('span');
      text.textContent = line.text;
      row.append(who, text);
      this.el.append(row);
      this.rows.set(line, row);
    }
  }
}
