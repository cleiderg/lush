import type {
  EntityInfoState, Equipment, HealthEntry, InventoryView, ItemInfo, PlayerStatsState, SlotState,
} from '../net/codec';

export type { EntityInfoState, Equipment, InventoryView, ItemInfo, PlayerStatsState, SlotState };

const EMPTY_EQUIPMENT: Equipment = { weapon: 0, head: 0, body: 0, shield: 0 };

function sameEquipment(a: Equipment, b: Equipment): boolean {
  return a.weapon === b.weapon && a.head === b.head && a.body === b.body && a.shield === b.shield;
}

/**
 * Player stats, inventory, item catalog and per-entity info and health.
 * Subscribers are told after any change so UI can redraw only then.
 */
export class ItemState {
  stats: PlayerStatsState = { hp: 0, maxHp: 0, combatLevel: 0, xp: 0, gold: 0 };
  inventory: InventoryView = { slots: [], equipment: { ...EMPTY_EQUIPMENT } };
  private catalog = new Map<number, ItemInfo>();
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

  setEntityInfo(info: EntityInfoState): void {
    const c = this.infos.get(info.id);
    if (c && c.kind === info.kind && c.npcType === info.npcType && c.combatLevel === info.combatLevel
      && sameEquipment(c.equipment, info.equipment)) return;
    const { id, kind, npcType, combatLevel, equipment } = info;
    this.infos.set(id, { id, kind, npcType, combatLevel, equipment: { ...equipment } });
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

  /** New session (Welcome): entity ids are session-scoped, so forget per-entity data. */
  resetEntities(): void {
    const changed = this.infos.size > 0 || this.health.size > 0;
    this.infos.clear();
    this.health.clear();
    this.inView.clear();
    if (changed) this.notify();
  }
}
