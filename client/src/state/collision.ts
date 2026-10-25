/**
 * Collision grid for prediction (shared/map/collision.bin, format in shared/map/README.md).
 * src/state can't import src/render, so this is its own reader; it mirrors the server's
 * world.Grid and movement slide rule so predicted movement matches the server's.
 */

const HEADER_BYTES = 22;
const VERSION = 1;

export interface Vec {
  x: number;
  z: number;
}

export class CollisionGrid {
  constructor(
    readonly cellSize: number, // metres
    readonly width: number,
    readonly height: number,
    readonly originX: number, // metres, west edge
    readonly originZ: number, // metres, north edge
    private readonly bits: Uint8Array,
  ) {}

  /** Decodes collision.bin. Throws on a malformed file. */
  static parse(buf: Uint8Array): CollisionGrid {
    if (buf.length < HEADER_BYTES) throw new Error(`collision grid: ${buf.length} bytes, need at least ${HEADER_BYTES}`);
    const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
    const magic = String.fromCharCode(buf[0], buf[1], buf[2], buf[3]);
    if (magic !== 'LCOL') throw new Error(`collision grid: bad magic ${JSON.stringify(magic)}`);
    if (buf[4] !== VERSION) throw new Error(`collision grid: unsupported version ${buf[4]}`);
    const cell = dv.getUint16(6, true);
    const w = dv.getUint16(10, true);
    const h = dv.getUint16(12, true);
    if (cell === 0 || w === 0 || h === 0) throw new Error('collision grid: zero cell size or dimensions');
    const n = Math.ceil((w * h) / 8);
    if (buf.length - HEADER_BYTES !== n) throw new Error(`collision grid: ${buf.length - HEADER_BYTES} data bytes, want ${n}`);
    return new CollisionGrid(cell / 10, w, h, dv.getInt32(14, true) / 10, dv.getInt32(18, true) / 10, buf.subarray(HEADER_BYTES));
  }

  /** True when cell (col,row) is blocked. Cells outside the grid are blocked. */
  cellBlocked(col: number, row: number): boolean {
    if (col < 0 || row < 0 || col >= this.width || row >= this.height) return true;
    const idx = row * this.width + col;
    return ((this.bits[idx >> 3] >> (idx & 7)) & 1) === 1;
  }

  /** True when the world point (metres) is blocked. Non-finite points are blocked. */
  blocked(x: number, z: number): boolean {
    const col = Math.floor((x - this.originX) / this.cellSize);
    const row = Math.floor((z - this.originZ) / this.cellSize);
    if (!Number.isFinite(col) || !Number.isFinite(row)) return true;
    return this.cellBlocked(col, row);
  }
}

/**
 * Where a move from → to ends up, by the server's rule (movement.slide): the move is split
 * into sub-steps of at most half a cell; each sub-step takes the full move if free, else its
 * X part, else its Z part, else stops there.
 *
 * One difference from the server: an axis fallback only counts when the move has a component
 * on that axis. The server treats the zero-length fallback of a straight move as progress, so a
 * single step longer than about a cell can tunnel through a one-cell wall. For steps shorter
 * than a cell (server ticks and client inputs are 0.2 m) both rules give the same result.
 */
export function slide(g: CollisionGrid, from: Vec, to: Vec): Vec {
  const dx = to.x - from.x, dz = to.z - from.z;
  const n = Math.ceil(Math.max(Math.abs(dx), Math.abs(dz)) / (g.cellSize / 2)) + 1;
  const cur = { x: from.x, z: from.z };
  for (let i = 1; i <= n; i++) {
    const t = i / n;
    const wx = i === n ? to.x : from.x + dx * t;
    const wz = i === n ? to.z : from.z + dz * t;
    if (!g.blocked(wx, wz)) {
      cur.x = wx;
      cur.z = wz;
    } else if (dx !== 0 && !g.blocked(wx, cur.z)) {
      cur.x = wx;
    } else if (dz !== 0 && !g.blocked(cur.x, wz)) {
      cur.z = wz;
    } else {
      return cur;
    }
  }
  return cur;
}
