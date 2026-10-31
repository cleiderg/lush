import * as THREE from 'three';
import { CSS2DObject } from 'three/examples/jsm/renderers/CSS2DRenderer.js';
import { CHARACTER_HEIGHT } from './character';
import type { ChatBubbleView, PlayerView } from './worldView';

/** Height of the text's bottom edge above the speaker's feet: over the health bar and its first hitsplat. */
export const CHAT_HEIGHT = CHARACTER_HEIGHT + 0.7;
/** Longest message drawn; longer ones are cut with an ellipsis. */
export const MAX_CHAT_CHARS = 80;

/** Class on every bubble element; CHAT_BUBBLE_CSS styles it. */
export const CHAT_BUBBLE_CLASS = 'lush-chat-bubble';
/** Class on the tail under each bubble. */
export const CHAT_TAIL_CLASS = 'lush-chat-tail';

/**
 * The references' speech bubble (visual-style.md, Interface): a rounded dark panel with a thin gold rim, italic
 * serif text in the theme's colours, and a small tail pointing down at the speaker. Colours, font and radius are
 * the shared theme's --lush-* custom properties, which inherit from the document root into each bubble.
 * It lives in each bubble's shadow root, so the bubble element itself holds only the message text.
 */
export const CHAT_BUBBLE_CSS = `
:host(.${CHAT_BUBBLE_CLASS}) { position: relative; display: block; box-sizing: border-box; width: max-content; max-width: 240px; margin-bottom: 7px; padding: 4px 12px 5px; background: var(--lush-panel-bg-solid, #1a1714); border: var(--lush-border, 1px solid rgba(201, 164, 92, 0.45)); border-radius: var(--lush-radius, 10px); box-shadow: var(--lush-shadow, 0 4px 14px rgba(0, 0, 0, 0.45)); color: var(--lush-text, #f1e8d6); font-family: var(--lush-font-body, Georgia, serif); font-style: italic; font-size: var(--lush-text-sm, 13px); line-height: 1.3; text-align: center; white-space: normal; overflow-wrap: anywhere; pointer-events: none; user-select: none; }
.${CHAT_TAIL_CLASS} { position: absolute; left: 50%; bottom: -5px; width: 8px; height: 8px; margin-left: -4px; box-sizing: border-box; background: var(--lush-panel-bg-solid, #1a1714); border-right: var(--lush-border, 1px solid rgba(201, 164, 92, 0.45)); border-bottom: var(--lush-border, 1px solid rgba(201, 164, 92, 0.45)); transform: rotate(45deg); }
`;

function makeBubble(): HTMLDivElement {
  const el = document.createElement('div');
  el.className = `chat-text ${CHAT_BUBBLE_CLASS}`;
  const root = el.attachShadow({ mode: 'open' });
  const style = document.createElement('style');
  style.textContent = CHAT_BUBBLE_CSS;
  const tail = document.createElement('div');
  tail.className = CHAT_TAIL_CLASS;
  // The message is the host's own text, shown through the slot.
  root.append(style, document.createElement('slot'), tail);
  return el;
}

/** A bubble's opacity from the view: missing means 1, and anything else is clamped to 0..1. */
export function bubbleOpacity(opacity: number | undefined): number {
  if (opacity === undefined || Number.isNaN(opacity)) return 1;
  return Math.min(1, Math.max(0, opacity));
}

export function clipChat(text: string): string {
  const chars = [...text];
  return chars.length > MAX_CHAT_CHARS ? chars.slice(0, MAX_CHAT_CHARS - 1).join('') + '…' : text;
}

/**
 * Overhead chat for players in view, in speech bubbles. Each message is one CSS2DObject (an HTML label the
 * CSS2DRenderer overlay positions over the canvas), so text costs no WebGL draw calls.
 * Text is set with textContent only: messages are never parsed as markup. A bubble at opacity 0 is hidden.
 */
export class ChatText {
  readonly group = new THREE.Group();
  private readonly labels = new Map<number, CSS2DObject>();

  constructor() {
    this.group.name = 'chat';
  }

  /** Labels shown now, by speaker id. */
  get shown(): ReadonlyMap<number, CSS2DObject> {
    return this.labels;
  }

  update(view: { local: PlayerView; remotes: readonly PlayerView[]; chat?: readonly ChatBubbleView[] }): void {
    const where = new Map<number, PlayerView>();
    where.set(view.local.id, view.local);
    for (const p of view.remotes) where.set(p.id, p);

    const live = new Set<number>();
    for (const b of view.chat ?? []) {
      const p = where.get(b.id);
      if (!p || !b.text) continue;
      live.add(b.id);
      let label = this.labels.get(b.id);
      if (!label) {
        label = new CSS2DObject(makeBubble());
        label.center.set(0.5, 1);
        label.name = `chat-${b.id}`;
        this.labels.set(b.id, label);
        this.group.add(label);
      }
      const text = clipChat(b.text);
      if (label.element.textContent !== text) label.element.textContent = text;
      const opacity = String(bubbleOpacity(b.opacity));
      if (label.element.style.opacity !== opacity) label.element.style.opacity = opacity;
      // CSS2DRenderer sets display:none on objects that aren't visible.
      label.visible = opacity !== '0';
      label.position.set(p.x, CHAT_HEIGHT, p.z);
    }
    for (const [id, label] of this.labels) {
      if (live.has(id)) continue;
      // CSS2DObject removes its element from the overlay when taken out of the scene.
      this.group.remove(label);
      this.labels.delete(id);
    }
  }
}
