// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { HomeView, PlacedItemState } from '../net/codec';
import { EquipSlot, HouseState, ItemKind, PlacementKind } from '../protocol';
import { HousingStore } from '../state/housing';
import { ItemState, type ItemInfo } from '../state/items';
import { HomeEditPanel, type Tile } from './homeEditPanel';

const HOUSE = 7;

function def(id: number, name: string, placement: PlacementKind): ItemInfo {
  return {
    id, name, kind: ItemKind.Loot, equipSlot: EquipSlot.None, levelToEquip: 1, accuracy: 0, strength: 0, defence: 0,
    attackSpeed: 0, heal: 0, shopBuysFor: 1, shopSells: false, shopSellsFor: 0, toolSkill: 0, toolLevel: 0, cookingLevel: 0, placement,
  };
}

function placed(placedId: number, itemId: number, rotation: number): PlacedItemState {
  return { placedId, itemId, x: 1, y: 1, rotation, layer: 0, parentPlacedId: 0, slot: 0, side: 0 };
}

function home(houseId: number, items: PlacedItemState[] = []): HomeView {
  return { houseId, ownerName: 'x', wallpaperItemId: 0, floorItemId: 0, items };
}

let current: HomeEditPanel | null = null;
afterEach(() => { current?.dispose(); current = null; });

function setup(opts: { houseMine?: boolean; inHome?: number | null; placedItems?: PlacedItemState[] } = {}) {
  const housing = new HousingStore();
  housing.setHouses([{ id: HOUSE, areaId: 1, ownerName: 'Me', mine: opts.houseMine ?? true, paidUntil: 0, state: HouseState.Owned, open: true }]);
  const inHome = opts.inHome === undefined ? HOUSE : opts.inHome;
  if (inHome !== null) housing.setHome(home(inHome, opts.placedItems));
  const items = new ItemState();
  items.setCatalog([
    def(1, 'Acorn', PlacementKind.None), def(2, 'Beanbag', PlacementKind.Floor), def(3, 'Poster', PlacementKind.Wall),
    def(4, 'Blue wallpaper', PlacementKind.Wallpaper), def(5, 'Wood flooring', PlacementKind.Flooring),
  ]);
  items.setInventory({
    slots: [1, 2, 0, 4, 5, 3].map((itemId) => ({ itemId, quantity: itemId ? 1 : 0 })),
    equipment: { weapon: 0, head: 0, body: 0, shield: 0 },
  });
  const session = {
    sendHomePlace: vi.fn(), sendHomeMove: vi.fn(), sendHomeRotate: vi.fn(),
    sendHomeRemove: vi.fn(), sendHomeSetWallpaper: vi.fn(), sendHomeSetFloor: vi.fn(),
  };
  let pick: ((t: Tile) => void) | null = null;
  const panel = new HomeEditPanel({
    parent: document.body, housing, items, session, viewport: () => ({ width: 390, height: 844 }),
    pickTile: (cb) => { pick = cb; },
  });
  current = panel;
  const q = (sel: string) => panel.drawer.querySelector<HTMLButtonElement>(sel)!;
  const qa = (sel: string) => [...panel.drawer.querySelectorAll<HTMLButtonElement>(sel)];
  return { housing, items, session, panel, q, qa, tap: (t: Tile) => pick!(t) };
}

describe('HomeEditPanel', () => {
  it('shows the Edit button only inside the player\'s own home', () => {
    const own = setup();
    expect(own.panel.button.hidden).toBe(false);
    own.panel.dispose();

    const visitor = setup({ houseMine: false });
    expect(visitor.panel.button.hidden).toBe(true);
    visitor.panel.setEditing(true);
    expect(visitor.panel.isEditing).toBe(false);
    visitor.panel.dispose();

    const outside = setup({ inHome: null });
    expect(outside.panel.button.hidden).toBe(true);
    outside.panel.dispose();
  });

  it('shows the button when the player walks into their home and hides it on leaving', () => {
    const t = setup({ inHome: null });
    expect(t.panel.button.hidden).toBe(true);
    t.housing.setHome(home(HOUSE));
    expect(t.panel.button.hidden).toBe(false);
    t.panel.button.click();
    expect(t.panel.isEditing).toBe(true);
    t.housing.beginLeave();
    t.housing.applyHomeResult(0);
    expect(t.panel.button.hidden).toBe(true);
    expect(t.panel.isEditing).toBe(false);
  });

  it('lists only furniture, wallpaper and floors from the inventory', () => {
    const t = setup();
    t.panel.button.click();
    expect(t.qa('.lush-edit-furniture').map((b) => b.textContent)).toEqual(['Beanbag', 'Poster']);
    expect(t.qa('.lush-edit-wallpaper').map((b) => b.textContent)).toEqual(['Blue wallpaper']);
    expect(t.qa('.lush-edit-floor').map((b) => b.textContent)).toEqual(['Wood flooring']);
    expect(t.panel.drawer.textContent).not.toContain('Acorn');
  });

  it('places furniture with its inventory slot, the picked tile and the current rotation', () => {
    const t = setup();
    t.panel.button.click();
    t.qa('.lush-edit-furniture')[0].click();
    expect(t.session.sendHomePlace).not.toHaveBeenCalled();
    t.q('.lush-edit-rotate').click();
    t.tap({ x: 3, y: 4 });
    expect(t.session.sendHomePlace).toHaveBeenCalledWith(1, { x: 3, y: 4, rotation: 1 });
  });

  it('applies wallpaper and floor at once', () => {
    const t = setup();
    t.panel.button.click();
    t.q('.lush-edit-wallpaper').click();
    t.q('.lush-edit-floor').click();
    expect(t.session.sendHomeSetWallpaper).toHaveBeenCalledWith(3);
    expect(t.session.sendHomeSetFloor).toHaveBeenCalledWith(4);
  });

  it('rotates a selected placed item to its next quarter turn', () => {
    const t = setup({ placedItems: [placed(9, 2, 3)] });
    t.panel.button.click();
    t.q('.lush-edit-placed').click();
    t.q('.lush-edit-rotate').click();
    expect(t.session.sendHomeRotate).toHaveBeenCalledWith(9, 0);
  });

  it('removes a selected placed item', () => {
    const t = setup({ placedItems: [placed(9, 2, 0)] });
    t.panel.button.click();
    t.panel.selectPlaced(9);
    t.q('.lush-edit-remove').click();
    expect(t.session.sendHomeRemove).toHaveBeenCalledWith(9);
  });

  it('moves a selected placed item to the picked tile, keeping its rotation', () => {
    const t = setup({ placedItems: [placed(9, 2, 2)] });
    t.panel.button.click();
    t.q('.lush-edit-placed').click();
    t.tap({ x: 5, y: 6 });
    expect(t.session.sendHomeMove).toHaveBeenCalledWith(9, { x: 5, y: 6, rotation: 2 });
  });
});
