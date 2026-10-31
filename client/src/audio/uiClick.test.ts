// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AudioManager } from './audioManager';
import { CLICK_SECONDS, installUiClicks, type ClickContext } from './uiClick';

/** A mocked AudioContext that records every oscillator it makes. */
function mockContext() {
  const param = () => ({ setValueAtTime: vi.fn(), exponentialRampToValueAtTime: vi.fn() });
  const oscillators: { type: string; start: ReturnType<typeof vi.fn>; stop: ReturnType<typeof vi.fn> }[] = [];
  const ctx = {
    state: 'suspended',
    currentTime: 2,
    destination: {},
    resume: vi.fn(() => {
      ctx.state = 'running';
      return Promise.resolve();
    }),
    createOscillator: vi.fn(() => {
      const o = { type: '', frequency: param(), connect: vi.fn(), start: vi.fn(), stop: vi.fn() };
      oscillators.push(o);
      return o;
    }),
    createGain: vi.fn(() => ({ gain: param(), connect: vi.fn() })),
  };
  return { ctx: ctx as typeof ctx & ClickContext, oscillators };
}

let cleanup: (() => void) | null = null;
afterEach(() => {
  cleanup?.();
  cleanup = null;
  document.body.replaceChildren();
});

function setup() {
  const { ctx, oscillators } = mockContext();
  const factory = vi.fn(() => ctx);
  const audio = new AudioManager<ClickContext>({ factory, window, document });
  cleanup = installUiClicks(document, audio, '.lush-button');
  const button = document.createElement('button');
  button.className = 'lush-button';
  const plain = document.createElement('div');
  document.body.append(button, plain);
  return { ctx, oscillators, factory, audio, button, plain };
}

describe('button click sound (#410 AC3)', () => {
  it('plays nothing before the audio unlock', () => {
    const { oscillators, factory, button } = setup();
    button.click();
    button.click();
    expect(factory).not.toHaveBeenCalled();
    expect(oscillators).toHaveLength(0);
  });

  it('after the unlock, pressing a button plays one short synthesized click', async () => {
    const { ctx, oscillators, audio, button, plain } = setup();
    // The first tap unlocks audio.
    window.dispatchEvent(new Event('touchend'));
    await Promise.resolve();
    expect(audio.isUnlocked()).toBe(true);
    expect(oscillators).toHaveLength(0);

    button.click();
    expect(oscillators).toHaveLength(1);
    expect(ctx.createGain).toHaveBeenCalledTimes(1);
    const [osc] = oscillators;
    expect(osc.start).toHaveBeenCalledTimes(1);
    expect(osc.stop).toHaveBeenCalledTimes(1);
    const start = osc.start.mock.calls[0][0] as number;
    const stop = osc.stop.mock.calls[0][0] as number;
    expect(stop - start).toBeCloseTo(CLICK_SECONDS);
    expect(stop - start).toBeLessThanOrEqual(0.1);

    // A click on something that isn't a button stays silent.
    plain.click();
    expect(oscillators).toHaveLength(1);
  });

  it('a click on a child of the button counts; a disabled button stays silent', async () => {
    const { oscillators, button } = setup();
    window.dispatchEvent(new Event('touchend'));
    await Promise.resolve();
    const label = document.createElement('span');
    button.append(label);
    label.click();
    expect(oscillators).toHaveLength(1);
    button.disabled = true;
    button.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(oscillators).toHaveLength(1);
  });

  it('the remover stops the clicks', async () => {
    const { oscillators, button } = setup();
    window.dispatchEvent(new Event('touchend'));
    await Promise.resolve();
    cleanup?.();
    cleanup = null;
    button.click();
    expect(oscillators).toHaveLength(0);
  });
});
