// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { HomeView, HouseInfoState } from '../net/codec';
import { HouseState } from '../protocol';
import { HousingStore } from '../state/housing';
import { HomeOpenToggle } from './homeOpenToggle';

const house = (id: number, mine: boolean, open: boolean): HouseInfoState =>
  ({ id, areaId: 1, ownerName: mine ? 'You' : 'Sam', mine, paidUntil: 0, state: HouseState.Owned, open, topBid: 0, auctionEnd: 0, myBid: 0 });
const home = (houseId: number): HomeView => ({ houseId, ownerName: 'x', wallpaperItemId: 0, floorItemId: 0, items: [] });

let current: HomeOpenToggle | null = null;
afterEach(() => { current?.dispose(); current = null; });

function make(housing: HousingStore, sendHomeSetOpen = vi.fn()): HomeOpenToggle {
  current = new HomeOpenToggle({ parent: document.body, housing, sendHomeSetOpen, viewport: () => ({ width: 390, height: 844 }) });
  return current;
}

describe('HomeOpenToggle', () => {
  it('AC1: shows only inside the player\'s own home', () => {
    const housing = new HousingStore();
    housing.setHouses([house(1, true, true), house(2, false, true)]);
    const t = make(housing);
    expect(t.button.hidden).toBe(true); // on the campus
    housing.setHome(home(2));
    expect(t.button.hidden).toBe(true); // visitor
    housing.setHome(home(1));
    expect(t.button.hidden).toBe(false);
  });

  it('AC2: reflects open and sends the opposite value when tapped', () => {
    const housing = new HousingStore();
    const send = vi.fn();
    housing.setHouses([house(1, true, true)]);
    housing.setHome(home(1));
    const t = make(housing, send);
    expect(t.button.textContent).toBe('Open');
    t.button.click();
    expect(send).toHaveBeenLastCalledWith(false);
    housing.setHouses([house(1, true, false)]);
    expect(t.button.textContent).toBe('Locked');
    t.button.click();
    expect(send).toHaveBeenLastCalledWith(true);
  });

  it('AC3: a later HousingState changing open updates the label', () => {
    const housing = new HousingStore();
    housing.setHouses([house(1, true, false)]);
    housing.setHome(home(1));
    const t = make(housing);
    expect(t.button.textContent).toBe('Locked');
    housing.setHouses([house(1, true, true)]);
    expect(t.button.textContent).toBe('Open');
  });
});
