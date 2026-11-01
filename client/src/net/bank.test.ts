import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  BankAmount, BankClose, BankDeposit, BankDepositEquipment, BankDepositInventory, BankOpen, BankResult, BankResultCode,
  BankSlot, BankState, BankWithdraw, Envelope, Message, Vec2, Welcome,
} from '../protocol';
import {
  decode, encodeBankClose, encodeBankDeposit, encodeBankDepositEquipment, encodeBankDepositInventory, encodeBankOpen,
  encodeBankWithdraw,
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

function amounts(encode: (itemId: number, amount: number) => Uint8Array, type: Message, read: (e: Envelope) => BankDeposit | BankWithdraw) {
  return [1, 5, BankAmount.All, 1234567].map((amount) => {
    const m = read(envelope(encode(42, amount), type));
    return [m.itemId(), m.amount()];
  });
}

describe('encode bank messages', () => {
  it('encodes BankOpen and BankClose', () => {
    expect(envelope(encodeBankOpen(), Message.BankOpen).msg(new BankOpen())).toBeInstanceOf(BankOpen);
    expect(envelope(encodeBankClose(), Message.BankClose).msg(new BankClose())).toBeInstanceOf(BankClose);
  });

  it('encodes BankDeposit of one, five, all and a typed amount', () => {
    expect(amounts(encodeBankDeposit, Message.BankDeposit, (e) => e.msg(new BankDeposit()) as BankDeposit)).toEqual([
      [42, 1], [42, 5], [42, 4294967295], [42, 1234567],
    ]);
    const all = envelope(encodeBankDeposit(7), Message.BankDeposit).msg(new BankDeposit()) as BankDeposit;
    expect(all.amount()).toBe(BankAmount.All);
  });

  it('encodes BankWithdraw of one, five, all and a typed amount', () => {
    expect(amounts(encodeBankWithdraw, Message.BankWithdraw, (e) => e.msg(new BankWithdraw()) as BankWithdraw)).toEqual([
      [42, 1], [42, 5], [42, 4294967295], [42, 1234567],
    ]);
    const all = envelope(encodeBankWithdraw(7), Message.BankWithdraw).msg(new BankWithdraw()) as BankWithdraw;
    expect(all.amount()).toBe(BankAmount.All);
  });

  it('encodes BankDepositInventory and BankDepositEquipment', () => {
    expect(envelope(encodeBankDepositInventory(), Message.BankDepositInventory).msg(new BankDepositInventory()))
      .toBeInstanceOf(BankDepositInventory);
    expect(envelope(encodeBankDepositEquipment(), Message.BankDepositEquipment).msg(new BankDepositEquipment()))
      .toBeInstanceOf(BankDepositEquipment);
  });
});

describe('decode bank messages', () => {
  it('decodes a full 200-slot BankState in server order', () => {
    const b = new flatbuffers.Builder(4096);
    const want = Array.from({ length: 200 }, (_, i) => ({ itemId: 1000 - i * 3, quantity: i === 0 ? 2 ** 40 : i + 1 }));
    BankState.startSlotsVector(b, want.length);
    for (let i = want.length - 1; i >= 0; i--) BankSlot.createBankSlot(b, want[i].itemId, BigInt(want[i].quantity));
    const msg = BankState.createBankState(b, b.endVector());
    expect(decode(finish(b, Message.BankState, msg))).toEqual({ type: 'bankState', slots: want });
  });

  it('decodes an empty BankState', () => {
    const b = new flatbuffers.Builder(32);
    BankState.startBankState(b);
    expect(decode(finish(b, Message.BankState, BankState.endBankState(b)))).toEqual({ type: 'bankState', slots: [] });
  });

  it('decodes every BankResult code', () => {
    const codes = Object.values(BankResultCode).filter((v): v is BankResultCode => typeof v === 'number');
    expect(codes.length).toBe(10);
    for (const code of codes) {
      const b = new flatbuffers.Builder(32);
      expect(decode(finish(b, Message.BankResult, BankResult.createBankResult(b, code)))).toEqual({ type: 'bankResult', code });
    }
  });
});

describe('Session bank send functions', () => {
  class FakeTransport implements Transport {
    onMessage: ((b: Uint8Array) => void) | null = null;
    onOpen: (() => void) | null = null;
    onClose: (() => void) | null = null;
    sent: Uint8Array[] = [];
    send(b: Uint8Array) { this.sent.push(b); }
    close() {}
  }

  it('sends each bank message once connected', () => {
    const t = new FakeTransport();
    const s = new Session({ factory: () => t, store: { get: () => '', set: () => {} }, onMessage: () => {} });
    s.start();
    const b = new flatbuffers.Builder(64);
    const tok = b.createString('tok');
    Welcome.startWelcome(b);
    Welcome.addGuestToken(b, tok);
    Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
    t.onMessage?.(finish(b, Message.Welcome, Welcome.endWelcome(b)));

    s.sendBankOpen();
    s.sendBankDeposit(3, 5);
    s.sendBankWithdraw(4, BankAmount.All);
    s.sendBankDepositInventory();
    s.sendBankDepositEquipment();
    s.sendBankClose();
    const types = t.sent.map((bytes) => Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes)).msgType());
    expect(types).toEqual([
      Message.BankOpen, Message.BankDeposit, Message.BankWithdraw, Message.BankDepositInventory,
      Message.BankDepositEquipment, Message.BankClose,
    ]);
    const dep = envelope(t.sent[1], Message.BankDeposit).msg(new BankDeposit()) as BankDeposit;
    expect([dep.itemId(), dep.amount()]).toEqual([3, 5]);
    const wd = envelope(t.sent[2], Message.BankWithdraw).msg(new BankWithdraw()) as BankWithdraw;
    expect([wd.itemId(), wd.amount()]).toEqual([4, BankAmount.All]);
  });
});
