/**
 * The shared UI look (visual-style.md, Interface): dark translucent panels, a thin gold border, rounded corners,
 * a soft shadow and a serif small-caps face for titles. Every panel reads these custom properties so they match.
 */

/** Self-hosted from client/public (OFL, licence alongside); never a third-party host. */
export const TITLE_FONT_FAMILY = 'Alegreya SC';
const FONT_DIR = '/fonts/alegreya-sc';
export const FONT_FILES = [
  { weight: 400, file: 'AlegreyaSC-Regular.ttf' },
  { weight: 700, file: 'AlegreyaSC-Bold.ttf' },
] as const;

export const THEME: Readonly<Record<string, string>> = {
  '--lush-panel-bg': 'rgba(22, 19, 17, 0.86)',
  '--lush-panel-bg-solid': '#1a1714',
  '--lush-surface': '#2a2420',
  '--lush-surface-raised': '#3a312a',
  '--lush-surface-pressed': '#4a3d2c',
  '--lush-gold': '#c9a45c',
  '--lush-gold-bright': '#e8c47a',
  '--lush-gold-dim': 'rgba(201, 164, 92, 0.45)',
  '--lush-border': '1px solid var(--lush-gold-dim)',
  '--lush-border-strong': '1px solid var(--lush-gold)',
  '--lush-text': '#f1e8d6',
  '--lush-text-muted': '#b5a68c',
  '--lush-accent': '#e0a04a',
  '--lush-active': '#5b7a3a',
  /** The PvP zone and other danger: red. */
  '--lush-danger': '#e5533d',
  '--lush-radius': '10px',
  '--lush-radius-sm': '6px',
  '--lush-radius-round': '50%',
  '--lush-shadow': '0 4px 14px rgba(0, 0, 0, 0.45)',
  '--lush-shadow-pressed': '0 1px 4px rgba(0, 0, 0, 0.5)',
  '--lush-font-title': `'${TITLE_FONT_FAMILY}', 'Palatino Linotype', Palatino, Georgia, serif`,
  '--lush-font-body': "Georgia, 'Times New Roman', serif",
  '--lush-text-xs': '11px',
  '--lush-text-sm': '13px',
  '--lush-text-md': '15px',
  '--lush-text-lg': '18px',
  '--lush-text-xl': '24px',
  '--lush-space-1': '4px',
  '--lush-space-2': '8px',
  '--lush-space-3': '12px',
  '--lush-space-4': '16px',
  '--lush-transition': '120ms ease-out',
};

export const FONT_FACE_CSS = FONT_FILES.map(
  ({ weight, file }) => `@font-face { font-family: '${TITLE_FONT_FAMILY}'; font-style: normal; font-weight: ${weight}; font-display: swap; src: url('${FONT_DIR}/${file}') format('truetype'); }`,
).join('\n');

/** How long a panel's open and close animation lasts (visual-style.md: 150 to 200 ms). */
export const PANEL_ANIM_MS = 180;
export const PANEL_TRANSITION = `opacity ${PANEL_ANIM_MS}ms ease-out, transform ${PANEL_ANIM_MS}ms ease-out`;
/** Smallest tap target for a panel button, in CSS px (PRD 5.1). */
export const MIN_TAP_PX = 44;

/** Shared classes every panel uses. */
export const SHARED_CSS = `
.lush-panel { background: var(--lush-panel-bg); border: var(--lush-border); border-radius: var(--lush-radius); box-shadow: var(--lush-shadow); color: var(--lush-text); font-family: var(--lush-font-body); }
.lush-danger { color: var(--lush-danger); }
.lush-title { font-family: var(--lush-font-title); font-variant: small-caps; color: var(--lush-accent); letter-spacing: 0.06em; font-size: var(--lush-text-sm); }
.lush-anim.lush-shut { opacity: 0; transform: translateY(8px) scale(0.98); pointer-events: none; }
.lush-anim.lush-leaving { display: flex !important; }
.lush-panel button { box-sizing: border-box; min-width: ${MIN_TAP_PX}px; min-height: ${MIN_TAP_PX}px; padding: 0 var(--lush-space-3); border: var(--lush-border-strong); border-radius: var(--lush-radius-sm); background: var(--lush-surface-raised); color: var(--lush-text); font: 600 var(--lush-text-sm) var(--lush-font-title); letter-spacing: 0.04em; cursor: pointer; touch-action: manipulation; -webkit-tap-highlight-color: transparent; transition: transform var(--lush-transition), background var(--lush-transition), border-color var(--lush-transition), box-shadow var(--lush-transition); }
.lush-panel button:disabled { opacity: 0.5; cursor: default; }
.lush-panel .usable { cursor: pointer; touch-action: manipulation; -webkit-tap-highlight-color: transparent; transition: transform var(--lush-transition), background var(--lush-transition), border-color var(--lush-transition); }
@media (hover: hover) { .lush-panel button:not(:disabled):hover, .lush-panel .usable:hover { border-color: var(--lush-gold-bright); background: var(--lush-surface-pressed); } }
.lush-panel button.pressed, .lush-panel .usable.pressed { transform: scale(0.95); background: var(--lush-surface-pressed); border-color: var(--lush-gold-bright); box-shadow: var(--lush-shadow-pressed); }
`;

const STYLE_ID = 'lush-theme';

/** Sets the theme properties on the document root and adds the font faces and shared classes once. */
export function applyTheme(doc: Document = document): void {
  const root = doc.documentElement;
  for (const [name, value] of Object.entries(THEME)) root.style.setProperty(name, value);
  if (doc.getElementById(STYLE_ID)) return;
  const s = doc.createElement('style');
  s.id = STYLE_ID;
  s.textContent = `${FONT_FACE_CSS}\n${SHARED_CSS}`;
  doc.head.append(s);
}

/**
 * Short open and close animations for a panel. `apply` sets the panel's logical state (the hidden attribute or
 * display) at once, so isOpen answers immediately; while closing, the panel stays drawn and fades out for
 * PANEL_ANIM_MS, ignoring pointer events and out of the tab order (inert) from the moment it starts closing.
 */
export class PanelMotion {
  private shown: boolean;
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(readonly el: HTMLElement, private readonly apply: (shown: boolean) => void, shown = false) {
    el.classList.add('lush-anim');
    el.style.transition = PANEL_TRANSITION;
    this.shown = shown;
    apply(shown);
    el.classList.toggle('lush-shut', !shown);
    el.toggleAttribute('inert', !shown);
  }

  get isShown(): boolean {
    return this.shown;
  }

  /** True while the close animation runs. */
  get closing(): boolean {
    return this.timer !== null;
  }

  set(shown: boolean): void {
    if (shown === this.shown) return;
    this.shown = shown;
    this.clearTimer();
    const el = this.el;
    this.apply(shown);
    if (shown) {
      el.classList.remove('lush-leaving');
      el.toggleAttribute('inert', false);
      // Start from the shut look, then let the transition carry it in.
      el.classList.add('lush-shut');
      void el.offsetWidth;
      el.classList.remove('lush-shut');
    } else {
      el.toggleAttribute('inert', true);
      el.classList.add('lush-leaving', 'lush-shut');
      this.timer = setTimeout(() => {
        this.timer = null;
        el.classList.remove('lush-leaving');
      }, PANEL_ANIM_MS);
    }
  }

  dispose(): void {
    this.clearTimer();
  }

  private clearTimer(): void {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }
}

/**
 * Tap feedback for every button (and `.usable` cell) inside root, including ones rendered later: the one under
 * the pointer carries the `pressed` class while the pointer is down on it.
 */
export function addPressFeedbackWithin(root: HTMLElement, selector = 'button, .usable'): void {
  let pressed: Element | null = null;
  const release = (): void => {
    pressed?.classList.remove('pressed');
    pressed = null;
  };
  root.addEventListener('pointerdown', (e) => {
    const t = e.target instanceof Element ? e.target.closest(selector) : null;
    release();
    if (!t || !root.contains(t) || (t as HTMLButtonElement).disabled) return;
    t.classList.add('pressed');
    pressed = t;
  });
  root.addEventListener('pointerup', release);
  root.addEventListener('pointercancel', release);
  root.addEventListener('pointerleave', release);
}

/** Tap feedback: the element carries the `pressed` class while a pointer is down on it. */
export function addPressFeedback(el: HTMLElement): void {
  const on = (): void => el.classList.add('pressed');
  const off = (): void => el.classList.remove('pressed');
  el.addEventListener('pointerdown', on);
  el.addEventListener('pointerup', off);
  el.addEventListener('pointercancel', off);
  el.addEventListener('pointerleave', off);
}
