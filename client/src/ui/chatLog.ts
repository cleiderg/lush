import type { ChatLine } from '../state/chat';
import { hudLayout, place } from './hudLayout';
import { PanelMotion } from './theme';

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

// Lighter than other panels so the scene shows through the log.
const STYLE = 'box-sizing:border-box;z-index:4;display:flex;flex-direction:column;gap:2px;' +
  'overflow:hidden;padding:4px 8px 6px;background:rgba(22,19,17,0.6);font-size:13px;line-height:16px;' +
  'text-shadow:0 1px 2px #000;pointer-events:none;user-select:none;-webkit-user-select:none;';
const BODY_STYLE = 'flex:1;min-height:0;display:flex;flex-direction:column;justify-content:flex-end;overflow:hidden;';

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
  private readonly body: HTMLElement;
  private readonly motion: PanelMotion;
  private readonly rows = new Map<ChatLine, HTMLElement>();
  private last: ChatLine | undefined;
  private count = 0;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: ChatLogOptions) {
    this.el = document.createElement('div');
    this.el.id = 'chat-log';
    this.el.setAttribute('role', 'log');
    this.el.setAttribute('aria-live', 'polite');
    this.el.className = 'lush-panel';
    this.el.style.cssText = STYLE;
    const title = document.createElement('div');
    title.className = 'lush-title';
    title.textContent = 'Chat';
    this.body = document.createElement('div');
    this.body.style.cssText = BODY_STYLE;
    this.el.append(title, this.body);
    // Inline display:flex would override the hidden attribute, so the log toggles display directly.
    this.motion = new PanelMotion(this.el, (shown) => { this.el.style.display = shown ? 'flex' : 'none'; }, !opts.covered?.());
    opts.parent.append(this.el);
    this.layout();
    window.addEventListener('resize', this.onResize);
    this.update();
  }

  dispose(): void {
    window.removeEventListener('resize', this.onResize);
    this.motion.dispose();
    this.el.remove();
  }

  layout(): void {
    const vp = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.el, hudLayout(vp.width, vp.height).chat);
  }

  /** Call once per frame; touches the DOM only when lines arrive or a panel opens or closes. */
  update(): void {
    this.motion.set(!this.opts.covered?.());

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
      who.style.cssText = 'font-weight:600;color:var(--lush-gold-bright);';
      who.textContent = `${speakerLabel(line.speakerId, me)}: `;
      const text = document.createElement('span');
      text.textContent = line.text;
      row.append(who, text);
      this.body.append(row);
      this.rows.set(line, row);
    }
  }
}
