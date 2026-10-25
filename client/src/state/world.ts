import type { ServerMessage } from '../net/codec';
import type { CollisionGrid } from './collision';
import { Interpolator } from './interpolation';
import { ItemState } from './items';
import { Prediction } from './prediction';

export interface PlayerView {
  readonly id: number;
  readonly x: number;
  readonly z: number;
  readonly heading: number;
  /** Animation id from the latest snapshot (protocol Animation). */
  readonly animation: number;
}

/** Game state: predicted local player plus interpolated remotes. */
export class GameState {
  playerId = -1;
  tickRate = 20;
  heading = 0;
  /** Local player's animation id from the latest snapshot. */
  animation = 0;
  /** Seconds until the server restarts, or null when no restart is announced. */
  restartSeconds: number | null = null;
  prediction = new Prediction();
  /** Collision grid for prediction; null (not loaded yet) means free movement. */
  private collision: CollisionGrid | null = null;
  readonly interp = new Interpolator();
  /** Stats, inventory, item catalog, entity info and health; subscribe for changes. */
  readonly items = new ItemState();

  /** Gives prediction the collision grid; it applies now and after every Welcome. */
  setCollision(grid: CollisionGrid | null): void {
    this.collision = grid;
    this.prediction.grid = grid;
  }

  handle(msg: ServerMessage, now: number): void {
    if (msg.type === 'welcome') {
      this.playerId = msg.playerId;
      this.tickRate = msg.tickRate;
      this.prediction = new Prediction(msg.spawn, this.collision);
      this.interp.clear();
      this.animation = 0;
      this.restartSeconds = null;
      this.items.resetEntities();
    } else if (msg.type === 'restartCountdown') {
      this.restartSeconds = msg.seconds;
    } else if (msg.type === 'snapshot') {
      const me = msg.entities.find((e) => e.id === this.playerId);
      if (me) {
        this.prediction.reconcile({ x: me.x, z: me.z }, msg.lastInputSeq);
        this.animation = me.animation;
      }
      this.interp.push(now, msg.entities.filter((e) => e.id !== this.playerId));
      this.items.setVisible(msg.entities.map((e) => e.id));
    } else if (msg.type === 'playerStats') {
      this.items.setStats(msg);
    } else if (msg.type === 'inventory') {
      this.items.setInventory(msg);
    } else if (msg.type === 'itemCatalog') {
      this.items.setCatalog(msg.items);
    } else if (msg.type === 'entityInfo') {
      this.items.setEntityInfo(msg);
    } else if (msg.type === 'entityHealth') {
      this.items.setHealth(msg.entries);
    }
  }

  get local(): PlayerView {
    return { id: this.playerId, x: this.prediction.pos.x, z: this.prediction.pos.z, heading: this.heading, animation: this.animation };
  }

  remotes(now: number): PlayerView[] {
    return this.interp.sample(now);
  }
}
