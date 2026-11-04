import areas from '../../../shared/map/areas.json';
import { encodeShopBuy, encodeShopSell } from '../net/codec';
import { ShopKind } from '../protocol';
import type { ItemInfo, ItemState } from '../state/items';
import { nearShop, shopResultText, type Point } from '../state/shop';
import { hudLayout, place } from './hudLayout';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

/** The Campus Market stall from shared/map/areas.json, in metres (the file is in decimetres). */
export const SHOP_STALL: Point = { x: areas.shop.x / 10, z: areas.shop.z / 10 };

export interface ShopPanelOptions {
  parent: HTMLElement;
  items: ItemState;
  /** Sends an encoded client message (ShopBuy, ShopSell) to the server. */
  send: (bytes: Uint8Array) => void;
  /** The local player's position in metres. */
  position: () => Point;
  /** Which stall this panel is: the Campus Market (default) or Campus Thrift, which has its own prices. */
  shop?: ShopKind;
  /** The stall in metres. Without one (Thrift until its stalls have positions) the panel opens only through open(). */
  stall?: Point;
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

/** Campus Market or Campus Thrift panel: buy what the stall sells, sell inventory items, and show each ShopResult. */
export class ShopPanel {
  readonly panel: HTMLElement;
  readonly message: HTMLElement;
  private readonly buyList: HTMLElement;
  private readonly sellList: HTMLElement;
  private readonly stall: Point | null;
  private readonly kind: ShopKind;
  private readonly title: string;
  /** ShopResult seq already seen when the panel opened; older results are not shown. */
  private seenSeq = 0;
  private readonly motion: PanelMotion;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: ShopPanelOptions) {
    injectStyle();
    this.kind = opts.shop ?? ShopKind.Market;
    this.title = this.kind === ShopKind.Thrift ? 'Campus Thrift' : 'Campus Market';
    this.stall = opts.stall ?? (this.kind === ShopKind.Market ? SHOP_STALL : null);
    this.panel = el('div', `lush-panel lush-shop${this.kind === ShopKind.Thrift ? ' lush-thrift' : ''}`);
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', this.title);
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    const head = el('div', 'lush-shop-head');
    head.append(el('span', 'lush-title lush-shop-title', this.title), button('lush-shop-close', 'Close', () => this.close()));
    this.message = el('div', 'lush-shop-msg');
    this.message.setAttribute('role', 'status');
    this.buyList = el('div', 'lush-shop-list');
    this.sellList = el('div', 'lush-shop-list');
    this.panel.append(head, this.message, el('div', 'lush-shop-sub', 'Buy'), this.buyList,
      el('div', 'lush-shop-sub', 'Sell'), this.sellList);
    opts.parent.append(this.panel);
    this.unsubscribe = opts.items.subscribe(() => this.render());
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  /** Interact pressed: opens the panel if the player is within the shop distance of the stall. */
  tryOpen(): boolean {
    if (!this.stall || !nearShop(this.opts.position(), this.stall)) return false;
    this.open();
    return true;
  }

  /** Opens the panel without the distance check (the offline shots; Thrift until its stalls have positions). */
  open(): void {
    if (this.isOpen) return;
    this.seenSeq = this.opts.items.shopResult?.seq ?? 0;
    this.motion.set(true);
    this.opts.onOpen?.();
    this.render();
  }

  close(): void {
    this.motion.set(false);
  }

  /** Call each frame: closes the panel once the player walks out of range. */
  update(): void {
    if (this.isOpen && this.stall && !nearShop(this.opts.position(), this.stall)) this.close();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.panel, hudLayout(v.width, v.height).shop);
  }

  render(): void {
    if (!this.isOpen) return;
    const items = this.opts.items;
    const result = items.shopResult;
    this.message.textContent = result && result.seq > this.seenSeq ? shopResultText(result.code) : '';

    const thrift = this.kind === ShopKind.Thrift;
    const sellsFor = (d: ItemInfo): number => (thrift ? d.thriftSellsFor : d.shopSells ? d.shopSellsFor : 0);
    const buysFor = (d: ItemInfo): number => (thrift ? d.thriftBuysFor : d.shopBuysFor);
    const forSale = [...items.catalogItems()].filter((d) => (thrift ? d.thriftSellsFor > 0 : d.shopSells)).sort((a, b) => sellsFor(a) - sellsFor(b) || a.id - b.id);
    this.buyList.replaceChildren(...forSale.map((d) => {
      const row = el('div', 'lush-shop-row');
      row.dataset.item = String(d.id);
      row.append(el('span', 'lush-shop-name', d.name), el('span', 'lush-shop-price', `${sellsFor(d)} gp`),
        button('lush-shop-buy', 'Buy', () => this.opts.send(encodeShopBuy(d.id, 1, this.kind))));
      return row;
    }));

    const rows: HTMLElement[] = [];
    items.inventory.slots.forEach((slot, i) => {
      if (!slot.itemId) return;
      const d = items.item(slot.itemId);
      if (thrift && !(d && d.thriftBuysFor > 0)) return;
      const row = el('div', 'lush-shop-row');
      row.dataset.slot = String(i);
      const name = d?.name ?? `Item ${slot.itemId}`;
      row.append(el('span', 'lush-shop-name', slot.quantity > 1 ? `${name} ×${slot.quantity}` : name),
        el('span', 'lush-shop-price', `${d ? buysFor(d) : 0} gp`),
        button('lush-shop-sell', 'Sell', () => this.opts.send(encodeShopSell(i, 1, this.kind))));
      if (slot.quantity > 1) {
        row.append(button('lush-shop-sell-all', 'All', () => this.opts.send(encodeShopSell(i, slot.quantity, this.kind))));
      }
      rows.push(row);
    });
    this.sellList.replaceChildren(...rows);
  }

  dispose(): void {
    this.unsubscribe();
    this.motion.dispose();
    window.removeEventListener('resize', this.onResize);
    this.panel.remove();
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-shop { box-sizing: border-box; z-index: 6; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: var(--lush-text-sm); line-height: 16px; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-shop[hidden] { display: none; }
.lush-shop-head { display: flex; align-items: center; justify-content: space-between; gap: var(--lush-space-2); }
.lush-shop-title { font-size: var(--lush-text-lg); }
.lush-shop-msg { min-height: 16px; color: var(--lush-gold-bright); font-weight: 600; }
.lush-shop-sub { color: var(--lush-text-muted); font: var(--lush-text-xs) var(--lush-font-title); font-variant: small-caps; letter-spacing: 0.08em; border-bottom: var(--lush-border); padding-bottom: 2px; }
.lush-shop-list { display: flex; flex-direction: column; gap: var(--lush-space-1); }
.lush-shop-row { display: flex; align-items: center; gap: var(--lush-space-2); padding: var(--lush-space-1) var(--lush-space-2); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); }
.lush-shop-name { flex: 1; min-width: 0; word-break: break-word; }
.lush-shop-price { color: var(--lush-gold); white-space: nowrap; }
.lush-shop .lush-shop-buy, .lush-shop .lush-shop-sell, .lush-shop .lush-shop-sell-all { background: var(--lush-active); }
`;
  document.head.append(s);
}
