import { ChatKeyboardState, LETTER_ROWS, SYMBOL_ROWS } from '../input/chatKeyboard';
import { KeyMovement } from '../input/desktop';
import { Joystick, toWorldMove, type Vec2 } from '../input/joystick';

export interface ControlsOptions {
  tray: HTMLElement;
  getCameraYaw: () => number;
  onInteract?: () => void;
  onWave?: () => void;
  /** Called with the finished message; sending to the server is the chat issue's job. */
  onChatSubmit?: (message: string) => void;
}

const JOYSTICK_RADIUS = 56;

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  cls: string,
  text = '',
): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = cls;
  e.textContent = text;
  return e;
}

/** Walking-mode controls and chat-mode keyboard living in the control tray. */
export class Controls {
  private readonly stick: Joystick;
  private readonly keys = new KeyMovement();
  private readonly kb = new ChatKeyboardState();
  private chatOpen = false;
  private running = false;
  private knob!: HTMLElement;
  private field!: HTMLElement;
  private keyArea!: HTMLElement;

  constructor(private readonly opts: ControlsOptions) {
    this.stick = new Joystick(JOYSTICK_RADIUS, (v) => this.moveKnob(v));
    this.injectStyle();
    this.showWalking();
    window.addEventListener('keydown', (e) => this.onKeyDown(e));
    window.addEventListener('keyup', (e) => this.keys.keyUp(e.key));
    window.addEventListener('blur', () => this.keys.clear());
  }

  get isChatOpen(): boolean {
    return this.chatOpen;
  }

  get isRunning(): boolean {
    return this.running;
  }

  /** World-space movement (x east, y south), magnitude 0..1, camera relative. */
  worldMove(): Vec2 {
    if (this.chatOpen) return { x: 0, y: 0 };
    const k = this.keys.vector;
    const s = this.stick.value;
    const v = Math.hypot(s.x, s.y) > 0 ? s : k;
    return toWorldMove(v, this.opts.getCameraYaw());
  }

  openChat(): void {
    if (this.chatOpen) return;
    this.chatOpen = true;
    this.keys.clear();
    this.stick.up(this.activePointer);
    this.showChat();
  }

  closeChat(): void {
    if (!this.chatOpen) return;
    this.chatOpen = false;
    this.kb.reset();
    this.showWalking();
  }

  private activePointer = -1;

  private onKeyDown(e: KeyboardEvent): void {
    if (this.chatOpen) {
      if (e.key === 'Enter') this.send();
      else if (e.key === 'Escape') this.closeChat();
      else if (e.key === 'Backspace') this.kb.backspace();
      else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) this.kb.type(e.key);
      else return;
      e.preventDefault();
      this.renderChat();
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      this.openChat();
    } else if (this.keys.keyDown(e.key)) {
      e.preventDefault();
    }
  }

  private send(): void {
    const msg = this.kb.submit();
    if (msg !== null) this.opts.onChatSubmit?.(msg);
    this.closeChat();
  }

  private moveKnob(v: Vec2): void {
    if (!this.knob) return;
    this.knob.style.transform = `translate(${v.x * JOYSTICK_RADIUS}px, ${v.y * JOYSTICK_RADIUS}px)`;
  }

  private showWalking(): void {
    const tray = this.opts.tray;
    tray.replaceChildren();
    const base = el('div', 'lush-stick');
    this.knob = el('div', 'lush-knob');
    base.append(this.knob);
    base.addEventListener('pointerdown', (e) => {
      if (this.stick.down(e.pointerId, e.clientX, e.clientY)) {
        this.activePointer = e.pointerId;
        base.setPointerCapture(e.pointerId);
      }
    });
    base.addEventListener('pointermove', (e) => this.stick.move(e.pointerId, e.clientX, e.clientY));
    const release = (e: PointerEvent): void => this.stick.up(e.pointerId);
    base.addEventListener('pointerup', release);
    base.addEventListener('pointercancel', release);

    const buttons = el('div', 'lush-actions');
    const mk = (label: string, fn: () => void): HTMLButtonElement => {
      const b = el('button', 'lush-btn', label);
      b.type = 'button';
      b.addEventListener('click', fn);
      return b;
    };
    const run = mk('Run', () => {
      this.running = !this.running;
      run.classList.toggle('on', this.running);
      run.setAttribute('aria-pressed', String(this.running));
    });
    run.classList.toggle('on', this.running);
    run.setAttribute('aria-pressed', String(this.running));
    buttons.append(
      mk('Chat', () => this.openChat()),
      mk('Interact', () => this.opts.onInteract?.()),
      mk('Wave', () => this.opts.onWave?.()),
      run,
    );
    tray.classList.remove('chat');
    tray.append(base, buttons);
  }

  private showChat(): void {
    const tray = this.opts.tray;
    tray.replaceChildren();
    tray.classList.add('chat');
    const top = el('div', 'lush-chat-top');
    this.field = el('div', 'lush-field');
    const send = el('button', 'lush-btn', 'Send');
    send.type = 'button';
    send.addEventListener('click', () => this.send());
    const close = el('button', 'lush-btn', '✕');
    close.type = 'button';
    close.setAttribute('aria-label', 'Close chat');
    close.addEventListener('click', () => this.closeChat());
    top.append(this.field, send, close);
    this.keyArea = el('div', 'lush-keys');
    tray.append(top, this.keyArea);
    this.renderChat();
  }

  private renderChat(): void {
    this.field.textContent = this.kb.text;
    this.keyArea.replaceChildren();
    const symbols = this.kb.page === 'symbols';
    const key = (label: string, fn: () => void, cls = ''): HTMLButtonElement => {
      const b = el('button', `lush-key ${cls}`.trim(), label);
      b.type = 'button';
      // Keep taps from stealing focus or starting browser gestures.
      b.addEventListener('pointerdown', (e) => e.preventDefault());
      b.addEventListener('click', () => {
        fn();
        this.renderChat();
      });
      return b;
    };
    const rows = symbols ? SYMBOL_ROWS : LETTER_ROWS;
    rows.forEach((row, i) => {
      const r = el('div', 'lush-row');
      for (const ch of row) {
        const shown = !symbols && this.kb.shift ? ch.toUpperCase() : ch;
        r.append(key(shown, () => this.kb.type(ch)));
      }
      if (i === rows.length - 1) r.append(key('⌫', () => this.kb.backspace(), 'wide'));
      this.keyArea.append(r);
    });
    const last = el('div', 'lush-row');
    last.append(
      key(symbols ? 'ABC' : '123', () => this.kb.toggleSymbols(), 'wide'),
      key('space', () => this.kb.type(' '), 'space'),
    );
    this.keyArea.append(last);
  }

  private injectStyle(): void {
    const s = document.createElement('style');
    s.textContent = `
#control-tray { position: relative; display: flex; justify-content: space-between; align-items: center; padding: 12px 12px calc(12px + env(safe-area-inset-bottom)); box-sizing: border-box; user-select: none; -webkit-user-select: none; touch-action: none; }
#control-tray.chat { flex-direction: column; align-items: stretch; justify-content: flex-start; gap: 6px; }
.lush-stick { width: 128px; height: 128px; border-radius: 50%; background: #2c2c3a; display: flex; align-items: center; justify-content: center; align-self: flex-end; margin-bottom: 8px; touch-action: none; }
.lush-knob { width: 56px; height: 56px; border-radius: 50%; background: #8a8aa8; pointer-events: none; }
.lush-actions { display: grid; grid-template-columns: repeat(2, 80px); gap: 10px; align-self: flex-end; margin-bottom: 8px; }
.lush-btn { height: 52px; border: 0; border-radius: 12px; background: #3b3b52; color: #fff; font: 600 15px sans-serif; touch-action: manipulation; }
.lush-btn.on { background: #4a8f4a; }
.lush-chat-top { display: flex; gap: 6px; }
.lush-chat-top .lush-btn { height: 40px; padding: 0 12px; }
.lush-field { flex: 1; min-width: 0; height: 40px; line-height: 40px; padding: 0 8px; background: #fff; color: #111; border-radius: 8px; overflow: hidden; white-space: nowrap; font: 16px/40px sans-serif; }
.lush-keys { display: flex; flex-direction: column; gap: 5px; flex: 1; }
.lush-row { display: flex; justify-content: center; gap: 4px; flex: 1; }
.lush-key { flex: 1; max-width: 40px; border: 0; border-radius: 6px; background: #3b3b52; color: #fff; font: 600 17px sans-serif; touch-action: manipulation; }
.lush-key.wide { max-width: 64px; }
.lush-key.space { max-width: none; flex: 4; }
`;
    document.head.append(s);
  }
}
