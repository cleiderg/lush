import * as flatbuffers from 'flatbuffers';
import {
  ChatMessage, ChatSend, Entity, Envelope, Hello, Input, Message, Ping, Pong, ProtocolVersion,
  Reload, Snapshot, Vec2, Welcome,
} from '../protocol';

export interface EntityState {
  id: number;
  /** Metres from the map origin. */
  x: number;
  z: number;
  /** Radians. */
  heading: number;
  animation: number;
}

export type ServerMessage =
  | { type: 'welcome'; playerId: number; guestToken: string; spawn: { x: number; z: number }; tickRate: number }
  | { type: 'snapshot'; tick: number; lastInputSeq: number; entities: EntityState[] }
  | { type: 'chat'; speakerId: number; text: string }
  | { type: 'pong'; token: number }
  | { type: 'reload'; reason: string };

function wrap(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

export function encodeHello(guestToken: string): Uint8Array {
  const b = new flatbuffers.Builder(64);
  const tok = b.createString(guestToken);
  return wrap(b, Message.Hello, Hello.createHello(b, ProtocolVersion.Current, tok));
}

export function encodeInput(seq: number, moveX: number, moveZ: number, facing: number, actions = 0): Uint8Array {
  const b = new flatbuffers.Builder(64);
  Input.startInput(b);
  Input.addSeq(b, seq);
  Input.addMove(b, Vec2.createVec2(b, moveX, moveZ));
  Input.addFacing(b, facing);
  Input.addActions(b, actions);
  return wrap(b, Message.Input, Input.endInput(b));
}

export function encodeChat(text: string): Uint8Array {
  const b = new flatbuffers.Builder(64);
  return wrap(b, Message.ChatSend, ChatSend.createChatSend(b, b.createString(text)));
}

export function encodePing(token: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.Ping, Ping.createPing(b, token));
}

export function decode(bytes: Uint8Array): ServerMessage | null {
  let env: Envelope;
  try {
    env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
  } catch {
    return null;
  }
  switch (env.msgType()) {
    case Message.Welcome: {
      const m = env.msg(new Welcome()) as Welcome;
      const s = m.spawn();
      return {
        type: 'welcome',
        playerId: m.playerId(),
        guestToken: m.guestToken() ?? '',
        spawn: { x: s?.x() ?? 0, z: s?.z() ?? 0 },
        tickRate: m.tickRate(),
      };
    }
    case Message.Snapshot: {
      const m = env.msg(new Snapshot()) as Snapshot;
      const entities: EntityState[] = [];
      const e = new Entity();
      for (let i = 0; i < m.entitiesLength(); i++) {
        m.entities(i, e);
        entities.push({
          id: e.id(),
          x: e.x() / 10,
          z: e.z() / 10,
          heading: (e.rotation() / 256) * Math.PI * 2,
          animation: e.animation(),
        });
      }
      return { type: 'snapshot', tick: m.tick(), lastInputSeq: m.lastInputSeq(), entities };
    }
    case Message.ChatMessage: {
      const m = env.msg(new ChatMessage()) as ChatMessage;
      return { type: 'chat', speakerId: m.speakerId(), text: m.text() ?? '' };
    }
    case Message.Pong:
      return { type: 'pong', token: (env.msg(new Pong()) as Pong).token() };
    case Message.Reload:
      return { type: 'reload', reason: (env.msg(new Reload()) as Reload).reason() ?? '' };
    default:
      return null;
  }
}
