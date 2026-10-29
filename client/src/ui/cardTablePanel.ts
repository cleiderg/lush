import { ShedColour } from '../protocol';
import type { CardTableStore, ShedCardValue } from '../state/cardTables';
import { hudLayout, place } from './hudLayout';
import { SHED_COLOURS, isWild, shedCardElement, shedColourCss, shedColourName } from './shedCards';

/** The card table messages the panel sends; Session implements this. */
export interface CardTableSender {
  sendCardTableSit(tableId: number): void;
  sendCardTableReady(): void;
  sendCardTableLeave(): void;
  sendShedPlay(card: ShedCardValue, chosenColour?: ShedColour): void;
  sendShedDraw(): void;
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
 * During a Shed game it also shows the top card, the colour to match, whose
 * turn it is, hand counts, the draw pile and the local hand, with Play
 * (tap a card; a Wild first asks for a colour) and Draw on your turn.
 * The server decides every action, including whether a move is legal; the
 * buttons only follow the store.
 */
export class CardTablePanel {
  readonly panel: HTMLElement;
  readonly title: HTMLElement;
  readonly stake: HTMLElement;
  readonly message: HTMLElement;
  readonly sitBtn: HTMLButtonElement;
  readonly readyBtn: HTMLButtonElement;
  readonly leaveBtn: HTMLButtonElement;
  /** The Shed game view; hidden outside a game at this table. */
  readonly game: HTMLElement;
  readonly topCard: HTMLElement;
  readonly colour: HTMLElement;
  readonly turn: HTMLElement;
  readonly drawPile: HTMLElement;
  readonly hand: HTMLElement;
  readonly drawBtn: HTMLButtonElement;
  /** Colour choice for a Wild; hidden unless one is pending. */
  readonly picker: HTMLElement;
  /** The Wild waiting for a colour choice, or null. */
  private pendingWild: ShedCardValue | null = null;
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

    this.game = el('div', 'lush-shed');
    this.game.hidden = true;
    const pile = el('div', 'lush-shed-pile');
    this.topCard = el('div', 'lush-shed-top');
    const info = el('div', 'lush-shed-info');
    this.colour = el('div', 'lush-shed-colour');
    this.turn = el('div', 'lush-shed-turn');
    this.drawPile = el('div', 'lush-shed-drawpile');
    info.append(this.colour, this.turn, this.drawPile);
    pile.append(this.topCard, info);
    this.hand = el('div', 'lush-shed-hand');
    this.hand.setAttribute('aria-label', 'Your hand');
    this.drawBtn = button('lush-shed-draw', 'Draw', () => this.draw());
    this.picker = el('div', 'lush-shed-picker');
    this.picker.hidden = true;
    this.picker.append(el('span', 'lush-shed-picker-title', 'Choose a colour'));
    for (const c of SHED_COLOURS) {
      const b = button('lush-shed-pick', shedColourName(c), () => this.chooseColour(c));
      const css = shedColourCss(c);
      b.style.backgroundColor = css.bg;
      b.style.color = css.fg;
      b.dataset.colour = String(c);
      this.picker.append(b);
    }
    this.picker.append(button('lush-shed-cancel', 'Cancel', () => this.cancelWild()));
    this.game.append(pile, this.hand, this.drawBtn, this.picker);

    this.panel.append(head, this.stake, this.status, this.message, this.seatList, this.game, actions);
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
    this.pendingWild = null;
  }

  /** True when the local player is seated here, a game runs and it is their turn. */
  get myTurn(): boolean {
    const store = this.opts.tables;
    const view = store.tables.get(this.tableId);
    const mine = store.mySeat?.tableId === this.tableId ? store.mySeat.seat : -1;
    return mine >= 0 && !!view?.inProgress && view.turnSeat === mine;
  }

  /** Plays card on your turn; a Wild first opens the colour choice. */
  play(card: ShedCardValue): void {
    if (!this.myTurn) return;
    if (isWild(card)) {
      this.pendingWild = { ...card };
      this.render();
      return;
    }
    this.opts.session.sendShedPlay(card, ShedColour.None);
  }

  /** Draws a card on your turn. */
  draw(): void {
    if (!this.myTurn) return;
    this.pendingWild = null;
    this.opts.session.sendShedDraw();
    this.render();
  }

  /** Sends the pending Wild with colour c. */
  chooseColour(c: ShedColour): void {
    const card = this.pendingWild;
    this.pendingWild = null;
    if (card && this.myTurn) this.opts.session.sendShedPlay(card, c);
    this.render();
  }

  cancelWild(): void {
    this.pendingWild = null;
    this.render();
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

    const shed = inProgress ? store.shed.get(this.tableId) : undefined;
    const seatCount = Math.max(info?.seats ?? 0, view?.seats.length ?? 0);
    const rows: HTMLElement[] = [];
    for (let i = 0; i < seatCount; i++) {
      const id = view?.seats[i] ?? 0;
      const row = el('div', 'lush-ctable-seat');
      row.dataset.seat = String(i);
      const who = id === 0 ? 'Empty' : i === mine ? 'You' : `Player ${id}`;
      const ready = id !== 0 && (view?.ready[i] ?? false);
      row.append(el('span', 'lush-ctable-who', `Seat ${i + 1}: ${who}`));
      if (shed) {
        const n = shed.handCounts[i] ?? 0;
        row.classList.toggle('lush-ctable-turn', view?.turnSeat === i);
        row.append(el('span', 'lush-ctable-count', id === 0 ? '' : `${n} card${n === 1 ? '' : 's'}`));
      } else {
        row.append(el('span', 'lush-ctable-flag', id === 0 ? '' : ready ? 'Ready' : 'Not ready'));
      }
      rows.push(row);
    }
    this.seatList.replaceChildren(...rows);

    this.sitBtn.hidden = store.mySeat !== null || inProgress;
    this.readyBtn.hidden = mine < 0 || inProgress;
    this.leaveBtn.hidden = mine < 0;

    this.renderGame(mine);

    const result = store.result;
    this.message.textContent = result && result.seq > this.seenSeq ? result.message : '';
  }

  private renderGame(mine: number): void {
    const store = this.opts.tables;
    const view = store.tables.get(this.tableId);
    const shed = view?.inProgress ? store.shed.get(this.tableId) : undefined;
    this.game.hidden = !shed;
    if (!shed || !view) {
      this.pendingWild = null;
      this.picker.hidden = true;
      return;
    }
    const myTurn = this.myTurn;
    if (!myTurn) this.pendingWild = null;

    this.topCard.replaceChildren(shedCardElement('div', shed.topCard));
    this.colour.textContent = `Colour: ${shedColourName(shed.colour)}`;
    const css = shedColourCss(shed.colour);
    this.colour.style.borderLeftColor = css.bg;
    const t = view.turnSeat;
    this.turn.textContent = t === null ? '' : t === mine ? 'Your turn' : `Seat ${t + 1}'s turn`;
    this.drawPile.textContent = `Draw pile: ${shed.drawPile}`;

    const cards = store.hand?.tableId === this.tableId ? store.hand.cards : [];
    this.hand.replaceChildren(...cards.map((c) => {
      const b = shedCardElement('button', c);
      b.type = 'button';
      b.disabled = !myTurn;
      b.addEventListener('click', () => this.play(c));
      return b;
    }));
    this.hand.hidden = mine < 0;
    this.drawBtn.hidden = mine < 0;
    this.drawBtn.disabled = !myTurn;
    this.picker.hidden = this.pendingWild === null;
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
.lush-ctable button:disabled { opacity: .5; }
.lush-ctable-turn { outline: 2px solid #ffd54f; }
.lush-ctable-count { color: #ccd; white-space: nowrap; }
.lush-shed { display: flex; flex-direction: column; gap: 6px; }
.lush-shed[hidden], .lush-shed [hidden] { display: none; }
.lush-shed-pile { display: flex; gap: 8px; align-items: center; }
.lush-shed-info { display: flex; flex-direction: column; gap: 2px; }
.lush-shed-colour { border-left: 6px solid transparent; padding-left: 4px; font-weight: 600; }
.lush-shed-turn { color: #ffd54f; font-weight: 600; }
.lush-shed-hand { display: flex; flex-wrap: wrap; gap: 4px; }
.lush-shed-card { box-sizing: border-box; display: flex; align-items: center; justify-content: center; width: 44px; height: 62px; padding: 2px; border: 2px solid #fff; border-radius: 6px; font: 700 13px/14px sans-serif; text-align: center; }
.lush-ctable button.lush-shed-card { min-width: 44px; padding: 2px; }
.lush-shed-picker { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; }
.lush-shed-picker-title { width: 100%; font-weight: 600; }
`;
  document.head.append(s);
}
