import type { QuestStore } from '../state/quests';
import { hudLayout, place } from './hudLayout';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

/** The most reply buttons shown (spec: Talking NPCs). */
export const MAX_REPLIES = 4;

export interface DialoguePanelOptions {
  parent: HTMLElement;
  quests: QuestStore;
  /** The NPC's display name for its entity id. */
  npcName: (npcId: number) => string;
  /** Sends DialogueReply with the picked index (0 continues a dialogue with no replies). */
  sendReply: (index: number) => void;
  viewport?: () => { width: number; height: number };
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls: string, text = ''): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = cls;
  e.textContent = text;
  return e;
}

/** Dialogue panel: the NPC's name, its lines and up to 4 reply buttons for the open dialogue in QuestStore. */
export class DialoguePanel {
  readonly panel: HTMLElement;
  private readonly title: HTMLElement;
  private readonly body: HTMLElement;
  private readonly motion: PanelMotion;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: DialoguePanelOptions) {
    injectStyle();
    this.panel = el('div', 'lush-panel lush-dialogue');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Dialogue');
    this.panel.hidden = true;
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    const close = el('button', `lush-dialogue-close ${BUTTON_CLASS}`, 'Close');
    close.type = 'button';
    close.addEventListener('click', () => this.close());
    this.title = el('span', 'lush-title lush-dialogue-title');
    const head = el('div', 'lush-dialogue-head');
    head.append(this.title, close);
    this.body = el('div', 'lush-dialogue-body');
    this.panel.append(head, this.body);
    opts.parent.append(this.panel);
    this.unsubscribe = opts.quests.subscribe(() => this.sync());
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.sync();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  /** Closes the panel and the dialogue in the store. */
  close(): void {
    this.opts.quests.closeDialogue();
    this.motion.set(false);
  }

  /** The local player sent a move: moving away closes the dialogue (OSRS). */
  playerMoved(): void {
    if (this.isOpen) this.close();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.panel, hudLayout(v.width, v.height).shop);
  }

  private sync(): void {
    const d = this.opts.quests.dialogue;
    if (!d) {
      this.motion.set(false);
      return;
    }
    this.title.textContent = this.opts.npcName(d.npcId);
    const lines = d.lines.map((t) => el('p', 'lush-dialogue-line', t));
    const replies = d.replies.slice(0, MAX_REPLIES);
    // No replies: a single Continue button, which sends reply 0.
    const labels = replies.length > 0 ? replies : ['Continue'];
    const buttons = labels.map((label, i) => {
      const b = el('button', `lush-dialogue-reply ${BUTTON_CLASS}`, label);
      b.type = 'button';
      b.dataset.reply = String(i);
      b.addEventListener('click', () => this.opts.sendReply(i));
      return b;
    });
    const row = el('div', 'lush-dialogue-replies');
    row.append(...buttons);
    this.body.replaceChildren(...lines, row);
    this.motion.set(true);
  }

  dispose(): void {
    this.unsubscribe();
    this.motion.dispose();
    window.removeEventListener('resize', this.onResize);
    this.panel.remove();
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-dialogue { box-sizing: border-box; z-index: 7; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: var(--lush-text-sm); line-height: 16px; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-dialogue[hidden] { display: none; }
.lush-dialogue-head { display: flex; align-items: center; justify-content: space-between; gap: var(--lush-space-2); }
.lush-dialogue-title { font-size: var(--lush-text-lg); }
.lush-dialogue-body { display: flex; flex-direction: column; gap: var(--lush-space-2); }
.lush-dialogue-line { margin: 0; padding: var(--lush-space-1) var(--lush-space-2); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); }
.lush-dialogue-replies { display: flex; flex-direction: column; gap: var(--lush-space-1); }
.lush-dialogue-reply { background: var(--lush-active); }
`;
  document.head.append(s);
}
