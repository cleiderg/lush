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

/** Shared classes any panel can use; the existing panels move onto them in later issues. */
const SHARED_CSS = `
.lush-panel { background: var(--lush-panel-bg); border: var(--lush-border); border-radius: var(--lush-radius); box-shadow: var(--lush-shadow); color: var(--lush-text); font-family: var(--lush-font-body); }
.lush-title { font-family: var(--lush-font-title); color: var(--lush-accent); letter-spacing: 0.06em; font-size: var(--lush-text-sm); }
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

/** Tap feedback: the element carries the `pressed` class while a pointer is down on it. */
export function addPressFeedback(el: HTMLElement): void {
  const on = (): void => el.classList.add('pressed');
  const off = (): void => el.classList.remove('pressed');
  el.addEventListener('pointerdown', on);
  el.addEventListener('pointerup', off);
  el.addEventListener('pointercancel', off);
  el.addEventListener('pointerleave', off);
}
