import { GameLoop } from './core/loop';
import { Controls } from './ui/controls';

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

new GameLoop({
  update: () => {
    controls.worldMove();
  },
  render: () => {
    if (!ctx) return;
    ctx.fillStyle = '#6aa7d8';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
  },
}).start();
