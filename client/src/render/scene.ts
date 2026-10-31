import * as THREE from 'three';
import { Character } from './character';
import { CameraRig } from './cameraRig';
import { MAX_REMOTE_PLAYERS, RemotePlayers } from './remotePlayers';
import { BlobShadows, PLAYER_SHADOW_RADIUS, npcShadowRadius, type ShadowEntry } from './blobShadows';
import { HealthBars, type BarEntry } from './healthBars';
import { Hitsplats } from './hitsplats';
import { CampusMap, loadCampus } from './campusMap';
import { GroundItems } from './groundItems';
import { Npcs } from './npcs';
import { ChatText } from './chatText';
import { AreaMarkers, loadAreas } from './gameAreas';
import { CardTables, loadCardTables } from './cardTables';
import { Nature } from './nature';
import { Props, loadProps } from './propKit';
import { DEFAULT_SKY, SkyRig } from './sky';
import { LampLights, lampSources } from './lights';
import type { CampusData } from './mapData';
import type { WorldView } from './worldView';

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
  mesh.receiveShadow = true;
  return mesh;
}

/** Everything drawn for the world. Reads a WorldView; never writes to it. */
export class WorldScene {
  readonly scene = new THREE.Scene();
  readonly rig = new CameraRig();
  readonly map = new CampusMap(loadCampus());
  readonly areas = new AreaMarkers(loadAreas());
  readonly cardTables = new CardTables(loadCardTables());
  /** Grass tufts and flowers, instanced per map chunk. */
  readonly nature = new Nature(loadCampus(), { exclude: [loadCardTables().plaza] });
  /** Campus props from props.json, merged per chunk. */
  readonly props = new Props(loadProps());
  /** Sun, ambient, sky colour and fog, following WorldView.sky. */
  readonly sky = new SkyRig();
  /** Warm point lights and glowing heads for lamp posts and lanterns, lit from sunset to sunrise. */
  readonly lamps = new LampLights(lampSources(loadProps()), this.props.glow);
  private readonly player = new Character();
  private readonly remotes = new RemotePlayers();
  readonly healthBars = new HealthBars();
  readonly shadows = new BlobShadows();
  private readonly shadowEntries: { x: number; z: number; radius: number }[] = [];
  private readonly bars: { x: number; z: number; ratio: number }[] = [];
  readonly hitsplats = new Hitsplats();
  readonly chat = new ChatText();
  /** Created on the first view that carries groundItems, so views without it leave the scene as before. */
  private groundItems: GroundItems | null = null;
  /** Created on the first view that carries npcs, like groundItems. */
  private npcs: Npcs | null = null;
  private lastX: number | null = null;
  private lastZ = 0;

  /** `now` is the render clock in milliseconds; tests pass a controlled one. */
  constructor(private readonly now: () => number = () => performance.now()) {
    this.sky.attach(this.scene);
    this.sky.update(undefined, 0, 0);
    this.scene.add(makeGround(loadCampus().bounds), this.map.group, this.areas.group, this.player.root, this.remotes.group, this.shadows.group, this.healthBars.group);
    // After the players: tests find the remote-player instanced meshes as the first ones in the scene.
    this.scene.add(this.cardTables.group, this.props.group, this.nature.group, this.lamps.group);
    this.lamps.update(DEFAULT_SKY.elevation, this.rig.camera.position, this.now());
    // Cull again for the camera actually rendering, in case its aspect changed since the last view.
    this.scene.onBeforeRender = (_renderer, _scene, camera) => this.nature.cull(camera);
    // The local player is drawn (at the origin) before the first view arrives, as a plain mesh would be.
    this.player.update(0, 0, 0, 0);
    this.remotes.update([], undefined, 0, this.player);
    this.shadows.update([{ x: 0, z: 0, radius: PLAYER_SHADOW_RADIUS }]);
    // Nature starts culled as for a camera following the origin, where the player is drawn until the first view.
    const start = new CameraRig(this.rig.camera.aspect);
    start.follow(0, 0);
    this.nature.cull(start.camera);
  }

  update(view: WorldView): void {
    const { local } = view;
    const dist = this.lastX === null ? 0 : Math.hypot(local.x - this.lastX, local.z - this.lastZ);
    this.lastX = local.x;
    this.lastZ = local.z;
    const time = this.now() / 1000;
    this.player.update(local.x, local.z, local.heading, dist, local.animation ?? 0, time);
    const lookup = view.entities;
    this.player.setEquipment(lookup?.entityInfo(local.id)?.equipment, lookup);
    this.remotes.update(view.remotes, lookup, time, this.player);
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
    this.shadows.update(this.shadowList(view));
    this.rig.follow(local.x, local.z);
    this.nature.cull(this.rig.camera);
    this.sky.update(view.sky, local.x, local.z);
    this.lamps.update((view.sky ?? DEFAULT_SKY).elevation, this.rig.camera.position, this.now());
    // Hitsplats join the scene with the first hit, so a view without hits leaves the graph as it was.
    if (view.hits?.length && !this.hitsplats.group.parent) this.scene.add(this.hitsplats.group);
    if (this.hitsplats.group.parent) this.hitsplats.update(view, this.now(), this.rig.camera.quaternion);
    this.healthBars.update(this.barEntries(view), this.rig.camera.quaternion);
    // Chat joins the scene with the first message, like hitsplats; it needs the DOM only from then on.
    if (view.chat?.length && !this.chat.group.parent) this.scene.add(this.chat.group);
    if (this.chat.group.parent) this.chat.update(view);
  }

  /** One shadow per drawn player and NPC, reusing the same objects each frame. */
  private shadowList(view: WorldView): readonly ShadowEntry[] {
    let n = 0;
    const add = (x: number, z: number, radius: number) => {
      const e = (this.shadowEntries[n++] ??= { x: 0, z: 0, radius: 0 });
      e.x = x;
      e.z = z;
      e.radius = radius;
    };
    add(view.local.x, view.local.z, PLAYER_SHADOW_RADIUS);
    const remotes = Math.min(view.remotes.length, MAX_REMOTE_PLAYERS);
    for (let i = 0; i < remotes; i++) add(view.remotes[i].x, view.remotes[i].z, PLAYER_SHADOW_RADIUS);
    for (const p of view.npcs ?? []) add(p.x, p.z, npcShadowRadius(p.npcType));
    this.shadowEntries.length = n;
    return this.shadowEntries;
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

/**
 * Whether the renderer would draw `mesh` for `frustum`. Like three.js, an instanced mesh is culled by
 * the bounding sphere of all its instances, and only when frustumCulled is set.
 */
function inView(frustum: THREE.Frustum, mesh: THREE.Mesh): boolean {
  if (!mesh.frustumCulled) return true;
  const inst = mesh as THREE.InstancedMesh;
  let s: THREE.Sphere;
  if (inst.isInstancedMesh) {
    if (!inst.boundingSphere) inst.computeBoundingSphere();
    s = inst.boundingSphere!.clone();
  } else {
    if (!mesh.geometry.boundingSphere) mesh.geometry.computeBoundingSphere();
    s = mesh.geometry.boundingSphere!.clone();
  }
  return frustum.intersectsSphere(s.applyMatrix4(mesh.matrixWorld));
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
    if (!inView(frustum, mesh)) return;
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
    if (!inView(frustum, mesh)) return;
    const per = (g.index ? g.index.count : g.getAttribute('position').count) / 3;
    tris += per * (inst.isInstancedMesh ? inst.count : 1);
  });
  return tris;
}
