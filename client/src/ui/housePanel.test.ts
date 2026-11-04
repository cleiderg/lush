// @vitest-environment happy-dom
import * as flatbuffers from 'flatbuffers';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { decode } from '../net/codec';
import { Envelope, HouseClaim, HouseInfo, HouseState, HousingState, Message, RentPay } from '../protocol';
import { RENT_MONTH_SECONDS, RENT_WARNING_LOGIN } from '../state/housing';
import { GameState } from '../state/world';
import { HousePanel } from './housePanel';

const NOW = 1_800_000_000;
const MONTH = RENT_MONTH_SECONDS;

function housingBytes(houses: { id: number; mine?: boolean; state: HouseState; paidUntil?: number; open?: boolean; topBid?: number; auctionEnd?: number; myBid?: number }[]): Uint8Array {
  const b = new flatbuffers.Builder(256);
  const offs = houses.map((h) =>
    HouseInfo.createHouseInfo(b, h.id, 1, b.createString(h.mine ? 'Me' : ''), !!h.mine, BigInt(h.paidUntil ?? 0), h.state, h.open ?? true, h.topBid ?? 0, BigInt(h.auctionEnd ?? 0), h.myBid ?? 0));
  const msg = HousingState.createHousingState(b, HousingState.createHousesVector(b, offs), 200, 2, 6);
  b.finish(Envelope.createEnvelope(b, Message.HousingState, msg));
  return b.asUint8Array().slice();
}

function setup(houses: Parameters<typeof housingBytes>[0], sendBid = vi.fn<(id: number, amount: number) => void>()) {
  const game = new GameState();
  game.handle(decode(housingBytes(houses))!, 0);
  const send = vi.fn<(bytes: Uint8Array) => void>();
  const panel = new HousePanel({ parent: document.body, housing: game.housing, send, sendBid, sendHomeEnter: vi.fn(), now: () => NOW, viewport: () => ({ width: 390, height: 844 }) });
  current = panel;
  return { game, send, sendBid, panel };
}

const q = <T extends HTMLElement>(p: HousePanel, sel: string) => p.panel.querySelector<T>(sel);
let current: HousePanel | null = null;
afterEach(() => { current?.dispose(); current = null; });

describe('HousePanel', () => {
  it('decodes the rent terms into state', () => {
    const { game } = setup([{ id: 1, state: HouseState.Empty }]);
    expect(game.housing).toMatchObject({ rentPerMonth: 200, claimMonths: 2, maxPrepaidMonths: 6 });
  });

  it('an empty house offers Claim at claim_months x rent_per_month and tapping sends the claim', () => {
    const { send, panel } = setup([{ id: 5, state: HouseState.Empty }]);
    panel.open(5);
    const claim = q<HTMLButtonElement>(panel, '.lush-house-claim')!;
    expect(claim.textContent).toContain('400 gp');
    claim.click();
    expect(send).toHaveBeenCalledTimes(1);
    const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(send.mock.calls[0][0]));
    expect(env.msgType()).toBe(Message.HouseClaim);
    expect((env.msg(new HouseClaim()) as HouseClaim).houseId()).toBe(5);
  });

  it('the owner sees paid-until and Rent cannot go past max_prepaid_months in total', () => {
    // 2 months and a bit prepaid: 3 more allowed, since 4 would put paid-until past now + 6 months.
    const { send, panel } = setup([{ id: 7, mine: true, state: HouseState.Owned, paidUntil: NOW + 2 * MONTH + 100 }]);
    panel.open(7);
    expect(q(panel, '.lush-house-paid')!.textContent).toMatch(/^Paid until \w{3} \d+, \d{4}$/);
    for (let i = 0; i < 10; i++) q<HTMLButtonElement>(panel, '.lush-house-plus')!.click();
    expect(q(panel, '.lush-house-months')!.textContent).toBe('3 months');
    expect(q<HTMLButtonElement>(panel, '.lush-house-plus')!.disabled).toBe(true);
    q<HTMLButtonElement>(panel, '.lush-house-rent-pay')!.click();
    const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(send.mock.calls[0][0]));
    expect(env.msgType()).toBe(Message.RentPay);
    const m = env.msg(new RentPay()) as RentPay;
    expect([m.houseId(), m.months()]).toEqual([7, 3]);
  });

  it('exactly 2 months prepaid leaves room for 4', () => {
    const { panel } = setup([{ id: 7, mine: true, state: HouseState.Owned, paidUntil: NOW + 2 * MONTH }]);
    panel.open(7);
    for (let i = 0; i < 10; i++) q<HTMLButtonElement>(panel, '.lush-house-plus')!.click();
    expect(q(panel, '.lush-house-months')!.textContent).toBe('4 months');
  });

  it('a fully prepaid house cannot be paid further', () => {
    const { panel } = setup([{ id: 7, mine: true, state: HouseState.Owned, paidUntil: NOW + 6 * MONTH + 100 }]);
    panel.open(7);
    expect(q<HTMLButtonElement>(panel, '.lush-house-rent-pay')!.disabled).toBe(true);
  });

  it('the warning banner shows only in the Warning state', () => {
    const { game, panel } = setup([{ id: 7, mine: true, state: HouseState.Owned, paidUntil: NOW + 3 * MONTH }]);
    expect(panel.banner.hidden).toBe(true);
    game.handle(decode(housingBytes([{ id: 7, mine: true, state: HouseState.Warning, paidUntil: NOW + MONTH + 3 * 86400 }]))!, 0);
    expect(panel.banner.hidden).toBe(false);
    expect(panel.banner.textContent).toContain('rent is running low');
    game.handle(decode(housingBytes([{ id: 7, mine: true, state: HouseState.Owned, paidUntil: NOW + 3 * MONTH }]))!, 0);
    expect(panel.banner.hidden).toBe(true);
  });
});

describe('login rent warning', () => {
  const lines = (g: GameState) => g.chat.log.filter((l) => l.text === RENT_WARNING_LOGIN).length;

  it('the first HousingState with a Warning house adds one chat line', () => {
    const g = new GameState();
    g.handle(decode(housingBytes([{ id: 7, mine: true, state: HouseState.Warning, paidUntil: NOW }]))!, 0);
    expect(lines(g)).toBe(1);
    g.handle(decode(housingBytes([{ id: 7, mine: true, state: HouseState.Warning, paidUntil: NOW }]))!, 0);
    expect(lines(g)).toBe(1);
  });

  it('no line when the house is fine, or when a later update turns Warning', () => {
    const g = new GameState();
    g.handle(decode(housingBytes([{ id: 7, mine: true, state: HouseState.Owned, paidUntil: NOW }]))!, 0);
    g.handle(decode(housingBytes([{ id: 7, mine: true, state: HouseState.Warning, paidUntil: NOW }]))!, 0);
    expect(lines(g)).toBe(0);
  });

  describe('auction', () => {
    const auction = (extra: { topBid?: number; myBid?: number } = {}) =>
      setup([{ id: 9, state: HouseState.Auction, auctionEnd: NOW + 2 * 86400 + 3 * 3600, ...extra }]);

    it('shows top bid, time left and held bid, and no Claim', () => {
      const { panel } = auction({ topBid: 450, myBid: 300 });
      panel.open(9);
      expect(q(panel, '.lush-house-top-bid')!.textContent).toBe('Top bid: 450 gp');
      expect(q(panel, '.lush-house-time-left')!.textContent).toBe('Time left: 2d 3h');
      expect(q(panel, '.lush-house-my-bid')!.textContent).toBe('Your held bid: 300 gp');
      expect(q(panel, '.lush-house-claim')).toBeNull();
    });

    it('the lowest bid is rent_per_month with no bids and top bid + 1 otherwise, and Bid sends the amount', () => {
      const none = auction();
      none.panel.open(9);
      expect(q(none.panel, '.lush-house-bid-amount')!.textContent).toBe('200 gp');
      expect(q<HTMLButtonElement>(none.panel, '.lush-house-bid-minus')!.disabled).toBe(true);
      q<HTMLButtonElement>(none.panel, '.lush-house-bid-minus')!.click();
      expect(q(none.panel, '.lush-house-bid-amount')!.textContent).toBe('200 gp');
      q<HTMLButtonElement>(none.panel, '.lush-house-bid-plus')!.click();
      expect(q(none.panel, '.lush-house-bid-amount')!.textContent).toBe('220 gp');
      q<HTMLButtonElement>(none.panel, '.lush-house-bid')!.click();
      expect(none.sendBid).toHaveBeenCalledWith(9, 220);
      none.panel.dispose();

      const top = auction({ topBid: 450 });
      top.panel.open(9);
      expect(q(top.panel, '.lush-house-bid-amount')!.textContent).toBe('451 gp');
      q<HTMLButtonElement>(top.panel, '.lush-house-bid-minus')!.click();
      expect(q(top.panel, '.lush-house-bid-amount')!.textContent).toBe('451 gp');
      q<HTMLButtonElement>(top.panel, '.lush-house-bid')!.click();
      expect(top.sendBid).toHaveBeenCalledWith(9, 451);
    });

    it('notes the winner also pays claim_months x rent_per_month, from state', () => {
      const { panel } = auction();
      panel.open(9);
      expect(q(panel, '.lush-house-note')!.textContent).toContain('also pays 400 gp (2 months of rent)');
    });
  });
});

describe('HousePanel Enter and door prompt (AC2, AC3)', () => {
  function withEnter(houses: Parameters<typeof housingBytes>[0]) {
    const s = setup(houses);
    const enter = vi.fn<(id: number) => void>();
    s.panel.dispose();
    const panel = new HousePanel({ parent: document.body, housing: s.game.housing, send: s.send, sendBid: s.sendBid, sendHomeEnter: enter, now: () => NOW, viewport: () => ({ width: 390, height: 844 }) });
    current = panel;
    return { ...s, panel, enter };
  }

  it('Enter shows for own house, sends HomeEnter and closes', () => {
    const { panel, enter } = withEnter([{ id: 7, mine: true, state: HouseState.Owned, paidUntil: NOW + MONTH }]);
    panel.open(7);
    q<HTMLButtonElement>(panel, '.lush-house-enter')!.click();
    expect(enter).toHaveBeenCalledWith(7);
    expect(panel.isOpen).toBe(false);
  });

  it('Enter shows for a claimed open house but not an empty one', () => {
    const { panel } = withEnter([{ id: 3, state: HouseState.Owned }, { id: 4, state: HouseState.Empty }]);
    panel.open(3);
    expect(q(panel, '.lush-house-enter')).not.toBeNull();
    panel.open(4);
    expect(q(panel, '.lush-house-enter')).toBeNull();
  });

  it('Enter shows for own locked house but not for a locked house of another player', () => {
    const { panel } = withEnter([
      { id: 5, mine: true, state: HouseState.Owned, paidUntil: NOW + MONTH, open: false },
      { id: 6, state: HouseState.Owned, open: false },
    ]);
    panel.open(5);
    expect(q(panel, '.lush-house-enter')).not.toBeNull();
    panel.open(6);
    expect(q(panel, '.lush-house-enter')).toBeNull();
  });
});
