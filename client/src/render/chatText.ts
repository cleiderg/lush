import * as THREE from 'three';
import { CSS2DObject } from 'three/examples/jsm/renderers/CSS2DRenderer.js';
import { CHARACTER_HEIGHT } from './character';
import type { ChatBubbleView, PlayerView } from './worldView';

/** Height of the text's bottom edge above the speaker's feet: over the health bar and its first hitsplat. */
export const CHAT_HEIGHT = CHARACTER_HEIGHT + 0.7;
/** Longest message drawn; longer ones are cut with an ellipsis. */
export const MAX_CHAT_CHARS = 80;

/** OSRS-style overhead chat: yellow text with a dark outline, no bubble. */
const STYLE =
  'font:bold 13px sans-serif;color:#ffff00;text-shadow:1px 1px 0 #000,-1px -1px 0 #000,1px -1px 0 #000,-1px 1px 0 #000;' +
  'white-space:nowrap;pointer-events:none;';

export function clipChat(text: string): string {
  const chars = [...text];
  return chars.length > MAX_CHAT_CHARS ? chars.slice(0, MAX_CHAT_CHARS - 1).join('') + '…' : text;
}

/**
 * Overhead chat for players in view. Each message is one CSS2DObject (an HTML label the
 * CSS2DRenderer overlay positions over the canvas), so text costs no WebGL draw calls.
 * Text is set with textContent only: messages are never parsed as markup.
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
        const el = document.createElement('div');
        el.className = 'chat-text';
        el.style.cssText = STYLE;
        label = new CSS2DObject(el);
        label.center.set(0.5, 1);
        label.name = `chat-${b.id}`;
        this.labels.set(b.id, label);
        this.group.add(label);
      }
      const text = clipChat(b.text);
      if (label.element.textContent !== text) label.element.textContent = text;
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
