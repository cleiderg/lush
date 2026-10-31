import { encodeItemAction } from '../net/codec';
import { EquipSlot, ItemActionKind, ItemKind } from '../protocol';
import type { Equipment, ItemState } from '../state/items';
import { hudLayout, place } from './hudLayout';

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
  private readonly unsubscribe: () => void;
  private readonly onResize = (): void => this.layout();

  constructor(private readonly opts: HudOptions) {
    injectStyle();
    this.hud = el('div', 'lush-hud');
    this.hp = el('div', 'lush-hud-hp');
    this.level = el('div', 'lush-hud-level');
    this.gold = el('div', 'lush-hud-gold');
    this.hud.append(this.hp, this.level, this.gold);

    this.toggle = el('button', 'lush-inv-toggle', 'Bag');
    this.toggle.type = 'button';
    this.toggle.setAttribute('aria-label', 'Inventory');
    this.toggle.setAttribute('aria-expanded', 'false');
    this.toggle.addEventListener('click', () => this.setOpen(!this.isOpen));

    this.panel = el('div', 'lush-inv');
    this.panel.setAttribute('role', 'dialog');
    this.panel.setAttribute('aria-label', 'Inventory');
    this.panel.hidden = true;
    this.equipment = el('div', 'lush-equip');
    this.grid = el('div', 'lush-inv-grid');
    this.panel.append(this.equipment, this.grid);

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
    this.panel.hidden = !open;
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
        cell.classList.add('usable');
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
          cell.classList.add('usable');
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
.lush-inv-toggle { box-sizing: border-box; z-index: 5; border: 0; border-radius: 12px; background: #3b3b52; color: #fff; font: 600 14px sans-serif; touch-action: manipulation; }
.lush-inv-toggle.on { background: #4a8f4a; }
.lush-inv { box-sizing: border-box; z-index: 5; display: flex; flex-direction: column; gap: 8px; padding: 8px; border-radius: 12px; background: rgba(29,29,38,.94); color: #fff; font: 12px/14px system-ui, sans-serif; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-inv[hidden] { display: none; }
.lush-equip { display: grid; grid-template-columns: repeat(4, 1fr); gap: 4px; }
.lush-equip-slot, .lush-slot { position: relative; min-width: 0; padding: 4px; border-radius: 6px; background: #3b3b52; overflow: hidden; }
.lush-equip-slot { display: flex; flex-direction: column; }
.lush-equip-label { color: #aab; font-size: 10px; }
.lush-equip-slot.empty .lush-equip-name { color: #778; }
.lush-inv-grid { display: grid; grid-template-columns: repeat(4, 1fr); grid-auto-rows: minmax(40px, 1fr); gap: 4px; flex: 1; }
.lush-equip-slot.usable, .lush-slot.usable { cursor: pointer; touch-action: manipulation; }
.lush-equip-slot.usable:active, .lush-slot.usable:active { background: #4a8f4a; }
.lush-slot.empty { background: #2c2c3a; }
.lush-slot-name { display: block; word-break: break-word; }
.lush-slot-qty { position: absolute; right: 4px; bottom: 2px; color: #ffd54f; font-weight: 700; }
`;
  document.head.append(s);
}
