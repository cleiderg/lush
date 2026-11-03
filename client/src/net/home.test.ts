import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  Envelope, HomeEnter, HomeLeave, HomeMove, HomePlace, HomeRemove, HomeResult, HomeResultCode, HomeRotate, HomeSetFloor,
  HomeSetOpen, HomeSetWallpaper, HomeState, HouseInfo, HousingState, Message, PlacedItem,
} from '../protocol';
import { GameState } from '../state/world';
import { homeResultText } from '../state/housing';
import { decode, encodeHomeEnter, encodeHomeLeave, encodeHomeMove, encodeHomePlace, encodeHomeRemove, encodeHomeRotate,
  encodeHomeSetFloor, encodeHomeSetOpen, encodeHomeSetWallpaper } from './codec';
import { memoryTokenStore } from './offline';
import { Session } from './session';
import type { Transport } from './transport';

function root(bytes: Uint8Array): Envelope {
  return Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
}

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

const resultBytes = (code: HomeResultCode) => {
  const b = new flatbuffers.Builder(32);
  return finish(b, Message.HomeResult, HomeResult.createHomeResult(b, code));
};

const homeBytes = (houseId: number) => {
  const b = new flatbuffers.Builder(128);
  const it = PlacedItem.createPlacedItem(b, 1, 10, 2, 3, 1, 4, 0, 2, 1);
  return finish(b, Message.HomeState, HomeState.createHomeState(b, houseId, b.createString('Peter'), 5, 8,
    HomeState.createItemsVector(b, [it])));
};

const housesBytes = () => {
  const b = new flatbuffers.Builder(128);
  const h = HouseInfo.createHouseInfo(b, 3, 1, b.createString('Peter'), true);
  return finish(b, Message.HousingState, HousingState.createHousingState(b, HousingState.createHousesVector(b, [h])));
};

describe('home senders round-trip', () => {
  it('HomeEnter, HomeLeave, HomeSetOpen', () => {
    const e = root(encodeHomeEnter(7));
    expect(e.msgType()).toBe(Message.HomeEnter);
    expect((e.msg(new HomeEnter()) as HomeEnter).houseId()).toBe(7);
    expect(root(encodeHomeLeave()).msgType()).toBe(Message.HomeLeave);
    expect(root(encodeHomeLeave()).msg(new HomeLeave())).not.toBeNull();
    const o = root(encodeHomeSetOpen(true));
    expect(o.msgType()).toBe(Message.HomeSetOpen);
    expect((o.msg(new HomeSetOpen()) as HomeSetOpen).open()).toBe(true);
  });

  it('HomePlace on floor, surface and wall', () => {
    const f = root(encodeHomePlace(2, { x: 3, y: 4, rotation: 1 }));
    expect(f.msgType()).toBe(Message.HomePlace);
    const fm = f.msg(new HomePlace()) as HomePlace;
    expect([fm.inventorySlot(), fm.x(), fm.y(), fm.rotation(), fm.parentPlacedId(), fm.slot(), fm.side()]).toEqual([2, 3, 4, 1, 0, 0, 0]);
    const s = root(encodeHomePlace(5, { x: 3, y: 4, rotation: 0, parentPlacedId: 99, slot: 2 })).msg(new HomePlace()) as HomePlace;
    expect([s.parentPlacedId(), s.slot()]).toEqual([99, 2]);
    const w = root(encodeHomePlace(6, { x: 5, y: 0, rotation: 0, side: 3 })).msg(new HomePlace()) as HomePlace;
    expect([w.x(), w.side()]).toEqual([5, 3]);
  });

  it('HomeMove, HomeRotate, HomeRemove, HomeSetWallpaper, HomeSetFloor', () => {
    const mv = root(encodeHomeMove(12, { x: 1, y: 2, rotation: 3, parentPlacedId: 4, slot: 1, side: 2 }));
    expect(mv.msgType()).toBe(Message.HomeMove);
    const m = mv.msg(new HomeMove()) as HomeMove;
    expect([m.placedId(), m.x(), m.y(), m.rotation(), m.parentPlacedId(), m.slot(), m.side()]).toEqual([12, 1, 2, 3, 4, 1, 2]);
    const r = root(encodeHomeRotate(12, 2));
    expect(r.msgType()).toBe(Message.HomeRotate);
    const rm = r.msg(new HomeRotate()) as HomeRotate;
    expect([rm.placedId(), rm.rotation()]).toEqual([12, 2]);
    const rem = root(encodeHomeRemove(12));
    expect(rem.msgType()).toBe(Message.HomeRemove);
    expect((rem.msg(new HomeRemove()) as HomeRemove).placedId()).toBe(12);
    const wp = root(encodeHomeSetWallpaper(4));
    expect(wp.msgType()).toBe(Message.HomeSetWallpaper);
    expect((wp.msg(new HomeSetWallpaper()) as HomeSetWallpaper).inventorySlot()).toBe(4);
    const fl = root(encodeHomeSetFloor(9));
    expect(fl.msgType()).toBe(Message.HomeSetFloor);
    expect((fl.msg(new HomeSetFloor()) as HomeSetFloor).inventorySlot()).toBe(9);
  });

  it('the session has a sender for each', () => {
    const sent: Uint8Array[] = [];
    const t: Transport = {
      send: (d: Uint8Array) => sent.push(d),
      close: () => {},
      onOpen: null, onMessage: null, onClose: null,
    } as unknown as Transport;
    const s = new Session({ factory: () => t, store: memoryTokenStore(), onMessage: () => {} });
    for (const k of ['sendHomeEnter', 'sendHomeLeave', 'sendHomeSetOpen', 'sendHomePlace', 'sendHomeMove', 'sendHomeRotate',
      'sendHomeRemove', 'sendHomeSetWallpaper', 'sendHomeSetFloor']) {
      expect(typeof (s as unknown as Record<string, unknown>)[k]).toBe('function');
    }
  });
});

describe('HomeResult', () => {
  it('is decoded', () => {
    expect(decode(resultBytes(HomeResultCode.Locked))).toEqual({ type: 'homeResult', code: HomeResultCode.Locked });
  });

  it('every non-Ok code has its own chat line', () => {
    const codes = Object.values(HomeResultCode).filter((c): c is HomeResultCode => typeof c === 'number' && c !== HomeResultCode.Ok);
    expect(codes.length).toBe(18);
    const lines = codes.map((c) => homeResultText(c));
    expect(lines.every((l) => !!l)).toBe(true);
    expect(new Set(lines).size).toBe(codes.length);
    expect(homeResultText(HomeResultCode.Ok)).toBeNull();
    const w = new GameState();
    w.handle(decode(resultBytes(HomeResultCode.Locked))!, 0);
    expect(w.chat.log.at(-1)).toMatchObject({ text: homeResultText(HomeResultCode.Locked), system: true });
  });
});

describe('HousingStore home tracking', () => {
  it('knows inside and own, and forgets after a successful leave', () => {
    const w = new GameState();
    expect(w.housing.inHome()).toBe(false);
    w.handle(decode(housesBytes())!, 0);
    w.handle(decode(homeBytes(3))!, 0);
    expect(w.housing.inHome()).toBe(true);
    expect(w.housing.inOwnHome()).toBe(true);
    expect(w.housing.home?.items[0]).toMatchObject({ slot: 2, side: 1 });
    w.handle(decode(homeBytes(4))!, 0);
    expect(w.housing.inOwnHome()).toBe(false);
    // an edit Ok does not leave
    w.handle(decode(resultBytes(HomeResultCode.Ok))!, 0);
    expect(w.housing.inHome()).toBe(true);
    // a refused edit sent before the leave keeps the home until the leave's Ok
    w.housing.beginLeave();
    w.handle(decode(resultBytes(HomeResultCode.RateLimited))!, 0);
    expect(w.housing.inHome()).toBe(true);
    w.handle(decode(resultBytes(HomeResultCode.Ok))!, 0);
    expect(w.housing.inHome()).toBe(false);
    // leaving when the server says you were not inside also forgets the home
    w.handle(decode(homeBytes(3))!, 0);
    w.housing.beginLeave();
    w.handle(decode(resultBytes(HomeResultCode.NotInHome))!, 0);
    expect(w.housing.inHome()).toBe(false);
  });
});
