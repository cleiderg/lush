import * as THREE from 'three';
import { CSS2DRenderer } from 'three/examples/jsm/renderers/CSS2DRenderer.js';
import { WorldScene } from './scene';
import { showAttribution } from './attribution';
import { PostChain, supportsPost } from './post';
import { RetroPass } from './retro';
import type { WorldView } from './worldView';

/** Exposure before tone mapping. Placeholder tuned by eye against day-tavern.jpg. */
export const TONE_EXPOSURE = 1;

export interface GameRenderer {
  readonly world: WorldScene;
  /** The underlying three renderer (tone mapping, shadows). */
  readonly gl: THREE.WebGLRenderer;
  /** Whether the retro post-process (PRD 5.2) is on. Starts off. */
  readonly retro: boolean;
  setRetro(enabled: boolean): void;
  /**
   * The post-process chain (scene, bloom, warm grade, vignette, then output or retro), or null
   * when the device can't draw half-float targets and the scene goes straight to the canvas.
   */
  readonly post: PostChain | null;
  /** Whether bloom is on. Starts on where the post chain runs. */
  readonly bloom: boolean;
  /** Removes or restores the bloom pass (for quality presets). */
  setBloom(enabled: boolean): void;
  render(view: WorldView): void;
  resize(): void;
  dispose(): void;
}

/** WebGL renderer on the given canvas. Call render() from the game loop's render step. */
export function createRenderer(canvas: HTMLCanvasElement): GameRenderer {
  const renderer = new THREE.WebGLRenderer({ canvas, antialias: false, powerPreference: 'high-performance' });
  const pixelRatio = Math.min(window.devicePixelRatio, 2);
  renderer.setPixelRatio(pixelRatio);
  // ACES filmic tone mapping into sRGB, applied at the end of the post chain (OutputPass or retro).
  renderer.toneMapping = THREE.ACESFilmicToneMapping;
  renderer.toneMappingExposure = TONE_EXPOSURE;
  renderer.outputColorSpace = THREE.SRGBColorSpace;
  // Soft sun shadows. three 0.186 removed PCFSoftShadowMap; PCFShadowMap is now the soft, filtered one.
  renderer.shadowMap.enabled = true;
  renderer.shadowMap.type = THREE.PCFShadowMap;
  /** Low-resolution target and pass, made while retro is on and freed when it goes off. */
  let retro: RetroPass | null = null;
  const bufferWidth = () => Math.round(canvas.clientWidth * pixelRatio);
  const bufferHeight = () => Math.round(canvas.clientHeight * pixelRatio);
  const world = new WorldScene();
  const post = supportsPost(renderer)
    ? new PostChain(renderer, world.scene, world.rig.camera, Math.max(1, bufferWidth()), Math.max(1, bufferHeight()))
    : null;
  /** The post chain runs at the retro target's low resolution while retro is on. */
  const sizePost = () => {
    if (!post) return;
    if (retro) post.setSize(retro.target.width, retro.target.height);
    else post.setSize(Math.max(1, bufferWidth()), Math.max(1, bufferHeight()));
  };
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
    retro?.setSize(bufferWidth(), bufferHeight());
    sizePost();
    placeLabels();
    world.rig.camera.aspect = w / Math.max(1, h);
    world.rig.camera.updateProjectionMatrix();
  };
  resize();
  const hideAttribution = showAttribution();
  return {
    world,
    gl: renderer,
    get retro() {
      return retro !== null;
    },
    setRetro(enabled) {
      if (enabled && !retro) retro = new RetroPass(bufferWidth(), bufferHeight());
      if (!enabled && retro) {
        retro.dispose();
        retro = null;
      }
      post?.setRetro(retro);
      sizePost();
    },
    post,
    get bloom() {
      return post?.bloom ?? false;
    },
    setBloom(enabled) {
      post?.setBloom(enabled);
    },
    render(view) {
      world.update(view);
      if (post) post.render();
      else if (retro) retro.render(renderer, world.scene, world.rig.camera);
      else renderer.render(world.scene, world.rig.camera);
      if (!labels && (world.chat.group.parent || world.floatTexts.group.parent)) {
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
      retro?.dispose();
      retro = null;
      post?.dispose();
      renderer.dispose();
    },
  };
}
