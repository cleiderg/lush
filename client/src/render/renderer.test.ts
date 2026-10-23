import { afterEach, describe, expect, it, vi } from 'vitest';

// No WebGL in node: stub the renderer, keep the rest of three.
vi.mock('three', async (importOriginal) => {
  const three = await importOriginal<typeof import('three')>();
  class FakeWebGLRenderer {
    setPixelRatio(): void {}
    setSize(): void {}
    render(): void {}
    dispose(): void {}
  }
  return { ...three, WebGLRenderer: FakeWebGLRenderer };
});

import { createRenderer } from './renderer';

interface FakeEl {
  id: string;
  textContent: string;
  style: { cssText: string; color: string };
  children: FakeEl[];
  appendChild(c: FakeEl): void;
  remove(): void;
}

describe('createRenderer (AC4)', () => {
  const g = globalThis as unknown as { document?: unknown; window?: unknown };
  const prev = { document: g.document, window: g.window };
  afterEach(() => {
    g.document = prev.document;
    g.window = prev.window;
  });

  it('shows the OpenStreetMap credit while the renderer exists', () => {
    const body: FakeEl[] = [];
    const mk = (): FakeEl => {
      const e: FakeEl = {
        id: '',
        textContent: '',
        style: { cssText: '', color: '' },
        children: [],
        appendChild(c) { this.children.push(c); },
        remove() { body.splice(body.indexOf(e), 1); },
      };
      return e;
    };
    g.document = { createElement: mk, body: { appendChild: (c: FakeEl) => body.push(c) } };
    g.window = { devicePixelRatio: 1 };
    const canvas = { clientWidth: 390, clientHeight: 844, addEventListener() {}, removeEventListener() {} };

    const r = createRenderer(canvas as unknown as HTMLCanvasElement);
    const credit = body.find((e) => e.id === 'osm-attribution');
    expect(credit?.children.some((c) => c.textContent === '© OpenStreetMap contributors')).toBe(true);

    r.dispose();
    expect(body.find((e) => e.id === 'osm-attribution')).toBeUndefined();
  });
});
