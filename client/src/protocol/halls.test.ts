import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  AlarmCause, DoorLog, DoorLogAction, DoorLogEntry, DoorLogRequest, DoorPublicState, DoorState, DoorVerb, Envelope,
  HallAlarm, HallClaim, HallDoorId, HallGiveUp, HallInfo, HallResult, HallResultCode, HallsState, Message,
  VaultDeposit, VaultWithdraw,
} from './index.js';
import { DoorAction } from './door-action.js';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array();
}

function open<T>(buf: Uint8Array, type: Message, m: T): T {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(buf));
  expect(env.msgType()).toBe(type);
  return env.msg(m as never) as T;
}

describe('hall protocol', () => {
  it('round-trips client messages', () => {
    let b = new flatbuffers.Builder(32);
    expect(open(finish(b, Message.HallClaim, HallClaim.createHallClaim(b, 6)), Message.HallClaim, new HallClaim()).hallId()).toBe(6);

    b = new flatbuffers.Builder(16);
    HallGiveUp.startHallGiveUp(b);
    open(finish(b, Message.HallGiveUp, HallGiveUp.endHallGiveUp(b)), Message.HallGiveUp, new HallGiveUp());

    for (const door of [HallDoorId.Outer0, HallDoorId.Outer1, HallDoorId.Inner]) {
      for (const verb of [DoorVerb.Open, DoorVerb.Close, DoorVerb.Lock, DoorVerb.Unlock, DoorVerb.Force, DoorVerb.Repair]) {
        b = new flatbuffers.Builder(32);
        const a = open(finish(b, Message.DoorAction, DoorAction.createDoorAction(b, 3, door, verb)), Message.DoorAction, new DoorAction());
        expect([a.hallId(), a.door(), a.action()]).toEqual([3, door, verb]);
      }
    }

    b = new flatbuffers.Builder(16);
    DoorLogRequest.startDoorLogRequest(b);
    open(finish(b, Message.DoorLogRequest, DoorLogRequest.endDoorLogRequest(b)), Message.DoorLogRequest, new DoorLogRequest());

    b = new flatbuffers.Builder(32);
    expect(open(finish(b, Message.VaultDeposit, VaultDeposit.createVaultDeposit(b, 5_000_000_000n)),
      Message.VaultDeposit, new VaultDeposit()).amount()).toBe(5_000_000_000n);
    b = new flatbuffers.Builder(32);
    expect(open(finish(b, Message.VaultWithdraw, VaultWithdraw.createVaultWithdraw(b, 750n)),
      Message.VaultWithdraw, new VaultWithdraw()).amount()).toBe(750n);
  });

  it('round-trips HallsState with an owned and an empty hall', () => {
    const b = new flatbuffers.Builder(128);
    const name = b.createString('Anteaters');
    const owned = HallInfo.createHallInfo(b, 1, 70000, name, 11);
    const empty = HallInfo.createHallInfo(b, 2, 0, 0, 0);
    const s = open(finish(b, Message.HallsState,
      HallsState.createHallsState(b, HallsState.createHallsVector(b, [owned, empty]))), Message.HallsState, new HallsState());
    expect(s.hallsLength()).toBe(2);
    const h0 = s.halls(0)!;
    expect([h0.id(), h0.groupId(), h0.groupName(), h0.banner()]).toEqual([1, 70000, 'Anteaters', 11]);
    const h1 = s.halls(1)!;
    expect([h1.id(), h1.groupId(), h1.groupName()]).toEqual([2, 0, null]);
  });

  it('round-trips DoorState', () => {
    for (const st of [DoorPublicState.Open, DoorPublicState.Shut, DoorPublicState.Broken]) {
      const b = new flatbuffers.Builder(32);
      const d = open(finish(b, Message.DoorState, DoorState.createDoorState(b, 4, HallDoorId.Inner, st, true, 275)),
        Message.DoorState, new DoorState());
      expect([d.hallId(), d.door(), d.state(), d.beingForced(), d.hp()]).toEqual([4, HallDoorId.Inner, st, true, 275]);
    }
  });

  it('DoorState cannot tell a closed door from a locked one', () => {
    const names = Object.keys(DoorPublicState).filter((k) => Number.isNaN(Number(k)));
    expect(names.sort()).toEqual(['Broken', 'Open', 'Shut']);
    const accessors = Object.getOwnPropertyNames(DoorState.prototype)
      .filter((k) => k !== 'constructor' && k !== '__init' && k !== 'bb' && k !== 'bb_pos')
      .sort();
    expect(accessors).toEqual(['beingForced', 'door', 'hallId', 'hp', 'state']);
  });

  it('round-trips DoorLog', () => {
    const actions = [DoorLogAction.Open, DoorLogAction.Close, DoorLogAction.Lock, DoorLogAction.Unlock, DoorLogAction.Break, DoorLogAction.Repair];
    const b = new flatbuffers.Builder(256);
    const entries = actions.map((a, i) =>
      DoorLogEntry.createDoorLogEntry(b, (i % 3) as HallDoorId, a, b.createString('Peter'), BigInt(1_800_000_000_000 + i)));
    const l = open(finish(b, Message.DoorLog, DoorLog.createDoorLog(b, 5, DoorLog.createEntriesVector(b, entries))),
      Message.DoorLog, new DoorLog());
    expect([l.hallId(), l.entriesLength()]).toEqual([5, actions.length]);
    actions.forEach((a, i) => {
      const e = l.entries(i)!;
      expect([e.door(), e.action(), e.playerName(), e.timeMs()]).toEqual([i % 3, a, 'Peter', BigInt(1_800_000_000_000 + i)]);
    });
  });

  it('round-trips HallAlarm and HallResult', () => {
    for (const c of [AlarmCause.Door, AlarmCause.Vault]) {
      const b = new flatbuffers.Builder(32);
      const a = open(finish(b, Message.HallAlarm, HallAlarm.createHallAlarm(b, 2, c)), Message.HallAlarm, new HallAlarm());
      expect([a.hallId(), a.cause()]).toEqual([2, c]);
    }
    const codes = Object.values(HallResultCode).filter((v): v is number => typeof v === 'number');
    expect(codes.length).toBeGreaterThan(20);
    for (const code of codes) {
      const b = new flatbuffers.Builder(32);
      expect(open(finish(b, Message.HallResult, HallResult.createHallResult(b, code)), Message.HallResult, new HallResult()).code()).toBe(code);
    }
  });
});
