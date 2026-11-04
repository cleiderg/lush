// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { buildWorldView } from '../core/view';
import type { HomeView } from '../net/codec';
import { HomeResultCode } from '../protocol';
import { HousingStore } from '../state/housing';
import { GameState } from '../state/world';
import { HomeLeaveButton } from './homeLeaveButton';

const home = (wallpaperItemId: number, floorItemId: number, items: HomeView['items'] = []): HomeView =>
  ({ houseId: 7, ownerName: 'Sam', wallpaperItemId, floorItemId, items });
const chair = { placedId: 3, itemId: 41, x: 2, y: 1, rotation: 2, layer: 1, parentPlacedId: 9, slot: 1, side: 3 };

let current: HomeLeaveButton | null = null;
afterEach(() => { current?.dispose(); current = null; });

describe('home room in the world view', () => {
  it('AC1: HomeState gives the room description and a later HomeState replaces it', () => {
    const game = new GameState();
    expect(buildWorldView(game, 0).home).toBeUndefined();
    game.housing.setHome(home(5, 6, [chair]));
    expect(buildWorldView(game, 0).home).toEqual({ wallpaperId: 5, floorId: 6, items: [chair] });
    game.housing.setHome(home(8, 0));
    expect(buildWorldView(game, 0).home).toEqual({ wallpaperId: 8, floorId: 0, items: [] });
  });

  it('AC3: after a successful leave the world view has no home', () => {
    const game = new GameState();
    game.housing.setHome(home(5, 6, [chair]));
    game.housing.beginLeave();
    game.housing.applyHomeResult(HomeResultCode.Ok);
    expect(buildWorldView(game, 0).home).toBeUndefined();
  });
});

describe('HomeLeaveButton', () => {
  it('AC2: shows only inside a home and calls the HomeLeave sender when tapped', () => {
    const housing = new HousingStore();
    const sendHomeLeave = vi.fn();
    current = new HomeLeaveButton({ parent: document.body, housing, sendHomeLeave, viewport: () => ({ width: 390, height: 844 }) });
    expect(current.button.hidden).toBe(true);
    housing.setHome(home(0, 0));
    expect(current.button.hidden).toBe(false);
    current.button.click();
    expect(sendHomeLeave).toHaveBeenCalledTimes(1);
    housing.beginLeave();
    housing.applyHomeResult(HomeResultCode.Ok);
    expect(current.button.hidden).toBe(true);
  });
});
