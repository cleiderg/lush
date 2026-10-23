export const MAX_CHAT_BYTES = 120;

export type KeyboardPage = 'letters' | 'symbols';

export const LETTER_ROWS = ['qwertyuiop', 'asdfghjkl', 'zxcvbnm'];
export const SYMBOL_ROWS = ['1234567890', '-/:;()$&@"', '.,?!\''];

export function byteLength(s: string): number {
  return new TextEncoder().encode(s).length;
}

/** Truncates to at most maxBytes of UTF-8 without splitting a code point. */
export function truncateToBytes(s: string, maxBytes = MAX_CHAT_BYTES): string {
  let out = '';
  let used = 0;
  for (const ch of s) {
    const n = byteLength(ch);
    if (used + n > maxBytes) break;
    out += ch;
    used += n;
  }
  return out;
}

/** State of the in-game keyboard and message field. */
export class ChatKeyboardState {
  text = '';
  page: KeyboardPage = 'letters';
  shift = true; // first letter is auto-capitalized

  /** Types a character (from the on-screen or physical keyboard). */
  type(ch: string): boolean {
    if (ch.length === 0) return false;
    let c = ch;
    if (this.page === 'letters' && this.shift) c = c.toUpperCase();
    if (byteLength(this.text + c) > MAX_CHAT_BYTES) return false;
    this.text += c;
    this.shift = this.autoShift();
    return true;
  }

  backspace(): void {
    const chars = [...this.text];
    chars.pop();
    this.text = chars.join('');
    this.shift = this.autoShift();
  }

  toggleSymbols(): void {
    this.page = this.page === 'letters' ? 'symbols' : 'letters';
  }

  /** Returns the trimmed message to send, or null if empty. Resets state. */
  submit(): string | null {
    const msg = this.text.trim();
    this.reset();
    return msg === '' ? null : msg;
  }

  reset(): void {
    this.text = '';
    this.page = 'letters';
    this.shift = true;
  }

  get bytesLeft(): number {
    return MAX_CHAT_BYTES - byteLength(this.text);
  }

  private autoShift(): boolean {
    return this.text === '';
  }
}
