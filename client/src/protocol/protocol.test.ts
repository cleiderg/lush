import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  ActionFlags, ChatMessage, ChatSend, Entity, Envelope, Hello, Input, Message, Ping, Pong,
  ProtocolVersion, Reload, RestartCountdown, Snapshot, Vec2, Welcome,
} from './index.js';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array();
}

function open<T>(buf: Uint8Array, type: Message, m: T): T {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(buf));
  expect(env.msgType()).toBe(type);
  return env.msg(m as never) as T;
}

function buildSnapshot(n: number): Uint8Array {
  const b = new flatbuffers.Builder(1024);
  Snapshot.startEntitiesVector(b, n);
  for (let i = n - 1; i >= 0; i--) {
    Entity.createEntity(b, i, i * 10 - 500, 0 - i * 7, i, i % 4);
  }
  const ents = b.endVector();
  Snapshot.startSnapshot(b);
  Snapshot.addTick(b, 12345);
  Snapshot.addLastInputSeq(b, 678);
  Snapshot.addEntities(b, ents);
  return finish(b, Message.Snapshot, Snapshot.endSnapshot(b));
}

describe('protocol', () => {
  it('exposes the version constant', () => {
    expect(ProtocolVersion.Current).toBe(2);
  });

  it('keeps a 101-entity snapshot under 1200 bytes', () => {
    expect(buildSnapshot(101).length).toBeLessThan(1200);
  });

  it('round-trips Hello', () => {
    const b = new flatbuffers.Builder(64);
    const tok = b.createString('guest-abc');
    const buf = finish(b, Message.Hello, Hello.createHello(b, ProtocolVersion.Current, tok));
    const m = open(buf, Message.Hello, new Hello());
    expect(m.protocolVersion()).toBe(ProtocolVersion.Current);
    expect(m.guestToken()).toBe('guest-abc');
  });

  it('round-trips Welcome', () => {
    const b = new flatbuffers.Builder(64);
    const tok = b.createString('tok');
    Welcome.startWelcome(b);
    Welcome.addPlayerId(b, 42);
    Welcome.addGuestToken(b, tok);
    Welcome.addSpawn(b, Vec2.createVec2(b, 1.5, -2.5));
    Welcome.addTickRate(b, 20);
    const buf = finish(b, Message.Welcome, Welcome.endWelcome(b));
    const m = open(buf, Message.Welcome, new Welcome());
    expect(m.playerId()).toBe(42);
    expect(m.guestToken()).toBe('tok');
    expect(m.spawn()!.x()).toBe(1.5);
    expect(m.spawn()!.z()).toBe(-2.5);
    expect(m.tickRate()).toBe(20);
  });

  it('round-trips Input', () => {
    const b = new flatbuffers.Builder(64);
    Input.startInput(b);
    Input.addSeq(b, 99);
    Input.addMove(b, Vec2.createVec2(b, 0.5, -1));
    Input.addFacing(b, 3.25);
    Input.addActions(b, ActionFlags.Attack | ActionFlags.Jump);
    const buf = finish(b, Message.Input, Input.endInput(b));
    const m = open(buf, Message.Input, new Input());
    expect(m.seq()).toBe(99);
    expect(m.move()!.x()).toBe(0.5);
    expect(m.move()!.z()).toBe(-1);
    expect(m.facing()).toBe(3.25);
    expect(m.actions()).toBe(ActionFlags.Attack | ActionFlags.Jump);
  });

  it('round-trips Snapshot', () => {
    const m = open(buildSnapshot(101), Message.Snapshot, new Snapshot());
    expect(m.tick()).toBe(12345);
    expect(m.lastInputSeq()).toBe(678);
    expect(m.entitiesLength()).toBe(101);
    for (let i = 0; i < 101; i++) {
      const e = m.entities(i)!;
      expect([e.id(), e.x(), e.z(), e.rotation(), e.animation()]).toEqual([
        i, i * 10 - 500, 0 - i * 7, i, i % 4,
      ]);
    }
  });

  it('round-trips ChatSend and ChatMessage', () => {
    let b = new flatbuffers.Builder(64);
    let buf = finish(b, Message.ChatSend, ChatSend.createChatSend(b, b.createString('hello anteaters')));
    expect(open(buf, Message.ChatSend, new ChatSend()).text()).toBe('hello anteaters');

    b = new flatbuffers.Builder(64);
    buf = finish(b, Message.ChatMessage, ChatMessage.createChatMessage(b, 7, b.createString('zot')));
    const m = open(buf, Message.ChatMessage, new ChatMessage());
    expect(m.speakerId()).toBe(7);
    expect(m.text()).toBe('zot');
  });

  it('round-trips Ping and Pong', () => {
    let b = new flatbuffers.Builder(32);
    let buf = finish(b, Message.Ping, Ping.createPing(b, 0xdeadbeef));
    expect(open(buf, Message.Ping, new Ping()).token()).toBe(0xdeadbeef);

    b = new flatbuffers.Builder(32);
    buf = finish(b, Message.Pong, Pong.createPong(b, 0xcafef00d));
    expect(open(buf, Message.Pong, new Pong()).token()).toBe(0xcafef00d);
  });

  it('round-trips Reload', () => {
    const b = new flatbuffers.Builder(32);
    const buf = finish(b, Message.Reload, Reload.createReload(b, b.createString('protocol mismatch')));
    expect(open(buf, Message.Reload, new Reload()).reason()).toBe('protocol mismatch');
  });

  it('round-trips RestartCountdown', () => {
    const b = new flatbuffers.Builder(32);
    const buf = finish(b, Message.RestartCountdown, RestartCountdown.createRestartCountdown(b, 42));
    const m = open(buf, Message.RestartCountdown, new RestartCountdown());
    expect(m.secondsRemaining()).toBe(42);
  });
});
