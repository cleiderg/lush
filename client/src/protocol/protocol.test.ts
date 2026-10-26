import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  ActionFlags, Animation, AttackTarget, CombatHit, Hit, NpcCatalog, NpcDef, ChatMessage, ChatSend, Emote, EmoteSend, EmptyItem, Entity, EntityHealth, EntityInfo,
  EntityKind, Envelope, EquipSlot, GroundItem, GroundItems, Hello, Input, ItemAction, ItemActionKind, PickUp,
  ShopBuy, ShopResult, ShopResultCode, ShopSell,
  InventorySlot, InventoryState, HealthRatio, ItemCatalog, ItemDef, ItemKind, Message, Ping, PlayerStats, Pong,
  ProtocolVersion, Reload, RestartCountdown, Snapshot, Vec2, Welcome,
  TradeAccept, TradeCancel, TradeItem, TradeOffer, TradeRemove, TradeRequest, TradeRequestNotice, TradeResult,
  TradeResultCode, TradeSetGold, TradeState,
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
    expect(ProtocolVersion.Current).toBe(8);
  });

  it('keeps a 101-entity snapshot under 1200 bytes', () => {
    expect(buildSnapshot(101).length).toBeLessThan(1200);
  });

  it('keeps a 100-entity snapshot under 1200 bytes with 8-byte entities', () => {
    expect(buildSnapshot(100).length).toBeLessThan(1200);
    expect(Entity.sizeOf()).toBe(8);
    expect(buildSnapshot(51).length - buildSnapshot(50).length).toBe(8);
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

  it('round-trips EntityInfo', () => {
    const want: [number, EntityKind, number, number, number, number, number, number][] = [
      [42, EntityKind.Player, 0, 30, 1, 7, 8, 5],
      [300, EntityKind.Npc, 6, 20, EmptyItem.Id, EmptyItem.Id, EmptyItem.Id, EmptyItem.Id],
      [65535, EntityKind.Npc, 65535, 255, 65535, 65535, 65535, 65535],
    ];
    for (const w of want) {
      const b = new flatbuffers.Builder(64);
      const buf = finish(b, Message.EntityInfo, EntityInfo.createEntityInfo(b, ...w));
      const m = open(buf, Message.EntityInfo, new EntityInfo());
      expect([
        m.id(), m.kind(), m.npcType(), m.combatLevel(), m.weapon(), m.head(), m.body(), m.shield(),
      ]).toEqual(w);
    }
  });

  it('round-trips EntityHealth', () => {
    const want: [number, number][] = [[1, 255], [300, 128], [65535, 0]];
    const b = new flatbuffers.Builder(64);
    EntityHealth.startEntriesVector(b, want.length);
    for (let i = want.length - 1; i >= 0; i--) {
      HealthRatio.createHealthRatio(b, want[i][0], want[i][1]);
    }
    const buf = finish(b, Message.EntityHealth, EntityHealth.createEntityHealth(b, b.endVector()));
    const m = open(buf, Message.EntityHealth, new EntityHealth());
    expect(m.entriesLength()).toBe(want.length);
    for (let i = 0; i < want.length; i++) {
      const h = m.entries(i)!;
      expect([h.id(), h.ratio()]).toEqual(want[i]);
    }
  });

  it('round-trips ItemAction', () => {
    const want: [ItemActionKind, number, EquipSlot][] = [
      [ItemActionKind.Equip, 0, EquipSlot.None],
      [ItemActionKind.Eat, 27, EquipSlot.None],
      [ItemActionKind.Unequip, 0, EquipSlot.Shield],
      [ItemActionKind.Unequip, 255, EquipSlot.Weapon],
    ];
    for (const w of want) {
      const b = new flatbuffers.Builder(64);
      const buf = finish(b, Message.ItemAction, ItemAction.createItemAction(b, ...w));
      const m = open(buf, Message.ItemAction, new ItemAction());
      expect([m.action(), m.slot(), m.equipSlot()]).toEqual(w);
    }
  });

  function buildGroundItems(want: [number, number, number, number, number][]): Uint8Array {
    const b = new flatbuffers.Builder(64);
    GroundItems.startItemsVector(b, want.length);
    for (let i = want.length - 1; i >= 0; i--) {
      GroundItem.createGroundItem(b, ...want[i]);
    }
    return finish(b, Message.GroundItems, GroundItems.createGroundItems(b, b.endVector()));
  }

  it('round-trips GroundItems', () => {
    const want: [number, number, number, number, number][] = [
      [1, 11, 1, 0, 0],
      [300, 13, 4294967295, -32768, 32767],
      [65535, 65535, 25, 1234, -987],
    ];
    const m = open(buildGroundItems(want), Message.GroundItems, new GroundItems());
    expect(m.itemsLength()).toBe(want.length);
    for (let i = 0; i < want.length; i++) {
      const g = m.items(i)!;
      expect([g.id(), g.itemId(), g.quantity(), g.x(), g.z()]).toEqual(want[i]);
    }
  });

  it('packs GroundItem to 12 bytes', () => {
    expect(GroundItem.sizeOf()).toBe(12);
    const items = (n: number) => Array.from({ length: n }, (_, i): [number, number, number, number, number] => [i, 1, 1, 0, 0]);
    expect(buildGroundItems(items(51)).length - buildGroundItems(items(50)).length).toBe(12);
  });

  it('round-trips PickUp', () => {
    for (const id of [0, 1, 65535]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.PickUp, PickUp.createPickUp(b, id));
      expect(open(buf, Message.PickUp, new PickUp()).groundItemId()).toBe(id);
    }
  });

  it('round-trips ShopBuy', () => {
    for (const w of [[1, 1], [10, 28], [65535, 4294967295]] as [number, number][]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.ShopBuy, ShopBuy.createShopBuy(b, ...w));
      const m = open(buf, Message.ShopBuy, new ShopBuy());
      expect([m.itemId(), m.quantity()]).toEqual(w);
    }
  });

  it('round-trips ShopSell', () => {
    for (const w of [[0, 1], [27, 500], [255, 4294967295]] as [number, number][]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.ShopSell, ShopSell.createShopSell(b, ...w));
      const m = open(buf, Message.ShopSell, new ShopSell());
      expect([m.slot(), m.quantity()]).toEqual(w);
    }
  });

  it('round-trips ShopResult', () => {
    for (const code of [
      ShopResultCode.Ok, ShopResultCode.TooFar, ShopResultCode.NotEnoughGold,
      ShopResultCode.InventoryFull, ShopResultCode.ItemNotSold,
    ]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.ShopResult, ShopResult.createShopResult(b, code));
      expect(open(buf, Message.ShopResult, new ShopResult()).code()).toBe(code);
    }
  });

  it('round-trips AttackTarget', () => {
    for (const id of [0, 300, 65535]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.AttackTarget, AttackTarget.createAttackTarget(b, id));
      expect(open(buf, Message.AttackTarget, new AttackTarget()).targetId()).toBe(id);
    }
  });

  it('round-trips CombatHit', () => {
    const want: [number, number, number, boolean][] = [
      [1, 300, 3, false], [300, 1, 0, false], [2, 300, 255, true], [65535, 65535, 0, true],
    ];
    const b = new flatbuffers.Builder(64);
    CombatHit.startHitsVector(b, want.length);
    for (let i = want.length - 1; i >= 0; i--) {
      Hit.createHit(b, ...want[i]);
    }
    const buf = finish(b, Message.CombatHit, CombatHit.createCombatHit(b, 123456, b.endVector()));
    const m = open(buf, Message.CombatHit, new CombatHit());
    expect(m.tick()).toBe(123456);
    expect(m.hitsLength()).toBe(want.length);
    expect(Hit.sizeOf()).toBe(6);
    for (let i = 0; i < want.length; i++) {
      const h = m.hits(i)!;
      expect([h.attacker(), h.target(), h.damage(), h.died()]).toEqual(want[i]);
    }
  });

  it('round-trips NpcCatalog', () => {
    const want: [number, string, number][] = [[1, 'Squirrel', 1], [6, 'Feral Anteater', 20], [65535, '', 255]];
    const b = new flatbuffers.Builder(128);
    const offs = want.map(([id, name, level]) => NpcDef.createNpcDef(b, id, b.createString(name), level));
    const buf = finish(b, Message.NpcCatalog, NpcCatalog.createNpcCatalog(b, NpcCatalog.createNpcsVector(b, offs)));
    const m = open(buf, Message.NpcCatalog, new NpcCatalog());
    expect(m.npcsLength()).toBe(want.length);
    for (let i = 0; i < want.length; i++) {
      const d = m.npcs(i)!;
      expect([d.id(), d.name(), d.combatLevel()]).toEqual(want[i]);
    }
  });

  it('round-trips TradeRequest', () => {
    for (const id of [0, 300, 65535]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.TradeRequest, TradeRequest.createTradeRequest(b, id));
      expect(open(buf, Message.TradeRequest, new TradeRequest()).targetId()).toBe(id);
    }
  });

  it('round-trips TradeOffer', () => {
    for (const [slot, qty] of [[0, 1], [27, 5000], [255, 4294967295]]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.TradeOffer, TradeOffer.createTradeOffer(b, slot, qty));
      const m = open(buf, Message.TradeOffer, new TradeOffer());
      expect([m.slot(), m.quantity()]).toEqual([slot, qty]);
    }
  });

  it('round-trips TradeRemove', () => {
    for (const idx of [0, 7, 255]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.TradeRemove, TradeRemove.createTradeRemove(b, idx));
      expect(open(buf, Message.TradeRemove, new TradeRemove()).index()).toBe(idx);
    }
  });

  it('round-trips TradeSetGold', () => {
    for (const gold of [0, 100, 4294967295]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.TradeSetGold, TradeSetGold.createTradeSetGold(b, gold));
      expect(open(buf, Message.TradeSetGold, new TradeSetGold()).gold()).toBe(gold);
    }
  });

  it('round-trips TradeAccept', () => {
    for (const screen of [1, 2]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.TradeAccept, TradeAccept.createTradeAccept(b, screen));
      expect(open(buf, Message.TradeAccept, new TradeAccept()).screen()).toBe(screen);
    }
  });

  it('round-trips TradeCancel', () => {
    const b = new flatbuffers.Builder(32);
    const buf = finish(b, Message.TradeCancel, TradeCancel.createTradeCancel(b));
    expect(open(buf, Message.TradeCancel, new TradeCancel())).toBeInstanceOf(TradeCancel);
  });

  it('round-trips TradeRequestNotice', () => {
    for (const id of [0, 300, 65535]) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.TradeRequestNotice, TradeRequestNotice.createTradeRequestNotice(b, id));
      expect(open(buf, Message.TradeRequestNotice, new TradeRequestNotice()).fromId()).toBe(id);
    }
  });

  it('round-trips TradeState', () => {
    type Side = [number, number][];
    const items = (b: flatbuffers.Builder, side: Side, start: (b: flatbuffers.Builder, n: number) => void) => {
      start(b, side.length);
      for (let i = side.length - 1; i >= 0; i--) {
        TradeItem.createTradeItem(b, ...side[i]);
      }
      return b.endVector();
    };
    const cases: [number, number, Side, number, Side, number, boolean, boolean][] = [
      [300, 1, [[1, 1], [12, 250], [65535, 4294967295]], 100, [[7, 3]], 0, true, false],
      [65535, 2, [], 4294967295, [[2, 1], [3, 2]], 55, false, true],
      [1, 2, [[9, 9]], 0, [], 4294967295, true, true],
    ];
    expect(TradeItem.sizeOf()).toBe(8);
    for (const [partner, screen, mine, myGold, theirs, partnerGold, myAcc, theirAcc] of cases) {
      const b = new flatbuffers.Builder(256);
      const mv = items(b, mine, TradeState.startMyItemsVector);
      const pv = items(b, theirs, TradeState.startPartnerItemsVector);
      const buf = finish(b, Message.TradeState,
        TradeState.createTradeState(b, partner, screen, mv, myGold, pv, partnerGold, myAcc, theirAcc));
      const m = open(buf, Message.TradeState, new TradeState());
      expect([m.partnerId(), m.screen(), m.myGold(), m.partnerGold(), m.myAccepted(), m.partnerAccepted()])
        .toEqual([partner, screen, myGold, partnerGold, myAcc, theirAcc]);
      expect(m.myItemsLength()).toBe(mine.length);
      mine.forEach((w, i) => {
        const it = m.myItems(i)!;
        expect([it.itemId(), it.quantity()]).toEqual(w);
      });
      expect(m.partnerItemsLength()).toBe(theirs.length);
      theirs.forEach((w, i) => {
        const it = m.partnerItems(i)!;
        expect([it.itemId(), it.quantity()]).toEqual(w);
      });
    }
  });

  it('round-trips TradeResult', () => {
    const codes = [
      TradeResultCode.Completed, TradeResultCode.Cancelled, TradeResultCode.TooFar, TradeResultCode.InPvpZone,
      TradeResultCode.PartnerBusy, TradeResultCode.OfferNotHeld, TradeResultCode.InventoryFull,
    ];
    for (const code of codes) {
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.TradeResult, TradeResult.createTradeResult(b, code));
      expect(open(buf, Message.TradeResult, new TradeResult()).code()).toBe(code);
    }
  });
});
