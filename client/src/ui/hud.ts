import { encodeItemAction } from '../net/codec';
import { EquipSlot, ItemActionKind, ItemKind } from '../protocol';
import type { Equipment, ItemState } from '../state/items';
import { hudLayout, place } from './hudLayout';
import { addPressFeedback, addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

export interface HudOptions {
  parent: HTMLElement;
  items: ItemState;
  /** Current viewport size; defaults to the window's. */
  viewport?: () => { width: number; height: number };
  /** Sends an encoded client message (ItemAction) to the server. */
  send?: (bytes: Uint8Array) => void;
  /** Called when the inventory panel opens, so other panels in its place can close. */
  onOpen?: () => void;
}

const EQUIP_SLOTS: { key: keyof Equipment; label: string; slot: EquipSlot }[] = [
  { key: 'weapon', label: 'Weapon', slot: EquipSlot.Weapon },
  { key: 'head', label: 'Head', slot: EquipSlot.Head },
  { key: 'body', label: 'Body', slot: EquipSlot.Body },
  { key: 'shield', label: 'Shield', slot: EquipSlot.Shield },
];

/** What tapping an inventory item of this kind does: equip gear, eat food; loot does nothing. */
export function slotAction(kind: ItemKind | undefined): ItemActionKind | null {
  if (kind === ItemKind.Weapon || kind === ItemKind.Armor) return ItemActionKind.Equip;
  if (kind === ItemKind.Food) return ItemActionKind.Eat;
  return null;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls: string, text = ''): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = cls;
  e.textContent = text;
  return e;
}

/** HP / combat level / gold readout, inventory toggle, and the inventory panel over the game canvas. */
export class Hud {
  readonly hud: HTMLElement;
  readonly toggle: HTMLButtonElement;
  readonly panel: HTMLElement;
  private readonly hp: HTMLElement;
  private readonly level: HTMLElement;
  private readonly gold: HTMLElement;
  private readonly equipment: HTMLElement;
  private readonly grid: HTMLElement;
  private readonly motion: PanelMotion;
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: HudOptions) {
    injectStyle();
    this.hud = el('div', 'lush-hud');
    this.hp = el('div', 'lush-hud-hp');
    this.level = el('div', 'lush-hud-level');
    this.gold = el('div', 'lush-hud-gold');
    this.hud.append(this.hp, this.level, this.gold);

    this.toggle = el('button', `lush-inv-toggle ${BUTTON_CLASS}`, 'Bag');
    this.toggle.type = 'button';
    this.toggle.setAttribute('aria-label', 'Inventory');
    this.toggle.setAttribute('aria-expanded', 'false');
    this.toggle.addEventListener('click', () => this.setOpen(!this.isOpen));
    addPressFeedback(this.toggle);

    this.panel = el('div', 'lush-panel lush-inv');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Inventory');
    this.motion = new PanelMotion(this.panel, (shown) => { this.panel.hidden = !shown; });
    addPressFeedbackWithin(this.panel);
    this.equipment = el('div', 'lush-equip');
    this.grid = el('div', 'lush-inv-grid');
    this.panel.append(el('div', 'lush-title lush-inv-title', 'Inventory'), this.equipment, this.grid);

    opts.parent.append(this.hud, this.toggle, this.panel);
    this.unsubscribe = opts.items.subscribe(() => this.render());
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  get isOpen(): boolean {
    return !this.panel.hidden;
  }

  setOpen(open: boolean): void {
    if (open && !this.isOpen) this.opts.onOpen?.();
    this.motion.set(open);
    this.toggle.setAttribute('aria-expanded', String(open));
    this.toggle.classList.toggle('on', open);
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    const l = hudLayout(v.width, v.height);
    place(this.hud, l.hud);
    place(this.toggle, l.toggle);
    place(this.panel, l.panel);
  }

  render(): void {
    const items = this.opts.items;
    const s = items.stats;
    this.hp.textContent = `HP ${s.hp}/${s.maxHp}`;
    this.level.textContent = `Combat ${s.combatLevel}`;
    this.gold.textContent = `Gold ${s.gold.toLocaleString('en-US')}`;

    const name = (id: number): string => items.item(id)?.name ?? `Item ${id}`;
    const inv = items.inventory;
    this.equipment.replaceChildren(...EQUIP_SLOTS.map(({ key, label, slot }) => {
      const id = inv.equipment[key];
      const cell = el('div', id ? 'lush-equip-slot' : 'lush-equip-slot empty');
      cell.dataset.slot = key;
      if (id) {
        cell.classList.add('usable', BUTTON_CLASS);
        cell.addEventListener('click', () => this.opts.send?.(encodeItemAction(ItemActionKind.Unequip, 0, slot)));
      }
      cell.append(el('span', 'lush-equip-label', label), el('span', 'lush-equip-name', id ? name(id) : 'Empty'));
      return cell;
    }));
    this.grid.replaceChildren(...inv.slots.map((slot, i) => {
      const cell = el('div', slot.itemId ? 'lush-slot' : 'lush-slot empty');
      cell.dataset.index = String(i);
      if (slot.itemId) {
        const action = slotAction(items.item(slot.itemId)?.kind);
        if (action !== null) {
          cell.classList.add('usable', BUTTON_CLASS);
          cell.addEventListener('click', () => this.opts.send?.(encodeItemAction(action, i)));
        }
        cell.append(el('span', 'lush-slot-name', name(slot.itemId)));
        if (slot.quantity > 1) cell.append(el('span', 'lush-slot-qty', String(slot.quantity)));
      }
      return cell;
    }));
  }

  dispose(): void {
    this.unsubscribe();
    this.motion.dispose();
    window.removeEventListener('resize', this.onResize);
    this.hud.remove();
    this.toggle.remove();
    this.panel.remove();
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-hud { box-sizing: border-box; z-index: 5; display: flex; flex-direction: column; justify-content: center; padding: 6px 8px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; border-radius: 10px; background: rgba(20,20,30,.72); color: #fff; font: 600 13px/17px system-ui, sans-serif; pointer-events: none; user-select: none; -webkit-user-select: none; }
.lush-hud-hp { color: #ff8a80; }
.lush-hud-gold { color: #ffd54f; }
.lush-inv-toggle { box-sizing: border-box; z-index: 5; min-width: 44px; min-height: 44px; border: var(--lush-border-strong); border-radius: var(--lush-radius); background: var(--lush-panel-bg); color: var(--lush-text); box-shadow: var(--lush-shadow); font: 600 var(--lush-text-sm) var(--lush-font-title); letter-spacing: 0.04em; }
.lush-inv-toggle.on { background: var(--lush-surface-pressed); border-color: var(--lush-gold-bright); color: var(--lush-gold-bright); }
.lush-inv { box-sizing: border-box; z-index: 5; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: 12px; line-height: 14px; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-inv[hidden] { display: none; }
.lush-inv-title { font-size: var(--lush-text-lg); }
.lush-equip { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--lush-space-1); }
.lush-equip-slot, .lush-slot { box-sizing: border-box; position: relative; min-width: 0; min-height: 44px; padding: var(--lush-space-1); border: var(--lush-border); border-radius: var(--lush-radius-sm); background: var(--lush-surface); overflow: hidden; }
.lush-equip-slot { display: flex; flex-direction: column; }
.lush-equip-label { color: var(--lush-text-muted); font: 10px var(--lush-font-title); letter-spacing: 0.04em; }
.lush-equip-slot.empty .lush-equip-name, .lush-slot.empty { color: var(--lush-text-muted); }
.lush-inv-grid { display: grid; grid-template-columns: repeat(4, 1fr); grid-auto-rows: minmax(44px, 1fr); gap: var(--lush-space-1); flex: 1; }
.lush-slot.empty { background: rgba(0, 0, 0, 0.25); }
.lush-slot-name { display: block; word-break: break-word; }
.lush-slot-qty { position: absolute; right: 4px; bottom: 2px; color: var(--lush-gold-bright); font-weight: 700; }
`;
  document.head.append(s);
}
