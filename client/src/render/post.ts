import * as THREE from 'three';
import { EffectComposer } from 'three/examples/jsm/postprocessing/EffectComposer.js';
import { OutputPass } from 'three/examples/jsm/postprocessing/OutputPass.js';
import { Pass } from 'three/examples/jsm/postprocessing/Pass.js';
import { RenderPass } from 'three/examples/jsm/postprocessing/RenderPass.js';
import { ShaderPass } from 'three/examples/jsm/postprocessing/ShaderPass.js';
import { UnrealBloomPass } from 'three/examples/jsm/postprocessing/UnrealBloomPass.js';
import type { RetroPass } from './retro';

// Placeholders from PRD 13 (Client and art), tuned after profiling on an iPhone 13 and playtests.
/** Linear luminance above which a pixel blooms (lamp heads, lit windows). */
export const BLOOM_THRESHOLD = 0.85;
/** How strongly the bloom is added back onto the frame. */
export const BLOOM_STRENGTH = 0.6;
/** Spread of the bloom across its mip levels (0..1). */
export const BLOOM_RADIUS = 0.4;
/** Bloom resolution as a fraction of the frame's drawing buffer. */
export const BLOOM_SCALE = 0.5;
/** How far mid-tones are pushed towards red and away from blue. */
export const GRADE_WARMTH = 0.05;
/** How much the corners darken (0 = none, 1 = black). */
export const LOOK_VIGNETTE = 0.25;

export type RGB = readonly [number, number, number];

/** Per-channel gain at full mid-tone weight; this is the grade shader's `tint` uniform. */
export function gradeTint(warmth: number): RGB {
  return [1 + warmth, 1, 1 - warmth];
}

/**
 * The warm grade, mirrored by the grade shader: mid-tones are scaled by `tint`, with a weight
 * that falls to zero at black and at white so highlights stay neutral.
 */
export function gradeColor(c: RGB, tint: RGB): RGB {
  const luma = Math.min(1, Math.max(0, 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]));
  const w = 4 * luma * (1 - luma);
  return [c[0] * (1 + (tint[0] - 1) * w), c[1] * (1 + (tint[1] - 1) * w), c[2] * (1 + (tint[2] - 1) * w)];
}

const vertexShader = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}`;

/** Warm colour grade on the linear HDR frame; see gradeColor. */
export const GradeShader = {
  name: 'GradeShader',
  uniforms: {
    tDiffuse: { value: null },
    tint: { value: new THREE.Vector3(...gradeTint(GRADE_WARMTH)) },
  },
  vertexShader,
  fragmentShader: /* glsl */ `
uniform sampler2D tDiffuse;
uniform vec3 tint;
varying vec2 vUv;
void main() {
  vec4 c = texture2D(tDiffuse, vUv);
  float luma = clamp(dot(c.rgb, vec3(0.2126, 0.7152, 0.0722)), 0.0, 1.0);
  float w = 4.0 * luma * (1.0 - luma);
  gl_FragColor = vec4(c.rgb * (1.0 + (tint - 1.0) * w), c.a);
}`,
};

/** Soft vignette: darkens towards the corners by up to `vignette`. */
export const VignetteShader = {
  name: 'VignetteShader',
  uniforms: {
    tDiffuse: { value: null },
    vignette: { value: LOOK_VIGNETTE },
  },
  vertexShader,
  fragmentShader: /* glsl */ `
uniform sampler2D tDiffuse;
uniform float vignette;
varying vec2 vUv;
void main() {
  vec4 c = texture2D(tDiffuse, vUv);
  vec2 d = vUv - 0.5;
  gl_FragColor = vec4(c.rgb * (1.0 - vignette * dot(d, d) * 2.0), c.a);
}`,
};

/** Unreal bloom drawn at BLOOM_SCALE of the frame it reads. */
export class ScaledBloomPass extends UnrealBloomPass {
  constructor(width: number, height: number) {
    super(new THREE.Vector2(scaled(width), scaled(height)), BLOOM_STRENGTH, BLOOM_RADIUS, BLOOM_THRESHOLD);
  }

  /** `width` × `height` is the frame's size; the bloom follows at BLOOM_SCALE. */
  override setSize(width: number, height: number): void {
    this.resolution.set(scaled(width), scaled(height));
    super.setSize(this.resolution.x, this.resolution.y);
  }
}

function scaled(n: number): number {
  return Math.max(1, Math.round(n * BLOOM_SCALE));
}

/** Last pass while retro is on: the retro shader reads the graded frame and draws it to the canvas. */
class RetroOutputPass extends Pass {
  retro: RetroPass | null = null;

  constructor() {
    super();
    this.needsSwap = false;
    this.enabled = false;
  }

  override render(renderer: THREE.WebGLRenderer, _write: THREE.WebGLRenderTarget, read: THREE.WebGLRenderTarget): void {
    this.retro?.renderInput(renderer, read.texture);
  }
}

/** Whether the renderer can draw into the half-float targets the HDR chain needs. */
export function supportsPost(gl: THREE.WebGLRenderer): boolean {
  const ext = (gl as Partial<THREE.WebGLRenderer>).extensions;
  if (typeof ext?.has !== 'function') return false;
  return ext.has('EXT_color_buffer_half_float') || ext.has('EXT_color_buffer_float');
}

/**
 * The frame's post-process chain: scene → bloom → warm grade → vignette, all in linear HDR,
 * then tone mapping and sRGB output (OutputPass), or the retro pass when retro is on.
 */
export class PostChain {
  readonly composer: EffectComposer;
  readonly scenePass: RenderPass;
  readonly bloomPass: ScaledBloomPass;
  readonly gradePass: ShaderPass;
  readonly vignettePass: ShaderPass;
  readonly outputPass: OutputPass;
  readonly retroPass = new RetroOutputPass();

  constructor(gl: THREE.WebGLRenderer, scene: THREE.Scene, camera: THREE.Camera, width: number, height: number) {
    const target = new THREE.WebGLRenderTarget(width, height, { type: THREE.HalfFloatType });
    this.composer = new EffectComposer(gl, target);
    // Sizes passed in are drawing-buffer pixels already.
    this.composer.setPixelRatio(1);
    this.scenePass = new RenderPass(scene, camera);
    this.bloomPass = new ScaledBloomPass(width, height);
    this.gradePass = new ShaderPass(GradeShader);
    this.vignettePass = new ShaderPass(VignetteShader);
    this.outputPass = new OutputPass();
    for (const p of [this.scenePass, this.bloomPass, this.gradePass, this.vignettePass, this.outputPass, this.retroPass]) {
      this.composer.addPass(p);
    }
    this.setSize(width, height);
  }

  get bloom(): boolean {
    return this.composer.passes.includes(this.bloomPass);
  }

  /** Removes or restores the bloom pass (quality presets turn it off). */
  setBloom(enabled: boolean): void {
    if (enabled === this.bloom) return;
    if (enabled) this.composer.insertPass(this.bloomPass, this.composer.passes.indexOf(this.scenePass) + 1);
    else this.composer.removePass(this.bloomPass);
  }

  /** Ends the chain in `retro` (drawn at its low resolution, nearest-filtered) or, with null, at full resolution. */
  setRetro(retro: RetroPass | null): void {
    this.retroPass.retro = retro;
    this.retroPass.enabled = retro !== null;
    this.outputPass.enabled = retro === null;
    const filter = retro ? THREE.NearestFilter : THREE.LinearFilter;
    for (const t of [this.composer.renderTarget1, this.composer.renderTarget2]) {
      if (t.texture.magFilter === filter) continue;
      t.texture.minFilter = filter;
      t.texture.magFilter = filter;
      // Filters are fixed when the GPU texture is made, so free it and let three remake it.
      t.dispose();
    }
  }

  /** Size of the frame in drawing-buffer pixels. */
  setSize(width: number, height: number): void {
    this.composer.setSize(width, height);
    // A removed bloom pass is not resized by the composer; keep it ready to restore.
    if (!this.bloom) this.bloomPass.setSize(width, height);
  }

  render(): void {
    this.composer.render(0);
  }

  dispose(): void {
    this.composer.dispose();
    this.bloomPass.dispose();
    this.gradePass.dispose();
    this.vignettePass.dispose();
    this.outputPass.dispose();
  }
}
