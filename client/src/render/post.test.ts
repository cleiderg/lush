import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type * as THREE from 'three';

// No WebGL in node: a WebGLRenderer with half-float support that records its draws; the rest of three is real.
const calls: { op: string; arg?: unknown }[] = [];
vi.mock('three', async (importOriginal) => {
  const three = await importOriginal<typeof import('three')>();
  class FakeWebGLRenderer {
    shadowMap = { enabled: false, type: 0 };
    extensions = { has: (name: string) => name === 'EXT_color_buffer_float' };
    private target: unknown = null;
    setPixelRatio(): void {}
    getPixelRatio(): number {
      return 2;
    }
    setSize(): void {}
    getRenderTarget(): unknown {
      return this.target;
    }
    setRenderTarget(t: unknown): void {
      this.target = t;
      calls.push({ op: 'setRenderTarget', arg: t });
    }
    render(scene: unknown): void {
      calls.push({ op: 'render', arg: scene });
    }
    dispose(): void {}
  }
  return { ...three, WebGLRenderer: FakeWebGLRenderer };
});

import * as T from 'three';
import { createRenderer, type GameRenderer } from './renderer';
import {
  BLOOM_RADIUS,
  BLOOM_SCALE,
  BLOOM_STRENGTH,
  BLOOM_THRESHOLD,
  GRADE_WARMTH,
  LOOK_VIGNETTE,
  gradeColor,
  gradeTint,
  type PostChain,
} from './post';
import { RETRO_SCALE } from './retro';
import type { WorldView } from './worldView';

const view: WorldView = { local: { id: 1, x: 0, z: 0, heading: 0 }, remotes: [] };

/** Replaces every pass's render with a recorder (except the retro pass), and returns the names run in order. */
function trace(post: PostChain): string[] {
  const order: string[] = [];
  const named: [string, { render: (...a: never[]) => void }][] = [
    ['scene', post.scenePass],
    ['bloom', post.bloomPass],
    ['grade', post.gradePass],
    ['vignette', post.vignettePass],
    ['output', post.outputPass],
  ];
  for (const [name, pass] of named) vi.spyOn(pass, 'render').mockImplementation(() => void order.push(name));
  const retro = post.retroPass;
  const real = retro.render.bind(retro);
  vi.spyOn(retro, 'render').mockImplementation((...a: Parameters<typeof retro.render>) => {
    order.push('retro');
    real(...a);
  });
  return order;
}

describe('post-process chain', () => {
  const g = globalThis as unknown as { document?: unknown; window?: unknown };
  const prev = { document: g.document, window: g.window };
  let canvas: { clientWidth: number; clientHeight: number; addEventListener(): void; removeEventListener(): void };
  let r: GameRenderer;

  beforeEach(() => {
    calls.length = 0;
    const el = () => ({ style: { cssText: '' }, appendChild() {}, remove() {} });
    g.document = { createElement: el, body: { appendChild() {} } };
    g.window = { devicePixelRatio: 2 };
    canvas = { clientWidth: 390, clientHeight: 844, addEventListener() {}, removeEventListener() {} };
    r = createRenderer(canvas as unknown as HTMLCanvasElement);
  });
  afterEach(() => {
    r.dispose();
    g.document = prev.document;
    g.window = prev.window;
    vi.restoreAllMocks();
  });

  it('uses the PRD 13 placeholder values', () => {
    expect(BLOOM_THRESHOLD).toBe(0.85);
    expect(BLOOM_STRENGTH).toBe(0.6);
    expect(BLOOM_RADIUS).toBe(0.4);
    expect(BLOOM_SCALE).toBe(0.5);
    expect(GRADE_WARMTH).toBe(0.05);
    expect(LOOK_VIGNETTE).toBe(0.25);
  });

  it('draws scene, bloom, warm grade, vignette, then output, with the placeholder constants (AC1)', () => {
    const post = r.post!;
    expect(post).not.toBeNull();
    expect(r.bloom).toBe(true);
    const order = trace(post);
    r.render(view);
    expect(order).toEqual(['scene', 'bloom', 'grade', 'vignette', 'output']);

    expect(post.scenePass.scene).toBe(r.world.scene);
    expect(post.scenePass.camera).toBe(r.world.rig.camera);
    expect(post.bloomPass.threshold).toBe(BLOOM_THRESHOLD);
    expect(post.bloomPass.strength).toBe(BLOOM_STRENGTH);
    expect(post.bloomPass.radius).toBe(BLOOM_RADIUS);
    expect((post.gradePass.uniforms.tint.value as T.Vector3).toArray()).toEqual([...gradeTint(GRADE_WARMTH)]);
    expect(post.vignettePass.uniforms.vignette.value).toBe(LOOK_VIGNETTE);
    // The HDR frame is kept in half-float buffers until the output pass tone maps it.
    expect(post.composer.renderTarget1.texture.type).toBe(T.HalfFloatType);
    expect(post.outputPass.renderToScreen).toBe(true);
  });

  it('setBloom(false) removes the bloom pass and setBloom(true) restores it (AC1)', () => {
    const order = trace(r.post!);
    r.setBloom(false);
    expect(r.bloom).toBe(false);
    expect(r.post!.composer.passes).not.toContain(r.post!.bloomPass);
    r.render(view);
    expect(order).toEqual(['scene', 'grade', 'vignette', 'output']);

    order.length = 0;
    r.setBloom(true);
    expect(r.bloom).toBe(true);
    r.render(view);
    expect(order).toEqual(['scene', 'bloom', 'grade', 'vignette', 'output']);
  });

  it('renders the bloom at BLOOM_SCALE of the canvas and resizes it with the canvas (AC2)', () => {
    const bloom = r.post!.bloomPass;
    const bright = () => [bloom.renderTargetBright.width, bloom.renderTargetBright.height];
    expect(bloom.resolution.toArray()).toEqual([390 * 2 * BLOOM_SCALE, 844 * 2 * BLOOM_SCALE]);
    // UnrealBloomPass's brightest mip starts at half its resolution.
    expect(bright()).toEqual([Math.round(390 * BLOOM_SCALE), Math.round(844 * BLOOM_SCALE)]);

    canvas.clientWidth = 800;
    canvas.clientHeight = 600;
    r.resize();
    expect(bloom.resolution.toArray()).toEqual([1600 * BLOOM_SCALE, 1200 * BLOOM_SCALE]);
    expect(bright()).toEqual([800 * BLOOM_SCALE, 600 * BLOOM_SCALE]);
    expect([r.post!.composer.renderTarget1.width, r.post!.composer.renderTarget1.height]).toEqual([1600, 1200]);

    // A removed bloom pass keeps following the canvas, so restoring it needs no resize.
    r.setBloom(false);
    canvas.clientWidth = 400;
    canvas.clientHeight = 300;
    r.resize();
    r.setBloom(true);
    expect(bloom.resolution.toArray()).toEqual([800 * BLOOM_SCALE, 600 * BLOOM_SCALE]);
  });

  it('with retro on, draws bloom, grade and vignette, then the retro pass, at the retro resolution (AC4)', () => {
    const post = r.post!;
    const order = trace(post);
    r.setRetro(true);
    calls.length = 0;
    r.render(view);
    expect(order).toEqual(['scene', 'bloom', 'grade', 'vignette', 'retro']);

    // The retro shader reads the graded frame and draws it to the canvas.
    const quad = calls.find((c) => c.op === 'render')!.arg as THREE.Mesh;
    const mat = quad.material as THREE.ShaderMaterial;
    expect(mat.fragmentShader).toContain('bayer4');
    expect(calls[0]).toEqual({ op: 'setRenderTarget', arg: null });
    const w = 390 * 2 * RETRO_SCALE;
    const h = 844 * 2 * RETRO_SCALE;
    expect([post.composer.renderTarget1.width, post.composer.renderTarget1.height]).toEqual([w, h]);
    expect(post.composer.renderTarget1.texture.magFilter).toBe(T.NearestFilter);
    expect(post.bloomPass.resolution.toArray()).toEqual([w * BLOOM_SCALE, h * BLOOM_SCALE]);
    expect((mat.uniforms.resolution.value as T.Vector2).toArray()).toEqual([w, h]);

    r.setRetro(false);
    order.length = 0;
    r.render(view);
    expect(order).toEqual(['scene', 'bloom', 'grade', 'vignette', 'output']);
    expect(post.composer.renderTarget1.width).toBe(390 * 2);
    expect(post.composer.renderTarget1.texture.magFilter).toBe(T.LinearFilter);
  });

  it('feeds the retro shader the frame from the end of the chain', () => {
    const post = r.post!;
    r.setRetro(true);
    const retro = post.retroPass.retro!;
    const input = new T.Texture();
    let seen: unknown;
    vi.spyOn(r.gl, 'render').mockImplementation((quad) => {
      seen = ((quad as THREE.Mesh).material as THREE.ShaderMaterial).uniforms.tScene.value;
    });
    post.retroPass.render(r.gl, post.composer.writeBuffer, { texture: input } as THREE.WebGLRenderTarget);
    expect(seen).toBe(input);
    // Its own low-resolution target is put back afterwards.
    expect(retro.material.uniforms.tScene.value).toBe(retro.target.texture);
  });
});

describe('warm grade', () => {
  const tint = gradeTint(GRADE_WARMTH);

  it('makes mid-grey warm: red ≥ green ≥ blue (AC3)', () => {
    for (const v of [0.18, 0.5]) {
      const [cr, cg, cb] = gradeColor([v, v, v], tint);
      expect(cr).toBeGreaterThanOrEqual(cg);
      expect(cg).toBeGreaterThanOrEqual(cb);
      expect(cr).toBeGreaterThan(cb);
    }
  });

  it('keeps white within 2% of white (AC3)', () => {
    for (const c of gradeColor([1, 1, 1], tint)) expect(Math.abs(c - 1)).toBeLessThanOrEqual(0.02);
  });

  it('feeds the same tint to the shader uniform', async () => {
    const { GradeShader } = await import('./post');
    expect((GradeShader.uniforms.tint.value as T.Vector3).toArray()).toEqual([...tint]);
    expect(GradeShader.fragmentShader).toContain('4.0 * luma * (1.0 - luma)');
  });
});
