import { describe, expect, it } from 'vitest';
import { ChatKeyboardState, MAX_CHAT_BYTES, byteLength, truncateToBytes } from './chatKeyboard';

describe('ChatKeyboardState', () => {
  it('auto-capitalizes only the first letter', () => {
    const k = new ChatKeyboardState();
    k.type('h');
    k.type('i');
    expect(k.text).toBe('Hi');
  });
  it('re-capitalizes after clearing the field', () => {
    const k = new ChatKeyboardState();
    k.type('a');
    k.backspace();
    k.type('b');
    expect(k.text).toBe('B');
  });
  it('123 key switches pages and does not capitalize symbols', () => {
    const k = new ChatKeyboardState();
    k.toggleSymbols();
    expect(k.page).toBe('symbols');
    k.type('1');
    expect(k.text).toBe('1');
    k.toggleSymbols();
    expect(k.page).toBe('letters');
  });
  it('enforces the 120-byte limit', () => {
    const k = new ChatKeyboardState();
    for (let i = 0; i < 200; i++) k.type('a');
    expect(byteLength(k.text)).toBe(MAX_CHAT_BYTES);
    expect(k.type('b')).toBe(false);
    expect(k.bytesLeft).toBe(0);
  });
  it('counts bytes, not characters', () => {
    const k = new ChatKeyboardState();
    for (let i = 0; i < 60; i++) k.type('é');
    expect(k.text.length).toBe(60);
    expect(k.type('é')).toBe(false);
    expect(truncateToBytes('😀'.repeat(40))).toBe('😀'.repeat(30));
  });
  it('submit trims, returns null when empty, and resets', () => {
    const k = new ChatKeyboardState();
    k.type(' ');
    expect(k.submit()).toBeNull();
    k.type('h');
    k.type('i');
    expect(k.submit()).toBe('Hi');
    expect(k.text).toBe('');
    expect(k.shift).toBe(true);
  });
});
