import { describe, expect, it } from 'vitest';
import type { ServerMessage } from '../net/codec';
import { CHAT_LOG_LINES, OVERHEAD_CHAT_MS } from './chat';
import { GameState } from './world';

const chat = (speakerId: number, text: string): ServerMessage => ({ type: 'chat', speakerId, text });
const welcome: ServerMessage = { type: 'welcome', playerId: 1, guestToken: 't', spawn: { x: 0, z: 0 }, tickRate: 20 };

describe('chat state', () => {
  it('keeps a chat log oldest first, capped at CHAT_LOG_LINES (AC1)', () => {
    expect(CHAT_LOG_LINES).toBe(50);
    const g = new GameState();
    g.handle(chat(3, 'hi'), 0);
    g.handle(chat(4, 'hello'), 1);
    expect(g.chat.log).toEqual([{ speakerId: 3, text: 'hi' }, { speakerId: 4, text: 'hello' }]);
    for (let i = 0; i < CHAT_LOG_LINES + 5; i++) g.handle(chat(5, `m${i}`), 2 + i);
    expect(g.chat.log.length).toBe(CHAT_LOG_LINES);
    expect(g.chat.log[0]).toEqual({ speakerId: 5, text: 'm5' });
    expect(g.chat.log[CHAT_LOG_LINES - 1]).toEqual({ speakerId: 5, text: `m${CHAT_LOG_LINES + 4}` });
  });

  it('shows the latest overhead message per speaker until OVERHEAD_CHAT_MS passes (AC2)', () => {
    expect(OVERHEAD_CHAT_MS).toBe(5000);
    const g = new GameState();
    g.handle(chat(3, 'first'), 1000);
    g.handle(chat(4, 'other'), 1000);
    g.handle(chat(3, 'second'), 2000);
    expect(g.chat.overhead(3, 2500)?.text).toBe('second');
    expect(g.chat.activeOverheads(2500).map((o) => o.text).sort()).toEqual(['other', 'second']);
    // Speaker 4's message expired at 6000; speaker 3's newer one lasts until 7000.
    expect(g.chat.overhead(4, 1000 + OVERHEAD_CHAT_MS)).toBeNull();
    expect(g.chat.overhead(3, 2000 + OVERHEAD_CHAT_MS - 1)?.text).toBe('second');
    expect(g.chat.overhead(3, 2000 + OVERHEAD_CHAT_MS)).toBeNull();
    expect(g.chat.activeOverheads(2000 + OVERHEAD_CHAT_MS)).toEqual([]);
  });

  it('Welcome clears overhead messages but keeps the chat log (AC3)', () => {
    const g = new GameState();
    g.handle(chat(3, 'hi'), 0);
    g.handle(welcome, 10);
    expect(g.chat.overhead(3, 10)).toBeNull();
    expect(g.chat.activeOverheads(10)).toEqual([]);
    expect(g.chat.log).toEqual([{ speakerId: 3, text: 'hi' }]);
  });
});
