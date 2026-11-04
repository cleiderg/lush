import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  Envelope, HomeState, HouseClaim, HouseInfo, HouseState, HousingResult, HousingResultCode, HousingState, Message,
  AuctionBid, AuctionResult, AuctionResultCode, PlacedItem, RentPay,
} from '../protocol';
import { GameState } from '../state/world';
import { auctionResultText, housingResultText } from '../state/housing';
import { decode, encodeAuctionBid, encodeHouseClaim, encodeRentPay } from './codec';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

function housingBytes(): Uint8Array {
  const b = new flatbuffers.Builder(256);
  const h1 = HouseInfo.createHouseInfo(b, 3, 1, b.createString('Peter'), true, 1795000000n, HouseState.Warning, true);
  const h2 = HouseInfo.createHouseInfo(b, 4);
  return finish(b, Message.HousingState, HousingState.createHousingState(b, HousingState.createHousesVector(b, [h1, h2])));
}

function homeBytes(wallpaper: number, items: number[]): Uint8Array {
  const b = new flatbuffers.Builder(256);
  const placed = items.map((id, i) => PlacedItem.createPlacedItem(b, i + 1, id, 2, 3, 1, 1, 0));
  const msg = HomeState.createHomeState(b, 3, b.createString('Peter'), wallpaper, 8, HomeState.createItemsVector(b, placed));
  return finish(b, Message.HomeState, msg);
}

function resultBytes(code: HousingResultCode): Uint8Array {
  const b = new flatbuffers.Builder(32);
  return finish(b, Message.HousingResult, HousingResult.createHousingResult(b, code));
}

describe('housing state', () => {
  it('HousingState replaces the house list and exposes my house and warning', () => {
    const w = new GameState();
    expect(w.housing.myHouse()).toBeNull();
    w.handle(decode(housingBytes())!, 0);
    expect(w.housing.houses).toHaveLength(2);
    expect(w.housing.houses[0]).toEqual({
      id: 3, areaId: 1, ownerName: 'Peter', mine: true, paidUntil: 1795000000, state: HouseState.Warning, open: true,
      topBid: 0, auctionEnd: 0, myBid: 0,
    });
    expect(w.housing.myHouse()?.id).toBe(3);
    expect(w.housing.inWarning()).toBe(true);
    const b = new flatbuffers.Builder(64);
    const h = HouseInfo.createHouseInfo(b, 9);
    w.handle(decode(finish(b, Message.HousingState, HousingState.createHousingState(b, HousingState.createHousesVector(b, [h]))))!, 0);
    expect(w.housing.houses.map((x) => x.id)).toEqual([9]);
    expect(w.housing.myHouse()).toBeNull();
    expect(w.housing.inWarning()).toBe(false);
  });

  it('HomeState replaces the current home', () => {
    const w = new GameState();
    w.handle(decode(homeBytes(5, [10, 11]))!, 0);
    expect(w.housing.home).toMatchObject({ houseId: 3, ownerName: 'Peter', wallpaperItemId: 5, floorItemId: 8 });
    expect(w.housing.home?.items).toEqual([
      { placedId: 1, itemId: 10, x: 2, y: 3, rotation: 1, layer: 1, parentPlacedId: 0, slot: 0, side: 0 },
      { placedId: 2, itemId: 11, x: 2, y: 3, rotation: 1, layer: 1, parentPlacedId: 0, slot: 0, side: 0 },
    ]);
    w.handle(decode(homeBytes(6, [12]))!, 0);
    expect(w.housing.home?.wallpaperItemId).toBe(6);
    expect(w.housing.home?.items.map((i) => i.itemId)).toEqual([12]);
  });
});

describe('housing senders', () => {
  it('encodes HouseClaim and RentPay', () => {
    const c = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(encodeHouseClaim(512)));
    expect(c.msgType()).toBe(Message.HouseClaim);
    expect((c.msg(new HouseClaim()) as HouseClaim).houseId()).toBe(512);
    const r = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(encodeRentPay(512, 4)));
    expect(r.msgType()).toBe(Message.RentPay);
    const m = r.msg(new RentPay()) as RentPay;
    expect([m.houseId(), m.months()]).toEqual([512, 4]);
  });
});

describe('housing results', () => {
  it('each code gives its own chat line', () => {
    const codes = [
      HousingResultCode.AlreadyOwned, HousingResultCode.OwnsAnother, HousingResultCode.NotEnoughGold,
      HousingResultCode.TooFar, HousingResultCode.TooManyMonths, HousingResultCode.NotOwner, HousingResultCode.SaveFailed,
    ];
    const lines = codes.map((c) => housingResultText(c));
    expect(new Set(lines).size).toBe(codes.length);
    expect(lines).toContain('You already own a house');
    expect(lines).toContain("You can't prepay that far ahead");
    expect(housingResultText(HousingResultCode.Ok)).toBeNull();
  });

  it('a HousingResult message lands in the chat log', () => {
    const w = new GameState();
    w.handle(decode(resultBytes(HousingResultCode.OwnsAnother))!, 0);
    expect(w.chat.log.at(-1)).toMatchObject({ text: 'You already own a house', system: true });
  });
});

describe('auctions', () => {
  it('HousingState carries top bid, end and my bid; no bids read 0', () => {
    const b = new flatbuffers.Builder(256);
    const h1 = HouseInfo.createHouseInfo(b, 3, 1, 0, false, 0n, HouseState.Auction, true, 900, 1795500000n, 800);
    const h2 = HouseInfo.createHouseInfo(b, 4, 1, 0, false, 0n, HouseState.Auction, true, 0, 1795600000n, 0);
    const bytes = finish(b, Message.HousingState, HousingState.createHousingState(b, HousingState.createHousesVector(b, [h1, h2])));
    const w = new GameState();
    w.handle(decode(bytes)!, 0);
    expect(w.housing.house(3)).toMatchObject({ state: HouseState.Auction, topBid: 900, auctionEnd: 1795500000, myBid: 800 });
    expect(w.housing.house(4)).toMatchObject({ state: HouseState.Auction, topBid: 0, auctionEnd: 1795600000, myBid: 0 });
  });

  it('encodes AuctionBid', () => {
    const e = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(encodeAuctionBid(512, 1500)));
    expect(e.msgType()).toBe(Message.AuctionBid);
    const m = e.msg(new AuctionBid()) as AuctionBid;
    expect([m.houseId(), m.amount()]).toEqual([512, 1500]);
  });

  it('every AuctionResult code has its own chat line', () => {
    const codes = [0, 1, 2, 3, 4, 5, 6, 7] as AuctionResultCode[];
    const lines = codes.map((c) => auctionResultText(c));
    expect(new Set(lines).size).toBe(codes.length);
    for (const c of codes) {
      const b = new flatbuffers.Builder(32);
      const bytes = finish(b, Message.AuctionResult, AuctionResult.createAuctionResult(b, c));
      const w = new GameState();
      w.handle(decode(bytes)!, 0);
      expect(w.chat.log.at(-1)).toMatchObject({ text: auctionResultText(c), system: true });
    }
  });
});
