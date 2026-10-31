import { EquipSlot, ItemKind, ShedColour, ShedSymbol } from '../protocol';
import type { ItemInfo, ServerMessage, ShedCardValue } from './codec';
import { OFFLINE_PLAYER_ID, seededRandom, type OfflineOptions } from './offline';

/**
 * Fixed panel data for offline mode (`?offline&panel=<name>`, npm run shots): an item catalog with
 * a shop, the player's stats and inventory, a card table where bots sit in a Shed game with the
 * player, chat lines and, for the trade shot, an open trade with a bot. All of it comes from the
 * seed, as decoded server messages, after the offline Welcome; online sessions never get any.
 */

/** The bots the panels use: the first offline bots' ids (makeBots numbers them from OFFLINE_PLAYER_ID + 1). */
const BOT_IDS = [OFFLINE_PLAYER_ID + 1, OFFLINE_PLAYER_ID + 2, OFFLINE_PLAYER_ID + 3];
/** Card table 1 (shared/map/cardtables.json), where OFFLINE_PLACES.cardtable stands. */
export const OFFLINE_TABLE_ID = 1;

function item(id: number, name: string, kind: ItemKind, extra: Partial<ItemInfo> = {}): ItemInfo {
  return {
    id, name, kind, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0, attackSpeed: 0, heal: 0,
    shopBuysFor: 1, shopSells: false, shopSellsFor: 0, ...extra,
  };
}

/** A small catalog like the server's (items.json); some of it the shop sells. */
const CATALOG: readonly ItemInfo[] = [
  item(1, 'Wooden stick', ItemKind.Weapon, { equipSlot: EquipSlot.Weapon, accuracy: 3, strength: 2, attackSpeed: 4, shopBuysFor: 2, shopSells: true, shopSellsFor: 5 }),
  item(2, 'Bronze dagger', ItemKind.Weapon, { equipSlot: EquipSlot.Weapon, accuracy: 8, strength: 5, attackSpeed: 3, shopBuysFor: 15, shopSells: true, shopSellsFor: 40 }),
  item(4, 'Hoodie', ItemKind.Armor, { equipSlot: EquipSlot.Body, defence: 4, shopBuysFor: 5, shopSells: true, shopSellsFor: 12 }),
  item(5, 'Cardboard shield', ItemKind.Armor, { equipSlot: EquipSlot.Shield, defence: 3, shopBuysFor: 3, shopSells: true, shopSellsFor: 8 }),
  item(6, 'Leather cap', ItemKind.Armor, { equipSlot: EquipSlot.Head, defence: 3, levelToEquip: 5, shopBuysFor: 10 }),
  item(9, 'Boba', ItemKind.Food, { heal: 4, shopBuysFor: 1, shopSells: true, shopSellsFor: 3 }),
  item(10, 'Burrito', ItemKind.Food, { heal: 10, shopBuysFor: 3, shopSells: true, shopSellsFor: 8 }),
  item(11, 'Acorn', ItemKind.Loot),
  item(12, 'Shiny trinket', ItemKind.Loot, { shopBuysFor: 25 }),
  item(13, 'Coyote pelt', ItemKind.Loot, { shopBuysFor: 12 }),
  item(14, 'Scrap metal', ItemKind.Loot, { shopBuysFor: 8 }),
];

/** Inventory slots (PRD: 28). */
const INVENTORY_SLOTS = 28;

const CHAT = [
  'anyone up for cards?', 'selling acorns, cheap', 'where is the shop?', 'gl everyone', 'brb getting boba',
  'watch out for the coyotes', 'trade?', 'nice hoodie', 'meet at the park', 'lol',
];
const CHAT_LINES = 6;

/** Random integer in [lo, hi]. */
function between(rand: () => number, lo: number, hi: number): number {
  return lo + Math.floor(rand() * (hi - lo + 1));
}

function shedCard(rand: () => number): ShedCardValue {
  const symbol = between(rand, ShedSymbol.Zero, ShedSymbol.WildDrawFour) as ShedSymbol;
  const wild = symbol === ShedSymbol.Wild || symbol === ShedSymbol.WildDrawFour;
  return { colour: wild ? ShedColour.None : (between(rand, ShedColour.One, ShedColour.Four) as ShedColour), symbol };
}

/**
 * The messages that fill the panels, in the order a server would send them. The same seed and
 * panel always give the same messages; the trade window is only opened for the trade panel,
 * since an open trade takes the panel area.
 */
export function offlinePanelMessages(opts: Pick<OfflineOptions, 'seed' | 'panel'>, playerId = OFFLINE_PLAYER_ID): ServerMessage[] {
  const rand = seededRandom(opts.seed ^ 0x5eed_9a7e);
  const pick = <T>(list: readonly T[]): T => list[Math.floor(rand() * list.length)];

  const slots = Array.from({ length: INVENTORY_SLOTS }, () => ({ itemId: 0, quantity: 0 }));
  const filled = between(rand, 8, 14);
  for (let i = 0; i < filled; i++) {
    const d = pick(CATALOG);
    slots[i] = { itemId: d.id, quantity: d.kind === ItemKind.Loot || d.kind === ItemKind.Food ? between(rand, 1, 20) : 1 };
  }
  const maxHp = between(rand, 20, 40);

  const msgs: ServerMessage[] = [
    { type: 'itemCatalog', items: CATALOG.map((d) => ({ ...d })) },
    { type: 'playerStats', hp: between(rand, 10, maxHp), maxHp, combatLevel: between(rand, 3, 20), xp: between(rand, 100, 5000), gold: between(rand, 50, 900) },
    { type: 'inventory', slots, equipment: { weapon: 1, head: 0, body: 4, shield: 0 } },
    { type: 'cardTableCatalog', tables: [{ id: OFFLINE_TABLE_ID, seats: 4, stake: 10 }] },
  ];

  // A Shed game at table 1: the player in seat 0 and three bots, the player's turn.
  const seats = [playerId, ...BOT_IDS];
  const hand = Array.from({ length: between(rand, 4, 7) }, () => shedCard(rand));
  let top = shedCard(rand);
  while (top.colour === ShedColour.None) top = shedCard(rand);
  msgs.push(
    {
      type: 'cardTableState', tableId: OFFLINE_TABLE_ID, seats, inProgress: true, turnSeat: 0, reversed: false,
      turnTicksLeft: 300, winnerSeat: null, ready: seats.map(() => true),
    },
    {
      type: 'shedState', tableId: OFFLINE_TABLE_ID, topCard: top, colour: top.colour,
      handCounts: [hand.length, ...BOT_IDS.map(() => between(rand, 2, 7))], drawPile: between(rand, 30, 70),
    },
    { type: 'shedHand', tableId: OFFLINE_TABLE_ID, cards: hand },
  );

  for (let i = 0; i < CHAT_LINES; i++) {
    msgs.push({ type: 'chat', speakerId: i === CHAT_LINES - 2 ? playerId : pick(BOT_IDS), text: pick(CHAT) });
  }

  if (opts.panel === 'trade') {
    const mine = slots.filter((s) => s.itemId).slice(0, 2).map((s) => ({ itemId: s.itemId, quantity: Math.min(s.quantity, 3) }));
    msgs.push({
      type: 'tradeState', partnerId: BOT_IDS[0], screen: 1,
      myItems: mine, myGold: between(rand, 0, 50),
      partnerItems: [{ itemId: pick(CATALOG).id, quantity: 1 }, { itemId: 13, quantity: between(rand, 1, 5) }], partnerGold: between(rand, 5, 100),
      myAccepted: false, partnerAccepted: true,
    });
  }
  return msgs;
}

/**
 * The Session's onMessage for these options: with a panel offline, the panel data follows each
 * Welcome (which clears trade and card table state); otherwise handle itself, so online sessions
 * and plain offline sessions get none of it.
 */
export function withOfflinePanels(
  opts: OfflineOptions | null,
  handle: (msg: ServerMessage) => void,
): (msg: ServerMessage) => void {
  if (!opts?.panel) return handle;
  return (msg) => {
    handle(msg);
    if (msg.type !== 'welcome') return;
    for (const m of offlinePanelMessages(opts, msg.playerId)) handle(m);
  };
}
