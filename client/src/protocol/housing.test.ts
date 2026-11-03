import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  AuctionBid, ShopBuy, ShopKind, WorkbenchCraft, Envelope, HomePlace, HomeResult, HomeResultCode, HomeState, WallSide, HouseClaim, HouseInfo, HouseState, HousingResult, HousingResultCode, HousingState,
  ItemCatalog, ItemDef, Message, PlacedItem, PlacementKind, RentPay,
} from './index.js';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array();
}

function open<T>(buf: Uint8Array, type: Message, m: T): T {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(buf));
  expect(env.msgType()).toBe(type);
  return env.msg(m as never) as T;
}

describe('housing protocol', () => {
  it('round-trips HousingState', () => {
    const b = new flatbuffers.Builder(256);
    const h1 = HouseInfo.createHouseInfo(b, 3, 1, b.createString('Peter'), true, 1795000000n, HouseState.Warning, true);
    const h2 = HouseInfo.createHouseInfo(b, 4);
    const buf = finish(b, Message.HousingState,
      HousingState.createHousingState(b, HousingState.createHousesVector(b, [h1, h2])));
    const m = open(buf, Message.HousingState, new HousingState());
    expect(m.housesLength()).toBe(2);
    const a = m.houses(0)!;
    expect([a.id(), a.areaId(), a.ownerName(), a.mine(), a.paidUntil(), a.state(), a.open()])
      .toEqual([3, 1, 'Peter', true, 1795000000n, HouseState.Warning, true]);
    const e = m.houses(1)!;
    expect([e.id(), e.ownerName(), e.mine(), e.paidUntil(), e.state(), e.open()])
      .toEqual([4, null, false, 0n, HouseState.Empty, false]);
  });

  it('round-trips HouseClaim and RentPay', () => {
    let b = new flatbuffers.Builder(32);
    let buf = finish(b, Message.HouseClaim, HouseClaim.createHouseClaim(b, 39));
    expect(open(buf, Message.HouseClaim, new HouseClaim()).houseId()).toBe(39);
    b = new flatbuffers.Builder(32);
    buf = finish(b, Message.RentPay, RentPay.createRentPay(b, 7, 6));
    const r = open(buf, Message.RentPay, new RentPay());
    expect([r.houseId(), r.months()]).toEqual([7, 6]);
  });

  it('round-trips HousingResult codes', () => {
    const codes = [
      HousingResultCode.Ok, HousingResultCode.AlreadyOwned, HousingResultCode.OwnsAnother,
      HousingResultCode.NotEnoughGold, HousingResultCode.TooFar, HousingResultCode.TooManyMonths,
      HousingResultCode.NotOwner, HousingResultCode.SaveFailed,
    ];
    codes.forEach((code, i) => {
      expect(code).toBe(i);
      const b = new flatbuffers.Builder(32);
      const buf = finish(b, Message.HousingResult, HousingResult.createHousingResult(b, code));
      expect(open(buf, Message.HousingResult, new HousingResult()).code()).toBe(code);
    });
  });

  it('round-trips HomeState', () => {
    const b = new flatbuffers.Builder(256);
    const p = PlacedItem.createPlacedItem(b, 100000, 205, 3, 7, 2, 1, 99999);
    const buf = finish(b, Message.HomeState, HomeState.createHomeState(
      b, 12, b.createString('Zot'), 300, 301, HomeState.createItemsVector(b, [p])));
    const m = open(buf, Message.HomeState, new HomeState());
    expect([m.houseId(), m.ownerName(), m.wallpaperItemId(), m.floorItemId(), m.itemsLength()])
      .toEqual([12, 'Zot', 300, 301, 1]);
    const it0 = m.items(0)!;
    expect([it0.placedId(), it0.itemId(), it0.x(), it0.y(), it0.rotation(), it0.layer(), it0.parentPlacedId()])
      .toEqual([100000, 205, 3, 7, 2, 1, 99999]);
  });

  it('round-trips ItemDef furniture fields and defaults to not furniture', () => {
    const b = new flatbuffers.Builder(128);
    ItemDef.startItemDef(b);
    ItemDef.addId(b, 205);
    ItemDef.addFootprintW(b, 2);
    ItemDef.addFootprintD(b, 1);
    ItemDef.addPlacement(b, PlacementKind.FloorOrSurface);
    ItemDef.addSurfaceSlots(b, 4);
    const d1 = ItemDef.endItemDef(b);
    ItemDef.startItemDef(b);
    ItemDef.addId(b, 1);
    const d2 = ItemDef.endItemDef(b);
    const buf = finish(b, Message.ItemCatalog,
      ItemCatalog.createItemCatalog(b, ItemCatalog.createItemsVector(b, [d1, d2])));
    const m = open(buf, Message.ItemCatalog, new ItemCatalog());
    const a = m.items(0)!;
    expect([a.footprintW(), a.footprintD(), a.placement(), a.surfaceSlots()]).toEqual([2, 1, PlacementKind.FloorOrSurface, 4]);
    const n = m.items(1)!;
    expect([n.footprintW(), n.footprintD(), n.placement(), n.surfaceSlots()]).toEqual([0, 0, PlacementKind.None, 0]);
  });

  it('round-trips HomePlace and HomeResult', () => {
    let b = new flatbuffers.Builder(64);
    let buf = finish(b, Message.HomePlace, HomePlace.createHomePlace(b, 5, 3, 4, 2, 77, 1, WallSide.West));
    const p = open(buf, Message.HomePlace, new HomePlace());
    expect([p.inventorySlot(), p.x(), p.y(), p.rotation(), p.parentPlacedId(), p.slot(), p.side()])
      .toEqual([5, 3, 4, 2, 77, 1, WallSide.West]);
    b = new flatbuffers.Builder(32);
    buf = finish(b, Message.HomeResult, HomeResult.createHomeResult(b, HomeResultCode.SurfaceFull));
    expect(open(buf, Message.HomeResult, new HomeResult()).code()).toBe(HomeResultCode.SurfaceFull);
  });
});

describe('auction, rent terms, workbench and thrift protocol', () => {
  it('round-trips AuctionBid', () => {
    const b = new flatbuffers.Builder(64);
    const buf = finish(b, Message.AuctionBid, AuctionBid.createAuctionBid(b, 7, 1500));
    const m = open(buf, Message.AuctionBid, new AuctionBid());
    expect([m.houseId(), m.amount()]).toEqual([7, 1500]);
  });

  it('round-trips HousingState with rent terms and auction fields', () => {
    const b = new flatbuffers.Builder(256);
    const h = HouseInfo.createHouseInfo(b, 2, 0, 0, false, 0n, HouseState.Auction, false, 900, 1795000000n, 800);
    const buf = finish(b, Message.HousingState,
      HousingState.createHousingState(b, HousingState.createHousesVector(b, [h]), 500, 2, 6));
    const m = open(buf, Message.HousingState, new HousingState());
    expect([m.rentPerMonth(), m.claimMonths(), m.maxPrepaidMonths()]).toEqual([500, 2, 6]);
    const a = m.houses(0)!;
    expect([a.topBid(), a.auctionEnd(), a.myBid()]).toEqual([900, 1795000000n, 800]);
  });

  it('round-trips WorkbenchCraft', () => {
    const b = new flatbuffers.Builder(64);
    const buf = finish(b, Message.WorkbenchCraft, WorkbenchCraft.createWorkbenchCraft(b, 321));
    expect(open(buf, Message.WorkbenchCraft, new WorkbenchCraft()).outputItemId()).toBe(321);
  });

  it('round-trips ShopBuy with the thrift shop and defaults to the market', () => {
    const b = new flatbuffers.Builder(64);
    const buf = finish(b, Message.ShopBuy, ShopBuy.createShopBuy(b, 12, 3, ShopKind.Thrift));
    const m = open(buf, Message.ShopBuy, new ShopBuy());
    expect([m.itemId(), m.quantity(), m.shop()]).toEqual([12, 3, ShopKind.Thrift]);
    const b2 = new flatbuffers.Builder(64);
    const buf2 = finish(b2, Message.ShopBuy, ShopBuy.createShopBuy(b2, 12, 3));
    expect(open(buf2, Message.ShopBuy, new ShopBuy()).shop()).toBe(ShopKind.Market);
  });
});
