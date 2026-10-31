/**
 * The server's clock, shared by every client (visual-style.md, "Day and night"). Welcome carries
 * server_time_ms (UTC); the client keeps the offset from its own clock, so a phone with a wrong
 * clock or time zone still sees the same sky. Until Welcome arrives the local clock is used.
 *
 * The developer time control (?debug) can override the time and pause it; players never do.
 */
export class SharedClock {
  /** Server time minus local time, in ms. */
  private offset = 0;
  /** The override: shared time `at` when the local clock read `local`. Null follows the server. */
  private anchor: { at: number; local: number } | null = null;
  /** The frozen time while paused, or null. */
  private frozen: number | null = null;

  constructor(private readonly localNow: () => number = () => Date.now()) {}

  /** Records the offset between the server's UTC ms and the local clock right now. */
  sync(serverTimeMs: number): void {
    this.offset = serverTimeMs - this.localNow();
  }

  /** Shared time in UTC ms: the server's clock, unless the debug control overrides or pauses it. */
  now(): number {
    if (this.frozen !== null) return this.frozen;
    const local = this.localNow();
    if (this.anchor) return this.anchor.at + (local - this.anchor.local);
    return local + this.offset;
  }

  /** The server's time, ignoring any override. */
  serverNow(): number {
    return this.localNow() + this.offset;
  }

  /** Overrides the shared time with utcMs; it runs on from there, or stays there while paused. */
  override(utcMs: number): void {
    this.anchor = { at: utcMs, local: this.localNow() };
    if (this.frozen !== null) this.frozen = utcMs;
  }

  /** Drops the override and the pause: back to the server's time. */
  live(): void {
    this.anchor = null;
    this.frozen = null;
  }

  get overridden(): boolean {
    return this.anchor !== null;
  }

  get paused(): boolean {
    return this.frozen !== null;
  }

  /** Freezes the shared time where it is. */
  pause(): void {
    if (this.frozen === null) this.frozen = this.now();
  }

  /** Lets the time run on from where it was frozen. */
  resume(): void {
    if (this.frozen === null) return;
    this.anchor = { at: this.frozen, local: this.localNow() };
    this.frozen = null;
  }
}
