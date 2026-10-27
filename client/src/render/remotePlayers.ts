import * as THREE from 'three';
import { PARTS, makePartGeometry, makePartMaterial, poseRoll, swingAngle, WALK_SPEED_RAD_PER_UNIT } from './character';
import { SLOTS, equipmentMaterial, itemColor, slotGeometry } from './equipment';
import type { EntityLookup, PlayerView } from './worldView';

export const MAX_REMOTE_PLAYERS = 100;

interface Track {
  x: number;
  z: number;
  phase: number;
}

/** Draws remote players: one InstancedMesh per body part, so draw calls do not grow with player count. */
export class RemotePlayers {
  readonly group = new THREE.Group();
  private readonly meshes: THREE.InstancedMesh[];
  /**
   * One InstancedMesh per (slot, item colour), made on first use and kept, so equipment costs one
   * draw call per distinct worn item no matter how many players wear it.
   */
  readonly equipment = new Map<string, THREE.InstancedMesh>();
  private readonly capacity: number;
  private readonly partMatrices = PARTS.map(() => new THREE.Matrix4());
  private readonly slotParts = SLOTS.map((def) => PARTS.findIndex((p) => p.name === def.part));
  private readonly tracks = new Map<number, Track>();
  private readonly q = new THREE.Quaternion();
  private readonly qs = new THREE.Quaternion();
  private readonly pos = new THREE.Vector3();
  private readonly one = new THREE.Vector3(1, 1, 1);
  private readonly axisX = new THREE.Vector3(1, 0, 0);
  private readonly axisZ = new THREE.Vector3(0, 0, 1);
  private readonly axisY = new THREE.Vector3(0, 1, 0);

  constructor(capacity = MAX_REMOTE_PLAYERS) {
    this.capacity = capacity;
    this.meshes = PARTS.map((part) => {
      const mesh = new THREE.InstancedMesh(makePartGeometry(part), makePartMaterial(part), capacity);
      mesh.count = 0;
      mesh.frustumCulled = false;
      this.group.add(mesh);
      return mesh;
    });
  }

  private equipmentMesh(slot: number, color: number): THREE.InstancedMesh {
    const key = `${SLOTS[slot].slot}:${color}`;
    let mesh = this.equipment.get(key);
    if (!mesh) {
      mesh = new THREE.InstancedMesh(slotGeometry(SLOTS[slot]), equipmentMaterial(color), this.capacity);
      mesh.name = `equip:${key}`;
      mesh.count = 0;
      mesh.frustumCulled = false;
      this.group.add(mesh);
      this.equipment.set(key, mesh);
    }
    return mesh;
  }

  update(players: readonly PlayerView[], lookup?: EntityLookup): void {
    for (const mesh of this.equipment.values()) mesh.count = 0;
    const capacity = this.meshes[0].instanceMatrix.count;
    const n = Math.min(players.length, capacity);
    const seen = new Set<number>();
    for (let i = 0; i < n; i++) {
      const p = players[i];
      seen.add(p.id);
      let t = this.tracks.get(p.id);
      if (!t) {
        t = { x: p.x, z: p.z, phase: 0 };
        this.tracks.set(p.id, t);
      }
      const d = Math.hypot(p.x - t.x, p.z - t.z);
      t.phase = d > 1e-4 ? t.phase + d * WALK_SPEED_RAD_PER_UNIT : 0;
      t.x = p.x;
      t.z = p.z;

      this.q.setFromAxisAngle(this.axisY, p.heading);
      for (let k = 0; k < PARTS.length; k++) {
        const part = PARTS[k];
        const roll = poseRoll(part, p.animation ?? 0);
        if (roll !== 0) this.qs.setFromAxisAngle(this.axisZ, roll);
        else this.qs.setFromAxisAngle(this.axisX, swingAngle(t.phase, part));
        // world = root(pos, yaw) * pivot translate * swing rotation
        const [px, py, pz] = part.pivot;
        this.pos.set(px, py, pz).applyQuaternion(this.q);
        this.pos.x += p.x;
        this.pos.z += p.z;
        const rot = this.qs.premultiply(this.q);
        this.partMatrices[k].compose(this.pos, rot, this.one);
        this.meshes[k].setMatrixAt(i, this.partMatrices[k]);
      }
      const eq = lookup?.entityInfo(p.id)?.equipment;
      if (eq) {
        for (let s = 0; s < SLOTS.length; s++) {
          const itemId = eq[SLOTS[s].slot];
          if (itemId === 0) continue;
          const mesh = this.equipmentMesh(s, itemColor(itemId, lookup));
          mesh.setMatrixAt(mesh.count++, this.partMatrices[this.slotParts[s]]);
        }
      }
    }
    for (const id of this.tracks.keys()) if (!seen.has(id)) this.tracks.delete(id);
    for (const mesh of this.meshes) {
      mesh.count = n;
      mesh.instanceMatrix.needsUpdate = true;
    }
    for (const mesh of this.equipment.values()) mesh.instanceMatrix.needsUpdate = true;
  }
}
