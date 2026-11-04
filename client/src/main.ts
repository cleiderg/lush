import { AudioManager, installUiClicks } from './audio';
import { initErrorReporting } from './core/errorReporting';
import { interact, interactPromptText, interactTarget } from './core/interact';
import { GameLoop } from './core/loop';
import { detectDevice, localQualityStore, QualityChooser, startingPreset } from './core/quality';
import { QualityController } from './core/qualityController';
import { QualityControl } from './ui/qualityControl';
import { sendMove } from './core/move';
import { buildWorldView, controlYaw } from './core/view';
import { sendWave } from './core/wave';
import { memoryTokenStore, OfflineTransport, offlineOptions, type OfflinePanel } from './net/offline';
import { OFFLINE_HOUSE_ID, OFFLINE_TABLE_ID, withOfflinePanels } from './net/offlinePanels';
import { localTokenStore, Session } from './net/session';
import { WebSocketTransport } from './net/transport';
import { createRenderer } from './render/renderer';
import collisionUrl from '../../shared/map/collision.bin?url';
import { CollisionGrid } from './state/collision';
import { GameState } from './state/world';
import { ChatLog, anyOpen } from './ui/chatLog';
import { BankPanel } from './ui/bankPanel';
import { CardTablePanel } from './ui/cardTablePanel';
import { Controls } from './ui/controls';
import { Hud } from './ui/hud';
import { InteractPrompt } from './ui/interactPrompt';
import { PvpWarning } from './ui/pvpWarning';
import { RestartBanner } from './ui/restartBanner';
import { campusMapData } from './ui/campusMapData';
import { LocationPanel } from './ui/locationPanel';
import { BigMap, Minimap } from './ui/minimap';
import { PlayerPortrait } from './ui/playerPortrait';
import { localRetroStore, RetroToggle } from './ui/retroToggle';
import { HomeLeaveButton } from './ui/homeLeaveButton';
import { HomeEditPanel, type Tile } from './ui/homeEditPanel';
import { HousePanel } from './ui/housePanel';
import { SHOP_STALL, ShopPanel } from './ui/shopPanel';
import { SkillsPanel } from './ui/skillsPanel';
import { applyTheme, BUTTON_CLASS } from './ui/theme';
import { mountTimePanel } from './ui/timePanel';
import { TradePanel } from './ui/tradePanel';

initErrorReporting();

// Theme properties and the self-hosted title font, before any panel is built.
applyTheme();

// Audio starts only after the first user gesture; from then on every button clicks softly.
const audio = new AudioManager({ factory: () => new AudioContext(), window, document });
installUiClicks(document, audio, `.${BUTTON_CLASS}`);

const canvas = document.getElementById('game-canvas') as HTMLCanvasElement;
const renderer = createRenderer(canvas);
window.addEventListener('resize', () => renderer.resize());
// Applies the saved Retro choice (off when nothing is saved) before the first frame.
new RetroToggle({ parent: document.body, target: renderer, store: localRetroStore() });
// Applies the saved Quality choice (Auto when nothing is saved); Auto steps down as frame times come in.
const quality = new QualityController(renderer, localQualityStore(), new QualityChooser(startingPreset(detectDevice())));
const qualityControl = new QualityControl({ parent: document.body, model: quality });
document.addEventListener('visibilitychange', () => {
  if (document.hidden) quality.pause();
});

const controls = new Controls({
  onWave: () => sendWave(session),
  onChatSubmit: (text) => session.sendChat(text),
  tray: document.getElementById('control-tray') as HTMLElement,
  getCameraYaw: () => controlYaw(renderer.world.rig.yaw),
  onInteract: () =>
    interact(game, performance.now(), SHOP_STALL, {
      openShop: () => shop.tryOpen(),
      openBank: () => bank.open(),
      openCardTable: (id) => cardTable.open(id),
      sendSkillAction: (target, id) => session.sendSkillAction(target, id),
      sendPickUp: (id) => session.sendPickUp(id),
      sendAttackTarget: (id) => session.sendAttackTarget(id),
      sendTradeRequest: (id) => session.sendTradeRequest(id),
    }),
});

const game = new GameState();
// Until the grid arrives (or if it fails to load) prediction moves freely and the server corrects.
fetch(collisionUrl)
  .then((res) => {
    if (!res.ok) throw new Error(`collision.bin: HTTP ${res.status}`);
    return res.arrayBuffer();
  })
  .then((buf) => game.setCollision(CollisionGrid.parse(new Uint8Array(buf))))
  .catch((err: unknown) => console.error('collision grid not loaded', err));
const banner = new RestartBanner(document.body);
const send = (bytes: Uint8Array): void => session.send(bytes);
const hud = new Hud({
  parent: document.body,
  items: game.items,
  send,
  onOpen: () => {
    skills.setOpen(false);
    shop.close();
    house.close();
    cardTable.close();
    bank.close();
    bigMap.close();
  },
});
// The Skills tab beside the inventory: the two swap in the panel area.
const skills = new SkillsPanel({
  parent: document.body,
  skills: game.skills,
  items: game.items,
  inventory: hud,
  onOpen: () => {
    hud.setOpen(false);
    shop.close();
    house.close();
    cardTable.close();
    bank.close();
    bigMap.close();
  },
});
const shop = new ShopPanel({
  parent: document.body,
  items: game.items,
  send,
  position: () => game.prediction.pos,
  onOpen: () => {
    hud.setOpen(false);
    skills.setOpen(false);
    house.close();
    cardTable.close();
    bank.close();
    bigMap.close();
  },
});
// Opened from the offline shot for now; Interact at a door comes with the door positions (#524).
const house = new HousePanel({
  parent: document.body,
  housing: game.housing,
  send,
  sendBid: (id, amount) => session.sendAuctionBid(id, amount),
  onOpen: () => {
    hud.setOpen(false);
    skills.setOpen(false);
    shop.close();
    cardTable.close();
    bank.close();
    bigMap.close();
  },
});
// Edit mode inside the player's own home. The room renderer answers the pick with the tapped tile (tile picking is a render issue).
let tilePick: ((tile: Tile) => void) | null = null;
const homeEdit = new HomeEditPanel({
  parent: document.body,
  housing: game.housing,
  items: game.items,
  session: {
    sendHomePlace: (slot, p) => session.sendHomePlace(slot, p),
    sendHomeMove: (id, p) => session.sendHomeMove(id, p),
    sendHomeRotate: (id, r) => session.sendHomeRotate(id, r),
    sendHomeRemove: (id) => session.sendHomeRemove(id),
    sendHomeSetWallpaper: (slot) => session.sendHomeSetWallpaper(slot),
    sendHomeSetFloor: (slot) => session.sendHomeSetFloor(slot),
  },
  pickTile: (onTile) => { tilePick = onTile; },
});
void tilePick;
const homeLeave = new HomeLeaveButton({ parent: document.body, housing: game.housing, sendHomeLeave: () => session.sendHomeLeave() });
void homeLeave;
const cardTable = new CardTablePanel({
  parent: document.body,
  tables: game.cardTables,
  session: {
    sendCardTableSit: (id) => session.sendCardTableSit(id),
    sendCardTableReady: () => session.sendCardTableReady(),
    sendCardTableLeave: () => session.sendCardTableLeave(),
    sendShedPlay: (card, colour) => session.sendShedPlay(card, colour),
    sendShedDraw: () => session.sendShedDraw(),
  },
  position: () => game.prediction.pos,
  onOpen: () => {
    hud.setOpen(false);
    skills.setOpen(false);
    shop.close();
    house.close();
    bank.close();
    bigMap.close();
  },
});
const bank = new BankPanel({
  parent: document.body,
  bank: game.bank,
  items: game.items,
  session: {
    sendBankOpen: () => session.sendBankOpen(),
    sendBankClose: () => session.sendBankClose(),
    sendBankDeposit: (id, n) => session.sendBankDeposit(id, n),
    sendBankWithdraw: (id, n) => session.sendBankWithdraw(id, n),
    sendBankDepositInventory: () => session.sendBankDepositInventory(),
    sendBankDepositEquipment: () => session.sendBankDepositEquipment(),
  },
  position: () => game.prediction.pos,
  onOpen: () => {
    hud.setOpen(false);
    skills.setOpen(false);
    shop.close();
    house.close();
    cardTable.close();
    bigMap.close();
  },
});
const locationPanel = new LocationPanel({ parent: document.body, position: () => game.prediction.pos, clock: game.clock });
const portrait = new PlayerPortrait({ parent: document.body, stats: () => game.items.stats });
// Shows what Interact will do, from the same target the button uses.
const interactPrompt = new InteractPrompt({
  parent: document.body,
  prompt: () => interactPromptText(game, interactTarget(game, performance.now(), SHOP_STALL)),
});
const pvpWarning = new PvpWarning({ parent: document.body, position: () => game.prediction.pos });
const trade = new TradePanel({
  parent: document.body,
  trade: game.trade,
  items: game.items,
  send,
  onOpen: () => {
    hud.setOpen(false);
    skills.setOpen(false);
    shop.close();
    house.close();
    cardTable.close();
    bank.close();
    bigMap.close();
  },
});
const remotePositions = () => game.remotes(performance.now());
// Tapping the minimap opens the big map in the panel area.
const bigMap = new BigMap({
  parent: document.body,
  data: campusMapData(),
  houses: () => game.housing.houses,
  position: () => game.prediction.pos,
  remotes: remotePositions,
  onOpen: () => {
    hud.setOpen(false);
    skills.setOpen(false);
    shop.close();
    house.close();
    cardTable.close();
    bank.close();
  },
});
const minimap = new Minimap({
  parent: document.body,
  data: campusMapData(),
  houses: () => game.housing.houses,
  position: () => game.prediction.pos,
  yaw: () => renderer.world.rig.yaw,
  remotes: remotePositions,
  onOpen: () => bigMap.toggle(),
});
const chatLog = new ChatLog({
  parent: document.body,
  lines: () => game.chat.log,
  playerId: () => game.playerId,
  covered: () => anyOpen([hud, skills, shop, house, bank, trade, cardTable, bigMap]),
});
// Developer-only time control (?debug): overrides the shared time the sky follows.
mountTimePanel(location.search, { parent: document.body, clock: game.clock });
// ?offline: local bots stand in for the server and no WebSocket is opened (npm run shots).
const offline = offlineOptions(location.search);
const session = new Session({
  factory: () => (offline ? new OfflineTransport(offline) : new WebSocketTransport()),
  // Offline never touches the saved guest token.
  store: offline ? memoryTokenStore() : localTokenStore(),
  // ?offline&panel=<name>: fixed panel data follows the offline Welcome, then that panel opens.
  onMessage: withOfflinePanels(offline, (msg) => {
    game.handle(msg, performance.now());
    banner.set(game.restartSeconds);
    if (offline?.panel) openOfflinePanel(offline.panel);
  }),
  onHomeLeave: () => game.housing.beginLeave(),
  onReload: () => location.reload(),
});
session.start();

/** Opens a panel for npm run shots once its data is in, and marks the page so the script knows it is open. */
function openOfflinePanel(panel: OfflinePanel): void {
  if (document.body.dataset.offlinePanel === panel) return;
  const opened: Record<OfflinePanel, () => boolean> = {
    inventory: () => {
      hud.setOpen(true);
      return hud.isOpen;
    },
    shop: () => shop.tryOpen(),
    house: () => {
      house.open(OFFLINE_HOUSE_ID);
      return house.isOpen;
    },
    'house-auction': () => {
      house.open(OFFLINE_HOUSE_ID);
      return house.isOpen;
    },
    'home-edit': () => {
      homeEdit.setEditing(true);
      return homeEdit.isEditing;
    },
    home: () => game.housing.inHome(),
    bank: () => bank.isOpen,
    trade: () => trade.isOpen,
    'card-table': () => {
      cardTable.open(OFFLINE_TABLE_ID);
      return cardTable.isOpen;
    },
    'chat-log': () => game.chat.log.length > 0,
    skills: () => {
      skills.setOpen(true);
      return skills.isOpen;
    },
    'big-map': () => {
      bigMap.open();
      return bigMap.isOpen;
    },
  };
  if (opened[panel]()) document.body.dataset.offlinePanel = panel;
}

new GameLoop({
  update: (stepMs) => {
    shop.update();
    bank.update();
    cardTable.update();
    pvpWarning.update();
    locationPanel.update();
    portrait.update();
    interactPrompt.update();
    minimap.update();
    bigMap.update();
    const m = controls.worldMove();
    // Inputs the server will never see would pile up unacknowledged; Welcome resets prediction anyway.
    if (!session.connected) return;
    sendMove(game, session, m.x, m.y, stepMs / 1000, controls.isRunning);
  },
  render: () => {
    quality.rendered(performance.now());
    qualityControl.update();
    chatLog.update();
    renderer.render(buildWorldView(game, performance.now()));
  },
}).start();
