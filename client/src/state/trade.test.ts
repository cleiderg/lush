import { describe, expect, it } from 'vitest';
import type { ServerMessage } from '../net/codec';
import { TradeResultCode } from '../protocol';
import { GameState } from './world';

const welcome: ServerMessage = { type: 'welcome', playerId: 1, guestToken: 't', spawn: { x: 0, z: 0 }, tickRate: 20 };

const view: ServerMessage = {
  type: 'tradeState',
  partnerId: 7,
  screen: 1,
  myItems: [{ itemId: 3, quantity: 2 }],
  myGold: 100,
  partnerItems: [{ itemId: 9, quantity: 1 }, { itemId: 4, quantity: 50 }],
  partnerGold: 5,
  myAccepted: true,
  partnerAccepted: false,
};

describe('trade state', () => {
  it('records a pending request from the entity id in TradeRequestNotice', () => {
    const g = new GameState();
    g.handle(welcome, 0);
    let calls = 0;
    g.trade.subscribe(() => calls++);
    g.handle({ type: 'tradeRequestNotice', fromId: 7 }, 0);
    expect(g.trade.pendingRequest?.fromId).toBe(7);
    const first = g.trade.pendingRequest!.seq;
    g.handle({ type: 'tradeRequestNotice', fromId: 7 }, 0);
    expect(g.trade.pendingRequest!.seq).toBeGreaterThan(first);
    expect(calls).toBe(2);
    expect(g.trade.open).toBeNull();
  });

  it('TradeState replaces the open trade view', () => {
    const g = new GameState();
    g.handle(welcome, 0);
    g.handle({ type: 'tradeRequestNotice', fromId: 7 }, 0);
    g.handle(view, 0);
    expect(g.trade.open).toEqual({
      partnerId: 7, screen: 1, myItems: [{ itemId: 3, quantity: 2 }], myGold: 100,
      partnerItems: [{ itemId: 9, quantity: 1 }, { itemId: 4, quantity: 50 }], partnerGold: 5,
      myAccepted: true, partnerAccepted: false,
    });
    expect(g.trade.pendingRequest).toBeNull();

    g.handle({
      type: 'tradeState', partnerId: 7, screen: 2, myItems: [], myGold: 0, partnerItems: [{ itemId: 4, quantity: 1 }],
      partnerGold: 40, myAccepted: false, partnerAccepted: true,
    }, 0);
    expect(g.trade.open).toEqual({
      partnerId: 7, screen: 2, myItems: [], myGold: 0, partnerItems: [{ itemId: 4, quantity: 1 }], partnerGold: 40,
      myAccepted: false, partnerAccepted: true,
    });
  });

  it('TradeResult closes the trade and records the code', () => {
    const g = new GameState();
    g.handle(welcome, 0);
    g.handle(view, 0);
    g.handle({ type: 'tradeResult', code: TradeResultCode.Completed }, 0);
    expect(g.trade.open).toBeNull();
    expect(g.trade.result?.code).toBe(TradeResultCode.Completed);
    const first = g.trade.result!.seq;
    g.handle({ type: 'tradeResult', code: TradeResultCode.Completed }, 0);
    expect(g.trade.result!.seq).toBeGreaterThan(first);
  });

  it('is cleared on a new Welcome', () => {
    const g = new GameState();
    g.handle(welcome, 0);
    g.handle({ type: 'tradeResult', code: TradeResultCode.TooFar }, 0);
    g.handle({ type: 'tradeRequestNotice', fromId: 3 }, 0);
    g.handle(view, 0);
    g.handle({ type: 'tradeRequestNotice', fromId: 5 }, 0);
    let calls = 0;
    g.trade.subscribe(() => calls++);
    g.handle(welcome, 0);
    expect(g.trade.pendingRequest).toBeNull();
    expect(g.trade.open).toBeNull();
    expect(g.trade.result).toBeNull();
    expect(calls).toBe(1);
  });
});
