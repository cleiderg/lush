import type { ItemState } from '../state/items';
import type { WorkbenchRecipeInfo, WorkbenchStore } from '../state/workbench';
import { hudLayout, place } from './hudLayout';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

export interface WorkbenchPanelOptions {
  parent: HTMLElement;
  workbench: WorkbenchStore;
  items: ItemState;
  /** Sends a WorkbenchCraft request for the output item id. */
  sendCraft: (outputItemId: number) => void;
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

/** Quantity of an item across the inventory slots. */
export function heldQuantity(items: ItemState, itemId: number): number {
  let n = 0;
  for (const s of items.inventory.slots) if (s.itemId === itemId) n += s.quantity;
  return n;
}

/** True when the player holds every input and the gold for the recipe. */
export function canCraft(items: ItemState, recipe: WorkbenchRecipeInfo): boolean {
  return items.stats.gold >= recipe.gold && recipe.inputs.every((i) => heldQuantity(items, i.itemId) >= i.quantity);
}

/** Workbench panel: every recipe with inputs held over needed, the gold cost and a Craft button. */
export class WorkbenchPanel {
  readonly panel: HTMLElement;
  private readonly list: HTMLElement;
  private readonly motion: PanelMotion;
  private readonly unsubscribes: (() => void)[];
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: WorkbenchPanelOptions) {
    injectStyle();
    this.panel = el('div', 'lush-panel lush-bench');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Workbench');
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    const close = el('button', `lush-bench-close ${BUTTON_CLASS}`, 'Close');
    close.type = 'button';
    close.addEventListener('click', () => this.close());
    const head = el('div', 'lush-bench-head');
    head.append(el('span', 'lush-title lush-bench-title', 'Workbench'), close);
    this.list = el('div', 'lush-bench-list');
    this.panel.append(head, this.list);
    opts.parent.append(this.panel);
    this.unsubscribes = [opts.items.subscribe(() => this.render()), opts.workbench.subscribe(() => this.render())];
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  open(): void {
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
    place(this.panel, hudLayout(v.width, v.height).shop);
  }

    render(): void {
    if (!this.isOpen) return;
    const { items, workbench, sendCraft } = this.opts;
    this.list.replaceChildren(...workbench.recipes.map((r) => {
      const row = el('div', 'lush-bench-row');
      row.dataset.output = String(r.outputItemId);
      row.append(el('div', 'lush-bench-name', items.item(r.outputItemId)?.name ?? `Item ${r.outputItemId}`));
      const inputs = el('div', 'lush-bench-inputs');
      for (const i of r.inputs) {
        const held = heldQuantity(items, i.itemId);
        const line = el('span', `lush-bench-input${held >= i.quantity ? '' : ' lush-bench-short'}`,
          `${items.item(i.itemId)?.name ?? `Item ${i.itemId}`} ${held}/${i.quantity}`);
        line.dataset.input = String(i.itemId);
        inputs.append(line);
      }
      const gold = el('span', `lush-bench-gold${items.stats.gold >= r.gold ? '' : ' lush-bench-short'}`, `${r.gold} gp`);
      const craft = el('button', `lush-bench-craft ${BUTTON_CLASS}`, 'Craft');
      craft.type = 'button';
      craft.disabled = !canCraft(items, r);
      craft.addEventListener('click', () => {
        if (!craft.disabled) sendCraft(r.outputItemId);
      });
      row.append(inputs, gold, craft);
      return row;
    }));
  }

  dispose(): void {
    for (const u of this.unsubscribes) u();
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
.lush-bench { box-sizing: border-box; z-index: 6; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: var(--lush-text-sm); line-height: 16px; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-bench[hidden] { display: none; }
.lush-bench-head { display: flex; align-items: center; justify-content: space-between; gap: var(--lush-space-2); }
.lush-bench-title { font-size: var(--lush-text-lg); }
.lush-bench-list { display: flex; flex-direction: column; gap: var(--lush-space-1); }
.lush-bench-row { display: grid; grid-template-columns: 1fr auto; gap: var(--lush-space-1) var(--lush-space-2); align-items: center; padding: var(--lush-space-1) var(--lush-space-2); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); }
.lush-bench-name { grid-column: 1 / -1; font: var(--lush-text-sm) var(--lush-font-title); }
.lush-bench-inputs { display: flex; flex-wrap: wrap; gap: 0 var(--lush-space-2); }
.lush-bench-gold { color: var(--lush-gold); white-space: nowrap; }
.lush-bench-short { color: var(--lush-text-muted); }
.lush-bench-gold.lush-bench-short { color: #d46a5a; }
.lush-bench-craft { grid-column: 1 / -1; background: var(--lush-active); }
.lush-bench-craft:disabled { opacity: 0.5; }
`;
  document.head.append(s);
}
