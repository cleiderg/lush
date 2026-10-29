import type { CardTableStore } from '../state/cardTables';
import { hudLayout, place } from './hudLayout';

/** The card table messages the panel sends; Session implements this. */
export interface CardTableSender {
  sendCardTableSit(tableId: number): void;
  sendCardTableReady(): void;
  sendCardTableLeave(): void;
}

export interface CardTablePanelOptions {
  parent: HTMLElement;
  tables: CardTableStore;
  session: CardTableSender;
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

/**
 * One card table (docs/specs/card-tables.md): its stake and seat count from
 * the catalog, each seat's player and ready flag, and Sit, Ready and Leave.
 * The server decides every action; the buttons only follow the store.
 */
export class CardTablePanel {
  readonly panel: HTMLElement;
  readonly title: HTMLElement;
  readonly stake: HTMLElement;
  readonly message: HTMLElement;
  readonly sitBtn: HTMLButtonElement;
  readonly readyBtn: HTMLButtonElement;
  readonly leaveBtn: HTMLButtonElement;
  private readonly status: HTMLElement;
  private readonly seatList: HTMLElement;
  private tableId = 0;
  /** CardTableResult seq already seen when the panel opened; older results are not shown. */
  private seenSeq = 0;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: CardTablePanelOptions) {
    injectStyle();
    this.panel = el('div', 'lush-ctable');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Card table');
    this.panel.hidden = true;
    const head = el('div', 'lush-ctable-head');
    this.title = el('span', 'lush-ctable-title');
    head.append(this.title, button('lush-ctable-close', 'Close', () => this.close()));
    this.stake = el('div', 'lush-ctable-stake');
    this.status = el('div', 'lush-ctable-status');
    this.message = el('div', 'lush-ctable-msg');
    this.message.setAttribute('role', 'status');
    this.seatList = el('div', 'lush-ctable-seats');
    this.sitBtn = button('lush-ctable-sit', 'Sit', () => this.opts.session.sendCardTableSit(this.tableId));
    this.readyBtn = button('lush-ctable-ready', 'Ready', () => this.opts.session.sendCardTableReady());
    this.leaveBtn = button('lush-ctable-leave', 'Leave', () => {
      this.opts.tables.noteLeave();
      this.opts.session.sendCardTableLeave();
    });
    const actions = el('div', 'lush-ctable-actions');
    actions.append(this.sitBtn, this.readyBtn, this.leaveBtn);
    this.panel.append(head, this.stake, this.status, this.message, this.seatList, actions);
    opts.parent.append(this.panel);
    this.unsubscribe = opts.tables.subscribe(() => this.render());
    window.addEventListener('resize', this.onResize);
    this.layout();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  /** The table the panel shows (0 before the first open). */
  get table(): number {
    return this.tableId;
  }

  /** Shows table tableId; calls onOpen. */
  open(tableId: number): void {
    if (!this.isOpen || tableId !== this.tableId) this.seenSeq = this.opts.tables.result?.seq ?? 0;
    this.tableId = tableId;
    this.panel.hidden = false;
    this.opts.onOpen?.();
    this.render();
  }

  close(): void {
    this.panel.hidden = true;
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    place(this.panel, hudLayout(v.width, v.height).cardTable);
  }

  render(): void {
    if (!this.isOpen) return;
    const store = this.opts.tables;
    const info = store.catalog.get(this.tableId);
    const view = store.tables.get(this.tableId);
    const mine = store.mySeat?.tableId === this.tableId ? store.mySeat.seat : -1;
    const inProgress = view?.inProgress ?? false;

    this.title.textContent = `Card table ${this.tableId}`;
    this.stake.textContent = info ? `Stake: ${info.stake} gold · ${info.seats} seats` : 'Stake: unknown';
    this.status.textContent = inProgress ? 'Game in progress' : 'Waiting for players';

    const seatCount = Math.max(info?.seats ?? 0, view?.seats.length ?? 0);
    const rows: HTMLElement[] = [];
    for (let i = 0; i < seatCount; i++) {
      const id = view?.seats[i] ?? 0;
      const row = el('div', 'lush-ctable-seat');
      row.dataset.seat = String(i);
      const who = id === 0 ? 'Empty' : i === mine ? 'You' : `Player ${id}`;
      const ready = id !== 0 && (view?.ready[i] ?? false);
      row.append(el('span', 'lush-ctable-who', `Seat ${i + 1}: ${who}`),
        el('span', 'lush-ctable-flag', id === 0 ? '' : ready ? 'Ready' : 'Not ready'));
      rows.push(row);
    }
    this.seatList.replaceChildren(...rows);

    this.sitBtn.hidden = store.mySeat !== null || inProgress;
    this.readyBtn.hidden = mine < 0 || inProgress;
    this.leaveBtn.hidden = mine < 0;

    const result = store.result;
    this.message.textContent = result && result.seq > this.seenSeq ? result.message : '';
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
.lush-ctable { box-sizing: border-box; z-index: 6; display: flex; flex-direction: column; gap: 6px; padding: 8px; border-radius: 12px; background: rgba(29,29,38,.96); color: #fff; font: 13px/16px system-ui, sans-serif; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-ctable[hidden], .lush-ctable button[hidden] { display: none; }
.lush-ctable-head { display: flex; align-items: center; justify-content: space-between; }
.lush-ctable-title { font-weight: 700; font-size: 15px; }
.lush-ctable-stake { color: #ffd54f; font-weight: 600; }
.lush-ctable-status { color: #aab; font-size: 11px; text-transform: uppercase; }
.lush-ctable-msg { min-height: 16px; color: #ffd54f; font-weight: 600; }
.lush-ctable-seats { display: flex; flex-direction: column; gap: 4px; }
.lush-ctable-seat { display: flex; align-items: center; gap: 6px; padding: 4px 6px; border-radius: 6px; background: #3b3b52; min-height: 28px; }
.lush-ctable-who { flex: 1; min-width: 0; word-break: break-word; }
.lush-ctable-flag { color: #9fd89f; white-space: nowrap; }
.lush-ctable-actions { display: flex; gap: 6px; }
.lush-ctable button { min-width: 44px; min-height: 32px; border: 0; border-radius: 8px; background: #4a8f4a; color: #fff; font: 600 13px sans-serif; touch-action: manipulation; }
.lush-ctable .lush-ctable-close, .lush-ctable .lush-ctable-leave { background: #5a3b3b; }
`;
  document.head.append(s);
}
