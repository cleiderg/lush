import { EntityKind, EquipSlot, HouseState, ItemKind, PlacementKind, QuestRequirementKind, QuestSkill, QuestState, ShedColour, ShedSymbol, Skill, ToolSkill } from '../protocol';
import type { ItemInfo, ServerMessage, ShedCardValue } from './codec';
import { OFFLINE_PLAYER_ID, seededRandom, type OfflineOptions } from './offline';

/**
 * Fixed panel data for offline mode (`?offline&panel=<name>`, npm run shots): an item catalog with
 * a shop, the player's stats and inventory, a card table where bots sit in a Shed game with the
 * player, skill levels and XP, chat lines and, for the trade shot, an open trade with a bot. All of it comes from the
 * seed, as decoded server messages, after the offline Welcome; online sessions never get any.
 */

/** The bots the panels use: the first offline bots' ids (makeBots numbers them from OFFLINE_PLAYER_ID + 1). */
const BOT_IDS = [OFFLINE_PLAYER_ID + 1, OFFLINE_PLAYER_ID + 2, OFFLINE_PLAYER_ID + 3];
/** The offline house the house panel shows: the player's own, in the rent warning. */
export const OFFLINE_HOUSE_ID = 7;
/** Card table 1 (shared/map/cardtables.json), where OFFLINE_PLACES.cardtable stands. */
export const OFFLINE_TABLE_ID = 1;

function item(id: number, name: string, kind: ItemKind, extra: Partial<ItemInfo> = {}): ItemInfo {
  return {
    id, name, kind, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0, attackSpeed: 0, heal: 0,
    shopBuysFor: 1, shopSells: false, shopSellsFor: 0, toolSkill: ToolSkill.None, toolLevel: 0, cookingLevel: 0, footprintW: 0, footprintD: 0, placement: 0, surfaceSlots: 0, thriftSellsFor: 0, thriftBuysFor: 0, ...extra,
  };
}

const NO_REWARDS = { questPoints: 0, gold: 0, items: [], xp: [] };

/** Three quests for the Quests panel shot: one complete, one in progress, one whose requirements aren't met. */
const OFFLINE_QUESTS = [
  { id: 1, name: 'Overdue', description: 'Return a library book.', requirements: [], steps: ['Talk to the librarian.'], rewards: { ...NO_REWARDS, questPoints: 1 }, startNpc: 1, finishNpc: 1 },
  {
    id: 2, name: 'Coyote Trouble', description: 'Help the ranger.', steps: ['Talk to the park ranger.', 'Kill 5 coyotes.'], rewards: { ...NO_REWARDS, questPoints: 2 }, startNpc: 2, finishNpc: 2,
    requirements: [{ kind: QuestRequirementKind.Level, skill: QuestSkill.Combat, level: 15, questId: 0 }],
  },
  {
    id: 3, name: 'Fish Fry', description: 'Feed the lunch rush.', steps: ['Bring 5 trout to the cook.'], rewards: { ...NO_REWARDS, questPoints: 1 }, startNpc: 3, finishNpc: 3,
    requirements: [{ kind: QuestRequirementKind.Level, skill: QuestSkill.Cooking, level: 40, questId: 0 }],
  },
];

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
  item(40, 'Beanbag', ItemKind.Loot, { placement: PlacementKind.Floor, thriftSellsFor: 30, thriftBuysFor: 10 }),
  item(41, 'Study desk', ItemKind.Loot, { placement: PlacementKind.Floor, thriftSellsFor: 60, thriftBuysFor: 20 }),
  item(42, 'Lava lamp', ItemKind.Loot, { placement: PlacementKind.Surface, thriftSellsFor: 24, thriftBuysFor: 8 }),
  item(43, 'Poster', ItemKind.Loot, { placement: PlacementKind.Wall, thriftSellsFor: 15, thriftBuysFor: 5 }),
  item(44, 'Blue wallpaper', ItemKind.Loot, { placement: PlacementKind.Wallpaper, thriftSellsFor: 50, thriftBuysFor: 16 }),
  item(45, 'Wood flooring', ItemKind.Loot, { placement: PlacementKind.Flooring, thriftSellsFor: 50, thriftBuysFor: 16 }),
];

/** The catalog items before the home furnishings, which only the home-edit panel puts in the inventory. */
const BASE_ITEMS = CATALOG.slice(0, 11);

/** Inventory slots (PRD: 28). */
const INVENTORY_SLOTS = 28;

const CHAT = [
  'anyone up for cards?', 'selling acorns, cheap', 'where is the shop?', 'gl everyone', 'brb getting boba',
  'watch out for the coyotes', 'trade?', 'nice hoodie', 'meet at the park', 'lol',
];
const CHAT_LINES = 6;

/** XP at which levels 1 to 16 start (the OSRS table the server uses), so offline skill XP fits its level. */
const LEVEL_XP = [0, 83, 174, 276, 388, 512, 650, 801, 969, 1154, 1358, 1584, 1833, 2107, 2411, 2746];

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
    const d = pick(BASE_ITEMS);
    slots[i] = { itemId: d.id, quantity: d.kind === ItemKind.Loot || d.kind === ItemKind.Food ? between(rand, 1, 20) : 1 };
  }
  const maxHp = between(rand, 20, 40);

  const msgs: ServerMessage[] = [
    { type: 'itemCatalog', items: CATALOG.map((d) => ({ ...d })) },
    { type: 'playerStats', hp: between(rand, 10, maxHp), maxHp, combatLevel: between(rand, 3, 20), xp: between(rand, 100, 5000), gold: between(rand, 50, 900) },
    { type: 'inventory', slots, equipment: { weapon: 1, head: 0, body: 4, shield: 0 } },
    {
      type: 'skillStats',
      skills: [Skill.Woodcutting, Skill.Fishing, Skill.Cooking].map((skill) => {
        const level = between(rand, 1, LEVEL_XP.length - 1);
        // Somewhere inside the level, with a half point (XP comes in tenths).
        const xp = LEVEL_XP[level - 1] + between(rand, 0, LEVEL_XP[level] - LEVEL_XP[level - 1] - 1) + 0.5;
        return { skill, level, xp };
      }),
    },
    { type: 'cardTableCatalog', tables: [{ id: OFFLINE_TABLE_ID, seats: 4, stake: 10 }] },
    { type: 'questCatalog', quests: OFFLINE_QUESTS },
    {
      type: 'questLog',
      quests: [
        { questId: 1, state: QuestState.Complete, step: 0, count: 0 },
        { questId: 2, state: QuestState.InProgress, step: 1, count: 3 },
      ],
      questPoints: 1,
    },
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

  if (opts.panel === 'bank') {
    // Every catalog item banked, so the panel has a list to search; BankState opens the bank.
    msgs.push({ type: 'bankState', slots: CATALOG.map((d) => ({ itemId: d.id, quantity: between(rand, 1, 400) })) });
  }

  if (opts.panel === 'workbench') {
    // Recipes for the furniture items: one the player can craft from the pack, one short of logs, one short of gold.
    const logs = slots.findIndex((x) => !x.itemId);
    slots[logs] = { itemId: 14, quantity: 12 };
    msgs.push(
      { type: 'inventory', slots, equipment: { weapon: 1, head: 0, body: 4, shield: 0 } },
      { type: 'playerStats', hp: 20, maxHp: 20, combatLevel: 3, xp: 500, gold: 120 },
      {
        type: 'workbenchCatalog',
        recipes: [
          { outputItemId: 40, inputs: [{ itemId: 14, quantity: 4 }], gold: 30 },
          { outputItemId: 41, inputs: [{ itemId: 14, quantity: 10 }, { itemId: 13, quantity: 2 }], gold: 80 },
          { outputItemId: 42, inputs: [{ itemId: 14, quantity: 6 }], gold: 400 },
        ],
      },
    );
  }

  if (opts.panel === 'dialogue') {
    // A talking NPC (a bot's id stands in for it) with two lines and three replies.
    msgs.push(
      { type: 'npcCatalog', npcs: [{ id: 90, name: 'Professor Peter', combatLevel: 0, talkable: true }] },
      { type: 'entityInfo', id: BOT_IDS[0], kind: EntityKind.Npc, npcType: 90, combatLevel: 0, totalLevel: 0, equipment: { weapon: 0, head: 0, body: 0, shield: 0 } },
      {
        type: 'dialogue', npcId: BOT_IDS[0],
        lines: ['Ah, a new face! The anteaters have been stealing my notes.', 'Could you help me get them back?'],
        replies: ['Sure, I will help', 'Tell me more', 'Not right now'],
      },
    );
  }

  if (opts.panel === 'house') {
    // The player's own house, 6 days short of dropping below one month prepaid, and one empty house.
    const paidUntil = Math.floor(Date.now() / 1000) + 36 * 24 * 3600;
    msgs.push({
      type: 'housingState', rentPerMonth: 200, claimMonths: 2, maxPrepaidMonths: 6,
      houses: [
        { id: OFFLINE_HOUSE_ID, areaId: 1, ownerName: 'You', mine: true, paidUntil, state: HouseState.Warning, open: true, topBid: 0, auctionEnd: 0, myBid: 0 },
        { id: 8, areaId: 1, ownerName: '', mine: false, paidUntil: 0, state: HouseState.Empty, open: false, topBid: 0, auctionEnd: 0, myBid: 0 },
      ],
    });
  }

  if (opts.panel === 'house-auction') {
    // A house in auction: a top bid, three days left and the player's own held bid.
    const now = Math.floor(Date.now() / 1000);
    msgs.push({
      type: 'housingState', rentPerMonth: 200, claimMonths: 2, maxPrepaidMonths: 6,
      houses: [{ id: OFFLINE_HOUSE_ID, areaId: 1, ownerName: '', mine: false, paidUntil: 0, state: HouseState.Auction, open: false, topBid: 450, auctionEnd: now + 3 * 24 * 3600, myBid: 300 }],
    });
  }

  if (opts.panel === 'home') {
    // Visiting someone else's furnished room: the world view shows it and the Leave button is up.
    msgs.push(
      {
        type: 'housingState', rentPerMonth: 200, claimMonths: 2, maxPrepaidMonths: 6,
        houses: [{ id: OFFLINE_HOUSE_ID, areaId: 1, ownerName: 'Sam', mine: false, paidUntil: 0, state: HouseState.Owned, open: true, topBid: 0, auctionEnd: 0, myBid: 0 }],
      },
      {
        type: 'homeState', houseId: OFFLINE_HOUSE_ID, ownerName: 'Sam', wallpaperItemId: 0, floorItemId: 0,
        items: [
          { placedId: 1, itemId: 41, x: 2, y: 1, rotation: 0, layer: 0, parentPlacedId: 0, slot: 0, side: 0 },
          { placedId: 2, itemId: 40, x: 4, y: 3, rotation: 1, layer: 0, parentPlacedId: 0, slot: 0, side: 0 },
        ],
      },
    );
  }

  if (opts.panel === 'thrift') {
    // Furniture in the inventory to sell back, beside the thrift stock the catalog lists.
    const free = slots.filter((x) => !x.itemId);
    [40, 42, 44].forEach((id, i) => { free[i].itemId = id; free[i].quantity = 1; });
    msgs.push({ type: 'inventory', slots, equipment: { weapon: 1, head: 0, body: 4, shield: 0 } });
  }

  if (opts.panel === 'home-edit') {
    // Inside the player's own home with furniture in the inventory and two pieces placed.
    const paidUntil = Math.floor(Date.now() / 1000) + 90 * 24 * 3600;
    const free = slots.filter((x) => !x.itemId);
    [40, 41, 42, 43, 44, 45].forEach((id, i) => { free[i].itemId = id; free[i].quantity = 1; });
    msgs.push(
      {
        type: 'housingState', rentPerMonth: 200, claimMonths: 2, maxPrepaidMonths: 6,
        houses: [{ id: OFFLINE_HOUSE_ID, areaId: 1, ownerName: 'You', mine: true, paidUntil, state: HouseState.Owned, open: true, topBid: 0, auctionEnd: 0, myBid: 0 }],
      },
      { type: 'inventory', slots, equipment: { weapon: 1, head: 0, body: 4, shield: 0 } },
      {
        type: 'homeState', houseId: OFFLINE_HOUSE_ID, ownerName: 'You', wallpaperItemId: 0, floorItemId: 0,
        items: [
          { placedId: 1, itemId: 41, x: 2, y: 1, rotation: 0, layer: 0, parentPlacedId: 0, slot: 0, side: 0 },
          { placedId: 2, itemId: 40, x: 4, y: 3, rotation: 1, layer: 0, parentPlacedId: 0, slot: 0, side: 0 },
        ],
      },
    );
  }

  if (opts.panel === 'trade') {
    const mine = slots.filter((s) => s.itemId).slice(0, 2).map((s) => ({ itemId: s.itemId, quantity: Math.min(s.quantity, 3) }));
    msgs.push({
      type: 'tradeState', partnerId: BOT_IDS[0], screen: 1,
      myItems: mine, myGold: between(rand, 0, 50),
      partnerItems: [{ itemId: pick(BASE_ITEMS).id, quantity: 1 }, { itemId: 13, quantity: between(rand, 1, 5) }], partnerGold: between(rand, 5, 100),
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
