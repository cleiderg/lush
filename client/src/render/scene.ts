import * as THREE from 'three';
import { Character } from './character';
import { CameraRig } from './cameraRig';
import { RemotePlayers } from './remotePlayers';
import { CampusMap, loadCampus } from './campusMap';
import type { WorldView } from './worldView';

const SKY = 0x6aa7d8;

/** Plain ground under and around the campus map. */
function makeGround(): THREE.Mesh {
  const mesh = new THREE.Mesh(
    new THREE.PlaneGeometry(400, 400).rotateX(-Math.PI / 2),
    new THREE.MeshLambertMaterial({ color: 0x5f9e4f }),
  );
  mesh.name = 'ground';
  return mesh;
}

/** Everything drawn for the world. Reads a WorldView; never writes to it. */
export class WorldScene {
  readonly scene = new THREE.Scene();
  readonly rig = new CameraRig();
  readonly map = new CampusMap(loadCampus());
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
    this.scene.add(makeGround(), this.map.group, this.player.root, this.remotes.group);
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

/** Draw calls after frustum culling, as the renderer would issue them for `camera`. */
export function countDrawCallsInView(root: THREE.Object3D, camera: THREE.Camera): number {
  camera.updateMatrixWorld();
  const frustum = new THREE.Frustum().setFromProjectionMatrix(
    new THREE.Matrix4().multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse),
  );
  let n = 0;
  root.traverseVisible((o) => {
    const mesh = o as THREE.Mesh;
    if (!mesh.isMesh) return;
    const inst = o as THREE.InstancedMesh;
    if (inst.isInstancedMesh && inst.count === 0) return;
    if (mesh.frustumCulled && !inst.isInstancedMesh) {
      if (!mesh.geometry.boundingSphere) mesh.geometry.computeBoundingSphere();
      const s = mesh.geometry.boundingSphere!.clone().applyMatrix4(mesh.matrixWorld);
      if (!frustum.intersectsSphere(s)) return;
    }
    n++;
  });
  return n;
}

/** Triangles drawn after frustum culling (instances count each time). */
export function countTrianglesInView(root: THREE.Object3D, camera: THREE.Camera): number {
  camera.updateMatrixWorld();
  const frustum = new THREE.Frustum().setFromProjectionMatrix(
    new THREE.Matrix4().multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse),
  );
  let tris = 0;
  root.traverseVisible((o) => {
    const mesh = o as THREE.Mesh;
    if (!mesh.isMesh) return;
    const inst = o as THREE.InstancedMesh;
    const g = mesh.geometry;
    if (!inst.isInstancedMesh) {
      if (!g.boundingSphere) g.computeBoundingSphere();
      if (!frustum.intersectsSphere(g.boundingSphere!.clone().applyMatrix4(mesh.matrixWorld))) return;
    }
    const per = (g.index ? g.index.count : g.getAttribute('position').count) / 3;
    tris += per * (inst.isInstancedMesh ? inst.count : 1);
  });
  return tris;
}
