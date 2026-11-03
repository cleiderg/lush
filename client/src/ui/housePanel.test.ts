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

function housingBytes(houses: { id: number; mine?: boolean; state: HouseState; paidUntil?: number }[]): Uint8Array {
  const b = new flatbuffers.Builder(256);
  const offs = houses.map((h) =>
    HouseInfo.createHouseInfo(b, h.id, 1, b.createString(h.mine ? 'Me' : ''), !!h.mine, BigInt(h.paidUntil ?? 0), h.state, true));
  const msg = HousingState.createHousingState(b, HousingState.createHousesVector(b, offs), 200, 2, 6);
  b.finish(Envelope.createEnvelope(b, Message.HousingState, msg));
  return b.asUint8Array().slice();
}

function setup(houses: Parameters<typeof housingBytes>[0]) {
  const game = new GameState();
  game.handle(decode(housingBytes(houses))!, 0);
  const send = vi.fn<(bytes: Uint8Array) => void>();
  const panel = new HousePanel({ parent: document.body, housing: game.housing, send, now: () => NOW, viewport: () => ({ width: 390, height: 844 }) });
  current = panel;
  return { game, send, panel };
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
    // 2 months and a bit prepaid: 4 more allowed.
    const { send, panel } = setup([{ id: 7, mine: true, state: HouseState.Owned, paidUntil: NOW + 2 * MONTH + 100 }]);
    panel.open(7);
    expect(q(panel, '.lush-house-paid')!.textContent).toMatch(/^Paid until \w{3} \d+, \d{4}$/);
    for (let i = 0; i < 10; i++) q<HTMLButtonElement>(panel, '.lush-house-plus')!.click();
    expect(q(panel, '.lush-house-months')!.textContent).toBe('4 months');
    expect(q<HTMLButtonElement>(panel, '.lush-house-plus')!.disabled).toBe(true);
    q<HTMLButtonElement>(panel, '.lush-house-rent-pay')!.click();
    const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(send.mock.calls[0][0]));
    expect(env.msgType()).toBe(Message.RentPay);
    const m = env.msg(new RentPay()) as RentPay;
    expect([m.houseId(), m.months()]).toEqual([7, 4]);
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
});
