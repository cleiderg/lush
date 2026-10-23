import { initErrorReporting } from './core/errorReporting';
import { GameLoop } from './core/loop';
import { encodeInput } from './net/codec';
import { localTokenStore, Session } from './net/session';
import { WebSocketTransport } from './net/transport';
import { GameState } from './state/world';
import { Controls } from './ui/controls';
import { RestartBanner } from './ui/restartBanner';

initErrorReporting();

const canvas = document.getElementById('game-canvas') as HTMLCanvasElement;
const ctx = canvas.getContext('2d');

function resize(): void {
  canvas.width = canvas.clientWidth;
  canvas.height = canvas.clientHeight;
}
window.addEventListener('resize', resize);
resize();

// Camera rotation arrives with the renderer; yaw 0 faces north for now.
const controls = new Controls({
  tray: document.getElementById('control-tray') as HTMLElement,
  getCameraYaw: () => 0,
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
  render: () => {
    if (!ctx) return;
    ctx.fillStyle = '#6aa7d8';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
  },
}).start();
