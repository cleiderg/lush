// @vitest-environment happy-dom
import * as flatbuffers from 'flatbuffers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { encodeCardTableLeave, encodeCardTableReady, encodeCardTableSit, type CardTableView } from '../net/codec';
import { CardTableResultCode, CardTableSit, Envelope, Message } from '../protocol';
import { CardTableStore, cardTableResultText } from '../state/cardTables';
import { ItemState } from '../state/items';
import { CardTablePanel, type CardTableSender } from './cardTablePanel';
import { hudLayout, inside, intersects } from './hudLayout';
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
