import areas from '../../../shared/map/areas.json';
import { encodeShopBuy, encodeShopSell } from '../net/codec';
import type { ItemState } from '../state/items';
import { nearShop, shopResultText, type Point } from '../state/shop';
import { hudLayout, place } from './hudLayout';

/** The Campus Market stall from shared/map/areas.json, in metres (the file is in decimetres). */
export const SHOP_STALL: Point = { x: areas.shop.x / 10, z: areas.shop.z / 10 };

export interface ShopPanelOptions {
  parent: HTMLElement;
  items: ItemState;
  /** Sends an encoded client message (ShopBuy, ShopSell) to the server. */
  send: (bytes: Uint8Array) => void;
  /** The local player's position in metres. */
  position: () => Point;
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
  const b = el('button', cls, text);
  b.type = 'button';
  b.addEventListener('click', onClick);
  return b;
}

/** Campus Market panel: buy what the shop sells, sell inventory items, and show each ShopResult. */
export class ShopPanel {
  readonly panel: HTMLElement;
  readonly message: HTMLElement;
  private readonly buyList: HTMLElement;
  private readonly sellList: HTMLElement;
  private readonly stall: Point;
  /** ShopResult seq already seen when the panel opened; older results are not shown. */
  private seenSeq = 0;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: ShopPanelOptions) {
    injectStyle();
    this.stall = opts.stall ?? SHOP_STALL;
    this.panel = el('div', 'lush-shop');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Campus Market');
    this.panel.hidden = true;
    const head = el('div', 'lush-shop-head');
    head.append(el('span', 'lush-shop-title', 'Campus Market'), button('lush-shop-close', 'Close', () => this.close()));
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
    if (!nearShop(this.opts.position(), this.stall)) return false;
    if (!this.isOpen) {
      this.seenSeq = this.opts.items.shopResult?.seq ?? 0;
      this.panel.hidden = false;
      this.opts.onOpen?.();
      this.render();
    }
    return true;
  }

  close(): void {
    this.panel.hidden = true;
  }

  /** Call each frame: closes the panel once the player walks out of range. */
  update(): void {
    if (this.isOpen && !nearShop(this.opts.position(), this.stall)) this.close();
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

    const forSale = [...items.catalogItems()].filter((d) => d.shopSells).sort((a, b) => a.shopSellsFor - b.shopSellsFor || a.id - b.id);
    this.buyList.replaceChildren(...forSale.map((d) => {
      const row = el('div', 'lush-shop-row');
      row.dataset.item = String(d.id);
      row.append(el('span', 'lush-shop-name', d.name), el('span', 'lush-shop-price', `${d.shopSellsFor} gp`),
        button('lush-shop-buy', 'Buy', () => this.opts.send(encodeShopBuy(d.id, 1))));
      return row;
    }));

    const rows: HTMLElement[] = [];
    items.inventory.slots.forEach((slot, i) => {
      if (!slot.itemId) return;
      const d = items.item(slot.itemId);
      const row = el('div', 'lush-shop-row');
      row.dataset.slot = String(i);
      const name = d?.name ?? `Item ${slot.itemId}`;
      row.append(el('span', 'lush-shop-name', slot.quantity > 1 ? `${name} ×${slot.quantity}` : name),
        el('span', 'lush-shop-price', `${d?.shopBuysFor ?? 0} gp`),
        button('lush-shop-sell', 'Sell', () => this.opts.send(encodeShopSell(i, 1))));
      if (slot.quantity > 1) {
        row.append(button('lush-shop-sell-all', 'All', () => this.opts.send(encodeShopSell(i, slot.quantity))));
      }
      rows.push(row);
    });
    this.sellList.replaceChildren(...rows);
  }

  dispose(): void {
    this.unsubscribe();
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
.lush-shop { box-sizing: border-box; z-index: 6; display: flex; flex-direction: column; gap: 6px; padding: 8px; border-radius: 12px; background: rgba(29,29,38,.96); color: #fff; font: 13px/16px system-ui, sans-serif; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-shop[hidden] { display: none; }
.lush-shop-head { display: flex; align-items: center; justify-content: space-between; }
.lush-shop-title { font-weight: 700; font-size: 15px; }
.lush-shop-msg { min-height: 16px; color: #ffd54f; font-weight: 600; }
.lush-shop-sub { color: #aab; font-size: 11px; text-transform: uppercase; }
.lush-shop-list { display: flex; flex-direction: column; gap: 4px; }
.lush-shop-row { display: flex; align-items: center; gap: 6px; padding: 4px 6px; border-radius: 6px; background: #3b3b52; }
.lush-shop-name { flex: 1; min-width: 0; word-break: break-word; }
.lush-shop-price { color: #ffd54f; white-space: nowrap; }
.lush-shop button { min-width: 44px; min-height: 32px; border: 0; border-radius: 8px; background: #4a8f4a; color: #fff; font: 600 13px sans-serif; touch-action: manipulation; }
.lush-shop .lush-shop-close { background: #5a3b3b; }
`;
  document.head.append(s);
}
