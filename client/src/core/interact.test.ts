// @vitest-environment happy-dom
import * as flatbuffers from 'flatbuffers';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AttackTarget, EntityKind, Envelope, Message, PickUp, TradeRequest, Vec2, Welcome } from '../protocol';
import { Session } from '../net/session';
import type { Transport } from '../net/transport';
import { CARD_TABLE_POSITIONS } from '../state/cardTables';
import { PVP_ZONES } from '../state/interact';
import { GameState } from '../state/world';
import { CardTablePanel } from '../ui/cardTablePanel';
import { ShopPanel, SHOP_STALL } from '../ui/shopPanel';
import { interact } from './interact';

class FakeTransport implements Transport {
  onMessage: ((b: Uint8Array) => void) | null = null;
  onOpen: (() => void) | null = null;
  onClose: (() => void) | null = null;
  sent: Uint8Array[] = [];
  send(b: Uint8Array) { this.sent.push(b); }
  close() {}
}

const NOW = 10_000;
const NO_EQUIPMENT = { weapon: 0, head: 0, body: 0, shield: 0 };
let shop: ShopPanel | null = null;
let cardTable: CardTablePanel | null = null;

/** A game whose local player (id 1) stands at pos, with the given other entities around. */
function setup(
  pos: { x: number; z: number },
  others: { id: number; x: number; z: number; kind: EntityKind }[],
  ground: { id: number; x: number; z: number }[] = [],
) {
  const game = new GameState();
  game.handle({ type: 'welcome', playerId: 1, guestToken: 't', spawn: pos, tickRate: 20 }, 0);
  const entities = [{ id: 1, x: pos.x, z: pos.z, heading: 0, animation: 0 }, ...others.map((o) => ({ ...o, heading: 0, animation: 0 }))];
  game.handle({ type: 'snapshot', tick: 1, lastInputSeq: 0, entities }, 0);
  game.handle({ type: 'groundItems', items: ground.map((g) => ({ ...g, itemId: 1, quantity: 1 })) }, 0);
  for (const o of others) game.handle({ type: 'entityInfo', id: o.id, kind: o.kind, npcType: 1, combatLevel: 3, equipment: NO_EQUIPMENT }, 0);

  const t = new FakeTransport();
  const session = new Session({ factory: () => t, store: { get: () => '', set: () => {} }, onMessage: () => {} });
  session.start();
  const b = new flatbuffers.Builder(64);
  const tok = b.createString('tok');
  Welcome.startWelcome(b);
  Welcome.addGuestToken(b, tok);
  Welcome.addSpawn(b, Vec2.createVec2(b, 0, 0));
  b.finish(Envelope.createEnvelope(b, Message.Welcome, Welcome.endWelcome(b)));
  t.onMessage?.(b.asUint8Array().slice());
  t.sent = [];

  const send = vi.fn<(bytes: Uint8Array) => void>();
  shop = new ShopPanel({ parent: document.body, items: game.items, send, position: () => game.prediction.pos });
  cardTable = new CardTablePanel({
    parent: document.body,
    tables: game.cardTables,
    session,
    position: () => game.prediction.pos,
    onOpen: () => shop?.close(),
  });
  const attack = vi.spyOn(session, 'sendAttackTarget');
  const trade = vi.spyOn(session, 'sendTradeRequest');
  const pickUp = vi.spyOn(session, 'sendPickUp');
  const press = () =>
    interact(game, NOW, SHOP_STALL, {
      openShop: () => shop?.tryOpen(),
      openCardTable: (id) => cardTable?.open(id),
      sendPickUp: (id) => session.sendPickUp(id),
      sendAttackTarget: (id) => session.sendAttackTarget(id),
      sendTradeRequest: (id) => session.sendTradeRequest(id),
    });
  return { game, t, attack, trade, pickUp, press, shop, cardTable };
}

function envelope(bytes: Uint8Array): Envelope {
  return Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
}

// Well away from the stall and outside the PvP zone.
const OPEN = { x: SHOP_STALL.x + 50.5, z: SHOP_STALL.z + 0.5 };
// Inside the PvP zone from areas.json.
const PVP = { x: 300.5, z: -349.5 };

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  shop?.dispose();
  shop = null;
  cardTable?.dispose();
  cardTable = null;
});

describe('Interact button', () => {
  it('opens the shop panel near the stall, as before, and sends nothing (AC2)', () => {
    const s = setup({ ...SHOP_STALL }, [{ id: 5, x: SHOP_STALL.x + 1, z: SHOP_STALL.z, kind: EntityKind.Npc }]);
    expect(s.shop.isOpen).toBe(false);
    expect(s.press()).toEqual({ type: 'shop' });
    expect(s.shop.isOpen).toBe(true);
    expect(s.t.sent).toEqual([]);
  });

  it('attacks the chosen NPC with sendAttackTarget (AC3)', () => {
    const s = setup(OPEN, [
      { id: 5, x: OPEN.x + 1, z: OPEN.z, kind: EntityKind.Npc },
      { id: 6, x: OPEN.x + 1, z: OPEN.z, kind: EntityKind.Player },
    ]);
    s.press();
    expect(s.attack).toHaveBeenCalledWith(5);
    expect(s.trade).not.toHaveBeenCalled();
    expect(s.t.sent.length).toBe(1);
    expect(envelope(s.t.sent[0]).msgType()).toBe(Message.AttackTarget);
    expect((envelope(s.t.sent[0]).msg(new AttackTarget()) as AttackTarget).targetId()).toBe(5);
  });

  it('sends TradeRequest to the chosen player outside the PvP zone (AC3)', () => {
    const s = setup(OPEN, [{ id: 6, x: OPEN.x + 2, z: OPEN.z, kind: EntityKind.Player }]);
    s.press();
    expect(s.trade).toHaveBeenCalledWith(6);
    expect(s.attack).not.toHaveBeenCalled();
    expect(s.t.sent.length).toBe(1);
    expect(envelope(s.t.sent[0]).msgType()).toBe(Message.TradeRequest);
    expect((envelope(s.t.sent[0]).msg(new TradeRequest()) as TradeRequest).targetId()).toBe(6);
  });

  it('sends AttackTarget to the chosen player inside the PvP zone (AC3)', () => {
    expect(PVP_ZONES.length).toBeGreaterThan(0);
    const s = setup(PVP, [{ id: 6, x: PVP.x + 2, z: PVP.z, kind: EntityKind.Player }]);
    s.press();
    expect(s.attack).toHaveBeenCalledWith(6);
    expect(s.trade).not.toHaveBeenCalled();
    expect(s.t.sent.length).toBe(1);
    expect((envelope(s.t.sent[0]).msg(new AttackTarget()) as AttackTarget).targetId()).toBe(6);
  });

  it('sends PickUp with the nearest ground item in pick-up range, before an NPC or player (#243 AC1)', () => {
    const s = setup(
      OPEN,
      [
        { id: 5, x: OPEN.x + 1, z: OPEN.z, kind: EntityKind.Npc },
        { id: 6, x: OPEN.x + 1, z: OPEN.z, kind: EntityKind.Player },
      ],
      [{ id: 12, x: OPEN.x + 1, z: OPEN.z + 1 }, { id: 11, x: OPEN.x, z: OPEN.z + 0.2 }, { id: 10, x: OPEN.x + 2, z: OPEN.z }],
    );
    expect(s.press()).toEqual({ type: 'pickUp', id: 11 });
    expect(s.pickUp).toHaveBeenCalledWith(11);
    expect(s.attack).not.toHaveBeenCalled();
    expect(s.trade).not.toHaveBeenCalled();
    expect(s.t.sent.length).toBe(1);
    expect(envelope(s.t.sent[0]).msgType()).toBe(Message.PickUp);
    expect((envelope(s.t.sent[0]).msg(new PickUp()) as PickUp).groundItemId()).toBe(11);
  });

  it('sends no PickUp when no ground item is in pick-up range (#243 AC1)', () => {
    const s = setup(OPEN, [], [{ id: 10, x: OPEN.x + 2, z: OPEN.z }, { id: 13, x: OPEN.x - 1, z: OPEN.z - 2 }]);
    expect(s.press()).toBeNull();
    expect(s.pickUp).not.toHaveBeenCalled();
    expect(s.t.sent).toEqual([]);
  });

  it('sends nothing with no valid target (AC4)', () => {
    const s = setup(OPEN, [
      { id: 5, x: OPEN.x + 3, z: OPEN.z, kind: EntityKind.Npc },
      { id: 6, x: OPEN.x + 4, z: OPEN.z, kind: EntityKind.Player },
    ]);
    expect(s.press()).toBeNull();
    expect(s.attack).not.toHaveBeenCalled();
    expect(s.trade).not.toHaveBeenCalled();
    expect(s.shop.isOpen).toBe(false);
    expect(s.t.sent).toEqual([]);
  });

  it('opens CardTablePanel for the table in reach, before a ground item or NPC, and sends nothing (#325 AC3)', () => {
    const table = CARD_TABLE_POSITIONS[1];
    const pos = { x: table.x + 1, z: table.z };
    const s = setup(pos, [{ id: 5, x: pos.x + 1, z: pos.z, kind: EntityKind.Npc }], [{ id: 10, x: pos.x, z: pos.z }]);
    expect(s.cardTable.isOpen).toBe(false);
    expect(s.press()).toEqual({ type: 'cardTable', id: table.id });
    expect(s.cardTable.isOpen).toBe(true);
    expect(s.cardTable.table).toBe(table.id);
    expect(s.cardTable.title.textContent).toBe(`Card table ${table.id}`);
    expect(s.pickUp).not.toHaveBeenCalled();
    expect(s.attack).not.toHaveBeenCalled();
    expect(s.t.sent).toEqual([]);
  });

  it('closes the card table panel once the player walks out of reach, as the shop panel does (#325 AC4)', () => {
    const table = CARD_TABLE_POSITIONS[0];
    const s = setup({ x: table.x, z: table.z + 1.5 }, []);
    s.press();
    expect(s.cardTable.isOpen).toBe(true);
    // Still within reach: the panel stays open.
    s.game.prediction.pos.z = table.z + 2;
    s.cardTable.update();
    expect(s.cardTable.isOpen).toBe(true);
    // Out of reach: it closes.
    s.game.prediction.pos.z = table.z + 2.1;
    s.cardTable.update();
    expect(s.cardTable.isOpen).toBe(false);
    // Walking back does not reopen it; Interact does.
    s.game.prediction.pos.z = table.z + 1;
    s.cardTable.update();
    expect(s.cardTable.isOpen).toBe(false);
  });
});
