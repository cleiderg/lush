import type { ShedCardValue } from '../state/cardTables';
import { ShedColour, ShedSymbol } from '../protocol';

/** The four playable colours, in protocol order. */
export const SHED_COLOURS: readonly ShedColour[] = [ShedColour.One, ShedColour.Two, ShedColour.Three, ShedColour.Four];

/** Display names for the colours (docs/specs/card-tables.md, placeholders). */
const COLOUR_NAMES: Record<number, string> = {
  [ShedColour.None]: 'Wild',
  [ShedColour.One]: 'Aldrich Green',
  [ShedColour.Two]: 'Anteater Blue',
  [ShedColour.Three]: 'Golden Hour Yellow',
  [ShedColour.Four]: 'Brick Red',
};

/** Flat card face and text colours; None is the Wild card face. */
const COLOUR_CSS: Record<number, { bg: string; fg: string }> = {
  [ShedColour.None]: { bg: '#2b2b2b', fg: '#ffffff' },
  [ShedColour.One]: { bg: '#3f8f4f', fg: '#ffffff' },
  [ShedColour.Two]: { bg: '#2f6db5', fg: '#ffffff' },
  [ShedColour.Three]: { bg: '#f2c23a', fg: '#1d1d26' },
  [ShedColour.Four]: { bg: '#b8432f', fg: '#ffffff' },
};

const SYMBOL_LABELS: Record<number, string> = {
  [ShedSymbol.Skip]: 'Skip',
  [ShedSymbol.Reverse]: 'Reverse',
  [ShedSymbol.DrawTwo]: '+2',
  [ShedSymbol.Wild]: 'Wild',
  [ShedSymbol.WildDrawFour]: 'Wild +4',
};

export function shedColourName(c: ShedColour): string {
  return COLOUR_NAMES[c] ?? 'Unknown';
}

/** The text symbol on a card: 0–9, Skip, Reverse, +2, Wild, Wild +4. */
export function shedSymbolLabel(s: ShedSymbol): string {
  return SYMBOL_LABELS[s] ?? (s >= ShedSymbol.Zero && s <= ShedSymbol.Nine ? String(s) : '?');
}

export function isWild(card: ShedCardValue): boolean {
  return card.symbol === ShedSymbol.Wild || card.symbol === ShedSymbol.WildDrawFour;
}

/** Full card name, for example "Anteater Blue 7" or "Wild +4". */
export function shedCardName(card: ShedCardValue): string {
  const symbol = shedSymbolLabel(card.symbol);
  return isWild(card) ? symbol : `${shedColourName(card.colour)} ${symbol}`;
}

export function shedColourCss(c: ShedColour): { bg: string; fg: string } {
  return COLOUR_CSS[c] ?? COLOUR_CSS[ShedColour.None];
}

/** A flat-colour card face: the symbol in large text on the colour, named for screen readers. */
export function shedCardElement<K extends 'div' | 'button'>(tag: K, card: ShedCardValue): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = 'lush-shed-card';
  const css = shedColourCss(isWild(card) ? ShedColour.None : card.colour);
  e.style.backgroundColor = css.bg;
  e.style.color = css.fg;
  e.dataset.colour = String(card.colour);
  e.dataset.symbol = String(card.symbol);
  e.textContent = shedSymbolLabel(card.symbol);
  e.title = shedCardName(card);
  e.setAttribute('aria-label', shedCardName(card));
  return e;
}
