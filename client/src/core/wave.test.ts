import * as flatbuffers from 'flatbuffers';
import { describe, expect, it, vi } from 'vitest';
import { encodeEmote } from '../net/codec';
import { Emote, EmoteSend, Envelope, Message, Vec2, Welcome } from '../protocol';
import { Session } from '../net/session';
import type { Transport } from '../net/transport';
import { GameState } from '../state/world';
import { sendWave } from './wave';

function emoteOf(bytes: Uint8Array): Emote {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
  expect(env.msgType()).toBe(Message.EmoteSend);
  return (env.msg(new EmoteSend()) as EmoteSend).emote();
}

describe('wave', () => {
  it('encodes an EmoteSend envelope carrying Emote.Wave', () => {
    expect(emoteOf(encodeEmote(Emote.Wave))).toBe(Emote.Wave);
  });

  it('sends exactly one EmoteSend(Wave) through the session', () => {
    const sent: Uint8Array[] = [];
    const t: Transport = {
      onMessage: null, onOpen: null, onClose: null,
      send: (b) => { sent.push(b); },
      close: () => {},
    };
    const session = new Session({
      factory: () => t,
      store: { get: () => '', set: () => {} },
      onMessage: (m) => new GameState().handle(m, 0),
      setTimer: vi.fn(),
    });
    session.start();
    t.onOpen?.();
    // Mark ready via a Welcome the way the session expects.
    const b = new flatbuffers.Builder(64);
    const tok = b.createString('x');
    Welcome.startWelcome(b);
    Welcome.addPlayerId(b, 1);
    Welcome.addGuestToken(b, tok);
    Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
    Welcome.addTickRate(b, 20);
    b.finish(Envelope.createEnvelope(b, Message.Welcome, Welcome.endWelcome(b)));
    t.onMessage?.(b.asUint8Array().slice());
    const before = sent.length;
    sendWave(session);
    expect(sent.length - before).toBe(1);
    expect(emoteOf(sent[sent.length - 1])).toBe(Emote.Wave);
  });
});
