import { EntityKind } from '../protocol';
import { pickInteractTarget, type InteractTarget } from '../state/interact';
import type { Point } from '../state/shop';
import type { GameState } from '../state/world';

export interface InteractActions {
  /** Opens the shop panel (ShopPanel.tryOpen). */
  openShop: () => void;
  /** Opens the card table panel for that table (CardTablePanel.open). */
  openCardTable: (tableId: number) => void;
  sendPickUp: (groundItemId: number) => void;
  sendAttackTarget: (targetId: number) => void;
  sendTradeRequest: (targetId: number) => void;
}

/** The Interact target for the predicted local player and the interpolated entities at `now`. */
export function interactTarget(game: GameState, now: number, stall: Point): InteractTarget | null {
  const entities = game.remotes(now).map((r) => ({ id: r.id, x: r.x, z: r.z, kind: game.items.entityInfo(r.id)?.kind }));
  return pickInteractTarget({ pos: game.prediction.pos, stall, entities, groundItems: game.items.groundItems });
}

/** Runs the Interact button: opens the shop or a card table's panel, picks up a ground item, attacks, or sends a trade request; nothing without a target. */
export function interact(game: GameState, now: number, stall: Point, actions: InteractActions): InteractTarget | null {
  const target = interactTarget(game, now, stall);
  if (target?.type === 'shop') actions.openShop();
  else if (target?.type === 'cardTable') actions.openCardTable(target.id);
  else if (target?.type === 'pickUp') actions.sendPickUp(target.id);
  else if (target?.type === 'attack') actions.sendAttackTarget(target.id);
  else if (target?.type === 'trade') actions.sendTradeRequest(target.id);
  return target;
}

/** What the Interact prompt shows: verb, then the name (drawn in gold) when there is one, then any detail. */
export interface InteractPromptText {
  verb: string;
  name?: string;
  detail?: string;
}

/** The shop's name, as on its panel. */
export const SHOP_NAME = 'Campus Market';

/** The prompt for target (from interactTarget), or null when Interact does nothing (visual-style.md, Interact prompt). */
export function interactPromptText(game: GameState, target: InteractTarget | null): InteractPromptText | null {
  if (!target) return null;
  switch (target.type) {
    case 'shop':
      return { verb: 'Open', name: SHOP_NAME };
    case 'cardTable':
      return { verb: 'Open card table' };
    case 'pickUp': {
      const g = game.items.groundItems.find((i) => i.id === target.id);
      return { verb: 'Pick up', name: (g && game.items.item(g.itemId)?.name) || 'item' };
    }
    case 'attack': {
      const info = game.items.entityInfo(target.id);
      if (info?.kind !== EntityKind.Npc) return { verb: 'Attack a player' };
      return { verb: 'Attack', name: game.items.npc(info.npcType)?.name || 'NPC', detail: `· Lvl ${info.combatLevel}` };
    }
    case 'trade':
      return { verb: 'Trade with a player' };
  }
}

/** The prompt as one line of text. */
export function promptString(p: InteractPromptText): string {
  return [p.verb, p.name, p.detail].filter(Boolean).join(' ');
}
