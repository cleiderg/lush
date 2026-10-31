import * as THREE from 'three';

// Placeholders from PRD 13 (Client and art), tuned after profiling on an iPhone 13.
/** Internal resolution as a fraction of the canvas's drawing buffer. */
export const RETRO_SCALE = 0.5;
/** Colour levels per channel after the 4x4 ordered dither. */
export const RETRO_COLOR_LEVELS = 32;
/** Grain amplitude, in output colour units. */
export const RETRO_GRAIN = 0.04;
/** How much the corners darken (0 = none, 1 = black). */
export const RETRO_VIGNETTE = 0.35;

const vertexShader = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = vec4(position.xy, 0.0, 1.0);
}`;

const fragmentShader = /* glsl */ `
uniform sampler2D tScene;
uniform vec2 resolution;
uniform float levels;
uniform float grain;
uniform float vignette;
uniform float frame;
varying vec2 vUv;

float bayer4(vec2 p) {
  vec2 q = mod(p, 4.0);
  int i = int(q.x) + int(q.y) * 4;
  // 4x4 Bayer matrix, row-major, values 0..15.
  int m[16] = int[16](0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5);
  return (float(m[i]) + 0.5) / 16.0;
}

float hash(vec2 p) {
  return fract(sin(dot(p, vec2(12.9898, 78.233))) * 43758.5453);
}

void main() {
  vec4 texel = texture2D(tScene, vUv);
  #ifdef TONE_MAPPING
  // three tone maps only when drawing to the canvas, so the low-res scene is tone mapped here.
  texel.rgb = toneMapping(texel.rgb);
  #endif
  texel = linearToOutputTexel(texel);
  vec2 pixel = floor(vUv * resolution);
  vec3 c = texel.rgb + (hash(pixel + frame) - 0.5) * grain;
  vec2 d = vUv - 0.5;
  c *= 1.0 - vignette * dot(d, d) * 2.0;
  float steps = levels - 1.0;
  c = floor(c * steps + bayer4(pixel)) / steps;
  gl_FragColor = vec4(clamp(c, 0.0, 1.0), 1.0);
}`;

/** Draws a scene at low resolution, then dithers, grains and vignettes it onto the canvas in one pass. */
export class RetroPass {
  readonly target: THREE.WebGLRenderTarget;
  readonly material: THREE.ShaderMaterial;
  readonly geometry = new THREE.PlaneGeometry(2, 2);
  private readonly quad: THREE.Mesh;
  private readonly camera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);
  private frame = 0;

  constructor(width: number, height: number) {
    this.target = new THREE.WebGLRenderTarget(1, 1, {
      minFilter: THREE.NearestFilter,
      magFilter: THREE.NearestFilter,
      colorSpace: THREE.SRGBColorSpace,
      depthBuffer: true,
    });
    this.material = new THREE.ShaderMaterial({
      uniforms: {
        tScene: { value: this.target.texture },
        resolution: { value: new THREE.Vector2(1, 1) },
        levels: { value: RETRO_COLOR_LEVELS },
        grain: { value: RETRO_GRAIN },
        vignette: { value: RETRO_VIGNETTE },
        frame: { value: 0 },
      },
      vertexShader,
      fragmentShader,
      depthTest: false,
      depthWrite: false,
    });
    this.quad = new THREE.Mesh(this.geometry, this.material);
    this.quad.frustumCulled = false;
    this.setSize(width, height);
  }

  /** Size of the canvas's drawing buffer in pixels; the target follows at RETRO_SCALE. */
  setSize(width: number, height: number): void {
    const w = Math.max(1, Math.round(width * RETRO_SCALE));
    const h = Math.max(1, Math.round(height * RETRO_SCALE));
    this.target.setSize(w, h);
    (this.material.uniforms.resolution.value as THREE.Vector2).set(w, h);
  }

  render(renderer: THREE.WebGLRenderer, scene: THREE.Scene, camera: THREE.Camera): void {
    renderer.setRenderTarget(this.target);
    renderer.render(scene, camera);
    renderer.setRenderTarget(null);
    this.frame = (this.frame + 1) % 1024;
    this.material.uniforms.frame.value = this.frame;
    renderer.render(this.quad, this.camera);
  }

  dispose(): void {
    this.target.dispose();
    this.material.dispose();
    this.geometry.dispose();
  }
}
