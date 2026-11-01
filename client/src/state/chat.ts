/** Most chat log lines kept; older lines are dropped. Placeholder (Q1, CONSTITUTION rule 4). */
export const CHAT_LOG_LINES = 50;
/** How long an overhead message shows after it arrives. Placeholder (Q1, CONSTITUTION rule 4). */
export const OVERHEAD_CHAT_MS = 5000;
/** An overhead message fades out over its last CHAT_FADE_MS. Placeholder (Q2, CONSTITUTION rule 4). */
export const CHAT_FADE_MS = 1000;

/**
 * Opacity of an overhead message `age` ms after it arrived: 1 until OVERHEAD_CHAT_MS - CHAT_FADE_MS,
 * then falling linearly to 0 at OVERHEAD_CHAT_MS. Never increases with age.
 */
export function overheadOpacity(age: number): number {
  const left = OVERHEAD_CHAT_MS - age;
  if (left >= CHAT_FADE_MS) return 1;
  return Math.max(0, left / CHAT_FADE_MS);
}

export interface ChatLine {
  readonly speakerId: number;
  readonly text: string;
  /** A game message to the local player (skill results, bank refusals, level-ups), not speech; speakerId is 0. */
  readonly system?: boolean;
}

export interface OverheadChat {
  readonly speakerId: number;
  readonly text: string;
  /** The `now` the message arrived with. */
  readonly at: number;
}

/**
 * ChatMessages from the server: a bounded log (oldest first) and the latest
 * overhead message per speaker, which expires OVERHEAD_CHAT_MS after arrival.
 */
export class ChatStore {
  private lines: ChatLine[] = [];
  private readonly overheads = new Map<number, OverheadChat>();

  /** Chat log, oldest first, at most CHAT_LOG_LINES lines. */
  get log(): readonly ChatLine[] {
    return this.lines;
  }

  push(speakerId: number, text: string, now: number): void {
    this.lines.push({ speakerId, text });
    if (this.lines.length > CHAT_LOG_LINES) this.lines.splice(0, this.lines.length - CHAT_LOG_LINES);
    this.overheads.set(speakerId, { speakerId, text, at: now });
  }

  /** Adds a game message to the log only (no overhead bubble). */
  system(text: string): void {
    this.lines.push({ speakerId: 0, text, system: true });
    if (this.lines.length > CHAT_LOG_LINES) this.lines.splice(0, this.lines.length - CHAT_LOG_LINES);
  }

  /** The speaker's current overhead message, or null when none or expired. */
  overhead(speakerId: number, now: number): OverheadChat | null {
    const o = this.overheads.get(speakerId);
    if (!o) return null;
    if (now - o.at >= OVERHEAD_CHAT_MS) {
      this.overheads.delete(speakerId);
      return null;
    }
    return o;
  }

  /** Every unexpired overhead message; expired ones are dropped. */
  activeOverheads(now: number): OverheadChat[] {
    const out: OverheadChat[] = [];
    for (const id of [...this.overheads.keys()]) {
      const o = this.overhead(id, now);
      if (o) out.push(o);
    }
    return out;
  }

  /** Drops overhead messages (for example on Welcome); the log stays. */
  clearOverheads(): void {
    this.overheads.clear();
  }
}
