import type { HomeView, HouseInfoState, PlacedItemState } from '../net/codec';
import { HouseState, HousingResultCode } from '../protocol';

export type { HomeView, HouseInfoState, PlacedItemState };

/** Houses from HousingState and the current home from HomeState. Subscribers are told after any change. */
export class HousingStore {
  houses: HouseInfoState[] = [];
  home: HomeView | null = null;
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
