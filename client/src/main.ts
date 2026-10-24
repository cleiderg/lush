import { AudioManager } from './audio';
import { initErrorReporting } from './core/errorReporting';
import { GameLoop } from './core/loop';
import { buildWorldView, controlYaw } from './core/view';
import { encodeInput } from './net/codec';
import { localTokenStore, Session } from './net/session';
import { WebSocketTransport } from './net/transport';
import { createRenderer } from './render/renderer';
import { GameState } from './state/world';
import { Controls } from './ui/controls';
import { RestartBanner } from './ui/restartBanner';

initErrorReporting();

// Audio starts only after the first user gesture.
new AudioManager({ factory: () => new AudioContext(), window, document });

const canvas = document.getElementById('game-canvas') as HTMLCanvasElement;
const renderer = createRenderer(canvas);
window.addEventListener('resize', () => renderer.resize());

const controls = new Controls({
  tray: document.getElementById('control-tray') as HTMLElement,
  getCameraYaw: () => controlYaw(renderer.world.rig.yaw),
});

const game = new GameState();
const banner = new RestartBanner(document.body);
const session = new Session({
  factory: () => new WebSocketTransport(),
  store: localTokenStore(),
  onMessage: (msg) => {
    game.handle(msg, performance.now());
    banner.set(game.restartSeconds);
  },
  onReload: () => location.reload(),
});
session.start();

new GameLoop({
  update: (stepMs) => {
    const m = controls.worldMove();
    // Inputs the server will never see would pile up unacknowledged; Welcome resets prediction anyway.
    if (!session.connected) return;
    const input = game.prediction.applyInput(m.x, m.y, stepMs / 1000);
    session.send(encodeInput(input.seq, m.x, m.y, game.heading));
  },
  render: () => renderer.render(buildWorldView(game, performance.now())),
}).start();
