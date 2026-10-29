// @vitest-environment happy-dom
import * as flatbuffers from 'flatbuffers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  encodeCardTableLeave, encodeCardTableReady, encodeCardTableSit, encodeShedDraw, encodeShedPlay, type CardTableView, type ShedCardValue,
} from '../net/codec';
import { CardTableResultCode, CardTableSit, Envelope, Message, ShedColour, ShedPlay, ShedSymbol } from '../protocol';
import { CardTableStore, cardTableResultText } from '../state/cardTables';
import { ItemState } from '../state/items';
import { CardTablePanel, type CardTableSender } from './cardTablePanel';
import { hudLayout, inside, intersects } from './hudLayout';
import { shedCardElement, shedCardName, shedColourName, shedSymbolLabel } from './shedCards';
import { ShopPanel } from './shopPanel';

const ME = 42;

/** Records the bytes Session would send for each card table call. */
class FakeSession implements CardTableSender {
  readonly sent: Uint8Array[] = [];
  sendCardTableSit(tableId: number): void {
    this.sent.push(encodeCardTableSit(tableId));
  }
  sendCardTableReady(): void {
    this.sent.push(encodeCardTableReady());
  }
  sendCardTableLeave(): void {
    this.sent.push(encodeCardTableLeave());
  }
  sendShedPlay(card: ShedCardValue, chosenColour: ShedColour = ShedColour.None): void {
    this.sent.push(encodeShedPlay(card, chosenColour));
  }
  sendShedDraw(): void {
    this.sent.push(encodeShedDraw());
  }
}

function envelope(bytes: Uint8Array): Envelope {
  return Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
}

function view(tableId: number, seats: number[], ready: boolean[], inProgress = false): CardTableView {
  return { tableId, seats, ready, inProgress, turnSeat: inProgress ? 0 : null, reversed: false, turnTicksLeft: 0, winnerSeat: null };
}

let panel: CardTablePanel | null = null;
let shop: ShopPanel | null = null;

function setup() {
  const tables = new CardTableStore();
  tables.setCatalog([{ id: 1, seats: 4, stake: 50 }, { id: 2, seats: 2, stake: 200 }]);
  const session = new FakeSession();
  const onOpen = vi.fn();
  panel = new CardTablePanel({ parent: document.body, tables, session, onOpen, viewport: () => ({ width: 390, height: 844 }) });
  return { tables, session, onOpen, panel };
}

function seats(p: CardTablePanel): string[] {
  return [...p.panel.querySelectorAll('.lush-ctable-seat')].map((r) => r.textContent ?? '');
}

function visible(p: CardTablePanel): string[] {
  return [p.sitBtn, p.readyBtn, p.leaveBtn].filter((b) => !b.hidden).map((b) => b.textContent ?? '');
}

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  panel?.dispose();
  shop?.dispose();
  panel = shop = null;
});

describe('table info and seats (AC1)', () => {
  it('shows the stake in gold and the seat count before the Sit button', () => {
    const { panel } = setup();
    panel.open(2);
    expect(panel.stake.textContent).toBe('Stake: 200 gold · 2 seats');
    expect(panel.stake.compareDocumentPosition(panel.sitBtn) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(panel.sitBtn.hidden).toBe(false);
    expect(seats(panel)).toEqual(['Seat 1: Empty', 'Seat 2: Empty']);
  });

  it('renders each seat with its player and ready flag and follows store changes', () => {
    const { tables, panel } = setup();
    panel.open(1);
    expect(seats(panel)).toHaveLength(4);
    tables.setState(view(1, [7, 0, ME, 9], [true, false, false, false]), ME);
    expect(seats(panel)).toEqual(['Seat 1: Player 7Ready', 'Seat 2: Empty', 'Seat 3: YouNot ready', 'Seat 4: Player 9Not ready']);
    tables.setState(view(1, [7, 0, ME, 0], [true, false, true, false]), ME);
    expect(seats(panel)).toEqual(['Seat 1: Player 7Ready', 'Seat 2: Empty', 'Seat 3: YouReady', 'Seat 4: Empty']);
    tables.setCatalog([{ id: 1, seats: 4, stake: 75 }]);
    expect(panel.stake.textContent).toBe('Stake: 75 gold · 4 seats');
  });

  it('ignores other tables', () => {
    const { tables, panel } = setup();
    panel.open(2);
    tables.setState(view(1, [7, 8, 0, 0], [true, true, false, false]), ME);
    expect(seats(panel)).toEqual(['Seat 1: Empty', 'Seat 2: Empty']);
  });
});

describe('Sit, Ready and Leave (AC2)', () => {
  it('Sit sends CardTableSit for the open table, Ready CardTableReady, Leave CardTableLeave', () => {
    const { tables, session, panel } = setup();
    panel.open(2);
    panel.sitBtn.click();
    tables.setState(view(2, [ME, 0], [false, false]), ME);
    panel.readyBtn.click();
    panel.leaveBtn.click();
    const envs = session.sent.map(envelope);
    expect(envs.map((e) => e.msgType())).toEqual([Message.CardTableSit, Message.CardTableReady, Message.CardTableLeave]);
    expect((envs[0].msg(new CardTableSit()) as CardTableSit).tableId()).toBe(2);
  });

  it('Leave marks the store as leaving so an Ok result clears the seat', () => {
    const { tables, panel } = setup();
    panel.open(2);
    tables.setState(view(2, [ME, 0], [false, false]), ME);
    panel.leaveBtn.click();
    tables.setResult(CardTableResultCode.Ok);
    expect(tables.mySeat).toBeNull();
    expect(visible(panel)).toEqual(['Sit']);
  });

  it('not seated, no game: only Sit', () => {
    const { panel } = setup();
    panel.open(1);
    expect(visible(panel)).toEqual(['Sit']);
  });

  it('seated here, no game: Ready and Leave, no Sit', () => {
    const { tables, panel } = setup();
    panel.open(1);
    tables.setState(view(1, [ME, 0, 0, 0], [false, false, false, false]), ME);
    expect(visible(panel)).toEqual(['Ready', 'Leave']);
  });

  it('seated here, game running: only Leave', () => {
    const { tables, panel } = setup();
    panel.open(1);
    tables.setState(view(1, [ME, 7, 0, 0], [true, true, false, false], true), ME);
    expect(visible(panel)).toEqual(['Leave']);
  });

  it('not seated, game running: no buttons', () => {
    const { tables, panel } = setup();
    panel.open(1);
    tables.setState(view(1, [8, 7, 0, 0], [true, true, false, false], true), ME);
    expect(visible(panel)).toEqual([]);
  });

  it('seated at another table: no Sit here', () => {
    const { tables, panel } = setup();
    tables.setState(view(2, [ME, 0], [false, false]), ME);
    panel.open(1);
    expect(visible(panel)).toEqual([]);
  });
});

describe('results (AC3)', () => {
  it('shows cardTableResultText for a rejection, but not a result from before opening', () => {
    const { tables, panel } = setup();
    tables.setResult(CardTableResultCode.InProgress);
    panel.open(1);
    expect(panel.message.textContent).toBe('');
    panel.sitBtn.click();
    tables.setResult(CardTableResultCode.TableFull);
    expect(panel.message.textContent).toBe(cardTableResultText(CardTableResultCode.TableFull));
    expect(panel.message.textContent).toBe('That table is full');
  });
});

describe('open, close and layout (AC4)', () => {
  it('open calls onOpen; Close hides the panel', () => {
    const { onOpen, panel } = setup();
    expect(panel.isOpen).toBe(false);
    panel.open(1);
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(panel.isOpen).toBe(true);
    panel.panel.querySelector<HTMLElement>('.lush-ctable-close')?.click();
    expect(panel.isOpen).toBe(false);
  });

  it('opening the card table closes the shop', () => {
    const tables = new CardTableStore();
    const v = () => ({ width: 390, height: 844 });
    const s = shop = new ShopPanel({ parent: document.body, items: new ItemState(), send: () => {}, position: () => ({ x: 0, z: 0 }),
      stall: { x: 0, z: 0 }, viewport: v });
    panel = new CardTablePanel({ parent: document.body, tables, session: new FakeSession(), viewport: v, onOpen: () => s.close() });
    s.tryOpen();
    panel.open(1);
    expect(s.isOpen).toBe(false);
  });

  it('is placed at the layout card table rectangle, clear of the joystick and controls', () => {
    const { panel } = setup();
    const l = hudLayout(390, 844);
    const r = l.cardTable;
    expect(panel.panel.style.position).toBe('fixed');
    expect(panel.panel.style.left).toBe(`${r.x}px`);
    expect(panel.panel.style.top).toBe(`${r.y}px`);
    expect(panel.panel.style.width).toBe(`${r.w}px`);
    expect(panel.panel.style.height).toBe(`${r.h}px`);
    expect(inside(r, { x: 0, y: 0, w: 390, h: 844 })).toBe(true);
    expect(intersects(r, l.controls)).toBe(false);
  });
});

const GREEN_7: ShedCardValue = { colour: ShedColour.One, symbol: ShedSymbol.Seven };
const RED_SKIP: ShedCardValue = { colour: ShedColour.Four, symbol: ShedSymbol.Skip };
const WILD: ShedCardValue = { colour: ShedColour.None, symbol: ShedSymbol.Wild };
const WILD4: ShedCardValue = { colour: ShedColour.None, symbol: ShedSymbol.WildDrawFour };

/** Table 1 with ME in seat 1 (index 1) and a game running on turnSeat. */
function game(turnSeat: number) {
  const s = setup();
  s.panel.open(1);
  s.tables.setState({ ...view(1, [7, ME, 9, 0], [true, true, true, false], true), turnSeat }, ME);
  s.tables.setShedState({ tableId: 1, topCard: { colour: ShedColour.Two, symbol: ShedSymbol.Three }, colour: ShedColour.Two,
    handCounts: [5, 3, 1, 0], drawPile: 61 });
  s.tables.setHand({ tableId: 1, cards: [GREEN_7, RED_SKIP, WILD, WILD4] });
  return s;
}

function handCards(p: CardTablePanel): HTMLButtonElement[] {
  return [...p.hand.querySelectorAll<HTMLButtonElement>('.lush-shed-card')];
}

function plays(session: FakeSession): { colour: number; symbol: number; chosen: number }[] {
  return session.sent.map(envelope).filter((e) => e.msgType() === Message.ShedPlay).map((e) => {
    const m = e.msg(new ShedPlay()) as ShedPlay;
    const c = m.card();
    return { colour: c?.colour() ?? -1, symbol: c?.symbol() ?? -1, chosen: m.chosenColour() };
  });
}

function picker(p: CardTablePanel, name: string): HTMLButtonElement {
  const b = [...p.picker.querySelectorAll<HTMLButtonElement>('button')].find((x) => x.textContent === name);
  if (!b) throw new Error(`no ${name} button`);
  return b;
}

describe('Shed game view (#324 AC1)', () => {
  it('shows the top card, colour, turn, hand counts, draw pile and the local hand from the store', () => {
    const { tables, panel } = game(0);
    expect(panel.game.hidden).toBe(false);
    const top = panel.topCard.querySelector<HTMLElement>('.lush-shed-card');
    expect(top?.textContent).toBe('3');
    expect(top?.getAttribute('aria-label')).toBe('Anteater Blue 3');
    expect(panel.colour.textContent).toBe('Colour: Anteater Blue');
    expect(panel.turn.textContent).toBe("Seat 1's turn");
    expect(panel.drawPile.textContent).toBe('Draw pile: 61');
    expect(seats(panel)).toEqual(['Seat 1: Player 75 cards', 'Seat 2: You3 cards', 'Seat 3: Player 91 card', 'Seat 4: Empty']);
    expect(handCards(panel).map((b) => b.getAttribute('aria-label'))).toEqual(['Aldrich Green 7', 'Brick Red Skip', 'Wild', 'Wild +4']);

    tables.setState({ ...view(1, [7, ME, 9, 0], [true, true, true, false], true), turnSeat: 1 }, ME);
    tables.setShedState({ tableId: 1, topCard: WILD, colour: ShedColour.Three, handCounts: [4, 3, 1, 0], drawPile: 60 });
    expect(panel.turn.textContent).toBe('Your turn');
    expect(panel.colour.textContent).toBe('Colour: Golden Hour Yellow');
    expect(panel.topCard.textContent).toBe('Wild');
    expect(panel.drawPile.textContent).toBe('Draw pile: 60');
    expect(seats(panel)[0]).toBe('Seat 1: Player 74 cards');
  });

  it('is hidden outside a game', () => {
    const { tables, panel } = game(0);
    tables.setState(view(1, [7, ME, 9, 0], [false, false, false, false]), ME);
    expect(panel.game.hidden).toBe(true);
  });
});

describe('Play and Draw (#324 AC2)', () => {
  it('on your turn tapping a card sends ShedPlay with it, and Draw sends ShedDraw', () => {
    const { session, panel } = game(1);
    handCards(panel)[1].click();
    expect(plays(session)).toEqual([{ colour: ShedColour.Four, symbol: ShedSymbol.Skip, chosen: ShedColour.None }]);
    panel.drawBtn.click();
    expect(session.sent.map((b) => envelope(b).msgType())).toEqual([Message.ShedPlay, Message.ShedDraw]);
  });

  it('sends nothing when it is not your turn', () => {
    const { session, panel } = game(0);
    expect(panel.drawBtn.disabled).toBe(true);
    expect(handCards(panel).every((b) => b.disabled)).toBe(true);
    handCards(panel)[0].click();
    panel.drawBtn.click();
    panel.play(GREEN_7);
    panel.draw();
    expect(session.sent).toEqual([]);
  });
});

describe('Wild colour choice (#324 AC3)', () => {
  it('a Wild asks for a colour, then sends ShedPlay with it', () => {
    const { session, panel } = game(1);
    expect(panel.picker.hidden).toBe(true);
    handCards(panel)[2].click();
    expect(session.sent).toEqual([]);
    expect(panel.picker.hidden).toBe(false);
    expect([...panel.picker.querySelectorAll('.lush-shed-pick')].map((b) => b.textContent))
      .toEqual(['Aldrich Green', 'Anteater Blue', 'Golden Hour Yellow', 'Brick Red']);
    picker(panel, 'Golden Hour Yellow').click();
    expect(plays(session)).toEqual([{ colour: ShedColour.None, symbol: ShedSymbol.Wild, chosen: ShedColour.Three }]);
    expect(panel.picker.hidden).toBe(true);
  });

  it('a Wild Draw Four asks for a colour too', () => {
    const { session, panel } = game(1);
    handCards(panel)[3].click();
    picker(panel, 'Brick Red').click();
    expect(plays(session)).toEqual([{ colour: ShedColour.None, symbol: ShedSymbol.WildDrawFour, chosen: ShedColour.Four }]);
  });

  it('Cancel closes the choice without sending', () => {
    const { session, panel } = game(1);
    handCards(panel)[3].click();
    picker(panel, 'Cancel').click();
    expect(panel.picker.hidden).toBe(true);
    expect(session.sent).toEqual([]);
  });

  it('the choice closes when the turn passes', () => {
    const { tables, session, panel } = game(1);
    handCards(panel)[2].click();
    tables.setState({ ...view(1, [7, ME, 9, 0], [true, true, true, false], true), turnSeat: 2 }, ME);
    expect(panel.picker.hidden).toBe(true);
    panel.chooseColour(ShedColour.One);
    expect(session.sent).toEqual([]);
  });
});

describe('card faces (#324 AC4)', () => {
  it('names each colour as agreed in Q1', () => {
    expect([ShedColour.One, ShedColour.Two, ShedColour.Three, ShedColour.Four].map(shedColourName))
      .toEqual(['Aldrich Green', 'Anteater Blue', 'Golden Hour Yellow', 'Brick Red']);
  });

  it('labels each symbol kind with text symbols', () => {
    const symbols = Object.values(ShedSymbol).filter((v): v is ShedSymbol => typeof v === 'number');
    expect(symbols.map(shedSymbolLabel))
      .toEqual(['0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'Skip', 'Reverse', '+2', 'Wild', 'Wild +4']);
  });

  it('draws flat-colour DOM cards with the label and no Uno name or artwork', () => {
    const bgs = new Set<string>();
    for (const colour of [ShedColour.One, ShedColour.Two, ShedColour.Three, ShedColour.Four]) {
      for (const symbol of [ShedSymbol.Zero, ShedSymbol.Nine, ShedSymbol.Skip, ShedSymbol.Reverse, ShedSymbol.DrawTwo]) {
        const e = shedCardElement('div', { colour, symbol });
        expect(e.textContent).toBe(shedSymbolLabel(symbol));
        expect(e.getAttribute('aria-label')).toBe(`${shedColourName(colour)} ${shedSymbolLabel(symbol)}`);
        expect(e.querySelector('img, svg, canvas')).toBeNull();
        expect(e.style.backgroundImage).toBe('');
        expect(`${e.textContent} ${e.title}`.toLowerCase()).not.toContain('uno');
        bgs.add(e.style.backgroundColor);
      }
    }
    expect(bgs.size).toBe(4);
    expect(shedCardName(WILD)).toBe('Wild');
    expect(shedCardName(WILD4)).toBe('Wild +4');
    expect(shedCardElement('div', WILD4).textContent).toBe('Wild +4');
  });
});
