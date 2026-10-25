import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  ActionFlags, Animation, ChatMessage, ChatSend, Emote, EmoteSend, EmptyItem, Entity, Envelope, EquipSlot, Hello, Input,
  InventorySlot, InventoryState, ItemCatalog, ItemDef, ItemKind, Message, Ping, PlayerStats, Pong,
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
    expect(ProtocolVersion.Current).toBe(4);
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

  it('round-trips EmoteSend', () => {
    const b = new flatbuffers.Builder(32);
    const buf = finish(b, Message.EmoteSend, EmoteSend.createEmoteSend(b, Emote.Wave));
    expect(open(buf, Message.EmoteSend, new EmoteSend()).emote()).toBe(Emote.Wave);
  });

  it('round-trips a waving entity', () => {
    const b = new flatbuffers.Builder(64);
    Snapshot.startEntitiesVector(b, 1);
    Entity.createEntity(b, 7, 10, 20, 30, Animation.Wave);
    const ents = b.endVector();
    Snapshot.startSnapshot(b);
    Snapshot.addEntities(b, ents);
    const buf = finish(b, Message.Snapshot, Snapshot.endSnapshot(b));
    expect(open(buf, Message.Snapshot, new Snapshot()).entities(0)!.animation()).toBe(Animation.Wave);
  });

  it('round-trips PlayerStats', () => {
    const b = new flatbuffers.Builder(64);
    const buf = finish(b, Message.PlayerStats, PlayerStats.createPlayerStats(b, 7, 39, 30, 13363, 4000000000));
    const m = open(buf, Message.PlayerStats, new PlayerStats());
    expect([m.hp(), m.maxHp(), m.combatLevel(), m.xp(), m.gold()]).toEqual([7, 39, 30, 13363, 4000000000]);
  });

  it('round-trips InventoryState with empty slots', () => {
    // 28 slots: 3 Boba (id 9) in slot 0, 250 acorns (id 11) in slot 5, rest empty.
    const want: [number, number][] = Array.from({ length: 28 }, () => [EmptyItem.Id, 0]);
    want[0] = [9, 3];
    want[5] = [11, 250];
    const b = new flatbuffers.Builder(256);
    InventoryState.startSlotsVector(b, want.length);
    for (let i = want.length - 1; i >= 0; i--) {
      InventorySlot.createInventorySlot(b, want[i][0], want[i][1]);
    }
    const slots = b.endVector();
    const buf = finish(b, Message.InventoryState,
      InventoryState.createInventoryState(b, slots, 1, EmptyItem.Id, 4, EmptyItem.Id));
    const m = open(buf, Message.InventoryState, new InventoryState());
    expect(m.slotsLength()).toBe(28);
    for (let i = 0; i < want.length; i++) {
      const s = m.slots(i)!;
      expect([s.itemId(), s.quantity()]).toEqual(want[i]);
    }
    expect(m.slots(1)!.itemId()).toBe(EmptyItem.Id);
    expect([m.weapon(), m.head(), m.body(), m.shield()]).toEqual([1, EmptyItem.Id, 4, EmptyItem.Id]);
  });

  it('round-trips ItemCatalog', () => {
    type Def = [number, string, ItemKind, EquipSlot, number, number, number, number, number, number, number, boolean, number];
    const want: Def[] = [
      [1, 'Wooden stick', ItemKind.Weapon, EquipSlot.Weapon, 1, 3, 2, 0, 4, 0, 2, true, 5],
      [8, 'Robot plating', ItemKind.Armor, EquipSlot.Body, 12, -2, 0, 14, 0, 0, 150, false, 0],
      [9, 'Boba', ItemKind.Food, EquipSlot.None, 1, 0, 0, 0, 0, 4, 1, true, 5],
      [11, 'Acorn', ItemKind.Loot, EquipSlot.None, 0, 0, 0, 0, 0, 0, 1, false, 0],
      [7, 'Bike helmet', ItemKind.Armor, EquipSlot.Head, 10, 0, 0, 6, 0, 0, 30, true, 80],
      [5, 'Cardboard shield', ItemKind.Armor, EquipSlot.Shield, 1, 0, 0, 3, 0, 0, 3, true, 10],
    ];
    const b = new flatbuffers.Builder(512);
    const offs = want.map(([id, name, ...rest]) => ItemDef.createItemDef(b, id, b.createString(name), ...rest));
    const buf = finish(b, Message.ItemCatalog, ItemCatalog.createItemCatalog(b, ItemCatalog.createItemsVector(b, offs)));
    const m = open(buf, Message.ItemCatalog, new ItemCatalog());
    expect(m.itemsLength()).toBe(want.length);
    for (let i = 0; i < want.length; i++) {
      const d = m.items(i)!;
      expect([
        d.id(), d.name(), d.kind(), d.equipSlot(), d.levelToEquip(), d.accuracy(), d.strength(), d.defence(),
        d.attackSpeed(), d.heal(), d.shopBuysFor(), d.shopSells(), d.shopSellsFor(),
      ]).toEqual(want[i]);
    }
  });
});
