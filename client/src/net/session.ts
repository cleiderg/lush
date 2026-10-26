import type { EquipSlot, ItemActionKind } from '../protocol';
import {
  decode, encodeAttackTarget, encodeHello, encodeItemAction, encodePickUp, encodeShopBuy, encodeShopSell,
  encodeTradeAccept, encodeTradeCancel, encodeTradeOffer, encodeTradeRemove, encodeTradeRequest, encodeTradeSetGold,
  type ServerMessage,
} from './codec';
import type { Transport, TransportFactory } from './transport';

export const TOKEN_KEY = 'lush.guestToken';
const BACKOFF_BASE_MS = 500;
const BACKOFF_MAX_MS = 15000;

export interface TokenStore {
  get(): string;
  set(token: string): void;
}

/** Guest token in localStorage; storage can throw (private mode, disabled), so every access is guarded. */
export function localTokenStore(): TokenStore {
  return {
    get() {
      try {
        return localStorage.getItem(TOKEN_KEY) ?? '';
      } catch {
        return '';
      }
    },
    set(token) {
      try {
        localStorage.setItem(TOKEN_KEY, token);
      } catch {
        /* ignore */
      }
    },
  };
}

export interface SessionOptions {
  factory: TransportFactory;
  store: TokenStore;
  onMessage: (msg: ServerMessage) => void;
  onReload?: () => void;
  onStatus?: (connected: boolean) => void;
  setTimer?: (fn: () => void, ms: number) => unknown;
  clearTimer?: (id: unknown) => void;
}

/** Connects, says Hello with the stored token, and reconnects with exponential backoff when dropped. */
export class Session {
  private transport: Transport | null = null;
  private attempts = 0;
  private timer: unknown = null;
  private stopped = false;
  private ready = false;
  private readonly setTimer: (fn: () => void, ms: number) => unknown;
  private readonly clearTimer: (id: unknown) => void;

  constructor(private readonly opts: SessionOptions) {
    this.setTimer = opts.setTimer ?? ((fn, ms) => setTimeout(fn, ms));
    this.clearTimer = opts.clearTimer ?? ((id) => clearTimeout(id as ReturnType<typeof setTimeout>));
  }

  get connected(): boolean {
    return this.ready;
  }

  start(): void {
    this.stopped = false;
    this.connect();
  }

  stop(): void {
    this.stopped = true;
    if (this.timer !== null) this.clearTimer(this.timer);
    this.timer = null;
    this.transport?.close();
    this.transport = null;
  }

  send(bytes: Uint8Array): void {
    if (this.ready) this.transport?.send(bytes);
  }

  /** Equip or Eat the item in inventory slot `slot`, or Unequip `equipSlot`. */
  sendItemAction(action: ItemActionKind, slot: number, equipSlot?: EquipSlot): void {
    this.send(encodeItemAction(action, slot, equipSlot));
  }

  sendShopBuy(itemId: number, quantity: number): void {
    this.send(encodeShopBuy(itemId, quantity));
  }

  sendShopSell(slot: number, quantity: number): void {
    this.send(encodeShopSell(slot, quantity));
  }

  sendPickUp(groundItemId: number): void {
    this.send(encodePickUp(groundItemId));
  }

  sendAttackTarget(targetId: number): void {
    this.send(encodeAttackTarget(targetId));
  }

  /** Ask targetId to trade, or accept their pending request. */
  sendTradeRequest(targetId: number): void {
    this.send(encodeTradeRequest(targetId));
  }

  sendTradeOffer(slot: number, quantity: number): void {
    this.send(encodeTradeOffer(slot, quantity));
  }

  sendTradeRemove(index: number): void {
    this.send(encodeTradeRemove(index));
  }

  sendTradeSetGold(gold: number): void {
    this.send(encodeTradeSetGold(gold));
  }

  sendTradeAccept(screen: number): void {
    this.send(encodeTradeAccept(screen));
  }

  sendTradeCancel(): void {
    this.send(encodeTradeCancel());
  }

  private connect(): void {
    this.timer = null;
    const t = this.opts.factory();
    this.transport = t;
    t.onOpen = () => t.send(encodeHello(this.opts.store.get()));
    t.onMessage = (bytes) => {
      const msg = decode(bytes);
      if (!msg) return;
      if (msg.type === 'welcome') {
        this.opts.store.set(msg.guestToken);
        this.attempts = 0;
        this.ready = true;
        this.opts.onStatus?.(true);
      } else if (msg.type === 'reload') {
        this.stop();
        this.opts.onReload?.();
        return;
      }
      this.opts.onMessage(msg);
    };
    t.onClose = () => {
      if (this.transport !== t) return;
      this.transport = null;
      if (this.ready) this.opts.onStatus?.(false);
      this.ready = false;
      if (this.stopped) return;
      const delay = Math.min(BACKOFF_MAX_MS, BACKOFF_BASE_MS * 2 ** this.attempts);
      this.attempts++;
      this.timer = this.setTimer(() => this.connect(), delay);
    };
  }
}
