/** Banner shown while the server counts down to a restart; absent from the DOM otherwise. */
export class RestartBanner {
  private el: HTMLElement | null = null;

  constructor(private readonly parent: HTMLElement) {}

  set(seconds: number | null): void {
    if (seconds === null) {
      this.el?.remove();
      this.el = null;
      return;
    }
    if (!this.el) {
      const el = document.createElement('div');
      el.id = 'restart-banner';
      el.setAttribute('role', 'alert');
      el.style.cssText =
        'position:fixed;top:0;left:0;right:0;padding:8px 12px;text-align:center;z-index:10;' +
        'background:#c0392b;color:#fff;font:600 16px system-ui,sans-serif;';
      this.parent.append(el);
      this.el = el;
    }
    this.el.textContent = `Server restarting in ${Math.max(0, Math.round(seconds))}s`;
  }
}
