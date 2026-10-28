import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  CardTableLeave, CardTableResult, CardTableResultCode, CardTableSit, CardTableState, Envelope, Message, Vec2, Welcome,
} from '../protocol';
import { decode, encodeCardTableLeave, encodeCardTableSit } from './codec';
import { Session } from './session';
import type { Transport } from './transport';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
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

describe('decode card table messages', () => {
  it('decodes a CardTableState in progress with a winner', () => {
    const b = new flatbuffers.Builder(64);
    const seats = CardTableState.createSeatsVector(b, [12, 0, 65535, 7]);
    const msg = CardTableState.createCardTableState(b, 3, seats, true, 2, true, 70000, true, 3);
    expect(decode(finish(b, Message.CardTableState, msg))).toEqual({
      type: 'cardTableState', tableId: 3, seats: [12, 0, 65535, 7], inProgress: true, turnSeat: 2, reversed: true,
      turnTicksLeft: 70000, winnerSeat: 3,
    });
  });

  it('decodes an idle CardTableState with no turn and no winner', () => {
    const b = new flatbuffers.Builder(64);
    const seats = CardTableState.createSeatsVector(b, [0, 0]);
    const msg = CardTableState.createCardTableState(b, 1, seats, false, 1, false, 0, false, 1);
    expect(decode(finish(b, Message.CardTableState, msg))).toEqual({
      type: 'cardTableState', tableId: 1, seats: [0, 0], inProgress: false, turnSeat: null, reversed: false,
      turnTicksLeft: 0, winnerSeat: null,
    });
  });

  it('decodes an empty CardTableState', () => {
    const b = new flatbuffers.Builder(32);
    CardTableState.startCardTableState(b);
    expect(decode(finish(b, Message.CardTableState, CardTableState.endCardTableState(b)))).toEqual({
      type: 'cardTableState', tableId: 0, seats: [], inProgress: false, turnSeat: null, reversed: false,
      turnTicksLeft: 0, winnerSeat: null,
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

  it('sends CardTableSit and CardTableLeave once connected, and nothing before', () => {
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
  });
});
