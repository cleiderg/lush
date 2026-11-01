import type {
  EntityInfoState, Equipment, GroundItemState, HealthEntry, InventoryView, ItemInfo, NpcInfo, PlayerStatsState, SlotState,
} from '../net/codec';
import type { ShopResultCode } from '../protocol';

export type { EntityInfoState, Equipment, GroundItemState, InventoryView, ItemInfo, NpcInfo, PlayerStatsState, SlotState };

/** The last ShopResult; seq goes up by one per result so a repeated code is still a new result. */
export interface ShopResultState {
  code: ShopResultCode;
  seq: number;
}

const EMPTY_EQUIPMENT: Equipment = { weapon: 0, head: 0, body: 0, shield: 0 };

function sameEquipment(a: Equipment, b: Equipment): boolean {
  return a.weapon === b.weapon && a.head === b.head && a.body === b.body && a.shield === b.shield;
}

/**
 * Player stats, inventory, item and NPC catalogs, ground items, the last shop
 * result, and per-entity info and health.
 * Subscribers are told after any change so UI can redraw only then.
 */
export class ItemState {
  stats: PlayerStatsState = { hp: 0, maxHp: 0, combatLevel: 0, xp: 0, gold: 0 };
  inventory: InventoryView = { slots: [], equipment: { ...EMPTY_EQUIPMENT } };
  /** Every ground item in view; each GroundItems message replaces the list. */
  groundItems: GroundItemState[] = [];
  /** The last ShopResult, or null before any. */
  shopResult: ShopResultState | null = null;
  private catalog = new Map<number, ItemInfo>();
  private npcs = new Map<number, NpcInfo>();
  private infos = new Map<number, EntityInfoState>();
  private health = new Map<number, number>();
  /** Entity ids in the previous snapshot; an id that drops out has left view. */
  private inView = new Set<number>();
  private listeners = new Set<() => void>();

  /** Calls fn after every change; returns an unsubscribe function. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private notify(): void {
    for (const fn of this.listeners) fn();
  }

  item(id: number): ItemInfo | undefined {
    return this.catalog.get(id);
  }

  /** Every item in the catalog. */
  catalogItems(): IterableIterator<ItemInfo> {
    return this.catalog.values();
  }

  /** NPC type by id (EntityInfo.npcType). */
  npc(typeId: number): NpcInfo | undefined {
    return this.npcs.get(typeId);
  }

  entityInfo(id: number): EntityInfoState | undefined {
    return this.infos.get(id);
  }

  /** Health ratio 0..1; full (1) until the server says otherwise. */
  healthRatio(id: number): number {
    return this.health.get(id) ?? 1;
  }

  setStats(s: PlayerStatsState): void {
    const c = this.stats;
    if (c.hp === s.hp && c.maxHp === s.maxHp && c.combatLevel === s.combatLevel && c.xp === s.xp && c.gold === s.gold) return;
    this.stats = { hp: s.hp, maxHp: s.maxHp, combatLevel: s.combatLevel, xp: s.xp, gold: s.gold };
    this.notify();
  }

  setInventory(inv: InventoryView): void {
    const c = this.inventory;
    const same = c.slots.length === inv.slots.length
      && c.slots.every((s, i) => s.itemId === inv.slots[i].itemId && s.quantity === inv.slots[i].quantity)
      && sameEquipment(c.equipment, inv.equipment);
    if (same) return;
    this.inventory = { slots: inv.slots.map((s) => ({ ...s })), equipment: { ...inv.equipment } };
    this.notify();
  }

  setCatalog(items: ItemInfo[]): void {
    this.catalog = new Map(items.map((d) => [d.id, d]));
    this.notify();
  }

  setNpcCatalog(npcs: NpcInfo[]): void {
    this.npcs = new Map(npcs.map((d) => [d.id, { ...d }]));
    this.notify();
  }

  setGroundItems(items: GroundItemState[]): void {
    const c = this.groundItems;
    const same = c.length === items.length && c.every((g, i) => {
      const n = items[i];
      return g.id === n.id && g.itemId === n.itemId && g.quantity === n.quantity && g.x === n.x && g.z === n.z;
    });
    if (same) return;
    this.groundItems = items.map((g) => ({ ...g }));
    this.notify();
  }

  setShopResult(code: ShopResultCode): void {
    this.shopResult = { code, seq: (this.shopResult?.seq ?? 0) + 1 };
    this.notify();
  }

  setEntityInfo(info: EntityInfoState): void {
    const c = this.infos.get(info.id);
    if (c && c.kind === info.kind && c.npcType === info.npcType && c.combatLevel === info.combatLevel && c.totalLevel === info.totalLevel
      && sameEquipment(c.equipment, info.equipment)) return;
    const { id, kind, npcType, combatLevel, totalLevel, equipment } = info;
    this.infos.set(id, { id, kind, npcType, combatLevel, totalLevel, equipment: { ...equipment } });
    this.notify();
  }

  setHealth(entries: HealthEntry[]): void {
    let changed = false;
    for (const e of entries) {
      if (this.health.get(e.id) === e.ratio) continue;
      this.health.set(e.id, e.ratio);
      changed = true;
    }
    if (changed) this.notify();
  }

  /** Drops info and health for entities that were in the last snapshot but not this one. */
  setVisible(ids: Iterable<number>): void {
    const now = new Set(ids);
    let changed = false;
    for (const id of this.inView) {
      if (now.has(id)) continue;
      changed = this.infos.delete(id) || changed;
      changed = this.health.delete(id) || changed;
    }
    this.inView = now;
    if (changed) this.notify();
  }

  /** New session (Welcome): entity and ground item ids are session-scoped, so forget them. */
  resetEntities(): void {
    const changed = this.infos.size > 0 || this.health.size > 0 || this.groundItems.length > 0;
    this.infos.clear();
    this.health.clear();
    this.groundItems = [];
    this.inView.clear();
    if (changed) this.notify();
  }
}
