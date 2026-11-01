import { BankAmount } from '../protocol';
import { BANK_BOOTH, filterBankSlots, nearBank, type BankStore } from '../state/bank';
import type { ItemState } from '../state/items';
import type { Point } from '../state/shop';
import { hudLayout, place } from './hudLayout';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

/** The Session calls the bank panel makes. */
export interface BankPanelSession {
  sendBankOpen: () => void;
  sendBankClose: () => void;
  /** amount is a count or BankAmount.All. */
  sendBankDeposit: (itemId: number, amount: number) => void;
  sendBankWithdraw: (itemId: number, amount: number) => void;
  sendBankDepositInventory: () => void;
  sendBankDepositEquipment: () => void;
}

export interface BankPanelOptions {
  parent: HTMLElement;
  bank: BankStore;
  items: ItemState;
  session: BankPanelSession;
  /** The local player's position in metres. */
  position: () => Point;
  booth?: Point;
  viewport?: () => { width: number; height: number };
  /** Called when the panel opens, so other panels in its place can close. */
  onOpen?: () => void;
}

/** The largest typed amount: one less than BankAmount.All, which means every one. */
export const MAX_TYPED_AMOUNT = BankAmount.All - 1;

/** The count in the amount box, or null if it is not a whole number from 1 up. */
export function parseAmount(text: string): number | null {
  const t = text.trim();
  if (!/^\d+$/.test(t)) return null;
  const n = Number(t);
  return n >= 1 ? Math.min(n, MAX_TYPED_AMOUNT) : null;
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

function input(cls: string, placeholder: string, label: string, mode: 'search' | 'numeric'): HTMLInputElement {
  const i = el('input', cls);
  i.type = mode === 'search' ? 'search' : 'text';
  i.inputMode = mode === 'search' ? 'search' : 'numeric';
  i.placeholder = placeholder;
  i.setAttribute('aria-label', label);
  i.autocomplete = 'off';
  // Typing must not move the character (WASD) or fire the desktop shortcuts.
  i.addEventListener('keydown', (e) => e.stopPropagation());
  i.addEventListener('keyup', (e) => e.stopPropagation());
  return i;
}

/**
 * The Bank panel: the bank's slots with a name search, and for each item buttons for one, five, all or a typed
 * amount, plus the same for the inventory items to deposit and Deposit inventory / Deposit equipment.
 * It shows while the BankStore is open (the server's BankState opens it), so a closing BankResult closes it.
 */
export class BankPanel {
  readonly panel: HTMLElement;
  readonly search: HTMLInputElement;
  readonly amount: HTMLInputElement;
  private readonly bankList: HTMLElement;
  private readonly invList: HTMLElement;
  private readonly booth: Point;
  private readonly motion: PanelMotion;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();
  private shown = false;

  constructor(private readonly opts: BankPanelOptions) {
    injectStyle();
    this.booth = opts.booth ?? BANK_BOOTH;
    this.panel = el('div', 'lush-panel lush-bank');
    this.panel.hidden = true;
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Bank');
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    const head = el('div', 'lush-bank-head');
    head.append(el('span', 'lush-title lush-bank-title', 'Bank'), button('lush-bank-close', 'Close', () => this.close()));
    this.search = input('lush-bank-search', 'Search items', 'Search the bank', 'search');
    this.search.addEventListener('input', () => this.render());
    this.amount = input('lush-bank-amount', 'Amount', 'Amount for the X buttons', 'numeric');
    const bulk = el('div', 'lush-bank-bulk');
    bulk.append(
      button('lush-bank-deposit-inventory', 'Deposit inventory', () => opts.session.sendBankDepositInventory()),
      button('lush-bank-deposit-equipment', 'Deposit equipment', () => opts.session.sendBankDepositEquipment()),
    );
    this.bankList = el('div', 'lush-bank-list');
    this.invList = el('div', 'lush-bank-list');
    this.panel.append(head, this.search, this.amount, bulk,
      el('div', 'lush-bank-sub', 'In the bank'), this.bankList, el('div', 'lush-bank-sub', 'Deposit from inventory'), this.invList);
    opts.parent.append(this.panel);
    this.unsubscribe = opts.bank.subscribe(() => this.sync());
    opts.items.subscribe(() => this.render());
    window.addEventListener('resize', this.onResize);
    this.layout();
  }

  get isOpen(): boolean {
    return this.shown;
  }

  /** Interact pressed at the booth: asks the server to open the bank; the panel shows when its BankState arrives. */
  open(): boolean {
    if (!nearBank(this.opts.position(), this.booth)) return false;
    this.opts.session.sendBankOpen();
    return true;
  }

  /** Closes the panel and tells the server. */
  close(): void {
    if (!this.opts.bank.isOpen) return;
    this.opts.session.sendBankClose();
    this.opts.bank.close();
  }

  /** Call each frame: closes the bank once the player walks out of range. */
  update(): void {
    if (this.shown && !nearBank(this.opts.position(), this.booth)) this.close();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.panel, hudLayout(v.width, v.height).bank);
  }

  /** Shows or hides the panel to match the bank store. */
  private sync(): void {
    const open = this.opts.bank.isOpen;
    if (open !== this.shown) {
      this.shown = open;
      this.motion.set(open);
      if (open) {
        this.search.value = '';
        this.opts.onOpen?.();
      }
    }
    this.render();
  }

  private amountButtons(onAmount: (amount: number) => void): HTMLElement {
    const box = el('span', 'lush-bank-amounts');
    box.append(
      button('lush-bank-1', '1', () => onAmount(1)),
      button('lush-bank-5', '5', () => onAmount(5)),
      button('lush-bank-all', 'All', () => onAmount(BankAmount.All)),
      button('lush-bank-x', 'X', () => {
        const n = parseAmount(this.amount.value);
        if (n === null) this.amount.focus();
        else onAmount(n);
      }),
    );
    return box;
  }

  render(): void {
    if (!this.shown) return;
    const { bank, items, session } = this.opts;
    const nameOf = (id: number): string => items.item(id)?.name ?? `Item ${id}`;

    this.bankList.replaceChildren(...filterBankSlots(bank.slots ?? [], this.search.value, nameOf).map((s) => {
      const row = el('div', 'lush-bank-row');
      row.dataset.item = String(s.itemId);
      row.append(el('span', 'lush-bank-name', nameOf(s.itemId)), el('span', 'lush-bank-qty', `×${s.quantity}`),
        this.amountButtons((n) => session.sendBankWithdraw(s.itemId, n)));
      return row;
    }));

    // Inventory items add up per item, since unstackable ones take a slot each.
    const held = new Map<number, number>();
    for (const slot of items.inventory.slots) if (slot.itemId) held.set(slot.itemId, (held.get(slot.itemId) ?? 0) + slot.quantity);
    this.invList.replaceChildren(...[...held].map(([itemId, quantity]) => {
      const row = el('div', 'lush-bank-row');
      row.dataset.inventoryItem = String(itemId);
      row.append(el('span', 'lush-bank-name', nameOf(itemId)), el('span', 'lush-bank-qty', `×${quantity}`),
        this.amountButtons((n) => session.sendBankDeposit(itemId, n)));
      return row;
    }));
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
.lush-bank { box-sizing: border-box; z-index: 6; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: var(--lush-text-sm); line-height: 16px; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-bank[hidden] { display: none; }
.lush-bank-head { display: flex; align-items: center; justify-content: space-between; gap: var(--lush-space-2); }
.lush-bank-title { font-size: var(--lush-text-lg); }
.lush-bank input { box-sizing: border-box; min-height: 44px; padding: 0 var(--lush-space-3); border: var(--lush-border-strong); border-radius: var(--lush-radius-sm); background: var(--lush-surface); color: var(--lush-text); font: var(--lush-text-sm) var(--lush-font-body); user-select: text; -webkit-user-select: text; }
.lush-bank-bulk { display: flex; gap: var(--lush-space-2); }
.lush-bank-bulk button { flex: 1; background: var(--lush-active); }
.lush-bank-sub { color: var(--lush-text-muted); font: var(--lush-text-xs) var(--lush-font-title); font-variant: small-caps; letter-spacing: 0.08em; border-bottom: var(--lush-border); padding-bottom: 2px; }
.lush-bank-list { display: flex; flex-direction: column; gap: var(--lush-space-1); }
.lush-bank-row { display: flex; flex-wrap: wrap; align-items: center; gap: var(--lush-space-1) var(--lush-space-2); padding: var(--lush-space-1) var(--lush-space-2); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); }
.lush-bank-name { flex: 1; min-width: 80px; word-break: break-word; }
.lush-bank-qty { color: var(--lush-gold); white-space: nowrap; }
.lush-bank-amounts { display: flex; gap: var(--lush-space-1); }
.lush-bank .lush-bank-amounts button { background: var(--lush-active); padding: 0 var(--lush-space-2); }
`;
  document.head.append(s);
}
