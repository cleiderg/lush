import * as THREE from 'three';
import { Character } from './character';
import { CameraRig } from './cameraRig';
import { RemotePlayers } from './remotePlayers';
import { HealthBars, type BarEntry } from './healthBars';
import { Hitsplats } from './hitsplats';
import { CampusMap, loadCampus } from './campusMap';
import { GroundItems } from './groundItems';
import { Npcs } from './npcs';
import { AreaMarkers, loadAreas } from './gameAreas';
import type { CampusData } from './mapData';
import type { WorldView } from './worldView';

const SKY = 0x6aa7d8;
/** Ground extends this far (metres) past the map bounds so its edge stays out of view. */
const GROUND_MARGIN = 200;

/** Plain ground under and around the campus map. Bounds are in decimetres. */
function makeGround(b: CampusData['bounds']): THREE.Mesh {
  const w = (b.maxX - b.minX) / 10 + 2 * GROUND_MARGIN;
  const d = (b.maxZ - b.minZ) / 10 + 2 * GROUND_MARGIN;
  const mesh = new THREE.Mesh(
    new THREE.PlaneGeometry(w, d).rotateX(-Math.PI / 2),
    new THREE.MeshLambertMaterial({ color: 0x5f9e4f }),
  );
  mesh.position.set((b.minX + b.maxX) / 20, 0, (b.minZ + b.maxZ) / 20);
  mesh.name = 'ground';
  return mesh;
}

/** Everything drawn for the world. Reads a WorldView; never writes to it. */
export class WorldScene {
  readonly scene = new THREE.Scene();
  readonly rig = new CameraRig();
  readonly map = new CampusMap(loadCampus());
  readonly areas = new AreaMarkers(loadAreas());
  private readonly player = new Character();
  private readonly remotes = new RemotePlayers();
  readonly healthBars = new HealthBars();
  private readonly bars: { x: number; z: number; ratio: number }[] = [];
  readonly hitsplats = new Hitsplats();
  /** Created on the first view that carries groundItems, so views without it leave the scene as before. */
  private groundItems: GroundItems | null = null;
  /** Created on the first view that carries npcs, like groundItems. */
  private npcs: Npcs | null = null;
  private lastX: number | null = null;
  private lastZ = 0;

  /** `now` is the render clock in milliseconds; tests pass a controlled one. */
  constructor(private readonly now: () => number = () => performance.now()) {
    this.scene.background = new THREE.Color(SKY);
    this.scene.add(new THREE.AmbientLight(0xffffff, 1.6));
    const sun = new THREE.DirectionalLight(0xffffff, 1.6);
    sun.position.set(30, 60, 20);
    this.scene.add(sun);
    this.scene.add(makeGround(loadCampus().bounds), this.map.group, this.areas.group, this.player.root, this.remotes.group, this.healthBars.group);
  }

  update(view: WorldView): void {
    const { local } = view;
    const dist = this.lastX === null ? 0 : Math.hypot(local.x - this.lastX, local.z - this.lastZ);
    this.lastX = local.x;
    this.lastZ = local.z;
    this.player.update(local.x, local.z, local.heading, dist, local.animation ?? 0);
    const lookup = view.entities;
    this.player.setEquipment(lookup?.entityInfo(local.id)?.equipment, lookup);
    this.remotes.update(view.remotes, lookup);
    if (view.groundItems) {
      if (!this.groundItems) {
        this.groundItems = new GroundItems();
        this.scene.add(this.groundItems.group);
      }
      this.groundItems.update(view.groundItems);
    } else if (this.groundItems) {
      this.groundItems.update([]);
    }
    if (view.npcs) {
      if (!this.npcs) {
        this.npcs = new Npcs();
        this.scene.add(this.npcs.group);
      }
      this.npcs.update(view.npcs);
    } else if (this.npcs) {
      this.npcs.update([]);
    }
    this.rig.follow(local.x, local.z);
    // Hitsplats join the scene with the first hit, so a view without hits leaves the graph as it was.
    if (view.hits?.length && !this.hitsplats.group.parent) this.scene.add(this.hitsplats.group);
    if (this.hitsplats.group.parent) this.hitsplats.update(view, this.now(), this.rig.camera.quaternion);
    this.healthBars.update(this.barEntries(view), this.rig.camera.quaternion);
  }

  /** Bar entries for every drawn player and NPC, reusing the same objects each frame. */
  private barEntries(view: WorldView): readonly BarEntry[] {
    const lookup = view.entities;
    let n = 0;
    const add = (p: { id: number; x: number; z: number }) => {
      const ratio = lookup!.healthRatio(p.id);
      if (!(ratio < 1)) return;
      const e = (this.bars[n] ??= { x: 0, z: 0, ratio: 1 });
      e.x = p.x;
      e.z = p.z;
      e.ratio = ratio;
      n++;
    };
    if (lookup) {
      add(view.local);
      for (const p of view.remotes) add(p);
      for (const p of view.npcs ?? []) add(p);
    }
    this.bars.length = n;
    return this.bars;
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
