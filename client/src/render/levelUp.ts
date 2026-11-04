import * as THREE from 'three';
import { CHARACTER_HEIGHT } from './character';
import type { NpcView, PlayerView } from './worldView';

/** How long a level-up effect lasts. Placeholder from PRD open question Q1 (2000 ms), tuned after playtests. */
export const LEVEL_UP_EFFECT_MS = 2000;
/** Most effects shown at once; the oldest goes first past this. */
export const MAX_LEVEL_UP_EFFECTS = 16;
/** Sparks in one effect. */
export const SPARKS_PER_EFFECT = 6;

const SPARK_COLOR = 0xffe066;
const RING_RADIUS = 0.55;
const RISE = CHARACTER_HEIGHT + 0.4;
const SPARK_SIZE = 0.16;
/** One turn of the ring of sparks, in seconds. */
const TURN_S = 1.5;

export interface LevelUpEffect {
  readonly entityId: number;
  readonly bornMs: number;
  /** World position of the effect's centre, updated each frame. */
  readonly position: THREE.Vector3;
}

/**
 * Level-up effect: a ring of gold sparks that spirals up an entity and shrinks away. One InstancedMesh
 * (one draw call) with an unlit material, so it adds no point light and keeps the 8-light limit.
 */
export class LevelUpEffects {
  readonly group = new THREE.Group();
  private readonly mesh: THREE.InstancedMesh;
  private readonly effects: LevelUpEffect[] = [];
  private readonly m = new THREE.Matrix4();
  private readonly pos = new THREE.Vector3();
  private readonly scale = new THREE.Vector3();
  private readonly rot = new THREE.Quaternion();
  private readonly axisY = new THREE.Vector3(0, 1, 0);

  constructor() {
    this.mesh = new THREE.InstancedMesh(
      new THREE.OctahedronGeometry(SPARK_SIZE),
      new THREE.MeshBasicMaterial({ color: SPARK_COLOR }),
      MAX_LEVEL_UP_EFFECTS * SPARKS_PER_EFFECT,
    );
    this.mesh.name = 'level-up-sparks';
    this.mesh.count = 0;
    this.mesh.frustumCulled = false;
    this.group.name = 'level-ups';
    this.group.add(this.mesh);
  }

  /** Effects currently shown, oldest first. */
  get active(): readonly LevelUpEffect[] {
    return this.effects;
  }

  /** Starts effects for new level-ups on entities in view, drops finished ones, and follows the entities. */
  update(
    view: { local: PlayerView; remotes: readonly PlayerView[]; npcs?: readonly NpcView[]; levelUps?: readonly { entityId: number }[] },
    nowMs: number,
  ): void {
    const where = new Map<number, { x: number; z: number }>();
    where.set(view.local.id, view.local);
    for (const p of view.remotes) where.set(p.id, p);
    for (const n of view.npcs ?? []) where.set(n.id, n);

    for (const e of view.levelUps ?? []) {
      if (where.has(e.entityId)) this.effects.push({ entityId: e.entityId, bornMs: nowMs, position: new THREE.Vector3() });
    }
    for (let i = this.effects.length - 1; i >= 0; i--) {
      const e = this.effects[i];
      if (nowMs - e.bornMs >= LEVEL_UP_EFFECT_MS || !where.has(e.entityId)) this.effects.splice(i, 1);
    }
    if (this.effects.length > MAX_LEVEL_UP_EFFECTS) this.effects.splice(0, this.effects.length - MAX_LEVEL_UP_EFFECTS);

    let n = 0;
    for (const e of this.effects) {
      const p = where.get(e.entityId)!;
      const u = (nowMs - e.bornMs) / LEVEL_UP_EFFECT_MS;
      e.position.set(p.x, u * RISE, p.z);
      const size = Math.sin(Math.min(1, u) * Math.PI);
      for (let k = 0; k < SPARKS_PER_EFFECT; k++) {
        const a = (k / SPARKS_PER_EFFECT) * Math.PI * 2 + ((nowMs / 1000) / TURN_S) * Math.PI * 2;
        this.pos.set(p.x + Math.cos(a) * RING_RADIUS, e.position.y + (k % 2) * 0.2, p.z + Math.sin(a) * RING_RADIUS);
        this.rot.setFromAxisAngle(this.axisY, a);
        this.m.compose(this.pos, this.rot, this.scale.setScalar(Math.max(size, 0.001)));
        this.mesh.setMatrixAt(n++, this.m);
      }
    }
    this.mesh.count = n;
    this.mesh.instanceMatrix.needsUpdate = true;
  }
}
