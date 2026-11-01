import { describe, expect, it } from 'vitest';
import type { ServerMessage } from '../net/codec';
import { BankResultCode } from '../protocol';
import { GameState } from './world';

const welcome: ServerMessage = { type: 'welcome', playerId: 1, guestToken: 't', spawn: { x: 0, z: 0 }, tickRate: 20 };
const slots = [{ itemId: 9, quantity: 3 }, { itemId: 2, quantity: 1 }, { itemId: 5, quantity: 70000 }];
const state: ServerMessage = { type: 'bankState', slots };

function opened(): GameState {
  const g = new GameState();
  g.handle(welcome, 0);
  g.handle({ type: 'bankResult', code: BankResultCode.Ok }, 0);
  g.handle(state, 0);
  return g;
}

describe('bank state', () => {
  it('is closed until a BankState, then open with the slots in server order', () => {
    const g = new GameState();
    g.handle(welcome, 0);
    expect(g.bank.isOpen).toBe(false);
    let calls = 0;
    g.bank.subscribe(() => calls++);
    g.handle(state, 0);
    expect(g.bank.isOpen).toBe(true);
    expect(g.bank.slots).toEqual(slots);
    expect(calls).toBe(1);

    g.handle({ type: 'bankState', slots: [{ itemId: 5, quantity: 1 }] }, 0);
    expect(g.bank.slots).toEqual([{ itemId: 5, quantity: 1 }]);
  });

  it('is closed after the client sends BankClose', () => {
    const g = opened();
    g.bank.close();
    expect(g.bank.isOpen).toBe(false);
    expect(g.bank.slots).toBeNull();
  });

  it('is closed after a BankResult the server closes the bank for', () => {
    for (const code of [BankResultCode.TooFar, BankResultCode.InCombat, BankResultCode.InPvpZone, BankResultCode.NotOpen]) {
      const g = opened();
      g.handle({ type: 'bankResult', code }, 0);
      expect(g.bank.isOpen, BankResultCode[code]).toBe(false);
    }
  });

  it('stays open after other BankResult codes', () => {
    for (const code of [BankResultCode.Ok, BankResultCode.BankFull, BankResultCode.InventoryFull, BankResultCode.NotEnoughItems, BankResultCode.Busy, BankResultCode.SaveFailed]) {
      const g = opened();
      g.handle({ type: 'bankResult', code }, 0);
      expect(g.bank.isOpen, BankResultCode[code]).toBe(true);
      expect(g.bank.slots).toEqual(slots);
    }
  });

  it('keeps the last BankResult code for the UI', () => {
    const g = opened();
    expect(g.bank.result?.code).toBe(BankResultCode.Ok);
    g.handle({ type: 'bankResult', code: BankResultCode.InventoryFull }, 0);
    expect(g.bank.result?.code).toBe(BankResultCode.InventoryFull);
    const first = g.bank.result!.seq;
    g.handle({ type: 'bankResult', code: BankResultCode.InventoryFull }, 0);
    expect(g.bank.result!.seq).toBeGreaterThan(first);
    g.bank.close();
    expect(g.bank.result?.code).toBe(BankResultCode.InventoryFull);
  });

  it('is cleared on a new Welcome', () => {
    const g = opened();
    g.handle(welcome, 0);
    expect(g.bank.isOpen).toBe(false);
    expect(g.bank.result).toBeNull();
  });
});
