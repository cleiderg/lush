import type { HomeView, HouseInfoState, PlacedItemState } from '../net/codec';
import { AuctionResultCode, HomeResultCode, HouseState, HousingResultCode } from '../protocol';

/** Rent terms the server sends with HousingState. */
export interface RentTerms {
  rentPerMonth: number;
  claimMonths: number;
  maxPrepaidMonths: number;
}

/** Chat line on login when the player's house is in the rent warning. */
export const RENT_WARNING_LOGIN = 'Your rent is running low. Pay rent at your house, or it will go to auction';

/** A rent month is 30 real days (docs/specs/housing.md). */
export const RENT_MONTH_SECONDS = 30 * 24 * 3600;

/** Whole months of rent still prepaid at `now` (unix seconds). */
export function prepaidMonths(paidUntil: number, now: number): number {
  return Math.max(0, Math.floor((paidUntil - now) / RENT_MONTH_SECONDS));
}

/**
 * Most whole months the owner can still add without paid-until passing `now` plus `maxPrepaid` months,
 * the server's PayRent check (a part month already prepaid counts against the room left).
 */
export function maxRentMonths(paidUntil: number, now: number, maxPrepaid: number): number {
  const left = Math.max(0, paidUntil - now);
  return Math.max(0, Math.floor((maxPrepaid * RENT_MONTH_SECONDS - left) / RENT_MONTH_SECONDS));
}

export type { HomeView, HouseInfoState, PlacedItemState };

/** Houses from HousingState and the current home from HomeState. Subscribers are told after any change. */
export class HousingStore {
  houses: HouseInfoState[] = [];
  home: HomeView | null = null;
  private leaving = false;
  /** Rent terms from HousingState (server data, rules.json); 0 until the first HousingState. */
  rentPerMonth = 0;
  claimMonths = 0;
  maxPrepaidMonths = 0;
  private listeners = new Set<() => void>();

  /** Calls fn after every change; returns an unsubscribe function. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private notify(): void {
    for (const fn of this.listeners) fn();
  }

  /** Replaces the whole house list and the rent terms. */
  setHouses(houses: readonly HouseInfoState[], terms?: RentTerms): void {
    this.houses = houses.map((h) => ({ ...h }));
    if (terms) {
      this.rentPerMonth = terms.rentPerMonth;
      this.claimMonths = terms.claimMonths;
      this.maxPrepaidMonths = terms.maxPrepaidMonths;
    }
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

  /**
   * Applies a HomeResult; a successful leave forgets the home. The server only
   * answers HomeLeave with Ok or NotInHome, so any other code is the reply to an
   * edit sent before the leave, and the leave is still pending.
   */
  applyHomeResult(code: HomeResultCode): void {
    if (!this.leaving || (code !== HomeResultCode.Ok && code !== HomeResultCode.NotInHome)) return;
    this.leaving = false;
    if (this.home) {
      this.home = null;
      this.notify();
    }
  }

  /** The house with this id, or null. */
  house(id: number): HouseInfoState | null {
    return this.houses.find((h) => h.id === id) ?? null;
  }

  /** Gold to claim an empty house: claimMonths months of rent. */
  claimCost(): number {
    return this.claimMonths * this.rentPerMonth;
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
    this.rentPerMonth = this.claimMonths = this.maxPrepaidMonths = 0;
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

/** Chat line for every AuctionResult code, Ok included (a bid is held, so the player wants to see it). */
export function auctionResultText(code: AuctionResultCode): string {
  switch (code) {
    case AuctionResultCode.Ok: return 'Your bid is the highest. The gold is held until the auction ends';
    case AuctionResultCode.NotAuction: return 'That house is not up for auction';
    case AuctionResultCode.TooFar: return 'You need to be closer to the house to bid';
    case AuctionResultCode.PreviousOwner: return "You can't bid on the house you lost";
    case AuctionResultCode.BidTooLow: return 'Your bid must be higher than the top bid';
    case AuctionResultCode.NotEnoughGold: return "You don't have enough gold to hold that bid";
    case AuctionResultCode.OwnsAnother: return 'You already own a house';
    case AuctionResultCode.SaveFailed: return 'Your bid could not be saved. Try again';
    default: return 'Something went wrong';
  }
}
