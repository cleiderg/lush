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
    expect(BLOOM_SCALE).toEqual({ high: 1, medium: 0.5, low: 0 });
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
    expect(bloom.resolution.toArray()).toEqual([390 * 2 * BLOOM_SCALE.high, 844 * 2 * BLOOM_SCALE.high]);
    // UnrealBloomPass's brightest mip starts at half its resolution.
    expect(bright()).toEqual([Math.round(390 * BLOOM_SCALE.high), Math.round(844 * BLOOM_SCALE.high)]);

    canvas.clientWidth = 800;
    canvas.clientHeight = 600;
    r.resize();
    expect(bloom.resolution.toArray()).toEqual([1600 * BLOOM_SCALE.high, 1200 * BLOOM_SCALE.high]);
    expect(bright()).toEqual([800 * BLOOM_SCALE.high, 600 * BLOOM_SCALE.high]);
    expect([r.post!.composer.renderTarget1.width, r.post!.composer.renderTarget1.height]).toEqual([1600, 1200]);

    // A removed bloom pass keeps following the canvas, so restoring it needs no resize.
    r.setBloom(false);
    canvas.clientWidth = 400;
    canvas.clientHeight = 300;
    r.resize();
    r.setBloom(true);
    expect(bloom.resolution.toArray()).toEqual([800 * BLOOM_SCALE.high, 600 * BLOOM_SCALE.high]);
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
    expect(post.bloomPass.resolution.toArray()).toEqual([w * BLOOM_SCALE.high, h * BLOOM_SCALE.high]);
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
  describe('bloom per quality preset', () => {
    const bloomTargets = (post: PostChain) => {
      const b = post.bloomPass;
      return [b.renderTargetBright, ...b.renderTargetsHorizontal, ...b.renderTargetsVertical];
    };
    const frame = [390 * 2, 844 * 2];

    it('High and Medium size the bloom at that preset\'s share of the canvas (AC1)', () => {
      const post = r.post!;
      const bright = () => [post.bloomPass.renderTargetBright.width, post.bloomPass.renderTargetBright.height];
      const share = (s: number) => frame.map((n) => Math.round(Math.round(n * s) / 2));
      r.setQuality('high');
      expect(post.bloomPass.resolution.toArray()).toEqual(frame.map((n) => n * BLOOM_SCALE.high));
      expect(bright()).toEqual(share(BLOOM_SCALE.high));
      r.setQuality('medium');
      expect(post.bloomPass.resolution.toArray()).toEqual(frame.map((n) => n * BLOOM_SCALE.medium));
      expect(bright()).toEqual(share(BLOOM_SCALE.medium));
      r.setQuality('high');
      expect(bright()).toEqual(share(BLOOM_SCALE.high));
    });

    it('Low disables bloom; grade and vignette stay on with the same uniforms as High (AC2)', () => {
      const post = r.post!;
      const uniforms = () => [
        post.gradePass.enabled, post.vignettePass.enabled,
        (post.gradePass.uniforms.tint.value as T.Vector3).toArray(), post.vignettePass.uniforms.vignette.value,
      ];
      const high = uniforms();
      r.setQuality('low');
      expect(r.bloom).toBe(false);
      expect(post.composer.passes).not.toContain(post.bloomPass);
      expect(uniforms()).toEqual(high);
      expect(high.slice(0, 2)).toEqual([true, true]);
      expect(high[2]).toEqual([...gradeTint(GRADE_WARMTH)]);
      expect(high[3]).toBe(LOOK_VIGNETTE);
      r.setQuality('medium');
      expect(r.bloom).toBe(true);
      expect(uniforms()).toEqual(high);
    });

    it('High → Low → Medium → High disposes every bloom target it replaces and keeps the target count (AC3)', () => {
      const post = r.post!;
      const targets = bloomTargets(post);
      const count = () => bloomTargets(post).length + 2;
      const start = count();
      const spies = targets.map((t) => vi.spyOn(t, 'dispose'));
      const disposed = () => spies.map((s) => s.mock.calls.length);
      r.setQuality('low');
      expect(disposed().every((n) => n === 0)).toBe(true);
      r.setQuality('medium');
      expect(disposed().every((n) => n >= 1)).toBe(true);
      const afterMedium = disposed();
      r.setQuality('high');
      expect(disposed().every((n, i) => n > afterMedium[i])).toBe(true);
      expect(count()).toBe(start);
      expect(bloomTargets(post)).toEqual(targets);
    });

    it('draws the retro pass on top on every preset (AC4)', () => {
      const post = r.post!;
      r.setRetro(true);
      for (const p of ['high', 'low', 'medium'] as const) {
        r.setQuality(p);
        const order = trace(post);
        r.render(view);
        expect(order).toEqual(p === 'low' ? ['scene', 'grade', 'vignette', 'retro'] : ['scene', 'bloom', 'grade', 'vignette', 'retro']);
        vi.restoreAllMocks();
      }
    });
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
