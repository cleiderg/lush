import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import { Envelope, Hello, Message, Reload, Welcome, Vec2 } from '../protocol';
import { decode } from './codec';
import { Session, type TokenStore } from './session';
import type { Transport } from './transport';

class FakeTransport implements Transport {
  onMessage: ((b: Uint8Array) => void) | null = null;
  onOpen: (() => void) | null = null;
  onClose: (() => void) | null = null;
  sent: Uint8Array[] = [];
  send(b: Uint8Array) { this.sent.push(b); }
  close() {}
}

function welcome(token: string): Uint8Array {
  const b = new flatbuffers.Builder(64);
  const t = b.createString(token);
  Welcome.startWelcome(b);
  Welcome.addPlayerId(b, 7);
  Welcome.addGuestToken(b, t);
  Welcome.addSpawn(b, Vec2.createVec2(b, 1, 2));
  Welcome.addTickRate(b, 20);
  b.finish(Envelope.createEnvelope(b, Message.Welcome, Welcome.endWelcome(b)));
  return b.asUint8Array().slice();
}

function reload(): Uint8Array {
  const b = new flatbuffers.Builder(32);
  b.finish(Envelope.createEnvelope(b, Message.Reload, Reload.createReload(b, b.createString('v'))));
  return b.asUint8Array().slice();
}

function helloToken(bytes: Uint8Array): string {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
  expect(env.msgType()).toBe(Message.Hello);
  return (env.msg(new Hello()) as Hello).guestToken() ?? '';
}

function setup() {
  const transports: FakeTransport[] = [];
  const timers: { fn: () => void; ms: number }[] = [];
  let saved = '';
  const store: TokenStore = { get: () => saved, set: (t) => { saved = t; } };
  const reloads: number[] = [];
  const session = new Session({
    factory: () => { const t = new FakeTransport(); transports.push(t); return t; },
    store,
    onMessage: () => {},
    onReload: () => reloads.push(1),
    setTimer: (fn, ms) => { timers.push({ fn, ms }); return timers.length; },
    clearTimer: () => {},
  });
  return { session, transports, timers, reloads, store };
}

describe('Session', () => {
  it('says Hello, stores the token, and reconnects with backoff using the same token', () => {
    const { session, transports, timers } = setup();
    session.start();
    transports[0].onOpen?.();
    expect(helloToken(transports[0].sent[0])).toBe('');
    transports[0].onMessage?.(welcome('guest-1'));
    expect(session.connected).toBe(true);

    transports[0].onClose?.();
    expect(session.connected).toBe(false);
    expect(timers[0].ms).toBe(500);
    timers[0].fn();
    transports[1].onOpen?.();
    expect(helloToken(transports[1].sent[0])).toBe('guest-1');

    // Connection fails before Welcome: backoff grows.
    transports[1].onClose?.();
    expect(timers[1].ms).toBe(1000);
    timers[1].fn();
    transports[2].onClose?.();
    expect(timers[2].ms).toBe(2000);
    timers[2].fn();
    transports[3].onOpen?.();
    transports[3].onMessage?.(welcome('guest-1'));
    transports[3].onClose?.();
    expect(timers[3].ms).toBe(500); // reset after success
  });

  it('does not reconnect after stop', () => {
    const { session, transports, timers } = setup();
    session.start();
    session.stop();
    transports[0].onClose?.();
    expect(timers.length).toBe(0);
  });

  it('calls onReload for a Reload message and does not reconnect (AC5)', () => {
    const { session, transports, timers, reloads } = setup();
    session.start();
    transports[0].onMessage?.(reload());
    expect(reloads.length).toBe(1);
    transports[0].onClose?.();
    expect(timers.length).toBe(0);
  });

  it('decodes welcome', () => {
    expect(decode(welcome('x'))).toMatchObject({ type: 'welcome', playerId: 7, guestToken: 'x', tickRate: 20 });
  });
});
