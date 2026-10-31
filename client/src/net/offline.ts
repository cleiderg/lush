import * as flatbuffers from 'flatbuffers';
import areas from '../../../shared/map/areas.json';
import cardTablesData from '../../../shared/map/cardtables.json';
import { Animation, Entity, Envelope, Message, Snapshot, Vec2, Welcome } from '../protocol';
import type { EntityState } from './codec';
import type { TokenStore } from './session';
import type { Transport } from './transport';

/**
 * Offline mode (`?offline`) for screenshots and local checks (visual-style.md, "Checking against
 * the references"): a local stand-in for the server feeds the client a Welcome and snapshots with a
 * fixed-seed set of bot players near the spawn, through the normal Session, and no WebSocket is
 * opened. The local player is left out of the snapshots, so prediction moves it freely.
 */

/**
 * Places the offline player can start at, in metres: the spawn from areas.json (decimetres), a point
 * among Aldrich Park's trees, one metre from the shop stall, and one metre from card table 1.
 */
export const OFFLINE_PLACES: Readonly<Record<string, { x: number; z: number }>> = {
  spawn: { x: areas.spawn.x / 10, z: areas.spawn.z / 10 },
  park: { x: -70, z: -40 },
  shop: { x: areas.shop.x / 10 + 1, z: areas.shop.z / 10 },
  cardtable: { x: cardTablesData.tables[0].x / 10, z: cardTablesData.tables[0].z / 10 + 1 },
};

/** UI panels offline mode can open with fixed data (`?offline&panel=<name>`, npm run shots). */
export const OFFLINE_PANELS = ['inventory', 'shop', 'trade', 'card-table', 'chat-log', 'big-map'] as const;
export type OfflinePanel = (typeof OFFLINE_PANELS)[number];

/** Where a panel's shot starts when the URL names no place: within reach of the shop stall or card table. */
const PANEL_PLACES: Partial<Record<OfflinePanel, string>> = { shop: 'shop', 'card-table': 'cardtable' };

export const OFFLINE_PLAYER_ID = 1;
const DEFAULT_SEED = 1;
const DEFAULT_BOTS = 8;
const MAX_BOTS = 64;
const TICK_RATE = 20;
/** A fixed date (2026-06-21 19:00 UTC, noon in Irvine), so the sky presets fall on the same day every run. */
export const OFFLINE_SERVER_TIME_MS = Date.UTC(2026, 5, 21, 19, 0, 0);
const BOT_MIN_RADIUS = 3;
const BOT_MAX_RADIUS = 12;

export interface OfflineOptions {
  seed: number;
  /** Where the local player starts and the bots gather, in metres. */
  spawn: { x: number; z: number };
  bots: number;
  /** The panel to fill with fixed data and open, if any. */
  panel?: OfflinePanel;
}

/**
 * Offline options from the URL query string, or null without `offline`. `seed` (integer, default 1),
 * `place` (a key of OFFLINE_PLACES, default spawn, or the panel's place), `bots` (0 to 64, default 8)
 * and `panel` (one of OFFLINE_PANELS) are optional.
 */
export function offlineOptions(search: string): OfflineOptions | null {
  const q = new URLSearchParams(search);
  if (!q.has('offline')) return null;
  const seed = Number.parseInt(q.get('seed') ?? '', 10);
  const bots = Number.parseInt(q.get('bots') ?? '', 10);
  const panel = OFFLINE_PANELS.find((p) => p === q.get('panel'));
  const place = OFFLINE_PLACES[q.get('place') ?? (panel && PANEL_PLACES[panel]) ?? 'spawn'] ?? OFFLINE_PLACES.spawn;
  return {
    seed: Number.isFinite(seed) ? seed : DEFAULT_SEED,
    spawn: { ...place },
    bots: Number.isFinite(bots) ? Math.max(0, Math.min(MAX_BOTS, bots)) : DEFAULT_BOTS,
    ...(panel ? { panel } : {}),
  };
}

/** mulberry32: a small deterministic PRNG returning floats in [0, 1). */
export function seededRandom(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export interface Bot {
  id: number;
  /** Home position in metres. */
  x: number;
  z: number;
  heading: number;
  /** Walkers circle their home at this radius (metres); 0 stands idle. */
  walkRadius: number;
  /** Starting angle on the circle, radians. */
  phase: number;
}

/** The bots for a seed: the same seed always gives the same bots, scattered around center. */
export function makeBots(seed: number, count: number, center: { x: number; z: number }): Bot[] {
  const rand = seededRandom(seed);
  const bots: Bot[] = [];
  for (let i = 0; i < count; i++) {
    const angle = rand() * Math.PI * 2;
    const dist = BOT_MIN_RADIUS + rand() * (BOT_MAX_RADIUS - BOT_MIN_RADIUS);
    const walks = rand() < 0.5;
    bots.push({
      id: OFFLINE_PLAYER_ID + 1 + i,
      x: center.x + Math.cos(angle) * dist,
      z: center.z + Math.sin(angle) * dist,
      heading: rand() * Math.PI * 2,
      walkRadius: walks ? 1 + rand() * 2 : 0,
      phase: rand() * Math.PI * 2,
    });
  }
  return bots;
}

/** Walking speed of the bots in radians per second around their circle. */
const BOT_TURN_RATE = 0.4;

/** The bots' entity states at a tick: idle bots stand still, walkers go round their circle. */
export function botEntities(bots: readonly Bot[], tick: number, tickRate = TICK_RATE): EntityState[] {
  const t = tick / tickRate;
  return bots.map((b) => {
    if (b.walkRadius === 0) return { id: b.id, x: b.x, z: b.z, heading: b.heading, animation: Animation.Idle };
    const a = b.phase + t * BOT_TURN_RATE;
    // Moving anticlockwise: velocity is (-sin a, cos a); heading faces +z at 0.
    return {
      id: b.id,
      x: b.x + Math.cos(a) * b.walkRadius,
      z: b.z + Math.sin(a) * b.walkRadius,
      heading: Math.atan2(-Math.sin(a), Math.cos(a)),
      animation: Animation.Walk,
    };
  });
}

function wrap(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

export function encodeWelcome(playerId: number, spawn: { x: number; z: number }, serverTimeMs: number, tickRate = TICK_RATE): Uint8Array {
  const b = new flatbuffers.Builder(64);
  const tok = b.createString('offline');
  Welcome.startWelcome(b);
  Welcome.addPlayerId(b, playerId);
  Welcome.addGuestToken(b, tok);
  Welcome.addSpawn(b, Vec2.createVec2(b, spawn.x, spawn.z));
  Welcome.addTickRate(b, tickRate);
  Welcome.addServerTimeMs(b, BigInt(serverTimeMs));
  return wrap(b, Message.Welcome, Welcome.endWelcome(b));
}

/** Snapshot bytes as the server sends them: decimetre positions and heading in 1/256 turns. */
export function encodeSnapshot(tick: number, lastInputSeq: number, entities: readonly EntityState[]): Uint8Array {
  const b = new flatbuffers.Builder(64 + entities.length * 8);
  Snapshot.startEntitiesVector(b, entities.length);
  for (let i = entities.length - 1; i >= 0; i--) {
    const e = entities[i];
    const turn = (((e.heading / (Math.PI * 2)) % 1) + 1) % 1;
    Entity.createEntity(b, e.id, Math.round(e.x * 10), Math.round(e.z * 10), Math.round(turn * 256) % 256, e.animation);
  }
  const vec = b.endVector();
  return wrap(b, Message.Snapshot, Snapshot.createSnapshot(b, tick, lastInputSeq, vec));
}

export interface OfflineTimers {
  setTimer(fn: () => void, ms: number): unknown;
  setRepeat(fn: () => void, ms: number): unknown;
  clearRepeat(id: unknown): void;
}

const browserTimers: OfflineTimers = {
  setTimer: (fn, ms) => setTimeout(fn, ms),
  setRepeat: (fn, ms) => setInterval(fn, ms),
  clearRepeat: (id) => clearInterval(id as ReturnType<typeof setInterval>),
};

/**
 * A Transport with a local stand-in for the server: it opens on the next tick, answers Hello with
 * a Welcome, then sends a snapshot of the bots every server tick. Everything the client sends is
 * dropped after that.
 */
export class OfflineTransport implements Transport {
  onMessage: ((bytes: Uint8Array) => void) | null = null;
  onOpen: (() => void) | null = null;
  onClose: (() => void) | null = null;
  readonly bots: Bot[];
  private tick = 0;
  private repeat: unknown = null;
  private welcomed = false;
  private closed = false;

  constructor(
    private readonly opts: OfflineOptions,
    private readonly timers: OfflineTimers = browserTimers,
  ) {
    this.bots = makeBots(opts.seed, opts.bots, opts.spawn);
    timers.setTimer(() => {
      if (!this.closed) this.onOpen?.();
    }, 0);
  }

  send(): void {
    // The first message is Hello (Session sends it on open).
    if (this.welcomed || this.closed) return;
    this.welcomed = true;
    this.onMessage?.(encodeWelcome(OFFLINE_PLAYER_ID, this.opts.spawn, OFFLINE_SERVER_TIME_MS));
    this.snapshot();
    this.repeat = this.timers.setRepeat(() => this.snapshot(), 1000 / TICK_RATE);
  }

  close(): void {
    this.closed = true;
    if (this.repeat !== null) this.timers.clearRepeat(this.repeat);
    this.repeat = null;
  }

  private snapshot(): void {
    this.onMessage?.(encodeSnapshot(this.tick, 0, botEntities(this.bots, this.tick)));
    this.tick++;
  }
}

/** A token store that lives only in memory, so offline mode never overwrites the saved guest token. */
export function memoryTokenStore(): TokenStore {
  let token = '';
  return {
    get: () => token,
    set: (t) => {
      token = t;
    },
  };
}
