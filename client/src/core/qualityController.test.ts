import { describe, expect, it, vi } from 'vitest';
import { GameLoop } from './loop';
import { QUALITY_WARMUP_S, QualityChooser, type QualityChoice, type QualityPreset, type QualityStore } from './quality';
import { QualityController } from './qualityController';

function memStore(initial: QualityChoice = 'auto'): QualityStore & { value: QualityChoice } {
  const s = { value: initial, get: () => s.value, set: (c: QualityChoice) => { s.value = c; } };
  return s;
}

function setup(initial: QualityChoice = 'auto', start: QualityPreset = 'high') {
  const setQuality = vi.fn<(p: QualityPreset) => void>();
  const store = memStore(initial);
  const chooser = new QualityChooser(start);
  const controller = new QualityController({ setQuality }, store, chooser);
  return { setQuality, store, chooser, controller };
}

describe('QualityController', () => {
  it('applies Low, Medium and High at once and stores them', () => {
    const { setQuality, store, controller } = setup();
    for (const p of ['low', 'medium', 'high'] as const) {
      controller.setChoice(p);
      expect(setQuality).toHaveBeenLastCalledWith(p);
      expect(store.value).toBe(p);
    }
  });

  it('applies a stored choice at startup', () => {
    const { setQuality } = setup('low');
    expect(setQuality).toHaveBeenCalledWith('low');
  });

  it('Auto applies the chooser preset, including after it steps down', () => {
    const { setQuality, controller } = setup('auto', 'medium');
    expect(setQuality).toHaveBeenLastCalledWith('medium');
    controller.setChoice('low');
    controller.setChoice('auto');
    expect(setQuality).toHaveBeenLastCalledWith('medium');
    for (let t = 0; t < QUALITY_WARMUP_S + 8; t += 0.1) controller.frame(0.1);
    expect(controller.preset).toBe('low');
    expect(setQuality).toHaveBeenLastCalledWith('low');
  });

  it('a stored override ignores the chooser stepping down', () => {
    const { setQuality, controller } = setup('high', 'high');
    for (let t = 0; t < QUALITY_WARMUP_S + 20; t += 0.1) controller.frame(0.1);
    expect(setQuality).toHaveBeenLastCalledWith('high');
  });

  it('the game loop feeds each rendered frame time to the chooser', () => {
    const { controller, chooser } = setup('auto', 'high');
    const frame = vi.spyOn(chooser, 'frame');
    const retro = vi.fn<(on: boolean) => void>();
    const loop = new GameLoop({ update: () => {}, render: () => { controller.rendered(performance.now()); retro(true); } });
    const now = vi.spyOn(performance, 'now');
    let t = 0;
    for (let i = 0; i < 5; i++) {
      t += 40;
      now.mockReturnValue(t);
      loop.frame(t);
    }
    expect(frame).toHaveBeenCalledTimes(4);
    for (const c of frame.mock.calls) expect(c[0]).toBeCloseTo(0.04);
    now.mockRestore();
  });
});
