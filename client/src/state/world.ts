import type { ServerMessage } from '../net/codec';
import { BankStore } from './bank';
import { CardTableStore } from './cardTables';
import { ChatStore } from './chat';
import { SharedClock } from './clock';
import type { CollisionGrid } from './collision';
import { GainTracker } from './gains';
import { HitQueue } from './combat';
import { Interpolator } from './interpolation';
import { ItemState } from './items';
import { type PendingInput, Prediction, RUN_SPEED, WALK_SPEED } from './prediction';
import { SkillState } from './skills';
import { bankResultText, levelUpText, skillResultText } from './skillMessages';
import { TradeStore } from './trade';

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
  /** Metres per second; D-044 defaults until Welcome sends the server's values. */
  walkSpeed = WALK_SPEED;
  runSpeed = RUN_SPEED;
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
  /** CombatHit batches for render to drain each frame. */
  readonly hits = new HitQueue();
  /** XP and gold gains worked out from PlayerStats, for the world view. */
  readonly gains = new GainTracker();
  /** Pending trade request, open trade window and last trade result; subscribe for changes. */
  readonly trade = new TradeStore();
  /** Open bank slots and the last bank result; subscribe for changes. */
  readonly bank = new BankStore();
  /** Card table seat and turn state, the local player's seat and the last result; subscribe for changes. */
  readonly cardTables = new CardTableStore();
  /** Chat log and overhead messages from the server's ChatMessages. */
  readonly chat = new ChatStore();
  /** Skill levels and XP, felled trees, fishing spot positions, and LevelUp and SkillResult events to drain. */
  readonly skills = new SkillState();
  /** The server's clock (UTC ms), kept as an offset from the local one; drives the sun. */
  readonly clock = new SharedClock();

  /** Gives prediction the collision grid; it applies now and after every Welcome. */
  setCollision(grid: CollisionGrid | null): void {
    this.collision = grid;
    this.prediction.grid = grid;
  }

  handle(msg: ServerMessage, now: number): void {
    if (msg.type === 'welcome') {
      this.playerId = msg.playerId;
      this.tickRate = msg.tickRate;
      // A missing or zero speed (older server) keeps the current value.
      if (msg.walkSpeed) this.walkSpeed = msg.walkSpeed;
      if (msg.runSpeed) this.runSpeed = msg.runSpeed;
      if (msg.serverTimeMs) this.clock.sync(msg.serverTimeMs);
      this.prediction = new Prediction(msg.spawn, this.collision);
      this.interp.clear();
      this.animation = 0;
      this.restartSeconds = null;
      this.items.resetEntities();
      this.hits.clear();
      this.gains.reset();
      this.trade.clear();
      this.bank.clear();
      this.cardTables.clear();
      this.chat.clearOverheads();
      this.skills.reset();
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
      this.gains.update(msg.xp, msg.gold, now);
    } else if (msg.type === 'inventory') {
      this.items.setInventory(msg);
    } else if (msg.type === 'itemCatalog') {
      this.items.setCatalog(msg.items);
    } else if (msg.type === 'entityInfo') {
      this.items.setEntityInfo(msg);
    } else if (msg.type === 'entityHealth') {
      this.items.setHealth(msg.entries);
    } else if (msg.type === 'groundItems') {
      this.items.setGroundItems(msg.items);
    } else if (msg.type === 'shopResult') {
      this.items.setShopResult(msg.code);
    } else if (msg.type === 'npcCatalog') {
      this.items.setNpcCatalog(msg.npcs);
    } else if (msg.type === 'tradeRequestNotice') {
      this.trade.setRequest(msg.fromId);
    } else if (msg.type === 'tradeState') {
      this.trade.setState(msg);
    } else if (msg.type === 'tradeResult') {
      this.trade.setResult(msg.code);
    } else if (msg.type === 'bankState') {
      this.bank.setState(msg.slots);
    } else if (msg.type === 'bankResult') {
      this.bank.setResult(msg.code);
      const text = bankResultText(msg.code);
      if (text) this.chat.system(text);
    } else if (msg.type === 'cardTableState') {
      this.cardTables.setState(msg, this.playerId);
    } else if (msg.type === 'cardTableResult') {
      this.cardTables.setResult(msg.code);
    } else if (msg.type === 'cardTableCatalog') {
      this.cardTables.setCatalog(msg.tables);
    } else if (msg.type === 'shedState') {
      this.cardTables.setShedState(msg);
    } else if (msg.type === 'shedHand') {
      this.cardTables.setHand(msg);
    } else if (msg.type === 'combatHit') {
      this.hits.push(msg.tick, msg.hits);
    } else if (msg.type === 'skillStats') {
      this.skills.setStats(msg.skills);
    } else if (msg.type === 'resourceState') {
      this.skills.setResources(msg.felledTrees, msg.fishingSpots);
    } else if (msg.type === 'levelUp') {
      this.skills.levelUps.push({ entityId: msg.entityId, skill: msg.skill, level: msg.level });
      if (msg.entityId === this.playerId) this.chat.system(levelUpText(msg));
    } else if (msg.type === 'skillResult') {
      this.skills.results.push({ skill: msg.skill, code: msg.code, itemId: msg.itemId, level: msg.level });
      this.chat.system(skillResultText(msg, (id) => this.items.item(id)?.name));
    } else if (msg.type === 'chat') {
      this.chat.push(msg.speakerId, msg.text, now);
    }
  }

  /** Predicts one local input at the walk or run speed; the caller sends it with the matching Run flag. */
  applyMove(moveX: number, moveZ: number, dt: number, running: boolean): PendingInput {
    return this.prediction.applyInput(moveX, moveZ, dt, running ? this.runSpeed : this.walkSpeed);
  }

  get local(): PlayerView {
    return { id: this.playerId, x: this.prediction.pos.x, z: this.prediction.pos.z, heading: this.heading, animation: this.animation };
  }

  remotes(now: number): PlayerView[] {
    return this.interp.sample(now);
  }
}
