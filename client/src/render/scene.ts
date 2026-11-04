import * as THREE from 'three';
import { Character, CombatAnims } from './character';
import { CameraRig, FOV } from './cameraRig';
import { MAX_REMOTE_PLAYERS, RemotePlayers } from './remotePlayers';
import { BlobShadows, PLAYER_SHADOW_RADIUS, npcShadowRadius, type ShadowEntry } from './blobShadows';
import { HealthBars, type BarEntry } from './healthBars';
import { Hitsplats } from './hitsplats';
import { LevelUpEffects } from './levelUp';
import { CampusMap, loadCampus } from './campusMap';
import { GroundItems } from './groundItems';
import { Npcs } from './npcs';
import { NpcMarkers } from './npcMarkers';
import { ChatText } from './chatText';
import { FloatTexts } from './floatText';
import { AreaMarkers, loadAreas } from './gameAreas';
import { CardTables, loadCardTables } from './cardTables';
import { createHousingLayer } from './housingLayer';
import { Nature, FLOWERS_PER_M2, GRASS_TUFTS_PER_M2, type NatureOptions } from './nature';
import { QUALITY_GRASS, QUALITY_SHADOW_MAP, type QualityPreset } from './quality';
import { Props, loadProps } from './propKit';
import { loadParkNature } from './parkNature';
import { SkillObjects, loadSkills } from './skillObjects';
import { Fountain, loadFountain } from './fountain';
import { DEFAULT_SKY, SkyRig } from './sky';
import { RoomView, ROOM_SIZE, WALL_HEIGHT } from './roomView';
import { CafeInterior, cafeLightSources } from './cafeInterior';
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

/** Bounding sphere of the room, so it fits the view at any yaw. */
const ROOM_CENTER = new THREE.Vector3(ROOM_SIZE / 2, WALL_HEIGHT / 2, ROOM_SIZE / 2);
const ROOM_RADIUS = Math.hypot(ROOM_SIZE / 2, ROOM_SIZE / 2, WALL_HEIGHT / 2) * 1.05;

/** Everything drawn for the world. Reads a WorldView; never writes to it. */
export class WorldScene {
  readonly scene = new THREE.Scene();
  readonly rig = new CameraRig();
  /** The campus, with Aldrich Park's trees, rocks, bushes, flower beds, picnic blankets and birds baked into its chunks. */
  readonly map = new CampusMap(loadCampus(), loadParkNature());
  readonly areas = new AreaMarkers(loadAreas());
  readonly cardTables = new CardTables(loadCardTables());
  /** House doors, auction signs, thrift stalls and workbenches in the housing areas. */
  readonly housing = createHousingLayer();
  /** Grass tufts and flowers, instanced per map chunk. */
  nature = new Nature(loadCampus(), this.natureOptions('high'));
  private preset: QualityPreset = 'high';
  /** Campus props from props.json, merged per chunk. */
  readonly props = new Props(loadProps());
  /** Woodcutting trees and stumps, the bank booth, the grill and the pond from skills.json. */
  readonly skills = new SkillObjects(loadSkills());
  /** The M8 fountain from fountain.json: basin, rippling water and its ring of stones. */
  readonly fountain = new Fountain(loadFountain());
  /** Sun, ambient, sky colour and fog, following WorldView.sky. */
  readonly sky = new SkyRig();
  /** Warm point lights and glowing heads for lamp posts and lanterns, lit from sunset to sunrise. */
  readonly lamps = new LampLights([...lampSources(loadProps()), ...cafeLightSources()], this.props.glow);
  /** The furnished Student Center café: floor, walls and props, two draw calls. */
  readonly cafe = new CafeInterior(this.props.glow);
  private readonly ground: THREE.Mesh;
  private readonly player = new Character();
  private readonly remotes = new RemotePlayers();
  readonly healthBars = new HealthBars();
  readonly shadows = new BlobShadows();
  private readonly shadowEntries: { x: number; z: number; radius: number }[] = [];
  private readonly bars: { x: number; z: number; ratio: number }[] = [];
  readonly hitsplats = new Hitsplats();
  /** Attack swings and hit flinches, started by the view's hits. */
  readonly combat = new CombatAnims();
  readonly levelUps = new LevelUpEffects();
  readonly chat = new ChatText();
  readonly floatTexts = new FloatTexts();
  /** Created on the first view that carries groundItems, so views without it leave the scene as before. */
  private groundItems: GroundItems | null = null;
  /** Created on the first view that carries npcs, like groundItems. */
  private npcs: Npcs | null = null;
  /** Created with the first marker, like hitsplats. */
  private npcMarkers: NpcMarkers | null = null;
  /** The home room, present only while a view carries `home`. */
  private room: RoomView | null = null;
  /** Campus objects hidden for the room, with the visibility to restore. */
  private readonly hiddenForRoom = new Map<THREE.Object3D, boolean>();
  private lastX: number | null = null;
  private lastZ = 0;

  /** `now` is the render clock in milliseconds; tests pass a controlled one. */
  constructor(private readonly now: () => number = () => performance.now()) {
    this.sky.attach(this.scene);
    this.sky.update(undefined, 0, 0);
    this.ground = makeGround(loadCampus().bounds);
    this.scene.add(this.ground, this.map.group, this.areas.group, this.player.root, this.remotes.group, this.shadows.group, this.healthBars.group);
    // After the players: tests find the remote-player instanced meshes as the first ones in the scene.
    this.scene.add(this.cardTables.group, this.housing.group, this.props.group, this.cafe.group, this.skills.group, this.fountain.group, this.nature.group, this.lamps.group);
    this.lamps.update(DEFAULT_SKY.elevation, this.rig.camera.position, this.now());
    // Cull again for the camera actually rendering, in case its aspect changed since the last view.
    this.scene.onBeforeRender = (_renderer, _scene, camera) => {
      this.nature.cull(camera);
      this.skills.cull(camera);
    };
    // The local player is drawn (at the origin) before the first view arrives, as a plain mesh would be.
    this.player.update(0, 0, 0, 0);
    this.remotes.update([], undefined, 0, this.player);
    this.shadows.update([{ x: 0, z: 0, radius: PLAYER_SHADOW_RADIUS }]);
    // Nature starts culled as for a camera following the origin, where the player is drawn until the first view.
    const start = new CameraRig(this.rig.camera.aspect);
    start.follow(0, 0);
    this.nature.cull(start.camera);
    this.skills.cull(start.camera);
  }

  private natureOptions(preset: QualityPreset): NatureOptions {
    const f = QUALITY_GRASS[preset];
    return { exclude: [loadCardTables().plaza], tuftsPerM2: GRASS_TUFTS_PER_M2 * f, flowersPerM2: FLOWERS_PER_M2 * f };
  }

  /** The current quality preset. */
  get quality(): QualityPreset {
    return this.preset;
  }

  /**
   * Switch the sun shadow map and the grass and flower density. Replaced geometries and shadow map
   * render targets are disposed. Props, lamps, sky colours and the nature draw call are unchanged.
   */
  setQuality(preset: QualityPreset): void {
    if (preset === this.preset) return;
    this.preset = preset;
    this.sky.setShadowMapSize(QUALITY_SHADOW_MAP[preset]);
    const old = this.nature;
    this.nature = new Nature(loadCampus(), this.natureOptions(preset));
    this.scene.remove(old.group);
    old.dispose();
    this.scene.add(this.nature.group);
    this.nature.cull(this.rig.camera);
  }

  update(view: WorldView): void {
    const { local } = view;
    const dist = this.lastX === null ? 0 : Math.hypot(local.x - this.lastX, local.z - this.lastZ);
    this.lastX = local.x;
    this.lastZ = local.z;
    const time = this.now() / 1000;
    this.combat.update(view.hits, time);
    this.player.update(local.x, local.z, local.heading, dist, local.animation ?? 0, time, this.combat.pose(local.id, time));
    const lookup = view.entities;
    this.player.setEquipment(lookup?.entityInfo(local.id)?.equipment, lookup);
    this.remotes.update(view.remotes, lookup, time, this.player, this.combat);
    this.skills.update(view.felledTrees);
    this.fountain.update(time);
    this.skills.setFishingSpots(view.fishingSpots);
    this.skills.setProgress(view.activeResource, view.resourceProgress);
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
      this.npcs.update(view.npcs, time, this.combat);
    } else if (this.npcs) {
      this.npcs.update([]);
    }
    this.shadows.update(this.shadowList(view));
    this.updateHome(view);
    if (this.room) {
      this.rig.follow(ROOM_CENTER.x, ROOM_CENTER.z, this.roomDistance(), ROOM_CENTER.y);
    } else {
      this.rig.follow(local.x, local.z);
      const cam = this.rig.camera.position;
      this.map.cutaway?.update(local.x, local.z, cam.x, cam.z, this.now());
      this.nature.cull(this.rig.camera);
      this.skills.cull(this.rig.camera);
    }
    this.sky.update(view.sky, local.x, local.z);
    // After the sky sets this frame's fog; at home the campus, housing included, stays hidden.
    if (!this.room) this.housing.update(this.rig.camera, this.sky.fog.far);
    this.lamps.update((view.sky ?? DEFAULT_SKY).elevation, this.rig.camera.position, this.now());
    this.map.updateSky(view.sky);
    // Hitsplats join the scene with the first hit, so a view without hits leaves the graph as it was.
    if (view.hits?.length && !this.hitsplats.group.parent) this.scene.add(this.hitsplats.group);
    if (this.hitsplats.group.parent) this.hitsplats.update(view, this.now(), this.rig.camera.quaternion);
    // Like hitsplats, the effect joins the scene with the first level-up.
    if (view.levelUps?.length && !this.levelUps.group.parent) this.scene.add(this.levelUps.group);
    if (this.levelUps.group.parent) this.levelUps.update(view, this.now());
    if (view.npcMarkers?.size && !this.npcMarkers) {
      this.npcMarkers = new NpcMarkers();
      this.scene.add(this.npcMarkers.group);
    }
    this.npcMarkers?.update(view.npcs ?? [], view.npcMarkers, this.rig.camera.quaternion, time);
    this.healthBars.update(this.barEntries(view), this.rig.camera.quaternion);
    // Chat joins the scene with the first message, like hitsplats; it needs the DOM only from then on.
    if (view.chat?.length && !this.chat.group.parent) this.scene.add(this.chat.group);
    if (this.chat.group.parent) this.chat.update(view);
    if (view.gains?.length && !this.floatTexts.group.parent) this.scene.add(this.floatTexts.group);
    if (this.floatTexts.group.parent) this.floatTexts.update(view, this.now());
  }

  /** Show the room for `view.home` and hide the campus, or undo both when it is gone. */
  private updateHome(view: WorldView): void {
    if (view.home) {
      if (!this.room) {
        this.room = new RoomView();
        this.scene.add(this.room.group);
      }
      this.room.update(view.home);
      // Checked every frame so groups added while at home (ground items, hitsplats...) are hidden too.
      for (const o of this.campusObjects()) {
        if (!this.hiddenForRoom.has(o)) this.hiddenForRoom.set(o, o.visible);
        o.visible = false;
      }
    } else if (this.room) {
      this.scene.remove(this.room.group);
      this.room = null;
      for (const [o, visible] of this.hiddenForRoom) o.visible = visible;
      this.hiddenForRoom.clear();
    }
  }

  /** Everything placed in campus space: scenery and everything standing on it. */
  private campusObjects(): THREE.Object3D[] {
    const list: (THREE.Object3D | null)[] = [
      this.ground, this.map.group, this.areas.group, this.cardTables.group, this.housing.group, this.props.group, this.cafe.group,
      this.skills.group, this.fountain.group, this.nature.group, this.lamps.group, this.player.root,
      this.remotes.group, this.shadows.group, this.healthBars.group, this.hitsplats.group, this.levelUps.group,
      this.chat.group, this.floatTexts.group, this.groundItems?.group ?? null, this.npcs?.group ?? null, this.npcMarkers?.group ?? null,
    ];
    return list.filter((o): o is THREE.Object3D => o !== null);
  }

  /** Camera distance at which the room's bounding sphere fits the narrower field of view. */
  private roomDistance(): number {
    const { camera } = this.rig;
    const tanV = Math.tan(THREE.MathUtils.degToRad(FOV) / 2);
    const half = Math.atan(Math.min(tanV, tanV * camera.aspect));
    return ROOM_RADIUS / Math.sin(half);
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
