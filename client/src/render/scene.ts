import * as THREE from 'three';
import { Character } from './character';
import { CameraRig } from './cameraRig';
import { RemotePlayers } from './remotePlayers';
import type { WorldView } from './worldView';

const SKY = 0x6aa7d8;

/** Placeholder ground until the campus map lands. */
function makeGround(): THREE.Mesh {
  const mesh = new THREE.Mesh(
    new THREE.PlaneGeometry(400, 400).rotateX(-Math.PI / 2),
    new THREE.MeshLambertMaterial({ color: 0x5f9e4f }),
  );
  mesh.name = 'ground-placeholder';
  return mesh;
}

/** Everything drawn for the world. Reads a WorldView; never writes to it. */
export class WorldScene {
  readonly scene = new THREE.Scene();
  readonly rig = new CameraRig();
  private readonly player = new Character();
  private readonly remotes = new RemotePlayers();
  private lastX: number | null = null;
  private lastZ = 0;

  constructor() {
    this.scene.background = new THREE.Color(SKY);
    this.scene.add(new THREE.AmbientLight(0xffffff, 1.6));
    const sun = new THREE.DirectionalLight(0xffffff, 1.6);
    sun.position.set(30, 60, 20);
    this.scene.add(sun);
    this.scene.add(makeGround(), this.player.root, this.remotes.group);
  }

  update(view: WorldView): void {
    const { local } = view;
    const dist = this.lastX === null ? 0 : Math.hypot(local.x - this.lastX, local.z - this.lastZ);
    this.lastX = local.x;
    this.lastZ = local.z;
    this.player.update(local.x, local.z, local.heading, dist);
    this.remotes.update(view.remotes);
    this.rig.follow(local.x, local.z);
  }
}

/** Number of draw calls the scene costs: one per visible mesh (InstancedMesh counts once). */
export function countDrawCalls(root: THREE.Object3D): number {
  let n = 0;
  root.traverseVisible((o) => {
    if ((o as THREE.Mesh).isMesh) {
      const inst = o as THREE.InstancedMesh;
      if (inst.isInstancedMesh && inst.count === 0) return;
      n++;
    }
  });
  return n;
}
