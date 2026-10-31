import * as flatbuffers from 'flatbuffers';
import {
  AttackTarget, CardTableCatalog, CardTableInfo, CardTableLeave, CardTableReady, CardTableResult, CardTableResultCode, CardTableSit, CardTableState, ChatMessage, ChatSend, CombatHit, Emote, EmoteSend, Entity, EntityHealth, EntityInfo, EntityKind, Envelope,
  EquipSlot, GroundItem, GroundItems, Hello, HealthRatio, Hit, Input, InventorySlot, InventoryState, ItemAction,
  ItemActionKind, ItemCatalog, ItemDef, ItemKind, Message, NpcCatalog, NpcDef, PickUp, Ping, PlayerStats, Pong,
  ProtocolVersion, Reload, RestartCountdown, ShedCard, ShedColour, ShedDraw, ShedHand, ShedPlay, ShedState, ShedSymbol, ShopBuy, ShopResult, ShopResultCode, ShopSell, Snapshot, TradeAccept,
  TradeCancel, TradeItem, TradeOffer, TradeRemove, TradeRequest, TradeRequestNotice, TradeResult, TradeResultCode,
  TradeSetGold, TradeState, Vec2, Welcome,
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

/** One offered item in a trade. */
export interface TradeItemState {
  itemId: number;
  quantity: number;
}

/** The whole trade window; "my" is this player's side, "partner" the other's. */
export interface TradeView {
  partnerId: number;
  /** 1 (offer) or 2 (final confirmation). */
  screen: number;
  myItems: TradeItemState[];
  myGold: number;
  partnerItems: TradeItemState[];
  partnerGold: number;
  myAccepted: boolean;
  partnerAccepted: boolean;
}

/** Public seat and turn state of one card table. */
export interface CardTableView {
  tableId: number;
  /** Entity id in each seat, 0 for an empty seat. */
  seats: number[];
  inProgress: boolean;
  /** Seat index whose turn it is, or null when no game is in progress. */
  turnSeat: number | null;
  reversed: boolean;
  /** World ticks until the turn times out. */
  turnTicksLeft: number;
  /** Seat index of the last game's winner, or null when there is none. */
  winnerSeat: number | null;
  /** Per seat, as in seats: that player pressed Ready. */
  ready: boolean[];
}

/** One table in the CardTableCatalog. */
export interface CardTableInfoState {
  id: number;
  /** Number of seats at the table. */
  seats: number;
  /** Gold each seated player puts into the pot when a game starts. */
  stake: number;
}

/** One Shed card; Wild and WildDrawFour have colour None. */
export interface ShedCardValue {
  colour: ShedColour;
  symbol: ShedSymbol;
}

/** Public Shed state of one table; turn order and timer are in CardTableView. */
export interface ShedPublicState {
  tableId: number;
  topCard: ShedCardValue;
  /** The colour to match (the chosen colour after a Wild). */
  colour: ShedColour;
  /** Cards in each seat's hand, as in CardTableView.seats. */
  handCounts: number[];
  /** Cards left in the draw pile. */
  drawPile: number;
}

/** The local player's own Shed hand. */
export interface ShedHandState {
  tableId: number;
  cards: ShedCardValue[];
}

export type ServerMessage =
  | { type: 'welcome'; playerId: number; guestToken: string; spawn: { x: number; z: number }; tickRate: number; walkSpeed?: number; runSpeed?: number; serverTimeMs?: number }
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
  | { type: 'npcCatalog'; npcs: NpcInfo[] }
  | { type: 'tradeRequestNotice'; fromId: number }
  | ({ type: 'tradeState' } & TradeView)
  | { type: 'tradeResult'; code: TradeResultCode }
  | ({ type: 'cardTableState' } & CardTableView)
  | { type: 'cardTableResult'; code: CardTableResultCode }
  | { type: 'cardTableCatalog'; tables: CardTableInfoState[] }
  | ({ type: 'shedState' } & ShedPublicState)
  | ({ type: 'shedHand' } & ShedHandState);

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

/** Ask targetId to trade, or accept their pending request. */
export function encodeTradeRequest(targetId: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.TradeRequest, TradeRequest.createTradeRequest(b, targetId));
}

/** Add quantity of the item in inventory slot `slot` to your offer. */
export function encodeTradeOffer(slot: number, quantity: number): Uint8Array {
  const b = new flatbuffers.Builder(32);
  return wrap(b, Message.TradeOffer, TradeOffer.createTradeOffer(b, slot, quantity));
}

/** Take the item at `index` of your offer back. */
export function encodeTradeRemove(index: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.TradeRemove, TradeRemove.createTradeRemove(b, index));
}

export function encodeTradeSetGold(gold: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.TradeSetGold, TradeSetGold.createTradeSetGold(b, gold));
}

/** Accept the screen (1 or 2) you are looking at. */
export function encodeTradeAccept(screen: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.TradeAccept, TradeAccept.createTradeAccept(b, screen));
}

export function encodeTradeCancel(): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.TradeCancel, TradeCancel.createTradeCancel(b));
}

/** Sit in the first free seat of table `tableId`. */
export function encodeCardTableSit(tableId: number): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.CardTableSit, CardTableSit.createCardTableSit(b, tableId));
}

export function encodeCardTableLeave(): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.CardTableLeave, CardTableLeave.createCardTableLeave(b));
}

/** Ready to start a game at the table you are seated at. */
export function encodeCardTableReady(): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.CardTableReady, CardTableReady.createCardTableReady(b));
}

/** Play `card` from your hand; chosenColour is the colour to match next after a Wild or WildDrawFour. */
export function encodeShedPlay(card: ShedCardValue, chosenColour: ShedColour = ShedColour.None): Uint8Array {
  const b = new flatbuffers.Builder(32);
  ShedPlay.startShedPlay(b);
  ShedPlay.addCard(b, ShedCard.createShedCard(b, card.colour, card.symbol));
  ShedPlay.addChosenColour(b, chosenColour);
  return wrap(b, Message.ShedPlay, ShedPlay.endShedPlay(b));
}

/** Draw one card; the turn passes. */
export function encodeShedDraw(): Uint8Array {
  const b = new flatbuffers.Builder(16);
  return wrap(b, Message.ShedDraw, ShedDraw.createShedDraw(b));
}

function tradeItems(len: number, at: (i: number, obj: TradeItem) => TradeItem | null): TradeItemState[] {
  const out: TradeItemState[] = [];
  const t = new TradeItem();
  for (let i = 0; i < len; i++) {
    at(i, t);
    out.push({ itemId: t.itemId(), quantity: t.quantity() });
  }
  return out;
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
        walkSpeed: m.walkSpeed(),
        runSpeed: m.runSpeed(),
        // UTC ms; 0 (an older server) means unknown, and the client keeps its own clock.
        serverTimeMs: Number(m.serverTimeMs()) || undefined,
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
    case Message.TradeRequestNotice:
      return { type: 'tradeRequestNotice', fromId: (env.msg(new TradeRequestNotice()) as TradeRequestNotice).fromId() };
    case Message.TradeState: {
      const m = env.msg(new TradeState()) as TradeState;
      return {
        type: 'tradeState',
        partnerId: m.partnerId(),
        screen: m.screen(),
        myItems: tradeItems(m.myItemsLength(), (i, o) => m.myItems(i, o)),
        myGold: m.myGold(),
        partnerItems: tradeItems(m.partnerItemsLength(), (i, o) => m.partnerItems(i, o)),
        partnerGold: m.partnerGold(),
        myAccepted: m.myAccepted(),
        partnerAccepted: m.partnerAccepted(),
      };
    }
    case Message.TradeResult:
      return { type: 'tradeResult', code: (env.msg(new TradeResult()) as TradeResult).code() };
    case Message.CardTableState: {
      const m = env.msg(new CardTableState()) as CardTableState;
      const seats: number[] = [];
      for (let i = 0; i < m.seatsLength(); i++) seats.push(m.seats(i) ?? 0);
      return {
        type: 'cardTableState',
        tableId: m.tableId(),
        seats,
        inProgress: m.inProgress(),
        turnSeat: m.inProgress() ? m.turnSeat() : null,
        reversed: m.reversed(),
        turnTicksLeft: m.turnTicksLeft(),
        winnerSeat: m.hasWinner() ? m.winnerSeat() : null,
        // A missing or short ready list means not ready.
        ready: seats.map((_, i) => i < m.readyLength() && !!m.ready(i)),
      };
    }
    case Message.CardTableResult:
      return { type: 'cardTableResult', code: (env.msg(new CardTableResult()) as CardTableResult).code() };
    case Message.CardTableCatalog: {
      const m = env.msg(new CardTableCatalog()) as CardTableCatalog;
      const tables: CardTableInfoState[] = [];
      const t = new CardTableInfo();
      for (let i = 0; i < m.tablesLength(); i++) {
        m.tables(i, t);
        tables.push({ id: t.id(), seats: t.seats(), stake: t.stake() });
      }
      return { type: 'cardTableCatalog', tables };
    }
    case Message.ShedState: {
      const m = env.msg(new ShedState()) as ShedState;
      const top = m.topCard();
      const handCounts: number[] = [];
      for (let i = 0; i < m.handCountsLength(); i++) handCounts.push(m.handCounts(i) ?? 0);
      return {
        type: 'shedState',
        tableId: m.tableId(),
        topCard: { colour: top?.colour() ?? ShedColour.None, symbol: top?.symbol() ?? ShedSymbol.Zero },
        colour: m.colour(),
        handCounts,
        drawPile: m.drawPile(),
      };
    }
    case Message.ShedHand: {
      const m = env.msg(new ShedHand()) as ShedHand;
      const cards: ShedCardValue[] = [];
      const c = new ShedCard();
      for (let i = 0; i < m.cardsLength(); i++) {
        m.cards(i, c);
        cards.push({ colour: c.colour(), symbol: c.symbol() });
      }
      return { type: 'shedHand', tableId: m.tableId(), cards };
    }
    default:
      return null;
  }
}
