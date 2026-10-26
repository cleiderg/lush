import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  AttackTarget, CombatHit, Envelope, EquipSlot, GroundItem, GroundItems, Hit, ItemAction, ItemActionKind, Message,
  NpcCatalog, NpcDef, PickUp, ShopBuy, ShopResult, ShopResultCode, ShopSell, Vec2, Welcome,
} from '../protocol';
import {
  decode, encodeAttackTarget, encodeItemAction, encodePickUp, encodeShopBuy, encodeShopSell,
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

describe('decode M6 server messages', () => {
  it('decodes GroundItems with positions in metres', () => {
    const b = new flatbuffers.Builder(128);
    GroundItems.startItemsVector(b, 2);
    GroundItem.createGroundItem(b, 4, 9, 70000, -15, 0);
    GroundItem.createGroundItem(b, 3, 5, 1, 123, -456);
    const v = b.endVector();
    expect(decode(finish(b, Message.GroundItems, GroundItems.createGroundItems(b, v)))).toEqual({
      type: 'groundItems',
      items: [
        { id: 3, itemId: 5, quantity: 1, x: 12.3, z: -45.6 },
        { id: 4, itemId: 9, quantity: 70000, x: -1.5, z: 0 },
      ],
    });
  });

  it('decodes an empty GroundItems', () => {
    const b = new flatbuffers.Builder(32);
    GroundItems.startItemsVector(b, 0);
    const v = GroundItems.createGroundItems(b, b.endVector());
    expect(decode(finish(b, Message.GroundItems, v))).toEqual({ type: 'groundItems', items: [] });
  });

  it('decodes ShopResult', () => {
    for (const code of [ShopResultCode.Ok, ShopResultCode.NotEnoughGold, ShopResultCode.ItemNotSold]) {
      const b = new flatbuffers.Builder(32);
      expect(decode(finish(b, Message.ShopResult, ShopResult.createShopResult(b, code)))).toEqual({ type: 'shopResult', code });
    }
  });

  it('decodes CombatHit', () => {
    const b = new flatbuffers.Builder(64);
    CombatHit.startHitsVector(b, 2);
    Hit.createHit(b, 3, 1, 0, false);
    Hit.createHit(b, 1, 3, 12, true);
    const v = b.endVector();
    expect(decode(finish(b, Message.CombatHit, CombatHit.createCombatHit(b, 99, v)))).toEqual({
      type: 'combatHit',
      tick: 99,
      hits: [
        { attacker: 1, target: 3, damage: 12, died: true },
        { attacker: 3, target: 1, damage: 0, died: false },
      ],
    });
  });

  it('decodes NpcCatalog', () => {
    const b = new flatbuffers.Builder(128);
    const rat = NpcDef.createNpcDef(b, 1, b.createString('Rat'), 2);
    const anteater = NpcDef.createNpcDef(b, 4, b.createString('Anteater'), 15);
    const v = NpcCatalog.createNpcsVector(b, [rat, anteater]);
    expect(decode(finish(b, Message.NpcCatalog, NpcCatalog.createNpcCatalog(b, v)))).toEqual({
      type: 'npcCatalog',
      npcs: [
        { id: 1, name: 'Rat', combatLevel: 2 },
        { id: 4, name: 'Anteater', combatLevel: 15 },
      ],
    });
  });
});

describe('encode M6 client messages', () => {
  it('encodes ItemAction', () => {
    const eat = envelope(encodeItemAction(ItemActionKind.Eat, 7), Message.ItemAction).msg(new ItemAction()) as ItemAction;
    expect([eat.action(), eat.slot(), eat.equipSlot()]).toEqual([ItemActionKind.Eat, 7, EquipSlot.None]);
    const un = envelope(encodeItemAction(ItemActionKind.Unequip, 0, EquipSlot.Shield), Message.ItemAction)
      .msg(new ItemAction()) as ItemAction;
    expect([un.action(), un.slot(), un.equipSlot()]).toEqual([ItemActionKind.Unequip, 0, EquipSlot.Shield]);
  });

  it('encodes ShopBuy', () => {
    const m = envelope(encodeShopBuy(5, 70000), Message.ShopBuy).msg(new ShopBuy()) as ShopBuy;
    expect([m.itemId(), m.quantity()]).toEqual([5, 70000]);
  });

  it('encodes ShopSell', () => {
    const m = envelope(encodeShopSell(27, 3), Message.ShopSell).msg(new ShopSell()) as ShopSell;
    expect([m.slot(), m.quantity()]).toEqual([27, 3]);
  });

  it('encodes PickUp', () => {
    const m = envelope(encodePickUp(65000), Message.PickUp).msg(new PickUp()) as PickUp;
    expect(m.groundItemId()).toBe(65000);
  });

  it('encodes AttackTarget', () => {
    const m = envelope(encodeAttackTarget(42), Message.AttackTarget).msg(new AttackTarget()) as AttackTarget;
    expect(m.targetId()).toBe(42);
  });
});

describe('Session send functions', () => {
  class FakeTransport implements Transport {
    onMessage: ((b: Uint8Array) => void) | null = null;
    onOpen: (() => void) | null = null;
    onClose: (() => void) | null = null;
    sent: Uint8Array[] = [];
    send(b: Uint8Array) { this.sent.push(b); }
    close() {}
  }

  it('sends each request once connected, and nothing before', () => {
    const t = new FakeTransport();
    const s = new Session({ factory: () => t, store: { get: () => '', set: () => {} }, onMessage: () => {} });
    s.start();
    s.sendPickUp(1);
    expect(t.sent.length).toBe(0);

    const b = new flatbuffers.Builder(64);
    const tok = b.createString('tok');
    Welcome.startWelcome(b);
    Welcome.addGuestToken(b, tok);
    Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
    t.onMessage?.(finish(b, Message.Welcome, Welcome.endWelcome(b)));

    s.sendItemAction(ItemActionKind.Equip, 2);
    s.sendShopBuy(5, 2);
    s.sendShopSell(3, 1);
    s.sendPickUp(8);
    s.sendAttackTarget(9);
    const types = t.sent.map((bytes) => Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes)).msgType());
    expect(types).toEqual([Message.ItemAction, Message.ShopBuy, Message.ShopSell, Message.PickUp, Message.AttackTarget]);
    const eq = envelope(t.sent[0], Message.ItemAction).msg(new ItemAction()) as ItemAction;
    expect([eq.action(), eq.slot()]).toEqual([ItemActionKind.Equip, 2]);
    expect((envelope(t.sent[1], Message.ShopBuy).msg(new ShopBuy()) as ShopBuy).itemId()).toBe(5);
    expect((envelope(t.sent[2], Message.ShopSell).msg(new ShopSell()) as ShopSell).slot()).toBe(3);
    expect((envelope(t.sent[3], Message.PickUp).msg(new PickUp()) as PickUp).groundItemId()).toBe(8);
    expect((envelope(t.sent[4], Message.AttackTarget).msg(new AttackTarget()) as AttackTarget).targetId()).toBe(9);
  });
});
