import * as THREE from 'three';
import { CSS2DRenderer } from 'three/examples/jsm/renderers/CSS2DRenderer.js';
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
  /** HTML overlay for chat text, made with the first message. */
  let labels: CSS2DRenderer | null = null;
  const placeLabels = () => {
    if (!labels) return;
    const r = canvas.getBoundingClientRect();
    labels.setSize(canvas.clientWidth, canvas.clientHeight);
    const s = labels.domElement.style;
    s.left = `${r.left}px`;
    s.top = `${r.top}px`;
  };
  const resize = () => {
    const w = canvas.clientWidth;
    const h = canvas.clientHeight;
    renderer.setSize(w, h, false);
    placeLabels();
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
      if (!labels && world.chat.group.parent) {
        labels = new CSS2DRenderer();
        labels.domElement.style.cssText += 'position:fixed;pointer-events:none;overflow:hidden;z-index:4;';
        document.body.appendChild(labels.domElement);
        placeLabels();
      }
      labels?.render(world.scene, world.rig.camera);
    },
    resize,
    dispose() {
      detach();
      hideAttribution();
      labels?.domElement.remove();
      renderer.dispose();
    },
  };
}
