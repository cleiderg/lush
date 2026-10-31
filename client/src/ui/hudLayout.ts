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
  /** The location panel (place name, area type, Irvine time on a sun arc): top left, beside the HUD. */
  location: Rect;
  hud: Rect;
  toggle: Rect;
  /** The Retro look toggle (Q2), left of the inventory toggle in the top bar. */
  retro: Rect;
  panel: Rect;
  /** The shop panel; it takes the inventory panel's place (only one is open at a time). */
  shop: Rect;
  /** The trade window; it also takes the inventory panel's place. */
  trade: Rect;
  /** The card table panel; it also takes the inventory panel's place. */
  cardTable: Rect;
  /** The trade request prompt (Accept / Decline), a strip at the top of the panel area. */
  tradePrompt: Rect;
  /** The PvP warning banner / in-zone indicator, a strip just above the control tray and below the panels. */
  pvp: Rect;
  /**
   * The Interact prompt (what Interact does now): a strip centred in the app column just under the top band.
   * Panels open over it.
   */
  interact: Rect;
  /**
   * The player's own portrait (HP ring, combat level): round, directly under the location panel, below the Interact
   * prompt when the two would touch. Panels open over it.
   */
  portrait: Rect;
  /**
   * The round minimap: top right of the app column, under the toggles and the Interact prompt, level with the
   * portrait on the other side. Tapping it opens the big map in the panel area.
   */
  minimap: Rect;
  /** The big map panel; it takes the inventory panel's place. */
  map: Rect;
  /** The chat log, a strip at the bottom of the panel area just above the PvP strip; hidden while a panel is open. */
  chat: Rect;
}

/** Mirrors index.html: #app is at most this wide and centred. */
export const APP_MAX_WIDTH = 480;
/** Mirrors index.html: #control-tray takes this share of the height at the bottom. */
export const TRAY_SHARE = 0.35;

const MARGIN = 8;
const HUD_W = 150;
/** Height of the top band: the location panel, the HUD and the toggles. */
const HUD_H = 76;
/** The location panel's widest size, and its share of the top band when narrower. */
const LOCATION_W = 200;
const LOCATION_SHARE = 0.6;
const TOGGLE = 52;
const PROMPT_H = 88;
const PVP_H = 36;
const CHAT_H = 120;
const INTERACT_W = 300;
const INTERACT_H = 36;
const PORTRAIT = 64;
const MINIMAP = 96;

/**
 * Where the location panel, HUD, inventory and Retro toggles, inventory panel, shop panel and trade window go for a viewport of
 * width × height. Everything sits over the game canvas, above the control tray.
 */
export function hudLayout(width: number, height: number): HudLayout {
  const appW = Math.min(width, APP_MAX_WIDTH);
  const left = Math.max(0, (width - appW) / 2);
  const trayH = Math.round(height * TRAY_SHARE);
  const trayTop = height - trayH;
  const controls = { x: left, y: trayTop, w: appW, h: trayH };

  const toggle = { x: left + appW - MARGIN - TOGGLE, y: MARGIN, w: TOGGLE, h: TOGGLE };
  const retro = { ...toggle, x: toggle.x - MARGIN - TOGGLE };
  // The top band left of the toggles holds the location panel, then the HUD.
  const band = retro.x - MARGIN - (left + MARGIN);
  const locationW = Math.min(LOCATION_W, Math.round((band - MARGIN) * LOCATION_SHARE));
  const location = { x: left + MARGIN, y: MARGIN, w: locationW, h: HUD_H };
  const hudX = location.x + location.w + MARGIN;
  const hud = { x: hudX, y: MARGIN, w: Math.min(HUD_W, retro.x - MARGIN - hudX), h: HUD_H };
  const panelTop = MARGIN + HUD_H + MARGIN;
  // The PvP strip sits just above the tray and the panels end above it, so it never hides panel content.
  const pvp = { x: left + MARGIN, y: trayTop - MARGIN - PVP_H, w: appW - 2 * MARGIN, h: PVP_H };
  const panel = {
    x: left + MARGIN,
    y: panelTop,
    w: appW - 2 * MARGIN,
    h: Math.max(0, pvp.y - MARGIN - panelTop),
  };
  const tradePrompt = { ...panel, h: Math.min(PROMPT_H, panel.h) };
  const chatH = Math.min(CHAT_H, panel.h);
  const chat = { x: panel.x, y: panel.y + panel.h - chatH, w: panel.w, h: chatH };
  const interactW = Math.min(INTERACT_W, appW - 2 * MARGIN);
  const interact = { x: left + (appW - interactW) / 2, y: panelTop, w: interactW, h: INTERACT_H };
  // Level with the Interact prompt if it fits beside it, otherwise just below it.
  const portraitX = location.x;
  const portraitY = portraitX + PORTRAIT + MARGIN <= interact.x ? panelTop : interact.y + interact.h + MARGIN;
  const portrait = { x: portraitX, y: portraitY, w: PORTRAIT, h: PORTRAIT };
  const minimap = { x: left + appW - MARGIN - MINIMAP, y: interact.y + interact.h + MARGIN, w: MINIMAP, h: MINIMAP };
  return {
    controls, location, hud, toggle, retro, panel, shop: { ...panel }, trade: { ...panel }, cardTable: { ...panel }, tradePrompt, pvp,
    chat, interact, portrait, minimap, map: { ...panel },
  };
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
