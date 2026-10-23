// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Controls } from './controls';

function setup(opts: { onInteract?: () => void; onWave?: () => void; onChatSubmit?: (m: string) => void } = {}) {
  const tray = document.createElement('div');
  tray.id = 'control-tray';
  document.body.append(tray);
  const controls = new Controls({ tray, getCameraYaw: () => 0, ...opts });
  return { tray, controls };
}

function button(tray: HTMLElement, label: string): HTMLButtonElement {
  const b = [...tray.querySelectorAll('button')].find((x) => x.textContent === label);
  if (!b) throw new Error(`no button ${label}`);
  return b;
}

function press(key: string): KeyboardEvent {
  const e = new KeyboardEvent('keydown', { key, cancelable: true });
  window.dispatchEvent(e);
  return e;
}

let controls: Controls | null = null;

beforeEach(() => {
  document.body.replaceChildren();
});

afterEach(() => {
  // Each Controls listens on window; close chat so stray instances stay inert.
  controls?.closeChat();
  controls = null;
});

describe('Controls walking mode', () => {
  it('shows the joystick and Chat, Interact, Wave, Run buttons', () => {
    const onInteract = vi.fn();
    const onWave = vi.fn();
    const s = setup({ onInteract, onWave });
    controls = s.controls;
    expect(s.tray.querySelector('.lush-stick')).not.toBeNull();
    const labels = [...s.tray.querySelectorAll('.lush-actions button')].map((b) => b.textContent);
    expect(labels).toEqual(['Chat', 'Interact', 'Wave', 'Run']);
    button(s.tray, 'Interact').click();
    button(s.tray, 'Wave').click();
    expect(onInteract).toHaveBeenCalledOnce();
    expect(onWave).toHaveBeenCalledOnce();
  });

  it('Run is a toggle', () => {
    const s = setup();
    controls = s.controls;
    const run = button(s.tray, 'Run');
    run.click();
    expect(s.controls.isRunning).toBe(true);
    expect(run.getAttribute('aria-pressed')).toBe('true');
    run.click();
    expect(s.controls.isRunning).toBe(false);
  });

  it('WASD and arrows move; Enter opens chat', () => {
    const s = setup();
    controls = s.controls;
    expect(press('w').defaultPrevented).toBe(true);
    expect(s.controls.worldMove().y).toBeCloseTo(-1);
    window.dispatchEvent(new KeyboardEvent('keyup', { key: 'w' }));
    press('ArrowRight');
    expect(s.controls.worldMove().x).toBeCloseTo(1);
    press('Enter');
    expect(s.controls.isChatOpen).toBe(true);
    expect(s.controls.worldMove()).toEqual({ x: 0, y: 0 });
  });
});

describe('Controls chat mode', () => {
  it('replaces the tray with field, Send, close and keyboard', () => {
    const s = setup();
    controls = s.controls;
    button(s.tray, 'Chat').click();
    expect(s.tray.querySelector('.lush-stick')).toBeNull();
    expect(s.tray.querySelector('.lush-actions')).toBeNull();
    expect(s.tray.querySelector('.lush-field')).not.toBeNull();
    expect(button(s.tray, 'Send')).toBeTruthy();
    expect(s.tray.querySelector('[aria-label="Close chat"]')).not.toBeNull();
    expect(button(s.tray, '123')).toBeTruthy();
    (s.tray.querySelector('[aria-label="Close chat"]') as HTMLButtonElement).click();
    expect(s.controls.isChatOpen).toBe(false);
    expect(s.tray.querySelector('.lush-stick')).not.toBeNull();
  });

  it('on-screen keys type, auto-capitalize, and Send submits', () => {
    const onChatSubmit = vi.fn();
    const s = setup({ onChatSubmit });
    controls = s.controls;
    button(s.tray, 'Chat').click();
    button(s.tray, 'H').click();
    button(s.tray, 'i').click();
    button(s.tray, '123').click();
    button(s.tray, '1').click();
    expect(s.tray.querySelector('.lush-field')?.textContent).toBe('Hi1');
    button(s.tray, 'Send').click();
    expect(onChatSubmit).toHaveBeenCalledWith('Hi1');
    expect(s.controls.isChatOpen).toBe(false);
  });

  it('physical keyboard types into chat, Enter sends, movement keys do not move', () => {
    const onChatSubmit = vi.fn();
    const s = setup({ onChatSubmit });
    controls = s.controls;
    press('Enter');
    for (const k of 'wasd 2') press(k);
    press('Backspace');
    expect(s.controls.worldMove()).toEqual({ x: 0, y: 0 });
    expect(s.tray.querySelector('.lush-field')?.textContent).toBe('Wasd ');
    press('Enter');
    expect(onChatSubmit).toHaveBeenCalledWith('Wasd');
    expect(s.controls.isChatOpen).toBe(false);
  });

  it('Escape closes chat without sending', () => {
    const onChatSubmit = vi.fn();
    const s = setup({ onChatSubmit });
    controls = s.controls;
    press('Enter');
    press('h');
    press('Escape');
    expect(s.controls.isChatOpen).toBe(false);
    expect(onChatSubmit).not.toHaveBeenCalled();
  });
});
