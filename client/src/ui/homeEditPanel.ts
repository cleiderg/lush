import type { HomePlacement } from '../net/codec';
import { PlacementKind } from '../protocol';
import type { HousingStore } from '../state/housing';
import type { ItemState } from '../state/items';
import { hudLayout, place } from './hudLayout';
import { addPressFeedbackWithin, BUTTON_CLASS, PanelMotion } from './theme';

/** A tile on the home floor, in whole tiles. */
export interface Tile {
  x: number;
  y: number;
  /** Placed id of the surface piece the tap landed on, when it landed on one. */
  parentPlacedId?: number;
  /** Surface slot, or -1 for the first free one. */
  slot?: number;
  /** Wall side, for a tap on a wall. */
  side?: number;
}

/** What the room renderer draws for edit mode (RoomEditView); the panel drives it and reads back the colour. */
export interface EditRoom {
  setGrid(on: boolean): void;
  /** Shows the ghost of `itemId` at `at` with `rotation`; true if it is green (the server would accept it). */
  showGhost(itemId: number, at: Tile, rotation: number, ignorePlacedId: number): boolean;
  hideGhost(): void;
}

/** The home senders the panel calls (Session.sendHome*). */
export interface HomeEditSession {
  sendHomePlace(inventorySlot: number, p: HomePlacement): void;
  sendHomeMove(placedId: number, p: HomePlacement): void;
  sendHomeRotate(placedId: number, rotation: number): void;
  sendHomeRemove(placedId: number): void;
  sendHomeSetWallpaper(inventorySlot: number): void;
  sendHomeSetFloor(inventorySlot: number): void;
}

export interface HomeEditPanelOptions {
  parent: HTMLElement;
  housing: HousingStore;
  items: ItemState;
  session: HomeEditSession;
  /**
   * Asks the renderer for the next tile the player taps in the room; it calls `onTile` with that tile.
   * Asking again replaces the earlier request.
   */
  pickTile: (onTile: (tile: Tile) => void) => void;
  /** The room's grid overlay and ghost; absent when the room isn't drawn. */
  room?: EditRoom;
  viewport?: () => { width: number; height: number };
  /** Called when edit mode opens or closes. */
  onEditing?: (editing: boolean) => void;
}

const QUARTER_TURNS = 4;

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

function sameSpot(a: Tile, b: Tile): boolean {
  return a.x === b.x && a.y === b.y && (a.parentPlacedId ?? 0) === (b.parentPlacedId ?? 0)
    && (a.slot ?? -1) === (b.slot ?? -1) && (a.side ?? 0) === (b.side ?? 0);
}

function placement(t: Tile, rotation: number): HomePlacement {
  return { x: t.x, y: t.y, rotation, parentPlacedId: t.parentPlacedId ?? 0, slot: Math.max(0, t.slot ?? 0), side: t.side ?? 0 };
}

const FURNITURE = new Set<PlacementKind>([
  PlacementKind.Floor, PlacementKind.Rug, PlacementKind.Surface, PlacementKind.FloorOrSurface, PlacementKind.Wall,
]);

/**
 * Home edit mode. An Edit button shows only inside the player's own home (visitors only look). It opens a
 * drawer from the bottom listing the inventory's furniture, wallpaper and floors: furniture is placed by
 * choosing it and then a tile (from `pickTile`), wallpaper and floor apply at once. Choosing a placed item
 * offers Rotate (next quarter turn), Remove, and a new tile to move it to.
 */
export class HomeEditPanel {
  readonly button: HTMLButtonElement;
  readonly drawer: HTMLElement;
  private readonly body: HTMLElement;
  private readonly motion: PanelMotion;
  private readonly unsubscribers: (() => void)[] = [];
  private readonly onResize = (): void => this.layout();
  private editing = false;
  /** Inventory slot of the furniture chosen to place, or -1. */
  private slot = -1;
  /** Placed item chosen to move, rotate or remove, or 0. */
  private placedId = 0;
  /** Rotation, in quarter turns, for the next item placed. */
  private rotation = 0;
  /** The tile the ghost is on and whether it is green, or null while no ghost is shown. */
  private ghost: { tile: Tile; valid: boolean } | null = null;

  constructor(private readonly opts: HomeEditPanelOptions) {
    injectStyle();
    this.button = button('lush-edit-button', 'Edit', () => this.setEditing(!this.editing));
    this.button.classList.add('lush-panel');
    this.button.hidden = true;
    this.drawer = el('div', 'lush-panel lush-edit-drawer');
    this.drawer.setAttribute('role', 'dialog');
    this.drawer.setAttribute('aria-label', 'Edit home');
    this.motion = new PanelMotion(this.drawer, (shown) => { this.drawer.hidden = !shown; });
    addPressFeedbackWithin(this.drawer);
    const head = el('div', 'lush-edit-head');
    head.append(el('span', 'lush-title', 'Edit home'), button('lush-edit-done', 'Done', () => this.setEditing(false)));
    this.body = el('div', 'lush-edit-body');
    this.drawer.append(head, this.body);
    opts.parent.append(this.button, this.drawer);
    this.unsubscribers.push(opts.housing.subscribe(() => this.render()), opts.items.subscribe(() => this.render()));
    window.addEventListener('resize', this.onResize);
    this.layout();
    this.render();
  }

  get isEditing(): boolean {
    return this.editing;
  }

  /** True while the Edit button is shown: only inside the player's own home. */
  get canEdit(): boolean {
    return this.opts.housing.inOwnHome();
  }

  /** Chooses a placed item, for the renderer when the player taps one in the room. */
  selectPlaced(placedId: number): void {
    if (!this.editing || !this.opts.housing.home?.items.some((i) => i.placedId === placedId)) return;
    this.slot = -1;
    this.placedId = placedId;
    this.rotation = this.opts.housing.home?.items.find((i) => i.placedId === placedId)?.rotation ?? 0;
    this.clearGhost();
    this.armMove(placedId);
    this.render();
  }

  setEditing(on: boolean): void {
    if (on && !this.canEdit) return;
    if (on === this.editing) return;
    this.editing = on;
    this.slot = -1;
    this.placedId = 0;
    this.clearGhost();
    this.opts.room?.setGrid(on);
    this.motion.set(on);
    this.opts.onEditing?.(on);
    this.render();
  }

  layout(): void {
    const v = this.opts.viewport?.() ?? { width: window.innerWidth, height: window.innerHeight };
    const l = hudLayout(v.width, v.height);
    place(this.button, l.editButton);
    place(this.drawer, l.editDrawer);
  }

  private clearGhost(): void {
    this.ghost = null;
    this.opts.room?.hideGhost();
  }

  /**
   * Handles a tap while an item is chosen. The first tap on a tile only moves the ghost there; a second tap
   * on the ghost's tile sends the place or move if the ghost is green. A red ghost sends nothing.
   */
  private tapTile(t: Tile, itemId: number): boolean {
    const same = this.ghost !== null && sameSpot(this.ghost.tile, t);
    if (same && this.ghost!.valid) return true;
    if (!same) this.showGhost(t, itemId);
    return false;
  }

  private showGhost(t: Tile, itemId: number): void {
    const room = this.opts.room;
    const valid = room ? room.showGhost(itemId, t, this.rotation, this.placedId) : true;
    this.ghost = { tile: t, valid };
  }

  private armMove(placedId: number): void {
    const again = (): void => this.opts.pickTile((t) => {
      if (this.placedId !== placedId) return;
      const item = this.opts.housing.home?.items.find((i) => i.placedId === placedId);
      if (!item) return;
      if (this.tapTile(t, item.itemId)) {
        this.opts.session.sendHomeMove(placedId, placement(t, this.rotation));
        this.clearGhost();
      }
      again();
    });
    again();
  }

  private chooseFurniture(slot: number): void {
    this.placedId = 0;
    this.slot = slot;
    this.clearGhost();
    const again = (): void => this.opts.pickTile((t) => {
      if (this.slot !== slot) return;
      const itemId = this.opts.items.inventory.slots[slot]?.itemId ?? 0;
      if (this.tapTile(t, itemId)) {
        this.opts.session.sendHomePlace(slot, placement(t, this.rotation));
        this.slot = -1;
        this.clearGhost();
        this.render();
        return;
      }
      again();
    });
    again();
    this.render();
  }

  private rotate(): void {
    const item = this.opts.housing.home?.items.find((i) => i.placedId === this.placedId);
    this.rotation = (this.rotation + 1) % QUARTER_TURNS;
    if (item) this.opts.session.sendHomeRotate(item.placedId, this.rotation);
    if (this.ghost) this.showGhost(this.ghost.tile, item?.itemId ?? this.opts.items.inventory.slots[this.slot]?.itemId ?? 0);
    this.render();
  }

  private remove(): void {
    if (!this.placedId) return;
    this.opts.session.sendHomeRemove(this.placedId);
    this.placedId = 0;
    this.clearGhost();
    this.render();
  }

  private section(title: string, entries: HTMLElement[]): void {
    if (!entries.length) return;
    const row = el('div', 'lush-edit-row');
    row.append(...entries);
    this.body.append(el('div', 'lush-edit-label', title), row);
  }

  render(): void {
    const own = this.canEdit;
    this.button.hidden = !own;
    if (!own && this.editing) this.setEditing(false);
    if (!this.editing) return;

    const { items, housing, session } = this.opts;
    const home = housing.home;
    if (this.placedId && !home?.items.some((i) => i.placedId === this.placedId)) this.placedId = 0;
    this.body.replaceChildren();

    const furniture: HTMLElement[] = [];
    const wallpaper: HTMLElement[] = [];
    const floors: HTMLElement[] = [];
    items.inventory.slots.forEach((s, slot) => {
      if (!s.itemId) return;
      const def = items.item(s.itemId);
      if (!def) return;
      const kind = def.placement ?? PlacementKind.None;
      const name = s.quantity > 1 ? `${def.name} x${s.quantity}` : def.name;
      if (FURNITURE.has(kind)) {
        const b = button('lush-edit-item lush-edit-furniture', name, () => this.chooseFurniture(slot));
        b.classList.toggle('selected', this.slot === slot);
        furniture.push(b);
      } else if (kind === PlacementKind.Wallpaper) {
        wallpaper.push(button('lush-edit-item lush-edit-wallpaper', name, () => session.sendHomeSetWallpaper(slot)));
      } else if (kind === PlacementKind.Flooring) {
        floors.push(button('lush-edit-item lush-edit-floor', name, () => session.sendHomeSetFloor(slot)));
      }
    });
    this.section('Furniture', furniture);
    this.section('Wallpaper', wallpaper);
    this.section('Floors', floors);
    if (!furniture.length && !wallpaper.length && !floors.length) {
      this.body.append(el('div', 'lush-edit-empty', 'Nothing to place. Craft furniture or buy it first.'));
    }

    const placed = (home?.items ?? []).map((p) => {
      const b = button('lush-edit-item lush-edit-placed', items.item(p.itemId)?.name ?? `Item ${p.itemId}`, () => this.selectPlaced(p.placedId));
      b.classList.toggle('selected', this.placedId === p.placedId);
      return b;
    });
    this.section('In this room', placed);

    if (this.slot >= 0 || this.placedId) {
      const bar = el('div', 'lush-edit-actions');
      bar.append(button('lush-edit-rotate', 'Rotate', () => this.rotate()));
      if (this.placedId) bar.append(button('lush-edit-remove', 'Remove', () => this.remove()));
      bar.append(el('span', 'lush-edit-hint', this.placedId ? 'Tap a tile to move it' : 'Tap a tile to place it'));
      this.body.append(bar);
    }
  }

  dispose(): void {
    for (const u of this.unsubscribers) u();
    this.motion.dispose();
    window.removeEventListener('resize', this.onResize);
    this.button.remove();
    this.drawer.remove();
  }
}

let styled = false;

function injectStyle(): void {
  if (styled) return;
  styled = true;
  const s = document.createElement('style');
  s.textContent = `
.lush-edit-button { z-index: 6; display: flex; align-items: center; justify-content: center; }
.lush-edit-button[hidden] { display: none; }
.lush-edit-drawer { box-sizing: border-box; z-index: 6; display: flex; flex-direction: column; gap: var(--lush-space-2); padding: var(--lush-space-2) var(--lush-space-3) var(--lush-space-3); font-size: var(--lush-text-sm); line-height: 16px; overflow-y: auto; touch-action: pan-y; user-select: none; -webkit-user-select: none; }
.lush-edit-drawer[hidden] { display: none; }
.lush-edit-head { display: flex; align-items: center; justify-content: space-between; gap: var(--lush-space-2); }
.lush-edit-body { display: flex; flex-direction: column; gap: var(--lush-space-2); }
.lush-edit-label { color: var(--lush-gold-bright); font-weight: 600; }
.lush-edit-row, .lush-edit-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--lush-space-2); }
.lush-edit-empty, .lush-edit-hint { color: var(--lush-text-muted); }
.lush-edit-drawer button.selected { background: var(--lush-active); border-color: var(--lush-gold-bright); }
`;
  document.head.append(s);
}
