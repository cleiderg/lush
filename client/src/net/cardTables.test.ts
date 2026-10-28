import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  CardTableCatalog, CardTableInfo, CardTableLeave, CardTableReady, CardTableResult, CardTableResultCode, CardTableSit,
  CardTableState, Envelope, Message, ShedCard, ShedColour, ShedDraw, ShedHand, ShedPlay, ShedState, ShedSymbol, Vec2,
  Welcome,
} from '../protocol';
import {
  decode, encodeCardTableLeave, encodeCardTableReady, encodeCardTableSit, encodeShedDraw, encodeShedPlay,
} from './codec';
import { Session } from './session';
import type { Transport } from './transport';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

interface StateFields {
  tableId: number; seats: number[]; inProgress: boolean; turnSeat: number; reversed: boolean;
  turnTicksLeft: number; hasWinner: boolean; winnerSeat: number; ready?: boolean[];
}

// Builds field by field so a missing ready list can be tested.
function buildState(b: flatbuffers.Builder, f: StateFields): flatbuffers.Offset {
  const seats = CardTableState.createSeatsVector(b, f.seats);
  const ready = f.ready ? CardTableState.createReadyVector(b, f.ready) : 0;
  CardTableState.startCardTableState(b);
  CardTableState.addTableId(b, f.tableId);
  CardTableState.addSeats(b, seats);
  CardTableState.addInProgress(b, f.inProgress);
  CardTableState.addTurnSeat(b, f.turnSeat);
  CardTableState.addReversed(b, f.reversed);
  CardTableState.addTurnTicksLeft(b, f.turnTicksLeft);
  CardTableState.addHasWinner(b, f.hasWinner);
  CardTableState.addWinnerSeat(b, f.winnerSeat);
  if (ready) CardTableState.addReady(b, ready);
  return CardTableState.endCardTableState(b);
}

function envelope(bytes: Uint8Array, type: Message): Envelope {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
  expect(env.msgType()).toBe(type);
  return env;
}

describe('encode card table messages', () => {
  it('encodes CardTableSit', () => {
    const m = envelope(encodeCardTableSit(65000), Message.CardTableSit).msg(new CardTableSit()) as CardTableSit;
    expect(m.tableId()).toBe(65000);
  });

  it('encodes CardTableLeave', () => {
    expect(envelope(encodeCardTableLeave(), Message.CardTableLeave).msg(new CardTableLeave())).toBeInstanceOf(CardTableLeave);
  });
});

describe('encode Ready and Shed moves', () => {
  it('encodes CardTableReady', () => {
    expect(envelope(encodeCardTableReady(), Message.CardTableReady).msg(new CardTableReady())).toBeInstanceOf(CardTableReady);
  });

  it('encodes ShedPlay for a coloured card with no chosen colour', () => {
    const m = envelope(encodeShedPlay({ colour: ShedColour.Three, symbol: ShedSymbol.Seven }), Message.ShedPlay)
      .msg(new ShedPlay()) as ShedPlay;
    expect(m.card()?.colour()).toBe(ShedColour.Three);
    expect(m.card()?.symbol()).toBe(ShedSymbol.Seven);
    expect(m.chosenColour()).toBe(ShedColour.None);
  });

  it('encodes ShedPlay for a Wild Draw Four with its chosen colour', () => {
    const bytes = encodeShedPlay({ colour: ShedColour.None, symbol: ShedSymbol.WildDrawFour }, ShedColour.Four);
    const m = envelope(bytes, Message.ShedPlay).msg(new ShedPlay()) as ShedPlay;
    expect(m.card()?.colour()).toBe(ShedColour.None);
    expect(m.card()?.symbol()).toBe(ShedSymbol.WildDrawFour);
    expect(m.chosenColour()).toBe(ShedColour.Four);
  });

  it('encodes ShedDraw', () => {
    expect(envelope(encodeShedDraw(), Message.ShedDraw).msg(new ShedDraw())).toBeInstanceOf(ShedDraw);
  });
});

describe('decode card table catalog and Shed messages', () => {
  it('decodes a CardTableCatalog with seat counts and stakes', () => {
    const b = new flatbuffers.Builder(64);
    const rows: [number, number, number][] = [[1, 2, 50], [2, 4, 4294967295], [65535, 3, 0]];
    CardTableCatalog.startTablesVector(b, rows.length);
    for (const [id, seats, stake] of [...rows].reverse()) CardTableInfo.createCardTableInfo(b, id, seats, stake);
    const tables = b.endVector();
    const msg = CardTableCatalog.createCardTableCatalog(b, tables);
    expect(decode(finish(b, Message.CardTableCatalog, msg))).toEqual({
      type: 'cardTableCatalog',
      tables: [{ id: 1, seats: 2, stake: 50 }, { id: 2, seats: 4, stake: 4294967295 }, { id: 65535, seats: 3, stake: 0 }],
    });
  });

  it('decodes an empty CardTableCatalog', () => {
    const b = new flatbuffers.Builder(16);
    CardTableCatalog.startCardTableCatalog(b);
    expect(decode(finish(b, Message.CardTableCatalog, CardTableCatalog.endCardTableCatalog(b))))
      .toEqual({ type: 'cardTableCatalog', tables: [] });
  });

  it('decodes a ShedState', () => {
    const b = new flatbuffers.Builder(64);
    const counts = ShedState.createHandCountsVector(b, [7, 0, 255]);
    ShedState.startShedState(b);
    ShedState.addTableId(b, 4);
    ShedState.addTopCard(b, ShedCard.createShedCard(b, ShedColour.None, ShedSymbol.Wild));
    ShedState.addColour(b, ShedColour.Two);
    ShedState.addHandCounts(b, counts);
    ShedState.addDrawPile(b, 80);
    expect(decode(finish(b, Message.ShedState, ShedState.endShedState(b)))).toEqual({
      type: 'shedState', tableId: 4, topCard: { colour: ShedColour.None, symbol: ShedSymbol.Wild },
      colour: ShedColour.Two, handCounts: [7, 0, 255], drawPile: 80,
    });
  });

  it('decodes an empty ShedState with defaults', () => {
    const b = new flatbuffers.Builder(16);
    ShedState.startShedState(b);
    expect(decode(finish(b, Message.ShedState, ShedState.endShedState(b)))).toEqual({
      type: 'shedState', tableId: 0, topCard: { colour: ShedColour.None, symbol: ShedSymbol.Zero },
      colour: ShedColour.None, handCounts: [], drawPile: 0,
    });
  });

  it('decodes a ShedHand in order', () => {
    const b = new flatbuffers.Builder(64);
    const cards: [ShedColour, ShedSymbol][] = [
      [ShedColour.One, ShedSymbol.Zero], [ShedColour.Four, ShedSymbol.DrawTwo], [ShedColour.None, ShedSymbol.WildDrawFour],
    ];
    ShedHand.startCardsVector(b, cards.length);
    for (const [c, sym] of [...cards].reverse()) ShedCard.createShedCard(b, c, sym);
    const vec = b.endVector();
    const msg = ShedHand.createShedHand(b, 9, vec);
    expect(decode(finish(b, Message.ShedHand, msg))).toEqual({
      type: 'shedHand', tableId: 9,
      cards: cards.map(([colour, symbol]) => ({ colour, symbol })),
    });
  });

  it('decodes an empty ShedHand', () => {
    const b = new flatbuffers.Builder(16);
    ShedHand.startShedHand(b);
    ShedHand.addTableId(b, 2);
    expect(decode(finish(b, Message.ShedHand, ShedHand.endShedHand(b)))).toEqual({ type: 'shedHand', tableId: 2, cards: [] });
  });
});

describe('decode card table messages', () => {
  it('decodes a CardTableState in progress with a winner', () => {
    const b = new flatbuffers.Builder(64);
    const msg = buildState(b, {
      tableId: 3, seats: [12, 0, 65535, 7], inProgress: true, turnSeat: 2, reversed: true, turnTicksLeft: 70000,
      hasWinner: true, winnerSeat: 3, ready: [true, false, true, true],
    });
    expect(decode(finish(b, Message.CardTableState, msg))).toEqual({
      type: 'cardTableState', tableId: 3, seats: [12, 0, 65535, 7], inProgress: true, turnSeat: 2, reversed: true,
      turnTicksLeft: 70000, winnerSeat: 3, ready: [true, false, true, true],
    });
  });

  it('decodes an idle CardTableState with no turn and no winner', () => {
    const b = new flatbuffers.Builder(64);
    const msg = buildState(b, {
      tableId: 1, seats: [0, 0], inProgress: false, turnSeat: 1, reversed: false, turnTicksLeft: 0,
      hasWinner: false, winnerSeat: 1,
    });
    // No ready list: every seat reads as not ready.
    expect(decode(finish(b, Message.CardTableState, msg))).toEqual({
      type: 'cardTableState', tableId: 1, seats: [0, 0], inProgress: false, turnSeat: null, reversed: false,
      turnTicksLeft: 0, winnerSeat: null, ready: [false, false],
    });
  });

  it('decodes an empty CardTableState', () => {
    const b = new flatbuffers.Builder(32);
    CardTableState.startCardTableState(b);
    expect(decode(finish(b, Message.CardTableState, CardTableState.endCardTableState(b)))).toEqual({
      type: 'cardTableState', tableId: 0, seats: [], inProgress: false, turnSeat: null, reversed: false,
      turnTicksLeft: 0, winnerSeat: null, ready: [],
    });
  });

  it('decodes every CardTableResult code', () => {
    for (const code of Object.values(CardTableResultCode).filter((v) => typeof v === 'number')) {
      const b = new flatbuffers.Builder(16);
      const msg = CardTableResult.createCardTableResult(b, code);
      expect(decode(finish(b, Message.CardTableResult, msg))).toEqual({ type: 'cardTableResult', code });
    }
  });
});

describe('Session card table send functions', () => {
  class FakeTransport implements Transport {
    onMessage: ((b: Uint8Array) => void) | null = null;
    onOpen: (() => void) | null = null;
    onClose: (() => void) | null = null;
    sent: Uint8Array[] = [];
    send(b: Uint8Array) { this.sent.push(b); }
    close() {}
  }

  it('sends CardTableSit, Leave, Ready and Shed moves once connected, and nothing before', () => {
    const t = new FakeTransport();
    const s = new Session({ factory: () => t, store: { get: () => '', set: () => {} }, onMessage: () => {} });
    s.start();
    s.sendCardTableSit(1);
    expect(t.sent.length).toBe(0);

    const b = new flatbuffers.Builder(64);
    const tok = b.createString('tok');
    Welcome.startWelcome(b);
    Welcome.addGuestToken(b, tok);
    Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
    t.onMessage?.(finish(b, Message.Welcome, Welcome.endWelcome(b)));

    s.sendCardTableSit(4);
    s.sendCardTableLeave();
    expect(t.sent.length).toBe(2);
    expect((envelope(t.sent[0], Message.CardTableSit).msg(new CardTableSit()) as CardTableSit).tableId()).toBe(4);
    envelope(t.sent[1], Message.CardTableLeave);

    s.sendCardTableReady();
    s.sendShedPlay({ colour: ShedColour.None, symbol: ShedSymbol.Wild }, ShedColour.One);
    s.sendShedDraw();
    expect(t.sent.length).toBe(5);
    envelope(t.sent[2], Message.CardTableReady);
    const play = envelope(t.sent[3], Message.ShedPlay).msg(new ShedPlay()) as ShedPlay;
    expect(play.card()?.symbol()).toBe(ShedSymbol.Wild);
    expect(play.chosenColour()).toBe(ShedColour.One);
    envelope(t.sent[4], Message.ShedDraw);
  });
});
