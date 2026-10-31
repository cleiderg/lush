import { AudioManager } from './audio';
import { initErrorReporting } from './core/errorReporting';
import { interact, interactPromptText, interactTarget } from './core/interact';
import { GameLoop } from './core/loop';
import { sendMove } from './core/move';
import { buildWorldView, controlYaw } from './core/view';
import { sendWave } from './core/wave';
import { memoryTokenStore, OfflineTransport, offlineOptions } from './net/offline';
import { localTokenStore, Session } from './net/session';
import { WebSocketTransport } from './net/transport';
import { createRenderer } from './render/renderer';
import collisionUrl from '../../shared/map/collision.bin?url';
import { CollisionGrid } from './state/collision';
import { GameState } from './state/world';
import { ChatLog, anyOpen } from './ui/chatLog';
import { CardTablePanel } from './ui/cardTablePanel';
import { Controls } from './ui/controls';
import { Hud } from './ui/hud';
import { InteractPrompt } from './ui/interactPrompt';
import { PvpWarning } from './ui/pvpWarning';
import { RestartBanner } from './ui/restartBanner';
import { LocationPanel } from './ui/locationPanel';
import { localRetroStore, RetroToggle } from './ui/retroToggle';
import { SHOP_STALL, ShopPanel } from './ui/shopPanel';
import { applyTheme } from './ui/theme';
import { mountTimePanel } from './ui/timePanel';
import { TradePanel } from './ui/tradePanel';

initErrorReporting();

// Theme properties and the self-hosted title font, before any panel is built.
applyTheme();

// Audio starts only after the first user gesture.
new AudioManager({ factory: () => new AudioContext(), window, document });

const canvas = document.getElementById('game-canvas') as HTMLCanvasElement;
const renderer = createRenderer(canvas);
window.addEventListener('resize', () => renderer.resize());
// Applies the saved Retro choice (off when nothing is saved) before the first frame.
new RetroToggle({ parent: document.body, target: renderer, store: localRetroStore() });

const controls = new Controls({
  onWave: () => sendWave(session),
  onChatSubmit: (text) => session.sendChat(text),
  tray: document.getElementById('control-tray') as HTMLElement,
  getCameraYaw: () => controlYaw(renderer.world.rig.yaw),
  onInteract: () =>
    interact(game, performance.now(), SHOP_STALL, {
      openShop: () => shop.tryOpen(),
      openCardTable: (id) => cardTable.open(id),
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
    shop.close();
    cardTable.close();
  },
});
const shop = new ShopPanel({
  parent: document.body,
  items: game.items,
  send,
  position: () => game.prediction.pos,
  onOpen: () => {
    hud.setOpen(false);
    cardTable.close();
  },
});
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
    shop.close();
  },
});
const locationPanel = new LocationPanel({ parent: document.body, position: () => game.prediction.pos, clock: game.clock });
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
    shop.close();
    cardTable.close();
  },
});
const chatLog = new ChatLog({
  parent: document.body,
  lines: () => game.chat.log,
  playerId: () => game.playerId,
  covered: () => anyOpen([hud, shop, trade, cardTable]),
});
// Developer-only time control (?debug): overrides the shared time the sky follows.
mountTimePanel(location.search, { parent: document.body, clock: game.clock });
// ?offline: local bots stand in for the server and no WebSocket is opened (npm run shots).
const offline = offlineOptions(location.search);
const session = new Session({
  factory: () => (offline ? new OfflineTransport(offline) : new WebSocketTransport()),
  // Offline never touches the saved guest token.
  store: offline ? memoryTokenStore() : localTokenStore(),
  onMessage: (msg) => {
    game.handle(msg, performance.now());
    banner.set(game.restartSeconds);
  },
  onReload: () => location.reload(),
});
session.start();

new GameLoop({
  update: (stepMs) => {
    shop.update();
    cardTable.update();
    pvpWarning.update();
    locationPanel.update();
    interactPrompt.update();
    const m = controls.worldMove();
    // Inputs the server will never see would pile up unacknowledged; Welcome resets prediction anyway.
    if (!session.connected) return;
    sendMove(game, session, m.x, m.y, stepMs / 1000, controls.isRunning);
  },
  render: () => {
    chatLog.update();
    renderer.render(buildWorldView(game, performance.now()));
  },
}).start();
