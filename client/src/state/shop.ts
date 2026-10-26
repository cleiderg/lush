import { ShopResultCode } from '../protocol';

/**
 * How close (in tiles) a player must be to the shop stall to open it.
 * Must equal shop_distance_tiles in server/internal/systems/gamedata/data/rules.json;
 * a test fails if they differ (D-044 precedent).
 */
export const SHOP_DISTANCE_TILES = 2;
/** One collision cell (tile) is this many metres (shared/map/config.mjs CELL_SIZE). */
export const TILE_METRES = 1;

export interface Point {
  x: number;
  z: number;
}

/** Whether pos (metres) is within SHOP_DISTANCE_TILES of the stall, counting diagonals like attack range. */
export function nearShop(pos: Point, stall: Point): boolean {
  const reach = SHOP_DISTANCE_TILES * TILE_METRES;
  return Math.abs(pos.x - stall.x) <= reach && Math.abs(pos.z - stall.z) <= reach;
}

const RESULT_TEXT: Record<ShopResultCode, string> = {
  [ShopResultCode.Ok]: 'Done',
  [ShopResultCode.TooFar]: 'Too far from the stall',
  [ShopResultCode.NotEnoughGold]: 'Not enough gold',
  [ShopResultCode.InventoryFull]: 'Inventory full',
  [ShopResultCode.ItemNotSold]: 'The shop does not sell that',
};

/** Short message the shop panel shows for a ShopResult code. */
export function shopResultText(code: ShopResultCode): string {
  return RESULT_TEXT[code] ?? 'Something went wrong';
}
