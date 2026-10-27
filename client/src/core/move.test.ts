import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import { decode } from '../net/codec';
import { ActionFlags, Envelope, Input, Message, Vec2, Welcome } from '../protocol';
import { RUN_SPEED, WALK_SPEED } from '../state/prediction';
import { GameState } from '../state/world';
import { sendMove } from './move';

function welcomeBytes(walk?: number, run?: number): Uint8Array {
  const b = new flatbuffers.Builder(64);
  const tok = b.createString('t');
  Welcome.startWelcome(b);
  Welcome.addPlayerId(b, 1);
  Welcome.addGuestToken(b, tok);
  Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
  Welcome.addTickRate(b, 20);
  if (walk !== undefined) Welcome.addWalkSpeed(b, walk);
  if (run !== undefined) Welcome.addRunSpeed(b, run);
  b.finish(Envelope.createEnvelope(b, Message.Welcome, Welcome.endWelcome(b)));
  return b.asUint8Array().slice();
}

function inputOf(bytes: Uint8Array): Input {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
  expect(env.msgType()).toBe(Message.Input);
  return env.msg(new Input()) as Input;
}

function welcomed(walk = 4.5, run = 8): GameState {
  const g = new GameState();
  const msg = decode(welcomeBytes(walk, run));
  if (!msg) throw new Error('welcome did not decode');
  g.handle(msg, 0);
  return g;
}

describe('Welcome speeds (AC1)', () => {
  it('start at the D-044 defaults and take the Welcome values', () => {
    const g = new GameState();
    expect(WALK_SPEED).toBe(4);
    expect(RUN_SPEED).toBe(7);
    expect(g.walkSpeed).toBe(4);
    expect(g.runSpeed).toBe(7);
    const m = decode(welcomeBytes(4.5, 8));
    if (!m) throw new Error('welcome did not decode');
    g.handle(m, 0);
    expect(g.walkSpeed).toBe(4.5);
    expect(g.runSpeed).toBe(8);
  });

  it('keeps the current speeds when Welcome omits them', () => {
    const g = new GameState();
    const m = decode(welcomeBytes());
    if (!m) throw new Error('welcome did not decode');
    g.handle(m, 0);
    expect(g.walkSpeed).toBe(4);
    expect(g.runSpeed).toBe(7);
  });
});

describe('sendMove Run flag and speed (AC2)', () => {
  it('with Run on, sets the Run bit and predicts runSpeed * dt', () => {
    const g = welcomed();
    const sent: Uint8Array[] = [];
    sendMove(g, { send: (b) => sent.push(b) }, 1, 0, 0.05, true);
    expect(sent).toHaveLength(1);
    const input = inputOf(sent[0]);
    expect(input.seq()).toBe(1);
    expect(input.actions() & ActionFlags.Run).toBe(ActionFlags.Run);
    expect(g.local.x).toBeCloseTo(8 * 0.05);
  });

  it('with Run off, clears the Run bit and predicts walkSpeed * dt', () => {
    const g = welcomed();
    const sent: Uint8Array[] = [];
    sendMove(g, { send: (b) => sent.push(b) }, 1, 0, 0.05, false);
    const input = inputOf(sent[0]);
    expect(input.actions() & ActionFlags.Run).toBe(0);
    expect(g.local.x).toBeCloseTo(4.5 * 0.05);
  });
});

describe('reconcile replays at each input speed (AC3)', () => {
  it('toggling Run with inputs in flight matches the server', () => {
    const g = welcomed();
    const send = { send: () => {} };
    // Inputs 1-2 walk, 3-4 run, 5-6 walk again.
    const runs = [false, false, true, true, false, false];
    for (const r of runs) sendMove(g, send, 1, 0, 0.05, r);
    // The server applied inputs 1-3 with the speed each was sent with.
    const serverX = (4.5 * 2 + 8) * 0.05;
    g.handle({ type: 'snapshot', tick: 1, lastInputSeq: 3, entities: [{ id: 1, x: serverX, z: 0, heading: 0, animation: 0 }] }, 0);
    expect(g.prediction.pendingCount).toBe(3);
    // Replay: input 4 at run speed, 5-6 at walk speed. The total equals the server's for all six.
    expect(g.local.x).toBeCloseTo((4.5 * 4 + 8 * 2) * 0.05);
  });
});
