// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { QualityChooser, type QualityChoice, type QualityPreset, type QualityStore } from '../core/quality';
import { QualityController } from '../core/qualityController';
import { hudLayout, intersects } from './hudLayout';
import { QualityControl } from './qualityControl';
import { RetroToggle, type RetroStore } from './retroToggle';

let control: QualityControl | null = null;

beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  control?.dispose();
  control = null;
});

function setup(initial: QualityChoice = 'auto') {
  const stored: QualityChoice[] = [];
  let value = initial;
  const store: QualityStore = { get: () => value, set: (c) => { value = c; stored.push(c); } };
  const setQuality = vi.fn<(p: QualityPreset) => void>();
  const model = new QualityController({ setQuality }, store, new QualityChooser('medium'));
  control = new QualityControl({ parent: document.body, model, viewport: () => ({ width: 390, height: 844 }) });
  return { model, stored, setQuality, button: control.button };
}

describe('QualityControl', () => {
  it('cycles Auto → Low → Medium → High → Auto, stores each choice and shows it', () => {
    const { button, stored, setQuality } = setup();
    expect(button.textContent).toContain('Auto: Medium');
    const seen: string[] = [];
    for (let i = 0; i < 4; i++) {
      button.click();
      seen.push(button.dataset.label!);
    }
    expect(seen).toEqual(['Low', 'Medium', 'High', 'Auto: Medium']);
    expect(stored).toEqual(['low', 'medium', 'high', 'auto']);
    expect(setQuality.mock.calls.map((c) => c[0])).toEqual(['medium', 'low', 'medium', 'high', 'medium']);
  });

  it('is themed with the shared button class', () => {
    const { button } = setup();
    expect(button.classList.contains('lush-button')).toBe(true);
    button.dispatchEvent(new Event('pointerdown'));
    expect(button.classList.contains('pressed')).toBe(true);
    button.dispatchEvent(new Event('pointerup'));
    expect(button.classList.contains('pressed')).toBe(false);
  });

  it('Retro keeps working on every preset', () => {
    const { button, model } = setup();
    const setRetro = vi.fn<(on: boolean) => void>();
    let saved = false;
    const store: RetroStore = { get: () => saved, set: (v) => { saved = v; } };
    const retro = new RetroToggle({ parent: document.body, target: { setRetro }, store, viewport: () => ({ width: 390, height: 844 }) });
    for (let i = 0; i < 4; i++) {
      button.click();
      setRetro.mockClear();
      retro.button.click();
      expect(setRetro).toHaveBeenCalledWith(!saved ? false : true);
      expect(retro.enabled).toBe(saved);
      expect(model.choice).toBeDefined();
    }
    retro.dispose();
  });
});

describe('hudLayout quality', () => {
  for (const [w, h] of [[390, 844], [1600, 1000]]) {
    it(`at ${w}×${h} is in the top bar, clear of the controls and other top-bar elements`, () => {
      const l = hudLayout(w, h);
      expect(l.quality.y).toBe(l.toggle.y);
      expect(l.quality.w).toBeGreaterThan(0);
      for (const name of ['location', 'hud', 'toggle', 'retro', 'controls', 'portrait', 'interact', 'minimap'] as const) {
        expect(intersects(l.quality, l[name]), name).toBe(false);
      }
    });
  }
});
