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
