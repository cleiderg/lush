import type { HomeView, HouseInfoState, PlacedItemState } from '../net/codec';
import { HomeResultCode, HouseState, HousingResultCode } from '../protocol';

export type { HomeView, HouseInfoState, PlacedItemState };

/** Houses from HousingState and the current home from HomeState. Subscribers are told after any change. */
export class HousingStore {
  houses: HouseInfoState[] = [];
  home: HomeView | null = null;
  private leaving = false;
  private listeners = new Set<() => void>();

  /** Calls fn after every change; returns an unsubscribe function. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private notify(): void {
    for (const fn of this.listeners) fn();
  }

  /** Replaces the whole house list. */
  setHouses(houses: readonly HouseInfoState[]): void {
    this.houses = houses.map((h) => ({ ...h }));
    this.notify();
  }

  /** Replaces the current home. */
  setHome(h: HomeView): void {
    this.home = { ...h, items: h.items.map((i) => ({ ...i })) };
    this.notify();
  }

  /** True when the player is inside a home. */
  inHome(): boolean {
    return this.home !== null;
  }

  /** True when the home the player is inside is their own house. */
  inOwnHome(): boolean {
    const mine = this.myHouse();
    return this.home !== null && mine !== null && mine.id === this.home.houseId;
  }

  /** Call when HomeLeave is sent; the next Ok HomeResult then drops the home. */
  beginLeave(): void {
    this.leaving = true;
  }

  /** Applies a HomeResult; a successful leave forgets the home. */
  applyHomeResult(code: HomeResultCode): void {
    const left = this.leaving && (code === HomeResultCode.Ok || code === HomeResultCode.NotInHome);
    this.leaving = false;
    if (left && this.home) {
      this.home = null;
      this.notify();
    }
  }

  /** The house this player owns, or null. */
  myHouse(): HouseInfoState | null {
    return this.houses.find((h) => h.mine) ?? null;
  }

  /** True when the player's own house is in the 7-day rent warning. */
  inWarning(): boolean {
    return this.myHouse()?.state === HouseState.Warning;
  }

  /** Forgets everything; used on a new Welcome. */
  clear(): void {
    if (this.houses.length === 0 && !this.home) return;
    this.houses = [];
    this.home = null;
    this.leaving = false;
    this.notify();
  }
}

/** Chat line for a HousingResult code; null for Ok (the HousingState update shows it). */
export function housingResultText(code: HousingResultCode): string | null {
  switch (code) {
    case HousingResultCode.Ok: return null;
    case HousingResultCode.AlreadyOwned: return 'That house already belongs to someone';
    case HousingResultCode.OwnsAnother: return 'You already own a house';
    case HousingResultCode.NotEnoughGold: return "You don't have enough gold";
    case HousingResultCode.TooFar: return 'You are too far from that house';
    case HousingResultCode.TooManyMonths: return "You can't prepay that far ahead";
    case HousingResultCode.NotOwner: return "You don't own that house";
    case HousingResultCode.SaveFailed: return 'The house could not save. Try again';
    default: return 'Something went wrong';
  }
}

/** Chat line for a HomeResult code; null for Ok. */
export function homeResultText(code: HomeResultCode): string | null {
  switch (code) {
    case HomeResultCode.Ok: return null;
    case HomeResultCode.OutOfBounds: return "That won't fit inside the room";
    case HomeResultCode.Overlap: return 'Something is already in the way there';
    case HomeResultCode.BadRotation: return "It can't be turned that way";
    case HomeResultCode.BadLayer: return "That can't go there";
    case HomeResultCode.NotSurfaceItem: return "That's too big to sit on a surface";
    case HomeResultCode.NoSurface: return 'There is nothing there to put it on';
    case HomeResultCode.SurfaceFull: return 'There is no room left on that surface';
    case HomeResultCode.HasItems: return 'Clear everything off it first';
    case HomeResultCode.NotFound: return "You can't find that item";
    case HomeResultCode.NotOwner: return 'Only the owner can do that';
    case HomeResultCode.Locked: return 'The door is locked';
    case HomeResultCode.Full: return 'The house is full of guests';
    case HomeResultCode.TooFar: return 'You need to be closer to the door';
    case HomeResultCode.NotInInventory: return "You don't have that item with you";
    case HomeResultCode.InventoryFull: return 'Your pack is too full to hold that';
    case HomeResultCode.NotInHome: return "You aren't inside a house";
    case HomeResultCode.RateLimited: return 'Slow down; you are redecorating too fast';
    case HomeResultCode.SaveFailed: return 'The change could not be saved. Try again';
    default: return 'Something went wrong';
  }
}
