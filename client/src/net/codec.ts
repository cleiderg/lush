import * as flatbuffers from 'flatbuffers';
import {
  AttackTarget, ChatMessage, ChatSend, CombatHit, Emote, EmoteSend, Entity, EntityHealth, EntityInfo, EntityKind, Envelope,
  EquipSlot, GroundItem, GroundItems, Hello, HealthRatio, Hit, Input, InventorySlot, InventoryState, ItemAction,
  ItemActionKind, ItemCatalog, ItemDef, ItemKind, Message, NpcCatalog, NpcDef, PickUp, Ping, PlayerStats, Pong,
  ProtocolVersion, Reload, RestartCountdown, ShopBuy, ShopResult, ShopResultCode, ShopSell, Snapshot, Vec2, Welcome,
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

export interface PlayerStatsState {
  hp: number;
  maxHp: number;
  combatLevel: number;
  xp: number;
  gold: number;
}

/** Item ids worn in each equipment slot; 0 (EmptyItem.Id) when empty. */
export interface Equipment {
  weapon: number;
  head: number;
  body: number;
  shield: number;
}

/** One inventory slot; an empty slot is itemId 0, quantity 0. */
export interface SlotState {
  itemId: number;
  quantity: number;
}

export interface InventoryView {
  slots: SlotState[];
  equipment: Equipment;
}

export interface ItemInfo {
  id: number;
  name: string;
  kind: ItemKind;
  equipSlot: EquipSlot;
  levelToEquip: number;
  accuracy: number;
  strength: number;
  defence: number;
  attackSpeed: number;
  heal: number;
  shopBuysFor: number;
  shopSells: boolean;
  shopSellsFor: number;
}

export interface EntityInfoState {
  id: number;
  kind: EntityKind;
  npcType: number;
  combatLevel: number;
  equipment: Equipment;
}

/** Health bar entry; ratio is hp / max_hp in 0..1. */
export interface HealthEntry {
  id: number;
  ratio: number;
}

/** One item lying on the ground; x and z in metres from the map origin. */
export interface GroundItemState {
  id: number;
  itemId: number;
  quantity: number;
  x: number;
  z: number;
}

/** One hit of a combat tick; damage 0 is a miss. */
export interface HitEvent {
  attacker: number;
  target: number;
  damage: number;
  died: boolean;
}

export interface NpcInfo {
  id: number;
  name: string;
  combatLevel: number;
}

export type ServerMessage =
  | { type: 'welcome'; playerId: number; guestToken: string; spawn: { x: number; z: number }; tickRate: number }
  | { type: 'snapshot'; tick: number; lastInputSeq: number; entities: EntityState[] }
  | { type: 'chat'; speakerId: number; text: string }
  | { type: 'pong'; token: number }
  | { type: 'reload'; reason: string }
  | { type: 'restartCountdown'; seconds: number }
  | ({ type: 'playerStats' } & PlayerStatsState)
  | ({ type: 'inventory' } & InventoryView)
  | { type: 'itemCatalog'; items: ItemInfo[] }
  | ({ type: 'entityInfo' } & EntityInfoState)
  | { type: 'entityHealth'; entries: HealthEntry[] }
  | { type: 'groundItems'; items: GroundItemState[] }
  | { type: 'shopResult'; code: ShopResultCode }
  | { type: 'combatHit'; tick: number; hits: HitEvent[] }
  | { type: 'npcCatalog'; npcs: NpcInfo[] };

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

export function encodeEmote(emote: Emote): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.EmoteSend, EmoteSend.createEmoteSend(b, emote));
}

export function encodePing(token: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.Ping, Ping.createPing(b, token));
}

/** Equip or Eat use slot (inventory index); Unequip uses equipSlot. */
export function encodeItemAction(action: ItemActionKind, slot: number, equipSlot: EquipSlot = EquipSlot.None): Uint8Array {
  const b = new flatbuffers.Builder(32);
  return wrap(b, Message.ItemAction, ItemAction.createItemAction(b, action, slot, equipSlot));
}

export function encodeShopBuy(itemId: number, quantity: number): Uint8Array {
  const b = new flatbuffers.Builder(32);
  return wrap(b, Message.ShopBuy, ShopBuy.createShopBuy(b, itemId, quantity));
}

export function encodeShopSell(slot: number, quantity: number): Uint8Array {
  const b = new flatbuffers.Builder(32);
  return wrap(b, Message.ShopSell, ShopSell.createShopSell(b, slot, quantity));
}

export function encodePickUp(groundItemId: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.PickUp, PickUp.createPickUp(b, groundItemId));
}

export function encodeAttackTarget(targetId: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.AttackTarget, AttackTarget.createAttackTarget(b, targetId));
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
    case Message.RestartCountdown:
      return { type: 'restartCountdown', seconds: (env.msg(new RestartCountdown()) as RestartCountdown).secondsRemaining() };
    case Message.PlayerStats: {
      const m = env.msg(new PlayerStats()) as PlayerStats;
      return { type: 'playerStats', hp: m.hp(), maxHp: m.maxHp(), combatLevel: m.combatLevel(), xp: m.xp(), gold: m.gold() };
    }
    case Message.InventoryState: {
      const m = env.msg(new InventoryState()) as InventoryState;
      const slots: SlotState[] = [];
      const s = new InventorySlot();
      for (let i = 0; i < m.slotsLength(); i++) {
        m.slots(i, s);
        slots.push({ itemId: s.itemId(), quantity: s.quantity() });
      }
      return {
        type: 'inventory',
        slots,
        equipment: { weapon: m.weapon(), head: m.head(), body: m.body(), shield: m.shield() },
      };
    }
    case Message.ItemCatalog: {
      const m = env.msg(new ItemCatalog()) as ItemCatalog;
      const items: ItemInfo[] = [];
      const d = new ItemDef();
      for (let i = 0; i < m.itemsLength(); i++) {
        m.items(i, d);
        items.push({
          id: d.id(),
          name: d.name() ?? '',
          kind: d.kind(),
          equipSlot: d.equipSlot(),
          levelToEquip: d.levelToEquip(),
          accuracy: d.accuracy(),
          strength: d.strength(),
          defence: d.defence(),
          attackSpeed: d.attackSpeed(),
          heal: d.heal(),
          shopBuysFor: d.shopBuysFor(),
          shopSells: d.shopSells(),
          shopSellsFor: d.shopSellsFor(),
        });
      }
      return { type: 'itemCatalog', items };
    }
    case Message.EntityInfo: {
      const m = env.msg(new EntityInfo()) as EntityInfo;
      return {
        type: 'entityInfo',
        id: m.id(),
        kind: m.kind(),
        npcType: m.npcType(),
        combatLevel: m.combatLevel(),
        equipment: { weapon: m.weapon(), head: m.head(), body: m.body(), shield: m.shield() },
      };
    }
    case Message.EntityHealth: {
      const m = env.msg(new EntityHealth()) as EntityHealth;
      const entries: HealthEntry[] = [];
      const h = new HealthRatio();
      for (let i = 0; i < m.entriesLength(); i++) {
        m.entries(i, h);
        entries.push({ id: h.id(), ratio: h.ratio() / 255 });
      }
      return { type: 'entityHealth', entries };
    }
    case Message.GroundItems: {
      const m = env.msg(new GroundItems()) as GroundItems;
      const items: GroundItemState[] = [];
      const g = new GroundItem();
      for (let i = 0; i < m.itemsLength(); i++) {
        m.items(i, g);
        items.push({ id: g.id(), itemId: g.itemId(), quantity: g.quantity(), x: g.x() / 10, z: g.z() / 10 });
      }
      return { type: 'groundItems', items };
    }
    case Message.ShopResult:
      return { type: 'shopResult', code: (env.msg(new ShopResult()) as ShopResult).code() };
    case Message.CombatHit: {
      const m = env.msg(new CombatHit()) as CombatHit;
      const hits: HitEvent[] = [];
      const h = new Hit();
      for (let i = 0; i < m.hitsLength(); i++) {
        m.hits(i, h);
        hits.push({ attacker: h.attacker(), target: h.target(), damage: h.damage(), died: h.died() });
      }
      return { type: 'combatHit', tick: m.tick(), hits };
    }
    case Message.NpcCatalog: {
      const m = env.msg(new NpcCatalog()) as NpcCatalog;
      const npcs: NpcInfo[] = [];
      const d = new NpcDef();
      for (let i = 0; i < m.npcsLength(); i++) {
        m.npcs(i, d);
        npcs.push({ id: d.id(), name: d.name() ?? '', combatLevel: d.combatLevel() });
      }
      return { type: 'npcCatalog', npcs };
    }
    default:
      return null;
  }
}
