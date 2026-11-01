import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  Envelope, HomeState, HouseClaim, HouseInfo, HouseState, HousingResult, HousingResultCode, HousingState,
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
});
