import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  CraftResult, CraftResultCode, Envelope, ItemCatalog, ItemDef, Message, PlacementKind, ShopBuy, ShopKind, ShopSell,
  WorkbenchCatalog, WorkbenchCraft, WorkbenchRecipe,
} from '../protocol';
import { GameState } from '../state/world';
import { craftResultText } from '../state/workbench';
import { decode, encodeShopBuy, encodeShopSell, encodeWorkbenchCraft } from './codec';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

function open(bytes: Uint8Array): Envelope {
  return Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
}

function catalogBytes(): Uint8Array {
  const b = new flatbuffers.Builder(256);
  const sofa = ItemDef.createItemDef(b, 40, b.createString('Sofa'), 0, 0, 0, 0, 0, 0, 0, 0, 0, false, 0, 0, 0, 0, 3, 2, PlacementKind.Floor, 0, 90, 30);
  const sword = ItemDef.createItemDef(b, 5, b.createString('Sword'));
  return finish(b, Message.ItemCatalog, ItemCatalog.createItemCatalog(b, ItemCatalog.createItemsVector(b, [sofa, sword])));
}

function workbenchBytes(outputs: number[]): Uint8Array {
  const b = new flatbuffers.Builder(256);
  const recipes = outputs.map((o) => WorkbenchRecipe.createWorkbenchRecipe(
    b, o, WorkbenchRecipe.createInputItemIdsVector(b, [7, 8]), WorkbenchRecipe.createInputQuantitiesVector(b, [2, 5]), 25));
  return finish(b, Message.WorkbenchCatalog, WorkbenchCatalog.createWorkbenchCatalog(b, WorkbenchCatalog.createRecipesVector(b, recipes)));
}

describe('furniture and thrift item fields', () => {
  it('fills furniture and thrift fields, zero for non-furniture', () => {
    const w = new GameState();
    w.handle(decode(catalogBytes())!, 0);
    expect(w.items.item(40)).toMatchObject({
      footprintW: 3, footprintD: 2, placement: PlacementKind.Floor, surfaceSlots: 0, thriftSellsFor: 90, thriftBuysFor: 30,
    });
    expect(w.items.item(5)).toMatchObject({
      footprintW: 0, footprintD: 0, placement: PlacementKind.None, surfaceSlots: 0, thriftSellsFor: 0, thriftBuysFor: 0,
    });
  });
});

describe('workbench catalog', () => {
  it('fills recipes and replaces the earlier catalog', () => {
    const w = new GameState();
    w.handle(decode(workbenchBytes([40, 41]))!, 0);
    expect(w.workbench.recipes).toEqual([
      { outputItemId: 40, inputs: [{ itemId: 7, quantity: 2 }, { itemId: 8, quantity: 5 }], gold: 25 },
      { outputItemId: 41, inputs: [{ itemId: 7, quantity: 2 }, { itemId: 8, quantity: 5 }], gold: 25 },
    ]);
    w.handle(decode(workbenchBytes([42]))!, 0);
    expect(w.workbench.recipes.map((r) => r.outputItemId)).toEqual([42]);
  });
});

describe('workbench and thrift senders', () => {
  it('encodes WorkbenchCraft', () => {
    const e = open(encodeWorkbenchCraft(40));
    expect(e.msgType()).toBe(Message.WorkbenchCraft);
    expect((e.msg(new WorkbenchCraft()) as WorkbenchCraft).outputItemId()).toBe(40);
  });

  it('targets the thrift stall and defaults to the market', () => {
    const buy = (b: Uint8Array) => open(b).msg(new ShopBuy()) as ShopBuy;
    const sell = (b: Uint8Array) => open(b).msg(new ShopSell()) as ShopSell;
    expect(buy(encodeShopBuy(3, 2, ShopKind.Thrift)).shop()).toBe(ShopKind.Thrift);
    expect(buy(encodeShopBuy(3, 2, ShopKind.Thrift)).itemId()).toBe(3);
    expect(buy(encodeShopBuy(3, 2)).shop()).toBe(ShopKind.Market);
    expect(sell(encodeShopSell(4, 1, ShopKind.Thrift)).shop()).toBe(ShopKind.Thrift);
    expect(sell(encodeShopSell(4, 1, ShopKind.Thrift)).slot()).toBe(4);
    expect(sell(encodeShopSell(4, 1)).shop()).toBe(ShopKind.Market);
  });
});

describe('craft results', () => {
  it('has its own chat line for every code, shown in the chat log', () => {
    const codes = Object.values(CraftResultCode).filter((v): v is CraftResultCode => typeof v === 'number');
    expect(codes).toHaveLength(7);
    const lines = codes.map(craftResultText);
    expect(new Set(lines).size).toBe(codes.length);
    for (const code of codes) {
      const w = new GameState();
      const b = new flatbuffers.Builder(32);
      w.handle(decode(finish(b, Message.CraftResult, CraftResult.createCraftResult(b, code)))!, 0);
      expect(JSON.stringify(w.chat)).toContain(craftResultText(code));
    }
  });
});
