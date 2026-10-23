import * as THREE from 'three';
import { WorldScene } from './scene';
import { showAttribution } from './attribution';
import type { WorldView } from './worldView';

export interface GameRenderer {
  readonly world: WorldScene;
  render(view: WorldView): void;
  resize(): void;
  dispose(): void;
}

/** WebGL renderer on the given canvas. Call render() from the game loop's render step. */
export function createRenderer(canvas: HTMLCanvasElement): GameRenderer {
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: false, powerPreference: 'high-performance' });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
  const world = new WorldScene();
  const detach = world.rig.attach(canvas);
  const resize = () => {
    const w = canvas.clientWidth;
    const h = canvas.clientHeight;
    renderer.setSize(w, h, false);
    world.rig.camera.aspect = w / Math.max(1, h);
    world.rig.camera.updateProjectionMatrix();
  };
  resize();
  const hideAttribution = showAttribution();
  return {
    world,
    render(view) {
      world.update(view);
      renderer.render(world.scene, world.rig.camera);
    },
    resize,
    dispose() {
      detach();
      hideAttribution();
      renderer.dispose();
    },
  };
}
