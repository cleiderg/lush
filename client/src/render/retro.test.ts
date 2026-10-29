import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type * as THREE from 'three';

// No WebGL in node: a WebGLRenderer that records its calls; the rest of three is real.
const calls: { op: string; arg?: unknown; arg2?: unknown }[] = [];
vi.mock('three', async (importOriginal) => {
  const three = await importOriginal<typeof import('three')>();
  class FakeWebGLRenderer {
    setPixelRatio(): void {}
    setSize(w: number, h: number): void {
      calls.push({ op: 'setSize', arg: w, arg2: h });
    }
    setRenderTarget(t: unknown): void {
      calls.push({ op: 'setRenderTarget', arg: t });
    }
    render(scene: unknown, camera: unknown): void {
      calls.push({ op: 'render', arg: scene, arg2: camera });
    }
    dispose(): void {}
  }
  return { ...three, WebGLRenderer: FakeWebGLRenderer };
});

import * as T from 'three';
import { createRenderer } from './renderer';
import { RETRO_COLOR_LEVELS, RETRO_GRAIN, RETRO_SCALE, RETRO_VIGNETTE, RetroPass } from './retro';
import type { WorldView } from './worldView';

const view: WorldView = { local: { id: 1, x: 0, z: 0, heading: 0 }, remotes: [] };

describe('retro post-process', () => {
  const g = globalThis as unknown as { document?: unknown; window?: unknown };
  const prev = { document: g.document, window: g.window };
  let canvas: { clientWidth: number; clientHeight: number; addEventListener(): void; removeEventListener(): void };

  beforeEach(() => {
    calls.length = 0;
    const el = () => ({ style: { cssText: '' }, appendChild() {}, remove() {} });
    g.document = { createElement: el, body: { appendChild() {} } };
    g.window = { devicePixelRatio: 2 };
    canvas = { clientWidth: 390, clientHeight: 844, addEventListener() {}, removeEventListener() {} };
  });
  afterEach(() => {
    g.document = prev.document;
    g.window = prev.window;
    vi.restoreAllMocks();
  });

  it('uses the PRD 13 placeholder values (AC2)', () => {
    expect(RETRO_SCALE).toBe(0.5);
    expect(RETRO_COLOR_LEVELS).toBe(32);
    expect(RETRO_GRAIN).toBe(0.04);
    expect(RETRO_VIGNETTE).toBe(0.35);
  });

  it('starts off and draws the scene straight to the canvas (AC1)', () => {
    const made = vi.spyOn(RetroPass.prototype, 'setSize');
    const r = createRenderer(canvas as unknown as HTMLCanvasElement);
    expect(r.retro).toBe(false);
    calls.length = 0;
    r.render(view);
    expect(calls).toEqual([{ op: 'render', arg: r.world.scene, arg2: r.world.rig.camera }]);
    expect(made).not.toHaveBeenCalled();
    r.dispose();
  });

  it('renders into a low-resolution target, then one full-screen pass to the canvas (AC2)', () => {
    const r = createRenderer(canvas as unknown as HTMLCanvasElement);
    r.setRetro(true);
    expect(r.retro).toBe(true);
    calls.length = 0;
    r.render(view);
    expect(calls.map((c) => c.op)).toEqual(['setRenderTarget', 'render', 'setRenderTarget', 'render']);
    const target = calls[0].arg as THREE.WebGLRenderTarget;
    expect(target).toBeInstanceOf(T.WebGLRenderTarget);
    expect(target.width).toBe(390 * 2 * RETRO_SCALE);
    expect(target.height).toBe(844 * 2 * RETRO_SCALE);
    expect(target.texture.magFilter).toBe(T.NearestFilter);
    expect(calls[1]).toMatchObject({ arg: r.world.scene, arg2: r.world.rig.camera });
    expect(calls[2].arg).toBeNull();
    const quad = calls[3].arg as THREE.Mesh;
    expect(quad).toBeInstanceOf(T.Mesh);
    const mat = quad.material as THREE.ShaderMaterial;
    expect(mat).toBeInstanceOf(T.ShaderMaterial);
    expect(mat.uniforms.tScene.value).toBe(target.texture);
    expect(mat.uniforms.levels.value).toBe(RETRO_COLOR_LEVELS);
    expect(mat.uniforms.grain.value).toBe(RETRO_GRAIN);
    expect(mat.uniforms.vignette.value).toBe(RETRO_VIGNETTE);
    expect(mat.fragmentShader).toContain('bayer4');

    r.setRetro(false);
    expect(r.retro).toBe(false);
    calls.length = 0;
    r.render(view);
    expect(calls.map((c) => c.op)).toEqual(['render']);
    r.dispose();
  });

  it('resizes the low-resolution target with the canvas (AC3)', () => {
    const r = createRenderer(canvas as unknown as HTMLCanvasElement);
    r.setRetro(true);
    canvas.clientWidth = 800;
    canvas.clientHeight = 600;
    r.resize();
    calls.length = 0;
    r.render(view);
    const target = calls[0].arg as THREE.WebGLRenderTarget;
    expect([target.width, target.height]).toEqual([800, 600]);
    const mat = (calls[3].arg as THREE.Mesh).material as THREE.ShaderMaterial;
    expect((mat.uniforms.resolution.value as THREE.Vector2).toArray()).toEqual([800, 600]);
    r.dispose();
  });

  /** Turns retro on and returns spies on the pass's target, material and geometry. */
  function retroSpies(r: ReturnType<typeof createRenderer>) {
    r.setRetro(true);
    calls.length = 0;
    r.render(view);
    const quad = calls[3].arg as THREE.Mesh;
    return [
      vi.spyOn(calls[0].arg as THREE.WebGLRenderTarget, 'dispose'),
      vi.spyOn(quad.material as THREE.ShaderMaterial, 'dispose'),
      vi.spyOn(quad.geometry, 'dispose'),
    ];
  }

  it('dispose frees the target, material and geometry (AC3)', () => {
    const r = createRenderer(canvas as unknown as HTMLCanvasElement);
    const spies = retroSpies(r);
    r.dispose();
    for (const s of spies) expect(s).toHaveBeenCalledTimes(1);
  });

  it('turning retro off frees the pass too', () => {
    const r = createRenderer(canvas as unknown as HTMLCanvasElement);
    const spies = retroSpies(r);
    r.setRetro(false);
    for (const s of spies) expect(s).toHaveBeenCalledTimes(1);
    r.dispose();
    for (const s of spies) expect(s).toHaveBeenCalledTimes(1);
  });
});
