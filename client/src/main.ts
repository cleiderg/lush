import { GameLoop } from './core/loop';

const canvas = document.getElementById('game-canvas') as HTMLCanvasElement;
const ctx = canvas.getContext('2d');

function resize(): void {
  canvas.width = canvas.clientWidth;
  canvas.height = canvas.clientHeight;
}
window.addEventListener('resize', resize);
resize();

new GameLoop({
  update: () => {},
  render: () => {
    if (!ctx) return;
    ctx.fillStyle = '#6aa7d8';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
  },
}).start();
