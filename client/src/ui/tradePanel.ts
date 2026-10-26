import {
  encodeTradeAccept, encodeTradeCancel, encodeTradeOffer, encodeTradeRemove, encodeTradeRequest, encodeTradeSetGold,
} from '../net/codec';
import type { ItemState } from '../state/items';
import { tradeResultText, type TradeItemState, type TradeStore } from '../state/trade';
import { hudLayout, place } from './hudLayout';

export interface TradePanelOptions {
  parent: HTMLElement;
  trade: TradeStore;
  items: ItemState;
  /** Sends an encoded client message (TradeRequest, TradeOffer, ...) to the server. */
  send: (bytes: Uint8Array) => void;
  viewport?: () => { width: number; height: number };
  /** Called when the trade window opens, so other panels in its place can close. */
  onOpen?: () => void;
  /** How long the result message stays up, in ms. */
  resultMs?: number;
}

/** Largest gold amount TradeSetGold can carry (uint32). */
const MAX_GOLD = 0xffffffff;

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

function playerName(id: number): string {
  return `Player ${id}`;
}

/**
 * Player trading: the request prompt (Accept / Decline), the trade window with
 * screen 1 (offer) and screen 2 (final confirmation), and the result message.
 */
export class TradePanel {
  readonly prompt: HTMLElement;
  readonly panel: HTMLElement;
  readonly message: HTMLElement;
  private readonly promptText: HTMLElement;
  private readonly title: HTMLElement;
  private readonly hint: HTMLElement;
  private readonly myList: HTMLElement;
  private readonly myGold: HTMLElement;
  private readonly myStatus: HTMLElement;
  private readonly partnerHead: HTMLElement;
  private readonly partnerList: HTMLElement;
  private readonly partnerGold: HTMLElement;
  private readonly partnerStatus: HTMLElement;
  private readonly offerArea: HTMLElement;
  private readonly goldInput: HTMLInputElement;
  private readonly invList: HTMLElement;
  private readonly acceptBtn: HTMLButtonElement;
  /** Request seq the player already answered (or that arrived before this panel). */
  private answeredSeq: number;
  /** Result seq already shown (or that arrived before this panel). */
  private shownResultSeq: number;
  private wasOpen = false;
  private messageTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: TradePanelOptions) {
    injectStyle();
    this.answeredSeq = 0;
    this.shownResultSeq = opts.trade.result?.seq ?? 0;

    this.prompt = el('div', 'lush-trade-prompt');
    this.prompt.setAttribute('role', 'alertdialog');
    this.prompt.setAttribute('aria-label', 'Trade request');
    this.prompt.hidden = true;
    this.promptText = el('div', 'lush-trade-prompt-text');
    const promptBtns = el('div', 'lush-trade-btns');
    promptBtns.append(
      button('lush-trade-accept lush-trade-prompt-accept', 'Accept', () => this.acceptRequest()),
      button('lush-trade-cancel lush-trade-prompt-decline', 'Decline', () => this.declineRequest()),
    );
    this.prompt.append(this.promptText, promptBtns);

    this.panel = el('div', 'lush-trade');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Trade');
    this.panel.hidden = true;
    this.title = el('div', 'lush-trade-title');
    this.hint = el('div', 'lush-trade-hint');

    const mine = el('div', 'lush-trade-side lush-trade-mine');
    this.myList = el('div', 'lush-trade-list');
    this.myGold = el('div', 'lush-trade-gold');
    this.myStatus = el('div', 'lush-trade-status');
    mine.append(el('div', 'lush-trade-sub', 'Your offer'), this.myList, this.myGold, this.myStatus);

    const theirs = el('div', 'lush-trade-side lush-trade-theirs');
    this.partnerHead = el('div', 'lush-trade-sub');
    this.partnerList = el('div', 'lush-trade-list');
    this.partnerGold = el('div', 'lush-trade-gold');
    this.partnerStatus = el('div', 'lush-trade-status');
    theirs.append(this.partnerHead, this.partnerList, this.partnerGold, this.partnerStatus);

    const sides = el('div', 'lush-trade-sides');
    sides.append(mine, theirs);

    this.offerArea = el('div', 'lush-trade-offer');
    const goldRow = el('form', 'lush-trade-gold-row');
    this.goldInput = el('input', 'lush-trade-gold-input');
    this.goldInput.type = 'number';
    this.goldInput.min = '0';
    this.goldInput.step = '1';
    this.goldInput.inputMode = 'numeric';
    this.goldInput.setAttribute('aria-label', 'Gold to offer');
    const setGold = el('button', 'lush-trade-set-gold', 'Set gold');
    setGold.type = 'submit';
    goldRow.append(this.goldInput, setGold);
    goldRow.addEventListener('submit', (e) => {
      e.preventDefault();
      this.submitGold();
    });
    this.invList = el('div', 'lush-trade-list lush-trade-inv');
    this.offerArea.append(goldRow, el('div', 'lush-trade-sub', 'Your inventory'), this.invList);

    const btns = el('div', 'lush-trade-btns');
    this.acceptBtn = button('lush-trade-accept', 'Accept', () => this.accept());
    btns.append(this.acceptBtn, button('lush-trade-cancel', 'Cancel', () => opts.send(encodeTradeCancel())));
    this.panel.append(this.title, this.hint, sides, this.offerArea, btns);

    this.message = el('div', 'lush-trade-msg');
    this.message.setAttribute('role', 'status');
    this.message.hidden = true;

    opts.parent.append(this.prompt, this.panel, this.message);
    const unsubTrade = opts.trade.subscribe(() => this.render());
    const unsubItems = opts.items.subscribe(() => this.render());
    this.unsubscribe = () => {
      unsubTrade();
      unsubItems();
    };
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  get promptOpen(): boolean {
    return !this.prompt.hidden;
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    const l = hudLayout(v.width, v.height);
    place(this.panel, l.trade);
    place(this.prompt, l.tradePrompt);
    place(this.message, { ...l.tradePrompt, h: 40 });
  }

  private acceptRequest(): void {
    const req = this.opts.trade.pendingRequest;
    if (!req) return;
    this.answeredSeq = req.seq;
    this.opts.send(encodeTradeRequest(req.fromId));
    this.render();
  }

  private declineRequest(): void {
    this.answeredSeq = this.opts.trade.pendingRequest?.seq ?? this.answeredSeq;
    this.render();
  }

  private accept(): void {
    const open = this.opts.trade.open;
    if (open) this.opts.send(encodeTradeAccept(open.screen));
  }

  private submitGold(): void {
    const n = Math.floor(Number(this.goldInput.value));
    if (!Number.isFinite(n) || n < 0) return;
    this.opts.send(encodeTradeSetGold(Math.min(n, MAX_GOLD)));
  }

  private itemName(it: TradeItemState): string {
    const name = this.opts.items.item(it.itemId)?.name ?? `Item ${it.itemId}`;
    return it.quantity > 1 ? `${name} ×${it.quantity}` : name;
  }

  /** One offer list; on screen 1 your own items are buttons that take the item back. */
  private offerRows(list: TradeItemState[], removable: boolean): HTMLElement[] {
    if (list.length === 0) return [el('div', 'lush-trade-empty', 'Nothing')];
    return list.map((it, i) => {
      if (!removable) {
        const row = el('div', 'lush-trade-row', this.itemName(it));
        row.dataset.index = String(i);
        return row;
      }
      const row = button('lush-trade-row', this.itemName(it), () => this.opts.send(encodeTradeRemove(i)));
      row.dataset.index = String(i);
      return row;
    });
  }

  private showMessage(text: string): void {
    this.message.textContent = text;
    this.message.hidden = false;
    if (this.messageTimer) clearTimeout(this.messageTimer);
    this.messageTimer = setTimeout(() => {
      this.message.hidden = true;
      this.messageTimer = null;
    }, this.opts.resultMs ?? 3000);
  }

  render(): void {
    const t = this.opts.trade;
    if (t.result && t.result.seq > this.shownResultSeq) {
      this.shownResultSeq = t.result.seq;
      this.showMessage(tradeResultText(t.result.code));
    }

    const req = t.pendingRequest;
    const showPrompt = !t.open && !!req && req.seq > this.answeredSeq;
    this.prompt.hidden = !showPrompt;
    if (showPrompt && req) this.promptText.textContent = `${playerName(req.fromId)} wants to trade with you.`;

    const open = t.open;
    this.panel.hidden = !open;
    if (!open) {
      this.wasOpen = false;
      return;
    }
    if (!this.wasOpen) {
      this.wasOpen = true;
      this.goldInput.value = String(open.myGold);
      this.opts.onOpen?.();
    }

    const offering = open.screen === 1;
    this.panel.dataset.screen = String(open.screen);
    const partner = playerName(open.partnerId);
    this.title.textContent = `Trading with ${partner}`;
    this.hint.textContent = offering
      ? 'Tap an item to offer it; tap your offer to take it back.'
      : 'Check both offers carefully before you accept.';
    this.partnerHead.textContent = `${partner}'s offer`;
    this.myList.replaceChildren(...this.offerRows(open.myItems, offering));
    this.partnerList.replaceChildren(...this.offerRows(open.partnerItems, false));
    this.myGold.textContent = `${open.myGold} gp`;
    this.partnerGold.textContent = `${open.partnerGold} gp`;
    this.myStatus.textContent = open.myAccepted ? 'Accepted' : '';
    this.partnerStatus.textContent = open.partnerAccepted ? 'Accepted' : '';
    this.acceptBtn.disabled = open.myAccepted;

    this.offerArea.hidden = !offering;
    if (!offering) {
      this.invList.replaceChildren();
      return;
    }
    if (document.activeElement !== this.goldInput) this.goldInput.value = String(open.myGold);
    const rows: HTMLElement[] = [];
    this.opts.items.inventory.slots.forEach((slot, i) => {
      if (!slot.itemId) return;
      const row = button('lush-trade-row', this.itemName(slot), () => this.opts.send(encodeTradeOffer(i, slot.quantity)));
      row.dataset.slot = String(i);
      rows.push(row);
    });
    this.invList.replaceChildren(...(rows.length ? rows : [el('div', 'lush-trade-empty', 'Nothing to offer')]));
  }

  dispose(): void {
    this.unsubscribe();
    if (this.messageTimer) clearTimeout(this.messageTimer);
    window.removeEventListener('resize', this.onResize);
    this.prompt.remove();
    this.panel.remove();
    this.message.remove();
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-trade, .lush-trade-prompt, .lush-trade-msg { box-sizing: border-box; z-index: 7; border-radius: 12px; background: rgba(29,29,38,.96); color: #fff; font: 13px/16px system-ui, sans-serif; user-select: none; -webkit-user-select: none; }
.lush-trade { display: flex; flex-direction: column; gap: 6px; padding: 8px; overflow-y: auto; touch-action: pan-y; }
.lush-trade-prompt { z-index: 8; display: flex; flex-direction: column; justify-content: center; gap: 8px; padding: 8px; }
.lush-trade-msg { z-index: 9; display: flex; align-items: center; justify-content: center; padding: 4px 8px; color: #ffd54f; font-weight: 600; pointer-events: none; }
.lush-trade[hidden], .lush-trade-prompt[hidden], .lush-trade-msg[hidden], .lush-trade-offer[hidden] { display: none; }
.lush-trade-title { font-weight: 700; font-size: 15px; }
.lush-trade-hint { color: #aab; font-size: 12px; }
.lush-trade-sides { display: flex; gap: 6px; }
.lush-trade-side { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; padding: 6px; border-radius: 8px; background: #2a2a3a; }
.lush-trade-sub { color: #aab; font-size: 11px; text-transform: uppercase; }
.lush-trade-list { display: flex; flex-direction: column; gap: 4px; }
.lush-trade-row { display: block; width: 100%; text-align: left; padding: 6px; border-radius: 6px; background: #3b3b52; color: #fff; word-break: break-word; }
.lush-trade-empty { color: #778; }
.lush-trade-gold { color: #ffd54f; }
.lush-trade-status { min-height: 16px; color: #8fdf8f; font-weight: 600; }
.lush-trade-offer { display: flex; flex-direction: column; gap: 6px; }
.lush-trade-gold-row { display: flex; gap: 6px; margin: 0; }
.lush-trade-gold-input { flex: 1; min-width: 0; min-height: 32px; border: 0; border-radius: 8px; padding: 0 8px; font: 16px sans-serif; }
.lush-trade-btns { display: flex; gap: 8px; }
.lush-trade button { min-height: 32px; border: 0; border-radius: 8px; font: 600 13px sans-serif; touch-action: manipulation; }
.lush-trade-btns button, .lush-trade-set-gold { flex: 1; min-height: 40px; border: 0; border-radius: 8px; background: #4a8f4a; color: #fff; font: 600 14px sans-serif; touch-action: manipulation; }
.lush-trade-btns .lush-trade-cancel { background: #8f3b3b; }
.lush-trade-btns button:disabled { opacity: .5; }
`;
  document.head.append(s);
}
