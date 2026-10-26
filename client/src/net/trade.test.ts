import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  Envelope, Message, TradeAccept, TradeCancel, TradeItem, TradeOffer, TradeRemove, TradeRequest, TradeRequestNotice,
  TradeResult, TradeResultCode, TradeSetGold, TradeState, Vec2, Welcome,
} from '../protocol';
import {
  decode, encodeTradeAccept, encodeTradeCancel, encodeTradeOffer, encodeTradeRemove, encodeTradeRequest,
  encodeTradeSetGold,
} from './codec';
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

describe('encode trade messages', () => {
  it('encodes TradeRequest', () => {
    const m = envelope(encodeTradeRequest(65000), Message.TradeRequest).msg(new TradeRequest()) as TradeRequest;
    expect(m.targetId()).toBe(65000);
  });

  it('encodes TradeOffer', () => {
    const m = envelope(encodeTradeOffer(27, 70000), Message.TradeOffer).msg(new TradeOffer()) as TradeOffer;
    expect([m.slot(), m.quantity()]).toEqual([27, 70000]);
  });

  it('encodes TradeRemove', () => {
    const m = envelope(encodeTradeRemove(5), Message.TradeRemove).msg(new TradeRemove()) as TradeRemove;
    expect(m.index()).toBe(5);
  });

  it('encodes TradeSetGold', () => {
    const m = envelope(encodeTradeSetGold(4_000_000_000), Message.TradeSetGold).msg(new TradeSetGold()) as TradeSetGold;
    expect(m.gold()).toBe(4_000_000_000);
  });

  it('encodes TradeAccept', () => {
    const m = envelope(encodeTradeAccept(2), Message.TradeAccept).msg(new TradeAccept()) as TradeAccept;
    expect(m.screen()).toBe(2);
  });

  it('encodes TradeCancel', () => {
    expect(envelope(encodeTradeCancel(), Message.TradeCancel).msg(new TradeCancel())).toBeInstanceOf(TradeCancel);
  });
});

describe('decode trade messages', () => {
  it('decodes TradeRequestNotice', () => {
    const b = new flatbuffers.Builder(32);
    const msg = TradeRequestNotice.createTradeRequestNotice(b, 42);
    expect(decode(finish(b, Message.TradeRequestNotice, msg))).toEqual({ type: 'tradeRequestNotice', fromId: 42 });
  });

  it('decodes TradeState with both offers', () => {
    const b = new flatbuffers.Builder(128);
    TradeState.startMyItemsVector(b, 2);
    TradeItem.createTradeItem(b, 9, 70000);
    TradeItem.createTradeItem(b, 3, 1);
    const mine = b.endVector();
    TradeState.startPartnerItemsVector(b, 1);
    TradeItem.createTradeItem(b, 7, 2);
    const theirs = b.endVector();
    const msg = TradeState.createTradeState(b, 12, 2, mine, 150, theirs, 99, true, false);
    expect(decode(finish(b, Message.TradeState, msg))).toEqual({
      type: 'tradeState',
      partnerId: 12,
      screen: 2,
      myItems: [{ itemId: 3, quantity: 1 }, { itemId: 9, quantity: 70000 }],
      myGold: 150,
      partnerItems: [{ itemId: 7, quantity: 2 }],
      partnerGold: 99,
      myAccepted: true,
      partnerAccepted: false,
    });
  });

  it('decodes an empty TradeState', () => {
    const b = new flatbuffers.Builder(64);
    TradeState.startTradeState(b);
    TradeState.addPartnerId(b, 4);
    TradeState.addScreen(b, 1);
    expect(decode(finish(b, Message.TradeState, TradeState.endTradeState(b)))).toEqual({
      type: 'tradeState', partnerId: 4, screen: 1, myItems: [], myGold: 0, partnerItems: [], partnerGold: 0,
      myAccepted: false, partnerAccepted: false,
    });
  });

  it('decodes TradeResult', () => {
    const b = new flatbuffers.Builder(32);
    const msg = TradeResult.createTradeResult(b, TradeResultCode.InventoryFull);
    expect(decode(finish(b, Message.TradeResult, msg))).toEqual({ type: 'tradeResult', code: TradeResultCode.InventoryFull });
  });
});

describe('Session trade send functions', () => {
  class FakeTransport implements Transport {
    onMessage: ((b: Uint8Array) => void) | null = null;
    onOpen: (() => void) | null = null;
    onClose: (() => void) | null = null;
    sent: Uint8Array[] = [];
    send(b: Uint8Array) { this.sent.push(b); }
    close() {}
  }

  it('sends each trade message once connected, and nothing before', () => {
    const t = new FakeTransport();
    const s = new Session({ factory: () => t, store: { get: () => '', set: () => {} }, onMessage: () => {} });
    s.start();
    s.sendTradeRequest(1);
    expect(t.sent.length).toBe(0);

    const b = new flatbuffers.Builder(64);
    const tok = b.createString('tok');
    Welcome.startWelcome(b);
    Welcome.addGuestToken(b, tok);
    Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
    t.onMessage?.(finish(b, Message.Welcome, Welcome.endWelcome(b)));

    s.sendTradeRequest(12);
    s.sendTradeOffer(3, 5);
    s.sendTradeRemove(1);
    s.sendTradeSetGold(250);
    s.sendTradeAccept(1);
    s.sendTradeCancel();
    const types = t.sent.map((bytes) => Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes)).msgType());
    expect(types).toEqual([
      Message.TradeRequest, Message.TradeOffer, Message.TradeRemove, Message.TradeSetGold, Message.TradeAccept,
      Message.TradeCancel,
    ]);
    expect((envelope(t.sent[0], Message.TradeRequest).msg(new TradeRequest()) as TradeRequest).targetId()).toBe(12);
    const offer = envelope(t.sent[1], Message.TradeOffer).msg(new TradeOffer()) as TradeOffer;
    expect([offer.slot(), offer.quantity()]).toEqual([3, 5]);
    expect((envelope(t.sent[2], Message.TradeRemove).msg(new TradeRemove()) as TradeRemove).index()).toBe(1);
    expect((envelope(t.sent[3], Message.TradeSetGold).msg(new TradeSetGold()) as TradeSetGold).gold()).toBe(250);
    expect((envelope(t.sent[4], Message.TradeAccept).msg(new TradeAccept()) as TradeAccept).screen()).toBe(1);
  });
});
