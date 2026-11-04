import { encodeAuctionBid, encodeHouseClaim, encodeRentPay } from '../net/codec';
import { HouseState } from '../protocol';
import { maxRentMonths, type HouseInfoState, prepaidMonths, RENT_MONTH_SECONDS, type HousingStore } from '../state/housing';
import { hudLayout, place } from './hudLayout';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

export interface HousePanelOptions {
  parent: HTMLElement;
  housing: HousingStore;
  /** Sends an encoded client message (HouseClaim, RentPay) to the server. */
  send: (bytes: Uint8Array) => void;
  /** Bids gold on a house in auction; defaults to sending an AuctionBid through `send`. */
  sendBid?: (houseId: number, amount: number) => void;
  /** Unix seconds now; defaults to the device clock. */
  now?: () => number;
  viewport?: () => { width: number; height: number };
  /** Called when the panel opens, so other panels in its place can close. */
  onOpen?: () => void;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls: string, text = ''): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = cls;
  e.textContent = text;
  return e;
}

function button(cls: string, text: string, onClick: () => void): HTMLButtonElement {
  const b = el('button', `${cls} ${BUTTON_CLASS}`, text);
  b.type = 'button';
  b.addEventListener('click', onClick);
  return b;
}

/** A unix-seconds date as text, in Irvine time so every player reads the same day. */
export function formatDate(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleDateString('en-US', {
    month: 'short', day: 'numeric', year: 'numeric', timeZone: 'America/Los_Angeles',
  });
}

/** Seconds as the two largest units, like "2d 3h", "5h 12m" or "40s". */
export function formatDuration(seconds: number): string {
  const s = Math.floor(seconds);
  const d = Math.floor(s / 86400);
  const hr = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d}d ${hr}h`;
  if (hr > 0) return `${hr}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${s}s`;
}

/**
 * House panel: owner and state of one house, Claim (claim_months of rent) for an empty house, and for
 * the player's own house the paid-until date and a Rent control for 1 to the room left under
 * max_prepaid_months. Also owns the rent warning banner shown while the player's house is in Warning.
 * Every number comes from the HousingState rent terms.
 */
export class HousePanel {
  readonly panel: HTMLElement;
  readonly banner: HTMLElement;
  private readonly status: HTMLElement;
  private readonly body: HTMLElement;
  private houseId = 0;
  private months = 1;
  private bid = 0;
  private bidHouse = 0;
  private readonly motion: PanelMotion;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: HousePanelOptions) {
    injectStyle();
    this.panel = el('div', 'lush-panel lush-house');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'House');
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    const head = el('div', 'lush-house-head');
    head.append(el('span', 'lush-title lush-house-title', 'House'), button('lush-house-close', 'Close', () => this.close()));
    this.status = el('div', 'lush-house-status');
    this.body = el('div', 'lush-house-body');
    this.panel.append(head, this.status, this.body);
    this.banner = el('div', 'lush-house-banner');
    this.banner.setAttribute('role', 'status');
    this.banner.hidden = true;
    opts.parent.append(this.panel, this.banner);
    this.unsubscribe = opts.housing.subscribe(() => this.render());
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  /** Shows the panel for this house. */
  open(houseId: number): void {
    this.houseId = houseId;
    this.months = 1;
    this.bidHouse = 0;
    if (!this.isOpen) {
      this.motion.set(true);
      this.opts.onOpen?.();
    }
    this.render();
  }

  close(): void {
    this.motion.set(false);
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    const l = hudLayout(v.width, v.height);
    place(this.panel, l.house);
    place(this.banner, l.houseBanner);
  }

  private now(): number {
    return this.opts.now?.() ?? Date.now() / 1000;
  }

  render(): void {
    const h = this.opts.housing;
    const mine = h.myHouse();
    const warn = h.inWarning() && mine !== null;
    this.banner.hidden = !warn;
    this.banner.textContent = warn && mine
      ? `Your rent is running low. Pay before ${formatDate(mine.paidUntil - RENT_MONTH_SECONDS)} or the house goes to auction`
      : '';
    if (!this.isOpen) return;

    const house = h.house(this.houseId);
    this.body.replaceChildren();
    if (!house) {
      this.status.textContent = 'No such house';
      return;
    }
    if (house.state === HouseState.Empty) {
      this.status.textContent = 'Empty house';
      const cost = h.claimCost();
      const claim = button('lush-house-claim', `Claim for ${cost} gp`, () => this.opts.send(encodeHouseClaim(house.id)));
      this.body.append(el('div', 'lush-house-line', `Claiming costs ${h.claimMonths} months of rent, ${h.rentPerMonth} gp a month.`), claim);
      return;
    }
    if (house.state === HouseState.Auction) {
      this.status.textContent = 'Up for auction';
      this.renderAuction(house);
      return;
    }
    this.status.textContent = house.mine ? 'Your house' : `Owned by ${house.ownerName}`;
    if (!house.mine) return;

    const now = this.now();
    const room = maxRentMonths(house.paidUntil, now, h.maxPrepaidMonths);
    this.months = Math.max(1, Math.min(this.months, room));
    const paid = el('div', 'lush-house-line lush-house-paid', `Paid until ${formatDate(house.paidUntil)}`);
    const rent = el('div', 'lush-house-rent');
    const minus = button('lush-house-minus', '-', () => this.step(-1));
    const plus = button('lush-house-plus', '+', () => this.step(1));
    minus.disabled = this.months <= 1;
    plus.disabled = this.months >= room;
    const count = el('span', 'lush-house-months', `${this.months} ${this.months === 1 ? 'month' : 'months'}`);
    const pay = button('lush-house-rent-pay', `Rent for ${this.months * h.rentPerMonth} gp`, () => {
      this.opts.send(encodeRentPay(house.id, this.months));
    });
    pay.disabled = room < 1;
    rent.append(minus, count, plus, pay);
    const note = el('div', 'lush-house-line lush-house-note',
      room < 1 ? `Already prepaid the most, ${h.maxPrepaidMonths} months.`
        : `${prepaidMonths(house.paidUntil, now)} of ${h.maxPrepaidMonths} months prepaid.`);
    this.body.append(paid, rent, note);
  }

  private renderAuction(house: HouseInfoState): void {
    const h = this.opts.housing;
    const min = house.topBid > 0 ? house.topBid + 1 : h.rentPerMonth;
    // Keep the chosen amount across re-renders of this house, but never below the lowest valid bid.
    this.bid = this.bidHouse === house.id ? Math.max(min, this.bid) : min;
    this.bidHouse = house.id;
    const left = Math.max(0, house.auctionEnd - this.now());
    this.body.append(
      el('div', 'lush-house-line lush-house-top-bid', house.topBid > 0 ? `Top bid: ${house.topBid} gp` : `No bids yet, starting at ${h.rentPerMonth} gp`),
      el('div', 'lush-house-line lush-house-time-left', left > 0 ? `Time left: ${formatDuration(left)}` : 'Auction ending'),
      el('div', 'lush-house-line lush-house-my-bid', house.myBid > 0 ? `Your held bid: ${house.myBid} gp` : 'You have no bid held'),
    );
    const step = Math.max(1, Math.floor(h.rentPerMonth / 10));
    const row = el('div', 'lush-house-rent');
    const minus = button('lush-house-bid-minus', '-', () => this.stepBid(-step, min));
    const plus = button('lush-house-bid-plus', '+', () => this.stepBid(step, min));
    minus.disabled = this.bid <= min;
    const amount = el('span', 'lush-house-bid-amount', `${this.bid} gp`);
    amount.style.flex = '1';
    amount.style.textAlign = 'center';
    const bidAmount = this.bid;
    const bid = button('lush-house-bid', 'Bid', () => {
      if (this.opts.sendBid) this.opts.sendBid(house.id, bidAmount);
      else this.opts.send(encodeAuctionBid(house.id, bidAmount));
    });
    row.append(minus, amount, plus, bid);
    this.body.append(row, el('div', 'lush-house-line lush-house-note',
      `Bids hold gold until you are outbid. The winner also pays ${h.claimMonths * h.rentPerMonth} gp (${h.claimMonths} months of rent) on top of the bid.`));
  }

  private stepBid(d: number, min: number): void {
    this.bid = Math.max(min, this.bid + d);
    this.render();
  }

  private step(d: number): void {
    this.months += d;
    this.render();
  }

  dispose(): void {
    this.unsubscribe();
    this.motion.dispose();
    window.removeEventListener('resize', this.onResize);
    this.panel.remove();
    this.banner.remove();
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-house { box-sizing: border-box; z-index: 6; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: var(--lush-text-sm); line-height: 16px; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-house[hidden] { display: none; }
.lush-house-head { display: flex; align-items: center; justify-content: space-between; gap: var(--lush-space-2); }
.lush-house-title { font-size: var(--lush-text-lg); }
.lush-house-status { color: var(--lush-gold-bright); font-weight: 600; }
.lush-house-body { display: flex; flex-direction: column; gap: var(--lush-space-2); }
.lush-house-line { padding: var(--lush-space-1) var(--lush-space-2); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); }
.lush-house-note { color: var(--lush-text-muted); }
.lush-house-rent { display: flex; align-items: center; gap: var(--lush-space-2); }
.lush-house-months { flex: 1; text-align: center; white-space: nowrap; }
.lush-house .lush-house-claim, .lush-house .lush-house-rent-pay { background: var(--lush-active); }
.lush-house button:disabled { opacity: 0.5; }
.lush-house-banner { box-sizing: border-box; z-index: 5; display: flex; align-items: center; padding: 0 var(--lush-space-3); font-size: var(--lush-text-sm); line-height: 16px; color: var(--lush-gold-bright); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); pointer-events: none; overflow: hidden; }
.lush-house-banner[hidden] { display: none; }
`;
  document.head.append(s);
}
