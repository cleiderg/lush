import * as THREE from 'three';
import { CSS2DObject } from 'three/examples/jsm/renderers/CSS2DRenderer.js';
import { CHARACTER_HEIGHT } from './character';
import { THEME } from '../ui/theme';
import type { GainView, PlayerView } from './worldView';

/** How long a floating number lives (PRD, milestone 8 placeholders; Q2). */
export const FLOAT_TEXT_MS = 1200;
/** How far a number rises over its life (metres). */
export const FLOAT_TEXT_RISE_M = 1;
/** Start height of a number's bottom edge above the player's feet: over the chat bubble. */
export const FLOAT_TEXT_HEIGHT = CHARACTER_HEIGHT + 1.0;
/** Line height of a number, in CSS pixels. */
export const FLOAT_TEXT_LINE_PX = 18;
/**
 * Screen-space gap between stacked numbers, in CSS pixels. Stacking in pixels rather than metres keeps numbers apart
 * at every zoom: at the default camera distance a metre is only about 11 px tall on a phone.
 */
export const FLOAT_TEXT_STACK_PX = 20;
/** Most numbers shown at once; the oldest goes first past this. */
export const MAX_FLOAT_TEXTS = 32;
/** Gain ids remembered, so a repeated id never spawns twice. */
const SEEN_LIMIT = 256;

export const FLOAT_TEXT_CLASS = 'lush-float-text';
/** XP in the theme's accent, gold in its bright gold. */
export const XP_COLOR = THEME['--lush-accent'];
export const GOLD_COLOR = THEME['--lush-gold-bright'];

/** Opacity over a number's life: solid for the first half, then fading to zero. */
export function floatOpacity(ageMs: number): number {
  const t = ageMs / FLOAT_TEXT_MS;
  return t <= 0.5 ? 1 : Math.min(1, Math.max(0, 2 - 2 * t));
}

export function gainText(g: GainView): string {
  const n = Math.max(0, Math.floor(g.amount));
  return g.kind === 'xp' ? `+${n} XP` : `+${n} gold`;
}

export interface FloatText {
  readonly id: number;
  readonly bornMs: number;
  /** Stack slot: the lowest one free among the numbers showing when it was born. */
  readonly slot: number;
  readonly label: CSS2DObject;
}

/**
 * Floating "+N XP" and "+N gold" numbers over the local player. Each is one CSS2DObject (HTML overlay), so any
 * number of them adds no WebGL draw calls. Text is set with textContent only.
 */
export class FloatTexts {
  readonly group = new THREE.Group();
  private readonly texts: FloatText[] = [];
  private readonly seen = new Set<number>();

  constructor() {
    this.group.name = 'float-texts';
  }

  get active(): readonly FloatText[] {
    return this.texts;
  }

  update(view: { local: PlayerView; gains?: readonly GainView[] }, nowMs: number): void {
    for (let i = this.texts.length - 1; i >= 0; i--) {
      const t = this.texts[i];
      if (nowMs - t.bornMs >= FLOAT_TEXT_MS) {
        this.group.remove(t.label);
        this.texts.splice(i, 1);
      }
    }
    for (const g of view.gains ?? []) {
      if (this.seen.has(g.id)) continue;
      this.seen.add(g.id);
      if (this.seen.size > SEEN_LIMIT) this.seen.delete(this.seen.values().next().value as number);
      const el = document.createElement('div');
      el.className = FLOAT_TEXT_CLASS;
      el.textContent = gainText(g);
      el.style.cssText = `color:${g.kind === 'xp' ? XP_COLOR : GOLD_COLOR};font:bold 15px ${THEME['--lush-font-body']};text-shadow:0 1px 2px #000,0 0 3px #000;line-height:${FLOAT_TEXT_LINE_PX}px;white-space:nowrap;pointer-events:none;user-select:none;`;
      const label = new CSS2DObject(el);
      label.center.set(0.5, 1);
      label.name = `gain-${g.id}`;
      const slot = this.freeSlot();
      // The label is anchored at its bottom edge, so bottom padding lifts the text by whole lines on screen.
      el.style.paddingBottom = `${slot * FLOAT_TEXT_STACK_PX}px`;
      this.group.add(label);
      this.texts.push({ id: g.id, bornMs: nowMs, slot, label });
    }
    while (this.texts.length > MAX_FLOAT_TEXTS) this.group.remove(this.texts.shift()!.label);

    const p = view.local;
    for (const t of this.texts) {
      const age = nowMs - t.bornMs;
      const opacity = floatOpacity(age);
      t.label.element.style.opacity = String(opacity);
      t.label.visible = opacity > 0;
      t.label.position.set(p.x, FLOAT_TEXT_HEIGHT + (age / FLOAT_TEXT_MS) * FLOAT_TEXT_RISE_M, p.z);
    }
  }

  private freeSlot(): number {
    let slot = 0;
    while (this.texts.some((t) => t.slot === slot)) slot++;
    return slot;
  }
}
