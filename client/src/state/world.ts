import type { ServerMessage } from '../net/codec';
import { Interpolator } from './interpolation';
import { Prediction } from './prediction';

export interface PlayerView {
  readonly id: number;
  readonly x: number;
  readonly z: number;
  readonly heading: number;
}

/** Game state: predicted local player plus interpolated remotes. */
export class GameState {
  playerId = -1;
  tickRate = 20;
  heading = 0;
  prediction = new Prediction();
  readonly interp = new Interpolator();

  handle(msg: ServerMessage, now: number): void {
    if (msg.type === 'welcome') {
      this.playerId = msg.playerId;
      this.tickRate = msg.tickRate;
      this.prediction = new Prediction(msg.spawn);
      this.interp.clear();
    } else if (msg.type === 'snapshot') {
      const me = msg.entities.find((e) => e.id === this.playerId);
      if (me) this.prediction.reconcile({ x: me.x, z: me.z }, msg.lastInputSeq);
      this.interp.push(now, msg.entities.filter((e) => e.id !== this.playerId));
    }
  }

  get local(): PlayerView {
    return { id: this.playerId, x: this.prediction.pos.x, z: this.prediction.pos.z, heading: this.heading };
  }

  remotes(now: number): PlayerView[] {
    return this.interp.sample(now);
  }
}
