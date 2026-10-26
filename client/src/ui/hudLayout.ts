/** Screen rectangle in CSS pixels, viewport coordinates. */
export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface HudLayout {
  /** The control tray (joystick and action buttons, or the chat keyboard). Overlays stay out of it. */
  controls: Rect;
  hud: Rect;
  toggle: Rect;
  panel: Rect;
  /** The shop panel; it takes the inventory panel's place (only one is open at a time). */
  shop: Rect;
  /** The trade window; it also takes the inventory panel's place. */
  trade: Rect;
  /** The trade request prompt (Accept / Decline), a strip at the top of the panel area. */
  tradePrompt: Rect;
}

/** Mirrors index.html: #app is at most this wide and centred. */
export const APP_MAX_WIDTH = 480;
/** Mirrors index.html: #control-tray takes this share of the height at the bottom. */
export const TRAY_SHARE = 0.35;

const MARGIN = 8;
const HUD_W = 150;
const HUD_H = 64;
const TOGGLE = 52;
const PROMPT_H = 88;

/**
 * Where the HUD, inventory toggle, inventory panel, shop panel and trade window go for a viewport of
 * width × height. Everything sits over the game canvas, above the control tray.
 */
export function hudLayout(width: number, height: number): HudLayout {
  const appW = Math.min(width, APP_MAX_WIDTH);
  const left = Math.max(0, (width - appW) / 2);
  const trayH = Math.round(height * TRAY_SHARE);
  const trayTop = height - trayH;
  const controls = { x: left, y: trayTop, w: appW, h: trayH };

  const hud = { x: left + MARGIN, y: MARGIN, w: Math.min(HUD_W, appW - TOGGLE - 3 * MARGIN), h: HUD_H };
  const toggle = { x: left + appW - MARGIN - TOGGLE, y: MARGIN, w: TOGGLE, h: TOGGLE };
  const panelTop = MARGIN + HUD_H + MARGIN;
  const panel = {
    x: left + MARGIN,
    y: panelTop,
    w: appW - 2 * MARGIN,
    h: Math.max(0, trayTop - MARGIN - panelTop),
  };
  const tradePrompt = { ...panel, h: Math.min(PROMPT_H, panel.h) };
  return { controls, hud, toggle, panel, shop: { ...panel }, trade: { ...panel }, tradePrompt };
}

export function intersects(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

export function inside(inner: Rect, outer: Rect): boolean {
  return inner.x >= outer.x && inner.y >= outer.y
    && inner.x + inner.w <= outer.x + outer.w && inner.y + inner.h <= outer.y + outer.h;
}

/** Positions el fixed at r. */
export function place(el: HTMLElement, r: Rect): void {
  el.style.position = 'fixed';
  el.style.left = `${r.x}px`;
  el.style.top = `${r.y}px`;
  el.style.width = `${r.w}px`;
  el.style.height = `${r.h}px`;
}
